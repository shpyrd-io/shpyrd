package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/shpyrd-io/shpyrd/internal/scheduling"
)

// buildWaiting says, in words, why a build that has not started is waiting
// for a machine (#360): the workspace otherwise shows "Waiting for the build
// to start" for as long as it lasts, which was 6h40 once, with a free pod
// slot on one machine and the free CPU on the other and an autoscaler that
// believed the build fit. "" when the build's pod is placed, is not waiting
// for capacity, or cannot be read.
//
// The build is not cancelled: kpack would start the same build again at
// once, on the same full machines. It starts by itself when room is made,
// and the operator is alerted (PodUnschedulable).
func (r *AppReconciler) buildWaiting(ctx context.Context, b *unstructured.Unstructured) string {
	if r.Kube == nil || b == nil {
		return ""
	}
	pod, err := r.Kube.CoreV1().Pods(b.GetNamespace()).Get(ctx, b.GetName()+"-build-pod", metav1.GetOptions{})
	if err != nil {
		return ""
	}
	condition := scheduling.Blocked(pod)
	if condition == nil || !scheduling.Capacity(condition.Message) {
		return ""
	}
	since := pod.CreationTimestamp.Time
	if r.now().Sub(since) < unschedulableGrace {
		return r.waitingMessage("Waiting for room on a machine to build on (usually a few minutes).", since)
	}
	var latest *corev1.Event
	for _, e := range r.instanceEvents(ctx, pod) {
		if e.Reason == "TriggeredScaleUp" || e.Reason == "NotTriggerScaleUp" {
			ev := e
			if latest == nil || eventObservedAt(&ev).After(eventObservedAt(latest)) {
				latest = &ev
			}
		}
	}
	if latest != nil && latest.Reason == "TriggeredScaleUp" {
		return r.waitingMessage("Waiting for a new machine to build on (usually 2–4 minutes).", since)
	}
	return r.waitingMessage("The build is waiting for a machine with room for it, and the cluster is not adding one: a fault of the platform, not of the app. The operator is alerted; the build starts by itself when room is made.", since)
}
