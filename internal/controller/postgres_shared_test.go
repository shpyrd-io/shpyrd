package controller

import (
	"context"
	"io"
	"sort"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/pgdump"
)

func sharedReconciler(t *testing.T, perServer int, objs ...client.Object) (*PostgresReconciler, client.Client) {
	base, c := newTestReconciler(t, objs...)
	r := &PostgresReconciler{Client: c, Scheme: base.Scheme, Recorder: record.NewFakeRecorder(50), SystemNamespace: "shpyrd-system",
		Storage: StorageProfile{Class: "oci-bv", MinSize: "50Gi"}, DataPool: "data", Shared: SharedPostgres{Instances: 2, DatabasesPerServer: perServer},
		ServerImage: "ghcr.io/shpyrd-io/shpyrd-server:test"}
	return r, c
}

func sharedPostgres(name, ns, uid string) *shpyrdv1.Postgres {
	cap := resource.MustParse("1Gi")
	return &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, UID: types.UID(uid), Generation: 1}, Spec: shpyrdv1.PostgresSpec{Type: shpyrdv1.PostgresTypeShared, Storage: &cap}}
}

// settle reconciles until the controller stops asking for an immediate
// requeue (the metadata it writes first), at most a few times.
func settle(t *testing.T, r *PostgresReconciler, key types.NamespacedName) {
	t.Helper()
	for i := 0; i < 4; i++ {
		res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: key})
		if err != nil {
			t.Fatal(err)
		}
		if !res.Requeue {
			return
		}
	}
	t.Fatal("the reconcile kept asking for an immediate requeue")
}

func getU(t *testing.T, c client.Client, gvk string, ns, name string) *unstructured.Unstructured {
	t.Helper()
	u := &unstructured.Unstructured{}
	switch gvk {
	case "Cluster":
		u.SetGroupVersionKind(CNPGClusterGVK)
	case "Database":
		u.SetGroupVersionKind(CNPGDatabaseGVK)
	case "DatabaseRole":
		u.SetGroupVersionKind(CNPGDatabaseRoleGVK)
	}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: name}, u); err != nil {
		t.Fatalf("%s %s/%s: %v", gvk, ns, name, err)
	}
	return u
}

func markApplied(t *testing.T, c client.Client, u *unstructured.Unstructured) {
	t.Helper()
	_ = unstructured.SetNestedField(u.Object, true, "status", "applied")
	if err := c.Update(context.Background(), u); err != nil { // the operator's side, in the fake
		t.Fatal(err)
	}
}

