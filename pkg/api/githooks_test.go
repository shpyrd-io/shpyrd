package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/configvars"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
)

const hookCommit = "89abcdef0123456789abcdef0123456789abcdef"

func githubPush(ref string) []byte {
	return []byte(`{"ref":"` + ref + `","before":"0123456789abcdef0123456789abcdef01234567","after":"` + hookCommit +
		`","head_commit":{"id":"` + hookCommit + `","message":"fix login\n\nThe redirect lost the session.","author":{"name":"Alice","username":"alice"}}}`)
}

func hmacHex(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func githubHeaders(secret, event, delivery string, body []byte) map[string]string {
	return map[string]string{"X-GitHub-Event": event, "X-GitHub-Delivery": delivery, "X-Hub-Signature-256": "sha256=" + hmacHex(secret, body)}
}

// deliver posts body to a project's webhook, as a git host does.
func deliver(t *testing.T, s *Server, slug, token string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/hooks/git/"+slug+"/"+token, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// hookOf sets up the project's webhook and returns its token and secret.
func hookOf(t *testing.T, s *Server, slug string) (string, string) {
	t.Helper()
	rec := do(t, s, "GET", "/api/projects/"+slug+"/git-hooks", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("git-hooks: %d %s", rec.Code, rec.Body.String())
	}
	var v GitHooksView
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(v.URL, "http://example.com/api/hooks/git/"+slug+"/") || len(path.Base(v.URL)) != 64 || len(v.Secret) != 64 {
		t.Fatalf("view = %+v", v)
	}
	return path.Base(v.URL), v.Secret
}

func appNamed(t *testing.T, cr client.Client, slug string) *shpyrdv1.App {
	t.Helper()
	app := &shpyrdv1.App{}
	if err := cr.Get(context.Background(), types.NamespacedName{Namespace: "app-" + slug, Name: slug}, app); err != nil {
		t.Fatal(err)
	}
	return app
}

func TestGitHooksKeepTheirURLUntilRotated(t *testing.T) {
	s, _ := newTestServer(t, nil, []client.Object{sampleApp("web1", shpyrdv1.PhaseRunning)})
	token, secret := hookOf(t, s, "web1")
	if again, _ := hookOf(t, s, "web1"); again != token {
		t.Error("the URL must not change by itself")
	}
	rec := do(t, s, "POST", "/api/projects/web1/git-hooks/rotate", "", true)
	var v GitHooksView
	_ = json.Unmarshal(rec.Body.Bytes(), &v)
	if rec.Code != http.StatusOK || path.Base(v.URL) == token || v.Secret == secret {
		t.Fatalf("rotate: %d %+v", rec.Code, v)
	}
	body := githubPush("refs/heads/main")
	if rec := deliver(t, s, "web1", token, body, githubHeaders(v.Secret, "push", "d-0", body)); rec.Code != http.StatusUnauthorized {
		t.Errorf("the old URL after a rotation: %d", rec.Code)
	}
	if rec := do(t, s, "GET", "/api/projects/web1/git-hooks", "", false); rec.Code != http.StatusUnauthorized {
		t.Errorf("the secret needs a session: %d", rec.Code)
	}
}

func TestGitHookAcceptsASignedGitHubPush(t *testing.T) {
	s, cr := newTestServer(t, nil, []client.Object{sampleApp("web1", shpyrdv1.PhaseRunning)})
	token, secret := hookOf(t, s, "web1")
	body := githubPush("refs/heads/main")
	if rec := deliver(t, s, "web1", token, body, githubHeaders(secret, "push", "d-1", body)); rec.Code != http.StatusAccepted {
		t.Fatalf("push: %d %s", rec.Code, rec.Body.String())
	}
	p, err := gitauth.ReadPush(appNamed(t, cr, "web1").Annotations[shpyrdv1.AnnotationGitPush])
	if err != nil || p == nil || p.SHA != hookCommit || p.Ref != "refs/heads/main" || p.Author != "alice" || p.Delivery != "d-1" || p.At.IsZero() {
		t.Fatalf("push = %+v, %v", p, err)
	}
	// The watcher took the push; the host delivers it again: ignored.
	app := appNamed(t, cr, "web1")
	delete(app.Annotations, shpyrdv1.AnnotationGitPush)
	app.Annotations[shpyrdv1.AnnotationGitSource] = `{"ref":"main","delivery":"d-1"}`
	if err := cr.Update(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	if rec := deliver(t, s, "web1", token, body, githubHeaders(secret, "push", "d-1", body)); rec.Code != http.StatusAccepted {
		t.Fatalf("again: %d", rec.Code)
	}
	if appNamed(t, cr, "web1").Annotations[shpyrdv1.AnnotationGitPush] != "" {
		t.Error("a repeated delivery is not applied twice")
	}
}

func TestGitHookRefusesAWrongTokenOrSignature(t *testing.T) {
	s, cr := newTestServer(t, nil, []client.Object{sampleApp("web1", shpyrdv1.PhaseRunning), sampleApp("web2", shpyrdv1.PhaseRunning)})
	token, secret := hookOf(t, s, "web1")
	body := githubPush("refs/heads/main")
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"wrong token":     deliver(t, s, "web1", strings.Repeat("0", 64), body, githubHeaders(secret, "push", "d", body)),
		"wrong signature": deliver(t, s, "web1", token, body, githubHeaders("not-the-secret", "push", "d", body)),
		"unsigned":        deliver(t, s, "web1", token, body, map[string]string{"X-GitHub-Event": "push"}),
		"no webhook yet":  deliver(t, s, "web2", token, body, githubHeaders(secret, "push", "d", body)),
	} {
		if rec.Code != http.StatusUnauthorized || strings.Contains(rec.Body.String(), secret) {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
	// A delivery to a project without a webhook does not make one.
	err := cr.Get(context.Background(), types.NamespacedName{Namespace: "app-web2", Name: gitauth.HookSecretName}, &corev1.Secret{})
	if !apierrors.IsNotFound(err) {
		t.Errorf("web2's hook Secret: %v", err)
	}
}

func TestGitHookDoesNotTellWhichProjectsExist(t *testing.T) {
	s, _ := newTestServer(t, nil, []client.Object{sampleApp("web1", shpyrdv1.PhaseRunning)})
	_, secret := hookOf(t, s, "web1")
	body := githubPush("refs/heads/main")
	wrong := deliver(t, s, "web1", strings.Repeat("0", 64), body, githubHeaders(secret, "push", "w", body))
	unknown := deliver(t, s, "nope", strings.Repeat("0", 64), body, githubHeaders(secret, "push", "u", body))
	if wrong.Code != http.StatusUnauthorized || unknown.Code != wrong.Code || unknown.Body.String() != wrong.Body.String() {
		t.Errorf("an unknown project %d %s, a wrong token %d %s", unknown.Code, unknown.Body.String(), wrong.Code, wrong.Body.String())
	}
}

func TestGitHookUnreadableBodyIsABadRequest(t *testing.T) {
	s, _ := newTestServer(t, nil, []client.Object{sampleApp("web1", shpyrdv1.PhaseRunning)})
	token, _ := hookOf(t, s, "web1")
	req := httptest.NewRequest("POST", "/api/hooks/git/web1/"+token, iotest.ErrReader(errors.New("connection reset")))
	req.Header.Set("X-GitHub-Event", "push")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("a body that cannot be read: %d %s", rec.Code, rec.Body.String())
	}
}

func TestGitHookDedupesAPendingDelivery(t *testing.T) {
	s, cr := newTestServer(t, nil, []client.Object{sampleApp("web1", shpyrdv1.PhaseRunning)})
	token, secret := hookOf(t, s, "web1")
	body := githubPush("refs/heads/main")
	if rec := deliver(t, s, "web1", token, body, githubHeaders(secret, "push", "d-1", body)); rec.Code != http.StatusAccepted {
		t.Fatalf("push: %d %s", rec.Code, rec.Body.String())
	}
	first := appNamed(t, cr, "web1").Annotations[shpyrdv1.AnnotationGitPush]
	time.Sleep(2 * time.Millisecond)
	rec := deliver(t, s, "web1", token, body, githubHeaders(secret, "push", "d-1", body))
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), "delivered before") {
		t.Errorf("again: %d %s", rec.Code, rec.Body.String())
	}
	if again := appNamed(t, cr, "web1").Annotations[shpyrdv1.AnnotationGitPush]; again != first {
		t.Errorf("the pending push was written again: %s -> %s", first, again)
	}
}

func TestGitHookReadsGitLabAndGitea(t *testing.T) {
	s, cr := newTestServer(t, nil, []client.Object{sampleApp("web1", shpyrdv1.PhaseRunning)})
	token, secret := hookOf(t, s, "web1")
	gitlab := []byte(`{"object_kind":"push","ref":"refs/heads/main","after":"` + hookCommit + `","checkout_sha":"` + hookCommit +
		`","user_username":"bruno","commits":[{"id":"` + hookCommit + `","message":"bump deps","author":{"name":"Bruno"}}]}`)
	if rec := deliver(t, s, "web1", token, gitlab, map[string]string{"X-Gitlab-Event": "Push Hook", "X-Gitlab-Token": secret, "X-Gitlab-Event-UUID": "g-1"}); rec.Code != http.StatusAccepted {
		t.Fatalf("gitlab: %d %s", rec.Code, rec.Body.String())
	}
	if p, _ := gitauth.ReadPush(appNamed(t, cr, "web1").Annotations[shpyrdv1.AnnotationGitPush]); p == nil || p.Author != "Bruno" || p.Message != "bump deps" || p.Delivery != "g-1" {
		t.Errorf("gitlab push = %+v", p)
	}
	gitea := githubPush("refs/heads/main")
	if rec := deliver(t, s, "web1", token, gitea, map[string]string{"X-Gitea-Event": "push", "X-Gitea-Delivery": "t-1", "X-Gitea-Signature": hmacHex(secret, gitea)}); rec.Code != http.StatusAccepted {
		t.Fatalf("gitea: %d %s", rec.Code, rec.Body.String())
	}
	if p, _ := gitauth.ReadPush(appNamed(t, cr, "web1").Annotations[shpyrdv1.AnnotationGitPush]); p == nil || p.Delivery != "t-1" {
		t.Errorf("gitea push = %+v", p)
	}
}

func TestGitHookIgnoresWhatIsNotAPushToTheBranch(t *testing.T) {
	s, cr := newTestServer(t, nil, []client.Object{sampleApp("web1", shpyrdv1.PhaseRunning)})
	token, secret := hookOf(t, s, "web1")
	ping := []byte(`{"zen":"Keep it logically awesome."}`)
	if rec := deliver(t, s, "web1", token, ping, githubHeaders(secret, "ping", "p", ping)); rec.Code != http.StatusOK || rec.Body.String() != "pong" {
		t.Errorf("ping: %d %q", rec.Code, rec.Body.String())
	}
	issue := []byte(`{"action":"opened"}`)
	other := githubPush("refs/heads/dev")
	deleted := []byte(`{"ref":"refs/heads/main","after":"0000000000000000000000000000000000000000"}`)
	tag := githubPush("refs/tags/v1.2")
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"an issue":         deliver(t, s, "web1", token, issue, githubHeaders(secret, "issues", "i", issue)),
		"another branch":   deliver(t, s, "web1", token, other, githubHeaders(secret, "push", "o", other)),
		"a deleted branch": deliver(t, s, "web1", token, deleted, githubHeaders(secret, "push", "x", deleted)),
		"a tag":            deliver(t, s, "web1", token, tag, githubHeaders(secret, "push", "v", tag)),
	} {
		if rec.Code != http.StatusAccepted {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
	if appNamed(t, cr, "web1").Annotations[shpyrdv1.AnnotationGitPush] != "" {
		t.Error("none of these is a push to deploy")
	}
	garbage := []byte(`{"ref":`)
	if rec := deliver(t, s, "web1", token, garbage, githubHeaders(secret, "push", "g", garbage)); rec.Code != http.StatusBadRequest {
		t.Errorf("an unreadable push: %d", rec.Code)
	}
}

func TestGitHookIsRateLimitedAndCapped(t *testing.T) {
	s, _ := newTestServer(t, nil, []client.Object{sampleApp("web1", shpyrdv1.PhaseRunning)})
	token, secret := hookOf(t, s, "web1")
	big := bytes.Repeat([]byte("x"), hookBodyLimit+1)
	if rec := deliver(t, s, "web1", token, big, githubHeaders(secret, "push", "b", big)); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("1 MiB and more: %d", rec.Code)
	}
	s.gitHookDeliveries = newRateLimiter(1)
	body := githubPush("refs/heads/main")
	deliver(t, s, "web1", token, body, githubHeaders(secret, "push", "r-1", body))
	if rec := deliver(t, s, "web1", token, body, githubHeaders(secret, "push", "r-2", body)); rec.Code != http.StatusTooManyRequests {
		t.Errorf("past the limit: %d", rec.Code)
	}
}

func TestAGitDeployKeepsAutoDeployAndReadsTheBranchAgain(t *testing.T) {
	app := sampleApp("web1", shpyrdv1.PhaseRunning)
	off := false
	app.Spec.Source.Git.AutoDeploy = &off
	app.Annotations = map[string]string{shpyrdv1.AnnotationGitCommit: hookCommit, shpyrdv1.AnnotationGitCommitRef: "main"}
	s, cr := newTestServer(t, nil, []client.Object{app})
	if rec := do(t, s, "POST", "/api/projects/web1/deploy", `{"git":{"url":"https://example.test/web1","revision":"main"}}`, true); rec.Code != http.StatusAccepted {
		t.Fatalf("deploy: %d %s", rec.Code, rec.Body.String())
	}
	got := appNamed(t, cr, "web1")
	if got.Annotations[shpyrdv1.AnnotationGitCommit] != "" || got.Annotations[shpyrdv1.AnnotationGitCommitRef] != "" {
		t.Errorf("a deploy reads the branch again: %v", got.Annotations)
	}
	// The deploy-requested time is what makes the watcher read the branch
	// at once, now that a missing pin alone does not.
	if got.Annotations[configvars.AnnotationDeployRequested] == "" {
		t.Errorf("a git deploy asks the watcher to read the branch now: %v", got.Annotations)
	}
	if got.Spec.Source.Git.AutoDeploy == nil || *got.Spec.Source.Git.AutoDeploy {
		t.Error("a deploy that does not say keeps auto-deploy off")
	}
	if rec := do(t, s, "POST", "/api/projects/web1/deploy", `{"git":{"url":"https://example.test/web1","autoDeploy":true}}`, true); rec.Code != http.StatusAccepted {
		t.Fatalf("deploy: %d", rec.Code)
	}
	if got := appNamed(t, cr, "web1"); got.Spec.Source.Git.AutoDeploy == nil || !*got.Spec.Source.Git.AutoDeploy {
		t.Error("--auto-deploy switches it back on")
	}
}

func TestAutoDeployIsAProjectSetting(t *testing.T) {
	app := sampleApp("web1", shpyrdv1.PhaseRunning)
	app.Status.Source = &shpyrdv1.SourceStatus{Commit: hookCommit, Latest: hookCommit, Author: "alice", Via: "push"}
	site := sampleApp("site", shpyrdv1.PhaseRunning)
	site.Spec.Source = &shpyrdv1.Source{Blob: &shpyrdv1.BlobSource{URL: "http://shpyrd-server/api/sources/ab.tgz", SHA256: "ab"}}
	s, cr := newTestServer(t, nil, []client.Object{app, site})
	rec := do(t, s, "PATCH", "/api/projects/web1", `{"autoDeploy":false}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}
	var d AppDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if d.Spec.Source == nil || d.Spec.Source.Git.AutoDeploy == nil || *d.Spec.Source.Git.AutoDeploy {
		t.Errorf("spec.source = %+v", d.Spec.Source)
	}
	if d.Status.Source == nil || d.Status.Source.Author != "alice" || d.Status.Source.Commit != hookCommit {
		t.Errorf("status.source = %+v", d.Status.Source)
	}
	if got := appNamed(t, cr, "web1"); *got.Spec.Source.Git.AutoDeploy {
		t.Error("not stored")
	}
	if rec := do(t, s, "PATCH", "/api/projects/site", `{"autoDeploy":true}`, true); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "git repository") {
		t.Errorf("an archive has no branch: %d %s", rec.Code, rec.Body.String())
	}
}

// A git:// URL is built as given and not watched: a push to it is
// ignored, as for a commit revision.
func TestGitHookIgnoresAPushToAnUnwatchedURL(t *testing.T) {
	app := sampleApp("web1", shpyrdv1.PhaseRunning)
	app.Spec.Source = &shpyrdv1.Source{Git: &shpyrdv1.GitSource{URL: "git://git.example.test/acme/web1", Revision: "main"}}
	s, cr := newTestServer(t, nil, []client.Object{app})
	token, secret := hookOf(t, s, "web1")
	body := githubPush("refs/heads/main")
	rec := deliver(t, s, "web1", token, body, githubHeaders(secret, "push", "d-1", body))
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), "ignored") {
		t.Errorf("push: %d %s", rec.Code, rec.Body.String())
	}
	if appNamed(t, cr, "web1").Annotations[shpyrdv1.AnnotationGitPush] != "" {
		t.Error("a push to an unwatched URL is not written")
	}
}

// Before the delivery is authenticated, a failure says nothing of the
// cluster.
func TestGitHookLookupFailureSaysNothingOfTheCluster(t *testing.T) {
	s, cr := newTestServer(t, nil, []client.Object{sampleApp("web1", shpyrdv1.PhaseRunning)})
	token, secret := hookOf(t, s, "web1")
	s.apps = interceptor.NewClient(cr.(client.WithWatch), interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if _, ok := obj.(*shpyrdv1.App); ok {
				return errors.New("etcdserver: leader changed (app-web1)")
			}
			return c.Get(ctx, key, obj, opts...)
		},
	})
	body := githubPush("refs/heads/main")
	rec := deliver(t, s, "web1", token, body, githubHeaders(secret, "push", "d-1", body))
	if rec.Code != http.StatusBadGateway || strings.Contains(rec.Body.String(), "etcd") || strings.Contains(rec.Body.String(), "app-web1") {
		t.Errorf("lookup failure: %d %s", rec.Code, rec.Body.String())
	}
}

// Through an internal door (the kubeconfig proxy, an in-cluster caller)
// the request's host is no address a git host reaches: the URL is the
// workspace's own.
func TestGitHooksURLAtAnInternalDoorIsTheWorkspaces(t *testing.T) {
	s, _ := newTestServerPublic(t, PublicConfig{Domain: "example.test", DashboardURL: "https://shpyrd.example.test"}, nil, []client.Object{sampleApp("web1", shpyrdv1.PhaseRunning)})
	if v := internalHooks(t, s, "web1"); !strings.HasPrefix(v.URL, "https://shpyrd.example.test/api/hooks/git/web1/") {
		t.Errorf("git-hooks: %+v", v)
	}
}

// internalHooks asks for a project's webhook through an internal door.
func internalHooks(t *testing.T, s *Server, slug string) GitHooksView {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/projects/"+slug+"/git-hooks", nil)
	req.Host = "10.96.0.12:8080"
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var v GitHooksView
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &v) != nil {
		t.Fatalf("git-hooks: %d %s", rec.Code, rec.Body.String())
	}
	return v
}

// In the single-tenant layout the URL an internal door prints is on the
// console host, which resolves no workspace: a delivery there reaches the
// default workspace's project, which its token authenticates.
func TestGitHookAtTheConsoleAddressReachesTheDefaultWorkspace(t *testing.T) {
	s, cr := newTestServerPublic(t, PublicConfig{Domain: "example.test", DashboardURL: "https://shpyrd.example.test"}, nil, []client.Object{sampleApp("web1", shpyrdv1.PhaseRunning)})
	v := internalHooks(t, s, "web1")
	u, err := url.Parse(v.URL)
	if err != nil || u.Host != "shpyrd.example.test" {
		t.Fatalf("url = %q", v.URL)
	}
	body := githubPush("refs/heads/main")
	req := httptest.NewRequest("POST", u.Path, bytes.NewReader(body))
	req.Host = u.Host
	req.Header.Set("Content-Type", "application/json")
	for k, h := range githubHeaders(v.Secret, "push", "d-1", body) {
		req.Header.Set(k, h)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), hookCommit) {
		t.Fatalf("delivery at the console address: %d %s", rec.Code, rec.Body.String())
	}
	if appNamed(t, cr, "web1").Annotations[shpyrdv1.AnnotationGitPush] == "" {
		t.Error("the push is not written")
	}
	// A wrong token there is still refused.
	req = httptest.NewRequest("POST", path.Dir(u.Path)+"/"+strings.Repeat("0", 64), bytes.NewReader(body))
	req.Host = u.Host
	for k, h := range githubHeaders(v.Secret, "push", "d-2", body) {
		req.Header.Set(k, h)
	}
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong token at the console address: %d %s", rec.Code, rec.Body.String())
	}
}
