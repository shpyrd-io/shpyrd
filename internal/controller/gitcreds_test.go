package controller

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/go-logr/logr/funcr"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/log"

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
	if got.Kind != gitauth.KindToken || got.CloneURL != "https://github.com/acme/shop" {
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
	if u, _, _ := unstructured.NestedString(img.Object, "spec", "source", "git", "url"); u != "https://github.com/acme/shop" {
		t.Errorf("a token keeps the app's https URL: kpack url = %s", u)
	}
	if n, _, _ := unstructured.NestedString(img.Object, "spec", "serviceAccountName"); n != BuildServiceAccount {
		t.Errorf("kpack SA = %s", n)
	}
}

func TestReconcileGitCredentialsDeployKeyAndRegistry(t *testing.T) {
	app := gitApp("shop", "https://github.com/acme/shop")
	dk := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: gitauth.DeployKeySecretName, Namespace: "app-shop", Labels: map[string]string{gitauth.LabelCredential: gitauth.KindDeployKey},
			Annotations: map[string]string{gitauth.AnnotationHost: "github.com"}},
		Type: corev1.SecretTypeSSHAuth,
		Data: map[string][]byte{corev1.SSHAuthPrivateKey: []byte("K"), gitauth.KeyKnownHosts: []byte("github.com ssh-ed25519 A")},
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

func TestReconcileGitCredentialsUnparseableURL(t *testing.T) {
	app := gitApp("shop", "git://example.com/a/b")
	r, c := newTestReconciler(t, app, workspaceToken("default", "example.com", "ghp"))
	got, err := r.reconcileGitCredentials(context.Background(), app)
	if err != nil || got.Kind != "" || got.Secret != nil {
		t.Fatalf("build = %+v, %v", got, err)
	}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: "shop-git"}, &corev1.Secret{}); err == nil {
		t.Error("no shop-git for a URL shpyrd cannot parse")
	}
	runReconcile(t, r, app)
	img := &unstructured.Unstructured{}
	img.SetGroupVersionKind(KpackImageGVK)
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: "shop"}, img); err != nil {
		t.Fatal(err)
	}
	if u, _, _ := unstructured.NestedString(img.Object, "spec", "source", "git", "url"); u != "git://example.com/a/b" {
		t.Errorf("the raw URL builds as before: %s", u)
	}
}

func dockerfileGitApp(url string) *shpyrdv1.App {
	app := dockerfileApp()
	app.Spec.Source = &shpyrdv1.Source{Git: &shpyrdv1.GitSource{URL: url, Revision: "main"}}
	return app
}

func TestDockerfileFetchWithToken(t *testing.T) {
	app := dockerfileGitApp("https://github.com/acme/dk")
	r, c := newTestReconciler(t, app, workspaceToken("default", "github.com", "ghp_s3cret"))
	runReconcile(t, r, app)
	job := getJob(t, c, "app-dk", "dk-build-1")
	init := job.Spec.Template.Spec.InitContainers[0]
	script := init.Command[2]
	if strings.Contains(script, "ghp_s3cret") {
		t.Fatal("the token must never be inlined in the script")
	}
	if !strings.Contains(script, "url='https://github.com/acme/dk'") || !strings.Contains(script, ".netrc") {
		t.Errorf("script: %s", script)
	}
	if !hasGitAuthMount(init) || !hasSecretVolume(job, "dk-git") || !gitAuthVolumeOptional(job) {
		t.Errorf("fetch container lacks the credential: %+v", init.VolumeMounts)
	}
	if hasGitAuthMount(job.Spec.Template.Spec.Containers[0]) {
		t.Error("the build container must not see the credential")
	}
}

func TestDockerfileFetchWithDeployKey(t *testing.T) {
	app := dockerfileGitApp("https://github.com/acme/dk")
	dk := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: gitauth.DeployKeySecretName, Namespace: "app-dk", Labels: map[string]string{gitauth.LabelCredential: gitauth.KindDeployKey},
			Annotations: map[string]string{gitauth.AnnotationHost: "github.com"}},
		Type: corev1.SecretTypeSSHAuth,
		Data: map[string][]byte{corev1.SSHAuthPrivateKey: []byte("K"), gitauth.KeyKnownHosts: []byte("github.com ssh-ed25519 A")},
	}
	r, c := newTestReconciler(t, app, dk)
	runReconcile(t, r, app)
	script := getJob(t, c, "app-dk", "dk-build-1").Spec.Template.Spec.InitContainers[0].Command[2]
	for _, want := range []string{"url='git@github.com:acme/dk.git'", "chmod 600", "StrictHostKeyChecking=yes", "UserKnownHostsFile=" + gitAuthDir + "/known_hosts", "GIT_SSH_COMMAND"} {
		if !strings.Contains(script, want) {
			t.Errorf("script lacks %q:\n%s", want, script)
		}
	}
}

