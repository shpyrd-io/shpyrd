package controller

import (
	"context"
	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"testing"
)

func TestPlacementGroupsFollowTransitiveVolumeSharing(t *testing.T) {
	app := &shpyrdv1.App{Spec: shpyrdv1.AppSpec{Processes: map[string]shpyrdv1.Process{
		"web":       {Volumes: []shpyrdv1.VolumeMount{{Name: "uploads"}}},
		"worker":    {Volumes: []shpyrdv1.VolumeMount{{Name: "uploads"}, {Name: "documents"}}},
		"indexer":   {Volumes: []shpyrdv1.VolumeMount{{Name: "documents"}}},
		"stateless": {},
	}}}
	got := PlacementGroups(app, []string{"uploads", "documents", "unmounted"})
	want := []PlacementGroup{{ID: "process:indexer", Processes: []string{"indexer", "web", "worker"}, Volumes: []string{"documents", "uploads"}}, {ID: "process:stateless", Processes: []string{"stateless"}, Volumes: []string{}}, {ID: "volume:unmounted", Processes: []string{}, Volumes: []string{"unmounted"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("groups: %#v", got)
	}
}
func TestLocalSharedVolumeUsesOneNodeAndPreservesLegacyClaims(t *testing.T) {
	vol := testVolume("uploads", "1Gi", corev1.ReadWriteMany)
	r, _ := newVolumeReconciler(t, vol)
	r.SharedClass = LocalStorageClass
	pvc := r.desiredPVC(vol, "")
	if pvc.Spec.AccessModes[0] != corev1.ReadWriteOnce {
		t.Fatal("local shared claim must use RWO")
	}
	if err := r.reconcileExisting(context.Background(), vol, pvc); err != nil {
		t.Fatal(err)
	}
	pvc.Spec.StorageClassName = ptr.To("shpyrd-fss")
	pvc.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany}
	if err := r.reconcileExisting(context.Background(), vol, pvc); err != nil {
		t.Fatalf("legacy shared claim changed: %v", err)
	}
}
func TestLocalDataProtectionPreservesOperatorOwnedProtection(t *testing.T) {
	owned := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "owned", Labels: map[string]string{corev1.LabelHostname: "owned"}}}
	external := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "external", Labels: map[string]string{corev1.LabelHostname: "external"}, Annotations: map[string]string{scaleDownDisabled: "true"}}}
	pv := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "data"}, Spec: corev1.PersistentVolumeSpec{StorageClassName: LocalStorageClass, NodeAffinity: &corev1.VolumeNodeAffinity{Required: &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{{MatchExpressions: []corev1.NodeSelectorRequirement{{Key: corev1.LabelHostname, Operator: corev1.NodeSelectorOpIn, Values: []string{"owned", "external"}}}}}}}}}
	_, c := newVolumeReconciler(t, owned, external, pv)
	protection := &LocalStorageProtection{Client: c}
	ctx := context.Background()
	if err := protection.reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(owned), owned); err != nil {
		t.Fatal(err)
	}
	if owned.Annotations[scaleDownDisabled] != "true" || owned.Annotations[localDataProtection] != "true" {
		t.Fatal("local data unprotected")
	}
	if err := c.Delete(ctx, pv); err != nil {
		t.Fatal(err)
	}
	if err := protection.reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(owned), owned); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(external), external); err != nil {
		t.Fatal(err)
	}
	if owned.Annotations[scaleDownDisabled] != "" || external.Annotations[scaleDownDisabled] != "true" {
		t.Fatal("protection ownership not respected")
	}
}
func TestLocalVolumeAffinityKeepsSharedProcessesTogether(t *testing.T) {
	app := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "project"}, Spec: shpyrdv1.AppSpec{Processes: map[string]shpyrdv1.Process{"web": {Volumes: []shpyrdv1.VolumeMount{{Name: "shared"}}}, "worker": {Volumes: []shpyrdv1.VolumeMount{{Name: "shared"}}}}}}
	local := []resolvedMount{{Name: "shared", Class: LocalStorageClass}}
	a, b := &corev1.PodTemplateSpec{}, &corev1.PodTemplateSpec{}
	localVolumeAffinity(app, "web", a, local)
	localVolumeAffinity(app, "worker", b, local)
	if a.Labels["shpyrd.io/placement-group"] == "" || !reflect.DeepEqual(a.Spec.Affinity, b.Spec.Affinity) {
		t.Fatal("shared volume consumers can diverge")
	}
	if a.Annotations["cluster-autoscaler.kubernetes.io/safe-to-evict"] != "false" {
		t.Fatal("local workload can be evicted")
	}
}