// A shared database gets a server rendered the plan's way, then a role and
// a database on it, the Binder's secret and its own Service; Ready once
// CloudNativePG applied the tenant objects.
func TestSharedPostgresGetsAServerAndATenant(t *testing.T) {
	ctx := context.Background()
	pg := sharedPostgres("db", "p-shop", "uid-shop-db")
	r, c := sharedReconciler(t, 200, pg)
	key := types.NamespacedName{Namespace: "p-shop", Name: "db"}
	settle(t, r, key)
	if err := c.Get(ctx, key, pg); err != nil {
		t.Fatal(err)
	}
	if pg.Status.Phase != shpyrdv1.ResourceProvisioning || !strings.Contains(pg.Status.Message, "pg-shared-1") || pg.Annotations[AnnotationSharedServer] != "pg-shared-1" {
		t.Errorf("first pass = %s %q annotations %v", pg.Status.Phase, pg.Status.Message, pg.Annotations)
	}
	server := getU(t, c, "Cluster", SharedPostgresNamespace, "pg-shared-1")
	instances, _, _ := unstructured.NestedInt64(server.Object, "spec", "instances")
	class, _, _ := unstructured.NestedString(server.Object, "spec", "storage", "storageClass")
	size, _, _ := unstructured.NestedString(server.Object, "spec", "storage", "size")
	superuser, _, _ := unstructured.NestedBool(server.Object, "spec", "enableSuperuserAccess")
	pool, _, _ := unstructured.NestedString(server.Object, "spec", "affinity", "nodeSelector", PoolLabel)
	anti, _, _ := unstructured.NestedString(server.Object, "spec", "affinity", "podAntiAffinityType")
	maxConn, _, _ := unstructured.NestedString(server.Object, "spec", "postgresql", "parameters", "max_connections")
	if instances != 2 || class != "oci-bv" || size != "50Gi" || !superuser || pool != "data" || anti != "required" || maxConn != "400" {
		t.Errorf("server = %d instances on %s/%s superuser=%v pool=%s anti=%s max_connections=%s", instances, class, size, superuser, pool, anti, maxConn)
	}
	// The namespace admits its own pods, the projects, the operator,
	// monitoring and the platform; nothing else.
	np := &networkingv1.NetworkPolicy{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: "shared-postgres"}, np); err != nil {
		t.Fatal(err)
	}
	from := np.Spec.Ingress[0].From
	names := []string{}
	for _, peer := range from {
		if peer.NamespaceSelector != nil {
			if n := peer.NamespaceSelector.MatchLabels[corev1.LabelMetadataName]; n != "" {
				names = append(names, n)
			}
		}
	}
	if len(np.Spec.PolicyTypes) != 1 || np.Spec.PolicyTypes[0] != networkingv1.PolicyTypeIngress || len(from) != 5 || from[0].PodSelector == nil || from[1].NamespaceSelector == nil || from[1].NamespaceSelector.MatchExpressions[0].Key != shpyrdv1.LabelProject || strings.Join(names, ",") != "cnpg-system,monitoring,shpyrd-system" {
		t.Errorf("network policy = types %v from %+v (namespaces %v)", np.Spec.PolicyTypes, from, names)
	}
	// The server's pods may read the namespace's Secrets: the operator
	// applies a DatabaseRole from the instance, as the Cluster's account.
	binding := &rbacv1.RoleBinding{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: "pg-shared-1-tenant-secrets"}, binding); err != nil {
		t.Fatal(err)
	}
	access := &rbacv1.Role{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: "pg-shared-1-tenant-secrets"}, access); err != nil {
		t.Fatal(err)
	}
	if binding.RoleRef.Name != "pg-shared-1-tenant-secrets" || len(binding.Subjects) != 1 || binding.Subjects[0].Kind != "ServiceAccount" || binding.Subjects[0].Name != "pg-shared-1" || len(access.Rules) != 1 || access.Rules[0].Resources[0] != "secrets" || strings.Join(access.Rules[0].Verbs, ",") != "get,list,watch" {
		t.Errorf("server access = %+v / %+v", binding.Subjects, access.Rules)
	}
	// The server's dump role, its bucket and its nightly run come with it.
	roles, _, _ := unstructured.NestedSlice(server.Object, "spec", "managed", "roles")
	if len(roles) != 1 {
		t.Fatalf("managed roles = %v", roles)
	}
	dumpRole, _ := roles[0].(map[string]interface{})
	inRoles, _, _ := unstructured.NestedStringSlice(dumpRole, "inRoles")
	dumpSecretRef, _, _ := unstructured.NestedString(dumpRole, "passwordSecret", "name")
	if dumpRole["name"] != "shpyrd_dump" || dumpRole["login"] != true || dumpRole["bypassrls"] != true || dumpRole["superuser"] != nil || len(inRoles) != 1 || inRoles[0] != "pg_read_all_data" || dumpSecretRef != "pg-shared-1-dump" {
		t.Errorf("dump role = %v", dumpRole)
	}
	dumpSecret := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: "pg-shared-1-dump"}, dumpSecret); err != nil || dumpSecret.Type != corev1.SecretTypeBasicAuth || string(dumpSecret.Data["username"]) != "shpyrd_dump" || len(dumpSecret.Data["password"]) == 0 {
		t.Errorf("dump role secret = %v %+v", err, dumpSecret.Data)
	}
	bucket := &shpyrdv1.ObjectBucket{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: "pg-shared-1-dumps"}, bucket); err != nil || bucket.Spec.SecretName != "pg-shared-1-dumps" || bucket.Spec.DeletionPolicy != "Retain" {
		t.Errorf("dumps bucket = %v %+v", err, bucket.Spec)
	}
	cron := &batchv1.CronJob{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: "pg-shared-1-dumps"}, cron); err != nil {
		t.Fatal(err)
	}
	pod := cron.Spec.JobTemplate.Spec.Template.Spec
	if cron.Spec.Schedule != "7 3 * * *" || cron.Spec.ConcurrencyPolicy != batchv1.ForbidConcurrent || len(pod.InitContainers) != 1 || pod.InitContainers[0].Image != r.ServerImage || strings.Join(pod.InitContainers[0].Args, " ") != "copy-self /tools" {
		t.Errorf("dumps cronjob = %s init %+v", cron.Spec.Schedule, pod.InitContainers)
	}
	dump := pod.Containers[0]
	if strings.Join(dump.Command, " ") != "/tools/shpyrd-server dumps" || dump.Env[0].Value != "pg-shared-1-rw" || dump.Env[4].ValueFrom.SecretKeyRef.Name != "pg-shared-1-dump" || dump.EnvFrom[0].SecretRef.Name != "pg-shared-1-dumps" || pod.AutomountServiceAccountToken == nil || *pod.AutomountServiceAccountToken || len(pod.ImagePullSecrets) != 0 {
		t.Errorf("dump container = %+v pull %v", dump, pod.ImagePullSecrets)
	}

	// The server comes up: the tenant's objects follow.
	_ = unstructured.SetNestedField(server.Object, int64(2), "status", "readyInstances")
	_ = unstructured.SetNestedField(server.Object, "Cluster in healthy state", "status", "phase")
	if err := c.Update(ctx, server); err != nil {
		t.Fatal(err)
	}
	settle(t, r, key)
	hash := tenantHash(pg.UID)
	role := getU(t, c, "DatabaseRole", SharedPostgresNamespace, "u-"+hash)
	roleName, _, _ := unstructured.NestedString(role.Object, "spec", "name")
	limit, _, _ := unstructured.NestedInt64(role.Object, "spec", "connectionLimit")
	secretRef, _, _ := unstructured.NestedString(role.Object, "spec", "passwordSecret", "name")
	db := getU(t, c, "Database", SharedPostgresNamespace, "db-"+hash)
	dbName, _, _ := unstructured.NestedString(db.Object, "spec", "name")
	owner, _, _ := unstructured.NestedString(db.Object, "spec", "owner")
	onServer, _, _ := unstructured.NestedString(db.Object, "spec", "cluster", "name")
	extensions, _, _ := unstructured.NestedSlice(db.Object, "spec", "extensions")
	if len(extensions) != 1 || extensions[0].(map[string]interface{})["name"] != "vector" {
		t.Errorf("the database's extensions = %v", extensions)
	}
	roleReclaim, _, _ := unstructured.NestedString(role.Object, "spec", "databaseRoleReclaimPolicy")
	if roleReclaim != "delete" || roleName != "u_"+hash || limit != 20 || secretRef != "u-"+hash || dbName != "db_"+hash || owner != roleName || onServer != "pg-shared-1" || db.GetLabels()[sharedOfServerLabel] != "pg-shared-1" {
		t.Errorf("tenant objects: role %s limit %d secret %s; database %s owner %s on %s labels %v", roleName, limit, secretRef, dbName, owner, onServer, db.GetLabels())
	}
	roleSecret := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: "u-" + hash}, roleSecret); err != nil || roleSecret.Type != corev1.SecretTypeBasicAuth || string(roleSecret.Data["username"]) != roleName || len(roleSecret.Data["password"]) == 0 {
		t.Errorf("role secret = %v %+v", err, roleSecret.Data)
	}
	app := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "p-shop", Name: PostgresSecretName("db")}, app); err != nil {
		t.Fatal(err)
	}
	uri := string(app.Data["uri"])
	if string(app.Data["host"]) != "db" || string(app.Data["dbname"]) != dbName || string(app.Data["username"]) != roleName || string(app.Data["password"]) != string(roleSecret.Data["password"]) || !strings.HasSuffix(uri, "?sslmode=require&connect_timeout=10") || !strings.Contains(uri, "@db:5432/"+dbName) {
		t.Errorf("app secret = %v", app.Data)
	}
	svc := &corev1.Service{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "p-shop", Name: "db"}, svc); err != nil || svc.Spec.Type != corev1.ServiceTypeExternalName || svc.Spec.ExternalName != "pg-shared-1-rw."+SharedPostgresNamespace+".svc.cluster.local" {
		t.Errorf("service = %v %+v", err, svc.Spec)
	}
	if err := c.Get(ctx, key, pg); err != nil {
		t.Fatal(err)
	}
	if pg.Status.Phase != shpyrdv1.ResourceProvisioning {
		t.Errorf("before the operator applies the tenant: %s", pg.Status.Phase)
	}
	// The Binder's view: the URL keeps its encryption and the database's own host.
	vars, err := PostgresBinder{}.ConfigVars(ctx, c, "p-shop", "db", "DATABASE")
	if err == nil {
		t.Errorf("not ready yet must be said: %v", vars)
	}

	// CloudNativePG applies both: the one-time grants run as the server's
	// superuser before the database is Ready.
	markApplied(t, c, role)
	markApplied(t, c, db)
	settle(t, r, key)
	if err := c.Get(ctx, key, pg); err != nil {
		t.Fatal(err)
	}
	if pg.Status.Phase != shpyrdv1.ResourceProvisioning || !strings.Contains(pg.Status.Message, "securing") {
		t.Errorf("before the grants: %s %q", pg.Status.Phase, pg.Status.Message)
	}
	job := &batchv1.Job{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: "grant-" + hash}, job); err != nil {
		t.Fatal(err)
	}
	sql := strings.Join(job.Spec.Template.Spec.Containers[0].Command, " ")
	user := job.Spec.Template.Spec.Containers[0].Env[4]
	if !strings.Contains(sql, "REVOKE CONNECT ON DATABASE "+dbName+" FROM PUBLIC") || !strings.Contains(sql, "GRANT CONNECT ON DATABASE "+dbName+" TO shpyrd_dump") || !strings.Contains(sql, "ALTER ROLE "+roleName+" SET temp_file_limit") || user.ValueFrom.SecretKeyRef.Name != "pg-shared-1-superuser" || job.Spec.Template.Spec.SecurityContext.RunAsNonRoot == nil {
		t.Errorf("grants job = %s env %+v", sql, user)
	}
	job.Status.Succeeded = 1
	if err := c.Status().Update(ctx, job); err != nil {
		if err := c.Update(ctx, job); err != nil {
			t.Fatal(err)
		}
	}
	settle(t, r, key)
	if err := c.Get(ctx, key, pg); err != nil {
		t.Fatal(err)
	}
	if pg.Status.Phase != shpyrdv1.ResourceReady || pg.Status.Endpoint != "db:5432" || pg.Status.CredentialsSecret != "db-app" || !strings.Contains(pg.Status.Message, "shared") {
		t.Errorf("ready = %s %s %s %q", pg.Status.Phase, pg.Status.Endpoint, pg.Status.CredentialsSecret, pg.Status.Message)
	}
	vars, err = PostgresBinder{}.ConfigVars(ctx, c, "p-shop", "db", "DATABASE")
	if err != nil || vars["DATABASE_HOST"] != "db" || vars["DATABASE_NAME"] != dbName || !strings.HasSuffix(vars["DATABASE_URL"], "?sslmode=require&connect_timeout=10") {
		t.Errorf("binder = %v %v", vars, err)
	}
	// No cluster of its own in the project.
	own := &unstructured.Unstructured{}
	own.SetGroupVersionKind(CNPGClusterGVK)
	if err := c.Get(ctx, key, own); err == nil {
		t.Error("a shared database must not get a cluster in its project")
	}
}

