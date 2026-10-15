package controller

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/pgdump"
)

// A shared database (storage plan, step 3) is a database and a role on a
// platform-owned CloudNativePG server, always on, with a logical cap: one
// CNPG Database and one DatabaseRole object per tenant, in the server's
// namespace, named from the Postgres UID so no neighbour's project name
// shows in pg_database or pg_roles. The project keeps the Secret the
// Binder reads and a Service named after the database that points at the
// server, so DATABASE_URL has the shape it always had.

const (
	// SharedPostgresNamespace holds the shared servers and their tenants'
	// objects; a platform namespace, not a project's.
	SharedPostgresNamespace = "shpyrd-shared-postgres"
	// sharedServerLabel marks a CNPG Cluster as a shared server.
	sharedServerLabel = "shpyrd.io/shared-postgres"
	// sharedTenantLabel on a tenant's objects: the Postgres UID.
	sharedTenantLabel = "shpyrd.io/shared-tenant"
	// sharedOfServerLabel on a tenant's objects: its server's name.
	sharedOfServerLabel = "shpyrd.io/shared-server"
	// AnnotationSharedServer records which server a database was placed on.
	AnnotationSharedServer = "shpyrd.io/shared-server"
	// AnnotationDropAfter on a tenant's Database object once its Postgres
	// is gone: the data stays until then, a platform sweep drops it.
	AnnotationDropAfter = "shpyrd.io/drop-after"
	// AnnotationRotate on a Postgres asks for new credentials: its value is
	// the moment asked, which the role's Secret and the DatabaseRole record
	// as shpyrd.io/rotated once they carry the new password.
	AnnotationRotate   = "shpyrd.io/rotate"
	annotationRotated  = "shpyrd.io/rotated"
	sharedFinalizer    = "shpyrd.io/shared-postgres"
	sharedKeep         = 7 * 24 * time.Hour
	sharedServerPrefix = "pg-shared-"
)

// CNPGDatabaseGVK and CNPGDatabaseRoleGVK are CloudNativePG's declarative
// database and role (operator 1.30 and later), handled as unstructured.
var (
	CNPGDatabaseGVK     = schema.GroupVersionKind{Group: "postgresql.cnpg.io", Version: "v1", Kind: "Database"}
	CNPGDatabaseRoleGVK = schema.GroupVersionKind{Group: "postgresql.cnpg.io", Version: "v1", Kind: "DatabaseRole"}
)

// SharedPostgres is the profile's shape for shared servers.
type SharedPostgres struct {
	// Instances per server: 2 on the cloud profiles, 1 on kind.
	Instances int32
	// DatabasesPerServer closes a server to new tenants (200 at launch).
	DatabasesPerServer int
}

func (r *PostgresReconciler) sharedConfig() SharedPostgres {
	c := r.Shared
	if c.Instances <= 0 {
		c.Instances = 1
	}
	if c.DatabasesPerServer <= 0 {
		c.DatabasesPerServer = 200
	}
	return c
}

// tenantHash names a tenant's database and role from the Postgres UID.
func tenantHash(uid types.UID) string {
	sum := sha256.Sum256([]byte(uid))
	return hex.EncodeToString(sum[:4])
}

func sharedDatabaseName(hash string) string { return "db_" + hash }
func sharedRoleName(hash string) string     { return "u_" + hash }
func sharedRoleObject(hash string) string   { return "u-" + hash }
func sharedDBObject(hash string) string     { return "db-" + hash }

// sharedConnectionLimit is the role's connection limit by cap tier.
func sharedConnectionLimit(pg *shpyrdv1.Postgres) int64 {
	if pg.Spec.Storage == nil {
		return 20
	}
	gi := pg.Spec.Storage.Value() / (1 << 30)
	switch {
	case gi > 5:
		return 60
	case gi > 1:
		return 40
	default:
		return 20
	}
}

// prepareShared writes what a shared database carries in its metadata,
// once: the finalizer that keeps its data on deletion, and the server it
// is placed on. It says whether it changed the object.
func (r *PostgresReconciler) prepareShared(ctx context.Context, pg *shpyrdv1.Postgres) (bool, error) {
	before := pg.DeepCopy()
	changed := false
	if !controllerutil.ContainsFinalizer(pg, sharedFinalizer) {
		controllerutil.AddFinalizer(pg, sharedFinalizer)
		changed = true
	}
	if pg.Annotations[AnnotationSharedServer] == "" {
		name, err := r.pickSharedServer(ctx)
		if err != nil {
			return false, err
		}
		if pg.Annotations == nil {
			pg.Annotations = map[string]string{}
		}
		pg.Annotations[AnnotationSharedServer] = name
		changed = true
	}
	if !changed {
		return false, nil
	}
	return true, r.Patch(ctx, pg, client.MergeFrom(before))
}

