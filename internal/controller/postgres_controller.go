package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/sizes"
)

// PostgresReconciler turns a Postgres resource into a CloudNativePG Cluster
// (RFC-0009). CNPG runs PostgreSQL, creates the application database and
// user and keeps their credentials in Secret <name>-app; the resource
// status summarises the cluster and the Binder turns the Secret into
// config vars.
type PostgresReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	Recorder        record.EventRecorder
	SystemNamespace string
	// Storage is the profile's disk rules (RFC-0060).
	Storage StorageProfile
	// DataPool is the node pool databases run on (RFC-0077): the data
	// pool, or the platform pool on a cluster without one; "" = any.
	DataPool string
	// WorkspaceSleepDefault answers the default idle period before a
	// database of the project namespace hibernates, from its workspace's
	// settings (RFC-0075); "" when there is none. A database with a policy
	// of its own does not ask. Nil: no defaults.
	WorkspaceSleepDefault func(ctx context.Context, namespace string) string
	// SleepAllowed says whether databases may sleep by themselves now (the
	// enterprise's auto sleep). Nil: never; one put to sleep by hand
	// (suspended) still is.
	SleepAllowed func() bool
}

// cnpgStorage is the CNPG storage section: the size and, when the profile
// names one, the class (RFC-0060).
// migratingStorage is what a cluster under migration is rendered with: the
// target class, at the size it has now.
func migratingStorage(current *unstructured.Unstructured, target string, requested resource.Quantity) (string, resource.Quantity) {
	if previous, _, _ := unstructured.NestedString(current.Object, "spec", "storage", "size"); previous != "" {
		if quantity, err := resource.ParseQuantity(previous); err == nil {
			return target, quantity
		}
	}
	return target, requested
}

// keepStorage returns the storage class and size a running cluster keeps
// when the profile names another class: its own. On the profile's class the
// profile decides (the rounded request), as for a new cluster. The class a
// cluster is on is the one its primary's volume has (dataClass, "" when
// there is no primary yet): the Cluster's spec can still name a migration's
// target class after the migration went back to the old volume, and the
// profile's minimum applied to that node-local claim would ask for an
// expansion the local provisioner cannot do.
func keepStorage(current *unstructured.Unstructured, profileClass string, requested resource.Quantity, dataClass string) (string, resource.Quantity) {
	class := dataClass
	if class == "" {
		class, _, _ = unstructured.NestedString(current.Object, "spec", "storage", "storageClass")
	}
	if class == profileClass {
		return profileClass, requested
	}
	if previous, _, _ := unstructured.NestedString(current.Object, "spec", "storage", "size"); previous != "" {
		if quantity, err := resource.ParseQuantity(previous); err == nil {
			return class, quantity
		}
	}
	return class, requested
}

// dataClass is the storage class of the primary instance's volume, "" when
// the cluster has no primary yet or its claim cannot be read.
func (r *PostgresReconciler) dataClass(ctx context.Context, pg *shpyrdv1.Postgres, current *unstructured.Unstructured) string {
	primary, _, _ := unstructured.NestedString(current.Object, "status", "currentPrimary")
	if primary == "" {
		return ""
	}
	claim := &corev1.PersistentVolumeClaim{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: pg.Namespace, Name: primary}, claim); err != nil {
		return ""
	}
	if claim.Spec.StorageClassName == nil {
		return ""
	}
	return *claim.Spec.StorageClassName
}

func cnpgStorage(size resource.Quantity, class string) map[string]interface{} {
	out := map[string]interface{}{"size": size.String()}
	if class != "" {
		out["storageClass"] = class
	}
	return out
}

// CNPGClusterGVK is CloudNativePG's Cluster (handled as unstructured).
var CNPGClusterGVK = schema.GroupVersionKind{Group: "postgresql.cnpg.io", Version: "v1", Kind: "Cluster"}

