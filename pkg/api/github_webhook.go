package api

import (
	"context"
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
// A push goes to every App, in every workspace that connected the
// installation, that builds that repository's branch, through the git-push annotation PR 2's source
// watcher reads; installation events keep the connection's state. Every
// answer that applies nothing is 2xx, so GitHub does not retry it.

// githubHookBodyLimit caps a delivery: GitHub sends at most 25 MB, a push
// of many commits stays far below this.
const githubHookBodyLimit = 5 << 20

func (s *Server) githubWebhook(c *gin.Context) {
	if !s.githubDeliveries.allow(c.ClientIP()) {
		c.Header("Retry-After", "60")
		abort(c, http.StatusTooManyRequests, errors.New("too many webhook deliveries from this address; GitHub delivers it again within about 5 minutes once shpyrd answers"))
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
		abort(c, http.StatusBadGateway, errors.New("shpyrd could not take the delivery now; GitHub delivers it again within about 5 minutes once shpyrd answers"))
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
	case "installation_repositories":
		s.githubRepositoriesEvent(c, body)
	case "check_suite", "status":
		s.githubCIEvent(c, event, body, c.GetHeader("X-GitHub-Delivery"))
	default:
		c.JSON(http.StatusAccepted, gin.H{"ignored": event})
	}
}

// githubPush leaves the push on every App, in the workspaces that
// connected the installation, that builds the repository's branch.
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
	conns, ok := s.githubLiveConnections(c, meta.Installation.ID)
	if !ok {
		return
	}
	push, err := gitauth.ParsePush(body)
	if err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	push.Delivery, push.At, push.App = delivery, time.Now().UTC(), true
	raw, _ := json.Marshal(push)
	patch, _ := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]string{shpyrdv1.AnnotationGitPush: string(raw)}}})
	accepted := []string{}
	for _, inst := range conns {
		list, err := s.githubWorkspaceApps(ctx, inst.Workspace)
		if err != nil {
			s.githubPushUnavailable(c, "could not list the apps", err)
			return
		}
		for i := range list {
			app := &list[i]
			if workspaceOf(app) != inst.Workspace || !app.DeletionTimestamp.IsZero() || !namesGitHubRepo(app, meta.Repository.FullName) || ignoredPush(app, push) != "" {
				continue
			}
			if err := s.apps.Patch(ctx, app.DeepCopy(), client.RawPatch(types.MergePatchType, patch)); err != nil {
				s.githubPushUnavailable(c, "could not leave the push on "+project.SlugOf(app), err)
				return
			}
			accepted = append(accepted, githubProjectName(conns, inst.Workspace, app))
		}
	}
	sort.Strings(accepted)
	c.JSON(http.StatusAccepted, gin.H{"accepted": push.SHA, "projects": accepted})
}

// githubLiveConnections are the unsuspended connections of the
// installation a delivery names, one a workspace. Without one it has
// answered the delivery, and ok is false.
func (s *Server) githubLiveConnections(c *gin.Context, id int64) (_ []store.GitHubInstallation, ok bool) {
	conns, ok := s.githubConnections(c, id)
	if !ok {
		return nil, false
	}
	live := []store.GitHubInstallation{}
	for _, inst := range conns {
		if inst.SuspendedAt == nil {
			live = append(live, inst)
		}
	}
	if len(live) == 0 {
		// A delivery queued before the suspension, or redelivered by hand.
		c.JSON(http.StatusAccepted, gin.H{"ignored": "the installation is suspended"})
		return nil, false
	}
	return live, true
}

// githubConnections are the workspaces' connections of the installation
// a delivery names. Without one it has answered the delivery, and ok is
// false.
func (s *Server) githubConnections(c *gin.Context, id int64) (_ []store.GitHubInstallation, ok bool) {
	conns, err := s.store.GitHubInstallationConnections(c.Request.Context(), id)
	if err != nil {
		s.log.Warn("github webhook: could not look up the installation", "installation", id, "err", err.Error())
		abort(c, http.StatusBadGateway, errors.New("shpyrd could not take the delivery now; GitHub delivers it again within about 5 minutes once shpyrd answers"))
		return nil, false
	}
	if len(conns) == 0 {
		// Before the setup callback, or never connected here.
		c.JSON(http.StatusAccepted, gin.H{"ignored": "no workspace has connected this installation"})
		return nil, false
	}
	return conns, true
}

// githubProjectName names a project in a delivery's answer: by its slug,
// with its workspace's when the installation is connected to several.
func githubProjectName(conns []store.GitHubInstallation, ws string, app *shpyrdv1.App) string {
	if len(conns) > 1 {
		return ws + "/" + project.SlugOf(app)
	}
	return project.SlugOf(app)
}

// githubWorkspaceApps are the Apps of an installation's workspace; the
// default workspace's include the Apps from before workspaces, which carry
// no label. Callers check workspaceOf again.
func (s *Server) githubWorkspaceApps(ctx context.Context, workspace string) ([]shpyrdv1.App, error) {
	var list shpyrdv1.AppList
	var opts []client.ListOption
	if workspace != store.DefaultWorkspace {
		opts = append(opts, client.MatchingLabels{shpyrdv1.LabelWorkspace: workspace})
	}
	if err := s.apps.List(ctx, &list, opts...); err != nil {
		return nil, err
	}
	return list.Items, nil
}

