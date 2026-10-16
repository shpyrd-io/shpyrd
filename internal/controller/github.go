package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

// GitHubApp is the App's credentials and the workspaces' installations.
type GitHubApp struct {
	Source        *github.Source
	Installations store.GitHub
}

// githubMarks are the annotations only a GitHub App credential carries:
// another credential replacing it must not keep them.
var githubMarks = []string{gitauth.AnnotationGitHubAccount, gitauth.AnnotationTokenMintedAt}

// build is the App's credential for the App's repository; nil when the
// App does not apply. refused says why it did not when it was tried, for
// the watcher's message. A failure is logged, never returned: the other
// credentials are tried.
func (gh *GitHubApp) build(ctx context.Context, app *shpyrdv1.App, repo gitauth.Repo) (*gitauth.Build, string) {
	if gh == nil || gh.Source == nil || gh.Installations == nil || !strings.EqualFold(repo.Host, "github.com") {
		return nil, ""
	}
	owner, name, ok := strings.Cut(repo.Path, "/")
	if !ok || strings.Contains(name, "/") {
		return nil, ""
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
		return nil, ""
	}
	list, err := gh.Installations.GitHubInstallations(ctx, workspaceOf(app))
	if err != nil {
		logger.Info("could not read the workspace's GitHub connections; building with the other credentials", "err", err.Error())
		return nil, ""
	}
	var inst *store.GitHubInstallation
	for i := range list {
		if strings.EqualFold(list[i].AccountLogin, owner) {
			inst = &list[i]
		}
	}
	switch {
	case inst == nil:
		return nil, ""
	case inst.SuspendedAt != nil:
		return nil, fmt.Sprintf("The GitHub App's installation on %s is suspended: an owner of %s unsuspends it on GitHub.", inst.AccountLogin, inst.AccountLogin)
	}
	tok, err := cl.InstallationToken(ctx, inst.ID, name)
	switch {
	case errors.Is(err, github.ErrRepoNotGranted):
		return nil, fmt.Sprintf("The GitHub App's installation on %s does not include %s: add the repository to it on GitHub.", inst.AccountLogin, name)
	case errors.Is(err, github.ErrInstallationGone):
		return nil, fmt.Sprintf("The GitHub App is no longer installed on %s: an owner of %s installs it again on GitHub.", inst.AccountLogin, inst.AccountLogin)
	case err != nil:
		logger.Info("GitHub gave no token; building with the other credentials", "installation", inst.ID, "err", err.Error())
		return nil, "GitHub did not give shpyrd a token for this repository just now."
	}
	sec := &corev1.Secret{Data: map[string][]byte{gitauth.KeyToken: []byte(tok.Value), gitauth.KeyUsername: []byte("x-access-token")}}
	b, err := gitauth.Resolve(app.Spec.Source.Git.URL, app.Name, app.Namespace, nil, sec)
	if err != nil {
		return nil, ""
	}
	b.Kind = gitauth.KindGitHubApp
	b.Secret.Annotations[gitauth.AnnotationGitHubAccount] = inst.AccountLogin
	b.Secret.Annotations[gitauth.AnnotationTokenMintedAt] = tok.MintedAt.UTC().Format(time.RFC3339)
	return &b, ""
}

// githubTokenRefresh is how long until <app>-git must be written again:
// 50 minutes after its token was minted, and a second. 0 when the App's
// builds do not clone through the GitHub App.
func (r *AppReconciler) githubTokenRefresh(ctx context.Context, app *shpyrdv1.App) time.Duration {
	sec := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: gitauth.BuildSecretName(app.Name)}, sec); err != nil {
		return 0
	}
	minted, err := time.Parse(time.RFC3339, sec.Annotations[gitauth.AnnotationTokenMintedAt])
	if err != nil {
		return 0
	}
	return max(minted.Add(githubTokenLife).Sub(r.now())+time.Second, time.Second)
}

// appRefusedError is a listing that failed after the GitHub App could not
// give access: the watcher says the App's reason first.
type appRefusedError struct {
	why string
	err error
}

func (e *appRefusedError) Error() string { return e.err.Error() }
func (e *appRefusedError) Unwrap() error { return e.err }
