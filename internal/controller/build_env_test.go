package controller

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// A buildpack build sees the variables the app runs with (#54): the
// project's config vars and the bound vars of its attachments reach it by
// reference to <app>-build-env, never as values in the Image; build.env
// sets its own names; a new config var waits for the next build instead
// of starting one.
func TestBuildSeesTheAppsVariables(t *testing.T) {
	ctx := context.Background()
	app := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "web1", Namespace: "app-web1", Generation: 1},
		Spec: shpyrdv1.AppSpec{
			Source: &shpyrdv1.Source{Blob: &shpyrdv1.BlobSource{URL: "http://shpyrd-server.shpyrd-system.svc:8082/api/sources/" + strings.Repeat("a", 64) + ".tgz", SHA256: strings.Repeat("a", 64)}},
			Build:  &shpyrdv1.Build{Env: []corev1.EnvVar{{Name: "NODE_ENV", Value: "production"}, {Name: "FOO", Value: "from-build"}}},
		},
	}
	env := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "web1-env", Namespace: "app-web1"},
		Data:       map[string][]byte{"DATABASE_URL": []byte("postgres://secret@db/app"), "FOO": []byte("from-config")},
	}
	r, c := newTestReconciler(t, app, env)
	runReconcile(t, r, app)

	cm := &corev1.ConfigMap{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "app-web1", Name: "web1-build-env"}, cm); err != nil {
		t.Fatal(err)
	}
	if cm.Data["DATABASE_URL"] != "postgres://secret@db/app" {
		t.Errorf("build variables = %v", cm.Data)
	}
	img := &unstructured.Unstructured{}
	img.SetGroupVersionKind(KpackImageGVK)
	if err := c.Get(ctx, types.NamespacedName{Namespace: "app-web1", Name: "web1"}, img); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(img.Object["spec"])
	if strings.Contains(string(raw), "secret@db") {
		t.Fatalf("a value sits in the Image: %s", raw)
	}
	names := func() []string {
		_ = c.Get(ctx, types.NamespacedName{Namespace: "app-web1", Name: "web1"}, img)
		entries, _, _ := unstructured.NestedSlice(img.Object, "spec", "build", "env")
		var out []string
		for _, e := range entries {
			m := e.(map[string]interface{})
			kind := "value"
			if isBuildVarRef(m) {
				kind = "ref"
			}
			out = append(out, m["name"].(string)+"="+kind)
		}
		return out
	}
	if got := strings.Join(names(), " "); got != "NODE_ENV=value FOO=value DATABASE_URL=ref" {
		t.Errorf("build env = %s", got)
	}

	// kpack writes defaults into the stored Image (seen on kind, kpack
	// 0.18): the names must not refresh because of them.
	_ = c.Get(ctx, types.NamespacedName{Namespace: "app-web1", Name: "web1"}, img)
	_ = unstructured.SetNestedMap(img.Object, map[string]interface{}{}, "spec", "build", "resources")
	if err := c.Update(ctx, img); err != nil {
		t.Fatal(err)
	}

	// A new config var: the ConfigMap has it at once, the Image waits.
	_ = c.Get(ctx, types.NamespacedName{Namespace: "app-web1", Name: "web1-env"}, env)
	env.Data["SENTRY_DSN"] = []byte("https://key@sentry.test/1")
	if err := c.Update(ctx, env); err != nil {
		t.Fatal(err)
	}
	runReconcile(t, r, app)
	_ = c.Get(ctx, types.NamespacedName{Namespace: "app-web1", Name: "web1-build-env"}, cm)
	if cm.Data["SENTRY_DSN"] == "" {
		t.Errorf("the new variable is not in the build variables: %v", cm.Data)
	}
	if got := strings.Join(names(), " "); got != "NODE_ENV=value FOO=value DATABASE_URL=ref" {
		t.Errorf("a new config var changed the Image (a build): %s", got)
	}

	// The next build (a redeploy) takes the new name, and is the Image's
	// update alone: a trigger too would build first without the name.
	last := &unstructured.Unstructured{}
	last.SetGroupVersionKind(KpackBuildGVK)
	last.SetNamespace("app-web1")
	last.SetName("web1-build-1")
	if err := c.Create(ctx, last); err != nil {
		t.Fatal(err)
	}
	_ = c.Get(ctx, types.NamespacedName{Namespace: "app-web1", Name: "web1"}, img)
	_ = unstructured.SetNestedField(img.Object, "web1-build-1", "status", "latestBuildRef")
	if err := c.Update(ctx, img); err != nil {
		t.Fatal(err)
	}
	cur := &shpyrdv1.App{}
	_ = c.Get(ctx, types.NamespacedName{Namespace: "app-web1", Name: "web1"}, cur)
	cur.Annotations = map[string]string{shpyrdv1.AnnotationRebuildAt: "2026-10-02T10:00:00Z"}
	if err := c.Update(ctx, cur); err != nil {
		t.Fatal(err)
	}
	runReconcile(t, r, cur)
	if got := strings.Join(names(), " "); got != "NODE_ENV=value FOO=value DATABASE_URL=ref SENTRY_DSN=ref" {
		t.Errorf("the redeploy's build env = %s", got)
	}
	_ = c.Get(ctx, types.NamespacedName{Namespace: "app-web1", Name: "web1-build-1"}, last)
	if last.GetAnnotations()[kpackBuildNeededAnnotation] != "" {
		t.Error("the redeploy triggered a build on the old names besides the update's")
	}
	// A redeploy with nothing new is the trigger.
	_ = c.Get(ctx, types.NamespacedName{Namespace: "app-web1", Name: "web1"}, cur)
	cur.Annotations[shpyrdv1.AnnotationRebuildAt] = "2026-10-02T11:00:00Z"
	if err := c.Update(ctx, cur); err != nil {
		t.Fatal(err)
	}
	runReconcile(t, r, cur)
	_ = c.Get(ctx, types.NamespacedName{Namespace: "app-web1", Name: "web1-build-1"}, last)
	if last.GetAnnotations()[kpackBuildNeededAnnotation] != "true" {
		t.Error("a redeploy with nothing new did not trigger a build")
	}
}

// A Git source is rebuilt by kpack on each commit without the Image
// changing: a new variable name joins the Image at once there.
func TestGitBuildTakesANewVariableAtOnce(t *testing.T) {
	ctx := context.Background()
	app := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "web1", Namespace: "app-web1", Generation: 1},
		Spec:       shpyrdv1.AppSpec{Source: &shpyrdv1.Source{Git: &shpyrdv1.GitSource{URL: "https://example.test/repo.git", Revision: "main"}}},
	}
	env := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "web1-env", Namespace: "app-web1"}, Data: map[string][]byte{"A": []byte("1")}}
	r, c := newTestReconciler(t, app, env)
	runReconcile(t, r, app)
	_ = c.Get(ctx, types.NamespacedName{Namespace: "app-web1", Name: "web1-env"}, env)
	env.Data["NEXT_PUBLIC_B"] = []byte("2")
	if err := c.Update(ctx, env); err != nil {
		t.Fatal(err)
	}
	runReconcile(t, r, app)
	img := &unstructured.Unstructured{}
	img.SetGroupVersionKind(KpackImageGVK)
	if err := c.Get(ctx, types.NamespacedName{Namespace: "app-web1", Name: "web1"}, img); err != nil {
		t.Fatal(err)
	}
	entries, _, _ := unstructured.NestedSlice(img.Object, "spec", "build", "env")
	if len(entries) != 2 || entries[1].(map[string]interface{})["name"] != "NEXT_PUBLIC_B" {
		t.Errorf("git build env = %v", entries)
	}
}
