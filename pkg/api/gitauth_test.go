package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
)

func TestWorkspaceGitTokens(t *testing.T) {
	s, k := newTestServer(t, nil, nil)
	rec := do(t, s, "PUT", "/api/workspace/git-auth/gitlab.example.com", `{"token":"glpat-s3cret"}`, true)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "s3cret") {
		t.Fatalf("put: %d %s", rec.Code, rec.Body.String())
	}
	sec := &corev1.Secret{}
	key := types.NamespacedName{Namespace: "shpyrd-system", Name: gitauth.TokenSecretName("default", "gitlab.example.com")}
	if err := k.Get(context.Background(), key, sec); err != nil {
		t.Fatal(err)
	}
	if string(sec.Data[gitauth.KeyToken]) != "glpat-s3cret" || sec.Labels[gitauth.LabelCredential] != gitauth.KindToken ||
		sec.Labels[shpyrdv1.LabelWorkspace] != "default" || sec.Annotations[gitauth.AnnotationHost] != "gitlab.example.com" {
		t.Errorf("secret = %+v", sec)
	}
	rec = do(t, s, "GET", "/api/workspace/git-auth", "", true)
	var list []GitTokenView
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if rec.Code != http.StatusOK || len(list) != 1 || list[0].Host != "gitlab.example.com" || list[0].Username != "x-access-token" || strings.Contains(rec.Body.String(), "s3cret") {
		t.Errorf("list: %d %s", rec.Code, rec.Body.String())
	}
	for _, bad := range []string{"not_a_host", "gitea.local:3000", "-x.com"} {
		if rec := do(t, s, "PUT", "/api/workspace/git-auth/"+bad, `{"token":"x"}`, true); rec.Code != http.StatusBadRequest {
			t.Errorf("bad host %s: %d", bad, rec.Code)
		}
	}
	if rec := do(t, s, "PUT", "/api/workspace/git-auth/github.com", `{}`, true); rec.Code != http.StatusBadRequest {
		t.Errorf("no token: %d", rec.Code)
	}
	if rec := do(t, s, "DELETE", "/api/workspace/git-auth/gitlab.example.com", "", true); rec.Code != http.StatusNoContent {
		t.Errorf("delete: %d", rec.Code)
	}
	if err := k.Get(context.Background(), key, sec); err == nil {
		t.Error("the token Secret must be gone")
	}
}

