package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
	"github.com/shpyrd-io/shpyrd/pkg/project"
)

// Git credentials (RFC-0017): a workspace's tokens, one per host, live in
// the system namespace labelled with the workspace; a project's deploy key
// lives in its namespace. Values are written, never returned.

type GitTokenView struct {
	Host      string    `json:"host"`
	Username  string    `json:"username"`
	UpdatedBy string    `json:"updatedBy,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type GitTokenRequest struct {
	Token    string `json:"token" binding:"required"`
	Username string `json:"username,omitempty"`
}

type DeployKeyRequest struct {
	// Host defaults to the project's git host.
	Host string `json:"host,omitempty"`
}

type DeployKeyView struct {
	Host               string    `json:"host"`
	PublicKey          string    `json:"publicKey"`
	Fingerprint        string    `json:"fingerprint"`
	HostKeyFingerprint string    `json:"hostKeyFingerprint"`
	CreatedAt          time.Time `json:"createdAt"`
}

type ProjectGitAuthView struct {
	URL       string         `json:"url,omitempty"`
	Using     string         `json:"using"`
	DeployKey *DeployKeyView `json:"deployKey,omitempty"`
	Token     *GitTokenView  `json:"token,omitempty"`
	// GitHubAccount is the account whose GitHub App installation builds
	// clone through, when Using is github-app (RFC-0054).
	GitHubAccount string `json:"githubAccount,omitempty"`
}

// A token is for a host name, without a port: the controller looks it up
// by the repository URL's host name, whatever port the URL names.
var gitHostRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?$`)

var errGitHost = errors.New("give a host name such as github.com or gitlab.example.com, without a port")

func gitHostParam(c *gin.Context) (string, bool) {
	host := c.Param("host")
	if !gitHostRe.MatchString(host) {
		abort(c, http.StatusBadRequest, errGitHost)
		return "", false
	}
	return host, true
}

func actorOf(c *gin.Context) string {
	if id, ok := ext.IdentityFrom(c); ok {
		return firstNonEmpty(id.Email, id.Name, id.Subject)
	}
	return ""
}

func gitTokenView(sec *corev1.Secret) GitTokenView {
	host := sec.Annotations[gitauth.AnnotationHost]
	return GitTokenView{
		Host:      host,
		Username:  firstNonEmpty(string(sec.Data[gitauth.KeyUsername]), gitauth.DefaultUsername(host)),
		UpdatedBy: sec.Annotations[gitauth.AnnotationUpdatedBy],
		UpdatedAt: sec.CreationTimestamp.Time,
	}
}

func (s *Server) listGitTokens(c *gin.Context) {
	var list corev1.SecretList
	if err := s.apps.List(c.Request.Context(), &list, client.InNamespace(s.kube.Namespace),
		client.MatchingLabels{gitauth.LabelCredential: gitauth.KindToken, shpyrdv1.LabelWorkspace: s.doorWorkspace(c)}); err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	out := make([]GitTokenView, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, gitTokenView(&list.Items[i]))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Host < out[j].Host })
	c.JSON(http.StatusOK, out)
}

func (s *Server) putGitToken(c *gin.Context) {
	host, ok := gitHostParam(c)
	if !ok {
		return
	}
	var req GitTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, errors.New("give the token"))
		return
	}
	ws := s.doorWorkspace(c)
	ctx := c.Request.Context()
	sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: gitauth.TokenSecretName(ws, host), Namespace: s.kube.Namespace}}
	data := map[string][]byte{gitauth.KeyToken: []byte(req.Token)}
	if req.Username != "" {
		data[gitauth.KeyUsername] = []byte(req.Username)
	}
	err := s.apps.Get(ctx, client.ObjectKeyFromObject(sec), sec)
	switch {
	case apierrors.IsNotFound(err):
		sec.Labels = map[string]string{shpyrdv1.LabelManagedBy: "shpyrd", gitauth.LabelCredential: gitauth.KindToken, shpyrdv1.LabelWorkspace: ws}
		sec.Annotations = map[string]string{gitauth.AnnotationHost: host, gitauth.AnnotationUpdatedBy: actorOf(c)}
		sec.Type = corev1.SecretTypeOpaque
		sec.Data = data
		err = s.apps.Create(ctx, sec)
	case err == nil:
		sec.Annotations = mergeStringMaps(sec.Annotations, map[string]string{gitauth.AnnotationUpdatedBy: actorOf(c)})
		sec.Data = data
		err = s.apps.Update(ctx, sec)
	}
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	s.audit(c, "", "git-auth.set", host, "token for "+host)
	c.JSON(http.StatusOK, gitTokenView(sec))
}

