package controller

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// SharedTenantSweep drops a deleted shared database's data once its drop
// date has passed: the Database object stamped by the Postgres finalizer
// is flipped to ensure absent with reclaim policy delete, and CloudNativePG
// drops the database. Idempotent; a date still ahead is waited for.
type SharedTenantSweep struct {
	client.Client
	// Now is the clock (tests set it).
	Now func() time.Time
}

func (s *SharedTenantSweep) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *SharedTenantSweep) SetupWithManager(mgr ctrl.Manager) error {
	db := &unstructured.Unstructured{}
	db.SetGroupVersionKind(CNPGDatabaseGVK)
	stamped := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		return obj.GetNamespace() == SharedPostgresNamespace && obj.GetAnnotations()[AnnotationDropAfter] != ""
	})
	return ctrl.NewControllerManagedBy(mgr).
		Named("shared-tenant-sweep").
		For(db, builder.WithPredicates(stamped)).
		Complete(s)
}

func (s *SharedTenantSweep) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	db := &unstructured.Unstructured{}
	db.SetGroupVersionKind(CNPGDatabaseGVK)
	if err := s.Get(ctx, req.NamespacedName, db); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	stamp := db.GetAnnotations()[AnnotationDropAfter]
	if stamp == "" {
		return ctrl.Result{}, nil
	}
	dropAt, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return ctrl.Result{}, nil // not a date we wrote; left alone
	}
	if remaining := dropAt.Sub(s.now()); remaining > 0 {
		return ctrl.Result{RequeueAfter: remaining}, nil
	}
	ensure, _, _ := unstructured.NestedString(db.Object, "spec", "ensure")
	policy, _, _ := unstructured.NestedString(db.Object, "spec", "databaseReclaimPolicy")
	if ensure == "absent" && policy == "delete" {
		return ctrl.Result{}, nil
	}
	before := db.DeepCopy()
	_ = unstructured.SetNestedField(db.Object, "absent", "spec", "ensure")
	_ = unstructured.SetNestedField(db.Object, "delete", "spec", "databaseReclaimPolicy")
	return ctrl.Result{}, s.Patch(ctx, db, client.MergeFrom(before))
}
