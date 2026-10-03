package controller

import (
	"context"
	"testing"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
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