// reconcileShared places a shared database on a server and renders its
// tenant objects; Ready once CloudNativePG applied them.
func (r *PostgresReconciler) reconcileShared(ctx context.Context, pg *shpyrdv1.Postgres) (ctrl.Result, error) {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: SharedPostgresNamespace}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, ns, func() error {
		ns.Labels = mergeMaps(ns.Labels, map[string]string{shpyrdv1.LabelManagedBy: "shpyrd"})
		return nil
	}); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.ensureSharedNetworkPolicy(ctx); err != nil {
		return ctrl.Result{}, err
	}
	server, err := r.sharedServerFor(ctx, pg)
	if err != nil {
		return ctrl.Result{}, err
	}
	if err := r.ensureSharedServerAccess(ctx, server.GetName()); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.ensureSharedDumps(ctx, server); err != nil {
		return ctrl.Result{}, err
	}
	ready, _, _ := unstructured.NestedInt64(server.Object, "status", "readyInstances")
	if ready < 1 {
		phase, _, _ := unstructured.NestedString(server.Object, "status", "phase")
		return ctrl.Result{}, &pendingError{msg: fmt.Sprintf("your database is being placed on shared server %s (%s); this usually takes a few minutes", server.GetName(), firstNonEmpty(phase, "starting"))}
	}
	hash := tenantHash(pg.UID)
	password, err := r.ensureSharedRoleSecret(ctx, pg, hash, server.GetName())
	if err != nil {
		return ctrl.Result{}, err
	}
	if err := r.ensureSharedRole(ctx, pg, hash, server.GetName()); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.ensureSharedDatabase(ctx, pg, hash, server.GetName()); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.ensureSharedAppSecret(ctx, pg, hash, password); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.ensureSharedService(ctx, pg, server.GetName()); err != nil {
		return ctrl.Result{}, err
	}
	pg.Status.CredentialsSecret = PostgresSecretName(pg.Name)
	pg.Status.Endpoint = fmt.Sprintf("%s:%d", pg.Name, PostgresPort)
	pg.Status.Storage = ""
	applied, message, err := r.sharedTenantApplied(ctx, hash)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !applied {
		pg.Status.Phase = shpyrdv1.ResourceProvisioning
		pg.Status.Message = firstNonEmpty(message, "creating your database on shared server "+server.GetName())
		setResourceCondition(&pg.Status, pg.Generation, metav1.ConditionFalse, "Provisioning", pg.Status.Message)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	secured, err := r.ensureSharedGrants(ctx, pg, hash, server.GetName())
	if err != nil {
		return ctrl.Result{}, err
	}
	if !secured {
		pg.Status.Phase = shpyrdv1.ResourceProvisioning
		pg.Status.Message = "securing your database on shared server " + server.GetName()
		setResourceCondition(&pg.Status, pg.Generation, metav1.ConditionFalse, "Provisioning", pg.Status.Message)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	if pg.Spec.Recovery != nil {
		restored, err := r.reconcileSharedRestore(ctx, pg, hash, server.GetName())
		if err != nil {
			return ctrl.Result{}, err
		}
		if !restored {
			pg.Status.Phase = shpyrdv1.ResourceProvisioning
			pg.Status.Message = fmt.Sprintf("loading the copy %s of %s into your database", pg.Spec.Recovery.Backup, pg.Spec.Recovery.From)
			setResourceCondition(&pg.Status, pg.Generation, metav1.ConditionFalse, "Provisioning", pg.Status.Message)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
	}
	fenced, err := r.reconcileSharedFence(ctx, pg, hash, server.GetName())
	if err != nil {
		return ctrl.Result{}, err
	}
	nudged, err := r.reconcileSharedNudge(ctx, pg, hash, server.GetName())
	if err != nil {
		return ctrl.Result{}, err
	}
	pg.Status.Phase = shpyrdv1.ResourceReady
	pg.Status.Message = fmt.Sprintf("PostgreSQL %s, shared, on %s", defaultPostgresVersion, server.GetName())
	switch {
	case fenced:
		pg.Status.Message = fmt.Sprintf("Connections are paused: your database is at %d%% of its %s cap. Free space, or raise the limit; connections return below 95%%.", ptr.Deref(pg.Status.CapPercent, 0), capText(pg))
	case nudged:
		pg.Status.Message = fmt.Sprintf("Writes are paused because the database is over its %s limit. Free space, or raise the limit.", capText(pg))
	}
	setResourceCondition(&pg.Status, pg.Generation, metav1.ConditionTrue, "Ready", pg.Status.Message)
	return ctrl.Result{}, nil
}

// The fence (storage plan, step 3): at 150 % of its cap a shared database's
// role takes connection limit 0, which the tenant cannot undo, and its
// sessions are ended by the server's superuser; both reverse below 95 %.
func sharedFenceJob(hash string) string { return "fence-" + hash }

func capText(pg *shpyrdv1.Postgres) string {
	if pg.Spec.Storage != nil && pg.Spec.Storage.Value() > 0 {
		return pg.Spec.Storage.String()
	}
	return "1Gi"
}

// reconcileSharedFence applies or lifts the fence by the last measurement
// and reports whether the database is fenced.
func (r *PostgresReconciler) reconcileSharedFence(ctx context.Context, pg *shpyrdv1.Postgres, hash, server string) (bool, error) {
	var reason string
	for _, c := range pg.Status.Conditions {
		if c.Type == ConditionCap {
			reason = c.Reason
		}
	}
	percent := ptr.Deref(pg.Status.CapPercent, 0)
	role := &unstructured.Unstructured{}
	role.SetGroupVersionKind(CNPGDatabaseRoleGVK)
	if err := r.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: sharedRoleObject(hash)}, role); err != nil {
		return false, err
	}
	limit, _, _ := unstructured.NestedInt64(role.Object, "spec", "connectionLimit")
	fenced := limit == 0
	switch {
	case reason == CapFar && !fenced:
		before := role.DeepCopy()
		_ = unstructured.SetNestedField(role.Object, int64(0), "spec", "connectionLimit")
		if err := r.Patch(ctx, role, client.MergeFrom(before)); err != nil {
			return false, err
		}
		job := r.desiredSharedSQLJob(pg, hash, server, sharedFenceJob(hash),
			fmt.Sprintf("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE usename = '%s';", sharedRoleName(hash)))
		if err := r.Create(ctx, job); err != nil && !apierrors.IsAlreadyExists(err) {
			return true, err
		}
		r.Recorder.Event(pg, corev1.EventTypeWarning, "CapFenced", fmt.Sprintf("at %d%% of its %s cap: connections paused", percent, capText(pg)))
		return true, nil
	case fenced && reason != CapFar && percent > 0 && percent < 95:
		before := role.DeepCopy()
		_ = unstructured.SetNestedField(role.Object, sharedConnectionLimit(pg), "spec", "connectionLimit")
		if err := r.Patch(ctx, role, client.MergeFrom(before)); err != nil {
			return true, err
		}
		policy := metav1.DeletePropagationBackground
		if err := r.Delete(ctx, &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: sharedFenceJob(hash), Namespace: SharedPostgresNamespace}}, &client.DeleteOptions{PropagationPolicy: &policy}); client.IgnoreNotFound(err) != nil {
			return true, err
		}
		r.Recorder.Event(pg, corev1.EventTypeNormal, "CapReleased", fmt.Sprintf("back at %d%% of its cap: connections return", percent))
		return false, nil
	}
	return fenced, nil
}

