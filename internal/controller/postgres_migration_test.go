package controller

import (
	"context"
	"testing"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/sizes"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestPostgresMigrationFreezesStorageReconciliation(t *testing.T) {
	pg := &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "app-project", Annotations: map[string]string{AnnotationDataMove: "migration"}}}
	base, c := newTestReconciler(t, pg)
	r := &PostgresReconciler{Client: c, Scheme: base.Scheme}
	result, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pg)})
	if err != nil || result.RequeueAfter == 0 {
		t.Fatalf("migration not deferred: %+v %v", result, err)
	}
	cluster := &unstructured.Unstructured{}
	cluster.SetGroupVersionKind(CNPGClusterGVK)
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(pg), cluster); err == nil {
		t.Fatal("controller created a cluster during migration")
	}
}

func TestPostgresPlacementKeepsTheDataPool(t *testing.T) {
	for _, pool := range []string{"data", "platform", ""} {
		t.Run("pool="+pool, func(t *testing.T) {
			pg := &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "app-project", Annotations: map[string]string{shpyrdv1.AnnotationPlacement: "target-node"}}}
			cluster := desiredCNPGCluster(pg, resource.MustParse("1Gi"), sizes.Defaults().Postgres.Sizes[0], corev1.ResourceRequirements{}, LocalStorageClass, pool)
			selector, _, err := unstructured.NestedStringMap(cluster.Object, "spec", "affinity", "nodeSelector")
			if err != nil {
				t.Fatal(err)
			}
			if selector[corev1.LabelHostname] != "target-node" || selector[PoolLabel] != pool {
				t.Fatalf("placement must keep node and pool: %v", selector)
			}
			if pool == "" && len(selector) != 1 {
				t.Fatalf("single-pool placement has extra selectors: %v", selector)
			}
		})
	}
}
