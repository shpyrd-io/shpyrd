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

// githubTokenExpiry is when GitHub's token expires, when <app>-git does
// not say: an App token already in <app>-git is kept while GitHub gives no
// new one, until githubKeepMargin before it expires counting the next
// try, so no build starts with an expired one.
const (
	githubTokenExpiry = time.Hour
	githubKeepMargin  = 5 * time.Minute
)

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

// githubRefusalLife is how long GitHub's refusal of a repository is kept:
// a repository not granted, an installation gone, a 403. A change of the
// workspace's connections, or an installation event, forgets it sooner.
const githubRefusalLife = 10 * time.Minute

// GitHubApp is the App's credentials and the workspaces' installations.
type GitHubApp struct {
	Source        *github.Source
	Installations store.GitHub
	// Requeue reconciles a workspace's Apps on github.com again; nil does
	// nothing (before the controllers start). Set it with SetRequeue once
	// the API may call Changed.
	Requeue func(ctx context.Context, workspace string)

	mu sync.Mutex
	// failed is, by installation, until when GitHub is not asked again.
	failed map[int64]time.Time
	// refused is GitHub's refusal, by installation and repository.
	refused map[githubRepoKey]githubRefusal
	// brokenLogged is when a broken App was last logged.
	brokenLogged time.Time
}

type githubRepoKey struct {
	installation int64
	repo         string // lower case
}

type githubRefusal struct {
	workspace string
	why       string
	until     time.Time
}

// refusedLately is GitHub's refusal of the repository, while it is kept.
func (gh *GitHubApp) refusedLately(id int64, repo string) (string, bool) {
	gh.mu.Lock()
	defer gh.mu.Unlock()
	key := githubRepoKey{id, strings.ToLower(repo)}
	r, ok := gh.refused[key]
	if ok && !gh.now().Before(r.until) {
		delete(gh.refused, key)
		return "", false
	}
	return r.why, ok
}

// keepRefusal keeps GitHub's refusal of the repository for
// githubRefusalLife.
func (gh *GitHubApp) keepRefusal(ws string, id int64, repo, why string) {
	gh.mu.Lock()
	defer gh.mu.Unlock()
	if gh.refused == nil {
		gh.refused = map[githubRepoKey]githubRefusal{}
	}
	gh.refused[githubRepoKey{id, strings.ToLower(repo)}] = githubRefusal{workspace: ws, why: why, until: gh.now().Add(githubRefusalLife)}
}

// Forget drops the refusals kept for a workspace's repositories: its
// connections changed.
func (gh *GitHubApp) Forget(workspace string) {
	if gh == nil {
		return
	}
	gh.mu.Lock()
	defer gh.mu.Unlock()
	for k, r := range gh.refused {
		if r.workspace == workspace {
			delete(gh.refused, k)
		}
	}
}

// Changed is a workspace's GitHub connections changing (a connect, a
// disconnect, an installation event): its refusals are forgotten and its
// Apps on github.com reconciled again, so <app>-git follows at once.
func (gh *GitHubApp) Changed(ctx context.Context, workspace string) {
	if gh == nil {
		return
	}
	gh.Forget(workspace)
	gh.mu.Lock()
	requeue := gh.Requeue
	gh.mu.Unlock()
	if requeue != nil {
		requeue(ctx, workspace)
	}
}

// SetRequeue sets Requeue while the API may be calling Changed.
func (gh *GitHubApp) SetRequeue(requeue func(ctx context.Context, workspace string)) {
	gh.mu.Lock()
	gh.Requeue = requeue
	gh.mu.Unlock()
}