// finalizeShared is a shared database's deletion: the role is fenced at
// once (no login, no connections, its sessions ended), a last copy is
// taken, the data stays seven days behind a drop date the platform sweep
// honours, the role and its object go with the data, and the Postgres is
// released now.
func (r *PostgresReconciler) finalizeShared(ctx context.Context, pg *shpyrdv1.Postgres) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(pg, sharedFinalizer) {
		return ctrl.Result{}, nil
	}
	hash, server := tenantHash(pg.UID), pg.Annotations[AnnotationSharedServer]
	role := &unstructured.Unstructured{}
	role.SetGroupVersionKind(CNPGDatabaseRoleGVK)
	err := r.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: sharedRoleObject(hash)}, role)
	if client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, err
	}
	if err == nil {
		before := role.DeepCopy()
		_ = unstructured.SetNestedField(role.Object, false, "spec", "login")
		_ = unstructured.SetNestedField(role.Object, int64(0), "spec", "connectionLimit")
		if err := r.Patch(ctx, role, client.MergeFrom(before)); err != nil {
			return ctrl.Result{}, err
		}
		job := r.desiredSharedSQLJob(pg, hash, server, sharedFinalFenceJob(hash),
			fmt.Sprintf("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE usename = '%s';", sharedRoleName(hash)))
		if err := r.Create(ctx, job); err != nil && !apierrors.IsAlreadyExists(err) {
			return ctrl.Result{}, err
		}
	}
	policy := metav1.DeletePropagationBackground
	for _, job := range []string{sharedGrantsJob(hash), sharedFenceJob(hash), sharedNudgeJob(hash), sharedUnnudgeJob(hash)} {
		if err := r.Delete(ctx, &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: job, Namespace: SharedPostgresNamespace}}, &client.DeleteOptions{PropagationPolicy: &policy}); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
	}
	db := &unstructured.Unstructured{}
	db.SetGroupVersionKind(CNPGDatabaseGVK)
	if err := r.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: sharedDBObject(hash)}, db); err == nil {
		before := db.DeepCopy()
		ann := db.GetAnnotations()
		if ann == nil {
			ann = map[string]string{}
		}
		if ann[AnnotationDropAfter] == "" {
			ann[AnnotationDropAfter] = time.Now().UTC().Add(sharedKeep).Format(time.RFC3339)
		}
		if ann[annotationSharedLastDump] == "" && ann[annotationSharedGrants] == "done" {
			name, err := r.sharedLastDump(ctx, pg, server)
			if err != nil {
				return ctrl.Result{}, err
			}
			if name != "" {
				ann[annotationSharedLastDump] = name
			}
		}
		db.SetAnnotations(ann)
		_ = unstructured.SetNestedField(db.Object, "retain", "spec", "databaseReclaimPolicy")
		if err := r.Patch(ctx, db, client.MergeFrom(before)); err != nil {
			return ctrl.Result{}, err
		}
	} else if client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, err
	}
	before := pg.DeepCopy()
	controllerutil.RemoveFinalizer(pg, sharedFinalizer)
	return ctrl.Result{}, r.Patch(ctx, pg, client.MergeFrom(before))
}

