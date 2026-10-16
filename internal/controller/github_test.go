package controller

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
	"github.com/shpyrd-io/shpyrd/pkg/github/githubtest"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// connectedGitHub is the fake GitHub, on sourceNow's clock, with its
// installation 42 on Acme connected to the default workspace.
func connectedGitHub(t *testing.T) (*githubtest.Server, store.Store) {
	t.Helper()
	fake := githubtest.NewServer(t)
	fake.SetNow(sourceNow)
	st := store.NewMemory()
	if err := st.AddGitHubInstallation(context.Background(), store.DefaultWorkspace,
		store.GitHubInstallation{ID: 42, AccountLogin: "Acme", AccountType: "Organization", ConnectedBy: "ana@acme.test"}); err != nil {
		t.Fatal(err)
	}
	return fake, st
}

// githubReconciler is an AppReconciler on a platform with the fake's App.
func githubReconciler(t *testing.T, objs ...client.Object) (*AppReconciler, client.Client, *githubtest.Server, store.Store) {
	t.Helper()
	fake, st := connectedGitHub(t)
	r, c := newTestReconciler(t, append([]client.Object{fake.Secret("shpyrd-system")}, objs...)...)
	r.Config.GitHub = &GitHubApp{Source: fake.Source(c, "shpyrd-system"), Installations: st}
	r.Now = fake.Time
	return r, c, fake, st
}

func buildSecretOf(t *testing.T, c client.Client, app *shpyrdv1.App) *corev1.Secret {
	t.Helper()
	sec := &corev1.Secret{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: app.Namespace, Name: gitauth.BuildSecretName(app.Name)}, sec); err != nil {
		t.Fatal(err)
	}
	return sec
}

func TestGitHubAppClonesWithATokenForThatRepositoryAlone(t *testing.T) {
	// The URL says acme; GitHub's account is Acme.
	app := gitApp("shop", "https://github.com/acme/shop")
	r, c, fake, _ := githubReconciler(t, app, workspaceToken("default", "github.com", "ghp_ours"))
	got, err := r.reconcileGitCredentials(context.Background(), app)
	if err != nil || got.Kind != gitauth.KindGitHubApp || got.CloneURL != "https://github.com/acme/shop" || got.AppRefused != "" {
		t.Fatalf("build = %+v, %v", got, err)
	}
	sec := buildSecretOf(t, c, app)
	if string(sec.Data[corev1.BasicAuthUsernameKey]) != "x-access-token" || string(sec.Data[corev1.BasicAuthPasswordKey]) != "ghs_42_1" {
		t.Errorf("the App's token comes before the workspace's: %q / %q", sec.Data[corev1.BasicAuthUsernameKey], sec.Data[corev1.BasicAuthPasswordKey])
	}
	if sec.Annotations[gitauth.AnnotationGitHubAccount] != "Acme" || sec.Annotations[gitauth.AnnotationTokenMintedAt] != sourceNow.Format(time.RFC3339) {
		t.Errorf("annotations %v", sec.Annotations)
	}
	if body := fake.LastTokenRequest(); fmt.Sprint(body["repositories"]) != "[shop]" || fmt.Sprint(body["permissions"]) != "map[contents:read]" {
		t.Errorf("asked GitHub for %v", body)
	}
}

func TestGitHubAppFallsThroughWhenTheRepositoryIsNotGranted(t *testing.T) {
	app := gitApp("vault", "https://github.com/acme/vault")
	r, _, _, _ := githubReconciler(t, app, workspaceToken("default", "github.com", "ghp_ours"))
	b, err := lookupGitBuild(context.Background(), r.Client, "shpyrd-system", r.Config.GitHub, app)
	if err != nil || b.Kind != gitauth.KindToken || b.AppRefused != "The GitHub App's installation on Acme does not include vault: add the repository to it on GitHub." {
		t.Errorf("build = %+v, %v", b, err)
	}
}

