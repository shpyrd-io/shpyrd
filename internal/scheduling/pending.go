// Package scheduling interprets current scheduler conditions, independently of metrics.
package scheduling

import (
	corev1 "k8s.io/api/core/v1"
	"strings"
)

// Blocked returns the current scheduling failure, excluding pods already placed,
// terminating or finished. A pending image pull is not a scheduling failure.
func Blocked(pod *corev1.Pod) *corev1.PodCondition {
	if pod.DeletionTimestamp != nil || pod.Spec.NodeName != "" || pod.Status.Phase != corev1.PodPending {
		return nil
	}
	for i := range pod.Status.Conditions {
		c := &pod.Status.Conditions[i]
		if c.Type == corev1.PodScheduled && c.Status == corev1.ConditionFalse {
			return c
		}
	}
	return nil
}

// Capacity reports an explicit resource shortage. Other constraints may also
// appear in the same message; this does not mean every node is full.
func Capacity(message string) bool {
	message = strings.ToLower(message)
	return strings.Contains(message, "insufficient ") || strings.Contains(message, "too many pods")
}