const (
	defaultPostgresVersion = "17"
	defaultPostgresStorage = "5Gi"
	// Under postgresTunedBelow a database runs with smallPostgresParameters,
	// which keep the instance manager and PostgreSQL inside the limit
	// through a migration's CREATE INDEX and about 20 connections
	// (measured on kind: 128Mi, 0.5 CPU, no OOM, #53).
	postgresTunedBelow = "256Mi"
	// smallPostgresLivenessTimeout (seconds) lets a small database's
	// instance manager answer late while a migration takes all of its CPU
	// share, instead of being restarted in the middle of it (CNPG's
	// default is 30).
	smallPostgresLivenessTimeout = 120
	// AnnotationPostgresSize on the CNPG Cluster records the size its
	// resources were rendered from, as postgresSizeRecord writes it:
	// memory goes down only when the size changes. A record without the
	// "postgres:" prefix (or none) is a cluster made before databases had
	// sizes of their own (#57), when the size named a process size raised
	// to a floor.
	AnnotationPostgresSize = "shpyrd.io/size"
	// PostgresDatabase and PostgresUser are what CNPG's initdb bootstrap creates.
	PostgresDatabase = "app"
	PostgresUser     = "app"
	PostgresPort     = 5432
)

func (r *PostgresReconciler) SetupWithManager(mgr ctrl.Manager) error {
	cluster := &unstructured.Unstructured{}
	cluster.SetGroupVersionKind(CNPGClusterGVK)
	backup := &unstructured.Unstructured{}
	backup.SetGroupVersionKind(CNPGBackupGVK)
	return ctrl.NewControllerManagedBy(mgr).
		For(&shpyrdv1.Postgres{}).
		Owns(cluster).
		Owns(&shpyrdv1.ObjectBucket{}).
		// A finished backup updates the database's status (RFC-0038).
		Watches(backup, handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []reconcile.Request {
			u, ok := obj.(*unstructured.Unstructured)
			if !ok {
				return nil
			}
			name, _, _ := unstructured.NestedString(u.Object, "spec", "cluster", "name")
			if name == "" {
				return nil
			}
			return []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: u.GetNamespace(), Name: name}}}
		})).
		Complete(r)
}

func (r *PostgresReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	pg := &shpyrdv1.Postgres{}
	if err := r.Get(ctx, req.NamespacedName, pg); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !pg.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}
	if pg.Annotations[AnnotationDataMove] != "" {
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}
	if moved, err := r.moveToPostgresSize(ctx, pg); err != nil || moved {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{Requeue: moved}, err
	}
	orig := pg.DeepCopy()
	res, err := r.reconcile(ctx, pg)
	if apierrors.IsConflict(err) {
		return ctrl.Result{Requeue: true}, nil
	}
	var pending *pendingError
	if errors.As(err, &pending) {
		// Not a failure: something the database depends on is still coming.
		pg.Status.Phase = shpyrdv1.ResourceProvisioning
		pg.Status.Message = pending.Error()
		setResourceCondition(&pg.Status, pg.Generation, metav1.ConditionFalse, "Waiting", pg.Status.Message)
		err, res = nil, ctrl.Result{RequeueAfter: 10 * time.Second}
	}
	if err != nil {
		logger.Error(err, "reconcile postgres failed")
		pg.Status.Phase = shpyrdv1.ResourceFailed
		pg.Status.Message = err.Error()
		setResourceCondition(&pg.Status, pg.Generation, metav1.ConditionFalse, "Error", err.Error())
		r.Recorder.Event(pg, corev1.EventTypeWarning, "ReconcileError", err.Error())
		res = ctrl.Result{RequeueAfter: 30 * time.Second}
	}
	pg.Status.ObservedGeneration = pg.Generation
	if statusErr := r.Status().Patch(ctx, pg, client.MergeFromWithOptions(orig, client.MergeFromWithOptimisticLock{})); statusErr != nil {
		if apierrors.IsConflict(statusErr) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, statusErr
	}
	return res, nil
}

// moveToPostgresSize gives a database made before databases had sizes of
// their own (#57) the Postgres size with the CPU and memory its cluster
// runs with: its spec named a process size, raised to a floor, or none.
// Nothing about the pod changes but PostgreSQL's settings, now the
// size's. It says whether the spec was updated.
func (r *PostgresReconciler) moveToPostgresSize(ctx context.Context, pg *shpyrdv1.Postgres) (bool, error) {
	current := &unstructured.Unstructured{}
	current.SetGroupVersionKind(CNPGClusterGVK)
	if err := r.Get(ctx, types.NamespacedName{Namespace: pg.Namespace, Name: pg.Name}, current); err != nil {
		return false, client.IgnoreNotFound(err)
	}
	if strings.HasPrefix(current.GetAnnotations()[AnnotationPostgresSize], postgresSizeRecord("")) {
		return false, nil
	}
	catalog := loadCatalog(ctx, r.Client, r.SystemNamespace)
	cpu, _, _ := unstructured.NestedString(current.Object, "spec", "resources", "limits", "cpu")
	mem, _, _ := unstructured.NestedString(current.Object, "spec", "resources", "limits", "memory")
	name := sizeLike(catalog.Postgres, cpu, mem)
	if name == "" || name == pg.Spec.Size {
		return false, nil // the cluster's record is rewritten as it is applied
	}
	was := firstNonEmpty(pg.Spec.Size, "none")
	pg.Spec.Size = name
	if err := r.Update(ctx, pg); err != nil {
		return false, err
	}
	r.Recorder.Eventf(pg, corev1.EventTypeNormal, "Sized", "the database had the size %s from before databases had sizes of their own; it now has the Postgres size %s (%s CPU, %s)", was, name, cpu, mem)
	return true, nil
}

