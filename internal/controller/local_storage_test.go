package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// The controller wakes on node-local volumes only, and every event maps to
// the one reconcile of the whole picture.
func TestLocalDataProtectionWatchesOnlyLocalVolumes(t *testing.T) {
	local := &corev1.PersistentVolume{Spec: corev1.PersistentVolumeSpec{StorageClassName: LocalStorageClass}}
	block := &corev1.PersistentVolume{Spec: corev1.PersistentVolumeSpec{StorageClassName: "oci-bv"}}
	node := &corev1.Node{}
	if !isLocalPV(local) || isLocalPV(block) || isLocalPV(node) {
		t.Errorf("predicate: local=%v block=%v node=%v", isLocalPV(local), isLocalPV(block), isLocalPV(node))
	}
	if reqs := toProtection(context.Background(), local); len(reqs) != 1 || reqs[0] != protectionRequest {
		t.Errorf("a volume event maps to %v", reqs)
	}
	if reqs := toProtection(context.Background(), node); len(reqs) != 1 || reqs[0] != protectionRequest {
		t.Errorf("a node event maps to %v", reqs)
	}
}

// With the last node-local volume gone, the protection this controller set
// is removed; a protection someone else set stays.
func TestLocalDataProtectionRetiresWithTheLastLocalVolume(t *testing.T) {
	ctx := context.Background()
	ours := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "ours", Labels: map[string]string{corev1.LabelHostname: "ours"}, Annotations: map[string]string{localDataProtection: "true", scaleDownDisabled: "true"}}}
	theirs := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "theirs", Labels: map[string]string{corev1.LabelHostname: "theirs"}, Annotations: map[string]string{scaleDownDisabled: "true"}}}
	block := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "csi-1"}, Spec: corev1.PersistentVolumeSpec{StorageClassName: "oci-bv", NodeAffinity: &corev1.VolumeNodeAffinity{Required: &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{{MatchExpressions: []corev1.NodeSelectorRequirement{{Key: corev1.LabelHostname, Operator: corev1.NodeSelectorOpIn, Values: []string{"ours"}}}}}}}}}
	c := fake.NewClientBuilder().WithObjects(ours, theirs, block).Build()
	r := &LocalStorageProtection{Client: c}
	if _, err := r.Reconcile(ctx, protectionRequest); err != nil {
		t.Fatal(err)
	}
	got := &corev1.Node{}
	if err := c.Get(ctx, types.NamespacedName{Name: "ours"}, got); err != nil {
		t.Fatal(err)
	}
	if got.Annotations[scaleDownDisabled] != "" || got.Annotations[localDataProtection] != "" {
		t.Errorf("our protection must go with the last local volume: %v", got.Annotations)
	}
	if err := c.Get(ctx, types.NamespacedName{Name: "theirs"}, got); err != nil {
		t.Fatal(err)
	}
	if got.Annotations[scaleDownDisabled] != "true" {
		t.Errorf("someone else's protection was removed: %v", got.Annotations)
	}
}
