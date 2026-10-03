package api

import (
	"context"
	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/internal/controller"
	"github.com/shpyrd-io/shpyrd/pkg/prom"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"net/http"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"testing"
)

func TestProjectArchiveExcludesInflightMutations(t *testing.T) {
	app := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "empty", Namespace: "app-empty"}, Status: shpyrdv1.AppStatus{Phase: shpyrdv1.PhasePending}}
	s, _ := newTestServer(t, nil, []client.Object{app})
	gate := s.projectGate(app.Namespace)
	gate.RLock()
	response := do(t, s, "POST", "/api/project-archives/empty/export", "", true)
	gate.RUnlock()
	if response.Code != http.StatusLocked {
		t.Fatalf("in-flight mutation not excluded: %d %s", response.Code, response.Body.String())
	}
	if _, err := s.readProjectArchive(context.Background(), app.Namespace); err == nil {
		t.Fatal("maintenance began during another mutation")
	}
}
func TestProjectPlacementRefusesWrongPoolAndCapacity(t *testing.T) {
	app := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "project", Namespace: "app-project"}}
	node := func(name, pool string) *corev1.Node {
		return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{corev1.LabelHostname: name, corev1.LabelArchStable: "arm64", controller.PoolLabel: pool}}, Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}, Allocatable: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("1Gi")}}}
	}
	source, target, data := node("source", "apps"), node("target", "apps"), node("data", "data")
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: app.Namespace, Labels: map[string]string{shpyrdv1.LabelApp: app.Name, shpyrdv1.LabelProcess: "web"}}, Spec: corev1.PodSpec{NodeName: source.Name, Containers: []corev1.Container{{Name: "web", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")}}}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}
	s, _ := newTestServer(t, nil, []client.Object{app, source, target, data, pod})
	s.opts.Vars = func(key string) string {
		if key == "SHPYRD_APPS_POOL" {
			return "apps"
		}
		return ""
	}
	for _, destination := range []string{"data", "target"} {
		if _, _, err := s.placementDestination(context.Background(), app, "process:web", destination); err == nil {
			t.Fatalf("accepted ineligible destination %s", destination)
		}
	}
	if app.Annotations[shpyrdv1.AnnotationMaintenance] != "" {
		t.Fatal("preflight entered maintenance")
	}
}

func TestPlacementRequestsIncludeInitSidecarsAndOverhead(t *testing.T) {
	always := corev1.ContainerRestartPolicyAlways
	container := func(cpu, memory string) corev1.Container {
		return corev1.Container{Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse(cpu), corev1.ResourceMemory: resource.MustParse(memory)}}}
	}
	sidecar := container("200m", "100Mi")
	sidecar.RestartPolicy = &always
	spec := corev1.PodSpec{Containers: []corev1.Container{container("500m", "200Mi")}, InitContainers: []corev1.Container{sidecar, container("1", "400Mi")}, Overhead: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("50m"), corev1.ResourceMemory: resource.MustParse("50Mi")}}
	cpu, memory := placementPodRequests(&spec)
	if cpu != 1250 || memory != 550<<20 {
		t.Fatalf("requests = %dm / %d bytes", cpu, memory)
	}
}

func TestPlacementDiskMetricsRequireEveryClaim(t *testing.T) {
	groups := []placementGroup{{Claims: []string{"a", "b"}}, {Claims: []string{"a"}}}
	placementDiskUsage(groups, []prom.Sample{{Labels: map[string]string{"persistentvolumeclaim": "a"}, Value: 123}})
	if groups[0].DiskUsed != nil {
		t.Fatal("missing metric became zero")
	}
	if groups[1].DiskUsed == nil || *groups[1].DiskUsed != 123 {
		t.Fatal("single claim usage missing")
	}
	placementDiskUsage(groups, []prom.Sample{{Labels: map[string]string{"persistentvolumeclaim": "a"}, Value: 123}, {Labels: map[string]string{"persistentvolumeclaim": "b"}, Value: 456}})
	if *groups[0].DiskUsed != 579 {
		t.Fatal("multiple claims not summed")
	}
}
