package controller

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/funcr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
)

const thirdCommit = "fedcba9876543210fedcba9876543210fedcba98"

// Switching auto-deploy back on deploys the commit that waited, from the
// record, without reading the branch again.
func TestSourceAutoDeployOnDeploysTheWaitingCommit(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": thirdCommit}}
	off := false
	app := withRecord(pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit),
		gitauth.SourceRecord{Ref: "main", Latest: otherCommit, Via: "push", Author: "alice", CheckedAt: at(sourceNow.Add(-30 * time.Second))})
	app.Spec.Source.Git.AutoDeploy = &off
	r, c := newSourceReconciler(t, ls, app)
	heard := heardAudits(t)
	_, got := reconcileSource(t, r, app)
	if got.PinnedCommit() != testCommit {
		t.Fatalf("auto-deploy off: pin %q", got.PinnedCommit())
	}
	on := true
	got.Spec.Source.Git.AutoDeploy = &on
	if err := c.Update(context.Background(), got); err != nil {
		t.Fatal(err)
	}
	_, got = reconcileSource(t, r, got)
	if got.PinnedCommit() != otherCommit || ls.calls != 0 {
		t.Fatalf("pin %q after %d listings", got.PinnedCommit(), ls.calls)
	}
	if note := got.Annotations[shpyrdv1.AnnotationReleaseNote]; note != "Commit 89abcde (auto-deploy on)" {
		t.Errorf("note %q", note)
	}
	if got.Annotations[shpyrdv1.AnnotationDeployedBy] != "git" || got.Annotations[shpyrdv1.AnnotationDeployedClient] != "auto-deploy" {
		t.Errorf("attribution %q via %q", got.Annotations[shpyrdv1.AnnotationDeployedBy], got.Annotations[shpyrdv1.AnnotationDeployedClient])
	}
	if len(*heard) != 1 || (*heard)[0].Action != "git.push" || (*heard)[0].Target != "89abcde" {
		t.Errorf("audit %+v", *heard)
	}
	// The pin made, the next pass has nothing to do.
	_, got = reconcileSource(t, r, got)
	if len(*heard) != 1 || ls.calls != 0 {
		t.Errorf("audits %d listings %d", len(*heard), ls.calls)
	}
	// A skipped commit, or one recorded with a failure since, waits.
	for name, rec := range map[string]gitauth.SourceRecord{
		"skipped": {Ref: "main", Latest: otherCommit, Skipped: otherCommit, CheckedAt: at(sourceNow.Add(-30 * time.Second))},
		"failing": {Ref: "main", Latest: otherCommit, Error: "x", Failures: 1, CheckedAt: at(sourceNow.Add(-5 * time.Second))},
	} {
		a := withRecord(pinGit(gitApp(name, "https://github.com/acme/shop"), testCommit), rec)
		r, _ := newSourceReconciler(t, ls, a)
		if _, got := reconcileSource(t, r, a); got.PinnedCommit() != testCommit {
			t.Errorf("%s: pin %q", name, got.PinnedCommit())
		}
	}
}

// A push applies directly only when it follows the commit last seen; an
// out-of-order or redelivered one makes the watcher read the branch.
func TestSourceListsWhenAPushDoesNotFollowTheLatest(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": thirdCommit}}
	app := withPush(withRecord(pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit),
		gitauth.SourceRecord{Ref: "main", Latest: testCommit, Via: "poll", CheckedAt: at(sourceNow.Add(-5 * time.Second))}),
		gitauth.Push{Ref: "refs/heads/main", SHA: otherCommit, Before: thirdCommit, Author: "alice", Delivery: "d-9"})
	raw := app.Annotations[shpyrdv1.AnnotationGitPush]
	r, c := newSourceReconciler(t, ls, app)
	_, got := reconcileSource(t, r, app)
	if ls.calls != 1 || got.PinnedCommit() != thirdCommit {
		t.Errorf("listings %d pin %q: the listed head, not the push's commit", ls.calls, got.PinnedCommit())
	}
	if got.Annotations[shpyrdv1.AnnotationGitPush] != "" {
		t.Error("the push is dropped")
	}
	// A stale read of the same push only drops it.
	got.Annotations[shpyrdv1.AnnotationGitPush] = raw
	if err := c.Update(context.Background(), got); err != nil {
		t.Fatal(err)
	}
	r.Now = func() time.Time { return sourceNow.Add(5 * time.Second) }
	_, got = reconcileSource(t, r, got)
	if ls.calls != 1 || got.PinnedCommit() != thirdCommit || got.Annotations[shpyrdv1.AnnotationGitPush] != "" {
		t.Errorf("listings %d pin %q", ls.calls, got.PinnedCommit())
	}
}