// Tenants fill the open server with the fewest databases; a full server
// closes and the next is created.
func TestSharedPostgresFillsAServerThenOpensTheNext(t *testing.T) {
	ctx := context.Background()
	a, b, d := sharedPostgres("a", "p-one", "uid-a"), sharedPostgres("b", "p-two", "uid-b"), sharedPostgres("d", "p-three", "uid-d")
	r, c := sharedReconciler(t, 2, a, b, d)
	ready := func(name string) {
		s := getU(t, c, "Cluster", SharedPostgresNamespace, name)
		_ = unstructured.SetNestedField(s.Object, int64(2), "status", "readyInstances")
		if err := c.Update(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	for _, pg := range []*shpyrdv1.Postgres{a, b} {
		key := client.ObjectKeyFromObject(pg)
		settle(t, r, key)
		ready("pg-shared-1")
		settle(t, r, key)
		if err := c.Get(ctx, key, pg); err != nil || pg.Annotations[AnnotationSharedServer] != "pg-shared-1" {
			t.Errorf("%s placed on %s (%v)", pg.Name, pg.Annotations[AnnotationSharedServer], err)
		}
	}
	key := client.ObjectKeyFromObject(d)
	settle(t, r, key)
	if err := c.Get(ctx, key, d); err != nil || d.Annotations[AnnotationSharedServer] != "pg-shared-2" {
		t.Errorf("the third tenant opens the next server: %s (%v)", d.Annotations[AnnotationSharedServer], err)
	}
	getU(t, c, "Cluster", SharedPostgresNamespace, "pg-shared-2")
}

// A server rendered before the dumps existed gets the dump role; on a
// platform whose server image is in a private registry, the dumps pod
// pulls with the platform's pull secret, mirrored into the server's
// namespace. Each server has its own minute of the dumps hour.
func TestSharedServerGetsTheDumpRoleAndThePullSecret(t *testing.T) {
	ctx := context.Background()
	pull := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "ocir-pull", Namespace: "shpyrd-system"}, Type: corev1.SecretTypeDockerConfigJson, Data: map[string][]byte{corev1.DockerConfigJsonKey: []byte(`{"auths":{}}`)}}
	pg := sharedPostgres("db", "p-shop", "uid-shop-db")
	r, c := sharedReconciler(t, 200, pg, pull)
	server := &unstructured.Unstructured{}
	server.SetGroupVersionKind(CNPGClusterGVK)
	server.SetNamespace(SharedPostgresNamespace)
	server.SetName("pg-shared-1")
	server.SetLabels(map[string]string{sharedServerLabel: "server"})
	_ = unstructured.SetNestedField(server.Object, int64(2), "spec", "instances")
	if err := c.Create(ctx, server); err != nil {
		t.Fatal(err)
	}
	settle(t, r, client.ObjectKeyFromObject(pg))
	server = getU(t, c, "Cluster", SharedPostgresNamespace, "pg-shared-1")
	roles, _, _ := unstructured.NestedSlice(server.Object, "spec", "managed", "roles")
	instances, _, _ := unstructured.NestedInt64(server.Object, "spec", "instances")
	if len(roles) != 1 || instances != 2 {
		t.Errorf("server = %d instances, managed roles %v", instances, roles)
	}
	mirror := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: "ocir-pull"}, mirror); err != nil || mirror.Type != corev1.SecretTypeDockerConfigJson || string(mirror.Data[corev1.DockerConfigJsonKey]) != `{"auths":{}}` {
		t.Errorf("mirrored pull secret = %v %+v", err, mirror)
	}
	cron := &batchv1.CronJob{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: "pg-shared-1-dumps"}, cron); err != nil || len(cron.Spec.JobTemplate.Spec.Template.Spec.ImagePullSecrets) != 1 || cron.Spec.JobTemplate.Spec.Template.Spec.ImagePullSecrets[0].Name != "ocir-pull" {
		t.Errorf("dumps cronjob = %v pull %v", err, cron.Spec.JobTemplate.Spec.Template.Spec.ImagePullSecrets)
	}
	for name, want := range map[string]string{"pg-shared-1": "7 3 * * *", "pg-shared-2": "14 3 * * *", "pg-shared-9": "3 3 * * *"} {
		if got := sharedDumpsSchedule(name); got != want {
			t.Errorf("schedule of %s = %q, want %q", name, got, want)
		}
	}
}

// A shared server's pooler: switched on by a stamp on the server, rendered
// in transaction mode, and once its Deployment answers the tenants'
// Services turn to it; "<name>-direct" and direct_uri keep the primary all
// along, and the binder hands DATABASE_DIRECT_URL.
func TestSharedServerPoolerTurnsTheTenants(t *testing.T) {
	ctx := context.Background()
	pg := sharedPostgres("db", "p-shop", "uid-shop-db")
	r, c := sharedReconciler(t, 200, pg)
	key := client.ObjectKeyFromObject(pg)
	settle(t, r, key)
	server := getU(t, c, "Cluster", SharedPostgresNamespace, "pg-shared-1")
	_ = unstructured.SetNestedField(server.Object, int64(2), "status", "readyInstances")
	if err := c.Update(ctx, server); err != nil {
		t.Fatal(err)
	}
	settle(t, r, key)
	target := func(name string) string {
		t.Helper()
		svc := &corev1.Service{}
		if err := c.Get(ctx, types.NamespacedName{Namespace: "p-shop", Name: name}, svc); err != nil {
			t.Fatalf("service %s: %v", name, err)
		}
		return svc.Spec.ExternalName
	}
	primary := "pg-shared-1-rw." + SharedPostgresNamespace + ".svc.cluster.local"
	if target("db") != primary || target("db-direct") != primary {
		t.Errorf("before the pooler: db -> %s, db-direct -> %s", target("db"), target("db-direct"))
	}
	sec := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "p-shop", Name: "db-app"}, sec); err != nil {
		t.Fatal(err)
	}
	if string(sec.Data["direct_host"]) != "db-direct" || !strings.Contains(string(sec.Data["direct_uri"]), "@db-direct:5432/") || !strings.HasSuffix(string(sec.Data["direct_uri"]), "?sslmode=require&connect_timeout=10") {
		t.Errorf("direct credentials = %s %s", sec.Data["direct_host"], sec.Data["direct_uri"])
	}
	pooler := &unstructured.Unstructured{}
	pooler.SetGroupVersionKind(CNPGPoolerGVK)
	if err := c.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: "pg-shared-1-pooler"}, pooler); err == nil {
		t.Error("no pooler before the server asks for one")
	}

	// The switch: the server stamped (by the metering loop or by hand).
	server = getU(t, c, "Cluster", SharedPostgresNamespace, "pg-shared-1")
	server.SetAnnotations(map[string]string{AnnotationPooler: "2026-10-15T09:00:00Z"})
	if err := c.Update(ctx, server); err != nil {
		t.Fatal(err)
	}
	if reqs := r.sharedServerTenants(ctx, server); len(reqs) != 1 || reqs[0].NamespacedName != key {
		t.Errorf("the server's tenants = %v", reqs)
	}
	res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
	if err != nil || res.RequeueAfter <= 0 {
		t.Errorf("while the pooler comes up, the tenant comes back: %+v %v", res, err)
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: "pg-shared-1-pooler"}, pooler); err != nil {
		t.Fatal(err)
	}
	cluster, _, _ := unstructured.NestedString(pooler.Object, "spec", "cluster", "name")
	kind, _, _ := unstructured.NestedString(pooler.Object, "spec", "type")
	mode, _, _ := unstructured.NestedString(pooler.Object, "spec", "pgbouncer", "poolMode")
	instances, _, _ := unstructured.NestedInt64(pooler.Object, "spec", "instances")
	poolSize, _, _ := unstructured.NestedString(pooler.Object, "spec", "pgbouncer", "parameters", "default_pool_size")
	pool, _, _ := unstructured.NestedString(pooler.Object, "spec", "template", "spec", "nodeSelector", PoolLabel)
	if cluster != "pg-shared-1" || kind != "rw" || mode != "transaction" || instances != 2 || poolSize != "5" || pool != "data" || pooler.GetLabels()[sharedServerLabel] != "pooler" {
		t.Errorf("pooler = %s %s %s x%d pool_size=%s on %s labels %v", cluster, kind, mode, instances, poolSize, pool, pooler.GetLabels())
	}
	if target("db") != primary {
		t.Errorf("the Service waits for the pooler: %s", target("db"))
	}
	// The pooler answers: the tenant's Service turns to it, the direct one stays.
	deploy := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "pg-shared-1-pooler", Namespace: SharedPostgresNamespace}, Status: appsv1.DeploymentStatus{ReadyReplicas: 1}}
	if err := c.Create(ctx, deploy); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatal(err)
	}
	if target("db") != "pg-shared-1-pooler."+SharedPostgresNamespace+".svc.cluster.local" || target("db-direct") != primary {
		t.Errorf("with the pooler: db -> %s, db-direct -> %s", target("db"), target("db-direct"))
	}
	for _, c := range []struct {
		p95  float64
		max  int
		want bool
	}{{241, 400, true}, {240, 400, false}, {300, 0, false}, {0, 400, false}} {
		if got := sharedPoolerWanted(c.p95, c.max); got != c.want {
			t.Errorf("sharedPoolerWanted(%v, %d) = %v", c.p95, c.max, got)
		}
	}
	other := sharedPostgres("x", "p-other", "uid-other")
	if reqs := r.sharedServerTenants(ctx, other); reqs != nil {
		t.Errorf("not a server: %v", reqs)
	}
}

