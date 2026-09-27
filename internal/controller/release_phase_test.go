package controller

import (
	"context"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// TestReleasePhase: an image with a "release" process type runs it as a
// Job before the workloads change; the rollout waits; a failure leaves the
// previous release untouched; a success releases.
func TestReleasePhase(t *testing.T) {
	ctx := context.Background()
	image := "ghcr.io/example/rails@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	app := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "rails", Namespace: "app-rails", Generation: 1},
		Spec:       shpyrdv1.AppSpec{Image: image, Processes: map[string]shpyrdv1.Process{"web": {}}},
	}
	r, c := newTestReconciler(t, app)
	r.ProcessTypes = func(context.Context, string) []string { return []string{"web", "release"} }

	// 1. The first reconcile creates the release Job and no Deployment yet.
	got := runReconcile(t, r, app)
	if got.Status.Phase != shpyrdv1.PhaseDeploying || !strings.Contains(got.Status.Message, "release phase") {
		t.Fatalf("phase = %q %q, want Deploying with the release phase", got.Status.Phase, got.Status.Message)
	}
	if got.Status.Release == nil || got.Status.Release.State != shpyrdv1.ReleaseRunning {
		t.Fatalf("release status = %+v", got.Status.Release)
	}
	if len(got.Status.Releases) != 0 {
		t.Errorf("a release was recorded before the release command ran: %+v", got.Status.Releases)
	}
	jobs := &batchv1.JobList{}
	if err := c.List(ctx, jobs); err != nil || len(jobs.Items) != 1 {
		t.Fatalf("release jobs = %d %v", len(jobs.Items), err)
	}
	job := jobs.Items[0]
	ct := job.Spec.Template.Spec.Containers[0]
	if ct.Image != image || len(ct.Command) != 1 || ct.Command[0] != "/cnb/process/release" || job.Labels[shpyrdv1.LabelProcess] != "release" {
		t.Errorf("release job = image %s command %v labels %v", ct.Image, ct.Command, job.Labels)
	}
	if !hasEnv(ct.Env, "SHPYRD_RELEASE_PHASE", "1") {
		t.Errorf("release env = %v", ct.Env)
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "app-rails", Name: "rails-web"}, &appsv1.Deployment{}); !apierrors.IsNotFound(err) {
		t.Errorf("the web Deployment exists before the release command finished: %v", err)
	}

	// 2. The command fails: the project says so, still nothing rolls out.
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: "BackoffLimitExceeded", Message: "Job has reached the specified backoff limit"}}
	if err := c.Status().Update(ctx, &job); err != nil {
		t.Fatal(err)
	}
	got = runReconcile(t, r, got)
	if got.Status.Phase != shpyrdv1.PhaseFailed || !strings.Contains(got.Status.Message, "release command failed") {
		t.Fatalf("after failure: %q %q", got.Status.Phase, got.Status.Message)
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "app-rails", Name: "rails-web"}, &appsv1.Deployment{}); !apierrors.IsNotFound(err) {
		t.Error("a failed release command must not roll out")
	}

	// 2b. A redeploy after the failure runs the command again: the failed
	// Job goes and a fresh one takes its name on the next pass.
	if got.Annotations == nil {
		got.Annotations = map[string]string{}
	}
	got.Annotations[shpyrdv1.AnnotationRestartedAt] = time.Now().Add(time.Minute).UTC().Format(time.RFC3339)
	if err := c.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	got = runReconcile(t, r, got)
	if got.Status.Release == nil || got.Status.Release.State != shpyrdv1.ReleaseRunning || !strings.Contains(got.Status.Release.Message, "again") {
		t.Fatalf("after a redeploy of a failed release: %+v", got.Status.Release)
	}
	got = runReconcile(t, r, got)
	if err := c.List(ctx, jobs); err != nil || len(jobs.Items) != 1 || len(jobs.Items[0].Status.Conditions) != 0 {
		t.Fatalf("after the retry: jobs = %v (want one fresh %s without the failure)", names(jobs), job.Name)
	}
	job = jobs.Items[0]
	// It fails again without a newer redeploy: Failed stays.
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: "BackoffLimitExceeded"}}
	if err := c.Status().Update(ctx, &job); err != nil {
		t.Fatal(err)
	}
	got = runReconcile(t, r, got)
	if got.Status.Phase != shpyrdv1.PhaseFailed {
		t.Fatalf("second failure: %q %q", got.Status.Phase, got.Status.Message)
	}

	// 3. A new deploy (another image) gets a new Job; the old one is pruned.
	got.Spec.Image = strings.Replace(image, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210", 1)
	got.Generation = 2
	if err := c.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	got = runReconcile(t, r, got)
	if err := c.List(ctx, jobs); err != nil || len(jobs.Items) != 1 || jobs.Items[0].Name == job.Name {
		t.Fatalf("after a new deploy: jobs = %v", names(jobs))
	}
	// 4. It succeeds: the workloads roll out and v1 is recorded.
	next := jobs.Items[0]
	next.Status.Succeeded = 1
	next.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}
	if err := c.Status().Update(ctx, &next); err != nil {
		t.Fatal(err)
	}
	got = runReconcile(t, r, got)
	if got.Status.Release == nil || got.Status.Release.State != shpyrdv1.ReleaseSucceeded {
		t.Fatalf("release status after success = %+v", got.Status.Release)
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "app-rails", Name: "rails-web"}, &appsv1.Deployment{}); err != nil {
		t.Errorf("web Deployment after the release command: %v", err)
	}
	if len(got.Status.Releases) != 1 {
		t.Errorf("releases = %+v, want v1", got.Status.Releases)
	}
	if got.Status.ProcessTypes == nil || len(got.Status.ProcessTypes) != 2 {
		t.Errorf("process types = %v", got.Status.ProcessTypes)
	}

	// 5. Without a release type nothing gates: an image with only web.
	plain := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "plain", Namespace: "app-plain", Generation: 1}, Spec: shpyrdv1.AppSpec{Image: image}}
	r2, c2 := newTestReconciler(t, plain)
	r2.ProcessTypes = func(context.Context, string) []string { return []string{"web"} }
	got2 := runReconcile(t, r2, plain)
	if got2.Status.Release != nil || got2.Status.Phase != shpyrdv1.PhaseDeploying {
		t.Errorf("plain app: release=%+v phase=%s", got2.Status.Release, got2.Status.Phase)
	}
	if err := c2.Get(ctx, types.NamespacedName{Namespace: "app-plain", Name: "plain-web"}, &appsv1.Deployment{}); err != nil {
		t.Errorf("plain app must roll out at once: %v", err)
	}
}

func names(l *batchv1.JobList) []string {
	var out []string
	for _, j := range l.Items {
		out = append(out, j.Name)
	}
	return out
}
