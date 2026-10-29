package controller

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/shpyrd-io/shpyrd/pkg/github"
)

// The catch-up sweep (RFC-0054, webhooks before polling). A repository the
// GitHub App covers is never listed on a timer, so a webhook GitHub failed
// to deliver (shpyrd down, a network fault) would be lost. Every 5 minutes,
// and at start, on the leader, the sweep reads the App's delivery attempts
// since the last sweep (a day back at start; GitHub keeps three) and asks
// GitHub to deliver again each push, check_suite, status and installation
// event that never reached shpyrd. One or two calls a sweep for the whole
// cluster, however many projects. The webhook ignores a delivery it took
// already, and the watcher one that does not follow. A delivery GitHub
// never made is not caught: a push or a Redeploy fixes it.

const (
	githubSweepEvery   = 5 * time.Minute
	githubSweepStart   = 24 * time.Hour
	githubSweepOverlap = time.Minute
	// maxRedeliveries a sweep, the oldest first.
	maxRedeliveries = 50
	// redeliveredFor: how long a resend is remembered.
	redeliveredFor = 24 * time.Hour
)

// GitHubSweep asks GitHub to deliver again what did not reach shpyrd.
type GitHubSweep struct {
	GitHub *GitHubApp
	// Every is the wait between sweeps; 0 is 5 minutes.
	Every time.Duration
	// Now is the clock; nil is time.Now.
	Now func() time.Time

	mu sync.Mutex
	// last is when the last sweep read the deliveries completely.
	last time.Time
	// floor is the first attempt of the oldest failure not settled: the
	// window starts there when it is before last.
	floor time.Time
	// resent is what was asked for again, by delivery (its GUID).
	resent map[string]resentRecord
}

// NeedLeaderElection makes the manager run the sweep on the leader only.
func (s *GitHubSweep) NeedLeaderElection() bool { return true }

