package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// platformExtensions are installed in every database by the platform,
// shared and dedicated (the operator creates them, as the Database object
// says): the ones the owner cannot create alone because PostgreSQL does
// not mark them trusted, and the plan calls essential. pgvector weighs
// 184 KB per database. The trusted extensions (pgcrypto, uuid-ossp,
// citext, hstore, pg_trgm, unaccent, btree_gin, btree_gist, ltree,
// intarray and the rest PostgreSQL marks so) are the owner's own CREATE
// EXTENSION; anything else the image does not carry is not available.
var platformExtensions = []string{"vector"}

// platformExtensionSpecs is the list as a Database object's spec.extensions.
func platformExtensionSpecs() []interface{} {
	specs := make([]interface{}, 0, len(platformExtensions))
	for _, name := range platformExtensions {
		specs = append(specs, map[string]interface{}{"name": name, "ensure": "present"})
	}
	return specs
}

// ensureDedicatedDatabase declares a dedicated cluster's database to
// CloudNativePG as a Database object, the way a shared tenant's is: its
// owner, and the extensions the platform installs (pgvector; the owner
// cannot create it, PostgreSQL does not trust it). The object adopts the
// database initdb made; its reclaim policy is retain, so removing the
// object drops nothing (the cluster's removal does). Owned by the Postgres
// resource, it goes with it. It says whether the operator applied it.
func (r *PostgresReconciler) ensureDedicatedDatabase(ctx context.Context, pg *shpyrdv1.Postgres) (bool, error) {
	initdb := dedicatedInitdb(pg)
	return r.ensureDatabaseObject(ctx, pg, initdb["database"].(string), initdb["owner"].(string))
}

// databaseObjectName is the Database object of a dedicated cluster.
func databaseObjectName(pg *shpyrdv1.Postgres) string { return pg.Name + "-db" }

// ensureDatabaseObject writes the Database object of a dedicated cluster
// for the database and owner named.
func (r *PostgresReconciler) ensureDatabaseObject(ctx context.Context, pg *shpyrdv1.Postgres, database, owner string) (bool, error) {
	db := &unstructured.Unstructured{}
	db.SetGroupVersionKind(CNPGDatabaseGVK)
	db.SetNamespace(pg.Namespace)
	db.SetName(databaseObjectName(pg))
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, db, func() error {
		db.SetLabels(mergeMaps(db.GetLabels(), map[string]string{shpyrdv1.LabelManagedBy: "shpyrd", "shpyrd.io/postgres": pg.Name}))
		if err := controllerutil.SetControllerReference(pg, db, r.Scheme); err != nil {
			return err
		}
		return unstructured.SetNestedMap(db.Object, map[string]interface{}{
			"cluster":               map[string]interface{}{"name": pg.Name},
			"name":                  database,
			"owner":                 owner,
			"databaseReclaimPolicy": "retain",
			"ensure":                "present",
			"extensions":            platformExtensionSpecs(),
		}, "spec")
	}); err != nil {
		return false, err
	}
	applied, _, _ := unstructured.NestedBool(db.Object, "status", "applied")
	return applied, nil
}
