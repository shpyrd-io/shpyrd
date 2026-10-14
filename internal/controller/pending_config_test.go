package controller

import (
	"context"
	"testing"
	"time"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/configvars"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestConfigWaitsForDeployAcrossReconciles(t *testing.T) {
	ctx := context.Background()
	app := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "app-web", Generation: 1}, Spec: shpyrdv1.AppSpec{Image: "registry/app@sha256:one", Processes: map[string]shpyrdv1.Process{"web": {}}}}
	active := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: app.EnvSecretName(), Namespace: app.Namespace}, Data: map[string][]byte{"GREETING": []byte("old")}}
	r, c := newTestReconciler(t, app, active)
	app = runReconcile(t, r, app)
	before := app.CurrentRelease().Number
	deployment := &appsv1.Deployment{}
	key := client.ObjectKey{Namespace: app.Namespace, Name: "web-web"}
	if err := c.Get(ctx, key, deployment); err != nil {
		t.Fatal(err)
	}
	beforeTemplate := deployment.Spec.Template.DeepCopy()
	pending := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: configvars.PendingName(app), Namespace: app.Namespace, Annotations: map[string]string{configvars.AnnotationPendingBase: configvars.DeployToken(app)}}, Data: map[string][]byte{"GREETING": []byte("new")}}
	if err := configvars.ApplyKind(pending, map[string]string{"APP_URL": "https://example.test"}, nil, nil, true, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := c.Create(ctx, pending); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		app = runReconcile(t, r, app)
	}
	if app.CurrentRelease().Number != before {
		t.Fatal("saving created a release")
	}
	if err := c.Get(ctx, key, deployment); err != nil {
		t.Fatal(err)
	}
	if !equalJSON(beforeTemplate, deployment.Spec.Template) {
		t.Fatal("saving changed the pod template")
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(active), active); err != nil {
		t.Fatal(err)
	}
	if string(active.Data["GREETING"]) != "old" {
		t.Fatal("replacement pods would see pending values")
	}
	app.Annotations = mergeMaps(app.Annotations, map[string]string{shpyrdv1.AnnotationRestartedAt: "deploy-now"})
	if err := c.Update(ctx, app); err != nil {
		t.Fatal(err)
	}
	app = runReconcile(t, r, app)
	if app.CurrentRelease().Number != before+1 {
		t.Fatalf("no new release: %+v", app.Status.Releases)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(active), active); err != nil {
		t.Fatal(err)
	}
	if string(active.Data["GREETING"]) != "new" || !configvars.Plain(active, "APP_URL") {
		t.Fatal("redeploy did not apply saved configuration")
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(pending), pending); !apierrors.IsNotFound(err) {
		t.Fatalf("draft not consumed: %v", err)
	}
	snapshot := &corev1.Secret{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: app.Namespace, Name: app.ReleaseSnapshotName(before + 1)}, snapshot); err != nil {
		t.Fatal(err)
	}
	if string(snapshot.Data["GREETING"]) != "new" || !configvars.Plain(snapshot, "APP_URL") {
		t.Fatal("snapshot lost values or visibility")
	}
}

func TestRollbackKeepsPendingEditsForNextDeploy(t *testing.T) {
	ctx := context.Background()
	app := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "app-web", Annotations: map[string]string{shpyrdv1.AnnotationRollbackTo: "1"}}, Spec: shpyrdv1.AppSpec{Image: "registry/app@sha256:old"}, Status: shpyrdv1.AppStatus{Releases: []shpyrdv1.Release{{Number: 2}}}}
	pending := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: configvars.PendingName(app), Namespace: app.Namespace, Annotations: map[string]string{configvars.AnnotationPendingBase: "previous-deployment"}}, Data: map[string][]byte{"A": []byte("saved")}}
	r, _ := newTestReconciler(t, app, pending)
	if got, err := r.pendingConfig(ctx, app); err != nil || got != nil {
		t.Fatalf("rollback selected draft: %v %v", got, err)
	}
	delete(app.Annotations, shpyrdv1.AnnotationRollbackTo)
	if got, err := r.pendingConfig(ctx, app); err != nil || got != nil {
		t.Fatalf("post-rollback selected draft: %v %v", got, err)
	}
	app.Annotations[shpyrdv1.AnnotationRestartedAt] = "next-deploy"
	got, err := r.pendingConfig(ctx, app)
	if err != nil || got == nil || string(got.Data["A"]) != "saved" {
		t.Fatalf("lost pending edits: %v %v", got, err)
	}
}

type editDuringDelete struct {
	client.Client
	app *shpyrdv1.App
}

func (c *editDuringDelete) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	if obj.GetName() == configvars.PendingName(c.app) {
		newer := &corev1.Secret{}
		if err := c.Client.Get(ctx, client.ObjectKeyFromObject(obj), newer); err != nil {
			return err
		}
		newer.Data["A"] = []byte("saved-during-deploy")
		newer.Annotations[configvars.AnnotationPendingBase] = configvars.DeployToken(c.app)
		if err := c.Client.Update(ctx, newer); err != nil {
			return err
		}
	}
	return c.Client.Delete(ctx, obj, opts...)
}

func TestConcurrentEditSurvivesConfigPromotion(t *testing.T) {
	ctx := context.Background()
	app := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "app-web"}, Status: shpyrdv1.AppStatus{Releases: []shpyrdv1.Release{{Number: 1}}}}
	pending := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: configvars.PendingName(app), Namespace: app.Namespace, Annotations: map[string]string{configvars.AnnotationPendingBase: "old"}}, Data: map[string][]byte{"A": []byte("chosen-for-deploy")}}
	r, c := newTestReconciler(t, app, pending)
	chosen, err := r.pendingConfig(ctx, app)
	if err != nil {
		t.Fatal(err)
	}
	r.Client = &editDuringDelete{Client: c, app: app}
	active, err := r.applyPendingConfig(ctx, app, chosen)
	if err != nil {
		t.Fatal(err)
	}
	if string(active.Data["A"]) != "chosen-for-deploy" {
		t.Fatal("deployment included a later edit")
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(pending), pending); err != nil {
		t.Fatal(err)
	}
	if string(pending.Data["A"]) != "saved-during-deploy" {
		t.Fatal("concurrent edit lost")
	}
	if next, err := r.pendingConfig(ctx, app); err != nil || next != nil {
		t.Fatalf("concurrent edit applied without another deploy: %v %v", next, err)
	}
}
