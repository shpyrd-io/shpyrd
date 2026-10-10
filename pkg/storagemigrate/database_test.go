package storagemigrate

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/internal/controller"
	"github.com/shpyrd-io/shpyrd/pkg/kube"
)

func node(name string) *corev1.Node {
	return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{corev1.LabelHostname: name, controller.PoolLabel: "data"}}, Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}, Allocatable: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2"), corev1.ResourceMemory: resource.MustParse("8Gi")}}}
}

func claim(ns, name, class, pv string) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}, Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: ptr.To(class), VolumeName: pv, Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")}}}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound}}
}

func instancePod(ns, name, nodeName string) *corev1.Pod {
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}, Spec: corev1.PodSpec{NodeName: nodeName, Containers: []corev1.Container{{Name: "postgres", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("62m"), corev1.ResourceMemory: resource.MustParse("256Mi")}}}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}
}

func cnpgCluster(ns, name string) *unstructured.Unstructured {
	c := &unstructured.Unstructured{}
	c.SetGroupVersionKind(controller.CNPGClusterGVK)
	c.SetName(name)
	c.SetNamespace(ns)
	c.Object["spec"] = map[string]interface{}{"instances": int64(1), "storage": map[string]interface{}{"size": "1Gi", "storageClass": controller.LocalStorageClass}, "affinity": map[string]interface{}{"nodeSelector": map[string]interface{}{controller.PoolLabel: "data"}}}
	c.Object["status"] = map[string]interface{}{"currentPrimary": name + "-1", "targetPrimary": name + "-1", "instanceNames": []interface{}{name + "-1"}, "readyInstances": int64(1), "phase": "Cluster in healthy state"}
	return c
}

type fixture struct {
	c        client.Client
	ns, name string
	sql      *fakeSQL
	// The demoted primary's restart after a switchover: ticks left, and
	// which instance is down for them.
	restart    int
	restarting string
	// revertAsks: how many switchover requests the fake operator cancels
	// ("Wrong target primary"), putting the target back on the primary.
	revertAsks int
}

func newFixture(t *testing.T, pg *shpyrdv1.Postgres, extra ...client.Object) fixture {
	t.Helper()
	scheme, err := kube.Scheme()
	if err != nil {
		t.Fatal(err)
	}
	ns, name := pg.Namespace, pg.Name
	objs := []client.Object{pg, claim(ns, name+"-1", controller.LocalStorageClass, "pv-old"),
		&corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "pv-old"}, Spec: corev1.PersistentVolumeSpec{StorageClassName: controller.LocalStorageClass, PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimDelete, Capacity: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")}}},
		instancePod(ns, name+"-1", "n1"), node("n1"), node("n2"), &storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "oci-bv"}, Provisioner: "x"},
		&shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: ns, UID: "shop-uid"}}}
	objs = append(objs, extra...)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).WithRuntimeObjects(cnpgCluster(ns, name)).WithStatusSubresource(cnpgCluster(ns, name)).Build()
	return fixture{c: c, ns: ns, name: name, sql: &fakeSQL{queries: map[string]int{}, rows: map[string]string{}, down: map[string]bool{}, downFor: map[string]int{}, refused: map[string]int{}}}
}