// A shared database that asks for more copies has its count on the
// server's ConfigMap, which the dumps pod reads; back at the default, the
// entry goes.
func TestSharedPostgresKeepsMoreCopiesWhenAsked(t *testing.T) {
	ctx := context.Background()
	pg := sharedPostgres("db", "p-shop", "uid-shop-db")
	pg.Spec.Backups = &shpyrdv1.PostgresBackups{Retention: "14d"}
	r, c := sharedReconciler(t, 200, pg)
	settle(t, r, client.ObjectKeyFromObject(pg))
	server := getU(t, c, "Cluster", SharedPostgresNamespace, "pg-shared-1")
	_ = unstructured.SetNestedField(server.Object, int64(2), "status", "readyInstances")
	if err := c.Update(ctx, server); err != nil {
		t.Fatal(err)
	}
	settle(t, r, client.ObjectKeyFromObject(pg))
	if err := c.Get(ctx, client.ObjectKeyFromObject(pg), pg); err != nil {
		t.Fatal(err)
	}
	key := types.NamespacedName{Namespace: SharedPostgresNamespace, Name: "pg-shared-1-dumps-keep"}
	keep := &corev1.ConfigMap{}
	if err := c.Get(ctx, key, keep); err != nil || keep.Data[SharedDatabaseName(pg)] != "14" || len(keep.Data) != 1 {
		t.Errorf("keep map = %v %v", err, keep.Data)
	}
	cron := &batchv1.CronJob{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: "pg-shared-1-dumps"}, cron); err != nil {
		t.Fatal(err)
	}
	spec := cron.Spec.JobTemplate.Spec.Template.Spec
	var mounted, env, volume bool
	for _, m := range spec.Containers[0].VolumeMounts {
		mounted = mounted || (m.Name == "keep" && m.MountPath == "/etc/dumps/keep" && m.ReadOnly)
	}
	for _, e := range spec.Containers[0].Env {
		env = env || (e.Name == "SHPYRD_DUMPS_KEEP_DIR" && e.Value == "/etc/dumps/keep")
	}
	for _, v := range spec.Volumes {
		volume = volume || (v.Name == "keep" && v.ConfigMap != nil && v.ConfigMap.Name == "pg-shared-1-dumps-keep" && v.ConfigMap.Optional != nil && *v.ConfigMap.Optional)
	}
	if !mounted || !env || !volume {
		t.Errorf("dumps pod: mounted=%v env=%v volume=%v", mounted, env, volume)
	}
	if SharedDumpsKeep(pg) != 14 || SharedDumpsKeep(sharedPostgres("x", "p", "u")) != 7 {
		t.Errorf("keep counts = %d %d", SharedDumpsKeep(pg), SharedDumpsKeep(sharedPostgres("x", "p", "u")))
	}

	pg.Spec.Backups = nil
	if err := c.Update(ctx, pg); err != nil {
		t.Fatal(err)
	}
	settle(t, r, client.ObjectKeyFromObject(pg))
	if err := c.Get(ctx, key, keep); err != nil || len(keep.Data) != 0 {
		t.Errorf("back at the default: %v %v", err, keep.Data)
	}
}

// A shared server gets its own backups once the platform's bucket answers:
// the plugin's ObjectStore, a daily schedule, and the plugin on the
// Cluster, put on a server rendered before the bucket was ready.
func TestSharedServerGetsItsOwnBackups(t *testing.T) {
	ctx := context.Background()
	pg := sharedPostgres("db", "p-shop", "uid-shop-db")
	r, c := sharedReconciler(t, 200, pg)
	key := client.ObjectKeyFromObject(pg)
	settle(t, r, key)
	bucket := &shpyrdv1.ObjectBucket{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: "pg-shared-1-backups"}, bucket); err != nil || bucket.Spec.DeletionPolicy != "Retain" || bucket.Spec.SecretName != "pg-shared-1-backups" {
		t.Fatalf("server backups bucket = %v %+v", err, bucket.Spec)
	}
	server := getU(t, c, "Cluster", SharedPostgresNamespace, "pg-shared-1")
	if plugins, _, _ := unstructured.NestedSlice(server.Object, "spec", "plugins"); len(plugins) != 0 {
		t.Errorf("no plugin before the bucket answers: %v", plugins)
	}
	bucket.Status = shpyrdv1.ObjectBucketStatus{Phase: shpyrdv1.BucketReady, Bucket: "shpyrd-abc", Endpoint: "https://objects.example.test", SecretName: "pg-shared-1-backups"}
	if err := c.Status().Update(ctx, bucket); err != nil {
		if err := c.Update(ctx, bucket); err != nil {
			t.Fatal(err)
		}
	}
	settle(t, r, key)
	store := &unstructured.Unstructured{}
	store.SetGroupVersionKind(BarmanObjectStoreGVK)
	if err := c.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: "pg-shared-1-backups"}, store); err != nil {
		t.Fatal(err)
	}
	path, _, _ := unstructured.NestedString(store.Object, "spec", "configuration", "destinationPath")
	retention, _, _ := unstructured.NestedString(store.Object, "spec", "retentionPolicy")
	keyRef, _, _ := unstructured.NestedString(store.Object, "spec", "configuration", "s3Credentials", "accessKeyId", "name")
	if path != "s3://shpyrd-abc/" || retention != "7d" || keyRef != "pg-shared-1-backups" {
		t.Errorf("object store = path %s retention %s key %s", path, retention, keyRef)
	}
	schedule := &unstructured.Unstructured{}
	schedule.SetGroupVersionKind(CNPGScheduledBackupGVK)
	if err := c.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: "pg-shared-1-scheduled"}, schedule); err != nil {
		t.Fatal(err)
	}
	cron, _, _ := unstructured.NestedString(schedule.Object, "spec", "schedule")
	target, _, _ := unstructured.NestedString(schedule.Object, "spec", "cluster", "name")
	if cron != "0 2 * * *" || target != "pg-shared-1" {
		t.Errorf("schedule = %s for %s", cron, target)
	}
	server = getU(t, c, "Cluster", SharedPostgresNamespace, "pg-shared-1")
	plugins, _, _ := unstructured.NestedSlice(server.Object, "spec", "plugins")
	if len(plugins) != 1 {
		t.Fatalf("plugins = %v", plugins)
	}
	plugin, _ := plugins[0].(map[string]interface{})
	object, _, _ := unstructured.NestedString(plugin, "parameters", "barmanObjectName")
	if plugin["name"] != BarmanPluginName || plugin["isWALArchiver"] != true || object != "pg-shared-1-backups" {
		t.Errorf("plugin = %v", plugin)
	}
	// Idempotent: the same plugin once.
	settle(t, r, key)
	server = getU(t, c, "Cluster", SharedPostgresNamespace, "pg-shared-1")
	if plugins, _, _ := unstructured.NestedSlice(server.Object, "spec", "plugins"); len(plugins) != 1 {
		t.Errorf("plugins after another pass = %v", plugins)
	}
}

