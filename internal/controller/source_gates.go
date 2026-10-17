package controller

import (
	"context"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
)

// Steps 4 and 5 of the source watcher (RFC-0054): watch paths and wait for
// CI, for a repository the GitHub App covers. decideSource stays pure: it
// says what it needs from GitHub (sourceUpdate.ask), Reconcile gathers it
// and decides again.

// githubGateTimeout bounds the calls one decision makes to GitHub.
const githubGateTimeout = 15 * time.Second

// sourceGates are GitHub's answers for steps 4 and 5.
type sourceGates struct {
	// covered: the GitHub App covers the repository; the gates apply only
	// then.
	covered bool
	// unknown says why shpyrd could not tell whether the App covers the
	// repository ("" when it could): its Secret or the workspace's
	// connections could not be read. It is never "not covered": a commit
	// deploys with its watch paths not checked, and the note says why.
	unknown string
	// changed is the compare of the pinned commit with the new one; nil
	// until asked.
	changed *changedFiles
}

// changedFiles is what changed since the pinned commit.
type changedFiles struct {
	files []string
	// untrusted says why the list cannot be trusted ("" when it can): the
	// commit then deploys, and its release note says so.
	untrusted string
}

// githubAsk is what decideSource needs from GitHub before it decides sha.
type githubAsk struct {
	sha     string
	changed bool
}

func (a githubAsk) any() bool { return a.changed }

// hasGates says the App sets watch paths or wait for CI.
func hasGates(app *shpyrdv1.App) bool {
	if app.Spec.Source == nil || app.Spec.Source.Git == nil {
		return false
	}
	git := app.Spec.Source.Git
	return len(git.WatchPaths) > 0 || git.WaitForChecks
}

// gateCommit runs steps 4 and 5 on a commit about to deploy. It returns
// what the release note gains, and stop when the commit does not deploy
// now: not deployed (the record says why), or GitHub must be asked first
// (u.ask says what).
func gateCommit(app *shpyrdv1.App, u *sourceUpdate, rec gitauth.SourceRecord, sha string, ev sourceEvent, now time.Time) (string, bool) {
	git := app.Spec.Source.Git
	if !ev.gates.covered && ev.gates.unknown == "" {
		return "", false
	}
	note := ""
	if len(git.WatchPaths) > 0 {
		c := ev.gates.changed
		switch {
		case ev.gates.unknown != "":
			note = " (watch paths not checked: " + ev.gates.unknown + ")"
		case c == nil:
			u.ask = githubAsk{sha: sha, changed: true}
			return "", true
		case c.untrusted != "":
			note = " (watch paths not checked: " + c.untrusted + ")"
		case !gitauth.MatchWatchPaths(git.WatchPaths, c.files):
			notDeploy(u, rec, sha, gitauth.ReasonNoWatchedChanges)
			return "", true
		}
	}
	return note, false
}

// notDeploy records sha as not deployed, for reason; a decision not
// recorded before is audited (git.skip, the reason as its detail).
func notDeploy(u *sourceUpdate, rec gitauth.SourceRecord, sha, reason string) {
	u.record.NotDeployed, u.record.NotDeployedReason = sha, reason
	if rec.NotDeployed != sha || rec.NotDeployedReason != reason {
		u.audit, u.target, u.reason = "git.skip", sha, reason
	}
}

// gather asks GitHub, through the App, what decideSource asked for.
func (r *SourceReconciler) gather(ctx context.Context, app *shpyrdv1.App, ev *sourceEvent, ask githubAsk, now time.Time) {
	ref, ok, err := r.GitHub.cover(ctx, app)
	if err != nil {
		ev.gates.covered, ev.gates.unknown = false, coverFailure(err)
		return
	}
	if !ok {
		ev.gates.covered = false
		return
	}
	ctx, cancel := context.WithTimeout(ctx, githubGateTimeout)
	defer cancel()
	if ask.changed {
		c := r.changedFiles(ctx, ref, app.PinnedCommit(), ask.sha)
		ev.gates.changed = &c
	}
}

// changedFiles is GitHub's compare of the pinned commit with head: every
// file changed since what runs, so a commit not deployed earlier counts
// once a later one matches. A list that cannot be trusted says why.
//
// A failure here is the watcher's own: it neither backs the clone off nor
// records a refusal. A clone backing off (GitHub limiting the
// installation's rate) is respected, not asked again.
func (r *SourceReconciler) changedFiles(ctx context.Context, ref githubRepoRef, base, head string) changedFiles {
	if r.GitHub.backingOff(ref.installation) {
		return changedFiles{untrusted: "GitHub is not answering"}
	}
	cmp, err := ref.client.Compare(ctx, ref.installation, ref.owner, ref.name, base, head)
	switch {
	case err != nil:
		log.FromContext(ctx).Info("could not compare the commits on GitHub; deploying without checking the watch paths", "commit", shortCommit(head), "err", err.Error())
		return changedFiles{untrusted: "GitHub's compare failed"}
	case cmp.Capped:
		return changedFiles{untrusted: "more than 300 files changed"}
	case cmp.Status == "diverged" || cmp.Status == "behind":
		return changedFiles{untrusted: "the branch's history was rewritten"}
	}
	return changedFiles{files: cmp.Files}
}
