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
	// redeliveredFor: a delivery resent is not resent again for a day.
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
	// resent is when each delivery (its GUID) was resent.
	resent map[string]time.Time
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
	s.mu.Unlock()
	list, err := cl.HookDeliveries(ctx, since)
	// A list cut short is used as far as it goes, and the window is kept.
	truncated := errors.Is(err, github.ErrTruncated)
	if err != nil && !truncated {
		return 0, err
	}
	s.mu.Lock()
	if s.resent == nil {
		s.resent = map[string]time.Time{}
	}
	picks := redeliveries(list, s.resent, now)
	s.mu.Unlock()
	sent, failed := 0, truncated
	for _, d := range picks {
		if err := cl.Redeliver(ctx, d.ID); err != nil {
			failed = true
			s.logger().Info("GitHub did not deliver again; the next sweep tries", "delivery", d.ID, "event", d.Event, "err", err.Error())
			var apiErr *github.APIError
			if errors.As(err, &apiErr) && apiErr.RateLimited {
				break
			}
			continue
		}
		s.mu.Lock()
		s.resent[d.GUID] = now
		s.mu.Unlock()
		sent++
	}
	s.mu.Lock()
	// A failed resend keeps the window: the next sweep reads it again.
	if !failed {
		s.last = now
	}
	for guid, at := range s.resent {
		if now.Sub(at) >= redeliveredFor {
			delete(s.resent, guid)
		}
	}
	s.mu.Unlock()
	if sent > 0 {
		s.logger().Info("asked GitHub to deliver again what did not reach shpyrd", "deliveries", sent)
	}
	return sent, nil
}

// redeliveries picks what a sweep asks for again: by delivery (its GUID),
// one shpyrd needs (push, check_suite, status, installation*) whose every
// attempt failed (no answer, or 4xx and 5xx), not resent in the last day;
// its first attempt, the oldest first, at most maxRedeliveries.
func redeliveries(list []github.HookDelivery, resent map[string]time.Time, now time.Time) []github.HookDelivery {
	type attempts struct {
		first     github.HookDelivery
		delivered bool
	}
	by := map[string]*attempts{}
	for _, d := range list {
		if !sweptEvent(d.Event) {
			continue
		}
		a := by[d.GUID]
		if a == nil {
			a = &attempts{first: d}
			by[d.GUID] = a
		}
		if d.DeliveredAt.Before(a.first.DeliveredAt) {
			a.first = d
		}
		a.delivered = a.delivered || d.Delivered()
	}
	var out []github.HookDelivery
	for guid, a := range by {
		if at, ok := resent[guid]; a.delivered || (ok && now.Sub(at) < redeliveredFor) {
			continue
		}
		out = append(out, a.first)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].DeliveredAt.Equal(out[j].DeliveredAt) {
			return out[i].DeliveredAt.Before(out[j].DeliveredAt)
		}
		return out[i].ID < out[j].ID
	})
	if len(out) > maxRedeliveries {
		out = out[:maxRedeliveries]
	}
	return out
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
