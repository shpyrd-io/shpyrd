package storagemigrate

import (
	"context"
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
	return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{corev1.LabelHostname: name, controller.PoolLabel: "data"}}, Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}}}
}

func claim(ns, name, class, pv string) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}, Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: ptr.To(class), VolumeName: pv, Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")}}}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound}}
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

// fakeOperator plays the platform and CloudNativePG: a second instance
// appears on the class the migration asked for, a switchover happens when
// asked, the extra instance goes when the database is back to one.
func fakeOperator(ctx context.Context, t *testing.T, c client.Client, ns, name string) {
	t.Helper()
	key := types.NamespacedName{Namespace: ns, Name: name}
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Millisecond):
		}
		pg := &shpyrdv1.Postgres{}
		if err := c.Get(ctx, key, pg); err != nil {
			continue
		}
		cluster := &unstructured.Unstructured{}
		cluster.SetGroupVersionKind(controller.CNPGClusterGVK)
		if err := c.Get(ctx, key, cluster); err != nil {
			continue
		}
		names, _, _ := unstructured.NestedStringSlice(cluster.Object, "status", "instanceNames")
		target := pg.Annotations[controller.AnnotationStorageMigration]
		switch {
		case ptr.Deref(pg.Spec.Instances, 1) == 2 && len(names) == 1 && target != "":
			second := claim(ns, name+"-2", target, "pv-new")
			if err := c.Create(ctx, second); err != nil && !strings.Contains(err.Error(), "already exists") {
				t.Logf("fake operator: %v", err)
			}
			_ = c.Create(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name + "-2", Namespace: ns}, Spec: corev1.PodSpec{NodeName: "n2"}})
			_ = unstructured.SetNestedField(cluster.Object, int64(2), "spec", "instances")
			_ = c.Update(ctx, cluster)
			_ = unstructured.SetNestedStringSlice(cluster.Object, []string{name + "-1", name + "-2"}, "status", "instanceNames")
			_ = unstructured.SetNestedField(cluster.Object, int64(2), "status", "readyInstances")
			_ = c.Status().Update(ctx, cluster)
		case primaryOf(cluster) != name+"-2" && func() bool {
			tp, _, _ := unstructured.NestedString(cluster.Object, "status", "targetPrimary")
			return tp == name+"-2"
		}():
			_ = unstructured.SetNestedField(cluster.Object, name+"-2", "status", "currentPrimary")
			_ = unstructured.SetNestedField(cluster.Object, "Cluster in healthy state", "status", "phase")
			_ = c.Status().Update(ctx, cluster)
		case ptr.Deref(pg.Spec.Instances, 1) == 1 && len(names) == 2:
			_ = c.Delete(ctx, &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: name + "-1", Namespace: ns}})
			_ = unstructured.SetNestedField(cluster.Object, int64(1), "spec", "instances")
			_ = c.Update(ctx, cluster)
			_ = unstructured.SetNestedStringSlice(cluster.Object, []string{name + "-2"}, "status", "instanceNames")
			_ = unstructured.SetNestedField(cluster.Object, int64(1), "status", "readyInstances")
			_ = c.Status().Update(ctx, cluster)
		}
	}
}

