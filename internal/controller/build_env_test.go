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

// A buildpack build sees the variables the app runs with (#54): they reach
// it through the Secret <app>-build-env, which the Image binds as a service
// and the build-env buildpack turns into build-time variables. The Image
// names the Secret, never a variable or a value, so a new config var
// changes the Secret and starts no build; a redeploy is one build.
func TestBuildSeesTheAppsVariables(t *testing.T) {
	ctx := context.Background()
	app := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "web1", Namespace: "app-web1", Generation: 1},
		Spec: shpyrdv1.AppSpec{
			Source: &shpyrdv1.Source{Blob: &shpyrdv1.BlobSource{URL: "http://shpyrd-server.shpyrd-system.svc:8082/api/sources/" + strings.Repeat("a", 64) + ".tgz", SHA256: strings.Repeat("a", 64)}},
			Build:  &shpyrdv1.Build{Env: []corev1.EnvVar{{Name: "NODE_ENV", Value: "production"}}},
		},
	}
	env := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "web1-env", Namespace: "app-web1"},
		Data:       map[string][]byte{"DATABASE_URL": []byte("postgres://secret@db/app"), "type": []byte("mine")},
	}
	r, c := newTestReconciler(t, app, env)
	runReconcile(t, r, app)

	vars := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "app-web1", Name: "web1-build-env"}, vars); err != nil {
		t.Fatal(err)
	}
	if string(vars.Data["DATABASE_URL"]) != "postgres://secret@db/app" || string(vars.Data["type"]) != "shpyrd-env" {
		t.Errorf("build variables = %v (a config var named type cannot take the binding's type)", vars.Data)
	}
	img := &unstructured.Unstructured{}
	img.SetGroupVersionKind(KpackImageGVK)
	get := func() {
		t.Helper()
		if err := c.Get(ctx, types.NamespacedName{Namespace: "app-web1", Name: "web1"}, img); err != nil {
			t.Fatal(err)
		}
	}
	get()
	raw, _ := json.Marshal(img.Object["spec"])
	if strings.Contains(string(raw), "secret@db") || strings.Contains(string(raw), "DATABASE_URL") {
		t.Fatalf("a variable sits in the Image: %s", raw)
	}
	if s := buildServices(img); len(s) != 1 || s[0].(map[string]interface{})["name"] != "web1-build-env" || s[0].(map[string]interface{})["kind"] != "Secret" {
		t.Errorf("services = %v", s)
	}
	spec := func() string { b, _ := json.Marshal(img.Object["spec"]); return string(b) }

	// A new config var: in the Secret at once; the Image does not move.
	// kpack writes defaults into the stored Image (seen on kind, kpack
	// 0.18): those are not a change either.
	_ = unstructured.SetNestedMap(img.Object, map[string]interface{}{}, "spec", "build", "resources")
	if err := c.Update(ctx, img); err != nil {
		t.Fatal(err)
	}
	get()
	before := img.DeepCopy()
	_ = c.Get(ctx, types.NamespacedName{Namespace: "app-web1", Name: "web1-env"}, env)
	env.Data["SENTRY_DSN"] = []byte("https://key@sentry.test/1")
	if err := c.Update(ctx, env); err != nil {
		t.Fatal(err)
	}
	runReconcile(t, r, app)
	_ = c.Get(ctx, types.NamespacedName{Namespace: "app-web1", Name: "web1-build-env"}, vars)
	if string(vars.Data["SENTRY_DSN"]) == "" {
		t.Errorf("the new variable is not in the build variables: %v", vars.Data)
	}
	get()
	if buildChanges(before, img) || !equalJSON(buildServices(before), buildServices(img)) {
		t.Errorf("a new config var changed what the Image builds (a build): %s", spec())
	}

	// A redeploy with nothing new is the trigger, one build.
	last := &unstructured.Unstructured{}
	last.SetGroupVersionKind(KpackBuildGVK)
	last.SetNamespace("app-web1")
	last.SetName("web1-build-1")
	if err := c.Create(ctx, last); err != nil {
		t.Fatal(err)
	}
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
	_ = c.Get(ctx, types.NamespacedName{Namespace: "app-web1", Name: "web1-build-1"}, last)
	if last.GetAnnotations()[kpackBuildNeededAnnotation] != "true" {
		t.Error("a redeploy did not trigger a build")
	}
}

