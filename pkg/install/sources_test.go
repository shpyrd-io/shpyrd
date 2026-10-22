package install

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
)

// Storage plan step 7: with sources in the bucket nothing of the server's
// is on a read-write-once claim, so it runs two replicas rolled one at a
// time; the filesystem base keeps one with Recreate.
func TestServerRunsTwoReplicasWithSourcesInTheBucket(t *testing.T) {
	dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "shpyrd-server"}, Spec: appsv1.DeploymentSpec{Replicas: ptr.To[int32](1), Strategy: appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
		Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "server"}}, Volumes: []corev1.Volume{{Name: "data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "shpyrd-data"}}}}}}}}
	raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(dep)
	if err != nil {
		t.Fatal(err)
	}
	u := &unstructured.Unstructured{Object: raw}
	u.SetKind("Deployment")
	out, err := sourcesInBucket([]*unstructured.Unstructured{u}, "gateway-sources")
	if err != nil {
		t.Fatal(err)
	}
	var got appsv1.Deployment
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(out[0].Object, &got); err != nil {
		t.Fatal(err)
	}
	if ptr.Deref(got.Spec.Replicas, 0) != 2 || got.Spec.Strategy.Type != appsv1.RollingUpdateDeploymentStrategyType || got.Spec.Strategy.RollingUpdate.MaxUnavailable.IntValue() != 0 || got.Spec.Strategy.RollingUpdate.MaxSurge.IntValue() != 1 {
		t.Errorf("deployment = replicas %d strategy %+v", ptr.Deref(got.Spec.Replicas, 0), got.Spec.Strategy)
	}
	if got.Spec.Template.Spec.Volumes[0].EmptyDir == nil {
		t.Error("the data volume must be scratch space")
	}
}