// fakeOperator plays the platform and CloudNativePG: the Cluster takes the
// class the mark asks for; a second instance appears on it; a switchover
// happens when asked and the demoted primary restarts for a few ticks (not
// ready, the cluster not healthy, its SQL refused) before it rejoins; the
// extra instance goes when the database is back to one. knowsMark false
// plays a platform without the storage migration.
func (f *fixture) fakeOperator(ctx context.Context, knowsMark bool) {
	key := types.NamespacedName{Namespace: f.ns, Name: f.name}
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Millisecond):
		}
		pg := &shpyrdv1.Postgres{}
		if err := f.c.Get(ctx, key, pg); err != nil {
			continue
		}
		cluster := &unstructured.Unstructured{}
		cluster.SetGroupVersionKind(controller.CNPGClusterGVK)
		if err := f.c.Get(ctx, key, cluster); err != nil {
			continue
		}
		target := pg.Annotations[controller.AnnotationStorageMigration]
		class, _, _ := unstructured.NestedString(cluster.Object, "spec", "storage", "storageClass")
		if knowsMark && target != "" && class != target {
			_ = unstructured.SetNestedField(cluster.Object, target, "spec", "storage", "storageClass")
			_ = f.c.Update(ctx, cluster)
			continue
		}
		if target == "" {
			// The controller renders the class the primary's volume is on.
			claim := &corev1.PersistentVolumeClaim{}
			if err := f.c.Get(ctx, types.NamespacedName{Namespace: f.ns, Name: primaryOf(cluster)}, claim); err == nil && ptr.Deref(claim.Spec.StorageClassName, "") != class {
				_ = unstructured.SetNestedField(cluster.Object, ptr.Deref(claim.Spec.StorageClassName, ""), "spec", "storage", "storageClass")
				_ = f.c.Update(ctx, cluster)
				continue
			}
		}
		names, _, _ := unstructured.NestedStringSlice(cluster.Object, "status", "instanceNames")
		tp, _, _ := unstructured.NestedString(cluster.Object, "status", "targetPrimary")
		switch {
		case tp != "" && primaryOf(cluster) != tp && (f.restart > 0 || f.revertAsks > 0):
			// The operator cancels a switchover whose target is not an
			// active instance: the target goes back on the primary.
			if f.restart == 0 {
				f.revertAsks--
			}
			_ = unstructured.SetNestedField(cluster.Object, primaryOf(cluster), "status", "targetPrimary")
			if f.restart == 0 {
				_ = unstructured.SetNestedField(cluster.Object, phaseHealthy, "status", "phase")
			}
			_ = f.c.Status().Update(ctx, cluster)
		case f.restart > 0:
			if f.restart--; f.restart == 0 {
				_ = unstructured.SetNestedField(cluster.Object, int64(len(names)), "status", "readyInstances")
				_ = unstructured.SetNestedField(cluster.Object, phaseHealthy, "status", "phase")
				_ = f.c.Status().Update(ctx, cluster)
				f.sql.set(f.restarting, false)
			}
		case ptr.Deref(pg.Spec.Instances, 1) == 2 && len(names) == 1:
			_ = f.c.Create(ctx, claim(f.ns, f.name+"-2", class, "pv-new"))
			_ = f.c.Create(ctx, instancePod(f.ns, f.name+"-2", "n2"))
			_ = unstructured.SetNestedField(cluster.Object, int64(2), "spec", "instances")
			_ = f.c.Update(ctx, cluster)
			_ = unstructured.SetNestedStringSlice(cluster.Object, []string{names[0], f.name + "-2"}, "status", "instanceNames")
			_ = unstructured.SetNestedField(cluster.Object, int64(2), "status", "readyInstances")
			_ = f.c.Status().Update(ctx, cluster)
		case tp != "" && primaryOf(cluster) != tp:
			f.restarting = primaryOf(cluster)
			f.restart = 5
			f.sql.set(f.restarting, true)
			_ = unstructured.SetNestedField(cluster.Object, tp, "status", "currentPrimary")
			_ = unstructured.SetNestedField(cluster.Object, int64(1), "status", "readyInstances")
			_ = unstructured.SetNestedField(cluster.Object, "Switchover in progress", "status", "phase")
			_ = f.c.Status().Update(ctx, cluster)
		case ptr.Deref(pg.Spec.Instances, 1) == 1 && len(names) == 2:
			primary := primaryOf(cluster)
			for _, n := range names {
				if n != primary {
					_ = f.c.Delete(ctx, &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: n, Namespace: f.ns}})
				}
			}
			_ = unstructured.SetNestedField(cluster.Object, int64(1), "spec", "instances")
			_ = f.c.Update(ctx, cluster)
			_ = unstructured.SetNestedStringSlice(cluster.Object, []string{primary}, "status", "instanceNames")
			_ = unstructured.SetNestedField(cluster.Object, int64(1), "status", "readyInstances")
			_ = f.c.Status().Update(ctx, cluster)
		}
	}
}

