package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
	"github.com/shpyrd-io/shpyrd/pkg/github"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// The GitHub App (RFC-0054): a build of a github.com repository clones
// with a token the App mints for that one repository, when the App's
// workspace has an installation on the repository's owner. Anything that
// stops it falls through to the deploy key, then the token.

// githubTokenLife is how old the App's token in <app>-git may get before
// it is written again: tokens last an hour, and kpack, which reads the
// Secret whenever one of its rebuilds starts, never gets one in its last
// ten minutes.
const githubTokenLife = 50 * time.Minute

// githubTokenExpiry is when GitHub's token expires: an App token already
// in <app>-git is kept until then while GitHub gives no new one.
const githubTokenExpiry = time.Hour

// githubRetry is how soon an App whose token GitHub did not give is
// reconciled again.
const githubRetry = 90 * time.Second

// A failed token call is not repeated for githubBackoff, or for
// githubRateBackoff after a 403 or 429 that names no time; each call has
// githubCallTimeout, so a slow GitHub does not hold the controller.
const (
	githubBackoff     = time.Minute
	githubRateBackoff = 5 * time.Minute
	githubCallTimeout = 5 * time.Second
)

// GitHubApp is the App's credentials and the workspaces' installations.
type GitHubApp struct {
	Source        *github.Source
	Installations store.GitHub

	// failed is, by installation, until when GitHub is not asked again.
	mu     sync.Mutex
	failed map[int64]time.Time
}

func (gh *GitHubApp) now() time.Time {
	if gh.Source != nil && gh.Source.Options.Now != nil {
		return gh.Source.Options.Now()
	}
	return time.Now()
}

// backingOff says a token call for the installation failed lately.
func (gh *GitHubApp) backingOff(id int64) bool {
	gh.mu.Lock()
	defer gh.mu.Unlock()
	until, ok := gh.failed[id]
	if ok && !gh.now().Before(until) {
		delete(gh.failed, id)
		return false
	}
	return ok
}

// remember keeps a failed token call: a minute, or what GitHub asked for
// when it limits the rate.
func (gh *GitHubApp) remember(id int64, err error) {
	wait := githubBackoff
	var apiErr *github.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.RetryAfter > 0:
			wait = max(githubBackoff, apiErr.RetryAfter)
		case apiErr.Status == http.StatusForbidden || apiErr.Status == http.StatusTooManyRequests:
			wait = githubRateBackoff
		}
	}
	gh.mu.Lock()
	defer gh.mu.Unlock()
	if gh.failed == nil {
		gh.failed = map[int64]time.Time{}
	}
	gh.failed[id] = gh.now().Add(wait)
}

// githubTransient is the App's reason when GitHub gave no token for a
// reason that may pass.
const githubTransient = "GitHub did not give shpyrd a token for this repository just now; shpyrd tries again by itself."

// githubMarks are the annotations only a GitHub App credential carries:
// another credential replacing it must not keep them.
var githubMarks = []string{gitauth.AnnotationGitHubAccount, gitauth.AnnotationTokenMintedAt}

