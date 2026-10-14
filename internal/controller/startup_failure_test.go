package controller

import (
	"testing"
	"time"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func assertNonRootIdentity(t *testing.T, sc *corev1.SecurityContext, uid int64) {
	t.Helper()
	if !ptr.Deref(sc.RunAsNonRoot, false) || ptr.Deref(sc.AllowPrivilegeEscalation, true) || ptr.Deref(sc.RunAsUser, int64(-1)) != uid || ptr.Deref(sc.RunAsGroup, int64(-1)) != uid {
		t.Fatalf("security context = %+v", sc)
	}
}

func TestOneOffIdentityMatchesImage(t *testing.T) {
	app := &shpyrdv1.App{Status: shpyrdv1.AppStatus{ImageUsers: []shpyrdv1.ImageUser{{Image: "registry/app@sha256:old", UID: 1000, GID: 1000}, {Image: "registry/app@sha256:new", UID: 2000, GID: 2000}, {Image: "registry/app@sha256:root", UID: 0, GID: 0}}}}
	for _, tc := range []struct {
		image string
		uid   int64
	}{{"old", 1000}, {"new", 2000}, {"other", -1}, {"root", -1}} {
		pod := OneOffPod(app, "registry/app@sha256:"+tc.image, []string{"true"}, corev1.ResourceRequirements{}, false, false, "")
		assertNonRootIdentity(t, pod.Spec.Containers[0].SecurityContext, tc.uid)
	}
}

func TestStartupFailureGrace(t *testing.T) {
	now := time.Now()
	for _, reason := range []string{"CreateContainerConfigError", "CreateContainerError", "InvalidImageName", "RunContainerError", "CrashLoopBackOff", "ErrImagePull", "ImagePullBackOff"} {
		for _, age := range []time.Duration{10 * time.Second, 2 * time.Minute} {
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(now.Add(-age))}, Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Name: "app", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reason}}}}}}
			wantFailure := age > time.Minute || (reason != "ErrImagePull" && reason != "ImagePullBackOff")
			if got := StartupFailure(pod, now); (got != "") != wantFailure {
				t.Fatalf("%s age %v: %q", reason, age, got)
			}
		}
	}
}