type fakeSQL struct {
	mu      sync.Mutex
	queries map[string]int
	rows    map[string]string // per instance: the count answered
	down    map[string]bool   // instances that refuse a connection (restarting)
	downFor map[string]int    // answers an instance still refuses once it is ready
	refused map[string]int
}

func (q *fakeSQL) set(pod string, down bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.down[pod] = down
}

func (q *fakeSQL) run(_ context.Context, _, pod, statement string) (string, error) {
	q.mu.Lock()
	if q.down[pod] || q.downFor[pod] > 0 {
		if !q.down[pod] {
			q.downFor[pod]--
		}
		q.refused[pod]++
		q.mu.Unlock()
		return "", errors.New("command terminated with exit code 2: psql: error: connection to server on socket failed")
	}
	q.queries[pod]++
	q.mu.Unlock()
	switch {
	case strings.Contains(statement, "pg_database_size"):
		return "1000000\n", nil
	case strings.Contains(statement, "relpersistence = 'u'"):
		return "\n", nil
	case strings.Contains(statement, "pg_stat_user_tables"):
		return "public.orders\n", nil
	default:
		if v, ok := q.rows[pod]; ok {
			return v + "\n", nil
		}
		return "42\n", nil
	}
}

func opts(ns, name string, sql SQL) Options {
	return Options{Namespace: ns, Name: name, TargetClass: "oci-bv", SnapshotClass: "oci-bv-backup", Wait: 15 * time.Second, Poll: 10 * time.Millisecond, SQL: sql, Out: io.Discard}
}

func TestDatabaseMigratesBySwitchoverAndKeepsTheOldVolume(t *testing.T) {
	f := newFixture(t, &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "p-shop"}})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	go f.fakeOperator(ctx, true)
	sql := f.sql
	res, err := Database(ctx, f.c, opts(f.ns, f.name, sql.run))
	if err != nil {
		t.Fatalf("migrate: %v\n%s", err, strings.Join(res.Steps, "\n"))
	}
	if res.From != controller.LocalStorageClass || res.To != "oci-bv" || !res.Verified || res.OldPV != "pv-old" || res.NewClaim != "db-2" || res.Rows["public.orders"] != [2]int{42, 42} {
		t.Errorf("result = %+v", res)
	}
	if sql.queries["db-1"] == 0 || sql.queries["db-2"] == 0 {
		t.Errorf("the checks must run on both instances after the switchover: %v", sql.queries)
	}
	if sql.refused["db-1"] != 0 {
		t.Errorf("the checks ran while the demoted primary was still restarting (%d refused queries): the switchover must wait for every instance", sql.refused["db-1"])
	}
	pv := &corev1.PersistentVolume{}
	if err := f.c.Get(ctx, types.NamespacedName{Name: "pv-old"}, pv); err != nil {
		t.Fatal(err)
	}
	var record RetainedDisk
	if pv.Spec.PersistentVolumeReclaimPolicy != corev1.PersistentVolumeReclaimRetain || pv.Labels[RetainedMigrationAnnotation] != "db" || unmarshal(pv.Annotations[RetainedMigrationAnnotation], &record) != nil || record.Claim != "db-1" || record.ProjectUID != "shop-uid" || record.Namespace != f.ns {
		t.Errorf("old volume = %s labels %v annotations %v", pv.Spec.PersistentVolumeReclaimPolicy, pv.Labels, pv.Annotations)
	}
	pg := &shpyrdv1.Postgres{}
	if err := f.c.Get(ctx, types.NamespacedName{Namespace: f.ns, Name: f.name}, pg); err != nil {
		t.Fatal(err)
	}
	if pg.Annotations[controller.AnnotationStorageMigration] != "" || ptr.Deref(pg.Spec.Instances, 0) != 1 {
		t.Errorf("database after = annotations %v instances %v", pg.Annotations, pg.Spec.Instances)
	}
	snaps := &unstructured.UnstructuredList{}
	snaps.SetGroupVersionKind(controller.VolumeSnapshotGVK.GroupVersion().WithKind("VolumeSnapshotList"))
	if err := f.c.List(ctx, snaps, client.InNamespace(f.ns)); err != nil {
		t.Fatal(err)
	}
	if len(snaps.Items) != 1 || res.Snapshot == "" {
		t.Errorf("snapshots after = %d (%q)", len(snaps.Items), res.Snapshot)
	}
	// Run again: nothing to do, nothing touched, no second snapshot.
	again, err := Database(ctx, f.c, opts(f.ns, f.name, sql.run))
	if err != nil || !again.Verified || len(again.Steps) != 1 || !strings.Contains(again.Steps[0], "already") {
		t.Errorf("second run = %v %+v", err, again)
	}
	if err := f.c.List(ctx, snaps, client.InNamespace(f.ns)); err != nil || len(snaps.Items) != 1 {
		t.Errorf("a repeat run must not snapshot again: %d %v", len(snaps.Items), err)
	}
}

