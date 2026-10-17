package controller

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
	"github.com/shpyrd-io/shpyrd/pkg/github"
)

// Steps 4 and 5 of the source watcher (RFC-0054): watch paths and wait for
// CI, for a repository the GitHub App covers. decideSource stays pure: it
// says what it needs from GitHub (sourceUpdate.ask), Reconcile gathers it
// and decides again.

// githubGateTimeout bounds the calls one decision makes to GitHub.
const githubGateTimeout = 15 * time.Second

// sourceGates are GitHub's answers for steps 4 and 5.
type sourceGates struct {
	// known: cover was asked (gather asks it with the first question for
	// GitHub, so a commit that reaches no gate never asks it).
	known bool
	// covered: the GitHub App covers the repository; the gates apply only
	// then. ref is the repository through the App.
	covered bool
	ref     githubRepoRef
	// unknown says why shpyrd could not tell whether the App covers the
	// repository ("" when it could): its Secret or the workspace's
	// connections could not be read. It is never "not covered": a commit
	// deploys with its watch paths not checked, and the note says why.
	unknown string
	// changed is the compare of the pinned commit with the new one; nil
	// until asked.
	changed *changedFiles
	// ci is the verdict on the commit's other checks; nil until asked.
	ci *github.CIVerdict
}

// changedFiles is what changed since the pinned commit.
type changedFiles struct {
	files []string
	// untrusted says why the list cannot be trusted ("" when it can): the
	// commit then deploys, and its release note says so.
	untrusted string
}

// githubAsk is what decideSource needs from GitHub before it decides sha:
// the files changed, or the CI verdict counted from since.
type githubAsk struct {
	sha     string
	changed bool
	ci      bool
	since   time.Time
}

func (a githubAsk) any() bool { return a.changed || a.ci }

// heldRecheck is how often a held commit's checks are read when no
// webhook says they changed: the net under missed deliveries, two reads.
const heldRecheck = 30 * time.Second

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
// now: not deployed or held (the record says why), or GitHub must be asked
// first (u.ask says what).
func gateCommit(app *shpyrdv1.App, u *sourceUpdate, rec gitauth.SourceRecord, sha string, ev sourceEvent, now time.Time) (string, bool) {
	if !hasGates(app) {
		return "", false
	}
	git := app.Spec.Source.Git
	g := ev.gates
	// lost: the App does not cover the repository. Its settings do
	// nothing then, but a commit the record waits on stays as it is: held
	// (given up at the hour), or CI failed. It is never deployed unchecked.
	lost := g.known && !g.covered && g.unknown == ""
	if lost && (!rec.WaitsOnCI(sha) || !git.WaitForChecks) {
		return "", false
	}
	if lost && rec.Held != sha {
		// CI failed, its re-run unreadable: still not deployed, as recorded.
		return "", true
	}
	note := ""
	// Checked on every decision, a held commit's again too: the pin may
	// have moved, and the note goes with the deploy.
	if len(git.WatchPaths) > 0 && !lost {
		c := g.changed
		switch {
		case g.unknown != "":
			note = " (watch paths not checked: " + g.unknown + ")"
		case c == nil:
			// The first question for GitHub; gather asks cover with it.
			u.ask = githubAsk{sha: sha, changed: true}
			return "", true
		case c.untrusted != "":
			note = " (watch paths not checked: " + c.untrusted + ")"
		case !gitauth.MatchWatchPaths(git.WatchPaths, c.files):
			notDeploy(u, rec, sha, gitauth.ReasonNoWatchedChanges)
			return "", true
		}
	}
	if !git.WaitForChecks {
		return note, false
	}
	since := heldSince(rec, sha, ev, u.record, now)
	v := g.ci
	switch {
	case g.unknown != "" || lost:
		// Whether the App covers the repository is not known, or it no
		// longer does: never an unverified deploy; the hour still counts.
		uv := github.Unanswered(since, now)
		v = &uv
	case v == nil:
		u.ask = githubAsk{sha: sha, ci: true, since: since}
		return "", true
	}
	switch v.State {
	case github.CIPassed:
		return note + " (CI passed)", false
	case github.CINone:
		return note + " (no CI)", false
	case github.CIFailed:
		notDeploy(u, rec, sha, gitauth.ReasonCIFailedPrefix+v.Failing)
		return "", true
	case github.CITimedOut:
		reason := gitauth.ReasonCITimeout
		if lost {
			reason = gitauth.ReasonCINotCovered
		}
		notDeploy(u, rec, sha, reason)
		return "", true
	}
	// Pending: held, nothing pinned; a newer commit replaces it at once.
	u.record.Held, u.record.HeldSince = sha, &since
	if u.record.NotDeployed == sha {
		u.record.NotDeployed, u.record.NotDeployedReason = "", ""
	}
	return "", true
}

