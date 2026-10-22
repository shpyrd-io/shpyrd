package install

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/ptr"

	"github.com/shpyrd-io/shpyrd/pkg/kube"
)

// The control plane runs on CloudNativePG when the postgres extension is
// on and no managed database is named; otherwise nothing changes.
func TestControlPlaneOnCNPGNeedsTheExtensionAndNoManagedDatabase(t *testing.T) {
	for _, tc := range []struct {
		vars map[string]string
		want bool
	}{
		{map[string]string{VarExtensions: "postgres,redis"}, true},
		{map[string]string{VarExtensions: " postgres "}, true},
		{map[string]string{VarExtensions: "redis"}, false},
		{map[string]string{}, false},
		{map[string]string{VarExtensions: "postgres", VarDatabaseURL: "postgres://rds/shpyrd"}, false},
	} {
		if got := controlPlaneOnCNPG(tc.vars); got != tc.want {
			t.Errorf("controlPlaneOnCNPG(%v) = %v, want %v", tc.vars, got, tc.want)
		}
	}
}

// The cluster: two instances on the platform pool, spread over nodes, a
// preferred synchronous replica, Barman to the gateway with a nightly
// schedule; one instance alone and no backups without the gateway.
func TestControlPlaneObjectsRenderTheCluster(t *testing.T) {
	ns := "shpyrd-system"
	vars := map[string]string{VarExtensions: "postgres", VarGatewayBucket: "shared", VarPlatformPool: "platform", VarDataPool: "data", VarControlPlaneDBSize: "5Gi", VarStorageClass: "oci-bv", VarDatabaseStorageClass: "shpyrd-local"}
	objs := controlPlaneObjects(vars, ns)
	kinds := map[string]*unstructured.Unstructured{}
	for _, o := range objs {
		kinds[o.GetKind()] = o
	}
	for _, k := range []string{"Cluster", "NetworkPolicy", "PodMonitor", "ObjectStore", "ScheduledBackup"} {
		if kinds[k] == nil {
			t.Fatalf("no %s among %d objects", k, len(objs))
		}
	}
	cl := kinds["Cluster"]
	if cl.GetName() != ControlPlaneCluster || cl.GetNamespace() != ns {
		t.Errorf("cluster %s/%s", cl.GetNamespace(), cl.GetName())
	}
	if n, _, _ := unstructured.NestedInt64(cl.Object, "spec", "instances"); n != 2 {
		t.Errorf("instances = %d, want 2 by default", n)
	}
	if size, _, _ := unstructured.NestedString(cl.Object, "spec", "storage", "size"); size != "5Gi" {
		t.Errorf("storage size = %q", size)
	}
	if class, _, _ := unstructured.NestedString(cl.Object, "spec", "storage", "storageClass"); class != "oci-bv" {
		t.Errorf("storage class = %q, want the platform's class, not the databases' (node-local on kind)", class)
	}
	if pool, _, _ := unstructured.NestedString(cl.Object, "spec", "affinity", "nodeSelector", PoolLabel); pool != "platform" {
		t.Errorf("node selector pool = %q, want the platform pool, where the StatefulSet ran", pool)
	}
	if kind, _, _ := unstructured.NestedString(cl.Object, "spec", "affinity", "podAntiAffinityType"); kind != "required" {
		t.Errorf("anti-affinity = %q, want required with two instances", kind)
	}
	if d, _, _ := unstructured.NestedString(cl.Object, "spec", "postgresql", "synchronous", "dataDurability"); d != "preferred" {
		t.Errorf("synchronous dataDurability = %q, want preferred", d)
	}
	if owner, _, _ := unstructured.NestedString(cl.Object, "spec", "bootstrap", "initdb", "secret", "name"); owner != ControlPlaneOwnerSecret {
		t.Errorf("initdb owner secret = %q", owner)
	}
	plugins, _, _ := unstructured.NestedSlice(cl.Object, "spec", "plugins")
	if len(plugins) != 1 || plugins[0].(map[string]interface{})["name"] != barmanPluginName {
		t.Errorf("plugins = %v, want the Barman plugin", plugins)
	}
	if pdb, _, _ := unstructured.NestedBool(cl.Object, "spec", "enablePDB"); !pdb {
		t.Error("two instances want a PodDisruptionBudget")
	}
	if smart, _, _ := unstructured.NestedInt64(cl.Object, "spec", "smartShutdownTimeout"); smart != 10 {
		t.Errorf("smartShutdownTimeout = %d, want 10 (the server's idle pool would hold the default 180 s before a failover)", smart)
	}
	store := kinds["ObjectStore"]
	if dest, _, _ := unstructured.NestedString(store.Object, "spec", "configuration", "destinationPath"); dest != "s3://"+ControlPlaneBackupsBucket+"/" {
		t.Errorf("destination = %q", dest)
	}
	if ep, _, _ := unstructured.NestedString(store.Object, "spec", "configuration", "endpointURL"); ep != gatewayEndpoint(ns) {
		t.Errorf("endpoint = %q", ep)
	}
	if sec, _, _ := unstructured.NestedString(store.Object, "spec", "configuration", "s3Credentials", "accessKeyId", "name"); sec != ControlPlaneBackupsSecret {
		t.Errorf("credential secret = %q", sec)
	}
	sched := kinds["ScheduledBackup"]
	if target, _, _ := unstructured.NestedString(sched.Object, "spec", "cluster", "name"); target != ControlPlaneCluster {
		t.Errorf("schedule targets %q", target)
	}

	// One instance, no gateway: a single primary, no synchronous block, no
	// anti-affinity, nothing to back up to.
	local := controlPlaneObjects(map[string]string{VarExtensions: "postgres", VarControlPlaneDBInstances: "1"}, ns)
	if len(local) != 3 {
		t.Fatalf("without the gateway: %d objects, want the cluster, its policy and its monitor", len(local))
	}
	if _, found, _ := unstructured.NestedMap(kinds["Cluster"].Object, "spec", "monitoring"); found {
		t.Error("spec.monitoring is deprecated by CloudNativePG 1.30; the PodMonitor is rendered by hand")
	}
	one := local[0]
	if n, _, _ := unstructured.NestedInt64(one.Object, "spec", "instances"); n != 1 {
		t.Errorf("instances = %d, want 1", n)
	}
	if _, found, _ := unstructured.NestedMap(one.Object, "spec", "postgresql", "synchronous"); found {
		t.Error("one instance has no synchronous replica to wait for")
	}
	if _, found, _ := unstructured.NestedSlice(one.Object, "spec", "plugins"); found {
		t.Error("no gateway, no Barman plugin")
	}
	if _, found, _ := unstructured.NestedMap(one.Object, "spec", "affinity"); found {
		t.Error("no pool and one instance: no affinity")
	}
}

