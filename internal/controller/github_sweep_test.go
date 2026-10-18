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
	resent := map[string]time.Time{"resent": now.Add(-time.Hour)}
	if got := fmt.Sprint(deliveryIDs(redeliveries(list, resent, now))); got != "[12 5 1 6 7]" {
		t.Errorf("picked %s: the oldest first, the first attempt of each, none delivered, none resent today", got)
	}
	resent["resent"] = now.Add(-25 * time.Hour)
	if got := fmt.Sprint(deliveryIDs(redeliveries(list, resent, now))); got != "[12 5 10 1 6 7]" {
		t.Errorf("a resend over a day ago no longer counts: %s", got)
	}
	var many []github.HookDelivery
	for i := 0; i < 60; i++ {
		many = append(many, github.HookDelivery{ID: int64(100 + i), GUID: fmt.Sprintf("g-%d", i), Event: "push", StatusCode: 0, DeliveredAt: ago(time.Duration(60-i) * time.Minute)})
	}
	got := redeliveries(many, map[string]time.Time{}, now)
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
