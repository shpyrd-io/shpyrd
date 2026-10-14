package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/shpyrd-io/shpyrd/internal/controller"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestClusterScheduling(t *testing.T) {
	blocked := func(name, message string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "app-shop"},
			Spec:       corev1.PodSpec{NodeSelector: map[string]string{controller.PoolLabel: "apps"}},
			Status:     corev1.PodStatus{Phase: corev1.PodPending, Conditions: []corev1.PodCondition{{Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: "Unschedulable", Message: message}}},
		}
	}
	cpu := blocked("cpu", "0/4 nodes are available: 1 Insufficient cpu, 3 node(s) didn't match Pod's node affinity/selector.")
	slots := blocked("slots", "Too many pods")
	placement := blocked("placement", "untolerated taint")
	image := blocked("image", "Insufficient memory")
	image.Spec.NodeName = "node"
	done := blocked("done", "Insufficient cpu")
	done.Status.Phase = corev1.PodSucceeded
	deleting := blocked("deleting", "Insufficient cpu")
	now := metav1.Now()
	deleting.DeletionTimestamp = &now
	fresh := blocked("fresh", "")
	fresh.Status.Conditions = nil
	s, _ := newTestServer(t, nil, nil, cpu, slots, placement, image, done, deleting, fresh)
	read := func() *SchedulingSummary {
		t.Helper()
		rec := do(t, s, "GET", "/api/cluster", "", true)
		if rec.Code != http.StatusOK {
			t.Fatalf("%d: %s", rec.Code, rec.Body.String())
		}
		var sum ClusterSummary
		if err := json.Unmarshal(rec.Body.Bytes(), &sum); err != nil {
			t.Fatal(err)
		}
		if sum.Scheduling == nil {
			t.Fatal("missing scheduling status")
		}
		rec = do(t, s, "GET", "/api/cluster/scheduling", "", true)
		var scheduling SchedulingSummary
		if rec.Code != http.StatusOK {
			t.Fatalf("scheduling endpoint: %d: %s", rec.Code, rec.Body.String())
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &scheduling); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(&scheduling, sum.Scheduling) {
			t.Fatalf("scheduling endpoint differs: %+v vs %+v", scheduling, sum.Scheduling)
		}
		return sum.Scheduling
	}
	sum := read()
	if sum.Error != "" || len(sum.Pending) != 3 {
		t.Fatalf("%+v", sum)
	}
	for i, want := range []struct {
		name     string
		capacity bool
	}{{"cpu", true}, {"placement", false}, {"slots", true}} {
		got := sum.Pending[i]
		if got.Name != want.name || got.Capacity != want.capacity || got.Pool != "apps" || got.Namespace != "app-shop" || got.Message == "" {
			t.Fatalf("%+v", got)
		}
	}
	// Once scheduled, the alert clears even if the old condition remains.
	for _, pod := range []*corev1.Pod{cpu, slots, placement} {
		pod.Spec.NodeName = "node"
		if _, err := s.kube.Kube.CoreV1().Pods(pod.Namespace).Update(t.Context(), pod, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	if got := read(); len(got.Pending) != 0 || got.Error != "" {
		t.Fatalf("%+v", got)
	}
	s.kube.Kube.(*kubefake.Clientset).PrependReactor("list", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("forbidden")
	})
	if got := read(); got.Error == "" {
		t.Fatal("failed lookup reported healthy scheduling")
	}
}
