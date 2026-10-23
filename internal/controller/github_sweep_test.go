package controller

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/shpyrd-io/shpyrd/pkg/github"
	"github.com/shpyrd-io/shpyrd/pkg/github/githubtest"
)

func deliveryIDs(list []github.HookDelivery) []int64 {
	var out []int64
	for _, d := range list {
		out = append(out, d.ID)
	}
	return out
}

func planned(list []github.HookDelivery, resent map[string]resentRecord, now time.Time) []github.HookDelivery {
	var out []github.HookDelivery
	for _, p := range planRedeliveries(list, resent, now).picks {
		out = append(out, p.HookDelivery)
	}
	return out
}

func TestTheSweepPicksFailedDeliveriesOnce(t *testing.T) {
	now := sourceNow
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	list := []github.HookDelivery{
		{ID: 1, GUID: "push-failed", Event: "push", StatusCode: 502, DeliveredAt: ago(4 * time.Minute)},
		{ID: 2, GUID: "push-ok", Event: "push", StatusCode: 202, DeliveredAt: ago(3 * time.Minute)},
		{ID: 3, GUID: "push-later-ok", Event: "push", StatusCode: 0, DeliveredAt: ago(10 * time.Minute)},
		{ID: 4, GUID: "push-later-ok", Event: "push", StatusCode: 202, Redelivery: true, DeliveredAt: ago(2 * time.Minute)},
		{ID: 5, GUID: "ci-failed", Event: "check_suite", StatusCode: 404, DeliveredAt: ago(6 * time.Minute)},
		{ID: 6, GUID: "status-failed", Event: "status", StatusCode: 500, DeliveredAt: ago(time.Minute)},
		{ID: 7, GUID: "install-failed", Event: "installation_repositories", StatusCode: 503, DeliveredAt: ago(30 * time.Second)},
		{ID: 8, GUID: "ping-failed", Event: "ping", StatusCode: 500, DeliveredAt: ago(time.Minute)},
		{ID: 9, GUID: "pr-failed", Event: "pull_request", StatusCode: 500, DeliveredAt: ago(time.Minute)},
		{ID: 10, GUID: "resent", Event: "push", StatusCode: 500, DeliveredAt: ago(5 * time.Minute)},
		{ID: 11, GUID: "redirected", Event: "push", StatusCode: 302, DeliveredAt: ago(time.Minute)},
		{ID: 12, GUID: "failed-twice", Event: "push", StatusCode: 502, DeliveredAt: ago(8 * time.Minute)},
		{ID: 13, GUID: "failed-twice", Event: "push", StatusCode: 502, Redelivery: true, DeliveredAt: ago(7 * time.Minute)},
	}
	resent := map[string]resentRecord{"resent": {at: now.Add(-time.Hour), after: list[9]}}
	if got := fmt.Sprint(deliveryIDs(planned(list, resent, now))); got != "[12 5 1 6 7]" {
		t.Errorf("picked %s: the oldest first, the first attempt of each, none delivered, none resent today", got)
	}
	resent["resent"] = resentRecord{at: now.Add(-25 * time.Hour), after: list[9]}
	if got := fmt.Sprint(deliveryIDs(planned(list, resent, now))); got != "[12 5 10 1 6 7]" {
		t.Errorf("a resend over a day ago no longer counts: %s", got)
	}
	var many []github.HookDelivery
	for i := 0; i < 60; i++ {
		many = append(many, github.HookDelivery{ID: int64(100 + i), GUID: fmt.Sprintf("g-%d", i), Event: "push", StatusCode: 0, DeliveredAt: ago(time.Duration(60-i) * time.Minute)})
	}
	got := planned(many, map[string]resentRecord{}, now)
	if len(got) != maxRedeliveries || got[0].ID != 100 || got[len(got)-1].ID != 149 {
		t.Errorf("%d picked, from %d to %d", len(got), got[0].ID, got[len(got)-1].ID)
	}
}

