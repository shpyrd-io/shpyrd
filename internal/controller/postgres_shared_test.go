package controller

import (
	"context"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
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
	if roleName != "u_"+hash || limit != 20 || secretRef != "u-"+hash || dbName != "db_"+hash || owner != roleName || onServer != "pg-shared-1" || db.GetLabels()[sharedOfServerLabel] != "pg-shared-1" {
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
	if string(app.Data["host"]) != "db" || string(app.Data["dbname"]) != dbName || string(app.Data["username"]) != roleName || string(app.Data["password"]) != string(roleSecret.Data["password"]) || !strings.HasSuffix(uri, "?sslmode=require") || !strings.Contains(uri, "@db:5432/"+dbName) {
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
	if err != nil || vars["DATABASE_HOST"] != "db" || vars["DATABASE_NAME"] != dbName || !strings.HasSuffix(vars["DATABASE_URL"], "?sslmode=require") {
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

// Deleting a shared database removes its role and secrets now and keeps
// the data seven days behind a drop date.
func TestSharedPostgresDeletionKeepsTheDataSevenDays(t *testing.T) {
	ctx := context.Background()
	pg := sharedPostgres("db", "p-shop", "uid-shop-db")
	r, c := sharedReconciler(t, 200, pg)
	key := client.ObjectKeyFromObject(pg)
	settle(t, r, key)
	s := getU(t, c, "Cluster", SharedPostgresNamespace, "pg-shared-1")
	_ = unstructured.SetNestedField(s.Object, int64(2), "status", "readyInstances")
	if err := c.Update(ctx, s); err != nil {
		t.Fatal(err)
	}
	settle(t, r, key)
	hash := tenantHash(pg.UID)
	if err := c.Get(ctx, key, pg); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(ctx, pg); err != nil {
		t.Fatal(err)
	}
	settle(t, r, key)
	role := &unstructured.Unstructured{}
	role.SetGroupVersionKind(CNPGDatabaseRoleGVK)
	if err := c.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: "u-" + hash}, role); err == nil {
		t.Error("the role must be gone")
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: "u-" + hash}, &corev1.Secret{}); err == nil {
		t.Error("the role's secret must be gone")
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: "grant-" + hash}, &batchv1.Job{}); err == nil {
		t.Error("the grants job must be gone")
	}
	db := getU(t, c, "Database", SharedPostgresNamespace, "db-"+hash)
	policy, _, _ := unstructured.NestedString(db.Object, "spec", "databaseReclaimPolicy")
	if db.GetAnnotations()[AnnotationDropAfter] == "" || policy != "retain" {
		t.Errorf("the data stays behind a drop date: %v %s", db.GetAnnotations(), policy)
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
		_ = unstructured.SetNestedMap(u.Object, map[string]interface{}{"name": "db_deadbeef", "ensure": "present", "databaseReclaimPolicy": "retain"}, "spec")
		return u
	}
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	key := types.NamespacedName{Namespace: SharedPostgresNamespace, Name: "db-deadbeef"}

	_, c := newTestReconciler(t)
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
	if res, err := sweep.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil || res.RequeueAfter != 0 {
		t.Errorf("done is done: %+v %v", res, err)
	}
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
