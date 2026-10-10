package install

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/shpyrd-io/shpyrd/pkg/kube"
)

func localPV(name, node string, phase corev1.PersistentVolumePhase, reclaim corev1.PersistentVolumeReclaimPolicy, claimNS, claim string) *corev1.PersistentVolume {
	pv := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: corev1.PersistentVolumeSpec{
		StorageClassName:              LocalStorageClass,
		PersistentVolumeReclaimPolicy: reclaim,
		NodeAffinity:                  &corev1.VolumeNodeAffinity{Required: &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{{MatchExpressions: []corev1.NodeSelectorRequirement{{Key: corev1.LabelHostname, Operator: corev1.NodeSelectorOpIn, Values: []string{node}}}}}}},
	}, Status: corev1.PersistentVolumeStatus{Phase: phase}}
	if claim != "" {
		pv.Spec.ClaimRef = &corev1.ObjectReference{Namespace: claimNS, Name: claim}
	}
	return pv
}

// Every node-local volume counts, Released and retained ones included: the
// bytes are on the node until the volume is gone.
func TestNodeLocalVolumesCountReleasedAndRetained(t *testing.T) {
	block := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "csi-1"}, Spec: corev1.PersistentVolumeSpec{StorageClassName: "oci-bv"}}
	cs := fake.NewClientset(
		localPV("pvc-b", "10.0.1.2", corev1.VolumeBound, corev1.PersistentVolumeReclaimDelete, "p-1", "data-db-1"),
		localPV("pvc-a", "10.0.1.2", corev1.VolumeReleased, corev1.PersistentVolumeReclaimRetain, "p-2", "shpyrd-vol-files"),
		localPV("pvc-c", "10.0.1.1", corev1.VolumeAvailable, corev1.PersistentVolumeReclaimDelete, "", ""),
		block,
	)
	got, err := NodeLocalVolumes(context.Background(), &kube.Client{Kube: cs})
	if err != nil {
		t.Fatal(err)
	}
	want := []NodeLocalVolume{
		{Name: "pvc-c", Node: "10.0.1.1", Phase: "Available"},
		{Name: "pvc-a", Namespace: "p-2", Claim: "shpyrd-vol-files", Node: "10.0.1.2", Phase: "Released", Retained: true},
		{Name: "pvc-b", Namespace: "p-1", Claim: "data-db-1", Node: "10.0.1.2", Phase: "Bound"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d volumes, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("volume %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	none, err := NodeLocalVolumes(context.Background(), &kube.Client{Kube: fake.NewClientset(block)})
	if err != nil || none != nil {
		t.Errorf("no node-local volumes: %v %v", none, err)
	}
}
