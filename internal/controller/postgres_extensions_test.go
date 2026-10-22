package controller

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// A dedicated database is declared to CloudNativePG as a Database object
// with pgvector, as a shared tenant's is: the owner cannot create the
// extension alone, the operator does. The object adopts initdb's database,
// drops nothing when removed, and goes with the Postgres resource.
func TestDedicatedDatabasesCarryPgvector(t *testing.T) {
	storage := resource.MustParse("1Gi")
	pg := &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "p-shop", UID: "uid-shop-db", Generation: 1}, Spec: shpyrdv1.PostgresSpec{Storage: &storage}}
	r, c := sharedReconciler(t, 200, pg)
	settle(t, r, client.ObjectKeyFromObject(pg))
	getU(t, c, "Cluster", "p-shop", "db")
	db := getU(t, c, "Database", "p-shop", "db-db")
	cluster, _, _ := unstructured.NestedString(db.Object, "spec", "cluster", "name")
	name, _, _ := unstructured.NestedString(db.Object, "spec", "name")
	owner, _, _ := unstructured.NestedString(db.Object, "spec", "owner")
	reclaim, _, _ := unstructured.NestedString(db.Object, "spec", "databaseReclaimPolicy")
	ensure, _, _ := unstructured.NestedString(db.Object, "spec", "ensure")
	if cluster != "db" || name != PostgresDatabase || owner != PostgresUser || reclaim != "retain" || ensure != "present" {
		t.Errorf("database object: cluster=%s name=%s owner=%s reclaim=%s ensure=%s", cluster, name, owner, reclaim, ensure)
	}
	exts, _, _ := unstructured.NestedSlice(db.Object, "spec", "extensions")
	if len(exts) != 1 || exts[0].(map[string]interface{})["name"] != "vector" || exts[0].(map[string]interface{})["ensure"] != "present" {
		t.Errorf("extensions = %v, want vector present", exts)
	}
	refs := db.GetOwnerReferences()
	if len(refs) != 1 || refs[0].Kind != "Postgres" || refs[0].Name != "db" {
		t.Errorf("owner references = %v, want the Postgres resource", refs)
	}

	// Converted from a shared database: the shared names, so the
	// extension lands in the database the app uses.
	conv := &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "conv", Namespace: "p-shop", UID: "uid-shop-conv", Annotations: map[string]string{AnnotationSharedTenant: "abc123"}}, Spec: shpyrdv1.PostgresSpec{Storage: &storage}}
	if err := c.Create(context.Background(), conv); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ensureDedicatedDatabase(context.Background(), conv); err != nil {
		t.Fatal(err)
	}
	cdb := getU(t, c, "Database", "p-shop", "conv-db")
	name, _, _ = unstructured.NestedString(cdb.Object, "spec", "name")
	owner, _, _ = unstructured.NestedString(cdb.Object, "spec", "owner")
	if name != sharedDatabaseName("abc123") || owner != sharedRoleName("abc123") {
		t.Errorf("converted database object: name=%s owner=%s", name, owner)
	}
}

// A conversion to shared stamps the shared tenant's names on the resource
// while the dedicated cluster still holds "app"; the Database object keeps
// the cluster's own names (production, 2026-10-22: every reconcile failed
// on "spec.name: name is immutable" and the database sat at Failed).
func TestDedicatedDatabaseObjectKeepsTheClustersNamesThroughAConversion(t *testing.T) {
	ctx := context.Background()
	storage := resource.MustParse("1Gi")
	pg := &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "p-shop", UID: "uid-shop-db", Generation: 1}, Spec: shpyrdv1.PostgresSpec{Storage: &storage}}
	r, c := sharedReconciler(t, 200, pg)
	settle(t, r, client.ObjectKeyFromObject(pg))
	before := getU(t, c, "Database", "p-shop", "db-db")
	if name, _, _ := unstructured.NestedString(before.Object, "spec", "name"); name != PostgresDatabase {
		t.Fatalf("object made with name %q", name)
	}
	// The conversion's first step: the shared names on the resource.
	if err := c.Get(ctx, client.ObjectKeyFromObject(pg), pg); err != nil {
		t.Fatal(err)
	}
	pg.Annotations = mergeMaps(pg.Annotations, map[string]string{AnnotationConvert: shpyrdv1.PostgresTypeShared, AnnotationSharedTenant: "d1119649", AnnotationSharedServer: "pg-shared-1"})
	if err := c.Update(ctx, pg); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ensureDedicatedDatabase(ctx, pg); err != nil {
		t.Fatalf("with the conversion stamped: %v", err)
	}
	after := getU(t, c, "Database", "p-shop", "db-db")
	name, _, _ := unstructured.NestedString(after.Object, "spec", "name")
	owner, _, _ := unstructured.NestedString(after.Object, "spec", "owner")
	if name != PostgresDatabase || owner != PostgresUser {
		t.Errorf("object renamed to %s/%s; the cluster still holds app/app", name, owner)
	}
}
