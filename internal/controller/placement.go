package controller

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sort"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

const AnnotationProcessNodes = "shpyrd.io/process-nodes"
const AnnotationDataMove = "shpyrd.io/data-move"

// AnnotationStorageMigration marks a database whose data is moving to the
// profile's storage class by switchover (storage plan, step 1): its Cluster
// is rendered on that class at its current size (never rounded until the
// move is done), awake, and with its Service kept while it briefly runs two
// instances. The value is the target class. A node pin is left as it is.
const AnnotationStorageMigration = "shpyrd.io/storage-migration"

// PlacementGroups is the transitive closure of processes sharing volumes.
// Moving any member requires moving every member. Stateless processes remain
// independent; unmounted volumes can also move on their own.
type PlacementGroup struct {
	ID        string   `json:"id"`
	Processes []string `json:"processes"`
	Volumes   []string `json:"volumes"`
}

func PlacementGroups(app *shpyrdv1.App, volumeNames []string) []PlacementGroup {
	parent := map[string]string{}
	var root func(string) string
	root = func(s string) string {
		if parent[s] == "" {
			parent[s] = s
		}
		if parent[s] != s {
			parent[s] = root(parent[s])
		}
		return parent[s]
	}
	join := func(a, b string) {
		a, b = root(a), root(b)
		if a < b {
			parent[b] = a
		} else {
			parent[a] = b
		}
	}
	for process, spec := range app.EffectiveProcesses() {
		root("process:" + process)
		for _, v := range spec.Volumes {
			join("process:"+process, "volume:"+v.Name)
		}
	}
	for _, name := range volumeNames {
		root("volume:" + name)
	}
	groups := map[string]*PlacementGroup{}
	for name := range parent {
		key := root(name)
		if groups[key] == nil {
			groups[key] = &PlacementGroup{ID: key, Processes: []string{}, Volumes: []string{}}
		}
		if name[:7] == "volume:" {
			groups[key].Volumes = append(groups[key].Volumes, name[7:])
		} else {
			groups[key].Processes = append(groups[key].Processes, name[8:])
		}
	}
	out := []PlacementGroup{}
	for _, group := range groups {
		sort.Strings(group.Processes)
		sort.Strings(group.Volumes)
		out = append(out, *group)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func ProcessNodes(app *shpyrdv1.App) map[string]string {
	nodes := map[string]string{}
	_ = json.Unmarshal([]byte(app.Annotations[AnnotationProcessNodes]), &nodes)
	return nodes
}
func (c Config) processNodeSelector(app *shpyrdv1.App, process string) map[string]string {
	selector := c.appsNodeSelector()
	if node := ProcessNodes(app)[process]; node != "" {
		if selector == nil {
			selector = map[string]string{}
		}
		selector[corev1.LabelHostname] = node
	}
	return selector
}

// A process whose mount is on a node's disk (the storage-local class) stays
// on that node and keeps the node: a placement-group label, a required
// self-matching pod affinity on the hostname, and safe-to-evict=false for
// the autoscaler. A shared local volume still allows several replicas, all
// on its node: the affinity lets the first pod choose the node and then
// serializes the rest of the connected volume group there. The gate is the
// claim's class, not the profile's (RFC-0060): a process on block storage
// moves with its disk and needs none of this, and a stale gate from before
// is cleared.
func localVolumeAffinity(app *shpyrdv1.App, process string, pod *corev1.PodTemplateSpec, mounts []resolvedMount) {
	if pod.Labels["shpyrd.io/placement-group"] != "" {
		delete(pod.Labels, "shpyrd.io/placement-group")
		delete(pod.Annotations, "cluster-autoscaler.kubernetes.io/safe-to-evict")
		pod.Spec.Affinity = nil
	}
	if !anyLocal(mounts) {
		return
	}
	for _, group := range PlacementGroups(app, nil) {
		if len(group.Volumes) == 0 {
			continue
		}
		for _, member := range group.Processes {
			if member != process {
				continue
			}
			value := fmt.Sprintf("%x", sha256.Sum256([]byte(app.Name+"/"+group.ID)))[:32]
			if pod.Labels == nil {
				pod.Labels = map[string]string{}
			}
			pod.Labels["shpyrd.io/placement-group"] = value
			if pod.Annotations == nil {
				pod.Annotations = map[string]string{}
			}
			pod.Annotations["cluster-autoscaler.kubernetes.io/safe-to-evict"] = "false"
			pod.Spec.Affinity = &corev1.Affinity{PodAffinity: &corev1.PodAffinity{RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{{LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"shpyrd.io/placement-group": value}}, TopologyKey: corev1.LabelHostname}}}}
			return
		}
	}
}