// A Postgres carrying its tenant hash (recreated from a platform backup,
// with a new UID) finds its database and role again on its server; a new
// one records the hash its UID gives.
func TestSharedPostgresKeepsItsTenantAcrossARecreation(t *testing.T) {
	ctx := context.Background()
	pg := sharedPostgres("db", "p-shop", "uid-new-after-restore")
	pg.Annotations = map[string]string{AnnotationSharedServer: "pg-shared-1", AnnotationSharedTenant: "cafe0001"}
	r, c := sharedReconciler(t, 200, pg)
	key := client.ObjectKeyFromObject(pg)
	settle(t, r, key)
	server := getU(t, c, "Cluster", SharedPostgresNamespace, "pg-shared-1")
	_ = unstructured.SetNestedField(server.Object, int64(2), "status", "readyInstances")
	if err := c.Update(ctx, server); err != nil {
		t.Fatal(err)
	}
	settle(t, r, key)
	role := getU(t, c, "DatabaseRole", SharedPostgresNamespace, "u-cafe0001")
	name, _, _ := unstructured.NestedString(role.Object, "spec", "name")
	db := getU(t, c, "Database", SharedPostgresNamespace, "db-cafe0001")
	dbName, _, _ := unstructured.NestedString(db.Object, "spec", "name")
	if name != "u_cafe0001" || dbName != "db_cafe0001" {
		t.Errorf("objects named by the recorded hash: role %s database %s", name, dbName)
	}
	fresh := sharedPostgres("db2", "p-shop", "uid-fresh")
	if err := c.Create(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	settle(t, r, client.ObjectKeyFromObject(fresh))
	if err := c.Get(ctx, client.ObjectKeyFromObject(fresh), fresh); err != nil {
		t.Fatal(err)
	}
	if fresh.Annotations[AnnotationSharedTenant] != tenantHash(fresh.UID) {
		t.Errorf("a new database records its hash: %v", fresh.Annotations)
	}
}

// A suspended workspace fences its shared databases' roles (no connections,
// sessions ended) and says so; the fence lifts when the workspace is back.
func TestSharedPostgresSuspendedWorkspaceFencesTheRole(t *testing.T) {
	ctx := context.Background()
	pg := sharedPostgres("db", "p-shop", "uid-shop-db")
	r, c := sharedReconciler(t, 200, pg)
	driveShared(t, r, c, pg)
	hash := tenantHash(pg.UID)
	key := client.ObjectKeyFromObject(pg)
	pg.Annotations[AnnotationWorkspaceSuspended] = "true"
	if err := c.Update(ctx, pg); err != nil {
		t.Fatal(err)
	}
	settle(t, r, key)
	role := getU(t, c, "DatabaseRole", SharedPostgresNamespace, "u-"+hash)
	limit, _, _ := unstructured.NestedInt64(role.Object, "spec", "connectionLimit")
	if err := c.Get(ctx, key, pg); err != nil {
		t.Fatal(err)
	}
	if limit != 0 || !strings.Contains(pg.Status.Message, "your workspace is suspended") {
		t.Errorf("suspended: limit %d, %q", limit, pg.Status.Message)
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: "fence-" + hash}, &batchv1.Job{}); err != nil {
		t.Errorf("the sessions are ended by the fence job: %v", err)
	}
	delete(pg.Annotations, AnnotationWorkspaceSuspended)
	if err := c.Update(ctx, pg); err != nil {
		t.Fatal(err)
	}
	settle(t, r, key)
	role = getU(t, c, "DatabaseRole", SharedPostgresNamespace, "u-"+hash)
	limit, _, _ = unstructured.NestedInt64(role.Object, "spec", "connectionLimit")
	if err := c.Get(ctx, key, pg); err != nil {
		t.Fatal(err)
	}
	if limit != 20 || pg.Status.Phase != shpyrdv1.ResourceReady || strings.Contains(pg.Status.Message, "paused") {
		t.Errorf("back: limit %d, %s %q", limit, pg.Status.Phase, pg.Status.Message)
	}
}

// A dump asked for now is a Job cut from the server's CronJob, for one
// database, named by its moment.
func TestSharedDumpJobIsCutFromTheCronJob(t *testing.T) {
	pg := sharedPostgres("db", "p-shop", "uid-shop-db")
	pg.Annotations = map[string]string{AnnotationSharedServer: "pg-shared-1"}
	r, _ := sharedReconciler(t, 200)
	cj := r.desiredSharedDumpsCronJob("pg-shared-1", nil)
	cj.UID = "cj-uid"
	at := time.Date(2026, 10, 14, 10, 15, 0, 0, time.UTC)
	job := SharedDumpJob(pg, cj, at, "now")
	hash := tenantHash(pg.UID)
	env := map[string]string{}
	for _, e := range job.Spec.Template.Spec.Containers[0].Env {
		env[e.Name] = e.Value
	}
	if job.Name != "dump-"+hash+"-20261014101500" || job.Namespace != SharedPostgresNamespace || env["SHPYRD_DUMPS_DATABASE"] != "db_"+hash || env["SHPYRD_DUMPS_KIND"] != "now" || env["SHPYRD_DUMPS_AT"] != "2026-10-14T10:15:00Z" || env["PGHOST"] != "pg-shared-1-rw" || env["SHPYRD_DUMPS_KEEP"] != "7" {
		t.Errorf("job = %s/%s env %v", job.Namespace, job.Name, env)
	}
	if job.OwnerReferences[0].UID != "cj-uid" || job.Labels[sharedTenantLabel] != "uid-shop-db" || job.Labels[sharedServerLabel] != "dump" || job.Spec.Template.Labels[sharedServerLabel] != "dump" || *job.Spec.BackoffLimit != 1 {
		t.Errorf("job owner %v labels %v pod labels %v", job.OwnerReferences, job.Labels, job.Spec.Template.Labels)
	}
	if got, ok := SharedDumpAt(job); !ok || !got.Equal(at) {
		t.Errorf("SharedDumpAt = %v %v", got, ok)
	}
	if SharedDumpJobsSelector(pg) != "shpyrd.io/shared-tenant=uid-shop-db,shpyrd.io/shared-postgres=dump" {
		t.Errorf("selector = %s", SharedDumpJobsSelector(pg))
	}
	if SharedDatabaseName(pg) != "db_"+hash {
		t.Errorf("database name = %s", SharedDatabaseName(pg))
	}
}

// driveShared takes a shared database up to its grants: the server up,
// the tenant objects applied by the fake operator, the grants Job done.
func driveShared(t *testing.T, r *PostgresReconciler, c client.Client, pg *shpyrdv1.Postgres) {
	t.Helper()
	ctx := context.Background()
	key := client.ObjectKeyFromObject(pg)
	settle(t, r, key)
	if err := c.Get(ctx, key, pg); err != nil {
		t.Fatal(err)
	}
	server := getU(t, c, "Cluster", SharedPostgresNamespace, pg.Annotations[AnnotationSharedServer])
	if ready, _, _ := unstructured.NestedInt64(server.Object, "status", "readyInstances"); ready < 1 {
		_ = unstructured.SetNestedField(server.Object, int64(2), "status", "readyInstances")
		if err := c.Update(ctx, server); err != nil {
			t.Fatal(err)
		}
	}
	settle(t, r, key)
	hash := tenantHash(pg.UID)
	markApplied(t, c, getU(t, c, "DatabaseRole", SharedPostgresNamespace, "u-"+hash))
	markApplied(t, c, getU(t, c, "Database", SharedPostgresNamespace, "db-"+hash))
	settle(t, r, key)
	job := &batchv1.Job{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: "grant-" + hash}, job); err != nil {
		t.Fatal(err)
	}
	job.Status.Succeeded = 1
	if err := c.Status().Update(ctx, job); err != nil {
		if err := c.Update(ctx, job); err != nil {
			t.Fatal(err)
		}
	}
	settle(t, r, key)
	if err := c.Get(ctx, key, pg); err != nil {
		t.Fatal(err)
	}
}

