package api

import (
	"net/http"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/internal/controller"
)

// #127: while a release rolls out, rollback and resize are refused with
// the way out in the sentence, and go through with force.
func TestTheWayBackIsNeverLocked(t *testing.T) {
	shop := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-shop", Generation: 3},
		Spec:       shpyrdv1.AppSpec{Image: "ghcr.io/acme/shop:3", Processes: map[string]shpyrdv1.Process{"web": {Size: "shared-s"}}},
		Status: shpyrdv1.AppStatus{ObservedGeneration: 3, Phase: shpyrdv1.PhaseDeploying, Message: "Releasing v3: web 0/1 ready",
			Releases: []shpyrdv1.Release{{Number: 2, Image: "ghcr.io/acme/shop:2"}, {Number: 3, Image: "ghcr.io/acme/shop:3"}}},
	}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n1"}, Status: corev1.NodeStatus{Allocatable: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("4"), corev1.ResourceMemory: resource.MustParse("16Gi")}}}
	s, _ := newTestServer(t, nil, []client.Object{shop}, node)
	rec := do(t, s, "POST", "/api/projects/shop/rollback", `{"release":2}`, true)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "roll back anyway with force") {
		t.Errorf("rollback during a rollout = %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, s, "POST", "/api/projects/shop/rollback", `{"release":2,"force":true}`, true); rec.Code != http.StatusOK {
		t.Errorf("forced rollback = %d %s", rec.Code, rec.Body)
	}
	shop.Status.Phase, shop.Status.Message = shpyrdv1.PhaseDeploying, "Releasing v4: web 0/1 ready"
	rec = do(t, s, "POST", "/api/projects/shop/processes", `{"processes":{"web":{"size":"shared-m"}}}`, true)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "apply the change anyway with force") {
		t.Errorf("resize during a rollout = %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, s, "POST", "/api/projects/shop/processes", `{"processes":{"web":{"size":"shared-m"}},"force":true}`, true); rec.Code != http.StatusOK {
		t.Errorf("forced resize = %d %s", rec.Code, rec.Body)
	}
}

// #127: a size no machine of the pool can hold is refused in the batch
// endpoint as in the single one; a process pinned to the machine that holds
// its disk is refused a size that machine has no room for, counting its
// own instance as free.
func TestImpossibleSizesAreRefusedUpFront(t *testing.T) {
	shop := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-shop", Annotations: map[string]string{controller.AnnotationProcessNodes: `{"web":"n1"}`}},
		Spec:       shpyrdv1.AppSpec{Image: "ghcr.io/acme/shop:1", Processes: map[string]shpyrdv1.Process{"web": {Size: "shared-s"}}},
		Status:     shpyrdv1.AppStatus{Phase: shpyrdv1.PhaseRunning},
	}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n1"}, Status: corev1.NodeStatus{Allocatable: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1830m"), corev1.ResourceMemory: resource.MustParse("8Gi")}}}
	own := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web-1", Namespace: "app-shop", Labels: map[string]string{shpyrdv1.LabelApp: "shop", shpyrdv1.LabelProcess: "web"}},
		Spec: corev1.PodSpec{NodeName: "n1", Containers: []corev1.Container{{Name: "app", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m")}}}}}}
	neighbour := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "app-other"},
		Spec: corev1.PodSpec{NodeName: "n1", Containers: []corev1.Container{{Name: "app", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1200m"), corev1.ResourceMemory: resource.MustParse("1Gi")}}}}}}
	s, _ := newTestServer(t, nil, []client.Object{shop}, node, own, neighbour)
	// No machine of the cluster has 2 cores: refused before anything is stopped.
	rec := do(t, s, "POST", "/api/projects/shop/processes", `{"processes":{"web":{"size":"dedicated-m"}}}`, true)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "the largest machine in this cluster offers 1.83 cores") {
		t.Errorf("batch resize to 2 cores = %d %s", rec.Code, rec.Body)
	}
	// n1 holds the disk: 1.83 − 1.2 (the neighbour) = 0.63 cores free with
	// the process's own instance counted as free; dedicated-s asks for 1.
	rec = do(t, s, "POST", "/api/projects/shop/resize", `{"process":"web","size":"dedicated-s"}`, true)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "web runs on n1, the machine that holds its disk; it has 0.63 cores") || !strings.Contains(rec.Body.String(), "dedicated-s asks for 1 core") {
		t.Errorf("resize on its machine = %d %s", rec.Code, rec.Body)
	}
	// shared-m asks for a share of a core: fits.
	if rec := do(t, s, "POST", "/api/projects/shop/resize", `{"process":"web","size":"shared-m"}`, true); rec.Code != http.StatusOK {
		t.Errorf("resize within the machine's room = %d %s", rec.Code, rec.Body)
	}
}
