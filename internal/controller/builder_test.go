package controller

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

func TestBuildComposition(t *testing.T) {
	ctx := context.Background()
	composed := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-shop"},
		Spec:       shpyrdv1.AppSpec{Build: &shpyrdv1.Build{Buildpacks: []string{"deb-packages", "ruby"}, Stack: "full"}},
	}
	plain := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "blog", Namespace: "app-blog"}}
	r, c := newTestReconciler(t, composed, plain)
	r.Config.RegistryHost = "10.96.0.50:5000"

	// A composed build gets a Builder: full stack, one group, in order.
	ref, err := r.reconcileBuilder(ctx, composed)
	if err != nil {
		t.Fatal(err)
	}
	if ref["kind"] != "Builder" || ref["name"] != "shop-builder" {
		t.Errorf("builder ref = %v", ref)
	}
	b := &unstructured.Unstructured{}
	b.SetGroupVersionKind(BuilderGVK)
	if err := c.Get(ctx, types.NamespacedName{Namespace: "app-shop", Name: "shop-builder"}, b); err != nil {
		t.Fatalf("builder: %v", err)
	}
	stack, _, _ := unstructured.NestedString(b.Object, "spec", "stack", "name")
	tag, _, _ := unstructured.NestedString(b.Object, "spec", "tag")
	order, _, _ := unstructured.NestedSlice(b.Object, "spec", "order")
	if stack != "jammy-full" || tag != "10.96.0.50:5000/apps/default/shop/builder" || len(order) != 1 {
		t.Errorf("builder spec: stack=%s tag=%s groups=%d", stack, tag, len(order))
	}
	group, _ := order[0].(map[string]interface{})["group"].([]interface{})
	var names []string
	for _, e := range group {
		names = append(names, e.(map[string]interface{})["name"].(string))
	}
	if strings.Join(names, ",") != "heroku-deb-packages,paketo-ruby" {
		t.Errorf("group = %v", names)
	}
	if len(b.GetOwnerReferences()) != 1 {
		t.Error("the Builder must be owned by the App")
	}

	// A plain app uses the platform's ClusterBuilder and has no Builder.
	ref, err = r.reconcileBuilder(ctx, plain)
	if err != nil || ref["kind"] != "ClusterBuilder" || ref["name"] != "shpyrd" {
		t.Errorf("plain ref = %v %v", ref, err)
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "app-blog", Name: "blog-builder"}, b); err == nil {
		t.Error("a plain app got a Builder")
	}

	// Unknown names are refused with the catalog.
	bad := composed.DeepCopy()
	bad.Spec.Build.Buildpacks = []string{"cobol"}
	if _, err := r.reconcileBuilder(ctx, bad); err == nil || !strings.Contains(err.Error(), "cobol") || !strings.Contains(err.Error(), "ruby") {
		t.Errorf("unknown buildpack: %v", err)
	}

	// Composition removed: the Builder goes, the platform's returns.
	composed.Spec.Build = nil
	if ref, err := r.reconcileBuilder(ctx, composed); err != nil || ref["kind"] != "ClusterBuilder" {
		t.Errorf("after clearing: %v %v", ref, err)
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "app-shop", Name: "shop-builder"}, b); err == nil {
		t.Error("the Builder survived the composition being cleared")
	}
}

// A project's own Builder puts the build-env buildpack first in every
// group, as the platform's does (#54), when the platform has it; without
// it (a kpack component from before) the project still builds.
func TestComposedBuilderReadsTheProjectsVariables(t *testing.T) {
	ctx := context.Background()
	app := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-shop"},
		Spec:       shpyrdv1.AppSpec{Build: &shpyrdv1.Build{Buildpacks: []string{"ruby"}, SystemPackages: true}},
	}
	r, c := newTestReconciler(t, app)
	groups := func() []string {
		t.Helper()
		if _, err := r.reconcileBuilder(ctx, app); err != nil {
			t.Fatal(err)
		}
		b := &unstructured.Unstructured{}
		b.SetGroupVersionKind(BuilderGVK)
		if err := c.Get(ctx, types.NamespacedName{Namespace: "app-shop", Name: "shop-builder"}, b); err != nil {
			t.Fatal(err)
		}
		order, _, _ := unstructured.NestedSlice(b.Object, "spec", "order")
		var out []string
		for _, g := range order {
			var names []string
			for _, e := range g.(map[string]interface{})["group"].([]interface{}) {
				names = append(names, e.(map[string]interface{})["name"].(string))
			}
			out = append(out, strings.Join(names, ","))
		}
		return out
	}
	if got := strings.Join(groups(), " | "); got != "heroku-deb-packages,paketo-ruby" {
		t.Errorf("without the buildpack on the platform = %s", got)
	}
	bp := &unstructured.Unstructured{}
	bp.SetGroupVersionKind(ClusterBuildpackGVK)
	bp.SetName(BuildEnvBuildpack)
	if err := c.Create(ctx, bp); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(groups(), " | "); got != "shpyrd-build-env,heroku-deb-packages,paketo-ruby" {
		t.Errorf("with it = %s", got)
	}
	app.Spec.Build.Buildpacks = nil // the platform's detection, one group per language
	for _, g := range groups() {
		if !strings.HasPrefix(g, "shpyrd-build-env,") {
			t.Errorf("a group without it first: %s", g)
		}
	}
}
