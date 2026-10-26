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

// gcWorld is a registry on a claim (no bucket: nothing to measure) whose
// Deployment runs replicas instances, ready ones of them.
func gcWorld(t *testing.T, replicas, ready int32) (*RegistryGC, client.Client) {
	t.Helper()
	ns := "shpyrd-system"
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: registryDeploymentName, Namespace: ns, Generation: 1},
		Spec: appsv1.DeploymentSpec{Replicas: &replicas, Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{{Name: "config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: registryConfigName}}}}},
		}}},
		Status: appsv1.DeploymentStatus{ObservedGeneration: 100, Replicas: replicas, UpdatedReplicas: replicas, ReadyReplicas: ready},
	}
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: registryConfigName, Namespace: ns}, Data: map[string]string{"config.yml": "version: 0.1\nstorage:\n  filesystem:\n    rootdirectory: /var/lib/registry\n"}}
	c := fake.NewClientBuilder().WithObjects(dep, cm).Build()
	return &RegistryGC{Client: c, Namespace: ns, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, c
}

func configOf(t *testing.T, c client.Client) string {
	t.Helper()
	dep := &appsv1.Deployment{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "shpyrd-system", Name: registryDeploymentName}, dep); err != nil {
		t.Fatal(err)
	}
	return dep.Spec.Template.Spec.Volumes[0].ConfigMap.Name
}

// On a bucket the registry runs two replicas (#238): the switch waits for
// both, not for one that never comes.
func TestTheRegistrySwitchWaitsForEveryReplicaItAsksFor(t *testing.T) {
	defer func(timeout, poll time.Duration) { registryRolloutTimeout, registryRolloutPoll = timeout, poll }(registryRolloutTimeout, registryRolloutPoll)
	registryRolloutTimeout, registryRolloutPoll = time.Second, time.Millisecond
	g, c := gcWorld(t, 2, 2)
	if err := g.setReadOnly(context.Background(), true); err != nil {
		t.Fatalf("read-only with two replicas: %v", err)
	}
	if got := configOf(t, c); got != registryReadOnlyConfig {
		t.Errorf("config = %s", got)
	}
	if err := g.setReadOnly(context.Background(), false); err != nil || configOf(t, c) != registryConfigName {
		t.Errorf("back to read-write: %v %s", err, configOf(t, c))
	}

	two, one := int32(2), int32(1)
	for _, d := range []struct {
		name string
		dep  appsv1.Deployment
		want bool
	}{
		{"one of one", appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: &one}, Status: appsv1.DeploymentStatus{Replicas: 1, UpdatedReplicas: 1, ReadyReplicas: 1}}, true},
		{"no replicas field means one", appsv1.Deployment{Status: appsv1.DeploymentStatus{Replicas: 1, UpdatedReplicas: 1, ReadyReplicas: 1}}, true},
		{"two of two", appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: &two}, Status: appsv1.DeploymentStatus{Replicas: 2, UpdatedReplicas: 2, ReadyReplicas: 2}}, true},
		{"an old one still running beside the new", appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: &two}, Status: appsv1.DeploymentStatus{Replicas: 3, UpdatedReplicas: 2, ReadyReplicas: 3}}, false},
		{"one new not ready", appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: &two}, Status: appsv1.DeploymentStatus{Replicas: 2, UpdatedReplicas: 2, ReadyReplicas: 1}}, false},
		{"not observed yet", appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Generation: 3}, Spec: appsv1.DeploymentSpec{Replicas: &two}, Status: appsv1.DeploymentStatus{ObservedGeneration: 2, Replicas: 2, UpdatedReplicas: 2, ReadyReplicas: 2}}, false},
	} {
		if got := registryRolledOut(&d.dep); got != d.want {
			t.Errorf("%s: rolled out = %v", d.name, got)
		}
	}
}

// A switch to read-only that times out has already pointed the registry at
// the read-only configuration: the collection still puts it back, so a
// failed run never leaves every push refused.
func TestAFailedSwitchToReadOnlyLeavesTheRegistryWritable(t *testing.T) {
	defer func(timeout, poll time.Duration) { registryRolloutTimeout, registryRolloutPoll = timeout, poll }(registryRolloutTimeout, registryRolloutPoll)
	registryRolloutTimeout, registryRolloutPoll = 20*time.Millisecond, time.Millisecond
	g, c := gcWorld(t, 2, 1) // one replica never gets ready
	if _, _, _, err := g.run(context.Background()); err == nil {
		t.Fatal("a switch that never rolled out succeeded")
	}
	if got := configOf(t, c); got != registryConfigName {
		t.Errorf("the registry was left on %s after a failed run", got)
	}
}