// logBroken says, at most once per github.Recheck, that the App's Secret
// makes no App.
func (gh *GitHubApp) logBroken(ctx context.Context, err error) {
	gh.mu.Lock()
	now := gh.now()
	due := gh.brokenLogged.IsZero() || now.Sub(gh.brokenLogged) >= github.Recheck
	if due {
		gh.brokenLogged = now
	}
	gh.mu.Unlock()
	if due {
		log.FromContext(ctx).Info("the GitHub App cannot be used; building with the other credentials", "err", err.Error())
	}
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
		case apiErr.RateLimited:
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
var githubMarks = []string{gitauth.AnnotationGitHubAccount, gitauth.AnnotationTokenMintedAt, gitauth.AnnotationTokenExpiresAt, gitauth.AnnotationGitHubRepo}

// build is the App's credential for the App's repository; nil when the
// App does not apply. refused says why it did not when it was tried, for
// the watcher's message, and retry that the reason may pass. A failure is
// logged, never returned: the other credentials are tried.
func (gh *GitHubApp) build(ctx context.Context, app *shpyrdv1.App, repo gitauth.Repo) (_ *gitauth.Build, refused string, retry bool) {
	ref, inst, err := gh.installationFor(ctx, app, repo)
	if err != nil {
		if errors.Is(err, errGitHubConnections) {
			log.FromContext(ctx).Info("building with the other credentials", "err", err.Error())
		} else {
			gh.logBroken(ctx, err)
		}
		return nil, "", false
	}
	logger := log.FromContext(ctx)
	cl, name, ws := ref.client, ref.name, workspaceOf(app)
	switch {
	case inst == nil:
		return nil, "", false
	case inst.SuspendedAt != nil:
		return nil, fmt.Sprintf("The GitHub App's installation on %s is suspended: an owner of %s unsuspends it on GitHub.", inst.AccountLogin, inst.AccountLogin), false
	}
	// A refusal is kept: the repository is not asked for again until it
	// ages or the workspace's connections change.
	if why, ok := gh.refusedLately(inst.ID, name); ok {
		return nil, why, false
	}
	// A token GitHub already gave is used without asking; the backoff
	// only holds back a call.
	tok, cached := cl.CachedInstallationToken(inst.ID, name, github.ContentsRead())
	if !cached && gh.backingOff(inst.ID) {
		return nil, githubTransient, true
	}
	if !cached {
		callCtx, cancel := context.WithTimeout(ctx, githubCallTimeout)
		defer cancel()
		tok, err = cl.InstallationToken(callCtx, inst.ID, name, github.ContentsRead())
	}
	var apiErr *github.APIError
	switch {
	case errors.Is(err, github.ErrRepoNotGranted):
		why := fmt.Sprintf("The GitHub App's installation on %s does not include %s: add the repository to it on GitHub.", inst.AccountLogin, name)
		gh.keepRefusal(ws, inst.ID, name, why)
		return nil, why, false
	case errors.Is(err, github.ErrInstallationGone):
		why := fmt.Sprintf("The GitHub App is no longer installed on %s: an owner of %s installs it again, then a workspace admin connects it from Workspace settings → Git.", inst.AccountLogin, inst.AccountLogin)
		gh.keepRefusal(ws, inst.ID, name, why)
		return nil, why, false
	case errors.As(err, &apiErr) && apiErr.Status == http.StatusForbidden && !apiErr.RateLimited:
		logger.Info("GitHub refused the App a token; building with the other credentials", "installation", inst.ID, "err", err.Error())
		why := fmt.Sprintf("GitHub refused the GitHub App a token for %s on %s: check the App's permissions and repository access on GitHub.", name, inst.AccountLogin)
		gh.keepRefusal(ws, inst.ID, name, why)
		return nil, why, false
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
	if !tok.ExpiresAt.IsZero() {
		b.Secret.Annotations[gitauth.AnnotationTokenExpiresAt] = tok.ExpiresAt.UTC().Format(time.RFC3339)
	}
	b.Secret.Annotations[gitauth.AnnotationGitHubRepo] = repo.Path
	return &b, "", false
}

// githubRepoRef is a repository the GitHub App reaches: the App's client,
// the installation, and the repository's owner and name.
type githubRepoRef struct {
	client       *github.Client
	installation int64
	owner, name  string
}

// errGitHubConnections: the workspace's GitHub connections could not be
// read.
var errGitHubConnections = errors.New("could not read the workspace's GitHub connections")

// installationFor is the GitHub App's hold on the repository, without a
// call to GitHub: a github.com URL, the App configured, and the App's
// workspace's installation on the repository's owner (an unsuspended one
// first). inst is nil when the App does not reach the repository. err is
// an App Secret that makes no App, or the connections unread
// (errGitHubConnections): it is not known then, never "no". The clone's
// credential (build) and cover are this one test.
func (gh *GitHubApp) installationFor(ctx context.Context, app *shpyrdv1.App, repo gitauth.Repo) (githubRepoRef, *store.GitHubInstallation, error) {
	if gh == nil || gh.Source == nil || gh.Installations == nil || !strings.EqualFold(repo.Host, "github.com") {
		return githubRepoRef{}, nil, nil
	}
	owner, name, ok := strings.Cut(repo.Path, "/")
	if !ok || strings.Contains(name, "/") {
		return githubRepoRef{}, nil, nil
	}
	cl, err := gh.Source.Client(ctx)
	if err == github.ErrNotConfigured { //nolint:errorlint // identity, not a wrapped error, is the point
		// A bare ErrNotConfigured is no Secret at all, the usual case on a
		// platform without the App; a broken Secret wraps it, and is an
		// error.
		return githubRepoRef{}, nil, nil
	}
	if err != nil {
		return githubRepoRef{}, nil, err
	}
	list, err := gh.Installations.GitHubInstallations(ctx, workspaceOf(app))
	if err != nil {
		return githubRepoRef{}, nil, fmt.Errorf("%w: %w", errGitHubConnections, err)
	}
	var inst *store.GitHubInstallation
	for i := range list {
		if strings.EqualFold(list[i].AccountLogin, owner) && (inst == nil || inst.SuspendedAt != nil) {
			inst = &list[i]
		}
	}
	if inst == nil {
		return githubRepoRef{}, nil, nil
	}
	return githubRepoRef{client: cl, installation: inst.ID, owner: owner, name: name}, inst, nil
}

// cover says the GitHub App covers the App's repository: installationFor
// finds a live, unsuspended installation. Watch paths, wait for CI and the
// reporter work only then. An error is "not known", never "not covered":
// the callers decide what an unknown cover does (coverFailure words it).
func (gh *GitHubApp) cover(ctx context.Context, app *shpyrdv1.App) (githubRepoRef, bool, error) {
	if gh == nil || app.Spec.Source == nil || app.Spec.Source.Git == nil {
		return githubRepoRef{}, false, nil
	}
	repo, err := gitauth.Parse(app.Spec.Source.Git.URL)
	if err != nil {
		return githubRepoRef{}, false, nil
	}
	ref, inst, err := gh.installationFor(ctx, app, repo)
	if err != nil {
		log.FromContext(ctx).Info("could not tell whether the GitHub App covers the repository", "err", err.Error())
		return githubRepoRef{}, false, err
	}
	if inst == nil || inst.SuspendedAt != nil {
		return githubRepoRef{}, false, nil
	}
	return ref, true, nil
}

// coverFailure is cover's error in a person's words, for a release note.
func coverFailure(err error) string {
	if errors.Is(err, errGitHubConnections) {
		return "shpyrd could not read the workspace's GitHub connections"
	}
	return "shpyrd could not read the GitHub App's settings"
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
// it is for the App's repository and will not have expired by the next
// try, with a margin; nil otherwise. GitHub giving no new one for a while
// leaves builds the one they have.
func (r *AppReconciler) keptAppToken(ctx context.Context, app *shpyrdv1.App) *gitauth.Build {
	old := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: gitauth.BuildSecretName(app.Name)}, old); err != nil {
		return nil
	}
	repo, err := gitauth.Parse(app.Spec.Source.Git.URL)
	if err != nil || !strings.EqualFold(old.Annotations[gitauth.AnnotationGitHubRepo], repo.Path) {
		return nil
	}
	account := old.Annotations[gitauth.AnnotationGitHubAccount]
	minted, err := time.Parse(time.RFC3339, old.Annotations[gitauth.AnnotationTokenMintedAt])
	if account == "" || err != nil {
		return nil
	}
	expires := minted.Add(githubTokenExpiry)
	if at, err := time.Parse(time.RFC3339, old.Annotations[gitauth.AnnotationTokenExpiresAt]); err == nil {
		expires = at
	}
	if !r.now().Add(githubRetry).Before(expires.Add(-githubKeepMargin)) {
		return nil
	}
	tok := &corev1.Secret{Data: map[string][]byte{gitauth.KeyToken: old.Data[corev1.BasicAuthPasswordKey], gitauth.KeyUsername: old.Data[corev1.BasicAuthUsernameKey]}}
	b, err := gitauth.Resolve(app.Spec.Source.Git.URL, app.Name, app.Namespace, nil, tok)
	if err != nil || b.Secret == nil {
		return nil
	}
	b.Kind = gitauth.KindGitHubApp
	for _, k := range githubMarks {
		if v, ok := old.Annotations[k]; ok {
			b.Secret.Annotations[k] = v
		}
	}
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