// A new shared database restoring another's copy gets it loaded by a Job
// as its own owner, from the source server's bucket, and is Ready only
// then; a source already deleted is found through the Database object
// kept behind its drop date.
func TestSharedPostgresRestoreLoadsTheCopy(t *testing.T) {
	ctx := context.Background()
	src := sharedPostgres("db", "p-shop", "uid-shop-db")
	tgt := sharedPostgres("db2", "p-shop", "uid-shop-db2")
	tgt.Spec.Recovery = &shpyrdv1.PostgresRecovery{From: "db", Backup: "20261014T030700Z"}
	r, c := sharedReconciler(t, 200, src, tgt)
	driveShared(t, r, c, src)
	if src.Status.Phase != shpyrdv1.ResourceReady {
		t.Fatalf("source = %s %q", src.Status.Phase, src.Status.Message)
	}
	driveShared(t, r, c, tgt)
	if tgt.Status.Phase != shpyrdv1.ResourceProvisioning || !strings.Contains(tgt.Status.Message, "loading the copy 20261014T030700Z of db") {
		t.Errorf("while restoring = %s %q", tgt.Status.Phase, tgt.Status.Message)
	}
	hash, srcHash := tenantHash(tgt.UID), tenantHash(src.UID)
	job := &batchv1.Job{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: "restore-" + hash}, job); err != nil {
		t.Fatal(err)
	}
	pod := job.Spec.Template.Spec
	env := map[string]string{}
	for _, e := range pod.Containers[0].Env {
		if e.Value != "" {
			env[e.Name] = e.Value
		}
	}
	user := pod.Containers[0].Env[4]
	if strings.Join(pod.Containers[0].Command, " ") != "/tools/shpyrd-server restore-dump" || env["PGDATABASE"] != "db_"+hash || env["PGHOST"] != "pg-shared-1-rw" || env["SHPYRD_RESTORE_KEY"] != "db_"+srcHash+"/20261014T030700Z.dump" || user.ValueFrom.SecretKeyRef.Name != "u-"+hash || pod.Containers[0].EnvFrom[0].SecretRef.Name != "pg-shared-1-dumps" || pod.InitContainers[0].Image != r.ServerImage {
		t.Errorf("restore job env %v user %+v envFrom %+v", env, user.ValueFrom.SecretKeyRef, pod.Containers[0].EnvFrom)
	}
	job.Status.Succeeded = 1
	if err := c.Status().Update(ctx, job); err != nil {
		if err := c.Update(ctx, job); err != nil {
			t.Fatal(err)
		}
	}
	key := client.ObjectKeyFromObject(tgt)
	settle(t, r, key)
	if err := c.Get(ctx, key, tgt); err != nil {
		t.Fatal(err)
	}
	db := getU(t, c, "Database", SharedPostgresNamespace, "db-"+hash)
	if tgt.Status.Phase != shpyrdv1.ResourceReady || db.GetAnnotations()[annotationSharedRestored] != "done" {
		t.Errorf("after the job = %s %q, database annotations %v", tgt.Status.Phase, tgt.Status.Message, db.GetAnnotations())
	}

	// The source gone: its kept Database object says where its copies are.
	if err := c.Get(ctx, client.ObjectKeyFromObject(src), src); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(ctx, src); err != nil {
		t.Fatal(err)
	}
	settle(t, r, client.ObjectKeyFromObject(src))
	late := sharedPostgres("db3", "p-shop", "uid-shop-db3")
	late.Spec.Recovery = &shpyrdv1.PostgresRecovery{From: "db", Backup: "20261013T030700Z"}
	if err := c.Create(ctx, late); err != nil {
		t.Fatal(err)
	}
	driveShared(t, r, c, late)
	lateJob := &batchv1.Job{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: "restore-" + tenantHash(late.UID)}, lateJob); err != nil {
		t.Fatal(err)
	}
	for _, e := range lateJob.Spec.Template.Spec.Containers[0].Env {
		if e.Name == "SHPYRD_RESTORE_KEY" && e.Value != "db_"+srcHash+"/20261013T030700Z.dump" {
			t.Errorf("key after the source's deletion = %s", e.Value)
		}
	}

	// A failed Job is said in words, as a failure.
	lateJob.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: "BackoffLimitExceeded", Message: "Job has reached the specified backoff limit"}}
	if err := c.Status().Update(ctx, lateJob); err != nil {
		if err := c.Update(ctx, lateJob); err != nil {
			t.Fatal(err)
		}
	}
	lateKey := client.ObjectKeyFromObject(late)
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: lateKey}); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, lateKey, late); err != nil {
		t.Fatal(err)
	}
	if late.Status.Phase != shpyrdv1.ResourceFailed || !strings.Contains(late.Status.Message, "restoring the copy 20261013T030700Z of db failed") {
		t.Errorf("failed restore = %s %q", late.Status.Phase, late.Status.Message)
	}
}

// A rotation asked for (the annotation's stamp) gives the role a new
// password, in its Secret and the project's, and stamps the DatabaseRole
// so the operator applies it; the same stamp again changes nothing.
func TestSharedPostgresRotateWritesANewPassword(t *testing.T) {
	ctx := context.Background()
	pg := sharedPostgres("db", "p-shop", "uid-shop-db")
	r, c := sharedReconciler(t, 200, pg)
	driveShared(t, r, c, pg)
	hash := tenantHash(pg.UID)
	roleSecretKey := types.NamespacedName{Namespace: SharedPostgresNamespace, Name: "u-" + hash}
	appKey := types.NamespacedName{Namespace: "p-shop", Name: PostgresSecretName("db")}
	password := func(key types.NamespacedName) (string, map[string]string) {
		sec := &corev1.Secret{}
		if err := c.Get(ctx, key, sec); err != nil {
			t.Fatal(err)
		}
		return string(sec.Data["password"]), sec.Annotations
	}
	first, _ := password(roleSecretKey)
	pg.Annotations[AnnotationRotate] = "2026-10-14T20:00:00Z"
	if err := c.Update(ctx, pg); err != nil {
		t.Fatal(err)
	}
	key := client.ObjectKeyFromObject(pg)
	settle(t, r, key)
	second, ann := password(roleSecretKey)
	if second == first || len(second) == 0 || ann[annotationRotated] != "2026-10-14T20:00:00Z" {
		t.Errorf("after the rotation: password changed=%v annotations %v", second != first, ann)
	}
	appPassword, _ := password(appKey)
	app := &corev1.Secret{}
	if err := c.Get(ctx, appKey, app); err != nil || appPassword != second || !strings.Contains(string(app.Data["uri"]), second) {
		t.Errorf("the project's credentials: %v password follows=%v", err, appPassword == second)
	}
	role := getU(t, c, "DatabaseRole", SharedPostgresNamespace, "u-"+hash)
	if role.GetAnnotations()[annotationRotated] != "2026-10-14T20:00:00Z" {
		t.Errorf("the role object is stamped so the operator reconciles it: %v", role.GetAnnotations())
	}
	settle(t, r, key)
	if again, _ := password(roleSecretKey); again != second {
		t.Error("the same stamp rotated again")
	}
	if err := c.Get(ctx, key, pg); err != nil {
		t.Fatal(err)
	}
	pg.Annotations[AnnotationRotate] = "2026-10-15T08:00:00Z"
	if err := c.Update(ctx, pg); err != nil {
		t.Fatal(err)
	}
	settle(t, r, key)
	if third, _ := password(roleSecretKey); third == second {
		t.Error("a new stamp rotates again")
	}
}