// sharedFinalFenceJob ends a deleted database's sessions; its own name,
// since the cap's fence Job may exist and be finished.
func sharedFinalFenceJob(hash string) string { return "fence-" + hash + "-final" }

// annotationSharedLastDump records, on the Database object, the copy taken
// at deletion.
const annotationSharedLastDump = "shpyrd.io/last-dump"

// sharedLastDump starts the copy taken at deletion, when the server has
// its dumps; the copy's name, or "" when there is nothing to run it with.
func (r *PostgresReconciler) sharedLastDump(ctx context.Context, pg *shpyrdv1.Postgres, server string) (string, error) {
	cj := &batchv1.CronJob{}
	err := r.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: SharedDumpsName(server)}, cj)
	if apierrors.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	at := time.Now().UTC().Truncate(time.Second)
	if err := r.Create(ctx, SharedDumpJob(pg, cj, at, pgdump.KindLast)); err != nil && !apierrors.IsAlreadyExists(err) {
		return "", err
	}
	return pgdump.Name(at, pgdump.KindLast), nil
}

// pickSharedServer is the server a new database goes to: the open one
// with the fewest tenants, else the next one (created on first use).
func (r *PostgresReconciler) pickSharedServer(ctx context.Context) (string, error) {
	cfg := r.sharedConfig()
	servers := &unstructured.UnstructuredList{}
	servers.SetGroupVersionKind(CNPGClusterGVK.GroupVersion().WithKind("ClusterList"))
	if err := r.List(ctx, servers, client.InNamespace(SharedPostgresNamespace), client.MatchingLabels{sharedServerLabel: "server"}); err != nil {
		return "", err
	}
	tenants := &unstructured.UnstructuredList{}
	tenants.SetGroupVersionKind(CNPGDatabaseGVK.GroupVersion().WithKind("DatabaseList"))
	if err := r.List(ctx, tenants, client.InNamespace(SharedPostgresNamespace)); err != nil {
		return "", err
	}
	count := map[string]int{}
	for _, t := range tenants.Items {
		count[t.GetLabels()[sharedOfServerLabel]]++
	}
	// Databases placed but not yet rendered count too.
	placed := &shpyrdv1.PostgresList{}
	if err := r.List(ctx, placed); err != nil {
		return "", err
	}
	seen := map[string]bool{}
	for _, t := range tenants.Items {
		seen[t.GetLabels()[sharedTenantLabel]] = true
	}
	for _, p := range placed.Items {
		if name := p.Annotations[AnnotationSharedServer]; name != "" && !seen[string(p.UID)] {
			count[name]++
		}
	}
	names := make([]string, 0, len(servers.Items))
	for _, s := range servers.Items {
		names = append(names, s.GetName())
	}
	sort.Strings(names)
	best, bestCount := "", -1
	for _, n := range names {
		if count[n] >= cfg.DatabasesPerServer {
			continue
		}
		if bestCount < 0 || count[n] < bestCount {
			best, bestCount = n, count[n]
		}
	}
	if best != "" {
		return best, nil
	}
	return fmt.Sprintf("%s%d", sharedServerPrefix, len(names)+1), nil
}

// sharedServerFor is the server recorded on a database, created on first
// use.
func (r *PostgresReconciler) sharedServerFor(ctx context.Context, pg *shpyrdv1.Postgres) (*unstructured.Unstructured, error) {
	name := pg.Annotations[AnnotationSharedServer]
	if name == "" {
		return nil, fmt.Errorf("the database has no shared server recorded yet")
	}
	server := &unstructured.Unstructured{}
	server.SetGroupVersionKind(CNPGClusterGVK)
	err := r.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: name}, server)
	if apierrors.IsNotFound(err) {
		backed, err := r.ensureSharedServerBackups(ctx, name)
		if err != nil {
			return nil, err
		}
		server = r.desiredSharedServer(name, r.sharedConfig())
		if backed {
			_ = unstructured.SetNestedSlice(server.Object, []interface{}{sharedServerBackupPlugin(name)}, "spec", "plugins")
		}
		if err := r.Create(ctx, server); err != nil && !apierrors.IsAlreadyExists(err) {
			return nil, err
		}
		r.Recorder.Event(pg, corev1.EventTypeNormal, "SharedServer", "created shared server "+name)
		return server, nil
	}
	if err != nil {
		return nil, err
	}
	backed, err := r.ensureSharedServerBackups(ctx, name)
	if err != nil {
		return nil, err
	}
	if backed {
		if err := r.ensureSharedServerPlugin(ctx, server); err != nil {
			return nil, err
		}
	}
	return server, nil
}

