package controller

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestRegistryGarbageCollectionUsesTheConfiguredStorage(t *testing.T) {
	g := &RegistryGC{Namespace: "shpyrd-system", Image: "registry:3"}
	job := g.job("registry-node")
	pod := &job.Spec.Template.Spec
	if pod.EnableServiceLinks == nil || *pod.EnableServiceLinks {
		t.Error("GC must disable registry service discovery environment variables")
	}
	if pod.NodeName != "registry-node" || pod.Volumes[0].PersistentVolumeClaim.ClaimName != "registry-data" {
		t.Fatal("filesystem GC needs the registry's volume and node")
	}
	g.useBucket(job)
	if pod.NodeName != "" {
		t.Error("bucket GC should run on any node")
	}
	for _, v := range pod.Volumes {
		if v.PersistentVolumeClaim != nil {
			t.Error("bucket GC still needs a PVC")
		}
	}
	c := pod.Containers[0]
	if strings.Contains(strings.Join(c.Command, " "), "du -") {
		t.Error("bucket GC measures a local directory")
	}
	if len(c.Env) != 2 {
		t.Fatal("missing S3 credentials")
	}
	for _, e := range c.Env {
		if e.ValueFrom.SecretKeyRef.Name != "registry-s3" || e.ValueFrom.SecretKeyRef.Key != e.Name {
			t.Errorf("wrong credential reference: %+v", e)
		}
	}
}

// registryFixture is the registry as the installer leaves it: config on a
// filesystem volume, the Deployment at replicas with the status given.
func registryFixture(t *testing.T, replicas int32, status appsv1.DeploymentStatus) (*RegistryGC, client.Client) {
	t.Helper()
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: registryDeploymentName, Namespace: "shpyrd-system", Generation: 1},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(replicas),
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "registry", Image: "registry:3"}},
				Volumes:    []corev1.Volume{{Name: "config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: registryConfigName}}}}},
			}},
		},
		Status: status,
	}
	cfg := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: registryConfigName, Namespace: "shpyrd-system"},
		Data:       map[string]string{"config.yml": "storage:\n  filesystem:\n    rootdirectory: /var/lib/registry\n"},
	}
	c := fake.NewClientBuilder().WithObjects(dep, cfg).WithStatusSubresource(dep).Build()
	return &RegistryGC{Client: c, Namespace: "shpyrd-system", Image: "registry:3", RolloutTimeout: 300 * time.Millisecond}, c
}

func registryConfigOf(t *testing.T, c client.Client) string {
	t.Helper()
	dep := &appsv1.Deployment{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "shpyrd-system", Name: registryDeploymentName}, dep); err != nil {
		t.Fatal(err)
	}
	return dep.Spec.Template.Spec.Volumes[0].ConfigMap.Name
}

// Two replicas on object storage (rolled one at a time) are rolled out
// when both run the new configuration and no old pod is left; the wait
// used to want exactly one replica and timed out every time.
func TestRegistryReadOnlySwitchWaitsForEveryReplica(t *testing.T) {
	g, c := registryFixture(t, 2, appsv1.DeploymentStatus{ObservedGeneration: 100, Replicas: 2, UpdatedReplicas: 2, ReadyReplicas: 2})
	if err := g.setReadOnly(context.Background(), true); err != nil {
		t.Fatalf("two replicas rolled out: %v", err)
	}
	if got := registryConfigOf(t, c); got != registryReadOnlyConfig {
		t.Errorf("config = %s, want the read-only one", got)
	}

	// An old pod still serving (three running for two wanted) is not done.
	g, _ = registryFixture(t, 2, appsv1.DeploymentStatus{ObservedGeneration: 100, Replicas: 3, UpdatedReplicas: 2, ReadyReplicas: 2})
	if err := g.setReadOnly(context.Background(), true); err == nil || !strings.Contains(err.Error(), "did not roll out") {
		t.Errorf("an old pod left: %v", err)
	}
}

// A collection whose switch to read-only times out after changing the
// Deployment still switches it back: the registry never stays read-only
// because a rollout was slow (25 Oct 2026, every build refused for 35 h).
func TestRegistryGarbageCollectionSwitchesBackWhenTheSwitchFails(t *testing.T) {
	g, c := registryFixture(t, 2, appsv1.DeploymentStatus{ObservedGeneration: 100, Replicas: 2, UpdatedReplicas: 1, ReadyReplicas: 1})
	g.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, _, _, err := g.run(context.Background()); err == nil || !strings.Contains(err.Error(), "read-only=true") {
		t.Fatalf("run = %v, want the switch's timeout", err)
	}
	if got := registryConfigOf(t, c); got != registryConfigName {
		t.Errorf("registry left on %s after a failed switch", got)
	}
}
