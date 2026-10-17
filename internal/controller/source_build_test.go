package controller

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
)

// kpackRevision is the revision of the App's kpack Image; false when there
// is no Image.
func kpackRevision(t *testing.T, c client.Client, app *shpyrdv1.App) (string, bool) {
	t.Helper()
	img := &unstructured.Unstructured{}
	img.SetGroupVersionKind(KpackImageGVK)
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(app), img); err != nil {
		return "", false
	}
	rev, _, _ := unstructured.NestedString(img.Object, "spec", "source", "git", "revision")
	return rev, true
}

func sourceCondition(app *shpyrdv1.App) *metav1.Condition {
	return meta.FindStatusCondition(app.Status.Conditions, shpyrdv1.ConditionSourceReady)
}

func TestABranchWaitsForItsCommit(t *testing.T) {
	app := gitApp("shop", "https://github.com/acme/shop")
	r, c := newTestReconciler(t, app)
	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(app)})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := kpackRevision(t, c, app); ok {
		t.Fatal("nothing is built before a commit is pinned")
	}
	got := &shpyrdv1.App{}
	_ = c.Get(context.Background(), client.ObjectKeyFromObject(app), got)
	if got.Status.Phase != shpyrdv1.PhaseBuilding || got.Status.Message != "Reading the branch's latest commit." {
		t.Errorf("phase %q %q", got.Status.Phase, got.Status.Message)
	}
	if res.RequeueAfter != 5*time.Second {
		t.Errorf("requeue %v: the watcher pins within seconds", res.RequeueAfter)
	}
	if cond := sourceCondition(got); cond == nil || cond.Status != metav1.ConditionUnknown {
		t.Errorf("SourceReady %+v", cond)
	}
}

func TestABranchThatCannotBeReadSaysWhy(t *testing.T) {
	why := "shpyrd could not read the repository: it is private, or does not exist under that name."
	app := withRecord(gitApp("shop", "https://github.com/acme/shop"), gitauth.SourceRecord{Ref: "main", Error: why, Failures: 1})
	r, _ := newTestReconciler(t, app)
	got := runReconcile(t, r, app)
	if got.Status.Phase != shpyrdv1.PhaseFailed || got.Status.Message != why {
		t.Errorf("phase %q %q", got.Status.Phase, got.Status.Message)
	}
	if got.Status.Source == nil || got.Status.Source.Error != why {
		t.Errorf("status.source %+v", got.Status.Source)
	}
	if cond := sourceCondition(got); cond == nil || cond.Status != metav1.ConditionFalse || cond.Message != why {
		t.Errorf("SourceReady %+v", cond)
	}
}

func TestKpackBuildsThePinnedCommit(t *testing.T) {
	app := withRecord(pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit),
		gitauth.SourceRecord{Ref: "main", Latest: testCommit, Message: "fix login", Author: "alice", Via: "push", CheckedAt: at(sourceNow)})
	r, c := newTestReconciler(t, app)
	got := runReconcile(t, r, app)
	if rev, ok := kpackRevision(t, c, app); !ok || rev != testCommit {
		t.Fatalf("kpack revision %q (%v): the pinned SHA, so kpack stops polling the branch", rev, ok)
	}
	s := got.Status.Source
	if s == nil || s.Commit != testCommit || s.Latest != testCommit || s.Author != "alice" || s.Via != "push" || s.CheckedAt == nil {
		t.Errorf("status.source %+v", s)
	}
	if cond := sourceCondition(got); cond == nil || cond.Status != metav1.ConditionTrue {
		t.Errorf("SourceReady %+v", cond)
	}
}

func TestAPinForAnotherBranchIsNotBuilt(t *testing.T) {
	app := pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit)
	app.Spec.Source.Git.Revision = "release"
	r, c := newTestReconciler(t, app)
	runReconcile(t, r, app)
	if _, ok := kpackRevision(t, c, app); ok {
		t.Error("main's commit must not build release")
	}
}

func TestACommitRevisionIsBuiltAsGiven(t *testing.T) {
	app := gitApp("shop", "https://github.com/acme/shop")
	app.Spec.Source.Git.Revision = "a1b2c3d"
	r, c := newTestReconciler(t, app)
	got := runReconcile(t, r, app)
	if rev, ok := kpackRevision(t, c, app); !ok || rev != "a1b2c3d" {
		t.Errorf("kpack revision %q (%v)", rev, ok)
	}
	if got.Status.Source != nil || sourceCondition(got) != nil {
		t.Errorf("a commit is not watched: %+v", got.Status.Source)
	}
}