// build is the App's credential for the App's repository; nil when the
// App does not apply. refused says why it did not when it was tried, for
// the watcher's message, and retry that the reason may pass. A failure is
// logged, never returned: the other credentials are tried.
func (gh *GitHubApp) build(ctx context.Context, app *shpyrdv1.App, repo gitauth.Repo) (_ *gitauth.Build, refused string, retry bool) {
	if gh == nil || gh.Source == nil || gh.Installations == nil || !strings.EqualFold(repo.Host, "github.com") {
		return nil, "", false
	}
	owner, name, ok := strings.Cut(repo.Path, "/")
	if !ok || strings.Contains(name, "/") {
		return nil, "", false
	}
	logger := log.FromContext(ctx)
	cl, err := gh.Source.Client(ctx)
	if err != nil {
		// A bare ErrNotConfigured is no Secret at all, the usual case on a
		// platform without the App; a broken Secret or a failed read is
		// worth a line.
		if err != github.ErrNotConfigured { //nolint:errorlint // identity, not a wrapped error, is the point
			logger.Info("the GitHub App cannot be used; building with the other credentials", "err", err.Error())
		}
		return nil, "", false
	}
	list, err := gh.Installations.GitHubInstallations(ctx, workspaceOf(app))
	if err != nil {
		logger.Info("could not read the workspace's GitHub connections; building with the other credentials", "err", err.Error())
		return nil, "", false
	}
	var inst *store.GitHubInstallation
	for i := range list {
		if strings.EqualFold(list[i].AccountLogin, owner) {
			inst = &list[i]
		}
	}
	switch {
	case inst == nil:
		return nil, "", false
	case inst.SuspendedAt != nil:
		return nil, fmt.Sprintf("The GitHub App's installation on %s is suspended: an owner of %s unsuspends it on GitHub.", inst.AccountLogin, inst.AccountLogin), false
	}
	if gh.backingOff(inst.ID) {
		return nil, githubTransient, true
	}
	callCtx, cancel := context.WithTimeout(ctx, githubCallTimeout)
	defer cancel()
	tok, err := cl.InstallationToken(callCtx, inst.ID, name)
	switch {
	case errors.Is(err, github.ErrRepoNotGranted):
		return nil, fmt.Sprintf("The GitHub App's installation on %s does not include %s: add the repository to it on GitHub.", inst.AccountLogin, name), false
	case errors.Is(err, github.ErrInstallationGone):
		return nil, fmt.Sprintf("The GitHub App is no longer installed on %s: an owner of %s installs it again, then a workspace admin connects it from Workspace settings → Git.", inst.AccountLogin, inst.AccountLogin), false
	case err != nil:
		gh.remember(inst.ID, err)
		logger.Info("GitHub gave no token; building with the other credentials", "installation", inst.ID, "err", err.Error())
		return nil, githubTransient, true
	}
	sec := &corev1.Secret{Data: map[string][]byte{gitauth.KeyToken: []byte(tok.Value), gitauth.KeyUsername: []byte("x-access-token")}}
	b, err := gitauth.Resolve(app.Spec.Source.Git.URL, app.Name, app.Namespace, nil, sec)
	if err != nil {
		return nil, "", false
	}
	b.Kind = gitauth.KindGitHubApp
	b.Secret.Annotations[gitauth.AnnotationGitHubAccount] = inst.AccountLogin
	b.Secret.Annotations[gitauth.AnnotationTokenMintedAt] = tok.MintedAt.UTC().Format(time.RFC3339)
	return &b, "", false
}

// githubTokenRefresh is how long until <app>-git must be written again:
// 50 minutes after its token was minted, and a second. 0 when the App's
// builds do not clone through the GitHub App.
func (r *AppReconciler) githubTokenRefresh(ctx context.Context, app *shpyrdv1.App) time.Duration {
	sec := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: gitauth.BuildSecretName(app.Name)}, sec); err != nil {
		return 0
	}
	return r.githubRefreshOf(gitauth.Build{Kind: gitauth.KindGitHubApp, Secret: sec})
}

// githubRefreshOf is when the credential just written needs the App again:
// soon when GitHub gave no token for a reason that may pass, 50 minutes
// after its token was minted when it holds one, else never (0).
func (r *AppReconciler) githubRefreshOf(b gitauth.Build) time.Duration {
	if b.AppRetry {
		return githubRetry
	}
	if b.Kind != gitauth.KindGitHubApp || b.Secret == nil {
		return 0
	}
	minted, err := time.Parse(time.RFC3339, b.Secret.Annotations[gitauth.AnnotationTokenMintedAt])
	if err != nil {
		return 0
	}
	return max(minted.Add(githubTokenLife).Sub(r.now())+time.Second, time.Second)
}

// keptAppToken is the App token already in <app>-git, as a build, while
// it has not expired; nil otherwise. GitHub giving no new one for a while
// leaves builds the one they have.
func (r *AppReconciler) keptAppToken(ctx context.Context, app *shpyrdv1.App) *gitauth.Build {
	old := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: gitauth.BuildSecretName(app.Name)}, old); err != nil {
		return nil
	}
	account := old.Annotations[gitauth.AnnotationGitHubAccount]
	minted, err := time.Parse(time.RFC3339, old.Annotations[gitauth.AnnotationTokenMintedAt])
	if account == "" || err != nil || r.now().Sub(minted) >= githubTokenExpiry {
		return nil
	}
	tok := &corev1.Secret{Data: map[string][]byte{gitauth.KeyToken: old.Data[corev1.BasicAuthPasswordKey], gitauth.KeyUsername: old.Data[corev1.BasicAuthUsernameKey]}}
	b, err := gitauth.Resolve(app.Spec.Source.Git.URL, app.Name, app.Namespace, nil, tok)
	if err != nil || b.Secret == nil {
		return nil
	}
	b.Kind = gitauth.KindGitHubApp
	b.Secret.Annotations[gitauth.AnnotationGitHubAccount] = account
	b.Secret.Annotations[gitauth.AnnotationTokenMintedAt] = old.Annotations[gitauth.AnnotationTokenMintedAt]
	return &b
}

// appRefusedError is a listing that failed after the GitHub App could not
// give access: the watcher says the App's reason first.
type appRefusedError struct {
	why string
	err error
}

func (e *appRefusedError) Error() string { return e.err.Error() }
func (e *appRefusedError) Unwrap() error { return e.err }
