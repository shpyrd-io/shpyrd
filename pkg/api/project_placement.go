package api

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/internal/controller"
	"github.com/shpyrd-io/shpyrd/pkg/install"
	"github.com/shpyrd-io/shpyrd/pkg/prom"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type placementGroup struct {
	controller.PlacementGroup
	NeedsMigration  bool     `json:"needsMigration,omitempty"`
	StorageClasses  []string `json:"storageClasses,omitempty"`
	Database        string   `json:"database,omitempty"`
	Nodes           []string `json:"nodes"`
	Pool            string   `json:"pool"`
	CPURequested    *int64   `json:"cpuRequestedMillicores,omitempty"`
	MemoryRequested *int64   `json:"memoryRequestedBytes,omitempty"`
	DiskUsed        *float64 `json:"diskUsedBytes,omitempty"`
	Claims          []string `json:"-"`
}
type placementNode struct {
	Name            string   `json:"name"`
	Hostname        string   `json:"hostname"`
	Pool            string   `json:"pool"`
	Eligible        bool     `json:"eligible"`
	Reason          string   `json:"reason,omitempty"`
	Architecture    string   `json:"architecture"`
	CPU             int64    `json:"cpuMillicores"`
	Memory          int64    `json:"memoryBytes"`
	CPURequested    int64    `json:"cpuRequestedMillicores"`
	MemoryRequested int64    `json:"memoryRequestedBytes"`
	DiskAvailable   *float64 `json:"diskAvailableBytes,omitempty"`
}