// A pinned database is refused before anything changes: the pin must be
// cleared by its own step.
func TestDatabaseMigrationRefusesAPinnedDatabase(t *testing.T) {
	f := newFixture(t, &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "p-shop", Annotations: map[string]string{shpyrdv1.AnnotationPlacement: "n1"}}})
	_, err := Database(context.Background(), f.c, opts(f.ns, f.name, nil))
	if err == nil || !strings.Contains(err.Error(), "cluster unpin") {
		t.Fatalf("expected the pin refusal, got %v", err)
	}
	pg := &shpyrdv1.Postgres{}
	_ = f.c.Get(context.Background(), types.NamespacedName{Namespace: f.ns, Name: f.name}, pg)
	if pg.Annotations[controller.AnnotationStorageMigration] != "" || pg.Spec.Instances != nil {
		t.Errorf("the refusal must leave the database untouched: %v %v", pg.Annotations, pg.Spec.Instances)
	}
}

// A platform that does not know the mark gets nothing but the mark, which
// is taken back.
func TestDatabaseMigrationRefusesAnOldPlatform(t *testing.T) {
	f := newFixture(t, &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "p-shop"}})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go f.fakeOperator(ctx, false)
	o := opts(f.ns, f.name, nil)
	o.Wait = 300 * time.Millisecond
	_, err := Database(ctx, f.c, o)
	if err == nil || !strings.Contains(err.Error(), "did not apply the migration class") {
		t.Fatalf("expected the platform refusal, got %v", err)
	}
	pg := &shpyrdv1.Postgres{}
	_ = f.c.Get(ctx, types.NamespacedName{Namespace: f.ns, Name: f.name}, pg)
	if pg.Annotations[controller.AnnotationStorageMigration] != "" || ptr.Deref(pg.Spec.Instances, 1) != 1 {
		t.Errorf("an old platform must be left as it was: %v %v", pg.Annotations, pg.Spec.Instances)
	}
}

// Interrupted after the scale-down with the mark still set: the resumed run
// finishes without asking for a second instance again.
func TestDatabaseMigrationResumesAfterTheScaleDown(t *testing.T) {
	pg := &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "p-shop", Annotations: map[string]string{controller.AnnotationStorageMigration: "oci-bv"}}, Spec: shpyrdv1.PostgresSpec{Instances: ptr.To[int32](1)}}
	f := newFixture(t, pg)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// The state after the switchover and the scale-down: one instance, on
	// the target class.
	cluster := &unstructured.Unstructured{}
	cluster.SetGroupVersionKind(controller.CNPGClusterGVK)
	if err := f.c.Get(ctx, types.NamespacedName{Namespace: f.ns, Name: f.name}, cluster); err != nil {
		t.Fatal(err)
	}
	_ = unstructured.SetNestedField(cluster.Object, "oci-bv", "spec", "storage", "storageClass")
	_ = f.c.Update(ctx, cluster)
	_ = unstructured.SetNestedField(cluster.Object, "db-2", "status", "currentPrimary")
	_ = unstructured.SetNestedStringSlice(cluster.Object, []string{"db-2"}, "status", "instanceNames")
	_ = f.c.Status().Update(ctx, cluster)
	_ = f.c.Create(ctx, claim(f.ns, "db-2", "oci-bv", "pv-new"))
	_ = f.c.Create(ctx, instancePod(f.ns, "db-2", "n2"))
	go f.fakeOperator(ctx, true)
	res, err := Database(ctx, f.c, opts(f.ns, f.name, nil))
	if err != nil {
		t.Fatalf("resume: %v\n%s", err, strings.Join(res.Steps, "\n"))
	}
	for _, step := range res.Steps {
		if strings.Contains(step, "requested") {
			t.Errorf("the resumed run asked for a second instance again: %s", step)
		}
	}
	if err := f.c.Get(ctx, types.NamespacedName{Namespace: f.ns, Name: f.name}, pg); err != nil {
		t.Fatal(err)
	}
	if pg.Annotations[controller.AnnotationStorageMigration] != "" || ptr.Deref(pg.Spec.Instances, 0) != 1 {
		t.Errorf("after the resume: %v %v", pg.Annotations, pg.Spec.Instances)
	}
	if res.Verified || len(res.Notes) == 0 {
		t.Errorf("a resume past the old instance cannot verify and must say so: %+v", res)
	}
}