// The connection string names the cluster's read-write Service and asks
// for TLS.
func TestControlPlaneURL(t *testing.T) {
	got := controlPlaneURL("s3cret", "shpyrd-system")
	want := "postgres://shpyrd:s3cret@control-plane-pg-rw.shpyrd-system.svc:5432/shpyrd?sslmode=require"
	if got != want {
		t.Errorf("url = %q, want %q", got, want)
	}
}

// The component's waits, at the apply and in the status: the cluster's
// primary, and the StatefulSet until the data moved; the declared waits
// for every other component and for the StatefulSet path.
func TestControlPlaneComponentWaits(t *testing.T) {
	ctx := context.Background()
	ns := "shpyrd-system"
	comp := &Component{Name: "control-plane-db", Namespace: ns, Wait: []WaitSpec{{StatefulSet: ns + "/control-plane-db"}}}
	cs := fake.NewSimpleClientset(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: ControlPlaneDBSecretName, Namespace: ns}, Data: map[string][]byte{"moved": []byte(ControlPlaneCluster)}})
	e := fakeEngine(ctx, cs, map[string]string{VarExtensions: "postgres"})
	if primary, waits := e.componentWaits(ctx, comp); !primary || len(waits) != 0 {
		t.Errorf("moved: primary=%v waits=%+v, want the primary alone", primary, waits)
	}
	if err := cs.CoreV1().Secrets(ns).Delete(ctx, ControlPlaneDBSecretName, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	if primary, waits := e.componentWaits(ctx, comp); !primary || len(waits) != 1 || waits[0].StatefulSet != ns+"/control-plane-db" {
		t.Errorf("not moved: primary=%v waits=%+v, want the primary and the StatefulSet", primary, waits)
	}
	plain := fakeEngine(ctx, fake.NewSimpleClientset(), map[string]string{VarExtensions: "redis"})
	if primary, waits := plain.componentWaits(ctx, comp); primary || len(waits) != 1 {
		t.Errorf("without the extension: primary=%v waits=%+v, want the declared wait alone", primary, waits)
	}
}