// An Image made before builds read the project's variables binds no
// Secret: it binds it with its next build, not by starting one; a redeploy
// that binds it builds once, through the update alone.
func TestOlderImageBindsTheVariablesWithItsNextBuild(t *testing.T) {
	ctx := context.Background()
	app := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "web1", Namespace: "app-web1", Generation: 1},
		Spec:       shpyrdv1.AppSpec{Source: &shpyrdv1.Source{Blob: &shpyrdv1.BlobSource{URL: "http://shpyrd-server.shpyrd-system.svc:8082/api/sources/" + strings.Repeat("b", 64) + ".tgz"}}},
	}
	r, c := newTestReconciler(t, app)
	old, err := r.Config.desiredKpackImage(app)
	if err != nil {
		t.Fatal(err)
	}
	old = withServicesOf(old, &unstructured.Unstructured{Object: map[string]interface{}{}})
	_ = unstructured.SetNestedField(old.Object, "web1-build-1", "status", "latestBuildRef")
	if err := c.Create(ctx, old); err != nil {
		t.Fatal(err)
	}
	last := &unstructured.Unstructured{}
	last.SetGroupVersionKind(KpackBuildGVK)
	last.SetNamespace("app-web1")
	last.SetName("web1-build-1")
	if err := c.Create(ctx, last); err != nil {
		t.Fatal(err)
	}
	img := &unstructured.Unstructured{}
	img.SetGroupVersionKind(KpackImageGVK)
	runReconcile(t, r, app)
	_ = c.Get(ctx, types.NamespacedName{Namespace: "app-web1", Name: "web1"}, img)
	if len(buildServices(img)) != 0 {
		t.Fatal("the upgrade bound the Secret, which starts a build")
	}
	cur := &shpyrdv1.App{}
	_ = c.Get(ctx, types.NamespacedName{Namespace: "app-web1", Name: "web1"}, cur)
	cur.Annotations = map[string]string{shpyrdv1.AnnotationRebuildAt: "2026-10-02T10:00:00Z"}
	if err := c.Update(ctx, cur); err != nil {
		t.Fatal(err)
	}
	runReconcile(t, r, cur)
	_ = c.Get(ctx, types.NamespacedName{Namespace: "app-web1", Name: "web1"}, img)
	if len(buildServices(img)) != 1 {
		t.Error("the redeploy did not bind the Secret")
	}
	_ = c.Get(ctx, types.NamespacedName{Namespace: "app-web1", Name: "web1-build-1"}, last)
	if last.GetAnnotations()[kpackBuildNeededAnnotation] != "" {
		t.Error("the redeploy triggered a build besides the update's (two builds, the first without the variables)")
	}
}

// A Git source is built by kpack on each commit without the Image
// changing: an older Image binds the Secret at once there.
func TestGitImageBindsTheVariablesAtOnce(t *testing.T) {
	ctx := context.Background()
	app := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "web1", Namespace: "app-web1", Generation: 1},
		Spec:       shpyrdv1.AppSpec{Source: &shpyrdv1.Source{Git: &shpyrdv1.GitSource{URL: "https://example.test/repo.git", Revision: "main"}}},
	}
	r, c := newTestReconciler(t, app)
	old, err := r.Config.desiredKpackImage(app)
	if err != nil {
		t.Fatal(err)
	}
	old = withServicesOf(old, &unstructured.Unstructured{Object: map[string]interface{}{}})
	if err := c.Create(ctx, old); err != nil {
		t.Fatal(err)
	}
	runReconcile(t, r, app)
	img := &unstructured.Unstructured{}
	img.SetGroupVersionKind(KpackImageGVK)
	_ = c.Get(ctx, types.NamespacedName{Namespace: "app-web1", Name: "web1"}, img)
	if len(buildServices(img)) != 1 {
		t.Error("a Git Image did not bind the Secret")
	}
}