func (s *Server) deleteGitToken(c *gin.Context) {
	host, ok := gitHostParam(c)
	if !ok {
		return
	}
	sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: gitauth.TokenSecretName(s.doorWorkspace(c), host), Namespace: s.kube.Namespace}}
	if err := s.apps.Delete(c.Request.Context(), sec); err != nil && !apierrors.IsNotFound(err) {
		abort(c, http.StatusBadGateway, err)
		return
	}
	s.audit(c, "", "git-auth.unset", host, "token for "+host)
	c.Status(http.StatusNoContent)
}

func deployKeyView(sec *corev1.Secret) *DeployKeyView {
	return &DeployKeyView{
		Host:               sec.Annotations[gitauth.AnnotationHost],
		PublicKey:          sec.Annotations[gitauth.AnnotationPublicKey],
		Fingerprint:        sec.Annotations[gitauth.AnnotationFingerprint],
		HostKeyFingerprint: sec.Annotations[gitauth.AnnotationHostKeyFingerprint],
		CreatedAt:          sec.CreationTimestamp.Time,
	}
}

// projectGitSecrets reads the project's deploy key and the workspace's
// token for its git host; either may be nil.
func (s *Server) projectGitSecrets(ctx context.Context, app *shpyrdv1.App, host string) (deployKey, token *corev1.Secret, err error) {
	dk := &corev1.Secret{}
	if err := s.apps.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: gitauth.DeployKeySecretName}, dk); err == nil {
		deployKey = dk
	} else if !apierrors.IsNotFound(err) {
		return nil, nil, err
	}
	if host != "" {
		tk := &corev1.Secret{}
		if err := s.apps.Get(ctx, types.NamespacedName{Namespace: s.kube.Namespace, Name: gitauth.TokenSecretName(workspaceOf(app), host)}, tk); err == nil {
			token = tk
		} else if !apierrors.IsNotFound(err) {
			return nil, nil, err
		}
	}
	return deployKey, token, nil
}

func (s *Server) showProjectGitAuth(c *gin.Context) {
	app, ok := s.loadApp(c)
	if !ok {
		return
	}
	view := ProjectGitAuthView{Using: "none"}
	host := ""
	if app.Spec.Source != nil && app.Spec.Source.Git != nil {
		view.URL = app.Spec.Source.Git.URL
		if r, err := gitauth.Parse(view.URL); err == nil {
			host = r.Host
		}
	}
	dk, tk, err := s.projectGitSecrets(c.Request.Context(), app, host)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	if dk != nil {
		view.DeployKey = deployKeyView(dk)
	}
	if tk != nil {
		tv := gitTokenView(tk)
		view.Token = &tv
	}
	// What builds clone with: the controller marks <app>-git when it
	// holds the GitHub App's token.
	viaApp := ""
	build := &corev1.Secret{}
	if err := s.apps.Get(c.Request.Context(), types.NamespacedName{Namespace: app.Namespace, Name: gitauth.BuildSecretName(app.Name)}, build); err == nil {
		viaApp = build.Annotations[gitauth.AnnotationGitHubAccount]
	}
	// Using is what builds really clone with: a deploy key made for
	// another host is shown, but not used.
	switch {
	case viaApp != "":
		view.Using, view.GitHubAccount = gitauth.KindGitHubApp, viaApp
	case host != "" && gitauth.DeployKeyFits(dk, host):
		view.Using = gitauth.KindDeployKey
	case tk != nil:
		view.Using = gitauth.KindToken
	}
	c.JSON(http.StatusOK, view)
}