// githubCIEvent is a commit's checks or statuses changing (a check suite
// completed, a status set): every App, in the workspaces that connected
// the installation, on the repository that waits on that commit's CI is nudged, through the
// git-ci annotation, to read it again.
func (s *Server) githubCIEvent(c *gin.Context, event string, body []byte, delivery string) {
	ctx := c.Request.Context()
	var ev struct {
		Action     string `json:"action"`
		SHA        string `json:"sha"`
		CheckSuite struct {
			HeadSHA string `json:"head_sha"`
		} `json:"check_suite"`
		Installation struct {
			ID int64 `json:"id"`
		} `json:"installation"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(body, &ev); err != nil || ev.Installation.ID == 0 || ev.Repository.FullName == "" {
		c.JSON(http.StatusAccepted, gin.H{"ignored": "the event names no installation or no repository"})
		return
	}
	if event == "check_suite" && ev.Action != "completed" {
		c.JSON(http.StatusAccepted, gin.H{"ignored": "check_suite " + ev.Action})
		return
	}
	sha := strings.ToLower(firstNonEmpty(ev.SHA, ev.CheckSuite.HeadSHA))
	if sha == "" {
		c.JSON(http.StatusAccepted, gin.H{"ignored": "the event names no commit"})
		return
	}
	conns, ok := s.githubLiveConnections(c, ev.Installation.ID)
	if !ok {
		return
	}
	raw, _ := json.Marshal(gitauth.CINudge{SHA: sha, Delivery: delivery, At: time.Now().UTC()})
	patch, _ := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]string{shpyrdv1.AnnotationGitCI: string(raw)}}})
	nudged := []string{}
	for _, inst := range conns {
		list, err := s.githubWorkspaceApps(ctx, inst.Workspace)
		if err != nil {
			s.githubPushUnavailable(c, "could not list the apps", err)
			return
		}
		for i := range list {
			app := &list[i]
			if workspaceOf(app) != inst.Workspace || !app.DeletionTimestamp.IsZero() || !namesGitHubRepo(app, ev.Repository.FullName) || !gitauth.Watched(app) {
				continue
			}
			rec := gitauth.ReadSourceRecord(app.Annotations[shpyrdv1.AnnotationGitSource])
			if rec.Ref != app.GitRevision() || !rec.WaitsOnCI(sha) {
				continue
			}
			if err := s.apps.Patch(ctx, app.DeepCopy(), client.RawPatch(types.MergePatchType, patch)); err != nil {
				s.githubPushUnavailable(c, "could not nudge "+project.SlugOf(app), err)
				return
			}
			nudged = append(nudged, githubProjectName(conns, inst.Workspace, app))
		}
	}
	sort.Strings(nudged)
	c.JSON(http.StatusAccepted, gin.H{"nudged": sha, "projects": nudged})
}

// githubPushUnavailable answers a push shpyrd could not apply: the real
// error goes to the log, the public answer says nothing of the cluster.
// GitHub delivers again (the catch-up sweep asks within about 5 minutes).
func (s *Server) githubPushUnavailable(c *gin.Context, what string, err error) {
	s.log.Warn("github webhook: "+what, "err", err.Error())
	abort(c, http.StatusBadGateway, errors.New("shpyrd could not take the delivery now; GitHub delivers it again within about 5 minutes once shpyrd answers"))
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
	conns, ok := s.githubConnections(c, ev.Installation.ID)
	if !ok {
		return
	}
	if deleted {
		suspended = conns[0].SuspendedAt != nil
	}
	if err := s.store.SetGitHubInstallationState(ctx, ev.Installation.ID, suspended, deleted); err != nil {
		abort(c, http.StatusBadGateway, errors.New("shpyrd could not take the delivery now"))
		return
	}
	for _, inst := range conns {
		s.forgetGitHubRepos(inst.Workspace)
		s.githubChanged(inst.Workspace)
		s.auditActor(c, "github:"+firstNonEmpty(ev.Sender.Login, "unknown"), action, githubTarget(inst.ID, inst.AccountLogin), "workspace "+inst.Workspace)
	}
	c.JSON(http.StatusAccepted, gin.H{"recorded": ev.Action})
}

// githubRepositoriesEvent is a connected installation's repositories
// added or removed on GitHub: the picker's listing and the refusals the
// controllers keep are dropped, so a repository just added is used at
// once.
func (s *Server) githubRepositoriesEvent(c *gin.Context, body []byte) {
	var ev struct {
		Installation struct {
			ID int64 `json:"id"`
		} `json:"installation"`
	}
	if err := json.Unmarshal(body, &ev); err != nil || ev.Installation.ID == 0 {
		c.JSON(http.StatusAccepted, gin.H{"ignored": "the event names no installation"})
		return
	}
	conns, ok := s.githubConnections(c, ev.Installation.ID)
	if !ok {
		return
	}
	for _, inst := range conns {
		s.forgetGitHubRepos(inst.Workspace)
		s.githubChanged(inst.Workspace)
	}
	c.JSON(http.StatusAccepted, gin.H{"recorded": "repositories"})
}
