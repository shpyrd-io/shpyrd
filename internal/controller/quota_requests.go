package controller

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

// QuotaRequestsFault names the first container of spec that a project's
// compute quota would refuse, "" when there is none (#117). A workspace
// with limits puts a quota on requests.cpu and requests.memory over every
// pod that asks for resources (NotBestEffort); Kubernetes then refuses
// such a pod when any of its containers, init containers included,
// declares no CPU or memory request. A pod that asks for nothing is
// outside the quota. Tests hold the pods the platform makes to it.
func QuotaRequestsFault(spec corev1.PodSpec) string {
	all := append(append([]corev1.Container{}, spec.InitContainers...), spec.Containers...)
	asks := false
	for _, c := range all {
		if len(c.Resources.Requests) > 0 || len(c.Resources.Limits) > 0 {
			asks = true
		}
	}
	if !asks {
		return ""
	}
	for _, c := range all {
		for _, res := range []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory} {
			if _, ok := c.Resources.Requests[res]; !ok {
				return fmt.Sprintf("container %q declares no %s request, so a workspace's quota refuses the pod", c.Name, res)
			}
		}
	}
	return ""
}
