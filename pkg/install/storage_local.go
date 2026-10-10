package install

import (
	"context"
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/shpyrd-io/shpyrd/pkg/kube"
)

// NodeLocalVolume is one PersistentVolume of the node-local class, whatever
// its phase: a Released or retained one still holds its bytes on the node,
// and that node cannot go until it is gone.
type NodeLocalVolume struct {
	Name string `json:"name"`
	// Namespace and Claim name the claim the volume was made for; empty
	// once the claim is gone.
	Namespace string `json:"namespace,omitempty"`
	Claim     string `json:"claim,omitempty"`
	Node      string `json:"node"`
	Phase     string `json:"phase"`
	Retained  bool   `json:"retained,omitempty"`
}

// NodeLocalVolumes lists the volumes of the node-local class, by node then
// name. While any exists the storage-local component must stay installed:
// its teardown is what deletes a volume's directory, and the autoscaler is
// kept from removing the node. Nil when there is none.
func NodeLocalVolumes(ctx context.Context, k *kube.Client) ([]NodeLocalVolume, error) {
	pvs, err := k.Kube.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var out []NodeLocalVolume
	for _, pv := range pvs.Items {
		if pv.Spec.StorageClassName != LocalStorageClass {
			continue
		}
		v := NodeLocalVolume{Name: pv.Name, Node: nodeOfLocalPV(pv), Phase: string(pv.Status.Phase), Retained: pv.Spec.PersistentVolumeReclaimPolicy == corev1.PersistentVolumeReclaimRetain}
		if ref := pv.Spec.ClaimRef; ref != nil {
			v.Namespace, v.Claim = ref.Namespace, ref.Name
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Node != out[j].Node {
			return out[i].Node < out[j].Node
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// nodeOfLocalPV reads the hostname the local provisioner pinned the volume
// to (a required node affinity on kubernetes.io/hostname).
func nodeOfLocalPV(pv corev1.PersistentVolume) string {
	if pv.Spec.NodeAffinity == nil || pv.Spec.NodeAffinity.Required == nil {
		return ""
	}
	for _, term := range pv.Spec.NodeAffinity.Required.NodeSelectorTerms {
		for _, expr := range term.MatchExpressions {
			if expr.Key == corev1.LabelHostname && expr.Operator == corev1.NodeSelectorOpIn && len(expr.Values) > 0 {
				return expr.Values[0]
			}
		}
	}
	return ""
}