func TestSourceAppliesAPushThatFollowsTheLatest(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": thirdCommit}}
	app := withPush(withRecord(pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit),
		gitauth.SourceRecord{Ref: "main", Latest: testCommit, Via: "poll", CheckedAt: at(sourceNow.Add(-5 * time.Second))}),
		gitauth.Push{Ref: "refs/heads/main", SHA: otherCommit, Before: testCommit, Author: "alice"})
	r, _ := newSourceReconciler(t, ls, app)
	if _, got := reconcileSource(t, r, app); ls.calls != 0 || got.PinnedCommit() != otherCommit {
		t.Errorf("listings %d pin %q", ls.calls, got.PinnedCommit())
	}
}

// A skip push that does not follow the latest: the skip is recorded only
// when the branch's head is that commit.
func TestSourceSkipPushOutOfOrder(t *testing.T) {
	for _, tc := range []struct {
		head, skipped, pin string
	}{
		{otherCommit, otherCommit, testCommit},
		{thirdCommit, "", thirdCommit},
	} {
		ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": tc.head}}
		app := withPush(withRecord(pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit),
			gitauth.SourceRecord{Ref: "main", Latest: testCommit, Via: "poll", CheckedAt: at(sourceNow.Add(-5 * time.Second))}),
			gitauth.Push{Ref: "refs/heads/main", SHA: otherCommit, Before: thirdCommit, Message: "docs", Skip: true, Author: "bruno"})
		r, _ := newSourceReconciler(t, ls, app)
		_, got := reconcileSource(t, r, app)
		if ls.calls != 1 || sourceRecord(got).Skipped != tc.skipped || got.PinnedCommit() != tc.pin {
			t.Errorf("head %s: listings %d skipped %q pin %q", tc.head[:7], ls.calls, sourceRecord(got).Skipped, got.PinnedCommit())
		}
	}
}

// The marker read on the whole message counts, even when the message
// kept was clipped before it.
func TestSourceHonoursASkipPastTheClippedMessage(t *testing.T) {
	app := withPush(pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit),
		gitauth.Push{Ref: "refs/heads/main", SHA: otherCommit, Message: strings.Repeat("x", 1000), Skip: true, Author: "bruno"})
	r, _ := newSourceReconciler(t, &fakeLsRemote{}, app)
	if _, got := reconcileSource(t, r, app); got.PinnedCommit() != testCommit || sourceRecord(got).Skipped != otherCommit {
		t.Errorf("pin %q record %+v", got.PinnedCommit(), sourceRecord(got))
	}
}

func TestSourceErrorsInFixedWords(t *testing.T) {
	ls := &fakeLsRemote{err: errors.New("dial tcp 10.0.0.7:443: i/o timeout, token=ghp_secret")}
	app := gitApp("shop", "https://github.com/acme/shop")
	r, _ := newSourceReconciler(t, ls, app)
	_, got := reconcileSource(t, r, app)
	if e := sourceRecord(got).Error; e != "shpyrd could not read the branch; it tries again by itself." {
		t.Errorf("error %q", e)
	}
	ls.err = &refusedForTest{}
	other := gitApp("other", "https://metadata.example/acme/shop")
	r, _ = newSourceReconciler(t, ls, other)
	if _, got = reconcileSource(t, r, other); !strings.Contains(sourceRecord(got).Error, "is not an address shpyrd connects to") {
		t.Errorf("error %q", sourceRecord(got).Error)
	}
}

type refusedForTest struct{}

func (refusedForTest) Error() string        { return "metadata.example is not an address shpyrd connects to" }
func (refusedForTest) Is(target error) bool { return target == gitauth.ErrRefusedAddress }

