package api

import (
	"context"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/internal/controller"
)

// githubSourceNote says watch paths or wait for CI are set where they do
// nothing (RFC-0054).
const githubSourceNote = "Watch paths and wait for CI work through the GitHub App, which does not cover this repository: every push deploys. A workspace admin connects GitHub for the repository's owner in Workspace settings → Git."

// githubCovers says the GitHub App covers the App's repository, by the
// controller's own test (GitHubApp.Covers, shared with the watcher and the
// reporter). Whether the installation includes the repository is asked of
// GitHub once, then kept: the token while it lasts, a refusal ten minutes
// or until the workspace's connections change.
func (s *Server) githubCovers(ctx context.Context, app *shpyrdv1.App) bool {
	s.githubCoverMu.Lock()
	gh := s.githubCover
	if gh == nil || gh.Source != s.githubApp {
		gh = &controller.GitHubApp{Source: s.githubApp, Installations: s.store}
		s.githubCover = gh
	}
	s.githubCoverMu.Unlock()
	return gh.Covers(ctx, app)
}

// forgetGitHubCover drops what the API kept of a workspace's repositories:
// its connections changed.
func (s *Server) forgetGitHubCover(ws string) {
	s.githubCoverMu.Lock()
	gh := s.githubCover
	s.githubCoverMu.Unlock()
	gh.Forget(ws)
}

// projectDetail is a project's view with what the GitHub App adds: whether
// it covers the repository, and a note when the settings it needs are set
// without it.
func (s *Server) projectDetail(ctx context.Context, app *shpyrdv1.App) AppDetail {
	d := detail(app, s.buildsByDigest(ctx, app))
	d.GitHubFeatures = s.githubCovers(ctx, app)
	if src := app.Spec.Source; src != nil && src.Git != nil && !d.GitHubFeatures && (len(src.Git.WatchPaths) > 0 || src.Git.WaitForChecks) {
		d.SourceNote = githubSourceNote
	}
	d.GitHubLinkedBy = app.Annotations[shpyrdv1.AnnotationGitHubLinkedBy]
	return d
}
