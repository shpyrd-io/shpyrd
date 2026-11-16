package controller

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

// #360: a build whose pod no machine takes says so, and for how long,
// instead of "Waiting for the build to start"; past the grace with no
// machine on its way it says the platform is at fault.
func TestBuildWaitingForAMachineIsSaidInWords(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	build := &unstructured.Unstructured{}
	build.SetGroupVersionKind(KpackBuildGVK)
	build.SetNamespace("app-shop")
	build.SetName("shop-build-29")
	pod := func(age time.Duration, condition string, nodeName string) *corev1.Pod {
		p := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "shop-build-29-build-pod", Namespace: "app-shop", UID: "build-pod-29", CreationTimestamp: metav1.NewTime(now.Add(-age))},
			Spec:       corev1.PodSpec{NodeName: nodeName},
			Status:     corev1.PodStatus{Phase: corev1.PodPending},
		}
		if condition != "" {
			p.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: "Unschedulable", Message: condition}}
		}
		return p
	}
	event := func(reason string) *corev1.Event {
		return &corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: reason, Namespace: "app-shop"}, InvolvedObject: corev1.ObjectReference{Name: "shop-build-29-build-pod", UID: "build-pod-29"}, Reason: reason, FirstTimestamp: metav1.NewTime(now.Add(-time.Minute)), LastTimestamp: metav1.NewTime(now)}
	}
	full := "0/7 nodes are available: 1 Insufficient cpu, 1 Too many pods, 5 node(s) didn't match Pod's node affinity/selector."
	for _, tc := range []struct {
		name  string
		objs  []runtime.Object
		words string // "" for no words
	}{
		{"no pod yet", nil, ""},
		{"placed", []runtime.Object{pod(time.Minute, "", "10.0.1.208")}, ""},
		{"not about room", []runtime.Object{pod(time.Hour, "pod has unbound immediate PersistentVolumeClaims", "")}, ""},
		{"young: room may come", []runtime.Object{pod(2 * time.Minute, full, "")}, "Waiting for room on a machine to build on (usually a few minutes). Waiting 2m0s."},
		{"a machine is on its way", []runtime.Object{pod(6 * time.Minute, full, ""), event("TriggeredScaleUp")}, "Waiting for a new machine to build on (usually 2–4 minutes). Waiting 6m0s."},
		{"the autoscaler believes it fits", []runtime.Object{pod(6*time.Hour + 40*time.Minute, full, "")}, "the cluster is not adding one: a fault of the platform, not of the app. The operator is alerted; the build starts by itself when room is made. Waiting 6h40m0s."},
		{"the autoscaler refuses", []runtime.Object{pod(20 * time.Minute, full, ""), event("NotTriggerScaleUp")}, "the cluster is not adding one"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := newTestReconciler(t)
			r.Now = func() time.Time { return now }
			r.Kube = kubefake.NewSimpleClientset(tc.objs...)
			got := r.buildWaiting(context.Background(), build)
			if (tc.words == "" && got != "") || (tc.words != "" && !strings.Contains(got, tc.words)) {
				t.Errorf("buildWaiting = %q, want %q", got, tc.words)
			}
		})
	}
	r, _ := newTestReconciler(t)
	if got := r.buildWaiting(context.Background(), nil); got != "" {
		t.Errorf("no build: %q", got)
	}
}
