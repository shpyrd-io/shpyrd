package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
	"github.com/shpyrd-io/shpyrd/pkg/github"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// githubAccessTimeout bounds the two calls the check makes.
const githubAccessTimeout = 15 * time.Second

// githubAccess checks that whoever the request acts as (the person, or
// for an API token its maker) can read the GitHub repository a project is
// made from or moved to (RFC-0054). It answers the request itself when
// they cannot, and says not ok. login is the GitHub account that could
// read it; "" when no check applied: a repository not on github.com, an
// owner the workspace has no live connection on, a platform without the
// App, a public repository, the admin token.
func (s *Server) githubAccess(c *gin.Context, gitURL string) (string, bool) {
	repo, err := gitauth.Parse(gitURL)
	if err != nil || !strings.EqualFold(repo.Host, "github.com") {
		return "", true
	}
	owner, name, found := strings.Cut(strings.TrimSuffix(repo.Path, ".git"), "/")
	if !found || strings.Contains(name, "/") {
		return "", true
	}
	ctx := c.Request.Context()
	cl, err := s.githubApp.Client(ctx)
	if err != nil {
		// A bare ErrNotConfigured is no App at all, the usual case; a broken
		// Secret wraps it, and is worth a line. Builds then use the deploy
		// key or the token, so nothing is reached through the App.
		if err != github.ErrNotConfigured { //nolint:errorlint // identity, not a wrapped error, is the point
			s.log.Warn("github: the App cannot be used; the repository is not checked", "err", err.Error())
		}
		return "", true
	}
	insts, err := s.store.GitHubInstallations(ctx, s.doorWorkspace(c))
	if err != nil {
		s.log.Warn("github: could not read the workspace's GitHub connections to check a repository", "err", err.Error())
		abort(c, http.StatusBadGateway, errors.New("shpyrd could not read the workspace's GitHub connections. Try again in a minute"))
		return "", false
	}
	// The first live installation on the owner, as the controller picks
	// its credential (installationFor).
	var inst *store.GitHubInstallation
	for i := range insts {
		if strings.EqualFold(insts[i].AccountLogin, owner) && insts[i].SuspendedAt == nil {
			inst = &insts[i]
			break
		}
	}
	if inst == nil {
		return "", true
	}
	provider, subject, person := s.githubPersonOf(c)
	if !person {
		return "", true
	}
	// One deadline for every call: the App's read, a renewal of the
	// person's token, and the person's read.
	callCtx, cancel := context.WithTimeout(ctx, githubAccessTimeout)
	defer cancel()
	info, err := cl.RepositoryAsApp(callCtx, inst.ID, owner, name)
	outside := github.IsNotFound(err)
	switch {
	case outside:
		// Not said yet: whether the installation includes a repository is
		// told only to someone linked who can read it.
	case err != nil:
		// GitHub's words stay in the log: the answer is one sentence.
		s.log.Warn("github: the App could not read a repository to check it", "repository", owner+"/"+name, "err", err.Error())
		abort(c, http.StatusBadGateway, errors.New("GitHub did not answer. Try again in a minute"))
		return "", false
	case !info.Private:
		return "", true
	}
	tok, login, err := s.githubPersonTokenIn(callCtx, c, cl)
	if err != nil {
		s.githubLinkAnswer(c, err)
		return "", false
	}
	_, err = cl.UserRepository(callCtx, tok, owner, name)
	if githubCantRead(err) {
		abort(c, http.StatusForbidden, errGitHubCantRead(owner, name))
		return "", false
	}
	if err != nil {
		s.githubPersonFailed(c, provider, subject, owner+"/"+name, err)
		return "", false
	}
	if outside {
		abort(c, http.StatusUnprocessableEntity, fmt.Errorf("The GitHub App's installation on %s does not include %s/%s: add it to the installation on GitHub, or give shpyrd a deploy key (shpyrd git-auth deploy-key)", inst.AccountLogin, owner, name))
		return "", false
	}
	return login, true
}

// sameRepository says two git URLs name one repository: the same host and
// path, in any case, in any form.
func sameRepository(a, b string) bool {
	ra, errA := gitauth.Parse(a)
	rb, errB := gitauth.Parse(b)
	if errA != nil || errB != nil {
		return strings.TrimSpace(a) == strings.TrimSpace(b)
	}
	return strings.EqualFold(ra.Host, rb.Host) && strings.EqualFold(ra.Path, rb.Path)
}

// markGitHubLinkedBy records whose GitHub access picked the repository;
// "" removes the record.
func markGitHubLinkedBy(a *shpyrdv1.App, login string) {
	if login == "" {
		delete(a.Annotations, shpyrdv1.AnnotationGitHubLinkedBy)
		return
	}
	if a.Annotations == nil {
		a.Annotations = map[string]string{}
	}
	a.Annotations[shpyrdv1.AnnotationGitHubLinkedBy] = login
}
