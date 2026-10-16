package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
)

func gitApp(name, url string) *shpyrdv1.App {
	app := sampleApp(name)
	app.Spec.Source = &shpyrdv1.Source{Git: &shpyrdv1.GitSource{URL: url}}
	return app
}

func workspaceToken(ws, host, token string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: gitauth.TokenSecretName(ws, host), Namespace: "shpyrd-system",
			Labels:      map[string]string{gitauth.LabelCredential: gitauth.KindToken, shpyrdv1.LabelWorkspace: ws},
			Annotations: map[string]string{gitauth.AnnotationHost: host}},
		Data: map[string][]byte{gitauth.KeyToken: []byte(token)},
	}
}

func TestReconcileGitCredentialsPicksWorkspaceToken(t *testing.T) {
	app := gitApp("shop", "https://github.com/acme/shop")
	other := workspaceToken("other", "github.com", "not-ours")
	r, c := newTestReconciler(t, app, workspaceToken("default", "github.com", "ghp_ours"), other)
	got, err := r.reconcileGitCredentials(context.Background(), app)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != gitauth.KindToken || got.CloneURL != "https://github.com/acme/shop.git" {
		t.Fatalf("build = %+v", got)
	}
	sec := &corev1.Secret{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: "shop-git"}, sec); err != nil {
		t.Fatal(err)
	}
	if string(sec.Data[corev1.BasicAuthPasswordKey]) != "ghp_ours" {
		t.Errorf("the default workspace's token, not another's: %q", sec.Data[corev1.BasicAuthPasswordKey])
	}
	if err := r.ensureBuildServiceAccount(context.Background(), app, got); err != nil {
		t.Fatal(err)
	}
	sa := &corev1.ServiceAccount{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: BuildServiceAccount}, sa); err != nil {
		t.Fatal(err)
	}
	if len(sa.Secrets) != 1 || sa.Secrets[0].Name != "shop-git" || len(sa.ImagePullSecrets) != 0 {
		t.Errorf("SA = %+v", sa)
	}
	img, _ := r.Config.desiredKpackImage(app, got)
	if u, _, _ := unstructured.NestedString(img.Object, "spec", "source", "git", "url"); u != "https://github.com/acme/shop.git" {
		t.Errorf("kpack url = %s", u)
	}
	if n, _, _ := unstructured.NestedString(img.Object, "spec", "serviceAccountName"); n != BuildServiceAccount {
		t.Errorf("kpack SA = %s", n)
	}
}

func TestReconcileGitCredentialsDeployKeyAndRegistry(t *testing.T) {
	app := gitApp("shop", "https://github.com/acme/shop")
	dk := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: gitauth.DeployKeySecretName, Namespace: "app-shop", Labels: map[string]string{gitauth.LabelCredential: gitauth.KindDeployKey}},
		Type:       corev1.SecretTypeSSHAuth,
		Data:       map[string][]byte{corev1.SSHAuthPrivateKey: []byte("K"), gitauth.KeyKnownHosts: []byte("github.com ssh-ed25519 A")},
	}
	reg := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "shpyrd-registry", Namespace: "shpyrd-system"}, Type: corev1.SecretTypeDockerConfigJson,
		Data: map[string][]byte{corev1.DockerConfigJsonKey: []byte(`{}`)}}
	r, c := newTestReconciler(t, app, dk, reg)
	r.Config.RegistrySecret = "shpyrd-registry"
	runReconcile(t, r, app)
	sa := &corev1.ServiceAccount{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: BuildServiceAccount}, sa); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, s := range sa.Secrets {
		names[s.Name] = true
	}
	if !names["shpyrd-registry"] || !names["shop-git"] || len(sa.ImagePullSecrets) != 1 || sa.ImagePullSecrets[0].Name != "shpyrd-registry" {
		t.Errorf("SA = %+v", sa)
	}
	img := &unstructured.Unstructured{}
	img.SetGroupVersionKind(KpackImageGVK)
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: "shop"}, img); err != nil {
		t.Fatal(err)
	}
	if u, _, _ := unstructured.NestedString(img.Object, "spec", "source", "git", "url"); u != "git@github.com:acme/shop.git" {
		t.Errorf("a deploy key clones over SSH: %s", u)
	}
}

func TestReconcileGitCredentialsCleansUp(t *testing.T) {
	app := gitApp("shop", "https://github.com/acme/shop")
	tok := workspaceToken("default", "github.com", "ghp")
	r, c := newTestReconciler(t, app, tok)
	got, _ := r.reconcileGitCredentials(context.Background(), app)
	_ = r.ensureBuildServiceAccount(context.Background(), app, got)
	if err := c.Delete(context.Background(), tok); err != nil {
		t.Fatal(err)
	}
	got, err := r.reconcileGitCredentials(context.Background(), app)
	if err != nil || got.Kind != "" {
		t.Fatalf("build = %+v, %v", got, err)
	}
	_ = r.ensureBuildServiceAccount(context.Background(), app, got)
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: "shop-git"}, &corev1.Secret{}); err == nil {
		t.Error("shop-git must be deleted with the credential")
	}
	sa := &corev1.ServiceAccount{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: BuildServiceAccount}, sa); err == nil && len(sa.Secrets) != 0 {
		t.Errorf("the SA keeps a stale reference: %+v", sa.Secrets)
	}
	if n := r.Config.buildServiceAccountName(got); n != "default" {
		t.Errorf("no credentials, no registry: SA %s", n)
	}
}

func TestGitCredentialSecretsReachTheirApps(t *testing.T) {
	app := gitApp("shop", "https://github.com/acme/shop")
	other := gitApp("blog", "https://github.com/acme/blog")
	other.Labels = map[string]string{shpyrdv1.LabelWorkspace: "other"}
	r, _ := newTestReconciler(t, app, other)
	reqs := r.secretToApps(context.Background(), workspaceToken("default", "github.com", "x"))
	if len(reqs) != 1 || reqs[0].Name != "shop" {
		t.Errorf("token -> %v", reqs)
	}
	dk := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: gitauth.DeployKeySecretName, Namespace: "app-shop", Labels: map[string]string{gitauth.LabelCredential: gitauth.KindDeployKey}}}
	if reqs := r.secretToApps(context.Background(), dk); len(reqs) != 1 || reqs[0].Name != "shop" {
		t.Errorf("deploy key -> %v", reqs)
	}
}
