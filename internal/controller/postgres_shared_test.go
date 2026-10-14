package controller

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// A shared database waits for the shared server, provisioning, with no
// cluster of its own; nothing else is rendered for it.
func TestSharedPostgresWaitsForTheSharedServer(t *testing.T) {
	ctx := context.Background()
	pg := &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "app-shop", Generation: 1}, Spec: shpyrdv1.PostgresSpec{Type: shpyrdv1.PostgresTypeShared}}
	base, c := newTestReconciler(t, pg)
	r := &PostgresReconciler{Client: c, Scheme: base.Scheme, Recorder: record.NewFakeRecorder(20), SystemNamespace: "shpyrd-system"}
	key := types.NamespacedName{Namespace: "app-shop", Name: "db"}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, key, pg); err != nil {
		t.Fatal(err)
	}
	if pg.Status.Phase != shpyrdv1.ResourceProvisioning || !strings.Contains(pg.Status.Message, "shared server") {
		t.Errorf("shared database = %s %q, want Provisioning and a word about the shared server", pg.Status.Phase, pg.Status.Message)
	}
	cluster := &unstructured.Unstructured{}
	cluster.SetGroupVersionKind(CNPGClusterGVK)
	if err := c.Get(ctx, key, cluster); client.IgnoreNotFound(err) != nil || err == nil {
		t.Errorf("no cluster of its own may exist for a shared database: %v", err)
	}
}