// The gate is the claim's class (RFC-0060): a process on block storage, or
// on a claim that does not exist yet, is not pinned to a node, and a pin
// left from a node-local past is cleared.
func TestLocalVolumeAffinityIsKeyedOnTheClaim(t *testing.T) {
	app := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "project"}, Spec: shpyrdv1.AppSpec{Processes: map[string]shpyrdv1.Process{"web": {Volumes: []shpyrdv1.VolumeMount{{Name: "data"}}}}}}
	for _, class := range []string{"oci-bv", ""} {
		pod := &corev1.PodTemplateSpec{}
		localVolumeAffinity(app, "web", pod, []resolvedMount{{Name: "data", Class: class}})
		if pod.Labels["shpyrd.io/placement-group"] != "" || pod.Annotations["cluster-autoscaler.kubernetes.io/safe-to-evict"] != "" || pod.Spec.Affinity != nil {
			t.Errorf("class %q: pinned a process whose disk follows it (labels %v, annotations %v, affinity %v)", class, pod.Labels, pod.Annotations, pod.Spec.Affinity)
		}
	}
	// Pinned while node-local, then migrated to block storage: the pin goes.
	pod := &corev1.PodTemplateSpec{}
	localVolumeAffinity(app, "web", pod, []resolvedMount{{Name: "data", Class: LocalStorageClass}})
	if pod.Labels["shpyrd.io/placement-group"] == "" {
		t.Fatal("a node-local mount must pin")
	}
	localVolumeAffinity(app, "web", pod, []resolvedMount{{Name: "data", Class: "oci-bv"}})
	if pod.Labels["shpyrd.io/placement-group"] != "" || pod.Annotations["cluster-autoscaler.kubernetes.io/safe-to-evict"] != "" || pod.Spec.Affinity != nil {
		t.Errorf("the pin of a migrated process was not cleared: labels %v, annotations %v, affinity %v", pod.Labels, pod.Annotations, pod.Spec.Affinity)
	}
}

// Through the reconciler: the Deployment of a process on a node-local claim
// carries the pin; on a block claim it does not, whatever the profile.
func TestLocalClaimGatesPlacementThroughReconciler(t *testing.T) {
	for _, tc := range []struct {
		class  string
		pinned bool
	}{{LocalStorageClass, true}, {"oci-bv", false}, {"", false}} {
		app := sampleApp("gate")
		app.Spec.Image = "registry.test/v@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		app.Spec.Processes = map[string]shpyrdv1.Process{"web": {Volumes: []shpyrdv1.VolumeMount{{Name: "data", Path: "/data"}}}}
		vol := testVolume("data", "1Gi", corev1.ReadWriteOnce)
		vol.Namespace = "app-gate"
		vol.Status.StorageClass = tc.class
		r, c := newTestReconciler(t, app, vol)
		runReconcile(t, r, app)
		dep := &appsv1.Deployment{}
		if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-gate", Name: "gate-web"}, dep); err != nil {
			t.Fatal(err)
		}
		tpl := dep.Spec.Template
		pinned := tpl.Labels["shpyrd.io/placement-group"] != "" && tpl.Annotations["cluster-autoscaler.kubernetes.io/safe-to-evict"] == "false" && tpl.Spec.Affinity != nil
		if pinned != tc.pinned {
			t.Errorf("claim class %q: pinned=%v, want %v (labels %v, affinity %v)", tc.class, pinned, tc.pinned, tpl.Labels, tpl.Spec.Affinity)
		}
	}
}
