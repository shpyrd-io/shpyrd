package github

import "time"

// The CI verdict (RFC-0054): what a commit's other checks and statuses on
// GitHub say, for wait for CI. Check suites are not read: the empty suite
// every installed App gets on every commit never holds one.

// CheckRun is a check run on a commit, as GitHub lists it.
type CheckRun struct {
	ID   int64
	Name string
	// Status is queued, in_progress, completed, waiting, requested or
	// pending; Conclusion is set once it is completed.
	Status     string
	Conclusion string
	// AppID is the App that made it.
	AppID int64
}

// CommitStatus is a commit status: a context and its state (pending,
// success, failure or error).
type CommitStatus struct {
	Context string
	State   string
}

// The states of a verdict.
const (
	CIPending  = "pending"
	CIPassed   = "passed"
	CIFailed   = "failed"
	CINone     = "none"
	CITimedOut = "timed-out"
)

const (
	// NoCIAfter: a commit with no check run and no status this long after
	// it was held has no CI, and deploys.
	NoCIAfter = 60 * time.Second
	// GiveUpAfter: a commit whose CI is still pending this long after it
	// was held is not deployed.
	GiveUpAfter = time.Hour
)

// CIVerdict is what a commit's CI says. Failing names the first check run
// or status that failed.
type CIVerdict struct {
	State   string
	Failing string
}

// Verdict judges a commit's check runs and statuses, leaving out the runs
// of the App appID, shpyrd's own. since is when the commit was held.
func Verdict(runs []CheckRun, statuses []CommitStatus, appID int64, since, now time.Time) CIVerdict {
	seen, done := 0, true
	for _, r := range runs {
		if appID != 0 && r.AppID == appID {
			continue
		}
		seen++
		if r.Status != "completed" {
			done = false
			continue
		}
		switch r.Conclusion {
		case "failure", "cancelled", "timed_out", "action_required", "startup_failure":
			return CIVerdict{State: CIFailed, Failing: r.Name}
		case "success", "neutral", "skipped":
		default:
			done = false
		}
	}
	for _, s := range statuses {
		seen++
		switch s.State {
		case "failure", "error":
			return CIVerdict{State: CIFailed, Failing: s.Context}
		case "success":
		default:
			done = false
		}
	}
	switch {
	case seen > 0 && done:
		return CIVerdict{State: CIPassed}
	case seen == 0 && now.Sub(since) >= NoCIAfter:
		return CIVerdict{State: CINone}
	}
	return Unanswered(since, now)
}

// Unanswered is the verdict while nothing decides it, GitHub not answering
// included: pending, or given up an hour after since.
func Unanswered(since, now time.Time) CIVerdict {
	if now.Sub(since) >= GiveUpAfter {
		return CIVerdict{State: CITimedOut}
	}
	return CIVerdict{State: CIPending}
}