func TestFetchScriptNeverInlinesSecrets(t *testing.T) {
	app := dockerfileGitApp("https://github.com/acme/dk")
	b, _ := gitauth.Resolve(app.Spec.Source.Git.URL, "dk", "app-dk", nil, workspaceToken("default", "github.com", "a'b$(c)\nd"))
	script := Config{}.fetchScript(app, b)
	if strings.Contains(script, "a'b") || strings.Contains(script, "$(c)") {
		t.Errorf("secret material in the script:\n%s", script)
	}
}

func TestPublicGitFetchUnchanged(t *testing.T) {
	app := dockerfileGitApp("https://github.com/acme/dk")
	script := Config{}.fetchScript(app, gitauth.Build{})
	if strings.Contains(script, ".netrc") || strings.Contains(script, "GIT_SSH_COMMAND") || !strings.Contains(script, "url='https://github.com/acme/dk'") {
		t.Errorf("a public repository's fetch changed:\n%s", script)
	}
}

func hasGitAuthMount(c corev1.Container) bool {
	for _, m := range c.VolumeMounts {
		if m.MountPath == gitAuthDir {
			return true
		}
	}
	return false
}

func hasSecretVolume(job *batchv1.Job, name string) bool {
	for _, v := range job.Spec.Template.Spec.Volumes {
		if v.Secret != nil && v.Secret.SecretName == name {
			return true
		}
	}
	return false
}

func gitAuthVolumeOptional(job *batchv1.Job) bool {
	for _, v := range job.Spec.Template.Spec.Volumes {
		if v.Name == "git-auth" && v.Secret != nil {
			return v.Secret.Optional != nil && *v.Secret.Optional
		}
	}
	return false
}

func TestReconcileSendsNoDeleteForAnAbsentGitSecret(t *testing.T) {
	app := sampleApp("shop")
	app.Spec.Source = &shpyrdv1.Source{Blob: &shpyrdv1.BlobSource{URL: "http://shpyrd-server.shpyrd-system.svc:8082/api/sources/" + strings.Repeat("a", 64) + ".tgz", SHA256: strings.Repeat("a", 64)}}
	r, _ := newTestReconciler(t, app)
	gitDeletes := 0
	r.Client = interceptor.NewClient(r.Client.(client.WithWatch), interceptor.Funcs{
		Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			if _, ok := obj.(*corev1.Secret); ok && obj.GetName() == gitauth.BuildSecretName(app.Name) {
				gitDeletes++
			}
			return c.Delete(ctx, obj, opts...)
		},
	})
	runReconcile(t, r, app)
	runReconcile(t, r, app)
	if gitDeletes != 0 {
		t.Errorf("a DELETE of shop-git per reconcile: %d", gitDeletes)
	}
}

func TestGitSecretIsOwnedByTheApp(t *testing.T) {
	app := gitApp("shop", "https://github.com/acme/shop")
	app.UID = "uid-shop"
	r, c := newTestReconciler(t, app, workspaceToken("default", "github.com", "ghp"))
	if _, err := r.reconcileGitCredentials(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	sec := &corev1.Secret{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: "shop-git"}, sec); err != nil {
		t.Fatal(err)
	}
	ref := metav1.GetControllerOf(sec)
	if ref == nil || ref.Kind != "App" || ref.Name != "shop" || ref.UID != "uid-shop" {
		t.Errorf("owner = %+v", sec.OwnerReferences)
	}
}

func TestUnparseableURLIsNotLogged(t *testing.T) {
	app := gitApp("shop", "git://user:s3cret@example.com/a/b")
	r, _ := newTestReconciler(t, app)
	var buf bytes.Buffer
	ctx := log.IntoContext(context.Background(), funcr.New(func(prefix, args string) { buf.WriteString(prefix + args + "\n") }, funcr.Options{}))
	if _, err := r.reconcileGitCredentials(ctx, app); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "s3cret") || strings.Contains(buf.String(), "example.com") || !strings.Contains(buf.String(), "its git URL is not one shpyrd can parse; building without credentials") {
		t.Errorf("log: %s", buf.String())
	}
}