func TestProjectDeployKey(t *testing.T) {
	app := sampleApp("shop", shpyrdv1.PhaseRunning)
	app.Spec.Source = &shpyrdv1.Source{Git: &shpyrdv1.GitSource{URL: "https://github.com/acme/shop"}}
	s, k := newTestServer(t, nil, []client.Object{app})
	s.scanHostKey = func(_ context.Context, host, port string) (string, string, error) {
		if host != "github.com" || port != "" {
			t.Errorf("scanned %s:%s", host, port)
		}
		return "github.com ssh-ed25519 AAAAhost", "SHA256:host", nil
	}
	rec := do(t, s, "POST", "/api/projects/shop/git-auth/deploy-key", `{}`, true)
	if rec.Code != http.StatusCreated || strings.Contains(rec.Body.String(), "PRIVATE KEY") {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var v DeployKeyView
	_ = json.Unmarshal(rec.Body.Bytes(), &v)
	if v.Host != "github.com" || !strings.HasPrefix(v.PublicKey, "ssh-ed25519 ") || v.HostKeyFingerprint != "SHA256:host" {
		t.Errorf("view = %+v", v)
	}
	sec := &corev1.Secret{}
	if err := k.Get(context.Background(), types.NamespacedName{Namespace: app.Namespace, Name: gitauth.DeployKeySecretName}, sec); err != nil {
		t.Fatal(err)
	}
	if sec.Type != corev1.SecretTypeSSHAuth || !strings.Contains(string(sec.Data[corev1.SSHAuthPrivateKey]), "PRIVATE KEY") || string(sec.Data[gitauth.KeyKnownHosts]) != "github.com ssh-ed25519 AAAAhost" {
		t.Errorf("secret = %+v", sec)
	}
	rec = do(t, s, "GET", "/api/projects/shop/git-auth", "", true)
	var pv ProjectGitAuthView
	_ = json.Unmarshal(rec.Body.Bytes(), &pv)
	if rec.Code != http.StatusOK || pv.Using != gitauth.KindDeployKey || pv.DeployKey == nil || pv.DeployKey.PublicKey != v.PublicKey {
		t.Errorf("show: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, s, "DELETE", "/api/projects/shop/git-auth/deploy-key", "", true); rec.Code != http.StatusNoContent {
		t.Errorf("delete: %d", rec.Code)
	}
	rec = do(t, s, "GET", "/api/projects/shop/git-auth", "", true)
	pv = ProjectGitAuthView{}
	_ = json.Unmarshal(rec.Body.Bytes(), &pv)
	if pv.Using != "none" || pv.DeployKey != nil {
		t.Errorf("after delete: %s", rec.Body.String())
	}
}

func TestProjectGitAuthUsesTheWorkspaceToken(t *testing.T) {
	app := sampleApp("shop", shpyrdv1.PhaseRunning)
	app.Spec.Source = &shpyrdv1.Source{Git: &shpyrdv1.GitSource{URL: "git@gitlab.example.com:team/shop.git"}}
	s, _ := newTestServer(t, nil, []client.Object{app})
	if rec := do(t, s, "PUT", "/api/workspace/git-auth/gitlab.example.com", `{"token":"t","username":"me"}`, true); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	rec := do(t, s, "GET", "/api/projects/shop/git-auth", "", true)
	var pv ProjectGitAuthView
	_ = json.Unmarshal(rec.Body.Bytes(), &pv)
	if pv.Using != gitauth.KindToken || pv.Token == nil || pv.Token.Username != "me" {
		t.Errorf("show: %s", rec.Body.String())
	}
}

func TestDeployKeyNeedsAHost(t *testing.T) {
	app := sampleApp("shop", shpyrdv1.PhaseRunning)
	app.Spec.Source = nil // no git source
	s, _ := newTestServer(t, nil, []client.Object{app})
	rec := do(t, s, "POST", "/api/projects/shop/git-auth/deploy-key", `{}`, true)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "--host") {
		t.Errorf("no source, no host: %d %s", rec.Code, rec.Body.String())
	}
}

func TestDeployKeyRefusesUnsafeHost(t *testing.T) {
	s, _ := newTestServer(t, nil, []client.Object{sampleApp("shop", shpyrdv1.PhaseRunning)})
	called := false
	s.scanHostKey = func(context.Context, string, string) (string, string, error) {
		called = true
		return "", "", nil
	}
	for _, bad := range []string{"10.0.0.1:6379", "a b"} {
		rec := do(t, s, "POST", "/api/projects/shop/git-auth/deploy-key", `{"host":"`+bad+`"}`, true)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("host %q: %d %s", bad, rec.Code, rec.Body.String())
		}
	}
	if called {
		t.Error("an unsafe host must not be dialed")
	}
}

func TestProjectGitAuthIgnoresADeployKeyForAnotherHost(t *testing.T) {
	app := sampleApp("shop", shpyrdv1.PhaseRunning)
	app.Spec.Source = &shpyrdv1.Source{Git: &shpyrdv1.GitSource{URL: "https://github.com/acme/shop"}}
	s, _ := newTestServer(t, nil, []client.Object{app})
	s.scanHostKey = func(context.Context, string, string) (string, string, error) {
		return "gitlab.com ssh-ed25519 AAAAhost", "SHA256:host", nil
	}
	if rec := do(t, s, "POST", "/api/projects/shop/git-auth/deploy-key", `{"host":"gitlab.com"}`, true); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body.String())
	}
	rec := do(t, s, "GET", "/api/projects/shop/git-auth", "", true)
	var pv ProjectGitAuthView
	_ = json.Unmarshal(rec.Body.Bytes(), &pv)
	if pv.Using != "none" || pv.DeployKey == nil || pv.DeployKey.Host != "gitlab.com" {
		t.Errorf("a gitlab.com key does not clone github.com, but is shown: %s", rec.Body.String())
	}
	if rec := do(t, s, "PUT", "/api/workspace/git-auth/github.com", `{"token":"t"}`, true); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	rec = do(t, s, "GET", "/api/projects/shop/git-auth", "", true)
	pv = ProjectGitAuthView{}
	_ = json.Unmarshal(rec.Body.Bytes(), &pv)
	if pv.Using != gitauth.KindToken || pv.DeployKey == nil || pv.Token == nil {
		t.Errorf("the token is used, the key still shown: %s", rec.Body.String())
	}
}