// Failures stop counting at 6: a failing app, its record unchanged, is
// not written on each attempt.
func TestSourceFailuresAreCapped(t *testing.T) {
	ls := &fakeLsRemote{err: gitauth.ErrNoAccess}
	app := gitApp("shop", "https://github.com/acme/shop")
	app = withRecord(app, gitauth.SourceRecord{Ref: "main", Error: describeSourceError(gitauth.ErrNoAccess, app), Failures: 6, CheckedAt: at(sourceNow.Add(-maxBackoff))})
	r, c := newSourceReconciler(t, ls, app)
	before := &shpyrdv1.App{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(app), before); err != nil {
		t.Fatal(err)
	}
	_, got := reconcileSource(t, r, app)
	if ls.calls != 1 || sourceRecord(got).Failures != 6 || got.ResourceVersion != before.ResourceVersion {
		t.Errorf("listings %d failures %d written %v", ls.calls, sourceRecord(got).Failures, got.ResourceVersion != before.ResourceVersion)
	}
}

// An app built from its branch before the watcher, never pinned, whose
// branch cannot be read: it keeps building the branch as kpack polls it.
func TestAnAppWithReleasesKeepsBuildingTheBranchWhenUnreadable(t *testing.T) {
	why := "shpyrd could not read the repository: it is private, or does not exist under that name."
	app := withRecord(gitApp("shop", "https://user:pass@git.example.com/acme/shop"), gitauth.SourceRecord{Ref: "main", Error: why, Failures: 1})
	app.Status.Releases = []shpyrdv1.Release{{Number: 1, Image: "registry/shop@sha256:abc", Source: "main", CreatedAt: metav1.Now()}}
	r, c := newTestReconciler(t, app)
	got := runReconcile(t, r, app)
	if rev, ok := kpackRevision(t, c, app); !ok || rev != "main" {
		t.Fatalf("kpack revision %q (%v): the branch, as before", rev, ok)
	}
	if got.Status.Source == nil || got.Status.Source.Error != why {
		t.Errorf("status.source %+v", got.Status.Source)
	}
	if cond := meta.FindStatusCondition(got.Status.Conditions, shpyrdv1.ConditionBuilt); cond != nil && cond.Reason == reasonWaitingForCommit {
		t.Errorf("stuck waiting: %+v", cond)
	}
	// Once a commit was pinned, the branch is the watcher's to follow.
	pinned := withRecord(gitApp("pinned", "https://git.example.com/acme/shop"), gitauth.SourceRecord{Ref: "main", Error: why, Failures: 1})
	pinned.Annotations[shpyrdv1.AnnotationGitCommit] = testCommit
	pinned.Annotations[shpyrdv1.AnnotationGitCommitRef] = "release"
	pinned.Status.Releases = app.Status.Releases
	if msg, failed := waitingForCommit(pinned); msg != why || !failed {
		t.Errorf("waiting %q %v", msg, failed)
	}
}

func TestABranchThatCannotBeReadIsSourceUnreadable(t *testing.T) {
	why := "shpyrd could not read the repository: it is private, or does not exist under that name."
	app := withRecord(gitApp("shop", "https://github.com/acme/shop"), gitauth.SourceRecord{Ref: "main", Error: why, Failures: 1})
	r, _ := newTestReconciler(t, app)
	got := runReconcile(t, r, app)
	for _, ct := range []string{shpyrdv1.ConditionBuilt, shpyrdv1.ConditionReady} {
		if cond := meta.FindStatusCondition(got.Status.Conditions, ct); cond == nil || cond.Reason != "SourceUnreadable" {
			t.Errorf("%s: %+v", ct, cond)
		}
	}
}

// A newer value kept is said at V(1); any other failure at Info.
func TestLogRemoveAnnotation(t *testing.T) {
	var lines []string
	l := funcr.New(func(prefix, args string) { lines = append(lines, args) }, funcr.Options{Verbosity: 0})
	ctx := log.IntoContext(context.Background(), logr.Logger(l))
	invalid := apierrors.NewInvalid(schema.GroupKind{Kind: "App"}, "shop", nil)
	logRemoveAnnotation(ctx, invalid, "kept a newer push for the next pass", "the push")
	if len(lines) != 0 {
		t.Errorf("a kept newer value is V(1): %v", lines)
	}
	logRemoveAnnotation(ctx, errors.New("connection refused"), "kept a newer push for the next pass", "the push")
	if len(lines) != 1 || !strings.Contains(lines[0], "could not clear the push") {
		t.Errorf("lines %v", lines)
	}
}