func TestGitHubAppFallsThroughWhenTheInstallationIsGone(t *testing.T) {
	// GitHub does not know installation 99: the App was uninstalled.
	app := gitApp("shop", "https://github.com/gone/shop")
	r, _, _, st := githubReconciler(t, app, workspaceToken("default", "github.com", "ghp_ours"))
	ctx := context.Background()
	if err := st.AddGitHubInstallation(ctx, store.DefaultWorkspace, store.GitHubInstallation{ID: 99, AccountLogin: "Gone", AccountType: "Organization", ConnectedBy: "ana@acme.test"}); err != nil {
		t.Fatal(err)
	}
	b, err := lookupGitBuild(ctx, r.Client, "shpyrd-system", r.Config.GitHub, app)
	if err != nil || b.Kind != gitauth.KindToken || b.AppRefused != "The GitHub App is no longer installed on Gone: an owner of Gone installs it again, then a workspace admin connects it from Workspace settings → Git." {
		t.Errorf("build = %+v, %v", b, err)
	}
}

func TestGitHubAppIsSkippedWhileSuspended(t *testing.T) {
	app := gitApp("shop", "https://github.com/acme/shop")
	r, _, fake, st := githubReconciler(t, app)
	ctx := context.Background()
	if err := st.SetGitHubInstallationState(ctx, 42, true, false); err != nil {
		t.Fatal(err)
	}
	b, err := lookupGitBuild(ctx, r.Client, "shpyrd-system", r.Config.GitHub, app)
	if err != nil || b.Kind != "" || b.AppRefused != "The GitHub App's installation on Acme is suspended: an owner of Acme unsuspends it on GitHub." {
		t.Errorf("build = %+v, %v", b, err)
	}
	if fake.MintedFor(42) != 0 {
		t.Error("no token is asked for a suspended installation")
	}
}

func TestGitHubAppIsSkippedForAnInstallationMovedAway(t *testing.T) {
	app := gitApp("shop", "https://github.com/acme/shop")
	r, _, fake, st := githubReconciler(t, app)
	ctx := context.Background()
	if _, err := st.CreateWorkspace(ctx, store.Workspace{Slug: "beta", Name: "Beta"}); err != nil {
		t.Fatal(err)
	}
	// The store never takes an installation from another workspace: it
	// moves by being disconnected, then connected to beta.
	if err := st.DeleteGitHubInstallation(ctx, store.DefaultWorkspace, 42); err != nil {
		t.Fatal(err)
	}
	if err := st.AddGitHubInstallation(ctx, "beta", store.GitHubInstallation{ID: 42, AccountLogin: "Acme", AccountType: "Organization", ConnectedBy: "bob"}); err != nil {
		t.Fatal(err)
	}
	if b, err := lookupGitBuild(ctx, r.Client, "shpyrd-system", r.Config.GitHub, app); err != nil || b.Kind != "" || b.AppRefused != "" || fake.MintedFor(42) != 0 {
		t.Errorf("the default workspace's app: %+v, %v", b, err)
	}
	theirs := gitApp("shop2", "https://github.com/acme/shop")
	theirs.Labels = map[string]string{shpyrdv1.LabelWorkspace: "beta"}
	if b, _ := lookupGitBuild(ctx, r.Client, "shpyrd-system", r.Config.GitHub, theirs); b.Kind != gitauth.KindGitHubApp {
		t.Errorf("beta's app: %+v", b)
	}
}

func TestGitHubAppIsNotTriedWhereItCannotApply(t *testing.T) {
	r, c, fake, _ := githubReconciler(t)
	ctx := context.Background()
	for _, url := range []string{"https://github.com/someone/shop", "https://gitlab.com/acme/shop", "https://github.com/acme/shop/extra"} {
		if b, err := lookupGitBuild(ctx, r.Client, "shpyrd-system", r.Config.GitHub, gitApp("x", url)); err != nil || b.Kind != "" || b.AppRefused != "" {
			t.Errorf("%s: %+v %v", url, b, err)
		}
	}
	if err := c.Delete(ctx, fake.Secret("shpyrd-system")); err != nil {
		t.Fatal(err)
	}
	fake.SetNow(sourceNow.Add(time.Minute)) // past the Source's 30 s
	if b, _ := lookupGitBuild(ctx, r.Client, "shpyrd-system", r.Config.GitHub, gitApp("shop", "https://github.com/acme/shop")); b.Kind != "" {
		t.Errorf("without the App's Secret: %+v", b)
	}
	if b, _ := lookupGitBuild(ctx, r.Client, "shpyrd-system", nil, gitApp("shop", "https://github.com/acme/shop")); b.Kind != "" {
		t.Errorf("without the App: %+v", b)
	}
	if calls := fake.Calls(); len(calls) != 0 {
		t.Errorf("GitHub was called: %v", calls)
	}
}