// Start sweeps at once, then every Every, until ctx ends.
func (s *GitHubSweep) Start(ctx context.Context) error {
	every := s.Every
	if every <= 0 {
		every = githubSweepEvery
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if _, err := s.Sweep(ctx); err != nil {
			s.logger().Info("could not read the GitHub App's deliveries; the next sweep tries again", "err", err.Error())
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

// Sweep is one pass: how many deliveries it asked for again.
func (s *GitHubSweep) Sweep(ctx context.Context) (int, error) {
	if s == nil || s.GitHub == nil || s.GitHub.Source == nil {
		return 0, nil
	}
	cl, err := s.GitHub.Source.Client(ctx)
	if err == github.ErrNotConfigured { //nolint:errorlint // a bare ErrNotConfigured is no App at all
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	now := s.now()
	s.mu.Lock()
	since := now.Add(-githubSweepStart)
	if !s.last.IsZero() {
		since = s.last.Add(-githubSweepOverlap)
	}
	// A failure not settled yet is read again, however long ago the last
	// complete read was.
	if !s.floor.IsZero() && s.floor.Before(since) {
		since = s.floor
	}
	if lim := now.Add(-githubSweepStart); since.Before(lim) {
		since = lim
	}
	s.mu.Unlock()
	list, err := cl.HookDeliveries(ctx, since)
	// A list cut short is used as far as it goes.
	truncated := errors.Is(err, github.ErrTruncated)
	if err != nil && !truncated {
		return 0, err
	}
	s.mu.Lock()
	if s.resent == nil {
		s.resent = map[string]resentRecord{}
	}
	plan := planRedeliveries(list, s.resent, now)
	s.mu.Unlock()
	if truncated {
		s.logger().Info("GitHub has more deliveries than a sweep reads; the newest are used and the older are left",
			"read", len(list), "toResend", len(plan.picks), "backlog", plan.backlog)
	}
	sent, failed := 0, plan.backlog
	for _, p := range plan.picks {
		if err := cl.Redeliver(ctx, p.ID); err != nil {
			failed = true
			s.logger().Info("GitHub did not deliver again; the next sweep tries", "delivery", p.ID, "event", p.Event, "err", err.Error())
			var apiErr *github.APIError
			if errors.As(err, &apiErr) && apiErr.RateLimited {
				break
			}
			continue
		}
		s.mu.Lock()
		rec := s.resent[p.GUID]
		if now.Sub(rec.at) >= redeliveredFor {
			rec = resentRecord{}
		}
		s.resent[p.GUID] = resentRecord{at: now, count: rec.count + 1, after: p.latest}
		s.mu.Unlock()
		sent++
	}
	s.mu.Lock()
	// A failed resend, or a backlog past one sweep's share, keeps the
	// window: the next sweep reads it again. A truncated read is taken as
	// read, or every sweep would crawl the same pages.
	if !failed || truncated {
		s.last = now
	}
	s.floor = plan.oldest
	for guid, rec := range s.resent {
		if now.Sub(rec.at) >= redeliveredFor {
			delete(s.resent, guid)
		}
	}
	s.mu.Unlock()
	if sent > 0 {
		s.logger().Info("asked GitHub to deliver again what did not reach shpyrd", "deliveries", sent)
	}
	return sent, nil
}

// resentRecord is a delivery asked for again: when, how many times in the
// day, and the newest attempt in the log then. The resend is pending until
// a later attempt of the GUID shows in the log.
type resentRecord struct {
	at    time.Time
	count int
	after github.HookDelivery
}

// A resend that failed is tried again once its attempt is over resendAfter
// old, at most maxResends times in redeliveredFor.
const (
	resendAfter = 5 * time.Minute
	maxResends  = 5
)

type redeliveryPick struct {
	github.HookDelivery
	// latest is the newest attempt of the GUID in the log.
	latest github.HookDelivery
	// first is when its first attempt was made.
	first time.Time
}

type sweepPlan struct {
	// picks are asked for again, the oldest first, at most maxRedeliveries.
	picks []redeliveryPick
	// oldest is the first attempt of the oldest failure not settled (zero
	// when none): the next window starts there.
	oldest time.Time
	// backlog: more were due than one sweep sends.
	backlog bool
}

// planRedeliveries picks what a sweep asks for again: by delivery (its
// GUID), one shpyrd needs (push, check_suite, status, installation*) that no
// attempt delivered (no answer, or 4xx and 5xx), and that is not waiting on
// a resend (resent, no later attempt in the log yet) nor given up (resent
// maxResends times in the day). A resend whose own attempt failed, and is
// over resendAfter old, is sent again. The first attempt is asked for the
// first time, the latest after.
func planRedeliveries(list []github.HookDelivery, resent map[string]resentRecord, now time.Time) sweepPlan {
	type attempts struct {
		first, latest github.HookDelivery
		delivered     bool
	}
	by := map[string]*attempts{}
	for _, d := range list {
		if !sweptEvent(d.Event) {
			continue
		}
		a := by[d.GUID]
		if a == nil {
			a = &attempts{first: d, latest: d}
			by[d.GUID] = a
		}
		if d.DeliveredAt.Before(a.first.DeliveredAt) {
			a.first = d
		}
		if later(d, a.latest) {
			a.latest = d
		}
		a.delivered = a.delivered || d.Delivered()
	}
	var plan sweepPlan
	var due []redeliveryPick
	for guid, a := range by {
		if a.delivered {
			continue
		}
		pick := redeliveryPick{HookDelivery: a.first, latest: a.latest, first: a.first.DeliveredAt}
		if rec, ok := resent[guid]; ok && now.Sub(rec.at) < redeliveredFor {
			if rec.count >= maxResends {
				continue
			}
			if !later(a.latest, rec.after) || now.Sub(a.latest.DeliveredAt) <= resendAfter {
				// Pending, or too fresh to call failed: not due, not settled.
				plan.note(pick.first)
				continue
			}
			pick.HookDelivery = a.latest
		}
		plan.note(pick.first)
		due = append(due, pick)
	}
	sort.Slice(due, func(i, j int) bool {
		if !due[i].first.Equal(due[j].first) {
			return due[i].first.Before(due[j].first)
		}
		return due[i].ID < due[j].ID
	})
	if len(due) > maxRedeliveries {
		due, plan.backlog = due[:maxRedeliveries], true
	}
	plan.picks = due
	return plan
}

func (p *sweepPlan) note(first time.Time) {
	if p.oldest.IsZero() || first.Before(p.oldest) {
		p.oldest = first
	}
}

// later says attempt a came after b.
func later(a, b github.HookDelivery) bool {
	if !a.DeliveredAt.Equal(b.DeliveredAt) {
		return a.DeliveredAt.After(b.DeliveredAt)
	}
	return a.ID > b.ID
}

func sweptEvent(event string) bool {
	return event == "push" || event == "check_suite" || event == "status" || strings.HasPrefix(event, "installation")
}

func (s *GitHubSweep) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *GitHubSweep) logger() logr.Logger { return log.Log.WithName("github-sweep") }