func TestDockerfileBuildsThePinnedCommit(t *testing.T) {
	a := dockerfileGitApp("https://github.com/acme/dk")
	b := pinGit(dockerfileGitApp("https://github.com/acme/dk"), otherCommit)
	if buildKey(a) == buildKey(b) {
		t.Error("a new commit is a new build")
	}
	if script := (Config{}).fetchScript(a, gitauth.Build{}); !strings.Contains(script, "rev='"+testCommit+"'") {
		t.Errorf("the fetch checks out the pinned commit:\n%s", script)
	}
}

// Review Focus 3: the note the watcher wrote for the next push survives
// the clearing of the note before it.
func TestClearReleaseNoteKeepsANewerNote(t *testing.T) {
	app := sampleApp("shop")
	app.Annotations = map[string]string{shpyrdv1.AnnotationReleaseNote: "Push 89abcde by bob: second"}
	r, c := newTestReconciler(t, app)
	ctx := context.Background()
	stale := app.DeepCopy()
	stale.Annotations[shpyrdv1.AnnotationReleaseNote] = "Push 0123456 by alice: first"
	r.clearReleaseNote(ctx, stale)
	got := &shpyrdv1.App{}
	_ = c.Get(ctx, client.ObjectKeyFromObject(app), got)
	if got.Annotations[shpyrdv1.AnnotationReleaseNote] != "Push 89abcde by bob: second" {
		t.Fatalf("the newer note is gone: %v", got.Annotations)
	}
	r.clearReleaseNote(ctx, got)
	_ = c.Get(ctx, client.ObjectKeyFromObject(app), got)
	if _, ok := got.Annotations[shpyrdv1.AnnotationReleaseNote]; ok {
		t.Error("the note that was read is cleared")
	}
}

func TestStatusSourceIgnoresAnotherBranchsRecord(t *testing.T) {
	raw, _ := json.Marshal(gitauth.SourceRecord{Ref: "main", Error: "old"})
	app := pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit)
	app.Spec.Source.Git.Revision = "release"
	app = pinGit(app, otherCommit)
	app.Annotations[shpyrdv1.AnnotationGitSource] = string(raw)
	if s := sourceStatus(app); s == nil || s.Commit != otherCommit || s.Error != "" {
		t.Errorf("status.source %+v", s)
	}
}

// Switching auto-deploy is a setting, not a source: the pinned commit is
// still what is built, so no new build starts.
func TestFlippingAutoDeployStartsNoBuild(t *testing.T) {
	app := withRecord(pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit),
		gitauth.SourceRecord{Ref: "main", Latest: testCommit, CheckedAt: at(sourceNow)})
	r, c := newTestReconciler(t, app)
	ctx := context.Background()
	kpackSource := func() map[string]any {
		t.Helper()
		img := &unstructured.Unstructured{}
		img.SetGroupVersionKind(KpackImageGVK)
		if err := c.Get(ctx, client.ObjectKeyFromObject(app), img); err != nil {
			t.Fatal(err)
		}
		src, _, _ := unstructured.NestedMap(img.Object, "spec", "source")
		return src
	}
	runReconcile(t, r, app)
	before := kpackSource()
	for _, on := range []bool{false, true} {
		got := &shpyrdv1.App{}
		if err := c.Get(ctx, client.ObjectKeyFromObject(app), got); err != nil {
			t.Fatal(err)
		}
		got.Spec.Source.Git.AutoDeploy = &on
		if err := c.Update(ctx, got); err != nil {
			t.Fatal(err)
		}
		runReconcile(t, r, got)
		if after := kpackSource(); !reflect.DeepEqual(before, after) {
			t.Errorf("auto-deploy %v changed the kpack source: %v -> %v", on, before, after)
		}
	}

	dk := dockerfileGitApp("https://github.com/acme/dk")
	key := buildKey(dk)
	for _, on := range []bool{false, true} {
		dk.Spec.Source.Git.AutoDeploy = &on
		if buildKey(dk) != key {
			t.Errorf("auto-deploy %v is a new Dockerfile build", on)
		}
	}
}

func TestSourceStatusShowsAHeldAndANotDeployedCommit(t *testing.T) {
	app := pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit)
	withRecord(app, gitauth.SourceRecord{Ref: "main", Latest: otherCommit, Held: otherCommit, HeldSince: at(sourceNow),
		NotDeployed: testCommit, NotDeployedReason: gitauth.ReasonNoWatchedChanges})
	st := sourceStatus(app)
	if st.Held != otherCommit || st.HeldSince == nil || !st.HeldSince.Time.Equal(sourceNow) ||
		st.NotDeployed != testCommit || st.NotDeployedReason != gitauth.ReasonNoWatchedChanges {
		t.Errorf("status.source = %+v", st)
	}
}