// Deleting a shared database removes its role and secrets now and keeps
// the data seven days behind a drop date.
func TestSharedPostgresDeletionKeepsTheDataSevenDays(t *testing.T) {
	ctx := context.Background()
	pg := sharedPostgres("db", "p-shop", "uid-shop-db")
	r, c := sharedReconciler(t, 200, pg)
	key := client.ObjectKeyFromObject(pg)
	driveShared(t, r, c, pg)
	hash := tenantHash(pg.UID)
	if err := c.Delete(ctx, pg); err != nil {
		t.Fatal(err)
	}
	settle(t, r, key)
	// The role stays, fenced: no login, no connections, its sessions ended.
	role := getU(t, c, "DatabaseRole", SharedPostgresNamespace, "u-"+hash)
	login, _, _ := unstructured.NestedBool(role.Object, "spec", "login")
	limit, _, _ := unstructured.NestedInt64(role.Object, "spec", "connectionLimit")
	reclaim, _, _ := unstructured.NestedString(role.Object, "spec", "databaseRoleReclaimPolicy")
	if login || limit != 0 || reclaim != "delete" {
		t.Errorf("the role after deletion: login=%v limit=%d reclaim=%s", login, limit, reclaim)
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: "u-" + hash}, &corev1.Secret{}); err != nil {
		t.Error("the role's secret stays until the role goes:", err)
	}
	fence := &batchv1.Job{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: "fence-" + hash + "-final"}, fence); err != nil || !strings.Contains(strings.Join(fence.Spec.Template.Spec.Containers[0].Command, " "), "pg_terminate_backend") {
		t.Errorf("the final fence job = %v", err)
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: "grant-" + hash}, &batchv1.Job{}); err == nil {
		t.Error("the grants job must be gone")
	}
	// A last copy, named on the Database object.
	db := getU(t, c, "Database", SharedPostgresNamespace, "db-"+hash)
	policy, _, _ := unstructured.NestedString(db.Object, "spec", "databaseReclaimPolicy")
	if db.GetAnnotations()[AnnotationDropAfter] == "" || policy != "retain" {
		t.Errorf("the data stays behind a drop date: %v %s", db.GetAnnotations(), policy)
	}
	last := db.GetAnnotations()[annotationSharedLastDump]
	if !strings.HasSuffix(last, ".last") {
		t.Errorf("last dump = %q", last)
	}
	dumps := &batchv1.JobList{}
	if err := c.List(ctx, dumps, client.InNamespace(SharedPostgresNamespace), client.MatchingLabels{sharedTenantLabel: string(pg.UID), sharedServerLabel: "dump"}); err != nil || len(dumps.Items) != 1 {
		t.Fatalf("dump jobs = %v %d", err, len(dumps.Items))
	}
	kind := ""
	for _, e := range dumps.Items[0].Spec.Template.Spec.Containers[0].Env {
		if e.Name == "SHPYRD_DUMPS_KIND" {
			kind = e.Value
		}
	}
	if kind != "last" {
		t.Errorf("the last dump's kind = %q", kind)
	}
	if err := c.Get(ctx, key, pg); err == nil {
		t.Error("the Postgres must be released once its finalizer is done")
	}
}

// The sweep drops a deleted tenant's data on its date, not before, once.
func TestSharedTenantSweepDropsOnTheDate(t *testing.T) {
	ctx := context.Background()
	stamp := func(at time.Time) *unstructured.Unstructured {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(CNPGDatabaseGVK)
		u.SetNamespace(SharedPostgresNamespace)
		u.SetName("db-deadbeef")
		u.SetAnnotations(map[string]string{AnnotationDropAfter: at.UTC().Format(time.RFC3339)})
		_ = unstructured.SetNestedMap(u.Object, map[string]interface{}{"name": "db_deadbeef", "ensure": "present", "databaseReclaimPolicy": "retain", "cluster": map[string]interface{}{"name": "pg-shared-1"}}, "spec")
		return u
	}
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	key := types.NamespacedName{Namespace: SharedPostgresNamespace, Name: "db-deadbeef"}

	// The server's dumps bucket holds the tenant's copies and another's;
	// the server's copy counts name both.
	copies := &fakeDumpObjects{keys: map[string]bool{"db_deadbeef/20261014T030700Z.dump": true, "db_deadbeef/20261014T120000Z.last.dump": true, "db_other/20261014T030700Z.dump": true}}
	previous := sharedDumpsObjects
	sharedDumpsObjects = func(*corev1.Secret) (pgdump.Objects, error) { return copies, nil }
	t.Cleanup(func() { sharedDumpsObjects = previous })
	dumps := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "pg-shared-1-dumps", Namespace: SharedPostgresNamespace}, Data: map[string][]byte{"BUCKET": []byte("b")}}
	keep := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "pg-shared-1-dumps-keep", Namespace: SharedPostgresNamespace}, Data: map[string]string{"db_deadbeef": "14", "db_other": "30"}}

	_, c := newTestReconciler(t, dumps, keep)
	if err := c.Create(ctx, stamp(now.Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	sweep := &SharedTenantSweep{Client: c, Now: func() time.Time { return now }}
	res, err := sweep.Reconcile(ctx, ctrl.Request{NamespacedName: key})
	if err != nil || res.RequeueAfter <= 0 {
		t.Errorf("a date ahead is waited for: %+v %v", res, err)
	}
	u := getU(t, c, "Database", SharedPostgresNamespace, "db-deadbeef")
	if ensure, _, _ := unstructured.NestedString(u.Object, "spec", "ensure"); ensure != "present" {
		t.Errorf("not before the date: ensure=%s", ensure)
	}

	sweep.Now = func() time.Time { return now.Add(2 * time.Hour) }
	if _, err := sweep.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatal(err)
	}
	u = getU(t, c, "Database", SharedPostgresNamespace, "db-deadbeef")
	ensure, _, _ := unstructured.NestedString(u.Object, "spec", "ensure")
	policy, _, _ := unstructured.NestedString(u.Object, "spec", "databaseReclaimPolicy")
	if ensure != "absent" || policy != "delete" {
		t.Errorf("on the date: ensure=%s policy=%s", ensure, policy)
	}
	// Until the operator says the drop is applied, the rest waits.
	role := &unstructured.Unstructured{}
	role.SetGroupVersionKind(CNPGDatabaseRoleGVK)
	role.SetNamespace(SharedPostgresNamespace)
	role.SetName("u-deadbeef")
	if err := c.Create(ctx, role); err != nil {
		t.Fatal(err)
	}
	if err := c.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "u-deadbeef", Namespace: SharedPostgresNamespace}}); err != nil {
		t.Fatal(err)
	}
	if res, err := sweep.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil || res.RequeueAfter != time.Minute {
		t.Errorf("waiting for the drop: %+v %v", res, err)
	}
	getU(t, c, "DatabaseRole", SharedPostgresNamespace, "u-deadbeef")
	_ = unstructured.SetNestedField(u.Object, true, "status", "applied")
	if err := c.Update(ctx, u); err != nil {
		t.Fatal(err)
	}
	if _, err := sweep.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: "u-deadbeef"}, role); err == nil {
		t.Error("the role object goes with the data")
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: "u-deadbeef"}, &corev1.Secret{}); err == nil {
		t.Error("the role's secret goes with it")
	}
	if err := c.Get(ctx, key, u); err == nil {
		t.Error("the Database object goes last")
	}
	if len(copies.keys) != 1 || !copies.keys["db_other/20261014T030700Z.dump"] {
		t.Errorf("the copies go with the data, another database's stay: %v", copies.keys)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(keep), keep); err != nil || len(keep.Data) != 1 || keep.Data["db_other"] != "30" {
		t.Errorf("the copy count goes with it: %v %v", err, keep.Data)
	}
	if res, err := sweep.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil || res.RequeueAfter != 0 {
		t.Errorf("gone is gone: %+v %v", res, err)
	}
}

// fakeDumpObjects is a dumps bucket in memory.
type fakeDumpObjects struct{ keys map[string]bool }

