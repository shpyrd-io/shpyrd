package controller

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shpyrd-io/shpyrd/pkg/audit"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// A project's first deploy with a builder of its own (#143): kpack says
// the Image's builder is not ready while the Builder is being made. The
// project waits, Building, until the Builder has had two minutes; then
// kpack's reason is the build's failure.
func TestFirstDeployWaitsForTheProjectsBuilder(t *testing.T) {
	for _, tc := range []struct {
		name     string
		age      time.Duration
		builder  []interface{}
		phase    string
		contains string
	}{
		{"out of date", 10 * time.Second, nil, shpyrdv1.PhaseBuilding, "Preparing this project's builder"},
		{"failed build", 10 * time.Second, nil, shpyrdv1.PhaseFailed, "a real build failure"},
		{"being made", 10 * time.Second, nil, shpyrdv1.PhaseBuilding, "Preparing this project's builder"},
		// An older Builder being remade (a changed buildpack, an upgrade):
		// the grace runs from its Ready condition's last change.
		{"remade", 30 * time.Minute, []interface{}{map[string]interface{}{"type": "Ready", "status": "False", "message": "rebuilding", "lastTransitionTime": time.Now().Add(-10 * time.Second).UTC().Format(time.RFC3339)}}, shpyrdv1.PhaseBuilding, "Preparing this project's builder"},
		{"failed for good", 5 * time.Minute, []interface{}{map[string]interface{}{"type": "Ready", "status": "False", "message": "buildpack image not found"}}, shpyrdv1.PhaseFailed, "buildpack image not found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			app := &shpyrdv1.App{
				ObjectMeta: metav1.ObjectMeta{Name: "site", Namespace: "app-site", Generation: 1},
				Spec: shpyrdv1.AppSpec{
					Source: &shpyrdv1.Source{Git: &shpyrdv1.GitSource{URL: "https://github.com/example/repo", Revision: "main"}},
					Build:  &shpyrdv1.Build{Workspace: "apps/web"},
				},
			}
			b := &unstructured.Unstructured{}
			b.SetGroupVersionKind(BuilderGVK)
			b.SetNamespace("app-site")
			b.SetName("site-builder")
			b.SetCreationTimestamp(metav1.NewTime(time.Now().Add(-tc.age)))
			if tc.builder != nil {
				_ = unstructured.SetNestedSlice(b.Object, tc.builder, "status", "conditions")
			}
			r, c := newTestReconciler(t, pinGit(app, testCommit), b)
			r.Kube = k8sfake.NewSimpleClientset()
			var heard []audit.Entry
			audit.Sink = func(_ audit.Ref, e audit.Entry) { heard = append(heard, e) }
			t.Cleanup(func() { audit.Sink = nil })
			runReconcile(t, r, app) // makes the kpack Image

			img := &unstructured.Unstructured{}
			img.SetGroupVersionKind(KpackImageGVK)
			if err := c.Get(ctx, types.NamespacedName{Namespace: "app-site", Name: "site"}, img); err != nil {
				t.Fatal(err)
			}
			_ = unstructured.SetNestedField(img.Object, int64(img.GetGeneration()), "status", "observedGeneration")
			builderCondition := "BuilderReady"
			readyReason := "BuilderNotReady"
			if tc.name == "out of date" {
				builderCondition, readyReason = "BuilderUpToDate", "BuilderNotUpToDate"
			}
			_ = unstructured.SetNestedSlice(img.Object, []interface{}{
				map[string]interface{}{"type": "Ready", "status": "False", "reason": readyReason, "message": "Builder site-builder is not ready"},
				map[string]interface{}{"type": builderCondition, "status": "False", "reason": readyReason},
			}, "status", "conditions")
			if tc.name == "failed build" {
				failed := &unstructured.Unstructured{}
				failed.SetGroupVersionKind(KpackBuildGVK)
				failed.SetNamespace(app.Namespace)
				failed.SetName("site-build-1")
				failed.SetAnnotations(map[string]string{shpyrdv1.AnnotationBuildFailure: "a real build failure"})
				_ = unstructured.SetNestedSlice(failed.Object, []interface{}{map[string]interface{}{"type": "Succeeded", "status": "False"}}, "status", "conditions")
				if err := c.Create(ctx, failed); err != nil {
					t.Fatal(err)
				}
				_ = unstructured.SetNestedField(img.Object, failed.GetName(), "status", "latestBuildRef")
			}
			if err := c.Update(ctx, img); err != nil {
				t.Fatal(err)
			}
			got := runReconcile(t, r, app)
			if got.Status.Phase != tc.phase || !strings.Contains(got.Status.Message, tc.contains) {
				t.Errorf("phase = %s, message = %q", got.Status.Phase, got.Status.Message)
			}
			if tc.phase == shpyrdv1.PhaseBuilding {
				condition := meta.FindStatusCondition(got.Status.Conditions, shpyrdv1.ConditionBuilt)
				if condition == nil || condition.Status != metav1.ConditionUnknown || condition.Reason != "PreparingBuilder" {
					t.Errorf("Built condition = %+v", condition)
				}
				for _, event := range heard {
					if event.Action == "release.failed" {
						t.Errorf("preparation audited as a failure: %+v", event)
					}
				}
			}
			if strings.Contains(got.Status.Message, "could not start") {
				t.Errorf("still the platform-fault sentence: %q", got.Status.Message)
			}
		})
	}
}