func eligiblePlacementNode(node *corev1.Node) string {
	if node.Spec.Unschedulable {
		return "Node is cordoned"
	}
	ready := false
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
			ready = true
		}
		if (condition.Type == corev1.NodeDiskPressure || condition.Type == corev1.NodeMemoryPressure || condition.Type == corev1.NodePIDPressure) && condition.Status == corev1.ConditionTrue {
			return string(condition.Type)
		}
	}
	if !ready {
		return "Node is not ready"
	}
	for _, taint := range node.Spec.Taints {
		if taint.Effect == corev1.TaintEffectNoSchedule || taint.Effect == corev1.TaintEffectNoExecute {
			return "Node has a scheduling taint: " + taint.Key
		}
	}
	if node.Labels[corev1.LabelHostname] == "" {
		return "Node has no hostname label"
	}
	return ""
}
func (s *Server) projectPlacement(ctx context.Context, app *shpyrdv1.App) ([]placementGroup, []placementNode, error) {
	var volumes shpyrdv1.VolumeList
	if err := s.apps.List(ctx, &volumes, client.InNamespace(app.Namespace)); err != nil {
		return nil, nil, err
	}
	names := []string{}
	for _, v := range volumes.Items {
		names = append(names, v.Name)
	}
	groups := []placementGroup{}
	for _, g := range controller.PlacementGroups(app, names) {
		groups = append(groups, placementGroup{PlacementGroup: g, Pool: s.vars("SHPYRD_APPS_POOL"), Nodes: []string{}})
	}
	var databases shpyrdv1.PostgresList
	if err := s.apps.List(ctx, &databases, client.InNamespace(app.Namespace)); err != nil {
		return nil, nil, err
	}
	for _, pg := range databases.Items {
		groups = append(groups, placementGroup{PlacementGroup: controller.PlacementGroup{ID: "postgres:" + pg.Name, Processes: []string{}, Volumes: []string{}}, Database: pg.Name, Pool: install.ProjectDataPool(s.vars), Nodes: []string{}})
	}
	var pods corev1.PodList
	if err := s.apps.List(ctx, &pods); err != nil {
		return nil, nil, err
	}
	var claims corev1.PersistentVolumeClaimList
	if err := s.apps.List(ctx, &claims, client.InNamespace(app.Namespace)); err != nil {
		return nil, nil, err
	}
	for i := range groups {
		group := &groups[i]
		for _, name := range group.Volumes {
			group.Claims = append(group.Claims, shpyrdv1.PVCPrefix+name)
		}
		if group.Database != "" {
			for _, claim := range claims.Items {
				if claim.Labels["cnpg.io/cluster"] == group.Database {
					group.Claims = append(group.Claims, claim.Name)
				}
			}
		}
		classes := map[string]bool{}
		for _, name := range group.Claims {
			for _, claim := range claims.Items {
				if claim.Name == name {
					class := ""
					if claim.Spec.StorageClassName != nil {
						class = *claim.Spec.StorageClassName
					}
					classes[class] = true
					if class != controller.LocalStorageClass {
						group.NeedsMigration = true
					}
				}
			}
		}
		for class := range classes {
			group.StorageClasses = append(group.StorageClasses, class)
		}
		sort.Strings(group.StorageClasses)
		if len(group.Claims) == 0 && group.Database == "" {
			zero := float64(0)
			group.DiskUsed = &zero
		}
		var cpu, memory int64
		seen := map[string]bool{}
		for _, pod := range pods.Items {
			if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed || !placementPodMatches(app, *group, &pod) {
				continue
			}
			c, m := placementPodRequests(&pod.Spec)
			cpu += c
			memory += m
			if group.Database != "" {
				seen[group.Database] = true
			} else {
				seen[pod.Labels[shpyrdv1.LabelProcess]] = true
			}
		}
		complete := true
		for _, process := range group.Processes {
			if !seen[process] {
				complete = false
			}
		}
		if group.Database != "" && !seen[group.Database] {
			complete = false
		}
		if complete {
			group.CPURequested = &cpu
			group.MemoryRequested = &memory
		}
		nodes := map[string]bool{}
		for _, pod := range pods.Items {
			if pod.Namespace != app.Namespace || pod.Spec.NodeName == "" || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
				continue
			}
			for _, process := range groups[i].Processes {
				if pod.Labels[shpyrdv1.LabelProcess] == process && pod.Labels[shpyrdv1.LabelApp] == app.Name {
					nodes[pod.Spec.NodeName] = true
				}
			}
			if groups[i].Database != "" && pod.Labels["cnpg.io/cluster"] == groups[i].Database {
				nodes[pod.Spec.NodeName] = true
			}
		}
		// Claims retain placement while processes or databases are stopped.
		for _, name := range groups[i].Claims {
			pvc := &corev1.PersistentVolumeClaim{}
			if err := s.apps.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: name}, pvc); err != nil {
				return nil, nil, err
			}
			if pvc.Spec.VolumeName == "" {
				continue
			}
			pv := &corev1.PersistentVolume{}
			if err := s.apps.Get(ctx, types.NamespacedName{Name: pvc.Spec.VolumeName}, pv); err != nil {
				return nil, nil, err
			}
			if pv.Spec.NodeAffinity != nil && pv.Spec.NodeAffinity.Required != nil {
				for _, term := range pv.Spec.NodeAffinity.Required.NodeSelectorTerms {
					for _, expr := range term.MatchExpressions {
						if expr.Key == corev1.LabelHostname {
							for _, node := range expr.Values {
								nodes[node] = true
							}
						}
					}
				}
			}
		}
		for name := range nodes {
			groups[i].Nodes = append(groups[i].Nodes, name)
		}
		sort.Strings(groups[i].Nodes)
	}
	var nodes corev1.NodeList
	if err := s.apps.List(ctx, &nodes); err != nil {
		return nil, nil, err
	}
	result := []placementNode{}
	for _, node := range nodes.Items {
		reason := eligiblePlacementNode(&node)
		n := placementNode{Name: node.Name, Hostname: node.Labels[corev1.LabelHostname], Pool: node.Labels[controller.PoolLabel], Eligible: reason == "", Reason: reason, Architecture: node.Labels[corev1.LabelArchStable], CPU: node.Status.Allocatable.Cpu().MilliValue(), Memory: node.Status.Allocatable.Memory().Value()}
		for _, pod := range pods.Items {
			if pod.Spec.NodeName != node.Name || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
				continue
			}
			cpu, memory := placementPodRequests(&pod.Spec)
			n.CPURequested += cpu
			n.MemoryRequested += memory
		}
		result = append(result, n)
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].ID < groups[j].ID })
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return groups, result, nil
}
func (s *Server) getProjectPlacement(c *gin.Context) {
	app, ok := s.loadApp(c)
	if !ok {
		return
	}
	groups, nodes, err := s.projectPlacement(c.Request.Context(), app)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	if s.prom != nil {
		// This is a display hint. The mounted destination filesystem is checked
		// directly before copying, so stale metrics never authorize a transfer.
		if samples, err := s.prom.Query(c.Request.Context(), `min by (node) (node_filesystem_avail_bytes{node!="",mountpoint=~"/|/var/lib/shpyrd|/var/lib/shpyrd/volumes",fstype!~"tmpfs|overlay"})`); err == nil {
			for i := range nodes {
				for _, sample := range samples {
					if sample.Labels["node"] == nodes[i].Name && sample.Value >= 0 && !math.IsNaN(sample.Value) && !math.IsInf(sample.Value, 0) {
						value := sample.Value
						nodes[i].DiskAvailable = &value
					}
				}
			}
		}
		if samples, err := s.prom.Query(c.Request.Context(), fmt.Sprintf(`max by (persistentvolumeclaim) (kubelet_volume_stats_used_bytes{namespace=%q})`, app.Namespace)); err == nil {
			placementDiskUsage(groups, samples)
		}
	}
	retained, err := s.retainedMigrationDisks(c.Request.Context(), app)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"groups": groups, "nodes": nodes, "retainedVolumes": retained})
}
func (s *Server) placementDestination(ctx context.Context, app *shpyrdv1.App, groupID, nodeName string) (placementGroup, *corev1.Node, error) {
	groups, _, err := s.projectPlacement(ctx, app)
	if err != nil {
		return placementGroup{}, nil, err
	}
	var selected *placementGroup
	for i := range groups {
		if groups[i].ID == groupID {
			selected = &groups[i]
		}
	}
	if selected == nil {
		return placementGroup{}, nil, errors.New("placement group no longer exists; refresh the project")
	}
	node := &corev1.Node{}
	if err := s.apps.Get(ctx, types.NamespacedName{Name: nodeName}, node); err != nil {
		return placementGroup{}, nil, err
	}
	if reason := eligiblePlacementNode(node); reason != "" {
		return placementGroup{}, nil, errors.New(reason)
	}
	if selected.Pool != "" && node.Labels[controller.PoolLabel] != selected.Pool {
		return placementGroup{}, nil, fmt.Errorf("this group requires the %s node pool", selected.Pool)
	}
	if strings.TrimSpace(node.Labels[corev1.LabelHostname]) == "" {
		return placementGroup{}, nil, errors.New("destination hostname missing")
	}
	if err := s.checkPlacementCapacity(ctx, app, *selected, node); err != nil {
		return placementGroup{}, nil, err
	}
	return *selected, node, nil
}

