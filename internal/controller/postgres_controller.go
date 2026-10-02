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
	// PlatformPool is the node pool databases run on (RFC-0077); "" = any.
	PlatformPool string
	// PlanSleepDefault answers the default idle period before a database
	// of the project namespace hibernates, from its workspace's plan
	// (RFC-0075); "" when there is none. A database with a policy of its
	// own does not ask. Nil: no plan defaults.
	PlanSleepDefault func(ctx context.Context, namespace string) string
}

// cnpgStorage is the CNPG storage section: the size and, when the profile
// names one, the class (RFC-0060).
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
	// postgresMinMemory is the least a database runs with (#53): a size
	// below it is raised to it. Under postgresTunedBelow the database runs
	// with smallPostgresParameters, which keep the instance manager and
	// PostgreSQL inside the limit through a migration's CREATE INDEX and
	// about 20 connections (measured on kind: 128Mi, 0.5 CPU, no OOM).
	postgresMinMemory = sizes.DBMinMemory
	// postgresDefaultMemory is what a database that names no size gets, as
	// before the floor came down; a small plan's database is given db-xs
	// by the API instead.
	postgresDefaultMemory = sizes.DBDefaultMemory
	postgresTunedBelow    = sizes.DBDefaultMemory
	// smallPostgresLivenessTimeout (seconds) lets a small database's
	// instance manager answer late while a migration takes all of its CPU
	// share, instead of being restarted in the middle of it (CNPG's
	// default is 30).
	smallPostgresLivenessTimeout = 120
	// AnnotationPostgresSize on the CNPG Cluster records the size its
	// resources were rendered from: memory goes down only when the size
	// changes, never because a newer platform lowered a floor.
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

func (r *PostgresReconciler) reconcile(ctx context.Context, pg *shpyrdv1.Postgres) (ctrl.Result, error) {
	catalog := loadCatalog(ctx, r.Client, r.SystemNamespace)
	resources, _, err := catalog.Resolve(pg.Spec.Size, corev1.ResourceRequirements{})
	if err != nil {
		return ctrl.Result{}, err
	}
	floor := postgresMinMemory
	if pg.Spec.Size == "" {
		floor = postgresDefaultMemory
	}
	resources = withMemoryFloor(resources, resource.MustParse(floor))
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
	if err == nil {
		resources = keepMemory(current, pg.Spec.Size, resources)
	}
	desired := desiredCNPGCluster(pg, storage, resources, r.Storage.Class, r.PlatformPool)
	if err := controllerutil.SetControllerReference(pg, desired, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	switch {
	case apierrors.IsNotFound(err):
		if err := r.Create(ctx, desired); err != nil {
			if strings.Contains(err.Error(), "no matches for kind") {
				return ctrl.Result{}, fmt.Errorf("the postgres extension is not installed on this cluster (CloudNativePG missing): run `shpyrd extensions enable postgres`")
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
	// The parameters of a small database (only the keys shpyrd sets; CNPG
	// writes its own beside them) and its liveness patience.
	params, _, _ := unstructured.NestedStringMap(current.Object, "spec", "postgresql", "parameters")
	wantParams, _, _ := unstructured.NestedStringMap(desired.Object, "spec", "postgresql", "parameters")
	if next, ok := withSmallParameters(params, wantParams); ok {
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

// withMemoryFloor raises memory requests and limits to at least floor.
func withMemoryFloor(res corev1.ResourceRequirements, floor resource.Quantity) corev1.ResourceRequirements {
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
// are lower and its size has not changed: a floor that came down in a
// newer platform applies to databases created after it. A cluster from
// before the size was recorded counts as unchanged.
func keepMemory(current *unstructured.Unstructured, size string, res corev1.ResourceRequirements) corev1.ResourceRequirements {
	if was, recorded := current.GetAnnotations()[AnnotationPostgresSize]; recorded && was != size {
		return res
	}
	raw, _, _ := unstructured.NestedString(current.Object, "spec", "resources", "limits", "memory")
	cur, err := resource.ParseQuantity(raw)
	if err != nil {
		return res
	}
	return withMemoryFloor(res, cur)
}

// smallPostgresParameters are PostgreSQL's settings for a database under
// postgresTunedBelow; above it CNPG's defaults stay.
var smallPostgresParameters = map[string]string{
	"shared_buffers":       "16MB",
	"work_mem":             "1MB",
	"maintenance_work_mem": "16MB",
	"effective_cache_size": "48MB",
	"max_connections":      "20",
	"wal_buffers":          "1MB",
	"autovacuum_work_mem":  "16MB",
}

// smallPostgres says a database with these resources runs tuned.
func smallPostgres(res corev1.ResourceRequirements) bool {
	mem, ok := res.Limits[corev1.ResourceMemory]
	return ok && mem.Cmp(resource.MustParse(postgresTunedBelow)) < 0
}

// withSmallParameters sets the keys of smallPostgresParameters in params to
// want's (removing those want leaves out) and leaves CNPG's own keys alone;
// ok says something changed.
func withSmallParameters(params, want map[string]string) (map[string]string, bool) {
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

// desiredCNPGCluster renders the CloudNativePG Cluster for a Postgres.
func desiredCNPGCluster(pg *shpyrdv1.Postgres, storage resource.Quantity, res corev1.ResourceRequirements, storageClass string, platformPool string) *unstructured.Unstructured {
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
	u.SetAnnotations(map[string]string{AnnotationPostgresSize: pg.Spec.Size})
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
	}
	if smallPostgres(res) {
		params := map[string]interface{}{}
		for k, v := range smallPostgresParameters {
			params[k] = v
		}
		spec["postgresql"] = map[string]interface{}{"parameters": params}
		spec["livenessProbeTimeout"] = int64(smallPostgresLivenessTimeout)
	}
	if platformPool != "" {
		// Databases are stateful and single-instance: the platform pool,
		// where the autoscaler never drains (RFC-0077).
		spec["affinity"] = map[string]interface{}{"nodeSelector": map[string]interface{}{PoolLabel: platformPool}}
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
	// policy may be the database's own or its workspace plan's default;
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
