package controller

import (
	corev1 "k8s.io/api/core/v1"
	"time"
)

// StartupFailure explains a refused app container, allowing a short retry
// window for image pulls. Check this before PodRunning: another container may
// be running while the app is still waiting.
func StartupFailure(pod *corev1.Pod, now time.Time) string {
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name == "app" && cs.State.Waiting != nil && failingReasons[cs.State.Waiting.Reason] && !imagePullGrace(pod, cs, now) {
			return waitingFailure(cs)
		}
	}
	return ""
}

func imagePullGrace(pod *corev1.Pod, cs corev1.ContainerStatus, now time.Time) bool {
	if cs.State.Waiting == nil {
		return false
	}
	reason := cs.State.Waiting.Reason
	return (reason == "ErrImagePull" || reason == "ImagePullBackOff") && !pod.CreationTimestamp.IsZero() && now.Sub(pod.CreationTimestamp.Time) < time.Minute
}