// sizeLike names the size of list with this CPU and memory, or failing
// that the smallest with at least both; the largest when none has.
func sizeLike(list *sizes.List, cpu, mem string) string {
	c, errC := resource.ParseQuantity(cpu)
	m, errM := resource.ParseQuantity(mem)
	if list == nil || errC != nil || errM != nil {
		return ""
	}
	sorted := list.Sorted()
	for _, s := range sorted {
		sc, sm := resource.MustParse(s.CPU), resource.MustParse(s.Memory)
		if sc.Cmp(c) == 0 && sm.Cmp(m) == 0 {
			return s.Name
		}
	}
	best := ""
	var bestMem resource.Quantity
	for _, s := range sorted {
		sc, sm := resource.MustParse(s.CPU), resource.MustParse(s.Memory)
		if sc.Cmp(c) >= 0 && sm.Cmp(m) >= 0 && (best == "" || sm.Cmp(bestMem) < 0) {
			best, bestMem = s.Name, sm
		}
	}
	if best == "" && len(sorted) > 0 {
		best = sorted[len(sorted)-1].Name
	}
	return best
}

// postgresSizeRecord is the AnnotationPostgresSize of a cluster rendered
// from the Postgres size name.
func postgresSizeRecord(name string) string { return sizes.ForPostgres + ":" + name }