// Reject a move that cannot fit current workload requests before pausing.
// The scheduler remains the final authority if competing workloads change.
func (s *Server) checkPlacementCapacity(ctx context.Context, app *shpyrdv1.App, group placementGroup, node *corev1.Node) error {
	var pods corev1.PodList
	if err := s.apps.List(ctx, &pods); err != nil {
		return err
	}
	var cpu, memory int64
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		moving := placementPodMatches(app, group, &pod)
		if moving && pod.Spec.NodeName != "" {
			source := &corev1.Node{}
			if err := s.apps.Get(ctx, types.NamespacedName{Name: pod.Spec.NodeName}, source); err != nil {
				return err
			}
			if source.Labels[corev1.LabelArchStable] != node.Labels[corev1.LabelArchStable] {
				return errors.New("choose a destination with the same CPU architecture")
			}
		}
		if !moving && pod.Spec.NodeName != node.Name {
			continue
		}
		podCPU, podMemory := placementPodRequests(&pod.Spec)
		cpu += podCPU
		memory += podMemory
	}
	if cpu > node.Status.Allocatable.Cpu().MilliValue() || memory > node.Status.Allocatable.Memory().Value() {
		return errors.New("destination lacks CPU or memory for the current workload requests")
	}
	return nil
}

// Include init-container peaks, restartable init sidecars and pod overhead.
// These are scheduling requests, not live utilization or limits.
func placementPodRequests(spec *corev1.PodSpec) (cpu, memory int64) {
	for _, c := range spec.Containers {
		cpu += c.Resources.Requests.Cpu().MilliValue()
		memory += c.Resources.Requests.Memory().Value()
	}
	var sideCPU, sideMemory, initCPU, initMemory int64
	for _, c := range spec.InitContainers {
		cCPU, cMemory := c.Resources.Requests.Cpu().MilliValue(), c.Resources.Requests.Memory().Value()
		if c.RestartPolicy != nil && *c.RestartPolicy == corev1.ContainerRestartPolicyAlways {
			sideCPU += cCPU
			sideMemory += cMemory
			cCPU = 0
			cMemory = 0
		}
		initCPU = max(initCPU, sideCPU+cCPU)
		initMemory = max(initMemory, sideMemory+cMemory)
	}
	cpu = max(cpu+sideCPU, initCPU)
	memory = max(memory+sideMemory, initMemory)
	if spec.Resources != nil {
		if value, ok := spec.Resources.Requests[corev1.ResourceCPU]; ok {
			cpu = value.MilliValue()
		}
		if value, ok := spec.Resources.Requests[corev1.ResourceMemory]; ok {
			memory = value.Value()
		}
	}
	return cpu + spec.Overhead.Cpu().MilliValue(), memory + spec.Overhead.Memory().Value()
}
func placementPodMatches(app *shpyrdv1.App, group placementGroup, pod *corev1.Pod) bool {
	if pod.Namespace != app.Namespace {
		return false
	}
	if group.Database != "" {
		return pod.Labels["cnpg.io/cluster"] == group.Database
	}
	if pod.Labels[shpyrdv1.LabelApp] != app.Name {
		return false
	}
	for _, process := range group.Processes {
		if pod.Labels[shpyrdv1.LabelProcess] == process {
			return true
		}
	}
	return false
}
func placementDiskUsage(groups []placementGroup, samples []prom.Sample) {
	usage := map[string]float64{}
	for _, sample := range samples {
		if sample.Value >= 0 && !math.IsNaN(sample.Value) && !math.IsInf(sample.Value, 0) {
			usage[sample.Labels["persistentvolumeclaim"]] = sample.Value
		}
	}
	for i := range groups {
		if len(groups[i].Claims) == 0 {
			continue
		}
		total, complete := float64(0), true
		for _, claim := range groups[i].Claims {
			value, ok := usage[claim]
			if !ok {
				complete = false
			}
			total += value
		}
		if complete {
			groups[i].DiskUsed = &total
		}
	}
}