func (o *fakeDumpObjects) List(_ context.Context, prefix string) ([]string, error) {
	var keys []string
	for k := range o.keys {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

func (o *fakeDumpObjects) Put(_ context.Context, key string, r io.Reader) error {
	_, err := io.Copy(io.Discard, r)
	o.keys[key] = true
	return err
}

func (o *fakeDumpObjects) Remove(_ context.Context, key string) error {
	delete(o.keys, key)
	return nil
}

// A tenant object's change reconciles its Postgres.
func TestSharedTenantObjectsMapToTheirPostgres(t *testing.T) {
	pg := sharedPostgres("db", "p-shop", "uid-shop-db")
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(CNPGDatabaseGVK)
	u.SetLabels(sharedTenantLabels(pg, "pg-shared-1"))
	reqs := sharedTenantToPostgres(context.Background(), u)
	if len(reqs) != 1 || reqs[0].Namespace != "p-shop" || reqs[0].Name != "db" {
		t.Errorf("mapped to %v", reqs)
	}
	u.SetLabels(nil)
	if reqs := sharedTenantToPostgres(context.Background(), u); reqs != nil {
		t.Errorf("an unlabelled object maps to nothing: %v", reqs)
	}
}

// At 150 % of its cap a shared database is fenced: connection limit 0 and
// its sessions ended as the superuser; below 95 % the limit returns.
func TestSharedPostgresFenceFollowsTheCap(t *testing.T) {
	ctx := context.Background()
	pg := sharedPostgres("db", "p-shop", "uid-shop-db")
	r, c := sharedReconciler(t, 200, pg)
	key := client.ObjectKeyFromObject(pg)
	settle(t, r, key)
	server := getU(t, c, "Cluster", SharedPostgresNamespace, "pg-shared-1")
	_ = unstructured.SetNestedField(server.Object, int64(2), "status", "readyInstances")
	if err := c.Update(ctx, server); err != nil {
		t.Fatal(err)
	}
	settle(t, r, key)
	hash := tenantHash(pg.UID)
	markApplied(t, c, getU(t, c, "DatabaseRole", SharedPostgresNamespace, "u-"+hash))
	markApplied(t, c, getU(t, c, "Database", SharedPostgresNamespace, "db-"+hash))
	settle(t, r, key)
	job := &batchv1.Job{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: "grant-" + hash}, job); err != nil {
		t.Fatal(err)
	}
	job.Status.Succeeded = 1
	if err := c.Status().Update(ctx, job); err != nil {
		if err := c.Update(ctx, job); err != nil {
			t.Fatal(err)
		}
	}
	settle(t, r, key)

	// The metering found 160 %.
	if err := c.Get(ctx, key, pg); err != nil {
		t.Fatal(err)
	}
	cap := resource.MustParse("1Gi")
	applyCap(&pg.Status, pg.Generation, int64(float64(cap.Value())*1.6), &cap, time.Now())
	if err := c.Status().Update(ctx, pg); err != nil {
		t.Fatal(err)
	}
	settle(t, r, key)
	role := getU(t, c, "DatabaseRole", SharedPostgresNamespace, "u-"+hash)
	limit, _, _ := unstructured.NestedInt64(role.Object, "spec", "connectionLimit")
	fence := &batchv1.Job{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: "fence-" + hash}, fence); err != nil || limit != 0 || !strings.Contains(strings.Join(fence.Spec.Template.Spec.Containers[0].Command, " "), "pg_terminate_backend") {
		t.Errorf("fenced: limit %d job %v", limit, err)
	}
	if err := c.Get(ctx, key, pg); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pg.Status.Message, "Connections are paused") || !strings.Contains(pg.Status.Message, "160%") {
		t.Errorf("fenced message = %q", pg.Status.Message)
	}

	// Still over, at 120 %: fenced stays (the release is below 95 %).
	applyCap(&pg.Status, pg.Generation, int64(float64(cap.Value())*1.2), &cap, time.Now())
	if err := c.Status().Update(ctx, pg); err != nil {
		t.Fatal(err)
	}
	settle(t, r, key)
	role = getU(t, c, "DatabaseRole", SharedPostgresNamespace, "u-"+hash)
	if limit, _, _ = unstructured.NestedInt64(role.Object, "spec", "connectionLimit"); limit != 0 {
		t.Errorf("at 120%% the fence stays: limit %d", limit)
	}

	// Freed space to 40 %: the limit returns, the job goes.
	if err := c.Get(ctx, key, pg); err != nil {
		t.Fatal(err)
	}
	applyCap(&pg.Status, pg.Generation, int64(float64(cap.Value())*0.4), &cap, time.Now())
	if err := c.Status().Update(ctx, pg); err != nil {
		t.Fatal(err)
	}
	settle(t, r, key)
	role = getU(t, c, "DatabaseRole", SharedPostgresNamespace, "u-"+hash)
	limit, _, _ = unstructured.NestedInt64(role.Object, "spec", "connectionLimit")
	if limit != 20 {
		t.Errorf("released: limit %d", limit)
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: "fence-" + hash}, fence); err == nil {
		t.Error("the fence job goes with the release")
	}
	if err := c.Get(ctx, key, pg); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(pg.Status.Message, "paused") {
		t.Errorf("released message = %q", pg.Status.Message)
	}
}

// Seven days over the cap, the owner's sessions default to read-only; back
// under, the default is reset. Both run as the superuser and are recorded.
func TestSharedPostgresNudgeAfterSevenDaysOver(t *testing.T) {
	ctx := context.Background()
	pg := sharedPostgres("db", "p-shop", "uid-shop-db")
	r, c := sharedReconciler(t, 200, pg)
	key := client.ObjectKeyFromObject(pg)
	settle(t, r, key)
	server := getU(t, c, "Cluster", SharedPostgresNamespace, "pg-shared-1")
	_ = unstructured.SetNestedField(server.Object, int64(2), "status", "readyInstances")
	if err := c.Update(ctx, server); err != nil {
		t.Fatal(err)
	}
	settle(t, r, key)
	hash := tenantHash(pg.UID)
	markApplied(t, c, getU(t, c, "DatabaseRole", SharedPostgresNamespace, "u-"+hash))
	markApplied(t, c, getU(t, c, "Database", SharedPostgresNamespace, "db-"+hash))
	settle(t, r, key)
	succeed := func(name string) {
		job := &batchv1.Job{}
		if err := c.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: name}, job); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		job.Status.Succeeded = 1
		if err := c.Status().Update(ctx, job); err != nil {
			if err := c.Update(ctx, job); err != nil {
				t.Fatal(err)
			}
		}
	}
	succeed("grant-" + hash)
	settle(t, r, key)

	// Over its cap since eight days, at 120 %.
	if err := c.Get(ctx, key, pg); err != nil {
		t.Fatal(err)
	}
	cap := resource.MustParse("1Gi")
	applyCap(&pg.Status, pg.Generation, int64(float64(cap.Value())*1.2), &cap, time.Now().Add(-8*24*time.Hour))
	if err := c.Status().Update(ctx, pg); err != nil {
		t.Fatal(err)
	}
	settle(t, r, key)
	nudge := &batchv1.Job{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: "nudge-" + hash}, nudge); err != nil || !strings.Contains(strings.Join(nudge.Spec.Template.Spec.Containers[0].Command, " "), "SET default_transaction_read_only = on") {
		t.Fatalf("the nudge job: %v", err)
	}
	succeed("nudge-" + hash)
	settle(t, r, key)
	if err := c.Get(ctx, key, pg); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pg.Status.Message, "Writes are paused") || getU(t, c, "Database", SharedPostgresNamespace, "db-"+hash).GetAnnotations()[annotationSharedNudge] != "on" {
		t.Errorf("nudged: %q", pg.Status.Message)
	}

	// Back under the cap: reset, recorded, the jobs gone.
	applyCap(&pg.Status, pg.Generation, int64(float64(cap.Value())*0.5), &cap, time.Now())
	if err := c.Status().Update(ctx, pg); err != nil {
		t.Fatal(err)
	}
	settle(t, r, key)
	if err := c.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: "unnudge-" + hash}, &batchv1.Job{}); err != nil {
		t.Fatalf("the reset job: %v", err)
	}
	succeed("unnudge-" + hash)
	settle(t, r, key)
	if err := c.Get(ctx, key, pg); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(pg.Status.Message, "paused") || getU(t, c, "Database", SharedPostgresNamespace, "db-"+hash).GetAnnotations()[annotationSharedNudge] != "off" {
		t.Errorf("released: %q", pg.Status.Message)
	}
	for _, name := range []string{"nudge-" + hash, "unnudge-" + hash} {
		if err := c.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: name}, &batchv1.Job{}); err == nil {
			t.Errorf("%s must be gone", name)
		}
	}
}