// heldSince is when a commit's wait for CI started: the hold already
// running; the nudge for a re-run of a commit CI failed, or auto-deploy
// switched back on; else when the commit was first seen, so a push's own
// time counts.
func heldSince(rec gitauth.SourceRecord, sha string, ev sourceEvent, next gitauth.SourceRecord, now time.Time) time.Time {
	switch {
	case rec.Held == sha && rec.HeldSince != nil:
		return *rec.HeldSince
	case ev.recheck, ev.resume:
		return now
	case next.Latest == sha && next.LatestAt != nil:
		return *next.LatestAt
	}
	return now
}

// notDeploy records sha as not deployed, for reason; a decision not
// recorded before is audited (git.skip, the reason as its detail).
func notDeploy(u *sourceUpdate, rec gitauth.SourceRecord, sha, reason string) {
	u.record.NotDeployed, u.record.NotDeployedReason = sha, reason
	if rec.NotDeployed != sha || rec.NotDeployedReason != reason {
		u.audit, u.target, u.reason = "git.skip", sha, reason
	}
}

// gather asks GitHub, through the App, what decideSource asked for; first
// whether the App covers the repository at all.
func (r *SourceReconciler) gather(ctx context.Context, app *shpyrdv1.App, ev *sourceEvent, ask githubAsk, now time.Time) {
	if ask.ci {
		// Read, or found unreadable: either way not again before
		// heldRecheck, unless a nudge says the checks changed.
		defer r.noteCI(app.UID, now)
	}
	g := &ev.gates
	if !g.known {
		ref, ok, err := r.GitHub.cover(ctx, app)
		g.known, g.covered, g.ref = true, ok, ref
		if err != nil {
			g.unknown = coverFailure(err)
		}
	}
	if !g.covered {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, githubGateTimeout)
	defer cancel()
	if ask.changed {
		c := r.changedFiles(ctx, g.ref, app.PinnedCommit(), ask.sha)
		g.changed = &c
	}
	if ask.ci {
		v, read := r.ciVerdict(ctx, app.UID, g.ref, ask.sha, ask.since, now)
		g.ci = &v
		if read {
			// A nudge for the commit is used up; one whose checks could
			// not be read is kept for the next pass.
			ev.ciRead = ask.sha
		}
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
		return changedFiles{untrusted: "300 or more files changed"}
	case cmp.Status == "diverged" || cmp.Status == "behind":
		return changedFiles{untrusted: "the branch's history was rewritten"}
	}
	return changedFiles{files: cmp.Files}
}

// ciVerdict reads the commit's check runs and statuses. GitHub not
// answering (a listing cut short, a permission not granted, an outage)
// keeps the commit held, and the hour still counts: an empty answer is
// never taken for "no CI". Like changedFiles, a failure here is the
// watcher's own: the clone's backoff is read, never written.
//
// read says GitHub answered.
func (r *SourceReconciler) ciVerdict(ctx context.Context, uid types.UID, ref githubRepoRef, sha string, since, now time.Time) (_ github.CIVerdict, read bool) {
	if r.GitHub.backingOff(ref.installation) {
		return github.Unanswered(since, now), false
	}
	runs, err := ref.client.CheckRuns(ctx, ref.installation, ref.owner, ref.name, sha)
	if err == nil {
		var statuses []github.CommitStatus
		if statuses, err = ref.client.CommitStatuses(ctx, ref.installation, ref.owner, ref.name, sha); err == nil {
			return github.Verdict(runs, statuses, ref.client.App().ID, since, now), true
		}
	}
	if r.logCI(uid, now) {
		log.FromContext(ctx).Info("could not read the commit's checks on GitHub; it stays held", "commit", shortCommit(sha), "err", err.Error())
	}
	return github.Unanswered(since, now), false
}

// ciLogEvery: GitHub unreadable is logged at most this often per App, not
// on each read of a held commit.
const ciLogEvery = time.Minute

// logCI says a failed read of the App's checks is logged now.
func (r *SourceReconciler) logCI(uid types.UID, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if last, ok := r.ciLogged[uid]; ok && now.Sub(last) < ciLogEvery {
		return false
	}
	if r.ciLogged == nil {
		r.ciLogged = map[types.UID]time.Time{}
	}
	r.ciLogged[uid] = now
	return true
}