func TestGitHubTokenIsRewrittenFiftyMinutesAfterItWasMinted(t *testing.T) {
	app := gitApp("shop", "https://github.com/acme/shop")
	r, c, fake, _ := githubReconciler(t, app)
	ctx := context.Background()
	reconcileAt := func(at time.Time) *corev1.Secret {
		t.Helper()
		fake.SetNow(at)
		if _, err := r.reconcileGitCredentials(ctx, app); err != nil {
			t.Fatal(err)
		}
		return buildSecretOf(t, c, app)
	}
	if sec := reconcileAt(sourceNow); string(sec.Data[corev1.BasicAuthPasswordKey]) != "ghs_42_1" {
		t.Fatalf("first: %q", sec.Data[corev1.BasicAuthPasswordKey])
	}
	if d := r.githubTokenRefresh(ctx, app); d != githubTokenLife+time.Second {
		t.Errorf("requeue in %v", d)
	}
	if sec := reconcileAt(sourceNow.Add(49 * time.Minute)); string(sec.Data[corev1.BasicAuthPasswordKey]) != "ghs_42_1" {
		t.Errorf("49 minutes: %q", sec.Data[corev1.BasicAuthPasswordKey])
	}
	if d := r.githubTokenRefresh(ctx, app); d != time.Minute+time.Second {
		t.Errorf("requeue in %v", d)
	}
	sec := reconcileAt(sourceNow.Add(githubTokenLife))
	if string(sec.Data[corev1.BasicAuthPasswordKey]) != "ghs_42_2" || sec.Annotations[gitauth.AnnotationTokenMintedAt] != sourceNow.Add(githubTokenLife).Format(time.RFC3339) {
		t.Errorf("50 minutes: %q %v", sec.Data[corev1.BasicAuthPasswordKey], sec.Annotations)
	}
}

func TestAnAppCloningThroughGitHubIsRequeuedBeforeItsTokenAges(t *testing.T) {
	// A settled app: its release's image built, its instances ready.
	app := pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit)
	app.Spec.Image = "example.test/shop:1"
	r, c, _, _ := githubReconciler(t, app)
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(app)}
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	markDeploymentReady(t, c, app.Namespace, "shop-web", 1)
	res, err := r.Reconcile(context.Background(), req)
	if err != nil || res.RequeueAfter != githubTokenLife+time.Second {
		t.Errorf("requeue after %v, %v", res.RequeueAfter, err)
	}
}

// oldAppToken is an <app>-git holding the App's token, minted at.
func oldAppToken(app *shpyrdv1.App, minted time.Time) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: gitauth.BuildSecretName(app.Name), Namespace: app.Namespace,
			Annotations: map[string]string{gitauth.AnnotationGitHubAccount: "Acme", gitauth.AnnotationTokenMintedAt: minted.UTC().Format(time.RFC3339)}},
		Type: corev1.SecretTypeBasicAuth,
		Data: map[string][]byte{corev1.BasicAuthUsernameKey: []byte("x-access-token"), corev1.BasicAuthPasswordKey: []byte("ghs_old")},
	}
}

func TestAnAppInMaintenanceDoesNotSpinOnAnAgedToken(t *testing.T) {
	app := pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit)
	app.Annotations[shpyrdv1.AnnotationMaintenance] = "paused"
	r, _, _, _ := githubReconciler(t, app, oldAppToken(app, sourceNow.Add(-55*time.Minute)))
	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(app)})
	// Maintenance polls on its own; the aged token adds nothing.
	if err != nil || res.RequeueAfter == time.Second {
		t.Errorf("requeue after %v, %v", res.RequeueAfter, err)
	}
}