func (s *Server) createDeployKey(c *gin.Context) {
	app, ok := s.loadApp(c)
	if !ok {
		return
	}
	var req DeployKeyRequest
	_ = c.ShouldBindJSON(&req)
	if req.Host != "" && !gitHostRe.MatchString(req.Host) {
		abort(c, http.StatusBadRequest, errGitHost)
		return
	}
	host, port := req.Host, ""
	if host == "" && app.Spec.Source != nil && app.Spec.Source.Git != nil {
		if r, err := gitauth.Parse(app.Spec.Source.Git.URL); err == nil {
			host = r.Host
			if r.Scheme == "ssh" {
				port = r.Port
			}
		}
	}
	if host == "" {
		abort(c, http.StatusBadRequest, errors.New("the project has no git repository yet: name its host with --host (github.com, gitlab.example.com)"))
		return
	}
	ctx := c.Request.Context()
	line, hostFP, err := s.scanHostKey(ctx, host, port)
	if err != nil {
		// One message whatever went wrong: the route dials a host the
		// caller names, and must not tell open ports from closed ones.
		s.log.Warn("deploy key: cannot scan the host key", "host", host, "port", port, "error", err)
		abort(c, http.StatusBadGateway, fmt.Errorf("could not record %s's SSH host key: check the host name, and that it accepts SSH on port %s", host, firstNonEmpty(port, "22")))
		return
	}
	slug := project.SlugOf(app)
	key, err := gitauth.GenerateDeployKey("shpyrd " + slug)
	if err != nil {
		abort(c, http.StatusInternalServerError, err)
		return
	}
	// A new key replaces the old one in place: the old public half stops
	// working once removed from the repository.
	sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: gitauth.DeployKeySecretName, Namespace: app.Namespace}}
	err = s.apps.Get(ctx, client.ObjectKeyFromObject(sec), sec)
	if err != nil && !apierrors.IsNotFound(err) {
		abort(c, http.StatusBadGateway, err)
		return
	}
	exists := err == nil
	sec.Labels = mergeStringMaps(sec.Labels, map[string]string{shpyrdv1.LabelManagedBy: "shpyrd", gitauth.LabelCredential: gitauth.KindDeployKey, shpyrdv1.LabelProject: app.Name})
	sec.Annotations = mergeStringMaps(sec.Annotations, map[string]string{
		gitauth.AnnotationHost: host, gitauth.AnnotationPublicKey: key.Public, gitauth.AnnotationFingerprint: key.Fingerprint,
		gitauth.AnnotationHostKeyFingerprint: hostFP, gitauth.AnnotationUpdatedBy: actorOf(c),
	})
	sec.Type = corev1.SecretTypeSSHAuth
	sec.Data = map[string][]byte{corev1.SSHAuthPrivateKey: key.PrivatePEM, gitauth.KeyKnownHosts: []byte(line)}
	if exists {
		err = s.apps.Update(ctx, sec)
	} else {
		err = s.apps.Create(ctx, sec)
	}
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	s.audit(c, slug, "git-auth.set", "deploy key", "deploy key "+key.Fingerprint+" for "+host)
	c.JSON(http.StatusCreated, deployKeyView(sec))
}

func (s *Server) deleteDeployKey(c *gin.Context) {
	app, ok := s.loadApp(c)
	if !ok {
		return
	}
	sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: gitauth.DeployKeySecretName, Namespace: app.Namespace}}
	if err := s.apps.Delete(c.Request.Context(), sec); err != nil && !apierrors.IsNotFound(err) {
		abort(c, http.StatusBadGateway, err)
		return
	}
	s.audit(c, project.SlugOf(app), "git-auth.unset", "deploy key", "deploy key")
	c.Status(http.StatusNoContent)
}

func mergeStringMaps(a, b map[string]string) map[string]string {
	out := make(map[string]string, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

// deployKeysPerMinute is how often one caller may make a deploy key. Each
// one dials the host the caller names to record its SSH host key; a person
// makes a key a few times a project, never dozens a minute.
const deployKeysPerMinute = 10

// throttleDeployKeys refuses a caller making deploy keys faster than that,
// keyed by actor as the shell tickets are.
func (s *Server) throttleDeployKeys() gin.HandlerFunc {
	return func(c *gin.Context) {
		id, _ := ext.IdentityFrom(c)
		if !s.deployKeyMints.allow(actorKey(id)) {
			c.Header("Retry-After", "60")
			abort(c, http.StatusTooManyRequests, errors.New("too many deploy keys made; wait a minute before making another"))
			return
		}
		c.Next()
	}
}
