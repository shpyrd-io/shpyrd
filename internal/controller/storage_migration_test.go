package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// Under the storage-migration annotation a node-local cluster is rendered
// on the target class at the size it has, without its node pin; once the
// annotation is gone the profile decides, as for any cluster on its class.
func TestStorageMigrationRendersTargetClassAtCurrentSize(t *testing.T) {
	ctx := context.Background()
	oneGi := resource.MustParse("1Gi")
	pg := &shpyrdv1.Postgres{
		ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "app-shop", Generation: 1, Annotations: map[string]string{
			AnnotationStorageMigration: "oci-bv", shpyrdv1.AnnotationPlacement: "10.0.1.228",
		}},
		Spec: shpyrdv1.PostgresSpec{Storage: &oneGi, Instances: ptr.To[int32](2)},
	}
	cluster := &unstructured.Unstructured{}
	cluster.SetGroupVersionKind(CNPGClusterGVK)
	cluster.SetName("db")
	cluster.SetNamespace("app-shop")
	cluster.Object["spec"] = map[string]interface{}{"instances": int64(1), "storage": map[string]interface{}{"size": "1Gi", "storageClass": LocalStorageClass}}
	base, c := newTestReconciler(t, pg, cluster)
	r := &PostgresReconciler{Client: c, Scheme: base.Scheme, Recorder: record.NewFakeRecorder(20), SystemNamespace: "shpyrd-system", Storage: StorageProfile{Class: "oci-bv", MinSize: "50Gi"}, DataPool: "data"}
	key := types.NamespacedName{Namespace: "app-shop", Name: "db"}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, key, cluster); err != nil {
		t.Fatal(err)
	}
	size, _, _ := unstructured.NestedString(cluster.Object, "spec", "storage", "size")
	class, _, _ := unstructured.NestedString(cluster.Object, "spec", "storage", "storageClass")
	instances, _, _ := unstructured.NestedInt64(cluster.Object, "spec", "instances")
	selector, _, _ := unstructured.NestedStringMap(cluster.Object, "spec", "affinity", "nodeSelector")
	if class != "oci-bv" || size != "1Gi" || instances != 2 {
		t.Errorf("migrating cluster = %s on %s with %d instances, want 1Gi on oci-bv with 2", size, class, instances)
	}
	if _, pinned := selector[corev1.LabelHostname]; pinned || selector[PoolLabel] != "data" {
		t.Errorf("migrating cluster keeps the node pin or loses the pool: %v", selector)
	}

	// Done: the annotation and the pin gone, one instance; the cluster is on
	// the profile's class now, so the profile's minimum applies.
	if err := c.Get(ctx, key, pg); err != nil {
		t.Fatal(err)
	}
	delete(pg.Annotations, AnnotationStorageMigration)
	delete(pg.Annotations, shpyrdv1.AnnotationPlacement)
	pg.Spec.Instances = ptr.To[int32](1)
	if err := c.Update(ctx, pg); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, key, cluster); err != nil {
		t.Fatal(err)
	}
	size, _, _ = unstructured.NestedString(cluster.Object, "spec", "storage", "size")
	class, _, _ = unstructured.NestedString(cluster.Object, "spec", "storage", "storageClass")
	if class != "oci-bv" || size != "50Gi" {
		t.Errorf("after the migration = %s on %s, want 50Gi on oci-bv", size, class)
	}
}

// While migrating, a database with a sleep policy stays awake and keeps its
// Service, although it runs two instances for the duration.
func TestStorageMigrationKeepsTheDatabaseAwakeWithItsService(t *testing.T) {
	ctx := context.Background()
	storage := resource.MustParse("1Gi")
	port := int32(15000)
	pg := &shpyrdv1.Postgres{
		ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "app-shop", Annotations: map[string]string{AnnotationStorageMigration: "oci-bv"}},
		Spec:       shpyrdv1.PostgresSpec{Storage: &storage, Instances: ptr.To[int32](2), Sleep: &shpyrdv1.PostgresSleepSpec{After: "5m"}},
		Status:     shpyrdv1.ResourceStatus{Sleep: &shpyrdv1.PostgresSleepStatus{State: pgSleeping, WakePort: &port}},
	}
	cluster := &unstructured.Unstructured{}
	cluster.SetGroupVersionKind(CNPGClusterGVK)
	cluster.SetName("db")
	cluster.SetNamespace("app-shop")
	cluster.SetAnnotations(map[string]string{pgHibernationAnnotation: "on"})
	cluster.Object["spec"] = map[string]interface{}{"instances": int64(2), "storage": map[string]interface{}{"size": "1Gi", "storageClass": LocalStorageClass}}
	base, c := newTestReconciler(t, pg, cluster)
	r := &PostgresReconciler{Client: c, Scheme: base.Scheme, Recorder: record.NewFakeRecorder(20), SystemNamespace: "shpyrd-system", SleepAllowed: always}
	if err := r.reconcilePostgresSleep(ctx, pg); err != nil {
		t.Fatal(err)
	}
	if pg.Status.Sleep.State != pgAwake {
		t.Errorf("migrating database sleep state = %q, want awake", pg.Status.Sleep.State)
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "app-shop", Name: "db"}, cluster); err != nil {
		t.Fatal(err)
	}
	if cluster.GetAnnotations()[pgHibernationAnnotation] == "on" {
		t.Error("migrating database left hibernated")
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "app-shop", Name: "db"}, &corev1.Service{}); err != nil {
		if client.IgnoreNotFound(err) != nil {
			t.Fatal(err)
		}
		t.Error("migrating database lost its Service")
	}
}
