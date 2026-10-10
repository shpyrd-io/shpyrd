package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/shpyrd-io/shpyrd/pkg/install"
)

const LocalStorageClass = install.LocalStorageClass
const localDataProtection = "shpyrd.io/local-data-protection"
const scaleDownDisabled = "cluster-autoscaler.kubernetes.io/scale-down-disabled"

// LocalStorageProtection prevents ordinary autoscaler consolidation from
// destroying the only copy of local data: a node that holds a node-local
// PersistentVolume (whatever the volume's phase; a Released one still holds
// the bytes) carries scale-down-disabled until the last such volume is gone.
// It cannot protect against machine/disk failure or an operator deleting a
// node, and never removes a protection annotation owned by someone else.
//
// It runs as a controller fed by the PersistentVolume and Node informers:
// an event on a node-local volume or on a node is one reconcile of the
// whole picture, which is small (nodes × local volumes). With no node-local
// volume left it has nothing to do and nothing to watch for.
type LocalStorageProtection struct{ Client client.Client }

// protectionRequest is the one key every event maps to, so a burst of
// events coalesces into one reconcile.
var protectionRequest = reconcile.Request{NamespacedName: types.NamespacedName{Name: "local-data-protection"}}

func toProtection(context.Context, client.Object) []reconcile.Request {
	return []reconcile.Request{protectionRequest}
}

func isLocalPV(o client.Object) bool {
	pv, ok := o.(*corev1.PersistentVolume)
	return ok && pv.Spec.StorageClassName == LocalStorageClass
}

// SetupWithManager registers the controller: node-local volumes (created,
// released, deleted) and nodes (joined, relabelled, annotated) trigger it.
func (r *LocalStorageProtection) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("local-data-protection").
		Watches(&corev1.PersistentVolume{}, handler.EnqueueRequestsFromMapFunc(toProtection), builder.WithPredicates(predicate.NewPredicateFuncs(isLocalPV))).
		Watches(&corev1.Node{}, handler.EnqueueRequestsFromMapFunc(toProtection)).
		Complete(r)
}

// Reconcile recomputes which nodes hold local data and patches the ones
// whose protection changed. A failing patch is retried with backoff; the
// protection that exists stays as it is meanwhile.
func (r *LocalStorageProtection) Reconcile(ctx context.Context, _ reconcile.Request) (ctrl.Result, error) {
	return ctrl.Result{}, r.reconcile(ctx)
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
