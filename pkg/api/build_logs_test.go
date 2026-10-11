package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// #117: a build refused before it ever had an instance ends its followed
// log at once, and the builds say why: a deploy following it does not wait
// out the half hour the log would otherwise wait for an instance.
func TestFollowingARefusedBuildEnds(t *testing.T) {
	const why = "The build cannot start: the workspace's memory ceiling is 400Mi, 64Mi of it taken by web (64Mi), and it needs 512Mi more."
	app := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-shop"}, Status: shpyrdv1.AppStatus{LatestBuild: "shop-build-2"}}
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: "shop-build-2", Namespace: "app-shop",
			Labels:      map[string]string{shpyrdv1.LabelApp: "shop", shpyrdv1.LabelBuildNumber: "2"},
			Annotations: map[string]string{shpyrdv1.AnnotationBuildFailure: why},
		},
		Spec: batchv1.JobSpec{Suspend: ptr.To(true)},
	}
	s, _ := newTestServer(t, nil, []client.Object{app, job})
	done := make(chan int, 1)
	go func() {
		done <- do(t, s, "GET", "/api/projects/shop/builds/shop-build-2/logs?follow=true", "", true).Code
	}()
	select {
	case code := <-done:
		if code != http.StatusOK {
			t.Errorf("followed log of a refused build = %d", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("following a refused build's log is still waiting for its instance")
	}
	rec := do(t, s, "GET", "/api/projects/shop/builds", "", true)
	if !strings.Contains(rec.Body.String(), `"status":"Failed"`) || !strings.Contains(rec.Body.String(), "memory ceiling is 400Mi") {
		t.Errorf("builds = %s", rec.Body.String())
	}
}

func TestJobBuildInfoFailureWithoutPod(t *testing.T) {
	finished := metav1.NewTime(time.Now().UTC().Truncate(time.Second))
	for _, tc := range []struct {
		name, reason, message, annotation, want string
		status                                  corev1.ConditionStatus
	}{
		{name: "deadline without pod", reason: "DeadlineExceeded", status: corev1.ConditionTrue, want: "The build did not finish within 30 minutes."},
		{name: "other condition", reason: "BackoffLimitExceeded", message: "Job has reached the specified backoff limit", status: corev1.ConditionTrue, want: "Job has reached the specified backoff limit"},
		{name: "annotation wins", reason: "DeadlineExceeded", annotation: "The source could not be fetched.", status: corev1.ConditionTrue, want: "The source could not be fetched."},
		{name: "false condition", reason: "DeadlineExceeded", status: corev1.ConditionFalse},
		{name: "unknown condition", reason: "DeadlineExceeded", status: corev1.ConditionUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			job := batchv1.Job{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{shpyrdv1.AnnotationBuildFailure: tc.annotation}}, Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: tc.status, Reason: tc.reason, Message: tc.message, LastTransitionTime: finished}}}}
			got := jobBuildInfo(job)
			if tc.want == "" {
				if got.Status != "Building" || got.CompletedAt != nil {
					t.Fatalf("nonterminal build = %+v", got)
				}
			} else if got.Status != "Failed" || got.Message != tc.want || got.CompletedAt == nil || !got.CompletedAt.Equal(finished.Time) {
				t.Fatalf("failed build = %+v, want %q at %v", got, tc.want, finished)
			}
		})
	}
}
