package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
	"github.com/shpyrd-io/shpyrd/pkg/github"
	"github.com/shpyrd-io/shpyrd/pkg/project"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// The GitHub App's webhook (RFC-0054): one URL for every installation.
// A push goes to every App of the installation's workspace that builds
// that repository's branch, through the git-push annotation PR 2's source
// watcher reads; installation events keep the connection's state. Every
// answer that applies nothing is 2xx, so GitHub does not retry it.

// githubHookBodyLimit caps a delivery: GitHub sends at most 25 MB, a push
// of many commits stays far below this.
const githubHookBodyLimit = 5 << 20

func (s *Server) githubWebhook(c *gin.Context) {
	if !s.githubDeliveries.allow(c.ClientIP()) {
		c.Header("Retry-After", "60")
		abort(c, http.StatusTooManyRequests, errors.New("too many webhook deliveries from this address; shpyrd checks the branches by itself meanwhile"))
		return
	}
	ctx := c.Request.Context()
	cl, err := s.githubApp.Client(ctx)
	if errors.Is(err, github.ErrNotConfigured) {
		abort(c, http.StatusNotFound, errors.New("not found"))
		return
	}
	if err != nil {
		s.log.Warn("github webhook: could not read the App", "err", err.Error())
		abort(c, http.StatusBadGateway, errors.New("shpyrd could not take the delivery now; it reads the branches by itself meanwhile"))
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, githubHookBodyLimit))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		abort(c, http.StatusRequestEntityTooLarge, errors.New("the delivery is larger than shpyrd reads (5 MiB)"))
		return
	}
	if err != nil {
		abort(c, http.StatusBadRequest, fmt.Errorf("could not read the delivery: %w", err))
		return
	}
	if gitauth.VerifyGitHubDelivery(c.Request.Header, body, cl.App().WebhookSecret) != nil {
		if s.hookRefusals.allow("github-app") {
			s.log.Warn("github webhook refused: the signature is not the App's", "from", c.ClientIP())
		}
		abort(c, http.StatusUnauthorized, errors.New("the delivery's signature is not the GitHub App's"))
		return
	}
	switch event := c.GetHeader("X-GitHub-Event"); event {
	case "ping":
		c.String(http.StatusOK, "pong")
	case "push":
		s.githubPush(c, body, c.GetHeader("X-GitHub-Delivery"))
	case "installation":
		s.githubInstallationEvent(c, body)
	default:
		// installation_repositories: the picker reads live. check_suite,
		// status: part 4.
		c.JSON(http.StatusAccepted, gin.H{"ignored": event})
	}
}

// githubPush leaves the push on every App of the installation's workspace
// that builds the repository's branch.
func (s *Server) githubPush(c *gin.Context, body []byte, delivery string) {
	ctx := c.Request.Context()
	var meta struct {
		Installation struct {
			ID int64 `json:"id"`
		} `json:"installation"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(body, &meta); err != nil || meta.Installation.ID == 0 || meta.Repository.FullName == "" {
		abort(c, http.StatusBadRequest, errors.New("the push names no installation or no repository"))
		return
	}
	inst, err := s.store.GitHubInstallation(ctx, meta.Installation.ID)
	if errors.Is(err, store.ErrNotFound) {
		// Before the setup callback, or never connected here.
		c.JSON(http.StatusAccepted, gin.H{"ignored": "no workspace has connected this installation"})
		return
	}
	if err != nil {
		s.log.Warn("github webhook: could not look up the installation", "installation", meta.Installation.ID, "err", err.Error())
		abort(c, http.StatusBadGateway, errors.New("shpyrd could not take the delivery now; it reads the branches by itself meanwhile"))
		return
	}
	push, err := gitauth.ParsePush(body)
	if err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	push.Delivery, push.At = delivery, time.Now().UTC()
	var list shpyrdv1.AppList
	if err := s.apps.List(ctx, &list); err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	raw, _ := json.Marshal(push)
	patch, _ := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]string{shpyrdv1.AnnotationGitPush: string(raw)}}})
	accepted := []string{}
	for i := range list.Items {
		app := &list.Items[i]
		if workspaceOf(app) != inst.Workspace || !app.DeletionTimestamp.IsZero() || !namesGitHubRepo(app, meta.Repository.FullName) || ignoredPush(app, push) != "" {
			continue
		}
		if err := s.apps.Patch(ctx, app.DeepCopy(), client.RawPatch(types.MergePatchType, patch)); err != nil {
			abort(c, http.StatusBadGateway, err)
			return
		}
		accepted = append(accepted, project.SlugOf(app))
	}
	sort.Strings(accepted)
	c.JSON(http.StatusAccepted, gin.H{"accepted": push.SHA, "projects": accepted})
}

// namesGitHubRepo says the App builds the repository GitHub names
// (owner/name), in any case: GitHub's names are not case-sensitive.
func namesGitHubRepo(app *shpyrdv1.App, fullName string) bool {
	if app.Spec.Source == nil || app.Spec.Source.Git == nil {
		return false
	}
	r, err := gitauth.Parse(app.Spec.Source.Git.URL)
	return err == nil && r.Host == "github.com" && strings.EqualFold(r.Path, fullName)
}

// githubInstallationEvent records a suspension, its end, or an uninstall;
// "created" connects nothing (the setup callback does, for the person).
func (s *Server) githubInstallationEvent(c *gin.Context, body []byte) {
	ctx := c.Request.Context()
	var ev struct {
		Action       string `json:"action"`
		Installation struct {
			ID int64 `json:"id"`
		} `json:"installation"`
		Sender struct {
			Login string `json:"login"`
		} `json:"sender"`
	}
	if err := json.Unmarshal(body, &ev); err != nil || ev.Installation.ID == 0 {
		abort(c, http.StatusBadRequest, errors.New("the event names no installation"))
		return
	}
	var action string
	var suspended, deleted bool
	switch ev.Action {
	case "suspend":
		action, suspended = "github.suspend", true
	case "unsuspend":
		action = "github.unsuspend"
	case "deleted":
		action, deleted = "github.disconnect", true
	default:
		c.JSON(http.StatusAccepted, gin.H{"ignored": "installation " + ev.Action})
		return
	}
	inst, err := s.store.GitHubInstallation(ctx, ev.Installation.ID)
	if errors.Is(err, store.ErrNotFound) {
		c.JSON(http.StatusAccepted, gin.H{"ignored": "no workspace has connected this installation"})
		return
	}
	if err != nil {
		abort(c, http.StatusBadGateway, errors.New("shpyrd could not take the delivery now"))
		return
	}
	if deleted {
		suspended = inst.SuspendedAt != nil
	}
	if err := s.store.SetGitHubInstallationState(ctx, inst.ID, suspended, deleted); err != nil {
		abort(c, http.StatusBadGateway, errors.New("shpyrd could not take the delivery now"))
		return
	}
	s.forgetGitHubRepos(inst.Workspace)
	s.auditActor(c, "github:"+firstNonEmpty(ev.Sender.Login, "unknown"), action, githubTarget(inst.ID, inst.AccountLogin), "workspace "+inst.Workspace)
	c.JSON(http.StatusAccepted, gin.H{"recorded": ev.Action})
}