// A run interrupted right after the switchover, resumed while the demoted
// primary is still restarting: the run waits for the cluster to settle, the
// checks wait for the instance to answer (PostgreSQL accepts connections a
// moment after the pod is ready), then the migration completes.
func TestDatabaseMigrationResumesOnTwoInstancesWhileTheOldOneRestarts(t *testing.T) {
	pg := &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "p-shop", Annotations: map[string]string{controller.AnnotationStorageMigration: "oci-bv"}}, Spec: shpyrdv1.PostgresSpec{Instances: ptr.To[int32](2)}}
	f := newFixture(t, pg)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cluster := &unstructured.Unstructured{}
	cluster.SetGroupVersionKind(controller.CNPGClusterGVK)
	if err := f.c.Get(ctx, types.NamespacedName{Namespace: f.ns, Name: f.name}, cluster); err != nil {
		t.Fatal(err)
	}
	_ = unstructured.SetNestedField(cluster.Object, "oci-bv", "spec", "storage", "storageClass")
	_ = unstructured.SetNestedField(cluster.Object, int64(2), "spec", "instances")
	_ = f.c.Update(ctx, cluster)
	_ = unstructured.SetNestedField(cluster.Object, "db-2", "status", "currentPrimary")
	_ = unstructured.SetNestedField(cluster.Object, "db-2", "status", "targetPrimary")
	_ = unstructured.SetNestedStringSlice(cluster.Object, []string{"db-1", "db-2"}, "status", "instanceNames")
	_ = unstructured.SetNestedField(cluster.Object, int64(1), "status", "readyInstances")
	_ = unstructured.SetNestedField(cluster.Object, "Switchover in progress", "status", "phase")
	_ = f.c.Status().Update(ctx, cluster)
	_ = f.c.Create(ctx, claim(f.ns, "db-2", "oci-bv", "pv-new"))
	_ = f.c.Create(ctx, instancePod(f.ns, "db-2", "n2"))
	f.restarting, f.restart = "db-1", 20
	f.sql.set("db-1", true)
	f.sql.downFor["db-1"] = 3
	go f.fakeOperator(ctx, true)
	res, err := Database(ctx, f.c, opts(f.ns, f.name, f.sql.run))
	if err != nil {
		t.Fatalf("resume: %v\n%s", err, strings.Join(res.Steps, "\n"))
	}
	if !res.Verified || res.OldPV != "pv-old" || res.Rows["public.orders"] != [2]int{42, 42} {
		t.Errorf("result = %+v", res)
	}
	if f.sql.refused["db-1"] != 3 {
		t.Errorf("the checks must wait for the old instance to answer: %d refused queries, want the 3 the instance refused once ready", f.sql.refused["db-1"])
	}
	if err := f.c.Get(ctx, types.NamespacedName{Namespace: f.ns, Name: f.name}, pg); err != nil {
		t.Fatal(err)
	}
	if pg.Annotations[controller.AnnotationStorageMigration] != "" || ptr.Deref(pg.Spec.Instances, 0) != 1 {
		t.Errorf("after the resume: %v %v", pg.Annotations, pg.Spec.Instances)
	}
}

