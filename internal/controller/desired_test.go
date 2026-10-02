package controller

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/ids"
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
	img, err := r.Config.desiredKpackImage(app, nil)
	if err != nil {
		t.Fatal(err)
	}
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

// Image repositories are keyed by the workspace's id in base58, every
// workspace included, so two workspaces with a shop never share one; an
// unknown workspace is an error to retry, not a guess; without a store the
// slug stands in.
func TestImageTagPerWorkspace(t *testing.T) {
	implicit := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-shop"}}
	acme := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-acme-shop", Labels: map[string]string{shpyrdv1.LabelWorkspace: "acme"}}}
	ids := map[string]string{"default": "3f2a9c1b-8d7e-4a1b-9c2d-0e1f2a3b4c5d", "acme": "9e8d7c6b-5a43-4f21-8e0d-1c2b3a495867"}
	c := Config{RegistryHost: "10.96.0.50:5000", WorkspaceID: func(slug string) string { return ids[slug] }}
	got, err := c.imageTag(implicit)
	if err != nil || got != "10.96.0.50:5000/apps/"+ids58(ids["default"])+"/shop" {
		t.Errorf("implicit = %s %v", got, err)
	}
	got2, err := c.imageTag(acme)
	if err != nil || got2 != "10.96.0.50:5000/apps/"+ids58(ids["acme"])+"/shop" || got2 == got {
		t.Errorf("acme = %s %v", got2, err)
	}
	if _, err := c.imageTag(&shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "x", Labels: map[string]string{shpyrdv1.LabelWorkspace: "ghost"}}}); err == nil {
		t.Error("an unknown workspace must be an error, never a guessed repository")
	}
	bare := Config{RegistryHost: "10.96.0.50:5000"}
	if got, err := bare.imageTag(implicit); err != nil || got != "10.96.0.50:5000/apps/default/shop" {
		t.Errorf("without a store = %s %v", got, err)
	}
}

func ids58(id string) string { return ids.Short(id) }