func TestDeployKeyScanFailuresLookAlike(t *testing.T) {
	s, _ := newTestServer(t, nil, []client.Object{sampleApp("shop", shpyrdv1.PhaseRunning)})
	var bodies []string
	for _, cause := range []error{errors.New("dial tcp 10.0.0.1:22: connect: connection refused"), errors.New("ssh: handshake failed: EOF")} {
		s.scanHostKey = func(context.Context, string, string) (string, string, error) { return "", "", cause }
		rec := do(t, s, "POST", "/api/projects/shop/git-auth/deploy-key", `{"host":"git.internal"}`, true)
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
		bodies = append(bodies, rec.Body.String())
	}
	if bodies[0] != bodies[1] || strings.Contains(bodies[0], "refused") || !strings.Contains(bodies[0], "could not record git.internal's SSH host key: check the host name, and that it accepts SSH on port 22") {
		t.Errorf("the failures must read the same: %q vs %q", bodies[0], bodies[1])
	}
}

func TestDeployKeyReplacesThePreviousOneInPlace(t *testing.T) {
	app := sampleApp("shop", shpyrdv1.PhaseRunning)
	app.Spec.Source = &shpyrdv1.Source{Git: &shpyrdv1.GitSource{URL: "https://github.com/acme/shop"}}
	s, k := newTestServer(t, nil, []client.Object{app})
	s.scanHostKey = func(context.Context, string, string) (string, string, error) {
		return "github.com ssh-ed25519 AAAAhost", "SHA256:host", nil
	}
	deletes := 0
	s.apps = interceptor.NewClient(s.apps.(client.WithWatch), interceptor.Funcs{
		Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			deletes++
			return c.Delete(ctx, obj, opts...)
		},
	})
	key := types.NamespacedName{Namespace: app.Namespace, Name: gitauth.DeployKeySecretName}
	var views [2]DeployKeyView
	for i := range views {
		rec := do(t, s, "POST", "/api/projects/shop/git-auth/deploy-key", `{}`, true)
		if rec.Code != http.StatusCreated {
			t.Fatalf("make %d: %d %s", i, rec.Code, rec.Body.String())
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &views[i])
		sec := &corev1.Secret{}
		if err := k.Get(context.Background(), key, sec); err != nil {
			t.Fatal(err)
		}
		if sec.Annotations[gitauth.AnnotationFingerprint] != views[i].Fingerprint || sec.Type != corev1.SecretTypeSSHAuth {
			t.Errorf("secret %d = %+v", i, sec.ObjectMeta)
		}
	}
	if views[0].Fingerprint == views[1].Fingerprint {
		t.Error("the second key must replace the first")
	}
	if deletes != 0 {
		t.Errorf("replaced in place, not deleted and created: %d deletes", deletes)
	}
}

func TestDeployKeyIsRateLimited(t *testing.T) {
	s, _ := newTestServer(t, nil, []client.Object{sampleApp("shop", shpyrdv1.PhaseRunning)})
	s.scanHostKey = func(context.Context, string, string) (string, string, error) {
		return "github.com ssh-ed25519 AAAAhost", "SHA256:host", nil
	}
	for i := 0; i < deployKeysPerMinute; i++ {
		if rec := do(t, s, "POST", "/api/projects/shop/git-auth/deploy-key", `{"host":"github.com"}`, true); rec.Code != http.StatusCreated {
			t.Fatalf("make %d: %d %s", i, rec.Code, rec.Body.String())
		}
	}
	if rec := do(t, s, "POST", "/api/projects/shop/git-auth/deploy-key", `{"host":"github.com"}`, true); rec.Code != http.StatusTooManyRequests {
		t.Errorf("over the limit: %d %s", rec.Code, rec.Body.String())
	}
}

func TestProjectGitAuthSaysTheGitHubApp(t *testing.T) {
	app := sampleApp("shop", shpyrdv1.PhaseRunning)
	app.Spec.Source = &shpyrdv1.Source{Git: &shpyrdv1.GitSource{URL: "https://github.com/acme/shop"}}
	build := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "shop-git", Namespace: "app-shop",
		Annotations: map[string]string{gitauth.AnnotationGitHubAccount: "Acme", gitauth.AnnotationTokenMintedAt: "2026-10-16T12:00:00Z"}}}
	s, _ := newTestServer(t, nil, []client.Object{app, build})
	rec := do(t, s, "GET", "/api/projects/shop/git-auth", "", true)
	var pv ProjectGitAuthView
	_ = json.Unmarshal(rec.Body.Bytes(), &pv)
	if rec.Code != http.StatusOK || pv.Using != gitauth.KindGitHubApp || pv.GitHubAccount != "Acme" {
		t.Errorf("%d %s", rec.Code, rec.Body.String())
	}
}