// --back asked right after the switchover, while the demoted primary is
// still restarting: the tool waits for the cluster to settle before asking
// (CloudNativePG cancels a switchover to an instance that is not active),
// asks again when the operator puts the target back, and gives up after
// three cancellations.
func TestDatabaseMigrationBackWaitsForTheClusterAndAsksAgain(t *testing.T) {
	twoInstances := func(t *testing.T) (*fixture, context.Context, context.CancelFunc) {
		pg := &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "p-shop", Annotations: map[string]string{controller.AnnotationStorageMigration: "oci-bv"}}, Spec: shpyrdv1.PostgresSpec{Instances: ptr.To[int32](2)}}
		f := newFixture(t, pg)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		cluster := &unstructured.Unstructured{}
		cluster.SetGroupVersionKind(controller.CNPGClusterGVK)
		if err := f.c.Get(ctx, types.NamespacedName{Namespace: f.ns, Name: f.name}, cluster); err != nil {
			t.Fatal(err)
		}
		_ = unstructured.SetNestedField(cluster.Object, "oci-bv", "spec", "storage", "storageClass")
		_ = unstructured.SetNestedField(cluster.Object, int64(2), "spec", "instances")
		_ = f.c.Update(ctx, cluster)
		_ = unstructured.SetNestedField(cluster.Object, "db-2", "status", "currentPrimary")
		_ = unstructured.SetNestedField(cluster.Object, "db-2", "status", "targetPrimary")
		_ = unstructured.SetNestedStringSlice(cluster.Object, []string{"db-1", "db-2"}, "status", "instanceNames")
		_ = unstructured.SetNestedField(cluster.Object, int64(1), "status", "readyInstances")
		_ = unstructured.SetNestedField(cluster.Object, "Switchover in progress", "status", "phase")
		_ = f.c.Status().Update(ctx, cluster)
		_ = f.c.Create(ctx, claim(f.ns, "db-2", "oci-bv", "pv-new"))
		_ = f.c.Create(ctx, instancePod(f.ns, "db-2", "n2"))
		f.restarting, f.restart = "db-1", 15
		return &f, ctx, cancel
	}
	back := func(f *fixture) Options {
		o := opts(f.ns, f.name, nil)
		o.Back = true
		return o
	}

	// Asked during the restart, cancelled once more by the operator: two
	// asks, then the way back completes.
	f, ctx, cancel := twoInstances(t)
	defer cancel()
	f.revertAsks = 1
	go f.fakeOperator(ctx, true)
	res, err := Database(ctx, f.c, back(f))
	cluster := &unstructured.Unstructured{}
	cluster.SetGroupVersionKind(controller.CNPGClusterGVK)
	_ = f.c.Get(context.Background(), types.NamespacedName{Namespace: f.ns, Name: f.name}, cluster)
	if err != nil {
		t.Fatalf("back: %v\n%s\nstatus=%v spec.instances=%v restart=%d reverts=%d", err, strings.Join(res.Steps, "\n"), cluster.Object["status"], cluster.Object["spec"].(map[string]interface{})["instances"], f.restart, f.revertAsks)
	}
	if primaryOf(cluster) != "db-1" || instancesIn(cluster) != 1 || res.ReturnedTo != controller.LocalStorageClass {
		t.Errorf("after --back: primary %s instances %d result %+v", primaryOf(cluster), instancesIn(cluster), res)
	}
	again := 0
	for _, step := range res.Steps {
		if strings.Contains(step, "asking for the switchover") {
			again++
		}
	}
	if again != 1 {
		t.Errorf("one cancelled ask must be repeated once, got %d repeats:\n%s", again, strings.Join(res.Steps, "\n"))
	}

	// An operator that keeps cancelling: the tool stops after three asks
	// with both instances in place.
	f, ctx, cancel = twoInstances(t)
	defer cancel()
	f.revertAsks = 10
	go f.fakeOperator(ctx, true)
	res, err = Database(ctx, f.c, back(f))
	if err == nil || !strings.Contains(err.Error(), "three times") {
		t.Fatalf("expected the tool to give up after three cancelled asks, got %v\n%s", err, strings.Join(res.Steps, "\n"))
	}
	pg := &shpyrdv1.Postgres{}
	_ = f.c.Get(ctx, types.NamespacedName{Namespace: f.ns, Name: f.name}, pg)
	if ptr.Deref(pg.Spec.Instances, 0) != 2 || pg.Annotations[controller.AnnotationStorageMigration] == "" {
		t.Errorf("a given-up --back must leave both instances and the mark: %v %v", pg.Spec.Instances, pg.Annotations)
	}
}

