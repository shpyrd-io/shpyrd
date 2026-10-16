package api

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
	"github.com/shpyrd-io/shpyrd/pkg/project"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// Deploy on push (RFC-0018): a project's webhook, which GitHub, GitLab or
// Gitea call on a push, and its URL and secret. The webhook only leaves
// the push on the App; the source watcher decides what it deploys.

// GitHooksView is a project's webhook: the URL to paste into the git host,
// and the secret it signs deliveries with.
type GitHooksView struct {
	URL    string `json:"url"`
	Secret string `json:"secret"`
}

const (
	// hookBodyLimit caps a delivery: a push of many commits stays far below.
	hookBodyLimit = 1 << 20
	// gitHooksPerMinute is how many deliveries one address may make: a busy
	// organisation's pushes all come from a few of its host's addresses.
	gitHooksPerMinute = 300
	// hookRefusalsPerMinute is how many refused deliveries are logged per
	// project.
	hookRefusalsPerMinute = 6
)

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// projectHookSecret is the project's webhook Secret, made on first use;
// rotate replaces its token and secret.
func (s *Server) projectHookSecret(ctx context.Context, app *shpyrdv1.App, rotate bool) (*corev1.Secret, error) {
	key := types.NamespacedName{Namespace: app.Namespace, Name: gitauth.HookSecretName}
	sec := &corev1.Secret{}
	err := s.apps.Get(ctx, key, sec)
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, err
	}
	exists := err == nil
	if exists && !rotate && len(sec.Data[gitauth.HookKeyToken]) > 0 && len(sec.Data[gitauth.HookKeySecret]) > 0 {
		return sec, nil
	}
	token, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	secret, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	if !exists {
		sec.ObjectMeta = metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace}
	}
	sec.Labels = mergeStringMaps(sec.Labels, map[string]string{shpyrdv1.LabelManagedBy: "shpyrd", shpyrdv1.LabelProject: app.Name})
	sec.Type = corev1.SecretTypeOpaque
	sec.Data = map[string][]byte{gitauth.HookKeyToken: []byte(token), gitauth.HookKeySecret: []byte(secret)}
	if exists {
		if err := s.apps.Update(ctx, sec); err != nil {
			return nil, err
		}
		return sec, nil
	}
	if err := s.apps.Create(ctx, sec); apierrors.IsAlreadyExists(err) {
		// Made by a request at the same moment: that one is the webhook.
		if err := s.apps.Get(ctx, key, sec); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	return sec, nil
}

// hookURL is the webhook's address at the door the request came through,
// HTTPS when the platform is.
func (s *Server) hookURL(c *gin.Context, slug, token string) string {
	scheme := requestScheme(c)
	if scheme == "http" && strings.HasPrefix(s.opts.Public.DashboardURL, "https://") {
		scheme = "https"
	}
	return scheme + "://" + c.Request.Host + "/api/hooks/git/" + url.PathEscape(slug) + "/" + token
}

func (s *Server) showGitHooks(c *gin.Context)   { s.gitHooks(c, false) }
func (s *Server) rotateGitHooks(c *gin.Context) { s.gitHooks(c, true) }

func (s *Server) gitHooks(c *gin.Context, rotate bool) {
	app, ok := s.loadApp(c)
	if !ok {
		return
	}
	sec, err := s.projectHookSecret(c.Request.Context(), app, rotate)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	slug := project.SlugOf(app)
	if rotate {
		s.audit(c, slug, "git-hooks.rotate", "webhook", "a new webhook URL and secret; the old ones are refused")
	}
	c.JSON(http.StatusOK, GitHooksView{
		URL:    s.hookURL(c, slug, string(sec.Data[gitauth.HookKeyToken])),
		Secret: string(sec.Data[gitauth.HookKeySecret]),
	})
}

