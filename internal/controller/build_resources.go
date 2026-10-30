package controller

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// A build asks for its place on a machine. Build pods with no request at
// all were packed wherever the scheduler found room: two builds on one
// machine took four times as long as one alone (export 140 s against 23 s,
// production October 2026), and when the machine's disk was near its line
// both were evicted together. With a request the scheduler puts a build
// where the CPU, memory and disk are free, and the cluster autoscaler adds
// a machine when none has them. Requests only: a buildpack build has no
// ceiling of its own (the stack's tools decide), and the Dockerfile build
// keeps the limits it had.
const (
	// buildRequestCPU is one core: a build is mostly one process at a time
	// (an install, a compile, an image export).
	buildRequestCPU = "1"
	// buildRequestMemory covers the common Node and JVM builds; a build may
	// use more when the machine has it.
	buildRequestMemory = "2Gi"
	// buildRequestStorage is the disk a build writes: the source, the
	// restored cache and the layers of the image it exports. A machine
	// with less free keeps the build off, instead of evicting it halfway.
	buildRequestStorage = "5Gi"
)

// buildRequests is the request of a build's step containers.
func buildRequests() corev1.ResourceList {
	return corev1.ResourceList{
		corev1.ResourceCPU:              resource.MustParse(buildRequestCPU),
		corev1.ResourceMemory:           resource.MustParse(buildRequestMemory),
		corev1.ResourceEphemeralStorage: resource.MustParse(buildRequestStorage),
	}
}

// kpackBuildResources is the same request as kpack reads it on an Image:
// spec.build.resources, applied to every step of the build pod.
func kpackBuildResources() map[string]interface{} {
	return map[string]interface{}{
		"requests": map[string]interface{}{
			"cpu":               buildRequestCPU,
			"memory":            buildRequestMemory,
			"ephemeral-storage": buildRequestStorage,
		},
	}
}