// A fresh platform: the owner Secret for initdb and the connection string
// on the cluster from the start, marked moved so nothing waits for a
// StatefulSet.
func TestControlPlaneHookOnAFreshPlatformPointsAtTheCluster(t *testing.T) {
	ctx := context.Background()
	ns := "shpyrd-system"
	cs := fake.NewSimpleClientset()
	e := fakeEngine(ctx, cs, map[string]string{VarExtensions: "postgres"})
	if err := controlPlaneDBHook(ctx, e, &Component{Name: "control-plane-db", Namespace: ns}); err != nil {
		t.Fatal(err)
	}
	owner, err := cs.CoreV1().Secrets(ns).Get(ctx, ControlPlaneOwnerSecret, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if owner.Type != corev1.SecretTypeBasicAuth || string(owner.Data["username"]) != "shpyrd" || len(owner.Data["password"]) == 0 {
		t.Errorf("owner secret type %s data %v", owner.Type, owner.Data)
	}
	db, err := cs.CoreV1().Secrets(ns).Get(ctx, ControlPlaneDBSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !controlPlaneMoved(db.Data) {
		t.Errorf("a fresh platform is on the cluster from the start: %v", db.Data)
	}
	if string(db.Data["password"]) != string(owner.Data["password"]) {
		t.Error("the connection string's password is not the owner's")
	}
	if want := controlPlaneURL(string(owner.Data["password"]), ns); string(db.Data["url"]) != want {
		t.Errorf("url = %q, want %q", db.Data["url"], want)
	}
	// Running again keeps the password.
	if err := controlPlaneDBHook(ctx, e, &Component{Name: ServerComponent, Namespace: ns}); err != nil {
		t.Fatal(err)
	}
	again, _ := cs.CoreV1().Secrets(ns).Get(ctx, ControlPlaneDBSecretName, metav1.GetOptions{})
	if string(again.Data["password"]) != string(db.Data["password"]) {
		t.Error("the second run changed the password")
	}
	if got := controlPlaneWaits(ns, true); len(got) != 0 {
		t.Errorf("waits once moved = %+v, want none beyond the primary's", got)
	}
}

// A platform with its data on the StatefulSet keeps its connection string
// until the server's turn; before that, and while the cluster is not
// ready, nothing moves.
func TestControlPlaneHookKeepsTheOldInstanceUntilTheMove(t *testing.T) {
	ctx := context.Background()
	ns := "shpyrd-system"
	oldURL := "postgres://shpyrd:pw@control-plane-db." + ns + ".svc:5432/shpyrd?sslmode=disable"
	cs := fake.NewSimpleClientset(
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: ControlPlaneDBSecretName, Namespace: ns}, Data: map[string][]byte{"password": []byte("pw"), "url": []byte(oldURL)}},
		&appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: controlPlaneStatefulSet, Namespace: ns}, Spec: appsv1.StatefulSetSpec{Replicas: ptr.To[int32](1)}, Status: appsv1.StatefulSetStatus{ReadyReplicas: 1}},
	)
	e := fakeEngine(ctx, cs, map[string]string{VarExtensions: "postgres"})
	for _, c := range []*Component{{Name: "control-plane-db", Namespace: ns}, {Name: ServerComponent, Namespace: ns}} {
		if err := controlPlaneDBHook(ctx, e, c); err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		db, _ := cs.CoreV1().Secrets(ns).Get(ctx, ControlPlaneDBSecretName, metav1.GetOptions{})
		if string(db.Data["url"]) != oldURL || controlPlaneMoved(db.Data) {
			t.Errorf("%s (cluster not ready): url %q moved %v, want the old url kept", c.Name, db.Data["url"], controlPlaneMoved(db.Data))
		}
	}
	owner, err := cs.CoreV1().Secrets(ns).Get(ctx, ControlPlaneOwnerSecret, metav1.GetOptions{})
	if err != nil || string(owner.Data["password"]) != "pw" {
		t.Errorf("owner secret reuses the StatefulSet's password: %v %v", err, owner)
	}
	if got := controlPlaneWaits(ns, false); len(got) != 1 || got[0].StatefulSet != ns+"/"+controlPlaneStatefulSet {
		t.Errorf("waits before the move = %+v, want the StatefulSet beyond the primary's", got)
	}
}