func TestAnAppTokenIsKeptWhileGitHubIsDown(t *testing.T) {
	app := pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit)
	r, c, fake, _ := githubReconciler(t, app)
	ctx := context.Background()
	if _, err := r.reconcileGitCredentials(ctx, app); err != nil {
		t.Fatal(err)
	}
	fake.SetDown(true)
	fake.SetNow(sourceNow.Add(52 * time.Minute))
	res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(app)})
	if err != nil || res.RequeueAfter <= 0 || res.RequeueAfter > 2*time.Minute {
		t.Errorf("requeue after %v, %v", res.RequeueAfter, err)
	}
	sec := buildSecretOf(t, c, app)
	if string(sec.Data[corev1.BasicAuthPasswordKey]) != "ghs_42_1" || sec.Annotations[gitauth.AnnotationTokenMintedAt] != sourceNow.Format(time.RFC3339) {
		t.Errorf("the token still valid is kept: %q %v", sec.Data[corev1.BasicAuthPasswordKey], sec.Annotations)
	}
	// An hour on, it has expired: it goes.
	fake.SetNow(sourceNow.Add(61 * time.Minute))
	if _, err := r.reconcileGitCredentials(ctx, app); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: gitauth.BuildSecretName(app.Name)}, &corev1.Secret{}); !apierrors.IsNotFound(err) {
		t.Errorf("an expired token is not kept: %v", err)
	}
}

// tokenCalls counts the token requests GitHub saw.
func tokenCalls(fake *githubtest.Server) int {
	n := 0
	for _, c := range fake.Calls() {
		if strings.HasSuffix(c, "/access_tokens") {
			n++
		}
	}
	return n
}

