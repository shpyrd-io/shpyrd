package controller

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/ptr"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/audit"
)

// A release's outcome is audited in the name of whoever asked for the
// deploy (the annotations the API leaves), once when it reaches Running,
// and again as release.failed when the app turns Failed (RFC-0022a).
func TestReleaseOutcomeIsAuditedOnce(t *testing.T) {
	app := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "web1", Namespace: "app-web1", Generation: 1, Annotations: map[string]string{
			shpyrdv1.AnnotationDeployedBy:      "ana@acme.test (token laptop)",
			shpyrdv1.AnnotationDeployedSubject: "person-1",
			shpyrdv1.AnnotationDeployedClient:  "cli",
		}},
		Spec: shpyrdv1.AppSpec{
			Image:     "ghcr.io/example/app@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			Processes: map[string]shpyrdv1.Process{"web": {Replicas: ptr.To[int32](1)}},
		},
	}
	r, c := newTestReconciler(t, app)
	r.Kube = k8sfake.NewSimpleClientset()
	var heard []audit.Entry
	audit.Sink = func(_ audit.Ref, e audit.Entry) { heard = append(heard, e) }
	t.Cleanup(func() { audit.Sink = nil })

	// Deploying: nothing to say yet.
	got := runReconcile(t, r, app)
	if got.Status.Phase != shpyrdv1.PhaseDeploying || len(heard) != 0 {
		t.Fatalf("phase = %q, heard %d entries", got.Status.Phase, len(heard))
	}

	// Running: release.succeeded, in the deployer's name, v1.
	markDeploymentReady(t, c, "app-web1", "web1-web", 1)
	got = runReconcile(t, r, app)
	if got.Status.Phase != shpyrdv1.PhaseRunning {
		t.Fatalf("phase = %q (%s)", got.Status.Phase, got.Status.Message)
	}
	if len(heard) != 1 {
		t.Fatalf("heard %+v, want one release.succeeded", heard)
	}
	e := heard[0]
	if e.Action != "release.succeeded" || e.Target != "v1" || e.Actor != "ana@acme.test (token laptop)" || e.Subject != "person-1" || e.Client != "cli" || e.Via != "controller" || e.Detail != "Initial deploy" {
		t.Errorf("entry = %+v", e)
	}
	if got.Annotations[shpyrdv1.AnnotationAuditedRelease] != "1" || got.Annotations[shpyrdv1.AnnotationDeployedBy] != "" {
		t.Errorf("annotations after the audit = %v", got.Annotations)
	}

	// Still Running on the next reconcile: not audited twice.
	got = runReconcile(t, r, app)
	if got.Status.Phase != shpyrdv1.PhaseRunning || len(heard) != 1 {
		t.Fatalf("phase = %q, heard %d", got.Status.Phase, len(heard))
	}

	// A failure is audited when the phase turns Failed, by the platform
	// now that the deployer's annotations were consumed.
	evs, err := r.Kube.CoreV1().Events("app-web1").List(context.Background(), metav1.ListOptions{})
	if err != nil || len(evs.Items) != 1 {
		t.Fatalf("audit events = %d, %v", len(evs.Items), err)
	}
	failed := got.DeepCopy()
	failed.Status.Phase = shpyrdv1.PhaseFailed
	failed.Status.Message = "instances failing: crash loop"
	r.auditReleaseOutcome(context.Background(), got, failed)
	if len(heard) != 2 || heard[1].Action != "release.failed" || heard[1].Target != "v1" || heard[1].Actor != "platform" || heard[1].Detail != "instances failing: crash loop" {
		t.Fatalf("heard %+v", heard)
	}
	// Failed again, from Failed: silence.
	r.auditReleaseOutcome(context.Background(), failed, failed)
	if len(heard) != 2 {
		t.Fatalf("a failure that persists must not be audited again: %+v", heard)
	}
}

// An app found Running by a fresh controller (an upgrade) is not
// back-filled: only the transition into Running is a success.
func TestReleaseOutcomeIsNotBackfilled(t *testing.T) {
	r, _ := newTestReconciler(t)
	r.Kube = k8sfake.NewSimpleClientset()
	audit.Sink = func(audit.Ref, audit.Entry) { t.Fatal("must not be called") }
	t.Cleanup(func() { audit.Sink = nil })
	running := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "p-a"}, Status: shpyrdv1.AppStatus{Phase: shpyrdv1.PhaseRunning, Releases: []shpyrdv1.Release{{Number: 3}}}}
	r.auditReleaseOutcome(context.Background(), running, running)
}

// Without a clientset the controller records nothing, as on a reconciler
// built without one (tests, tools).
func TestReleaseOutcomeNeedsAClientset(t *testing.T) {
	r, _ := newTestReconciler(t)
	audit.Sink = func(audit.Ref, audit.Entry) { t.Fatal("must not be called") }
	t.Cleanup(func() { audit.Sink = nil })
	app := &shpyrdv1.App{Status: shpyrdv1.AppStatus{Phase: shpyrdv1.PhaseFailed}}
	r.auditReleaseOutcome(context.Background(), &shpyrdv1.App{}, app)
}