// At the server's turn, with the cluster ready, the data moves: the server
// stops, the copy Job runs with both connection strings, the Secret turns
// to the cluster and the StatefulSet stops.
func TestControlPlaneHookMovesTheDataAtTheServersTurn(t *testing.T) {
	ctx := context.Background()
	ns := "shpyrd-system"
	oldURL := "postgres://shpyrd:pw@control-plane-db." + ns + ".svc:5432/shpyrd?sslmode=disable"
	cs := fake.NewSimpleClientset(
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: ControlPlaneDBSecretName, Namespace: ns}, Data: map[string][]byte{"password": []byte("pw"), "url": []byte(oldURL)}},
		&appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: controlPlaneStatefulSet, Namespace: ns}, Spec: appsv1.StatefulSetSpec{Replicas: ptr.To[int32](1)}, Status: appsv1.StatefulSetStatus{ReadyReplicas: 1}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "shpyrd-server", Namespace: ns}, Spec: appsv1.DeploymentSpec{Replicas: ptr.To[int32](2)}},
	)
	e := fakeEngine(ctx, cs, map[string]string{VarExtensions: "postgres"})
	ready := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "postgresql.cnpg.io/v1", "kind": "Cluster",
		"metadata": map[string]interface{}{"name": ControlPlaneCluster, "namespace": ns},
		"status":   map[string]interface{}{"readyInstances": int64(2)},
	}}
	// Beside the Secret reactor fakeEngine installed.
	if err := e.kube.Dynamic.(*dynamicfake.FakeDynamicClient).Tracker().Add(ready); err != nil {
		t.Fatal(err)
	}
	var sawOld, sawNew string
	waitJobFn = func(ctx context.Context, k *kube.Client, ns, name string) error {
		j, err := k.Kube.BatchV1().Jobs(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		sec, err := k.Kube.CoreV1().Secrets(ns).Get(ctx, controlPlaneMoveJob, metav1.GetOptions{})
		if err != nil {
			return err
		}
		sawOld, sawNew = string(sec.Data["old"]), string(sec.Data["new"])
		if j.Spec.Template.Labels["shpyrd.io/control-plane-client"] != "true" {
			t.Error("the move job is not a control-plane client (the network policies would refuse it)")
		}
		if cmd := strings.Join(j.Spec.Template.Spec.Containers[0].Command, " "); !strings.Contains(cmd, "pg_dump") || !strings.Contains(cmd, "pg_restore") {
			t.Errorf("job command %q", cmd)
		}
		return nil
	}
	defer func() { waitJobFn = waitJob }()
	if err := controlPlaneDBHook(ctx, e, &Component{Name: ServerComponent, Namespace: ns}); err != nil {
		t.Fatal(err)
	}
	db, _ := cs.CoreV1().Secrets(ns).Get(ctx, ControlPlaneDBSecretName, metav1.GetOptions{})
	want := controlPlaneURL("pw", ns)
	if string(db.Data["url"]) != want || !controlPlaneMoved(db.Data) {
		t.Errorf("after the move: url %q moved %v", db.Data["url"], controlPlaneMoved(db.Data))
	}
	if sawOld != oldURL || sawNew != want {
		t.Errorf("the job copied from %q to %q", sawOld, sawNew)
	}
	sts, _ := cs.AppsV1().StatefulSets(ns).Get(ctx, controlPlaneStatefulSet, metav1.GetOptions{})
	if sts.Spec.Replicas == nil || *sts.Spec.Replicas != 0 {
		t.Errorf("the old instance keeps running: replicas %v", sts.Spec.Replicas)
	}
	dep, _ := cs.AppsV1().Deployments(ns).Get(ctx, "shpyrd-server", metav1.GetOptions{})
	if dep.Spec.Replicas == nil || *dep.Spec.Replicas != 0 {
		t.Errorf("the server was not stopped for the copy: replicas %v (the component applies it again right after)", dep.Spec.Replicas)
	}
	if _, err := cs.CoreV1().Secrets(ns).Get(ctx, controlPlaneMoveJob, metav1.GetOptions{}); err == nil {
		t.Error("the transient Secret with both connection strings outlived the move")
	}
	// A second run at the server's turn finds it moved and does nothing.
	waitJobFn = func(context.Context, *kube.Client, string, string) error { t.Error("moved twice"); return nil }
	if err := controlPlaneDBHook(ctx, e, &Component{Name: ServerComponent, Namespace: ns}); err != nil {
		t.Fatal(err)
	}
}
