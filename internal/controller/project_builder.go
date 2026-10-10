package controller

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// builderGrace is how long a project's own Builder may take to become
// ready (#143): kpack makes it a few seconds after the first deploy, and
// retries while the buildpacks it names are still being pulled.
const builderGrace = 2 * time.Minute

// projectBuilderFailure is the sentence for a project's Builder that is
// not ready builderGrace after it was made or last changed, or "" while
// it may still be.
func (r *AppReconciler) projectBuilderFailure(ctx context.Context, app *shpyrdv1.App) string {
	b := &unstructured.Unstructured{}
	b.SetGroupVersionKind(BuilderGVK)
	if err := r.Get(ctx, client.ObjectKey{Namespace: app.Namespace, Name: builderName(app)}, b); err != nil {
		return ""
	}
	// The grace runs from the Builder's making, or from its Ready
	// condition's last change when kpack remakes an older one.
	since := b.GetCreationTimestamp().Time
	reason := "it is still not ready"
	conds, _, _ := unstructured.NestedSlice(b.Object, "status", "conditions")
	for _, raw := range conds {
		m, ok := raw.(map[string]interface{})
		if !ok || m["type"] != "Ready" {
			continue
		}
		if m["status"] == "True" {
			return ""
		}
		if msg, _ := m["message"].(string); msg != "" {
			reason = msg
		}
		if at, _ := m["lastTransitionTime"].(string); at != "" {
			if t, err := time.Parse(time.RFC3339, at); err == nil && t.After(since) {
				since = t
			}
		}
	}
	if time.Since(since) < builderGrace {
		return ""
	}
	return "The project's builder could not be made: " + reason + ". A fault of the platform, not of the app; deploying again later usually passes."
}
