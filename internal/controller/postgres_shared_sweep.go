package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/shpyrd-io/shpyrd/pkg/objectstore"
	"github.com/shpyrd-io/shpyrd/pkg/pgdump"
)

// sharedDumpsObjects opens the bucket a server's dumps Secret names; a
// test puts a fake here.
var sharedDumpsObjects = func(sec *corev1.Secret) (pgdump.Objects, error) {
	data := func(key string) string { return string(sec.Data[key]) }
	s3, err := objectstore.NewS3(data("AWS_ENDPOINT_URL"), data("AWS_REGION"), data("BUCKET"), "", data("AWS_ACCESS_KEY_ID"), data("AWS_SECRET_ACCESS_KEY"))
	if err != nil {
		return nil, err
	}
	return pgdump.Bucket{Client: s3.Client, Name: s3.Bucket}, nil
}

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
		return s.finish(ctx, db)
	}
	before := db.DeepCopy()
	_ = unstructured.SetNestedField(db.Object, "absent", "spec", "ensure")
	_ = unstructured.SetNestedField(db.Object, "delete", "spec", "databaseReclaimPolicy")
	return ctrl.Result{}, s.Patch(ctx, db, client.MergeFrom(before))
}

// finish, once the operator dropped the database, removes what is left of
// the tenant: its copies in the server's dumps bucket and its entry in the
// server's copy counts, the role's object (dropping the role, which
// nothing owns now), its password Secret and the Database object itself.
func (s *SharedTenantSweep) finish(ctx context.Context, db *unstructured.Unstructured) (ctrl.Result, error) {
	applied, _, _ := unstructured.NestedBool(db.Object, "status", "applied")
	observed, _, _ := unstructured.NestedInt64(db.Object, "status", "observedGeneration")
	if !applied || (db.GetGeneration() > 0 && observed < db.GetGeneration()) {
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}
	hash := strings.TrimPrefix(db.GetName(), "db-")
	if server, _, _ := unstructured.NestedString(db.Object, "spec", "cluster", "name"); server != "" {
		if err := s.dropCopies(ctx, server, sharedDatabaseName(hash)); err != nil {
			return ctrl.Result{}, err
		}
		keep := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: sharedDumpsKeepMap(server), Namespace: SharedPostgresNamespace}}
		patch := []byte(fmt.Sprintf(`{"data":{%q:null}}`, sharedDatabaseName(hash)))
		if err := s.Patch(ctx, keep, client.RawPatch(types.MergePatchType, patch)); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
	}
	role := &unstructured.Unstructured{}
	role.SetGroupVersionKind(CNPGDatabaseRoleGVK)
	role.SetNamespace(SharedPostgresNamespace)
	role.SetName(sharedRoleObject(hash))
	if err := s.Delete(ctx, role); client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, err
	}
	if err := s.Delete(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: sharedRoleObject(hash), Namespace: SharedPostgresNamespace}}); client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, client.IgnoreNotFound(s.Delete(ctx, db))
}

// dropCopies removes a dropped database's copies from its server's dumps
// bucket: nothing can restore them now that the kept Database is going.
// A server without dumps set up has none.
func (s *SharedTenantSweep) dropCopies(ctx context.Context, server, database string) error {
	sec := &corev1.Secret{}
	if err := s.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: SharedDumpsName(server)}, sec); err != nil {
		return client.IgnoreNotFound(err)
	}
	objects, err := sharedDumpsObjects(sec)
	if err != nil {
		return err
	}
	keys, err := objects.List(ctx, database+"/")
	if err != nil {
		return fmt.Errorf("list the copies of %s: %w", database, err)
	}
	for _, key := range keys {
		if err := objects.Remove(ctx, key); err != nil {
			return fmt.Errorf("remove %s: %w", key, err)
		}
	}
	if len(keys) > 0 {
		log.FromContext(ctx).Info("copies of a dropped database removed", "database", database, "server", server, "copies", len(keys))
	}
	return nil
}