func TestTheSweepAsksGitHubToDeliverAgainWhatFailed(t *testing.T) {
	fake, st := connectedGitHub(t)
	_, c := newTestReconciler(t, fake.Secret("shpyrd-system"))
	sweep := &GitHubSweep{GitHub: &GitHubApp{Source: fake.Source(c, "shpyrd-system"), Installations: st}, Now: fake.Time}
	ctx := context.Background()
	failed := fake.AddDelivery(githubtest.Delivery{GUID: "g-push", Event: "push", StatusCode: 502, DeliveredAt: sourceNow.Add(-2 * time.Minute), InstallationID: 42})
	fake.AddDelivery(githubtest.Delivery{GUID: "g-ok", Event: "push", StatusCode: 202, DeliveredAt: sourceNow.Add(-time.Minute), InstallationID: 42})
	old := fake.AddDelivery(githubtest.Delivery{GUID: "g-day-old", Event: "push", StatusCode: 0, DeliveredAt: sourceNow.Add(-23 * time.Hour), InstallationID: 42})
	fake.AddDelivery(githubtest.Delivery{GUID: "g-too-old", Event: "push", StatusCode: 0, DeliveredAt: sourceNow.Add(-25 * time.Hour), InstallationID: 42})
	if n, err := sweep.Sweep(ctx); err != nil || n != 2 || fmt.Sprint(fake.Redelivered()) != fmt.Sprint([]int64{old, failed}) {
		t.Fatalf("first sweep: %d %v, redelivered %v", n, err, fake.Redelivered())
	}
	// The next sweep reads back to the last one: nothing failed since, and
	// what was resent is not sent again, even when its resend failed too.
	fake.SetRedeliveryStatus(502)
	advance(fake, 5*time.Minute)
	if n, err := sweep.Sweep(ctx); err != nil || n != 0 || len(fake.Redelivered()) != 2 {
		t.Errorf("second sweep: %d %v, redelivered %v", n, err, fake.Redelivered())
	}
	late := fake.AddDelivery(githubtest.Delivery{GUID: "g-late", Event: "status", StatusCode: 404, DeliveredAt: fake.Time().Add(-time.Minute), InstallationID: 42})
	advance(fake, 5*time.Minute)
	if n, _ := sweep.Sweep(ctx); n != 1 || fake.Redelivered()[2] != late {
		t.Errorf("third sweep: %d, redelivered %v", n, fake.Redelivered())
	}
	// GitHub down: an error, and the window is kept for the next sweep.
	fake.SetDown(true)
	if _, err := sweep.Sweep(ctx); err == nil {
		t.Error("GitHub down is no error")
	}
}

func TestTheSweepDoesNothingWithoutTheApp(t *testing.T) {
	_, c := newTestReconciler(t)
	sweep := &GitHubSweep{GitHub: &GitHubApp{Source: &github.Source{Reader: c, Namespace: "shpyrd-system"}}}
	if n, err := sweep.Sweep(context.Background()); n != 0 || err != nil {
		t.Errorf("%d %v", n, err)
	}
	if !sweep.NeedLeaderElection() {
		t.Error("the sweep runs on every replica")
	}
}

func newSweep(t *testing.T) (*githubtest.Server, *GitHubSweep) {
	t.Helper()
	fake, st := connectedGitHub(t)
	_, c := newTestReconciler(t, fake.Secret("shpyrd-system"))
	return fake, &GitHubSweep{GitHub: &GitHubApp{Source: fake.Source(c, "shpyrd-system"), Installations: st}, Now: fake.Time}
}

func TestTheSweepDrainsABacklogPastOneSweepsShare(t *testing.T) {
	fake, sweep := newSweep(t)
	ctx := context.Background()
	for i := 0; i < 120; i++ {
		fake.AddDelivery(githubtest.Delivery{GUID: fmt.Sprintf("g-%03d", i), Event: "push", StatusCode: 502,
			DeliveredAt: sourceNow.Add(-time.Duration(120-i) * time.Minute), InstallationID: 42})
	}
	for round, want := range []int{50, 50, 20, 0} {
		if n, err := sweep.Sweep(ctx); err != nil || n != want {
			t.Fatalf("sweep %d: %d %v, want %d", round+1, n, err, want)
		}
		advance(fake, time.Minute)
	}
	seen := map[int64]bool{}
	for _, id := range fake.Redelivered() {
		if seen[id] {
			t.Errorf("delivery %d was sent twice", id)
		}
		seen[id] = true
	}
	if len(seen) != 120 {
		t.Errorf("%d of 120 resent", len(seen))
	}
}

