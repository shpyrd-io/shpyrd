package controller

import (
	"fmt"
	"path"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// A process's volumes on a pod that is not the process (#126): a one-off
// run asked to see them (`shpyrd run --process web`, `shpyrd shell
// --debug`), or the release step when the release process declares them.
// The pod gets the claims at the process's paths, and, for a
// single-instance volume, the machine where the claim is held right now,
// so the two pods share the attached disk (a block volume attaches to one
// machine; a node-local one never leaves its node).

// ProcessMount is one volume of a process as a pod mounts it.
type ProcessMount struct {
	Name   string
	Path   string
	Claim  string
	Shared bool
}

// ProcessMounts lists the volumes a process mounts, from the spec and the
// project's Volume objects (by name). Unknown volumes are an error, as
// they are for the process itself.
func ProcessMounts(app *shpyrdv1.App, process string, volumes map[string]*shpyrdv1.Volume) ([]ProcessMount, error) {
	p, ok := app.Spec.Processes[process]
	if !ok {
		return nil, fmt.Errorf("the project has no process %q", process)
	}
	var out []ProcessMount
	for _, m := range p.Volumes {
		vol := volumes[m.Name]
		if vol == nil {
			return nil, fmt.Errorf("the volume %q the process %s mounts does not exist", m.Name, process)
		}
		out = append(out, ProcessMount{Name: m.Name, Path: path.Clean(m.Path), Claim: vol.PVCName(), Shared: vol.Shared()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ClaimHolderNode is the machine where a single-instance claim of the
// mounts is held right now: the node of a pod of the namespace that mounts
// it and has one. Empty when none does (the disk is free to attach
// anywhere) or when every mount is shared.
func ClaimHolderNode(pods []corev1.Pod, mounts []ProcessMount) string {
	claims := map[string]bool{}
	for _, m := range mounts {
		if !m.Shared {
			claims[m.Claim] = true
		}
	}
	if len(claims) == 0 {
		return ""
	}
	for _, pod := range pods {
		if pod.Spec.NodeName == "" || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		for _, v := range pod.Spec.Volumes {
			if v.PersistentVolumeClaim != nil && claims[v.PersistentVolumeClaim.ClaimName] {
				return pod.Spec.NodeName
			}
		}
	}
	return ""
}

// MountVolumesOnPod gives a pod spec the mounts at their paths, the group
// that lets any image user write the disk (as the process's pods have),
// and, when node is set, the machine to run on.
func MountVolumesOnPod(spec *corev1.PodSpec, mounts []ProcessMount, node string) {
	if len(mounts) == 0 {
		return
	}
	for _, m := range mounts {
		spec.Volumes = append(spec.Volumes, corev1.Volume{Name: "vol-" + m.Name, VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: m.Claim},
		}})
		if len(spec.Containers) > 0 {
			spec.Containers[0].VolumeMounts = append(spec.Containers[0].VolumeMounts, corev1.VolumeMount{Name: "vol-" + m.Name, MountPath: m.Path})
		}
	}
	if spec.SecurityContext == nil {
		spec.SecurityContext = &corev1.PodSecurityContext{}
	}
	spec.SecurityContext.FSGroup = ptr.To(VolumeGroup)
	spec.SecurityContext.FSGroupChangePolicy = ptr.To(corev1.FSGroupChangeOnRootMismatch)
	if node != "" {
		if spec.NodeSelector == nil {
			spec.NodeSelector = map[string]string{}
		}
		spec.NodeSelector[corev1.LabelHostname] = node
	}
}

// MountsNote says what a pod got, for the person who asked: "with the
// volumes of web (data at /data), on 10.0.1.38".
func MountsNote(process string, mounts []ProcessMount, node string) string {
	if len(mounts) == 0 {
		return fmt.Sprintf("the process %s mounts no volume", process)
	}
	parts := make([]string, 0, len(mounts))
	for _, m := range mounts {
		parts = append(parts, m.Name+" at "+m.Path)
	}
	note := fmt.Sprintf("with the volumes of %s (%s)", process, strings.Join(parts, ", "))
	if node != "" {
		note += ", on " + node + ", the machine that holds the disk"
	}
	return note
}
