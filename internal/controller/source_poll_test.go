package controller

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
)

func TestTheBucketCapsListingsAndLetsAPersonsReadThrough(t *testing.T) {
	b := &pollBucket{rate: 5}
	now := sourceNow
	for i := 0; i < 5; i++ {
		if _, ok := b.take(now, false); !ok {
			t.Fatalf("take %d of the burst", i)
		}
	}
	w1, ok1 := b.take(now, false)
	w2, ok2 := b.take(now, false)
	if ok1 || ok2 || w1 != 200*time.Millisecond || w2 != 400*time.Millisecond {
		t.Errorf("over the cap: %v %v, %v %v", ok1, w1, ok2, w2)
	}
	// A read a person caused borrows up to a burst ahead.
	for i := 0; i < 5; i++ {
		if _, ok := b.take(now, true); !ok {
			t.Fatalf("a person's read %d", i)
		}
	}
	if _, ok := b.take(now, true); ok {
		t.Error("borrowed past a burst")
	}
	// Three seconds later: the debt is paid, and the queue has moved.
	if _, ok := b.take(now.Add(3*time.Second), false); !ok {
		t.Error("refilled")
	}
}

func TestPollRateIsReadFromItsVariable(t *testing.T) {
	for in, want := range map[string]float64{"": 5, "2": 2, "0.5": 0.5, "0": 5, "-1": 5, "fast": 5, "NaN": 5} {
		if got := PollRate(in); got != want {
			t.Errorf("PollRate(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestAListingIsSharedByRepositoryAndCredential(t *testing.T) {
	key := func(url string, b gitauth.Build) string { return listingKey(url, b) }
	plain := gitauth.Build{}
	if key("https://gitlab.com/Acme/Mono", plain) != key("https://gitlab.com/acme/mono.git", plain) {
		t.Error("one repository in two forms is two listings")
	}
	if key("https://gitlab.com/acme/mono", plain) == key("https://gitlab.com/acme/other", plain) {
		t.Error("two repositories share a listing")
	}
	keyA := gitauth.Build{Kind: gitauth.KindDeployKey, Secret: &corev1.Secret{Data: map[string][]byte{"ssh-privatekey": []byte("a")}}}
	keyB := gitauth.Build{Kind: gitauth.KindDeployKey, Secret: &corev1.Secret{Data: map[string][]byte{"ssh-privatekey": []byte("b")}}}
	if key("git@gitlab.com:acme/mono.git", keyA) == key("git@gitlab.com:acme/mono.git", keyB) {
		t.Error("two deploy keys share a listing")
	}
	if k := key("git@gitlab.com:acme/mono.git", keyA); strings.Contains(k, "ssh-privatekey") || strings.HasSuffix(k, ":a") {
		t.Errorf("the key shows the credential: %q", k)
	}
}

func TestARepositoryIsListedOnceForItsProjectsThenNotAfterItsWebhook(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": otherCommit}}
	names := []string{"api", "web", "worker"}
	var objs []client.Object
	for _, name := range names {
		objs = append(objs, withRecord(pinGit(gitApp(name, "https://gitlab.com/acme/mono"), testCommit),
			gitauth.SourceRecord{Ref: "main", Latest: testCommit, CheckedAt: at(sourceNow.Add(-10 * time.Minute))}))
	}
	r, c := newSourceReconciler(t, ls, objs...)
	clock := sourceNow
	r.Now = func() time.Time { return clock }
	for _, o := range objs {
		if _, got := reconcileSource(t, r, o.(*shpyrdv1.App)); got.PinnedCommit() != otherCommit {
			t.Fatalf("%s: pin %s", o.GetName(), got.PinnedCommit())
		}
	}
	if ls.calls != 1 {
		t.Fatalf("three projects on one repository: %d listings", ls.calls)
	}
	// Each project's own webhook delivers once: none is listed again.
	const newest = "fedcba9876543210fedcba9876543210fedcba98"
	for i, o := range objs {
		got := &shpyrdv1.App{}
		if err := c.Get(t.Context(), client.ObjectKeyFromObject(o), got); err != nil {
			t.Fatal(err)
		}
		withPush(got, gitauth.Push{Ref: "refs/heads/main", SHA: newest, Before: otherCommit, Author: "alice", Delivery: fmt.Sprintf("d-%d", i), At: clock})
		if err := c.Update(t.Context(), got); err != nil {
			t.Fatal(err)
		}
		if _, got = reconcileSource(t, r, got); got.PinnedCommit() != newest || !sourceRecord(got).ProjectHook {
			t.Fatalf("%s: pin %s, record %+v", o.GetName(), got.PinnedCommit(), sourceRecord(got))
		}
	}
	for _, d := range []time.Duration{10 * time.Minute, time.Hour, 24 * time.Hour} {
		clock = sourceNow.Add(d)
		for _, o := range objs {
			if res, _ := reconcileSource(t, r, o.(*shpyrdv1.App)); res.RequeueAfter != pushDrivenRecheck {
				t.Errorf("%s after %v: requeue %v", o.GetName(), d, res.RequeueAfter)
			}
		}
	}
	if ls.calls != 1 {
		t.Errorf("listed after the webhooks delivered: %d listings", ls.calls)
	}
}

// Review Focus 6.
func TestAThousandProjectsStayUnderTheCap(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": testCommit}}
	var objs []client.Object
	for i := 0; i < 1000; i++ {
		objs = append(objs, withRecord(pinGit(gitApp(fmt.Sprintf("p%d", i), fmt.Sprintf("https://gitlab.com/acme/p%d", i)), testCommit),
			gitauth.SourceRecord{Ref: "main", Latest: testCommit, CheckedAt: at(sourceNow.Add(-time.Hour))}))
	}
	r, _ := newSourceReconciler(t, ls, objs...)
	r.PollRate = 5
	clock := sourceNow
	r.Now = func() time.Time { return clock }
	next := map[string]time.Time{}
	for _, o := range objs {
		next[o.GetName()] = sourceNow
	}
	for s := 0; s <= 240; s++ {
		clock = sourceNow.Add(time.Duration(s) * time.Second)
		before := ls.calls
		for _, o := range objs {
			when, ok := next[o.GetName()]
			if !ok || when.After(clock) {
				continue
			}
			res, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(o)})
			if err != nil {
				t.Fatal(err)
			}
			next[o.GetName()] = clock.Add(res.RequeueAfter)
		}
		if n := ls.calls - before; n > 5 {
			t.Fatalf("second %d: %d listings, over the cap of 5", s, n)
		}
	}
	if ls.calls != 1000 {
		t.Errorf("%d listings in 240 s: each of the thousand once, within 1000 / 5 = 200 s", ls.calls)
	}
}

// P12: a webhook that delivered before the watch mode was stored.
func TestAProjectWhoseWebhookDeliveredBeforeThisChangeIsNotPolled(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": otherCommit}}
	delivered := sourceNow.Add(-2 * time.Hour)
	obj := pinGit(gitApp("old", "https://gitlab.com/acme/old"), testCommit)
	// A record from before the watch mode was stored: no mode in it.
	raw, _ := json.Marshal(gitauth.SourceRecord{Ref: "main", Latest: testCommit, CheckedAt: at(sourceNow.Add(-time.Hour)), LastWebhookAt: &delivered})
	obj.Annotations[shpyrdv1.AnnotationGitSource] = string(raw)
	r, _ := newSourceReconciler(t, ls, obj)
	clock := sourceNow
	r.Now = func() time.Time { return clock }
	app := obj
	for _, d := range []time.Duration{0, 10 * time.Minute, 24 * time.Hour} {
		clock = sourceNow.Add(d)
		if res, got := reconcileSource(t, r, app); res.RequeueAfter != pushDrivenRecheck || got.PinnedCommit() != testCommit {
			t.Errorf("after %v: requeue %v, pin %s", d, res.RequeueAfter, got.PinnedCommit())
		}
	}
	if ls.calls != 0 {
		t.Errorf("listed %d times", ls.calls)
	}
	if got := WatchMode(false, sourceRecord(app)); got != WatchWebhook {
		t.Errorf("mode %q", got)
	}
}

func TestARecordOfTheAppsPushIsPolledOnceTheAppStopsCoveringIt(t *testing.T) {
	delivered := sourceNow.Add(-2 * time.Hour)
	rec := gitauth.SourceRecord{Ref: "main", Watch: WatchGitHubApp, LastWebhookAt: &delivered}
	if got := WatchMode(false, rec); got != WatchPoll {
		t.Errorf("mode %q", got)
	}
}

func TestAPersonsReadBorrowsFromTheBurstAndTheTimerNeverExceedsTheRate(t *testing.T) {
	b := &pollBucket{rate: 5}
	now := sourceNow
	for i := 0; i < 5; i++ {
		b.take(now, true)
	}
	// The person's reads used the burst: a timer's check waits.
	if w, ok := b.take(now, false); ok || w <= 0 {
		t.Errorf("timer after borrowing: %v %v", ok, w)
	}
	// Over 100 s the timer is let through at most 5 a second.
	ok := 0
	for s := 0; s < 100; s++ {
		for i := 0; i < 20; i++ {
			if _, got := b.take(now.Add(time.Duration(s)*time.Second), false); got {
				ok++
			}
		}
	}
	if ok > 5*100 {
		t.Errorf("%d timer listings in 100 s", ok)
	}
}