// desiredSharedServer renders a shared server (the plan's shape): the
// profile's instances on the data pool, the database storage class at the
// provider minimum, superuser access for the platform, the parameters
// hundreds of small databases need.
func (r *PostgresReconciler) desiredSharedServer(name string, cfg SharedPostgres) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(CNPGClusterGVK)
	u.SetName(name)
	u.SetNamespace(SharedPostgresNamespace)
	u.SetLabels(map[string]string{shpyrdv1.LabelManagedBy: "shpyrd", sharedServerLabel: "server"})
	spec := map[string]interface{}{
		"instances":             int64(cfg.Instances),
		"imageName":             "ghcr.io/cloudnative-pg/postgresql:" + defaultPostgresVersion,
		"storage":               cnpgStorage(resourceMustParse(firstNonEmpty(r.Storage.MinSize, "1Gi")), r.Storage.Class),
		"resources":             map[string]interface{}{"requests": map[string]interface{}{"cpu": "500m", "memory": "2Gi"}, "limits": map[string]interface{}{"memory": "4Gi"}},
		"enableSuperuserAccess": true,
		"managed":               map[string]interface{}{"roles": []interface{}{sharedDumpManagedRole(name)}},
		"monitoring":            map[string]interface{}{"enablePodMonitor": true},
		"enablePDB":             cfg.Instances > 1,
		"postgresql": map[string]interface{}{"parameters": map[string]interface{}{
			"max_connections":                     "400",
			"temp_file_limit":                     "1GB",
			"idle_in_transaction_session_timeout": "5min",
		}},
	}
	affinity := map[string]interface{}{}
	if r.DataPool != "" {
		affinity["nodeSelector"] = map[string]interface{}{PoolLabel: r.DataPool}
	}
	if cfg.Instances > 1 {
		affinity["enablePodAntiAffinity"] = true
		affinity["topologyKey"] = corev1.LabelHostname
		affinity["podAntiAffinityType"] = "required"
	}
	if len(affinity) > 0 {
		spec["affinity"] = affinity
	}
	u.Object["spec"] = spec
	return u
}

// ensureSharedRoleSecret is the role's password, generated once and again
// at each rotation asked for: the basic-auth Secret CloudNativePG reads,
// in the server's namespace.
func (r *PostgresReconciler) ensureSharedRoleSecret(ctx context.Context, pg *shpyrdv1.Postgres, hash, server string) (string, error) {
	sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: sharedRoleObject(hash), Namespace: SharedPostgresNamespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, sec, func() error {
		sec.Labels = mergeMaps(sec.Labels, sharedTenantLabels(pg, server))
		sec.Type = corev1.SecretTypeBasicAuth
		if sec.Data == nil {
			sec.Data = map[string][]byte{}
		}
		sec.Data["username"] = []byte(sharedRoleName(hash))
		rotate := pg.Annotations[AnnotationRotate]
		if len(sec.Data["password"]) == 0 || (rotate != "" && sec.Annotations[annotationRotated] != rotate) {
			sec.Data["password"] = []byte(randomPassword())
		}
		if rotate != "" {
			sec.Annotations = mergeMaps(sec.Annotations, map[string]string{annotationRotated: rotate})
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return string(sec.Data["password"]), nil
}

// sharedTenantLabels mark a tenant's objects with the Postgres they belong
// to, so a change to them reconciles that Postgres.
func sharedTenantLabels(pg *shpyrdv1.Postgres, server string) map[string]string {
	return map[string]string{shpyrdv1.LabelManagedBy: "shpyrd", sharedTenantLabel: string(pg.UID), sharedOfServerLabel: server,
		sharedTenantNamespaceLabel: pg.Namespace, sharedTenantNameLabel: pg.Name}
}

const (
	sharedTenantNamespaceLabel = "shpyrd.io/postgres-namespace"
	sharedTenantNameLabel      = "shpyrd.io/postgres-name"
)

// sharedTenantToPostgres maps a tenant object to its Postgres.
func sharedTenantToPostgres(_ context.Context, obj client.Object) []reconcile.Request {
	l := obj.GetLabels()
	if l[sharedTenantNamespaceLabel] == "" || l[sharedTenantNameLabel] == "" {
		return nil
	}
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: l[sharedTenantNamespaceLabel], Name: l[sharedTenantNameLabel]}}}
}

