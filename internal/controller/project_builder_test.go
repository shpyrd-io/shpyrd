package controller

import (
	"context"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

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
		{"being made", 10 * time.Second, nil, shpyrdv1.PhaseBuilding, "preparing the project's builder"},
		// An older Builder being remade (a changed buildpack, an upgrade):
		// the grace runs from its Ready condition's last change.
		{"remade", 30 * time.Minute, []interface{}{map[string]interface{}{"type": "Ready", "status": "False", "message": "rebuilding", "lastTransitionTime": time.Now().Add(-10 * time.Second).UTC().Format(time.RFC3339)}}, shpyrdv1.PhaseBuilding, "preparing the project's builder"},
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
			r, c := newTestReconciler(t, app, b)
			runReconcile(t, r, app) // makes the kpack Image

			img := &unstructured.Unstructured{}
			img.SetGroupVersionKind(KpackImageGVK)
			if err := c.Get(ctx, types.NamespacedName{Namespace: "app-site", Name: "site"}, img); err != nil {
				t.Fatal(err)
			}
			_ = unstructured.SetNestedField(img.Object, int64(img.GetGeneration()), "status", "observedGeneration")
			_ = unstructured.SetNestedSlice(img.Object, []interface{}{
				map[string]interface{}{"type": "Ready", "status": "False", "reason": "BuilderNotReady", "message": "Builder site-builder is not ready"},
				map[string]interface{}{"type": "BuilderReady", "status": "False", "reason": "BuilderNotReady"},
			}, "status", "conditions")
			if err := c.Update(ctx, img); err != nil {
				t.Fatal(err)
			}
			got := runReconcile(t, r, app)
			if got.Status.Phase != tc.phase || !strings.Contains(got.Status.Message, tc.contains) {
				t.Errorf("phase = %s, message = %q", got.Status.Phase, got.Status.Message)
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
