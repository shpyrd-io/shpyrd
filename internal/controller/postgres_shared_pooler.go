package controller

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// The pooler (storage plan, step 3): a CloudNativePG Pooler in transaction
// mode per shared server, switched on when the seven-day p95 of the
// server's client connections passes 60 % of max_connections. The metering
// loop stamps the server (an operator may stamp it by hand); the Postgres
// reconciler renders the Pooler and, once it answers, turns the tenants'
// Services to it. DATABASE_DIRECT_URL, there from the first day, keeps the
// server itself for what a transaction pool cannot carry. Once on, it
// stays on.
var CNPGPoolerGVK = schema.GroupVersionKind{Group: "postgresql.cnpg.io", Version: "v1", Kind: "Pooler"}

const (
	// AnnotationPooler on a shared server switches its pooler on; its value
	// is when it was asked for.
	AnnotationPooler = "shpyrd.io/pooler"
	// sharedPoolerAt is the share of max_connections the seven-day p95 of
	// client connections passes for the pooler to come.
	sharedPoolerAt     = 0.60
	sharedPoolerWindow = "7d"
)

func sharedPoolerName(server string) string { return server + "-pooler" }

// sharedPoolerParameters bound what hundreds of small databases take from
// the server: PgBouncer pools per user and database, so each pool is kept
// small and an idle server connection closes within a minute; prepared
// statements are carried at the protocol level.
var sharedPoolerParameters = map[string]interface{}{
	"max_client_conn":         "4000",
	"default_pool_size":       "5",
	"max_db_connections":      "10",
	"server_idle_timeout":     "60",
	"max_prepared_statements": "200",
}

// sharedPoolerWanted says whether a server whose client connections' p95
// is this wants its pooler.
func sharedPoolerWanted(p95 float64, maxConnections int) bool {
	return maxConnections > 0 && p95 > sharedPoolerAt*float64(maxConnections)
}

// sharedPoolerSpec is the Pooler of a server: as many instances as the
// server, in front of its primary, transaction mode, on the data pool.
func (r *PostgresReconciler) sharedPoolerSpec(server string, cfg SharedPostgres) map[string]interface{} {
	pod := map[string]interface{}{
		"containers": []interface{}{map[string]interface{}{
			"name": "pgbouncer",
			"resources": map[string]interface{}{
				"requests": map[string]interface{}{"cpu": "100m", "memory": "128Mi"},
				"limits":   map[string]interface{}{"memory": "512Mi"},
			},
		}},
	}
	if r.DataPool != "" {
		pod["nodeSelector"] = map[string]interface{}{PoolLabel: r.DataPool}
	}
	return map[string]interface{}{
		"cluster":   map[string]interface{}{"name": server},
		"instances": int64(cfg.Instances),
		"type":      "rw",
		"pgbouncer": map[string]interface{}{"poolMode": "transaction", "parameters": sharedPoolerParameters},
		"template":  map[string]interface{}{"spec": pod},
	}
}

// ensureSharedPooler renders the server's pooler when the server asks for
// one, and says whether tenants connect through it now: it does once the
// pooler's Deployment answers. A pooler coming up is waited for.
func (r *PostgresReconciler) ensureSharedPooler(ctx context.Context, server *unstructured.Unstructured) (pooled, waiting bool, err error) {
	if server.GetAnnotations()[AnnotationPooler] == "" {
		return false, false, nil
	}
	name := sharedPoolerName(server.GetName())
	pooler := &unstructured.Unstructured{}
	pooler.SetGroupVersionKind(CNPGPoolerGVK)
	pooler.SetNamespace(SharedPostgresNamespace)
	pooler.SetName(name)
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, pooler, func() error {
		pooler.SetLabels(mergeMaps(pooler.GetLabels(), map[string]string{shpyrdv1.LabelManagedBy: "shpyrd", sharedServerLabel: "pooler", sharedOfServerLabel: server.GetName()}))
		pooler.Object["spec"] = r.sharedPoolerSpec(server.GetName(), r.sharedConfig())
		return nil
	}); err != nil {
		return false, false, err
	}
	deploy := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: SharedPostgresNamespace, Name: name}, deploy); err != nil {
		if apierrors.IsNotFound(err) {
			return false, true, nil
		}
		return false, false, err
	}
	if deploy.Status.ReadyReplicas < 1 {
		return false, true, nil
	}
	return true, false, nil
}

// sharedServerTenants maps a shared server to the databases on it, for the
// day its pooler is switched on: each turns to the pooler.
func (r *PostgresReconciler) sharedServerTenants(ctx context.Context, obj client.Object) []reconcile.Request {
	if obj.GetNamespace() != SharedPostgresNamespace || obj.GetLabels()[sharedServerLabel] != "server" {
		return nil
	}
	var list shpyrdv1.PostgresList
	if err := r.List(ctx, &list); err != nil {
		return nil
	}
	var out []reconcile.Request
	for _, pg := range list.Items {
		if pg.Spec.Type == shpyrdv1.PostgresTypeShared && pg.Annotations[AnnotationSharedServer] == obj.GetName() {
			out = append(out, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&pg)})
		}
	}
	return out
}

// sharedServiceTarget is where a tenant's Service points: the server's
// pooler when the tenants go through it, else its primary.
func sharedServiceTarget(server string, pooled bool) string {
	if pooled {
		return fmt.Sprintf("%s.%s.svc.cluster.local", sharedPoolerName(server), SharedPostgresNamespace)
	}
	return sharedDirectTarget(server)
}

// sharedDirectTarget is the server's primary, which DATABASE_DIRECT_URL
// always reaches.
func sharedDirectTarget(server string) string {
	return fmt.Sprintf("%s-rw.%s.svc.cluster.local", server, SharedPostgresNamespace)
}

// sharedDirectService names the tenant's second Service, at the primary.
func sharedDirectService(pg *shpyrdv1.Postgres) string { return pg.Name + "-direct" }