func (r *PostgresReconciler) ensureSharedRole(ctx context.Context, pg *shpyrdv1.Postgres, hash, server string) error {
	role := &unstructured.Unstructured{}
	role.SetGroupVersionKind(CNPGDatabaseRoleGVK)
	role.SetNamespace(SharedPostgresNamespace)
	role.SetName(sharedRoleObject(hash))
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, role, func() error {
		role.SetLabels(mergeMaps(role.GetLabels(), sharedTenantLabels(pg, server)))
		// A rotation stamps the object, so the operator reconciles the role
		// and applies the password its Secret now holds.
		if rotate := pg.Annotations[AnnotationRotate]; rotate != "" {
			role.SetAnnotations(mergeMaps(role.GetAnnotations(), map[string]string{annotationRotated: rotate}))
		}
		// The fence owns the connection limit while it is on (0); the tier's
		// limit otherwise, and again when the fence lifts.
		limit := sharedConnectionLimit(pg)
		if current, found, _ := unstructured.NestedInt64(role.Object, "spec", "connectionLimit"); found && current == 0 {
			limit = 0
		}
		return unstructured.SetNestedMap(role.Object, map[string]interface{}{
			"cluster":         map[string]interface{}{"name": server},
			"name":            sharedRoleName(hash),
			"login":           true,
			"inherit":         true,
			"connectionLimit": limit,
			"passwordSecret":  map[string]interface{}{"name": sharedRoleObject(hash)},
			"ensure":          "present",
			// Dropped with its object: the sweep deletes the object once
			// the data is gone (a role owning a database cannot be dropped).
			"databaseRoleReclaimPolicy": "delete",
		}, "spec")
	})
	return err
}

// sharedExtensions are installed in every shared database by the platform
// (the operator creates them, as the Database object says): the ones the
// owner cannot create alone because PostgreSQL does not mark them trusted,
// and the plan calls essential. pgvector weighs 184 KB per database. The
// trusted extensions (pgcrypto, uuid-ossp, citext, hstore, pg_trgm,
// unaccent, btree_gin, btree_gist, ltree, intarray and the rest PostgreSQL
// marks so) are the owner's own CREATE EXTENSION; anything else the image
// does not carry or PostgreSQL does not trust is a dedicated database's.
var sharedExtensions = []string{"vector"}

func (r *PostgresReconciler) ensureSharedDatabase(ctx context.Context, pg *shpyrdv1.Postgres, hash, server string) error {
	db := &unstructured.Unstructured{}
	db.SetGroupVersionKind(CNPGDatabaseGVK)
	db.SetNamespace(SharedPostgresNamespace)
	db.SetName(sharedDBObject(hash))
	extensions := make([]interface{}, 0, len(sharedExtensions))
	for _, name := range sharedExtensions {
		extensions = append(extensions, map[string]interface{}{"name": name, "ensure": "present"})
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, db, func() error {
		db.SetLabels(mergeMaps(db.GetLabels(), sharedTenantLabels(pg, server)))
		return unstructured.SetNestedMap(db.Object, map[string]interface{}{
			"cluster":               map[string]interface{}{"name": server},
			"name":                  sharedDatabaseName(hash),
			"owner":                 sharedRoleName(hash),
			"databaseReclaimPolicy": "retain",
			"ensure":                "present",
			"extensions":            extensions,
		}, "spec")
	})
	return err
}

// sharedConnectTimeout, in seconds, is what a client waits for a connection
// to the server: a client that connects while the server fails over (five
// seconds on dev, the primary deleted) would otherwise hang on the old
// address until TCP gives up, two minutes measured; libpq waits forever by
// default. Ten seconds is twice the failover and well under a request's
// patience. A libpq URI parameter; drivers that do not know it ignore it.
const sharedConnectTimeout = "10"

// ensureSharedAppSecret is the Secret the Binder reads, in the project:
// the host is the database's own Service, the URL carries sslmode=require
// (encrypted; the server's certificate names the server, not the tenant)
// and connect_timeout.
func (r *PostgresReconciler) ensureSharedAppSecret(ctx context.Context, pg *shpyrdv1.Postgres, hash, password string) error {
	sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: PostgresSecretName(pg.Name), Namespace: pg.Namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, sec, func() error {
		sec.Labels = mergeMaps(sec.Labels, map[string]string{shpyrdv1.LabelManagedBy: "shpyrd", "shpyrd.io/postgres": pg.Name})
		user, db := sharedRoleName(hash), sharedDatabaseName(hash)
		sec.Data = map[string][]byte{
			"host":            []byte(pg.Name),
			"port":            []byte(fmt.Sprint(PostgresPort)),
			"dbname":          []byte(db),
			"username":        []byte(user),
			"password":        []byte(password),
			"sslmode":         []byte("require"),
			"connect_timeout": []byte(sharedConnectTimeout),
			"uri":             []byte(fmt.Sprintf("postgresql://%s:%s@%s:%d/%s?sslmode=require&connect_timeout=%s", user, password, pg.Name, PostgresPort, db, sharedConnectTimeout)),
		}
		return controllerutil.SetControllerReference(pg, sec, r.Scheme)
	})
	return err
}

