package controller

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
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
	sharedFinalizer     = "shpyrd.io/shared-postgres"
	sharedKeep          = 7 * 24 * time.Hour
	sharedServerPrefix  = "pg-shared-"
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
	server, err := r.sharedServerFor(ctx, pg)
	if err != nil {
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
	pg.Status.Phase = shpyrdv1.ResourceReady
	pg.Status.Message = fmt.Sprintf("PostgreSQL %s, shared, on %s", defaultPostgresVersion, server.GetName())
	setResourceCondition(&pg.Status, pg.Generation, metav1.ConditionTrue, "Ready", pg.Status.Message)
	return ctrl.Result{}, nil
}

// finalizeShared is a shared database's deletion: the role and its
// secrets go now, the data stays seven days behind a drop date a platform
// sweep honours, and the Postgres is released.
func (r *PostgresReconciler) finalizeShared(ctx context.Context, pg *shpyrdv1.Postgres) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(pg, sharedFinalizer) {
		return ctrl.Result{}, nil
	}
	hash := tenantHash(pg.UID)
	role := &unstructured.Unstructured{}
	role.SetGroupVersionKind(CNPGDatabaseRoleGVK)
	role.SetNamespace(SharedPostgresNamespace)
	role.SetName(sharedRoleObject(hash))
	if err := r.Delete(ctx, role); client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, err
	}
	if err := r.Delete(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: sharedRoleObject(hash), Namespace: SharedPostgresNamespace}}); client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, err
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
		server = r.desiredSharedServer(name, r.sharedConfig())
		if err := r.Create(ctx, server); err != nil && !apierrors.IsAlreadyExists(err) {
			return nil, err
		}
		r.Recorder.Event(pg, corev1.EventTypeNormal, "SharedServer", "created shared server "+name)
		return server, nil
	}
	return server, err
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

// ensureSharedRoleSecret is the role's password, generated once: the
// basic-auth Secret CloudNativePG reads, in the server's namespace.
func (r *PostgresReconciler) ensureSharedRoleSecret(ctx context.Context, pg *shpyrdv1.Postgres, hash, server string) (string, error) {
	sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: sharedRoleObject(hash), Namespace: SharedPostgresNamespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, sec, func() error {
		sec.Labels = mergeMaps(sec.Labels, sharedTenantLabels(pg, server))
		sec.Type = corev1.SecretTypeBasicAuth
		if sec.Data == nil {
			sec.Data = map[string][]byte{}
		}
		sec.Data["username"] = []byte(sharedRoleName(hash))
		if len(sec.Data["password"]) == 0 {
			sec.Data["password"] = []byte(randomPassword())
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return string(sec.Data["password"]), nil
}

func sharedTenantLabels(pg *shpyrdv1.Postgres, server string) map[string]string {
	return map[string]string{shpyrdv1.LabelManagedBy: "shpyrd", sharedTenantLabel: string(pg.UID), sharedOfServerLabel: server}
}

func (r *PostgresReconciler) ensureSharedRole(ctx context.Context, pg *shpyrdv1.Postgres, hash, server string) error {
	role := &unstructured.Unstructured{}
	role.SetGroupVersionKind(CNPGDatabaseRoleGVK)
	role.SetNamespace(SharedPostgresNamespace)
	role.SetName(sharedRoleObject(hash))
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, role, func() error {
		role.SetLabels(mergeMaps(role.GetLabels(), sharedTenantLabels(pg, server)))
		return unstructured.SetNestedMap(role.Object, map[string]interface{}{
			"cluster":         map[string]interface{}{"name": server},
			"name":            sharedRoleName(hash),
			"login":           true,
			"inherit":         true,
			"connectionLimit": sharedConnectionLimit(pg),
			"passwordSecret":  map[string]interface{}{"name": sharedRoleObject(hash)},
			"ensure":          "present",
		}, "spec")
	})
	return err
}

func (r *PostgresReconciler) ensureSharedDatabase(ctx context.Context, pg *shpyrdv1.Postgres, hash, server string) error {
	db := &unstructured.Unstructured{}
	db.SetGroupVersionKind(CNPGDatabaseGVK)
	db.SetNamespace(SharedPostgresNamespace)
	db.SetName(sharedDBObject(hash))
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, db, func() error {
		db.SetLabels(mergeMaps(db.GetLabels(), sharedTenantLabels(pg, server)))
		return unstructured.SetNestedMap(db.Object, map[string]interface{}{
			"cluster":               map[string]interface{}{"name": server},
			"name":                  sharedDatabaseName(hash),
			"owner":                 sharedRoleName(hash),
			"databaseReclaimPolicy": "retain",
			"ensure":                "present",
		}, "spec")
	})
	return err
}

// ensureSharedAppSecret is the Secret the Binder reads, in the project:
// the host is the database's own Service, the URL carries sslmode=require
// (encrypted; the server's certificate names the server, not the tenant).
func (r *PostgresReconciler) ensureSharedAppSecret(ctx context.Context, pg *shpyrdv1.Postgres, hash, password string) error {
	sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: PostgresSecretName(pg.Name), Namespace: pg.Namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, sec, func() error {
		sec.Labels = mergeMaps(sec.Labels, map[string]string{shpyrdv1.LabelManagedBy: "shpyrd", "shpyrd.io/postgres": pg.Name})
		user, db := sharedRoleName(hash), sharedDatabaseName(hash)
		sec.Data = map[string][]byte{
			"host":     []byte(pg.Name),
			"port":     []byte(fmt.Sprint(PostgresPort)),
			"dbname":   []byte(db),
			"username": []byte(user),
			"password": []byte(password),
			"sslmode":  []byte("require"),
			"uri":      []byte(fmt.Sprintf("postgresql://%s:%s@%s:%d/%s?sslmode=require", user, password, pg.Name, PostgresPort, db)),
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
