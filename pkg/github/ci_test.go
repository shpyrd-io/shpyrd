package github_test

import (
	"testing"
	"time"

	"github.com/shpyrd-io/shpyrd/pkg/github"
)

const ourApp = 1234

var held = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

func run(name, status, conclusion string) github.CheckRun {
	return github.CheckRun{Name: name, Status: status, Conclusion: conclusion, AppID: 99}
}

func TestEachConclusionOfACheckRun(t *testing.T) {
	now := held.Add(5 * time.Minute)
	for conclusion, want := range map[string]string{
		"success": github.CIPassed, "neutral": github.CIPassed, "skipped": github.CIPassed,
		"failure": github.CIFailed, "cancelled": github.CIFailed, "timed_out": github.CIFailed,
		"action_required": github.CIFailed, "startup_failure": github.CIFailed,
		"stale": github.CIPending,
	} {
		v := github.Verdict([]github.CheckRun{run("test", "completed", conclusion)}, nil, ourApp, held, now)
		if v.State != want {
			t.Errorf("%s: %+v", conclusion, v)
		}
		if want == github.CIFailed && v.Failing != "test" {
			t.Errorf("%s names %q", conclusion, v.Failing)
		}
	}
	for _, status := range []string{"queued", "in_progress", "waiting", "requested", "pending"} {
		if v := github.Verdict([]github.CheckRun{run("test", status, "")}, nil, ourApp, held, now); v.State != github.CIPending {
			t.Errorf("%s: %+v", status, v)
		}
	}
}

func TestAFailureIsFinalWhileOthersStillRun(t *testing.T) {
	runs := []github.CheckRun{run("lint", "in_progress", ""), run("test", "completed", "failure"), run("build", "completed", "failure")}
	if v := github.Verdict(runs, nil, ourApp, held, held.Add(time.Minute)); v.State != github.CIFailed || v.Failing != "test" {
		t.Errorf("the first failing check is named: %+v", v)
	}
}

func TestCommitStatusesCountToo(t *testing.T) {
	now := held.Add(time.Minute)
	ok := []github.CheckRun{run("test", "completed", "success")}
	if v := github.Verdict(ok, []github.CommitStatus{{Context: "ci/circle", State: "success"}}, ourApp, held, now); v.State != github.CIPassed {
		t.Errorf("both green: %+v", v)
	}
	if v := github.Verdict(ok, []github.CommitStatus{{Context: "ci/circle", State: "pending"}}, ourApp, held, now); v.State != github.CIPending {
		t.Errorf("a status pending: %+v", v)
	}
	for _, state := range []string{"failure", "error"} {
		if v := github.Verdict(ok, []github.CommitStatus{{Context: "ci/circle", State: state}}, ourApp, held, now); v.State != github.CIFailed || v.Failing != "ci/circle" {
			t.Errorf("a status %s: %+v", state, v)
		}
	}
	if v := github.Verdict(nil, []github.CommitStatus{{Context: "deploy/preview", State: "success"}}, ourApp, held, now); v.State != github.CIPassed {
		t.Errorf("statuses alone: %+v", v)
	}
}

func TestTheVerdictLeavesOutShpyrdsOwnRuns(t *testing.T) {
	own := github.CheckRun{Name: "shpyrd / shop", Status: "queued", AppID: ourApp}
	runs := []github.CheckRun{own, run("test", "completed", "success")}
	if v := github.Verdict(runs, nil, ourApp, held, held.Add(10*time.Second)); v.State != github.CIPassed {
		t.Errorf("shpyrd's queued run does not hold its own commit: %+v", v)
	}
	if v := github.Verdict([]github.CheckRun{own}, nil, ourApp, held, held.Add(time.Minute)); v.State != github.CINone {
		t.Errorf("only shpyrd's own run is no CI: %+v", v)
	}
}

func TestNoCIIsSaidAfterAMinuteWithNothingOnTheCommit(t *testing.T) {
	if v := github.Verdict(nil, nil, ourApp, held, held.Add(59*time.Second)); v.State != github.CIPending {
		t.Errorf("before a minute: %+v", v)
	}
	if v := github.Verdict(nil, nil, ourApp, held, held.Add(60*time.Second)); v.State != github.CINone {
		t.Errorf("after a minute: %+v", v)
	}
}

func TestCIStillPendingAfterAnHourIsGivenUp(t *testing.T) {
	runs := []github.CheckRun{run("test", "in_progress", "")}
	if v := github.Verdict(runs, nil, ourApp, held, held.Add(59*time.Minute)); v.State != github.CIPending {
		t.Errorf("59 minutes: %+v", v)
	}
	if v := github.Verdict(runs, nil, ourApp, held, held.Add(time.Hour)); v.State != github.CITimedOut {
		t.Errorf("an hour: %+v", v)
	}
	if v := github.Unanswered(held, held.Add(10*time.Minute)); v.State != github.CIPending {
		t.Errorf("GitHub not answering is pending, never no CI: %+v", v)
	}
	if v := github.Unanswered(held, held.Add(time.Hour)); v.State != github.CITimedOut {
		t.Errorf("not answering for an hour: %+v", v)
	}
}
