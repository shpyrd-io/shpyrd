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

// Build pods fetch archives from the server's sources port; archives
// uploaded when the API port served them are pointed at it too, and any
// other URL is left alone.
func TestSourceURL(t *testing.T) {
	c := Config{SystemNamespace: "shpyrd-system"}
	for in, want := range map[string]string{
		"http://shpyrd-server.shpyrd-system.svc/api/sources/abc.tgz":               "http://shpyrd-server.shpyrd-system.svc:8082/api/sources/abc.tgz",
		"http://shpyrd-server.shpyrd-system.svc:80/api/sources/abc.tgz":            "http://shpyrd-server.shpyrd-system.svc:8082/api/sources/abc.tgz",
		"http://shpyrd-server.shpyrd-system.svc.cluster.local/api/sources/abc.tgz": "http://shpyrd-server.shpyrd-system.svc.cluster.local:8082/api/sources/abc.tgz",
		"http://shpyrd-server.shpyrd-system.svc:8082/api/sources/abc.tgz":          "http://shpyrd-server.shpyrd-system.svc:8082/api/sources/abc.tgz",
		"https://bucket.example.test/sources/abc.tgz":                              "https://bucket.example.test/sources/abc.tgz",
		"http://shpyrd-server.other.svc/api/sources/abc.tgz":                       "http://shpyrd-server.other.svc/api/sources/abc.tgz",
	} {
		if got := c.sourceURL(in); got != want {
			t.Errorf("sourceURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// A platform upgrade that moves source archives to the sources port must
// not rebuild every app: an Image whose archive still names the API port
// keeps it until a build happens anyway (a rebuild asked for, a new
// archive), which takes the new address.
func TestSourcePortMoveDoesNotRebuild(t *testing.T) {
	old := "http://shpyrd-server.shpyrd-system.svc/api/sources/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.tgz"
	app := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "web1", Namespace: "app-web1", Generation: 1},
		Spec:       shpyrdv1.AppSpec{Source: &shpyrdv1.Source{Blob: &shpyrdv1.BlobSource{URL: old, SHA256: strings.Repeat("a", 64)}}},
	}
	r, c := newTestReconciler(t, app)
	r.Config.SystemNamespace = "shpyrd-system"
	// The Image as an older platform left it.
	img := r.Config.desiredKpackImage(app)
	_ = unstructured.SetNestedField(img.Object, old, "spec", "source", "blob", "url")
	if err := c.Create(context.Background(), img); err != nil {
		t.Fatal(err)
	}
	runReconcile(t, r, app)
	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(KpackImageGVK)
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-web1", Name: "web1"}, got); err != nil {
		t.Fatal(err)
	}
	if u, _, _ := unstructured.NestedString(got.Object, "spec", "source", "blob", "url"); u != old {
		t.Errorf("the archive address changed without a build: %s", u)
	}
	// A rebuild asked for takes the new address in the same update.
	cur := &shpyrdv1.App{}
	_ = c.Get(context.Background(), types.NamespacedName{Namespace: "app-web1", Name: "web1"}, cur)
	cur.Annotations = map[string]string{shpyrdv1.AnnotationRebuildAt: "2026-09-27T10:00:00Z"}
	if err := c.Update(context.Background(), cur); err != nil {
		t.Fatal(err)
	}
	runReconcile(t, r, cur)
	_ = c.Get(context.Background(), types.NamespacedName{Namespace: "app-web1", Name: "web1"}, got)
	if u, _, _ := unstructured.NestedString(got.Object, "spec", "source", "blob", "url"); u != r.Config.sourceURL(old) {
		t.Errorf("a rebuild must fetch from the sources port: %s", u)
	}
	// A new archive names the sources port from the start.
	fresh := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "web2", Namespace: "app-web2", Generation: 1},
		Spec:       shpyrdv1.AppSpec{Source: &shpyrdv1.Source{Blob: &shpyrdv1.BlobSource{URL: strings.Replace(old, "svc/", "svc:8082/", 1)}}},
	}
	r2, c2 := newTestReconciler(t, fresh)
	r2.Config.SystemNamespace = "shpyrd-system"
	runReconcile(t, r2, fresh)
	_ = c2.Get(context.Background(), types.NamespacedName{Namespace: "app-web2", Name: "web2"}, got)
	if u, _, _ := unstructured.NestedString(got.Object, "spec", "source", "blob", "url"); !strings.Contains(u, ":8082/") {
		t.Errorf("new archive url = %s", u)
	}
}

// Two workspaces may both have a shop: their images live in repositories
// of their own; the implicit workspace keeps apps/<slug>.
func TestImageTagPerWorkspace(t *testing.T) {
	c := Config{RegistryHost: "10.96.0.50:5000"}
	implicit := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-shop"}}
	acme := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-acme-shop", Labels: map[string]string{shpyrdv1.LabelWorkspace: "acme"}}}
	if got := c.imageTag(implicit); got != "10.96.0.50:5000/apps/shop" {
		t.Errorf("implicit = %s", got)
	}
	if got := c.imageTag(acme); got != "10.96.0.50:5000/apps/acme/shop" {
		t.Errorf("acme = %s", got)
	}
}