// ensureSharedService is the database's Service in the project: a name
// apps keep, pointing at the server's primary.
func (r *PostgresReconciler) ensureSharedService(ctx context.Context, pg *shpyrdv1.Postgres, server string) error {
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: pg.Name, Namespace: pg.Namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
		svc.Labels = mergeMaps(svc.Labels, map[string]string{shpyrdv1.LabelManagedBy: "shpyrd", "shpyrd.io/postgres": pg.Name})
		svc.Spec.Type = corev1.ServiceTypeExternalName
		svc.Spec.ExternalName = fmt.Sprintf("%s-rw.%s.svc.cluster.local", server, SharedPostgresNamespace)
		svc.Spec.Selector = nil
		svc.Spec.ClusterIP, svc.Spec.ClusterIPs = "", nil
		svc.Spec.Ports = []corev1.ServicePort{{Name: "postgres", Port: PostgresPort, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt32(PostgresPort)}}
		return controllerutil.SetControllerReference(pg, svc, r.Scheme)
	})
	return err
}

// sharedTenantApplied reports whether CloudNativePG applied the tenant's
// role and database, with its message when it did not.
func (r *PostgresReconciler) sharedTenantApplied(ctx context.Context, hash string) (bool, string, error) {
	for _, item := range []struct {
		gvk  schema.GroupVersionKind
		name string
	}{{CNPGDatabaseRoleGVK, sharedRoleObject(hash)}, {CNPGDatabaseGVK, sharedDBObject(hash)}} {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(item.gvk)
		if err := r.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: item.name}, u); err != nil {
			return false, "", err
		}
		applied, _, _ := unstructured.NestedBool(u.Object, "status", "applied")
		if !applied {
			message, _, _ := unstructured.NestedString(u.Object, "status", "message")
			return false, message, nil
		}
	}
	return true, "", nil
}

func randomPassword() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// The tenant's one-time grants, run as the server's superuser once the
// operator created the database and the role: nobody but the owner (and
// later the platform's dump role) may connect to the database, and the
// role's temp files are bounded. Recorded on the Database object, so a
// re-run of the Job is never needed and the Postgres status write is
// never raced.
const annotationSharedGrants = "shpyrd.io/grants"

func sharedGrantsJob(hash string) string { return "grant-" + hash }

// ensureSharedGrants runs the grants Job and reports whether it succeeded.
func (r *PostgresReconciler) ensureSharedGrants(ctx context.Context, pg *shpyrdv1.Postgres, hash, server string) (bool, error) {
	db := &unstructured.Unstructured{}
	db.SetGroupVersionKind(CNPGDatabaseGVK)
	if err := r.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: sharedDBObject(hash)}, db); err != nil {
		return false, err
	}
	if db.GetAnnotations()[annotationSharedGrants] == "done" {
		return true, nil
	}
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: sharedGrantsJob(hash), Namespace: SharedPostgresNamespace}}
	err := r.Get(ctx, client.ObjectKeyFromObject(job), job)
	if apierrors.IsNotFound(err) {
		job = r.desiredSharedGrantsJob(pg, hash, server)
		if err := r.Create(ctx, job); err != nil && !apierrors.IsAlreadyExists(err) {
			return false, err
		}
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if job.Status.Succeeded < 1 {
		return false, nil
	}
	before := db.DeepCopy()
	ann := db.GetAnnotations()
	if ann == nil {
		ann = map[string]string{}
	}
	ann[annotationSharedGrants] = "done"
	db.SetAnnotations(ann)
	if err := r.Patch(ctx, db, client.MergeFrom(before)); err != nil {
		return false, err
	}
	return true, nil
}

// desiredSharedGrantsJob is the tenant's one-time grants: nobody but the
// owner and the platform's dump role may open the database.
func (r *PostgresReconciler) desiredSharedGrantsJob(pg *shpyrdv1.Postgres, hash, server string) *batchv1.Job {
	dbName, roleName := sharedDatabaseName(hash), sharedRoleName(hash)
	return r.desiredSharedSQLJob(pg, hash, server, sharedGrantsJob(hash),
		fmt.Sprintf("REVOKE CONNECT ON DATABASE %s FROM PUBLIC; GRANT CONNECT ON DATABASE %s TO %s; ALTER ROLE %s SET temp_file_limit = '1GB';", dbName, dbName, sharedDumpRole, roleName))
}

