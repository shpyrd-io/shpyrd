package api

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// A shared database lives on the platform's server: an archive carries its
// spec and not its data, an operation neither fences nor waits for a pod
// of it, and a move has nothing of it to move.
func TestProjectArchiveSkipsSharedDatabases(t *testing.T) {
	app := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-shop"}}
	shared := &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "app-shop"}, Spec: shpyrdv1.PostgresSpec{Type: shpyrdv1.PostgresTypeShared}}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n1", Labels: map[string]string{corev1.LabelHostname: "n1"}}}
	s, cr := newTestServer(t, nil, []client.Object{app, shared, node})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	op := &projectArchiveOperation{ID: "op1", Limits: map[string]int{}}
	databases := []archivedDatabase{{Name: "db", Spec: shared.Spec}}
	if err := s.fenceArchiveDatabases(ctx, app, op, databases); err != nil {
		t.Fatalf("fencing with a shared database waited or failed: %v", err)
	}
	if len(op.Limits) != 0 {
		t.Errorf("a shared database is not fenced: limits %v", op.Limits)
	}
	if err := cr.Get(ctx, types.NamespacedName{Namespace: "app-shop", Name: "db"}, shared); err != nil || shared.Annotations[shpyrdv1.AnnotationMaintenance] != "" {
		t.Errorf("a shared database is not put in maintenance: %v %v", err, shared.Annotations)
	}
	if databases[0].carried() {
		t.Error("a shared database's data is not in the archive")
	}
	if dedicated := (archivedDatabase{Name: "big", Spec: shpyrdv1.PostgresSpec{Size: "shared-s"}}); !dedicated.carried() {
		t.Error("a dedicated database's dump is in the archive")
	}
	if _, err := s.prepareProjectMove(ctx, app, placementGroup{Database: "db"}, node, "oci-bv"); err == nil || !strings.Contains(err.Error(), "has no disk of its own to move") {
		t.Errorf("moving a shared database = %v", err)
	}
	if !strings.Contains(sharedArchiveNote("db"), "shpyrd pg restore") {
		t.Errorf("the note names the way back: %s", sharedArchiveNote("db"))
	}
}

// The placement view shows what is placed in the project: a shared
// database, on the platform's server, is not a group of it.
func TestProjectPlacementLeavesSharedDatabasesOut(t *testing.T) {
	app := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-shop"}}
	shared := &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "app-shop"}, Spec: shpyrdv1.PostgresSpec{Type: shpyrdv1.PostgresTypeShared}}
	dedicated := &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "big", Namespace: "app-shop"}, Spec: shpyrdv1.PostgresSpec{Size: "shared-s"}}
	s, _ := newTestServer(t, nil, []client.Object{app, shared, dedicated})
	groups, _, err := s.projectPlacement(context.Background(), app)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, g := range groups {
		ids = append(ids, g.ID)
	}
	joined := strings.Join(ids, " ")
	if strings.Contains(joined, "postgres:db") || !strings.Contains(joined, "postgres:big") {
		t.Errorf("groups = %v", ids)
	}
}