// A data check that fails stops the migration on two instances; --back
// returns the database to its old volume.
func TestDatabaseMigrationStopsOnAFailedCheckAndGoesBack(t *testing.T) {
	f := newFixture(t, &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "p-shop"}})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	go f.fakeOperator(ctx, true)
	sql := f.sql
	sql.rows = map[string]string{"db-1": "1000", "db-2": "10"}
	res, err := Database(ctx, f.c, opts(f.ns, f.name, sql.run))
	if !errors.Is(err, ErrDataCheck) || !res.StoppedOn2 || res.Verified {
		t.Fatalf("expected the data check to stop the run: %v %+v", err, res)
	}
	pg := &shpyrdv1.Postgres{}
	if err := f.c.Get(ctx, types.NamespacedName{Namespace: f.ns, Name: f.name}, pg); err != nil {
		t.Fatal(err)
	}
	if ptr.Deref(pg.Spec.Instances, 1) != 2 || pg.Annotations[controller.AnnotationStorageMigration] == "" {
		t.Errorf("both instances must stay: %v %v", pg.Spec.Instances, pg.Annotations)
	}
	pv := &corev1.PersistentVolume{}
	_ = f.c.Get(ctx, types.NamespacedName{Name: "pv-old"}, pv)
	if pv.Spec.PersistentVolumeReclaimPolicy == corev1.PersistentVolumeReclaimRetain {
		t.Error("nothing is retained before the check passes")
	}
	// The way back.
	o := opts(f.ns, f.name, sql.run)
	o.Back = true
	back, err := Database(ctx, f.c, o)
	if err != nil {
		t.Fatalf("back: %v\n%s", err, strings.Join(back.Steps, "\n"))
	}
	cluster := &unstructured.Unstructured{}
	cluster.SetGroupVersionKind(controller.CNPGClusterGVK)
	_ = f.c.Get(ctx, types.NamespacedName{Namespace: f.ns, Name: f.name}, cluster)
	_ = f.c.Get(ctx, types.NamespacedName{Namespace: f.ns, Name: f.name}, pg)
	if primaryOf(cluster) != "db-1" || instancesIn(cluster) != 1 || pg.Annotations[controller.AnnotationStorageMigration] != "" || ptr.Deref(pg.Spec.Instances, 0) != 1 {
		t.Errorf("after --back: primary %s instances %d annotations %v spec %v", primaryOf(cluster), instancesIn(cluster), pg.Annotations, pg.Spec.Instances)
	}
	if back.ReturnedTo != controller.LocalStorageClass || back.To != controller.LocalStorageClass || back.From != "oci-bv" || back.Verified {
		t.Errorf("the result of --back must say where the database is: %+v", back)
	}
	// The platform re-renders the Cluster on the class the data is on.
	deadline := time.Now().Add(2 * time.Second)
	for {
		_ = f.c.Get(ctx, types.NamespacedName{Namespace: f.ns, Name: f.name}, cluster)
		if class, _, _ := unstructured.NestedString(cluster.Object, "spec", "storage", "storageClass"); class == controller.LocalStorageClass {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the Cluster still names the target class after --back")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Without room for the second instance the migration refuses before asking.
func TestDatabaseMigrationNeedsRoom(t *testing.T) {
	hard := corev1.ResourceList{corev1.ResourceRequestsCPU: resource.MustParse("100m"), corev1.ResourceRequestsMemory: resource.MustParse("300Mi")}
	quota := &corev1.ResourceQuota{ObjectMeta: metav1.ObjectMeta{Name: "shpyrd", Namespace: "p-shop"}, Spec: corev1.ResourceQuotaSpec{Hard: hard}, Status: corev1.ResourceQuotaStatus{Hard: hard, Used: corev1.ResourceList{corev1.ResourceRequestsCPU: resource.MustParse("62m"), corev1.ResourceRequestsMemory: resource.MustParse("256Mi")}}}
	f := newFixture(t, &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "p-shop"}}, quota)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go f.fakeOperator(ctx, true)
	_, err := Database(ctx, f.c, opts(f.ns, f.name, nil))
	if err == nil || !strings.Contains(err.Error(), "ceiling leaves no room") {
		t.Fatalf("expected the quota refusal, got %v", err)
	}
	pg := &shpyrdv1.Postgres{}
	_ = f.c.Get(ctx, types.NamespacedName{Namespace: f.ns, Name: f.name}, pg)
	if ptr.Deref(pg.Spec.Instances, 1) != 1 || pg.Annotations[controller.AnnotationStorageMigration] != "" {
		t.Errorf("no second instance may be asked for without room, and the mark goes back: %v %v", pg.Spec.Instances, pg.Annotations)
	}
	// Room under the ceiling: a 16-core quota with 72m used has room for a
	// 62m instance (millicores against cores once refused this).
	wide := corev1.ResourceList{corev1.ResourceRequestsCPU: resource.MustParse("16"), corev1.ResourceRequestsMemory: resource.MustParse("32Gi")}
	wideQuota := &corev1.ResourceQuota{ObjectMeta: metav1.ObjectMeta{Name: "shpyrd", Namespace: "p-shop"}, Spec: corev1.ResourceQuotaSpec{Hard: wide}, Status: corev1.ResourceQuotaStatus{Hard: wide, Used: corev1.ResourceList{corev1.ResourceRequestsCPU: resource.MustParse("72m"), corev1.ResourceRequestsMemory: resource.MustParse("192Mi")}}}
	f4 := newFixture(t, &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "p-shop"}}, wideQuota)
	go f4.fakeOperator(ctx, true)
	if res, err := Database(ctx, f4.c, opts(f4.ns, f4.name, nil)); err != nil {
		t.Fatalf("a 62m instance fits under a 16-core ceiling with 72m used: %v\n%s", err, strings.Join(res.Steps, "\n"))
	}
	// The storage ceiling: the new volume at the size it grows to.
	storageHard := corev1.ResourceList{corev1.ResourceRequestsStorage: resource.MustParse("40Gi")}
	storageQuota := &corev1.ResourceQuota{ObjectMeta: metav1.ObjectMeta{Name: "shpyrd-storage", Namespace: "p-shop"}, Spec: corev1.ResourceQuotaSpec{Hard: storageHard}, Status: corev1.ResourceQuotaStatus{Hard: storageHard, Used: corev1.ResourceList{corev1.ResourceRequestsStorage: resource.MustParse("1Gi")}}}
	f3 := newFixture(t, &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "p-shop"}}, storageQuota)
	go f3.fakeOperator(ctx, true)
	o := opts(f3.ns, f3.name, nil)
	o.MinSize = resource.MustParse("50Gi")
	if _, err := Database(ctx, f3.c, o); err == nil || !strings.Contains(err.Error(), "storage ceiling leaves no room") {
		t.Fatalf("expected the storage ceiling refusal, got %v", err)
	}
	o.MinSize = resource.Quantity{}
	if res, err := Database(ctx, f3.c, o); err != nil {
		t.Fatalf("1Gi more under a 40Gi ceiling fits: %v\n%s", err, strings.Join(res.Steps, "\n"))
	}
	// No other node in the pool: refused too.
	f2 := newFixture(t, &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "p-shop"}})
	_ = f2.c.Delete(ctx, node("n2"))
	go f2.fakeOperator(ctx, true)
	if _, err := Database(ctx, f2.c, opts(f2.ns, f2.name, nil)); err == nil || !strings.Contains(err.Error(), "no other ready node") {
		t.Fatalf("expected the node refusal, got %v", err)
	}
}

func unmarshal(raw string, into *RetainedDisk) error {
	if raw == "" {
		return errors.New("empty")
	}
	return json.Unmarshal([]byte(raw), into)
}