// gitHook is POST /api/hooks/git/:slug/:token. The tenant comes from the
// Host. Every answer that is not a refusal is 2xx, so hosts do not retry
// what will never apply.
func (s *Server) gitHook(c *gin.Context) {
	if !s.gitHookDeliveries.allow(c.ClientIP()) {
		c.Header("Retry-After", "60")
		abort(c, http.StatusTooManyRequests, errors.New("too many webhook deliveries from this address; shpyrd checks the branch by itself meanwhile"))
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, hookBodyLimit))
	if err != nil {
		abort(c, http.StatusRequestEntityTooLarge, errors.New("the delivery is larger than shpyrd reads (1 MiB)"))
		return
	}
	ctx := c.Request.Context()
	app, err := s.findApp(ctx, s.workspace(c), c.Param("slug"))
	if errors.Is(err, store.ErrNotFound) || apierrors.IsNotFound(err) {
		c.JSON(http.StatusAccepted, gin.H{"ignored": "no such project"})
		return
	}
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	sec := &corev1.Secret{}
	err = s.apps.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: gitauth.HookSecretName}, sec)
	if err != nil && !apierrors.IsNotFound(err) {
		abort(c, http.StatusBadGateway, err)
		return
	}
	if err != nil || subtle.ConstantTimeCompare([]byte(c.Param("token")), sec.Data[gitauth.HookKeyToken]) != 1 ||
		gitauth.VerifyDelivery(c.Request.Header, body, string(sec.Data[gitauth.HookKeySecret])) != nil {
		s.refuseHook(c, project.SlugOf(app))
		return
	}
	hook := gitauth.HookOf(c.Request.Header)
	switch hook.Event {
	case "ping":
		c.String(http.StatusOK, "pong")
		return
	case "push":
	default:
		c.JSON(http.StatusAccepted, gin.H{"ignored": "not a push"})
		return
	}
	push, err := gitauth.ParsePush(body)
	if err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	push.Delivery, push.At = hook.Delivery, time.Now().UTC()
	if why := ignoredPush(app, push); why != "" {
		c.JSON(http.StatusAccepted, gin.H{"ignored": why})
		return
	}
	raw, _ := json.Marshal(push)
	patch, _ := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]string{shpyrdv1.AnnotationGitPush: string(raw)}}})
	if err := s.apps.Patch(ctx, app.DeepCopy(), client.RawPatch(types.MergePatchType, patch)); err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"accepted": push.SHA})
}

// ignoredPush says why a push is not for the project's branch; "" when it
// is. Each write is an App event for two controllers: none for nothing.
func ignoredPush(app *shpyrdv1.App, p gitauth.Push) string {
	switch {
	case p.Deleted:
		return "the branch was deleted; what runs keeps running"
	case !app.WatchesGit():
		return "the project does not follow a git branch"
	case p.Ref != "refs/heads/"+app.GitRevision():
		return "a push to another branch"
	case p.Delivery != "" && seenDelivery(app, p.Delivery):
		return "delivered before"
	}
	return ""
}

// seenDelivery says the host delivers again what was applied: the push
// waiting for the watcher, or the last one it took.
func seenDelivery(app *shpyrdv1.App, id string) bool {
	if last, _ := gitauth.ReadPush(app.Annotations[shpyrdv1.AnnotationGitPush]); last != nil && last.Delivery == id {
		return true
	}
	return gitauth.ReadSourceRecord(app.Annotations[shpyrdv1.AnnotationGitSource]).Delivery == id
}

// refuseHook answers a delivery whose URL or signature is not the
// project's, and says so in the log a few times a minute at most.
func (s *Server) refuseHook(c *gin.Context, slug string) {
	if s.hookRefusals.allow(slug) {
		s.log.Warn("git webhook refused: wrong URL or signature", "project", slug, "from", c.ClientIP())
	}
	abort(c, http.StatusUnauthorized, errors.New("this webhook's URL or signature is not the project's: copy them again from shpyrd git-hooks show"))
}