func TestGitHubIsNotAskedAgainSoonAfterItFailed(t *testing.T) {
	app := gitApp("shop", "https://github.com/acme/shop")
	r, _, fake, _ := githubReconciler(t, app)
	ctx := context.Background()
	fake.SetDown(true)
	lookup := func() gitauth.Build {
		t.Helper()
		b, err := lookupGitBuild(ctx, r.Client, "shpyrd-system", r.Config.GitHub, app)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	if b := lookup(); b.AppRefused != "GitHub did not give shpyrd a token for this repository just now; shpyrd tries again by itself." {
		t.Errorf("refused %q", b.AppRefused)
	}
	fake.SetNow(sourceNow.Add(30 * time.Second))
	if b := lookup(); tokenCalls(fake) != 1 || b.AppRefused == "" {
		t.Errorf("within the minute: %d calls, %+v", tokenCalls(fake), b)
	}
	fake.SetDown(false)
	fake.SetNow(sourceNow.Add(61 * time.Second))
	if b := lookup(); tokenCalls(fake) != 2 || b.Kind != gitauth.KindGitHubApp {
		t.Errorf("after the minute: %d calls, %+v", tokenCalls(fake), b)
	}
}

func TestGitHubsRetryAfterIsHonoured(t *testing.T) {
	app := gitApp("shop", "https://github.com/acme/shop")
	r, _, fake, _ := githubReconciler(t, app)
	ctx := context.Background()
	fake.SetRateLimited(5 * time.Minute)
	_, _ = lookupGitBuild(ctx, r.Client, "shpyrd-system", r.Config.GitHub, app)
	fake.SetRateLimited(0)
	fake.SetNow(sourceNow.Add(4 * time.Minute))
	_, _ = lookupGitBuild(ctx, r.Client, "shpyrd-system", r.Config.GitHub, app)
	if tokenCalls(fake) != 1 {
		t.Errorf("asked again within Retry-After: %d calls", tokenCalls(fake))
	}
	fake.SetNow(sourceNow.Add(5*time.Minute + time.Second))
	if b, _ := lookupGitBuild(ctx, r.Client, "shpyrd-system", r.Config.GitHub, app); tokenCalls(fake) != 2 || b.Kind != gitauth.KindGitHubApp {
		t.Errorf("after Retry-After: %d calls, %+v", tokenCalls(fake), b)
	}
}

func TestATokenReplacingTheGitHubAppDropsItsMarks(t *testing.T) {
	app := gitApp("shop", "https://github.com/acme/shop")
	r, c, _, st := githubReconciler(t, app, workspaceToken("default", "github.com", "ghp_ours"))
	ctx := context.Background()
	if _, err := r.reconcileGitCredentials(ctx, app); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteGitHubInstallation(ctx, store.DefaultWorkspace, 42); err != nil {
		t.Fatal(err)
	}
	got, err := r.reconcileGitCredentials(ctx, app)
	if err != nil || got.Kind != gitauth.KindToken {
		t.Fatalf("build = %+v, %v", got, err)
	}
	sec := buildSecretOf(t, c, app)
	if string(sec.Data[corev1.BasicAuthPasswordKey]) != "ghp_ours" || sec.Annotations[gitauth.AnnotationGitHubAccount] != "" || sec.Annotations[gitauth.AnnotationTokenMintedAt] != "" {
		t.Errorf("secret %q %v", sec.Data[corev1.BasicAuthPasswordKey], sec.Annotations)
	}
	if d := r.githubTokenRefresh(ctx, app); d != 0 {
		t.Errorf("a token needs no refresh: %v", d)
	}
}

// githubWatcher is a SourceReconciler on a platform with the fake's App.
func githubWatcher(t *testing.T, ls *fakeLsRemote, objs ...client.Object) *SourceReconciler {
	t.Helper()
	fake, st := connectedGitHub(t)
	r, c := newSourceReconciler(t, ls, append([]client.Object{fake.Secret("shpyrd-system")}, objs...)...)
	r.GitHub = &GitHubApp{Source: fake.Source(c, "shpyrd-system"), Installations: st}
	return r
}

func TestSourceListsWithTheGitHubAppsToken(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": testCommit}}
	app := gitApp("shop", "https://github.com/acme/shop")
	_, got := reconcileSource(t, githubWatcher(t, ls, app), app)
	if ls.kind != gitauth.KindGitHubApp || ls.url != "https://github.com/acme/shop" || got.PinnedCommit() != testCommit {
		t.Errorf("listed %q with %q; pinned %q", ls.url, ls.kind, got.PinnedCommit())
	}
}

// TestSourceListsAPrivateRepositoryWithTheGitHubAppsToken goes through
// the real listing, against a host that answers only the App's token.
func TestSourceListsAPrivateRepositoryWithTheGitHubAppsToken(t *testing.T) {
	gitauth.AllowLoopbackForTests(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if u, p, ok := req.BasicAuth(); !ok || u != "x-access-token" || p != "ghs_42_1" {
			w.Header().Set("WWW-Authenticate", `Basic realm="GitHub"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		pkt := func(s string) string { return fmt.Sprintf("%04x%s", len(s)+4, s) }
		_, _ = io.WriteString(w, pkt("# service=git-upload-pack\n")+"0000"+pkt(testCommit+" refs/heads/main\x00agent=git/2.47.0\n")+"0000")
	}))
	defer srv.Close()
	app := gitApp("shop", "https://github.com/acme/shop")
	r := githubWatcher(t, &fakeLsRemote{}, app)
	// The repository is github.com's; the listing goes to the test host,
	// with the credential the watcher chose.
	r.LsRemote = func(ctx context.Context, _ string, b gitauth.Build) (map[string]string, error) {
		return gitauth.LsRemote(ctx, srv.URL+"/acme/shop", b)
	}
	_, got := reconcileSource(t, r, app)
	if got.PinnedCommit() != testCommit {
		t.Errorf("pinned %q; record %+v", got.PinnedCommit(), sourceRecord(got))
	}
}

func TestSourceSaysWhyTheGitHubAppCouldNotReadTheRepository(t *testing.T) {
	ls := &fakeLsRemote{err: gitauth.ErrNoAccess}
	app := gitApp("vault", "https://github.com/acme/vault")
	_, got := reconcileSource(t, githubWatcher(t, ls, app), app)
	rec := sourceRecord(got)
	if !strings.HasPrefix(rec.Error, "The GitHub App's installation on Acme does not include vault: add the repository to it on GitHub. shpyrd could not read the repository") ||
		!strings.Contains(rec.Error, "shpyrd git-auth deploy-key") {
		t.Errorf("error %q", rec.Error)
	}
}

func TestGitHubTokenIsKeptOnlyWellBeforeItExpires(t *testing.T) {
	for _, age := range []time.Duration{52 * time.Minute, 54 * time.Minute} {
		app := pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit)
		r, c, fake, _ := githubReconciler(t, app)
		ctx := context.Background()
		if _, err := r.reconcileGitCredentials(ctx, app); err != nil {
			t.Fatal(err)
		}
		if sec := buildSecretOf(t, c, app); sec.Annotations[gitauth.AnnotationGitHubRepo] != "acme/shop" ||
			sec.Annotations[gitauth.AnnotationTokenExpiresAt] != sourceNow.Add(time.Hour).Format(time.RFC3339) {
			t.Errorf("annotations %v", sec.Annotations)
		}
		fake.SetDown(true)
		now := sourceNow.Add(age)
		fake.SetNow(now)
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(app)})
		if err != nil || res.RequeueAfter <= 0 || !now.Add(res.RequeueAfter).Before(sourceNow.Add(time.Hour)) {
			t.Errorf("%v: requeue after %v, %v", age, res.RequeueAfter, err)
		}
		err = c.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: gitauth.BuildSecretName(app.Name)}, &corev1.Secret{})
		if kept := err == nil; kept != (age < 53*time.Minute) {
			t.Errorf("%v: kept %v (%v)", age, kept, err)
		}
	}
}

func TestGitHubTokenIsNotKeptForAnotherRepository(t *testing.T) {
	app := gitApp("shop", "https://github.com/acme/shop")
	r, c, fake, _ := githubReconciler(t, app)
	ctx := context.Background()
	if _, err := r.reconcileGitCredentials(ctx, app); err != nil {
		t.Fatal(err)
	}
	fake.SetDown(true)
	fake.SetNow(sourceNow.Add(time.Minute))
	app.Spec.Source.Git.URL = "https://github.com/acme/site"
	b, err := r.reconcileGitCredentials(ctx, app)
	if err != nil || b.Kind != "" {
		t.Errorf("build = %+v, %v", b, err)
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: gitauth.BuildSecretName(app.Name)}, &corev1.Secret{}); !apierrors.IsNotFound(err) {
		t.Errorf("shop's token is not kept for site: %v", err)
	}
}

func TestGitHubForbiddingATokenIsARefusalNotABackoff(t *testing.T) {
	app := gitApp("shop", "https://github.com/acme/shop")
	r, _, fake, _ := githubReconciler(t, app, workspaceToken("default", "github.com", "ghp_ours"))
	ctx := context.Background()
	fake.SetForbidden(true)
	b, err := lookupGitBuild(ctx, r.Client, "shpyrd-system", r.Config.GitHub, app)
	if err != nil || b.Kind != gitauth.KindToken || b.AppRetry || b.AppRefused != "GitHub refused the GitHub App's installation on Acme a token for shop." {
		t.Errorf("build = %+v, %v", b, err)
	}
	fake.SetForbidden(false)
	if b, _ := lookupGitBuild(ctx, r.Client, "shpyrd-system", r.Config.GitHub, app); b.Kind != gitauth.KindGitHubApp || tokenCalls(fake) != 2 {
		t.Errorf("asked again at once: %d calls, %+v", tokenCalls(fake), b)
	}
}

func TestATokenGitHubAlreadyGaveIsUsedWhileItsInstallationIsBackedOff(t *testing.T) {
	shop := gitApp("shop", "https://github.com/acme/shop")
	site := gitApp("site", "https://github.com/acme/site")
	r, _, fake, _ := githubReconciler(t, shop, site)
	ctx := context.Background()
	if b, _ := lookupGitBuild(ctx, r.Client, "shpyrd-system", r.Config.GitHub, shop); b.Kind != gitauth.KindGitHubApp {
		t.Fatalf("shop: %+v", b)
	}
	fake.SetDown(true)
	if b, _ := lookupGitBuild(ctx, r.Client, "shpyrd-system", r.Config.GitHub, site); !b.AppRetry {
		t.Fatalf("site: %+v", b)
	}
	b, _ := lookupGitBuild(ctx, r.Client, "shpyrd-system", r.Config.GitHub, shop)
	if b.Kind != gitauth.KindGitHubApp || b.AppRetry || string(b.Secret.Data[corev1.BasicAuthPasswordKey]) != "ghs_42_1" || tokenCalls(fake) != 2 {
		t.Errorf("shop while backed off: %d calls, %+v", tokenCalls(fake), b)
	}
}