func TestDatabaseMigratesBySwitchoverAndKeepsTheOldVolume(t *testing.T) {
	scheme, err := kube.Scheme()
	if err != nil {
		t.Fatal(err)
	}
	ns, name := "p-shop", "db"
	pg := &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Annotations: map[string]string{shpyrdv1.AnnotationPlacement: "n1"}}}
	oldPV := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "pv-old"}, Spec: corev1.PersistentVolumeSpec{StorageClassName: controller.LocalStorageClass, PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimDelete}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		pg, claim(ns, name+"-1", controller.LocalStorageClass, "pv-old"), oldPV,
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name + "-1", Namespace: ns}, Spec: corev1.PodSpec{NodeName: "n1"}},
		node("n1"), node("n2"), &storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "oci-bv"}, Provisioner: "x"},
	).WithRuntimeObjects(cnpgCluster(ns, name)).WithStatusSubresource(cnpgCluster(ns, name)).Build()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	go fakeOperator(ctx, t, c, ns, name)

	var mu sync.Mutex
	queries := map[string]int{}
	sql := func(_ context.Context, _, pod, statement string) (string, error) {
		mu.Lock()
		queries[pod]++
		mu.Unlock()
		switch {
		case strings.Contains(statement, "pg_database_size"):
			return "1000000\n", nil
		case strings.Contains(statement, "pg_stat_user_tables"):
			return "public.orders\npublic.users\n", nil
		case strings.Contains(statement, "public.orders"):
			return "42\n", nil
		default:
			return "7\n", nil
		}
	}
	res, err := Database(ctx, c, Options{Namespace: ns, Name: name, TargetClass: "oci-bv", SnapshotClass: "oci-bv-backup", Wait: 15 * time.Second, Poll: 10 * time.Millisecond, SQL: sql, Out: io.Discard})
	if err != nil {
		t.Fatalf("migrate: %v\n%s", err, strings.Join(res.Steps, "\n"))
	}
	if res.From != controller.LocalStorageClass || res.To != "oci-bv" || !res.Verified || res.OldPV != "pv-old" || res.NewClaim != "db-2" {
		t.Errorf("result = %+v", res)
	}
	if res.Rows["public.orders"] != [2]int{42, 42} || res.Rows["public.users"] != [2]int{7, 7} || res.SizeBefore != 1000000 || res.SizeAfter != 1000000 {
		t.Errorf("checks = rows %v size %d/%d", res.Rows, res.SizeBefore, res.SizeAfter)
	}
	if queries["db-1"] == 0 || queries["db-2"] == 0 {
		t.Errorf("the checks must run on the old primary before and the new one after: %v", queries)
	}
	// The old volume is kept, the marks are gone, the database runs one
	// instance on the new volume, and a snapshot of it was requested.
	if err := c.Get(ctx, types.NamespacedName{Name: "pv-old"}, oldPV); err != nil {
		t.Fatal(err)
	}
	if oldPV.Spec.PersistentVolumeReclaimPolicy != corev1.PersistentVolumeReclaimRetain || oldPV.Labels[LabelRetainedMigration] != name {
		t.Errorf("old volume = %s %v", oldPV.Spec.PersistentVolumeReclaimPolicy, oldPV.Labels)
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, pg); err != nil {
		t.Fatal(err)
	}
	if pg.Annotations[controller.AnnotationStorageMigration] != "" || pg.Annotations[shpyrdv1.AnnotationPlacement] != "" || ptr.Deref(pg.Spec.Instances, 0) != 1 {
		t.Errorf("database after = annotations %v instances %v", pg.Annotations, pg.Spec.Instances)
	}
	snaps := &unstructured.UnstructuredList{}
	snaps.SetGroupVersionKind(controller.VolumeSnapshotGVK.GroupVersion().WithKind("VolumeSnapshotList"))
	if err := c.List(ctx, snaps, client.InNamespace(ns)); err != nil {
		t.Fatal(err)
	}
	if len(snaps.Items) != 1 || res.Snapshot == "" {
		t.Errorf("snapshots after = %d (%q)", len(snaps.Items), res.Snapshot)
	}
	// Run again: nothing to do, and nothing breaks.
	again, err := Database(ctx, c, Options{Namespace: ns, Name: name, TargetClass: "oci-bv", Wait: 2 * time.Second, Poll: 10 * time.Millisecond, Out: io.Discard})
	if err != nil || !again.Verified || len(again.Steps) == 0 || !strings.Contains(again.Steps[0], "already") {
		t.Errorf("second run = %v %+v", err, again)
	}
}

// Without another node for the new instance the migration refuses before
// touching anything.
func TestDatabaseMigrationNeedsAnotherNode(t *testing.T) {
	scheme, err := kube.Scheme()
	if err != nil {
		t.Fatal(err)
	}
	ns, name := "p-shop", "db"
	pg := &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		pg, claim(ns, name+"-1", controller.LocalStorageClass, "pv-old"),
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name + "-1", Namespace: ns}, Spec: corev1.PodSpec{NodeName: "n1"}},
		node("n1"), &storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "oci-bv"}, Provisioner: "x"},
	).WithRuntimeObjects(cnpgCluster(ns, name)).WithStatusSubresource(cnpgCluster(ns, name)).Build()
	_, err = Database(context.Background(), c, Options{Namespace: ns, Name: name, TargetClass: "oci-bv", Wait: time.Second, Poll: 10 * time.Millisecond, Out: io.Discard})
	if err == nil || !strings.Contains(err.Error(), "no other ready node") {
		t.Fatalf("expected the node refusal, got %v", err)
	}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: name}, pg); err != nil {
		t.Fatal(err)
	}
	if pg.Annotations[controller.AnnotationStorageMigration] != "" || pg.Spec.Instances != nil {
		t.Errorf("the refusal must leave the database untouched: %v %v", pg.Annotations, pg.Spec.Instances)
	}
}
