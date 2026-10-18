package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
)

// Listings of repositories the GitHub App does not cover (RFC-0054,
// webhooks before polling): one listing answers every project on a
// repository read with the same credential, for the interval; and the
// cluster lists at most SHPYRD_GIT_POLL_RATE repositories a second, from a
// token bucket. A read a person caused (a new project, a deploy, a push
// that does not follow) lists fresh and may borrow a burst ahead.

// DefaultPollRate is how many listings a second the cluster makes at most
// (SHPYRD_GIT_POLL_RATE).
const DefaultPollRate = 5.0

// PollRate reads SHPYRD_GIT_POLL_RATE: listings a second; empty,
// unreadable or not above 0 is the default.
func PollRate(v string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || f <= 0 || math.IsNaN(f) || math.IsInf(f, 0) {
		return DefaultPollRate
	}
	return f
}

// pollBucket is the cluster-wide cap: rate listings a second, a burst of
// as many (at least one). A listing over the cap is told how long to
// wait: its place in the queue, so a crowd spreads over time.
type pollBucket struct {
	mu     sync.Mutex
	rate   float64
	tokens float64
	queued float64
	last   time.Time
}

// take spends a listing now, or says how long until this one's turn. asked
// is a read a person caused: it may borrow up to a burst ahead.
func (b *pollBucket) take(now time.Time, asked bool) (time.Duration, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	burst := math.Max(1, b.rate)
	if b.last.IsZero() {
		b.tokens, b.last = burst, now
	}
	if now.After(b.last) {
		elapsed := now.Sub(b.last).Seconds()
		b.tokens = math.Min(burst, b.tokens+elapsed*b.rate)
		b.queued = math.Max(0, b.queued-elapsed*b.rate)
		b.last = now
	}
	floor := 1.0
	if asked {
		floor = 1 - burst
	}
	if b.tokens >= floor {
		b.tokens--
		return 0, true
	}
	b.queued++
	wait := time.Duration((floor - b.tokens + b.queued - 1) / b.rate * float64(time.Second))
	return max(wait, time.Millisecond), false
}

// pollDeferred is a listing over the cap: the App's turn comes after wait.
type pollDeferred struct{ wait time.Duration }

func (e *pollDeferred) Error() string {
	return fmt.Sprintf("over the cluster's cap of listings: the turn comes in %v", e.wait)
}

// repoListing is what one listing of a repository found, and when.
type repoListing struct {
	refs map[string]string
	at   time.Time
}

// listingKey is a repository as one listing answers it: its host and path
// in lower case, and the credential it is read with (its kind, and a
// fingerprint of the Secret's data, never the data). Projects with the
// same key share a listing.
func listingKey(cloneURL string, b gitauth.Build) string {
	where := strings.ToLower(strings.TrimSpace(cloneURL))
	if repo, err := gitauth.Parse(cloneURL); err == nil {
		where = strings.ToLower(repo.Host + "/" + repo.Path)
	}
	cred := b.Kind
	if b.Secret != nil {
		if b.Kind == gitauth.KindGitHubApp {
			cred += ":" + strings.ToLower(b.Secret.Annotations[gitauth.AnnotationGitHubAccount])
		} else {
			cred += ":" + secretFingerprint(b.Secret)
		}
	}
	return where + "|" + cred
}

func secretFingerprint(sec *corev1.Secret) string {
	keys := make([]string, 0, len(sec.Data))
	for k := range sec.Data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte{0})
		h.Write(sec.Data[k])
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// bucket is the watcher's cap, made at first use.
func (r *SourceReconciler) bucket() *pollBucket {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.polls == nil {
		rate := r.PollRate
		if rate <= 0 {
			rate = DefaultPollRate
		}
		r.polls = &pollBucket{rate: rate}
	}
	return r.polls
}

// sharedListing is a listing of the key made less than 90 % of the
// interval ago.
func (r *SourceReconciler) sharedListing(key string, now time.Time) (repoListing, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	l, ok := r.listings[key]
	if !ok || now.Before(l.at) || now.Sub(l.at) >= r.interval()*9/10 {
		return repoListing{}, false
	}
	return l, true
}

// keepListing keeps a listing that answered, and drops those past the
// interval.
func (r *SourceReconciler) keepListing(key string, l repoListing) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.listings == nil {
		r.listings = map[string]repoListing{}
	}
	for k, old := range r.listings {
		if l.at.Sub(old.at) >= r.interval() {
			delete(r.listings, k)
		}
	}
	r.listings[key] = l
}