func (r *PostgresReconciler) reconcile(ctx context.Context, pg *shpyrdv1.Postgres) (ctrl.Result, error) {
	catalog := loadCatalog(ctx, r.Client, r.SystemNamespace)
	size, err := catalog.Postgres.Pick(sizes.ForPostgres, pg.Spec.Size)
	if err != nil {
		return ctrl.Result{}, err
	}
	resources := size.Resources()
	storage := resource.MustParse(defaultPostgresStorage)
	if pg.Spec.Storage != nil {
		storage = *pg.Spec.Storage
	}
	if storage.Sign() <= 0 {
		return ctrl.Result{}, fmt.Errorf("storage must be positive")
	}
	// The provider minimum (RFC-0060): the disk is that size whatever was
	// asked; the status says so.
	pg.Status.Storage = ""
	if rounded, applied := r.Storage.Size(storage); applied {
		storage = rounded
		pg.Status.Storage = rounded.String()
	}

	// Backups (RFC-0038): the bucket, the plugin's store and the schedule
	// exist before the cluster, so WAL archiving starts with it.
	if err := r.reconcileBackups(ctx, pg); err != nil {
		return ctrl.Result{}, err
	}
	if pg.Spec.Recovery != nil {
		if err := r.checkRecoverySource(ctx, pg); err != nil {
			return ctrl.Result{}, err
		}
	}
	current := &unstructured.Unstructured{}
	current.SetGroupVersionKind(CNPGClusterGVK)
	err = r.Get(ctx, types.NamespacedName{Namespace: pg.Namespace, Name: pg.Name}, current)
	storageClass := r.Storage.Class
	if err == nil {
		resources = keepMemory(current, size.Name, resources)
		// A profile switch leaves running clusters as they are: a cluster on
		// another class keeps that class and its disk size (RFC-0060). The
		// profile's minimum belongs to the profile's class; applied to a
		// node-local 1 GiB cluster it would ask for a 50 GiB expansion the
		// local provisioner cannot do, and the other way round it would
		// shrink a provider disk that was rounded up by the former minimum.
		storageClass, storage = keepStorage(current, storageClass, storage, r.dataClass(ctx, pg, current))
		if target := pg.Annotations[AnnotationStorageMigration]; target != "" {
			// Migrating: the new instance provisions on the target class at
			// the size the cluster has (a size change with the class change
			// makes CloudNativePG try to grow the old claim first, and never
			// create the new instance). The profile's minimum applies once
			// the move is done and the annotation is gone.
			storageClass, storage = migratingStorage(current, target, storage)
		}
		if storageClass != r.Storage.Class {
			pg.Status.Storage = ""
			if pg.Spec.Storage == nil || storage.Cmp(*pg.Spec.Storage) != 0 {
				pg.Status.Storage = storage.String()
			}
		}
	}
	desired := desiredCNPGCluster(pg, storage, size, resources, storageClass, r.DataPool)
	if err := controllerutil.SetControllerReference(pg, desired, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	switch {
	case apierrors.IsNotFound(err):
		if err := r.Create(ctx, desired); err != nil {
			if strings.Contains(err.Error(), "no matches for kind") {
				log.FromContext(ctx).Info("the postgres extension is not installed (CloudNativePG missing)", "postgres", pg.Name)
				return ctrl.Result{}, fmt.Errorf("databases are not offered on this platform yet: its operator turns them on")
			}
			return ctrl.Result{}, fmt.Errorf("create database cluster: %w", err)
		}
		r.Recorder.Eventf(pg, corev1.EventTypeNormal, "Provisioning", "created PostgreSQL %s cluster %s (%s, %d instance(s))", version(pg), desired.GetName(), storage.String(), instances(pg))
		current = desired
	case err != nil:
		return ctrl.Result{}, fmt.Errorf("get database cluster: %w", err)
	default:
		// Apply the fields we own; CNPG owns the rest of the spec.
		if err := r.updateCluster(ctx, pg, current, desired); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Sleep: shpyrd-owned Service and hibernation management (RFC-0075).
	if err := r.reconcilePostgresSleep(ctx, pg); err != nil {
		// Non-fatal: log and continue; the database still functions.
		log.FromContext(ctx).Error(err, "postgres sleep reconcile failed")
	}
	// Status from the CNPG cluster and its application Secret.
	r.backupStatus(ctx, pg, current)
	pg.Status.Endpoint = fmt.Sprintf("%s-rw.%s.svc:%d", pg.Name, pg.Namespace, PostgresPort)
	pg.Status.CredentialsSecret = PostgresSecretName(pg.Name)
	ready, _, _ := unstructured.NestedInt64(current.Object, "status", "readyInstances")
	phase, _, _ := unstructured.NestedString(current.Object, "status", "phase")
	reason, _, _ := unstructured.NestedString(current.Object, "status", "phaseReason")
	secretExists := false
	if err := r.Get(ctx, types.NamespacedName{Namespace: pg.Namespace, Name: PostgresSecretName(pg.Name)}, &corev1.Secret{}); err == nil {
		secretExists = true
	}
	switch {
	case strings.Contains(strings.ToLower(phase), "failed") || strings.Contains(strings.ToLower(phase), "unrecoverable"):
		pg.Status.Phase = shpyrdv1.ResourceFailed
		pg.Status.Message = firstNonEmpty(reason, phase)
		setResourceCondition(&pg.Status, pg.Generation, metav1.ConditionFalse, "ClusterFailed", pg.Status.Message)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	case ready >= 1 && secretExists:
		pg.Status.Phase = shpyrdv1.ResourceReady
		pg.Status.Message = fmt.Sprintf("PostgreSQL %s, %d/%d instance(s) ready", version(pg), ready, instances(pg))
		setResourceCondition(&pg.Status, pg.Generation, metav1.ConditionTrue, "Ready", pg.Status.Message)
		if ready < int64(instances(pg)) {
			return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
		}
		if after, _, _ := r.effectiveSleep(ctx, pg); after > 0 || pg.Spec.Sleep != nil {
			return ctrl.Result{RequeueAfter: time.Minute}, nil // the idle clock (RFC-0075)
		}
		if pg.Spec.Backups != nil {
			return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil // follow the backups
		}
		return ctrl.Result{}, nil
	default:
		// A hibernated cluster reports no ready instance: that is sleep,
		// not provisioning (RFC-0075).
		if desc := pgSleepDescription(pg); desc != "" {
			pg.Status.Phase = shpyrdv1.ResourceReady
			pg.Status.Message = desc
			setResourceCondition(&pg.Status, pg.Generation, metav1.ConditionTrue, "Sleeping", desc)
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		pg.Status.Phase = shpyrdv1.ResourceProvisioning
		pg.Status.Message = firstNonEmpty(phase, "creating the PostgreSQL cluster")
		setResourceCondition(&pg.Status, pg.Generation, metav1.ConditionFalse, "Provisioning", pg.Status.Message)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
}

// updateCluster applies instances, storage growth and resources.
func (r *PostgresReconciler) updateCluster(ctx context.Context, pg *shpyrdv1.Postgres, current, desired *unstructured.Unstructured) error {
	changed := false
	patch := client.MergeFrom(current.DeepCopy())
	if cur, _, _ := unstructured.NestedInt64(current.Object, "spec", "instances"); cur != int64(instances(pg)) {
		_ = unstructured.SetNestedField(current.Object, int64(instances(pg)), "spec", "instances")
		changed = true
	}
	// The storage class follows the render: kept as it is for a running
	// cluster (keepStorage), the target class under a migration.
	wantClass, _, _ := unstructured.NestedString(desired.Object, "spec", "storage", "storageClass")
	if curClass, _, _ := unstructured.NestedString(current.Object, "spec", "storage", "storageClass"); curClass != wantClass {
		if wantClass == "" {
			unstructured.RemoveNestedField(current.Object, "spec", "storage", "storageClass")
		} else {
			_ = unstructured.SetNestedField(current.Object, wantClass, "spec", "storage", "storageClass")
		}
		changed = true
	}
	wantSize, _, _ := unstructured.NestedString(desired.Object, "spec", "storage", "size")
	if curSize, _, _ := unstructured.NestedString(current.Object, "spec", "storage", "size"); curSize != wantSize {
		want := resource.MustParse(wantSize)
		if cur, err := resource.ParseQuantity(curSize); err == nil && want.Cmp(cur) < 0 {
			return fmt.Errorf("storage cannot shrink (currently %s)", curSize)
		}
		_ = unstructured.SetNestedField(current.Object, wantSize, "spec", "storage", "size")
		changed = true
	}
	// Backups on or off: the plugin entry follows the spec (RFC-0038).
	wantPlugins, _, _ := unstructured.NestedSlice(desired.Object, "spec", "plugins")
	if curPlugins, _, _ := unstructured.NestedSlice(current.Object, "spec", "plugins"); !equalJSON(map[string]interface{}{"p": curPlugins}, map[string]interface{}{"p": wantPlugins}) {
		if len(wantPlugins) == 0 {
			unstructured.RemoveNestedField(current.Object, "spec", "plugins")
		} else {
			_ = unstructured.SetNestedSlice(current.Object, wantPlugins, "spec", "plugins")
		}
		changed = true
	}
	wantRes, _, _ := unstructured.NestedMap(desired.Object, "spec", "resources")
	if curRes, _, _ := unstructured.NestedMap(current.Object, "spec", "resources"); !equalJSON(curRes, wantRes) {
		_ = unstructured.SetNestedMap(current.Object, wantRes, "spec", "resources")
		changed = true
	}
	// The size's parameters (only the keys shpyrd sets; CNPG writes its
	// own beside them) and a small database's liveness patience.
	params, _, _ := unstructured.NestedStringMap(current.Object, "spec", "postgresql", "parameters")
	wantParams, _, _ := unstructured.NestedStringMap(desired.Object, "spec", "postgresql", "parameters")
	if next, ok := withParameters(params, wantParams); ok {
		if len(next) == 0 {
			unstructured.RemoveNestedField(current.Object, "spec", "postgresql", "parameters")
		} else {
			_ = unstructured.SetNestedStringMap(current.Object, next, "spec", "postgresql", "parameters")
		}
		changed = true
	}
	wantLive, wantSet, _ := unstructured.NestedInt64(desired.Object, "spec", "livenessProbeTimeout")
	if curLive, curSet, _ := unstructured.NestedInt64(current.Object, "spec", "livenessProbeTimeout"); curSet != wantSet || curLive != wantLive {
		if wantSet {
			_ = unstructured.SetNestedField(current.Object, wantLive, "spec", "livenessProbeTimeout")
		} else {
			unstructured.RemoveNestedField(current.Object, "spec", "livenessProbeTimeout")
		}
		changed = true
	}
	if was, recorded := current.GetAnnotations()[AnnotationPostgresSize]; !recorded || was != desired.GetAnnotations()[AnnotationPostgresSize] {
		want := desired.GetAnnotations()[AnnotationPostgresSize]
		current.SetAnnotations(mergeMaps(current.GetAnnotations(), map[string]string{AnnotationPostgresSize: want}))
		changed = true
	}
	if pm, _, _ := unstructured.NestedBool(current.Object, "spec", "monitoring", "enablePodMonitor"); !pm {
		_ = unstructured.SetNestedField(current.Object, true, "spec", "monitoring", "enablePodMonitor")
		changed = true
	}
	// The disruption budget follows the instance count (none on a single
	// instance, or a drain waits forever).
	wantPDB, _, _ := unstructured.NestedBool(desired.Object, "spec", "enablePDB")
	if curPDB, set, _ := unstructured.NestedBool(current.Object, "spec", "enablePDB"); !set || curPDB != wantPDB {
		_ = unstructured.SetNestedField(current.Object, wantPDB, "spec", "enablePDB")
		changed = true
	}
	// Node pool (RFC-0077): follow the desired affinity (set or absent).
	wantAff, _, _ := unstructured.NestedMap(desired.Object, "spec", "affinity")
	if curAff, _, _ := unstructured.NestedMap(current.Object, "spec", "affinity"); !equalJSON(curAff, wantAff) {
		if len(wantAff) == 0 {
			unstructured.RemoveNestedField(current.Object, "spec", "affinity")
		} else {
			_ = unstructured.SetNestedMap(current.Object, wantAff, "spec", "affinity")
		}
		changed = true
	}
	if !changed {
		return nil
	}
	if err := r.Patch(ctx, current, patch); err != nil {
		return fmt.Errorf("update database cluster: %w", err)
	}
	r.Recorder.Event(pg, corev1.EventTypeNormal, "Updated", "database cluster settings applied")
	return nil
}

// withMemoryAtLeast raises memory requests and limits to at least floor.
func withMemoryAtLeast(res corev1.ResourceRequirements, floor resource.Quantity) corev1.ResourceRequirements {
	if res.Requests == nil {
		res.Requests = corev1.ResourceList{}
	}
	if res.Limits == nil {
		res.Limits = corev1.ResourceList{}
	}
	if q, ok := res.Requests[corev1.ResourceMemory]; !ok || q.Cmp(floor) < 0 {
		res.Requests[corev1.ResourceMemory] = floor
	}
	if q, ok := res.Limits[corev1.ResourceMemory]; !ok || q.Cmp(floor) < 0 {
		res.Limits[corev1.ResourceMemory] = floor
	}
	return res
}

// keepMemory keeps a running database's memory when the resources asked
// are lower and its size has not changed: memory goes down when a person
// picks a smaller size, never because the catalog's entry shrank. A
// cluster rendered before sizes were recorded as Postgres sizes counts
// as changed (moveToPostgresSize gave it the size it runs with).
func keepMemory(current *unstructured.Unstructured, size string, res corev1.ResourceRequirements) corev1.ResourceRequirements {
	if current.GetAnnotations()[AnnotationPostgresSize] != postgresSizeRecord(size) {
		return res
	}
	raw, _, _ := unstructured.NestedString(current.Object, "spec", "resources", "limits", "memory")
	cur, err := resource.ParseQuantity(raw)
	if err != nil {
		return res
	}
	return withMemoryAtLeast(res, cur)
}

// smallPostgresParameters are PostgreSQL's settings for a size under
// postgresTunedBelow, measured at 128Mi (#53); max_connections is the
// size's when it says.
var smallPostgresParameters = map[string]string{
	"shared_buffers":       "16MB",
	"work_mem":             "1MB",
	"maintenance_work_mem": "16MB",
	"effective_cache_size": "48MB",
	"max_connections":      "20",
	"wal_buffers":          "1MB",
	"autovacuum_work_mem":  "16MB",
}

// smallPostgres says a size runs with smallPostgresParameters.
func smallPostgres(size sizes.Size) bool {
	mem := resource.MustParse(size.Memory)
	return mem.Cmp(resource.MustParse(postgresTunedBelow)) < 0
}

// postgresParameters are PostgreSQL's settings for a size. Under
// postgresTunedBelow, those measured at 128Mi; from it, the rule measured
// at 256Mi, 512Mi and 1Gi (#57): shared_buffers a quarter of the memory,
// the cache the planner counts on three quarters, a sixteenth for index
// builds and vacuum, and the rest shared by four sorts or hashes per
// connection. max_connections is the size's (PostgreSQL's 100 when the
// size does not say).
func postgresParameters(size sizes.Size) map[string]string {
	conns := size.Connections
	if smallPostgres(size) {
		out := map[string]string{}
		for k, v := range smallPostgresParameters {
			out[k] = v
		}
		if conns > 0 {
			out["max_connections"] = fmt.Sprint(conns)
		}
		return out
	}
	if conns <= 0 {
		conns = 100
	}
	mem := resource.MustParse(size.Memory)
	mib := mem.Value() >> 20
	buffers := mib / 4
	maintenance := min(mib/16, 2048)
	work := max((mib-buffers)*1024/(4*int64(conns)), 1024)
	return map[string]string{
		"shared_buffers":       fmt.Sprintf("%dMB", buffers),
		"effective_cache_size": fmt.Sprintf("%dMB", mib*3/4),
		"maintenance_work_mem": fmt.Sprintf("%dMB", maintenance),
		"work_mem":             fmt.Sprintf("%dkB", work),
		"max_connections":      fmt.Sprint(conns),
	}
}

// withParameters sets the keys shpyrd manages (those of
// smallPostgresParameters, a superset of the larger sizes') in params to
// want's, removing those want leaves out, and leaves CNPG's own keys
// alone; ok says something changed.
func withParameters(params, want map[string]string) (map[string]string, bool) {
	next := map[string]string{}
	for k, v := range params {
		next[k] = v
	}
	changed := false
	for k := range smallPostgresParameters {
		w, wanted := want[k]
		v, has := next[k]
		switch {
		case wanted && (!has || v != w):
			next[k] = w
			changed = true
		case !wanted && has:
			delete(next, k)
			changed = true
		}
	}
	return next, changed
}

// PostgresSecretName is the Secret CNPG creates for the application user.
func PostgresSecretName(name string) string { return name + "-app" }

func version(pg *shpyrdv1.Postgres) string {
	return firstNonEmpty(pg.Spec.Version, defaultPostgresVersion)
}

func instances(pg *shpyrdv1.Postgres) int32 {
	if pg.Spec.Instances != nil && *pg.Spec.Instances > 0 {
		return *pg.Spec.Instances
	}
	return 1
}

// desiredCNPGCluster renders the CloudNativePG Cluster for a Postgres of
// a size, with res its resources (more memory than the size's when kept).
func desiredCNPGCluster(pg *shpyrdv1.Postgres, storage resource.Quantity, size sizes.Size, res corev1.ResourceRequirements, storageClass string, pool string) *unstructured.Unstructured {
	toMap := func(l corev1.ResourceList) map[string]interface{} {
		out := map[string]interface{}{}
		for k, v := range l {
			out[string(k)] = v.String()
		}
		return out
	}
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(CNPGClusterGVK)
	u.SetName(pg.Name)
	u.SetNamespace(pg.Namespace)
	u.SetLabels(map[string]string{
		shpyrdv1.LabelManagedBy: "shpyrd",
		"shpyrd.io/postgres":    pg.Name,
	})
	u.SetAnnotations(map[string]string{AnnotationPostgresSize: postgresSizeRecord(size.Name)})
	spec := map[string]interface{}{
		"instances":             int64(instances(pg)),
		"imageName":             "ghcr.io/cloudnative-pg/postgresql:" + version(pg),
		"storage":               cnpgStorage(storage, storageClass),
		"resources":             map[string]interface{}{"requests": toMap(res.Requests), "limits": toMap(res.Limits)},
		"bootstrap":             map[string]interface{}{"initdb": map[string]interface{}{"database": PostgresDatabase, "owner": PostgresUser}},
		"enableSuperuserAccess": false,
		// Metrics (cnpg_backends_total, ...) scraped by the platform's
		// Prometheus: the sleep activity signal reads them (RFC-0075).
		"monitoring": map[string]interface{}{"enablePodMonitor": true},
		// CloudNativePG guards the primary with a PodDisruptionBudget. On a
		// single instance that budget can never be met, so a node drain
		// (the autoscaler, a node roll) waits on it forever; the instance
		// restarts on another node either way, and a drain is how its disk
		// follows it there. Clusters with replicas keep the budget: a drain
		// then waits for a switchover, which is the point.
		"enablePDB": instances(pg) > 1,
	}
	params := map[string]interface{}{}
	for k, v := range postgresParameters(size) {
		params[k] = v
	}
	spec["postgresql"] = map[string]interface{}{"parameters": params}
	if smallPostgres(size) {
		spec["livenessProbeTimeout"] = int64(smallPostgresLivenessTimeout)
	}
	if pool != "" {
		// Databases are stateful and single-instance: the pool kept for
		// the projects' data (RFC-0077).
		spec["affinity"] = map[string]interface{}{"nodeSelector": map[string]interface{}{PoolLabel: pool}}
	}
	// A node pin stays as it is during a migration: changing the pod spec
	// of the running primary makes CloudNativePG restart it at a moment of
	// its choosing. The migration refuses a pinned database and asks for
	// the pin to be cleared first (its own, announced restart).
	if node := pg.Annotations[shpyrdv1.AnnotationPlacement]; node != "" {
		selector := map[string]interface{}{corev1.LabelHostname: node}
		if pool != "" {
			selector[PoolLabel] = pool
		}
		spec["affinity"] = map[string]interface{}{"nodeSelector": selector}
	}
	// Backups and recovery (RFC-0038).
	if pg.Spec.Backups != nil {
		spec["plugins"] = []interface{}{backupPlugin(pg)}
	}
	if pg.Spec.Recovery != nil {
		bootstrap, external := recoveryBootstrap(pg)
		spec["bootstrap"] = bootstrap
		spec["externalClusters"] = external
	}
	u.Object["spec"] = spec
	return u
}

// setResourceCondition maintains the Ready condition of a resource status.
func setResourceCondition(st *shpyrdv1.ResourceStatus, generation int64, status metav1.ConditionStatus, reason, msg string) {
	cond := metav1.Condition{Type: shpyrdv1.ConditionReady, Status: status, Reason: reason, Message: msg, LastTransitionTime: metav1.Now(), ObservedGeneration: generation}
	for i, c := range st.Conditions {
		if c.Type == cond.Type {
			if c.Status == cond.Status {
				cond.LastTransitionTime = c.LastTransitionTime
			}
			st.Conditions[i] = cond
			return
		}
	}
	st.Conditions = append(st.Conditions, cond)
}

// ---- binding ----------------------------------------------------------------

// PostgresBinder exposes a Postgres as config vars (RFC-0003).
type PostgresBinder struct{}

// DefaultPrefix gives DATABASE_URL and friends.
func (PostgresBinder) DefaultPrefix() string { return "DATABASE" }

// ConfigVars reads CNPG's application Secret.
func (PostgresBinder) ConfigVars(ctx context.Context, c client.Client, namespace, name, prefix string) (map[string]string, error) {
	pg := &shpyrdv1.Postgres{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, pg); err != nil {
		return nil, err
	}
	if pg.Status.Phase != shpyrdv1.ResourceReady {
		return nil, &NotReadyError{Msg: fmt.Sprintf("Postgres %s is %s", name, strings.ToLower(firstNonEmpty(pg.Status.Phase, "provisioning")))}
	}
	sec := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: PostgresSecretName(name)}, sec); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, &NotReadyError{Msg: fmt.Sprintf("Postgres %s has no credentials yet", name)}
		}
		return nil, err
	}
	get := func(k string) string { return string(sec.Data[k]) }
	// With a sleep policy apps connect through the shpyrd-owned Service
	// "<name>": it points at the primary while awake and at the wake proxy
	// while asleep (RFC-0075). Without one, CNPG's "-rw" as always. The
	// policy may be the database's own or its workspace's default;
	// the Service exists exactly when one applies, so it is what is asked.
	host := firstNonEmpty(get("host"), name+"-rw")
	sleeps := sleepAfterDuration(pg.Spec.Sleep) > 0 || (pg.Spec.Sleep != nil && pg.Spec.Sleep.Suspended)
	if !sleeps {
		svc := &corev1.Service{}
		sleeps = c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, svc) == nil
	}
	if sleeps {
		host = name
	}
	port := firstNonEmpty(get("port"), fmt.Sprint(PostgresPort))
	db := firstNonEmpty(get("dbname"), PostgresDatabase)
	user := firstNonEmpty(get("username"), PostgresUser)
	pass := get("password")
	// CNPG's ready-made "uri" names its own "-rw" Service; with a sleep
	// policy the URL must carry the same host as DATABASE_HOST, or every
	// framework that reads the URL (Rails, Django, Prisma, node-pg) connects
	// to a Service with no endpoints while the database sleeps and nothing
	// wakes it.
	url := get("uri")
	if sleeps || url == "" {
		url = fmt.Sprintf("postgresql://%s:%s@%s:%s/%s", user, pass, host, port, db)
	}
	return map[string]string{
		prefix + "_URL":      url,
		prefix + "_HOST":     host,
		prefix + "_PORT":     port,
		prefix + "_USER":     user,
		prefix + "_PASSWORD": pass,
		prefix + "_NAME":     db,
	}, nil
}
