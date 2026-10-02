package controller

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kubefake "k8s.io/client-go/kubernetes/fake"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// What production showed on 2026-10-02 (#52): the free plan's 256Mi taken
// by the database, the release step and then web refused by the quota,
// the app saying "running" for an hour. The app says, in words, what
// cannot start, what takes the ceiling and what is missing.
func TestQuotaRefusalsInWords(t *testing.T) {
	ctx := context.Background()
	image := "ghcr.io/example/next@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	app := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "next", Namespace: "p-next", Generation: 1},
		Spec:       shpyrdv1.AppSpec{Image: image, Processes: map[string]shpyrdv1.Process{"web": {Size: "shared-m"}}},
	}
	db := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "db-1", Namespace: "p-next", Labels: map[string]string{"cnpg.io/cluster": "db"}},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "postgres", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("256Mi")}}}}},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	}
	r, c := newTestReconciler(t, app, db)
	r.ProcessTypes = func(context.Context, string) []string { return []string{"web", "release"} }
	kube := kubefake.NewSimpleClientset()
	r.Kube = kube

	// The release Job exists, its pod refused.
	got := runReconcile(t, r, app)
	var jobs batchv1.JobList
	if err := c.List(ctx, &jobs); err != nil || len(jobs.Items) != 1 {
		t.Fatalf("jobs = %v %v", jobs.Items, err)
	}
	job := jobs.Items[0]
	refused := `Error creating: pods "` + job.Name + `-x7k2p" is forbidden: exceeded quota: shpyrd, requested: requests.memory=64Mi, used: requests.memory=256Mi, limited: requests.memory=256Mi`
	if _, err := kube.CoreV1().Events("p-next").Create(ctx, &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Name: "refused", Namespace: "p-next"},
		InvolvedObject: corev1.ObjectReference{Kind: "Job", Name: job.Name, Namespace: "p-next"},
		Reason:         "FailedCreate", Message: refused, LastTimestamp: metav1.Now(),
	}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	got = runReconcile(t, r, got)
	want := "The release step cannot start: the workspace's memory ceiling, 256Mi, is all taken by the database db (256Mi), and it needs 64Mi more. Give the database a smaller size or remove it"
	if !strings.HasPrefix(got.Status.Message, want) || readyReason(got) != "QuotaExceeded" {
		t.Fatalf("release refused = %q (%s)", got.Status.Message, readyReason(got))
	}
	if bad := PlatformWordingFault(got.Status.Message); bad != "" {
		t.Errorf("%q names %q", got.Status.Message, bad)
	}

	// Given up at its deadline, it still says why.
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: batchv1.JobReasonDeadlineExceeded, Message: "Job was active longer than specified deadline"}}
	if err := c.Status().Update(ctx, &job); err != nil {
		t.Fatal(err)
	}
	got = runReconcile(t, r, got)
	if got.Status.Phase != shpyrdv1.PhaseFailed || !strings.Contains(got.Status.Message, "given up after 30 minutes without starting") || !strings.Contains(got.Status.Message, "all taken by the database db") || strings.Contains(got.Status.Message, "deadline") {
		t.Errorf("release given up = %s %q", got.Status.Phase, got.Status.Message)
	}

	// The release step done, web's instances are refused in turn.
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}
	job.Status.Succeeded = 1
	if err := c.Status().Update(ctx, &job); err != nil {
		t.Fatal(err)
	}
	got = runReconcile(t, r, got)
	d := &appsv1.Deployment{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "p-next", Name: workloadName(app, "web")}, d); err != nil {
		t.Fatal(err)
	}
	d.Status.ObservedGeneration = d.Generation
	d.Status.Conditions = []appsv1.DeploymentCondition{{Type: appsv1.DeploymentReplicaFailure, Status: corev1.ConditionTrue, Reason: "FailedCreate",
		Message: `pods "web-5cb4bdc597-89prn" is forbidden: exceeded quota: shpyrd, requested: requests.memory=256Mi, used: requests.memory=256Mi, limited: requests.memory=256Mi`}}
	if err := c.Status().Update(ctx, d); err != nil {
		t.Fatal(err)
	}
	got = runReconcile(t, r, got)
	if !strings.HasPrefix(got.Status.Message, "New instances of web cannot start: the workspace's memory ceiling, 256Mi, is all taken by the database db (256Mi), and they need 256Mi more.") || readyReason(got) != "QuotaExceeded" {
		t.Errorf("web refused = %s %q (%s)", got.Status.Phase, got.Status.Message, readyReason(got))
	}
}

func readyReason(app *shpyrdv1.App) string {
	for _, c := range app.Status.Conditions {
		if c.Type == shpyrdv1.ConditionReady {
			return c.Reason
		}
	}
	return ""
}