// desiredSharedSQLJob is one psql run against the server's primary with
// the superuser credentials CloudNativePG keeps in <server>-superuser.
func (r *PostgresReconciler) desiredSharedSQLJob(pg *shpyrdv1.Postgres, hash, server, name, sql string) *batchv1.Job {
	env := func(name, key string) corev1.EnvVar {
		return corev1.EnvVar{Name: name, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: server + "-superuser"}, Key: key}}}
	}
	labels := sharedTenantLabels(pg, server)
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: SharedPostgresNamespace, Labels: labels},
		Spec: batchv1.JobSpec{
			BackoffLimit:            ptr.To[int32](6),
			TTLSecondsAfterFinished: ptr.To[int32](3600),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy:   corev1.RestartPolicyOnFailure,
					SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: ptr.To(true), RunAsUser: ptr.To[int64](26), RunAsGroup: ptr.To[int64](26), SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
					Containers: []corev1.Container{{
						Name:    "psql",
						Image:   "ghcr.io/cloudnative-pg/postgresql:" + defaultPostgresVersion,
						Command: []string{"psql", "-v", "ON_ERROR_STOP=1", "-c", sql},
						Env: []corev1.EnvVar{
							{Name: "PGHOST", Value: server + "-rw"},
							{Name: "PGPORT", Value: fmt.Sprint(PostgresPort)},
							{Name: "PGDATABASE", Value: "postgres"},
							{Name: "PGSSLMODE", Value: "require"},
							env("PGUSER", "username"),
							env("PGPASSWORD", "password"),
						},
						Resources:       corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resourceMustParse("10m"), corev1.ResourceMemory: resourceMustParse("32Mi")}, Limits: corev1.ResourceList{corev1.ResourceMemory: resourceMustParse("128Mi")}},
						SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: ptr.To(false), ReadOnlyRootFilesystem: ptr.To(true), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}},
					}},
				},
			},
		},
	}
}

// The nudge (storage plan, step 3): seven days over the cap, the owner's
// sessions default to read-only. That is a session default the tenant can
// override, so it is advisory and the words say "paused", not "blocked";
// it lifts as soon as the database is back under its cap. Its state is
// recorded on the Database object; the SQL runs as the server's superuser.
const (
	annotationSharedNudge = "shpyrd.io/nudge"
	sharedNudgeAfter      = 7 * 24 * time.Hour
)

func sharedNudgeJob(hash string) string   { return "nudge-" + hash }
func sharedUnnudgeJob(hash string) string { return "unnudge-" + hash }

// reconcileSharedNudge applies or lifts the nudge and reports whether the
// database is nudged.
func (r *PostgresReconciler) reconcileSharedNudge(ctx context.Context, pg *shpyrdv1.Postgres, hash, server string) (bool, error) {
	db := &unstructured.Unstructured{}
	db.SetGroupVersionKind(CNPGDatabaseGVK)
	if err := r.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: sharedDBObject(hash)}, db); err != nil {
		return false, err
	}
	nudged := db.GetAnnotations()[annotationSharedNudge] == "on"
	over := pg.Status.CapOverSince != nil
	longOver := over && time.Since(pg.Status.CapOverSince.Time) >= sharedNudgeAfter
	role := sharedRoleName(hash)
	switch {
	case longOver && !nudged:
		return r.sharedNudgeStep(ctx, pg, db, hash, server, sharedNudgeJob(hash), sharedUnnudgeJob(hash),
			fmt.Sprintf("ALTER ROLE %s SET default_transaction_read_only = on;", role), "on")
	case nudged && !over:
		done, err := r.sharedNudgeStep(ctx, pg, db, hash, server, sharedUnnudgeJob(hash), sharedNudgeJob(hash),
			fmt.Sprintf("ALTER ROLE %s RESET default_transaction_read_only;", role), "off")
		return !done, err
	}
	return nudged, nil
}

// sharedNudgeStep runs one SQL Job and, when it succeeded, records the
// state on the Database object and removes the opposite Job. It reports
// whether the state is recorded.
func (r *PostgresReconciler) sharedNudgeStep(ctx context.Context, pg *shpyrdv1.Postgres, db *unstructured.Unstructured, hash, server, jobName, opposite, sql, state string) (bool, error) {
	job := &batchv1.Job{}
	err := r.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: jobName}, job)
	if apierrors.IsNotFound(err) {
		if err := r.Create(ctx, r.desiredSharedSQLJob(pg, hash, server, jobName, sql)); err != nil && !apierrors.IsAlreadyExists(err) {
			return false, err
		}
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if job.Status.Succeeded < 1 {
		return false, nil
	}
	before := db.DeepCopy()
	ann := db.GetAnnotations()
	if ann == nil {
		ann = map[string]string{}
	}
	ann[annotationSharedNudge] = state
	db.SetAnnotations(ann)
	if err := r.Patch(ctx, db, client.MergeFrom(before)); err != nil {
		return false, err
	}
	policy := metav1.DeletePropagationBackground
	for _, name := range []string{opposite, jobName} {
		if err := r.Delete(ctx, &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: SharedPostgresNamespace}}, &client.DeleteOptions{PropagationPolicy: &policy}); client.IgnoreNotFound(err) != nil {
			return true, err
		}
	}
	if state == "on" {
		r.Recorder.Event(pg, corev1.EventTypeWarning, "CapNudge", "over its cap for seven days: writes paused")
	} else {
		r.Recorder.Event(pg, corev1.EventTypeNormal, "CapNudgeLifted", "back under its cap: writes return")
	}
	return true, nil
}