// A project's Builder that is not there at all fails the build once the
// Image has waited for it longer than the grace; before that, it waits.
func TestMissingProjectBuilder(t *testing.T) {
	app := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "site", Namespace: "app-site"},
		Spec:       shpyrdv1.AppSpec{Build: &shpyrdv1.Build{Workspace: "apps/web"}},
	}
	r, _ := newTestReconciler(t, app)
	ctx := context.Background()
	if msg := r.projectBuilderFailure(ctx, app, time.Now().Add(-10*time.Second)); msg != "" {
		t.Errorf("failed while waiting: %q", msg)
	}
	if msg := r.projectBuilderFailure(ctx, app, time.Time{}); msg != "" {
		t.Errorf("failed with no time to go by: %q", msg)
	}
	if msg := r.projectBuilderFailure(ctx, app, time.Now().Add(-5*time.Minute)); !strings.Contains(msg, "builder") {
		t.Errorf("waits forever: %q", msg)
	}
}

func TestBuilderWaitFallsBackToImageCreation(t *testing.T) {
	img := &unstructured.Unstructured{}
	created := time.Now().Add(-5 * time.Minute).Truncate(time.Second)
	img.SetCreationTimestamp(metav1.NewTime(created))
	_ = unstructured.SetNestedSlice(img.Object, []interface{}{map[string]interface{}{"type": "Ready", "status": "False", "reason": "BuilderNotReady", "lastTransitionTime": "invalid"}}, "status", "conditions")
	state := readBuildState(img)
	if !state.ReadySince.Equal(created) {
		t.Fatalf("waiting since %v, want %v", state.ReadySince, created)
	}
	app := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "site", Namespace: "app-site"}}
	r, _ := newTestReconciler(t, app)
	if message := r.projectBuilderFailure(t.Context(), app, state.ReadySince); message == "" {
		t.Fatal("missing builder must time out even without a valid condition timestamp")
	}
}

func TestReadyBuilderDoesNotLeaveImagePreparingForever(t *testing.T) {
	app := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "site", Namespace: "app-site"}}
	b := &unstructured.Unstructured{}
	b.SetGroupVersionKind(BuilderGVK)
	b.SetNamespace(app.Namespace)
	b.SetName(builderName(app))
	_ = unstructured.SetNestedSlice(b.Object, []interface{}{map[string]interface{}{"type": "Ready", "status": "True"}}, "status", "conditions")
	r, _ := newTestReconciler(t, app, b)
	for _, age := range []time.Duration{10 * time.Second, 5 * time.Minute} {
		message := r.projectBuilderFailure(t.Context(), app, time.Now().Add(-age))
		if (message != "") != (age >= builderGrace) {
			t.Errorf("age=%s message=%q", age, message)
		}
	}
}