func TestTheSweepResendsARedeliveryThatFailedAgain(t *testing.T) {
	fake, sweep := newSweep(t)
	ctx := context.Background()
	fake.SetRedeliveryStatus(502)
	first := fake.AddDelivery(githubtest.Delivery{GUID: "g", Event: "push", StatusCode: 502, DeliveredAt: sourceNow.Add(-time.Minute), InstallationID: 42})
	if n, err := sweep.Sweep(ctx); err != nil || n != 1 {
		t.Fatalf("first: %d %v", n, err)
	}
	// The resend's attempt failed, but is too fresh to call it: no resend.
	advance(fake, 4*time.Minute)
	if n, _ := sweep.Sweep(ctx); n != 0 {
		t.Errorf("a fresh failed attempt was resent: %d", n)
	}
	// Over 5 minutes old: sent again, from the failed attempt.
	advance(fake, 2*time.Minute)
	if n, _ := sweep.Sweep(ctx); n != 1 || len(fake.Redelivered()) != 2 || fake.Redelivered()[0] != first || fake.Redelivered()[1] == first {
		t.Errorf("second resend: %d, %v", n, fake.Redelivered())
	}
	// This time the resend is taken: nothing more, ever.
	fake.SetRedeliveryStatus(202)
	advance(fake, 6*time.Minute)
	if n, _ := sweep.Sweep(ctx); n != 1 {
		t.Fatalf("third resend: %d", n)
	}
	for i := 0; i < 3; i++ {
		advance(fake, 6*time.Minute)
		if n, _ := sweep.Sweep(ctx); n != 0 {
			t.Errorf("sweep after success resent %d", n)
		}
	}
	if len(fake.Redelivered()) != 3 {
		t.Errorf("redelivered %v", fake.Redelivered())
	}
}

func TestTheSweepGivesUpOnADeliveryAfterFiveResends(t *testing.T) {
	fake, sweep := newSweep(t)
	ctx := context.Background()
	fake.SetRedeliveryStatus(502)
	fake.AddDelivery(githubtest.Delivery{GUID: "g", Event: "push", StatusCode: 502, DeliveredAt: sourceNow.Add(-time.Minute), InstallationID: 42})
	for i := 0; i < 10; i++ {
		_, _ = sweep.Sweep(ctx)
		advance(fake, 6*time.Minute)
	}
	if got := len(fake.Redelivered()); got != 5 {
		t.Errorf("%d resends, want 5", got)
	}
}

func TestATruncatedReadIsTakenAsReadOnce(t *testing.T) {
	fake, sweep := newSweep(t)
	ctx := context.Background()
	for i := 0; i < 100*100+50; i++ {
		fake.AddDelivery(githubtest.Delivery{GUID: fmt.Sprintf("ok-%d", i), Event: "push", StatusCode: 202,
			DeliveredAt: sourceNow.Add(-time.Hour + time.Duration(i)*time.Millisecond*100), InstallationID: 42})
	}
	if _, err := sweep.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if !sweep.last.Equal(fake.Time()) {
		t.Fatalf("last %v: a truncated read was not taken as read", sweep.last)
	}
	advance(fake, 5*time.Minute)
	late := fake.AddDelivery(githubtest.Delivery{GUID: "late", Event: "push", StatusCode: 500, DeliveredAt: fake.Time().Add(-time.Minute), InstallationID: 42})
	if n, err := sweep.Sweep(ctx); err != nil || n != 1 || fake.Redelivered()[0] != late {
		t.Errorf("next sweep: %d %v %v", n, err, fake.Redelivered())
	}
}
