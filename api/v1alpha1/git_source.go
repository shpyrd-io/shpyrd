package v1alpha1

import "regexp"

// The source watcher (RFC-0018): a git source on a branch or tag is
// watched and builds the commit pinned for it; a commit is built as given.

var (
	commitRevision = regexp.MustCompile(`(?i)^[0-9a-f]{7,40}$`)
	fullCommit     = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)
)

// IsCommit says a revision names a commit: 7 to 40 hex digits.
func IsCommit(rev string) bool { return commitRevision.MatchString(rev) }

// GitRevision is the revision the git source follows: "main" when unset,
// "" without a git source.
func (a *App) GitRevision() string {
	if a.Spec.Source == nil || a.Spec.Source.Git == nil {
		return ""
	}
	if a.Spec.Source.Git.Revision == "" {
		return "main"
	}
	return a.Spec.Source.Git.Revision
}

// WatchesGit says the App follows a git branch or tag, and so builds the
// commit pinned for it.
func (a *App) WatchesGit() bool {
	rev := a.GitRevision()
	return rev != "" && !IsCommit(rev)
}

// PinnedCommit is the commit pinned for the revision the App follows; ""
// when none is, or the pin was made for another revision.
func (a *App) PinnedCommit() string {
	if !a.WatchesGit() || a.Annotations[AnnotationGitCommitRef] != a.GitRevision() {
		return ""
	}
	if sha := a.Annotations[AnnotationGitCommit]; fullCommit.MatchString(sha) {
		return sha
	}
	return ""
}

// BuildRevision is what a build of the git source checks out: the pinned
// commit, else the revision as given.
func (a *App) BuildRevision() string {
	if sha := a.PinnedCommit(); sha != "" {
		return sha
	}
	return a.GitRevision()
}

// AutoDeploys says a new commit of the branch deploys: true unless
// spec.source.git.autoDeploy is false.
func (a *App) AutoDeploys() bool {
	if a.Spec.Source == nil || a.Spec.Source.Git == nil {
		return false
	}
	return a.Spec.Source.Git.AutoDeploy == nil || *a.Spec.Source.Git.AutoDeploy
}
