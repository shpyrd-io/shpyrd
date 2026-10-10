package controller

import (
	"context"
	"github.com/shpyrd-io/shpyrd/pkg/install"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const LocalStorageClass = install.LocalStorageClass
const localDataProtection = "shpyrd.io/local-data-protection"
const scaleDownDisabled = "cluster-autoscaler.kubernetes.io/scale-down-disabled"

// LocalStorageProtection prevents ordinary autoscaler consolidation from
// destroying the only copy of local data. Retained migration sources count
// too. It cannot protect against machine/disk failure or an operator deleting
// a node, and never removes a protection annotation owned by someone else.
type LocalStorageProtection struct{ Client client.Client }

func (r *LocalStorageProtection) NeedLeaderElection() bool { return true }
func (r *LocalStorageProtection) Start(ctx context.Context) error {
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	for {
		// A temporary API failure must leave existing protection intact.
		if err := r.reconcile(ctx); err != nil {
			log.FromContext(ctx).Error(err, "protect nodes containing local data")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}
func (r *LocalStorageProtection) reconcile(ctx context.Context) error {
	var volumes corev1.PersistentVolumeList
	if err := r.Client.List(ctx, &volumes); err != nil {
		return err
	}
	var nodes corev1.NodeList
	if err := r.Client.List(ctx, &nodes); err != nil {
		return err
	}
	protected := map[string]bool{}
	for _, pv := range volumes.Items {
		if pv.Spec.StorageClassName != LocalStorageClass || pv.Spec.NodeAffinity == nil || pv.Spec.NodeAffinity.Required == nil {
			continue
		}
		for _, term := range pv.Spec.NodeAffinity.Required.NodeSelectorTerms {
			for _, expr := range term.MatchExpressions {
				if expr.Key == corev1.LabelHostname && expr.Operator == corev1.NodeSelectorOpIn {
					for _, value := range expr.Values {
						protected[value] = true
					}
				}
			}
		}
	}
	for i := range nodes.Items {
		node := &nodes.Items[i]
		before := node.DeepCopy()
		owned := node.Annotations[localDataProtection] == "true"
		needed := protected[node.Labels[corev1.LabelHostname]]
		if needed && node.Annotations[scaleDownDisabled] != "true" {
			if node.Annotations == nil {
				node.Annotations = map[string]string{}
			}
			// Save only annotations we set ourselves; preserve externally set true.
			node.Annotations[localDataProtection] = "true"
			node.Annotations[scaleDownDisabled] = "true"
		} else if !needed && owned {
			delete(node.Annotations, localDataProtection)
			delete(node.Annotations, scaleDownDisabled)
		} else {
			continue
		}
		if err := r.Client.Patch(ctx, node, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
			return err
		}
	}
	return nil
}
