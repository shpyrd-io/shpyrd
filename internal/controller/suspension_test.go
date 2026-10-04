package controller

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/project"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// A suspended workspace costs nothing: the workspace reconciler marks its
// apps, databases and Redis, each is reconciled at once, the Redis stops
// and the database hibernates, an HA one too. Activated, the marks go and
// everything comes back; another workspace's things are never marked.
func TestASuspendedWorkspaceStopsItsStores(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	for _, slug := range []string{"acme", "beta"} {
		if _, err := st.CreateWorkspace(ctx, store.Workspace{Slug: slug, Name: slug}); err != nil {
			t.Fatal(err)
		}
	}
	storage := resource.MustParse("10Gi")
	app := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-acme-shop", Labels: project.NamespaceLabels("acme", "shop")}}
	other := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "blog", Namespace: "app-beta-blog", Labels: project.NamespaceLabels("beta", "blog")}}
	pg := &shpyrdv1.Postgres{
		ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "app-acme-shop"},
		Spec:       shpyrdv1.PostgresSpec{Storage: &storage, Instances: ptr.To[int32](2)},
	}
	rd := &shpyrdv1.Redis{ObjectMeta: metav1.ObjectMeta{Name: "cache", Namespace: "app-acme-shop"}}
	otherRd := &shpyrdv1.Redis{ObjectMeta: metav1.ObjectMeta{Name: "cache", Namespace: "app-beta-blog"}}
	cluster := &unstructured.Unstructured{}
	cluster.SetGroupVersionKind(CNPGClusterGVK)
	cluster.SetName("db")
	cluster.SetNamespace("app-acme-shop")
	base, c := newTestReconciler(t, app, other, pg, rd, otherRd, cluster)
	ws := &WorkspaceReconciler{Client: c, Scheme: base.Scheme, Store: st, Config: Config{SystemNamespace: "shpyrd-system"}}
	redis := &RedisReconciler{Client: c, Scheme: base.Scheme, Recorder: record.NewFakeRecorder(20), SystemNamespace: "shpyrd-system"}
	postgres := &PostgresReconciler{Client: c, Scheme: base.Scheme, Recorder: record.NewFakeRecorder(20), SystemNamespace: "shpyrd-system"}

	pass := func() {
		t.Helper()
		all, err := st.ListWorkspaces(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := ws.syncSuspension(ctx, all); err != nil {
			t.Fatal(err)
		}
	}
	marked := func(obj client.Object) bool {
		t.Helper()
		if err := c.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
			t.Fatal(err)
		}
		return suspendedWithWorkspace(obj)
	}
	redisReplicas := func(ns string) int32 {
		t.Helper()
		if _, err := redis.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: "cache"}}); err != nil {
			t.Fatal(err)
		}
		sts := &appsv1.StatefulSet{}
		if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: "cache"}, sts); err != nil {
			t.Fatal(err)
		}
		return *sts.Spec.Replicas
	}
	hibernated := func() bool {
		t.Helper()
		got := &shpyrdv1.Postgres{}
		if err := c.Get(ctx, client.ObjectKeyFromObject(pg), got); err != nil {
			t.Fatal(err)
		}
		if err := postgres.reconcilePostgresSleep(ctx, got); err != nil {
			t.Fatal(err)
		}
		if err := c.Status().Update(ctx, got); err != nil { // as Reconcile does
			t.Fatal(err)
		}
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(CNPGClusterGVK)
		if err := c.Get(ctx, types.NamespacedName{Namespace: "app-acme-shop", Name: "db"}, u); err != nil {
			t.Fatal(err)
		}
		return u.GetAnnotations()[pgHibernationAnnotation] == "on"
	}

	pass()
	if marked(app) || marked(pg) || marked(rd) || redisReplicas("app-acme-shop") != 1 || hibernated() {
		t.Fatal("an active workspace's things must run")
	}

	if _, err := st.SetWorkspaceStatus(ctx, "acme", store.WorkspaceSuspended); err != nil {
		t.Fatal(err)
	}
	pass()
	if !marked(app) || !marked(pg) || !marked(rd) {
		t.Error("a suspended workspace's app, database and Redis must be marked")
	}
	if marked(other) || marked(otherRd) || redisReplicas("app-beta-blog") != 1 {
		t.Error("another workspace's things must not be touched")
	}
	if n := redisReplicas("app-acme-shop"); n != 0 {
		t.Errorf("a suspended workspace's Redis must stop, got %d replicas", n)
	}
	if !hibernated() {
		t.Error("a suspended workspace's database must hibernate, HA ones too")
	}

	if _, err := st.SetWorkspaceStatus(ctx, "acme", store.WorkspaceActive); err != nil {
		t.Fatal(err)
	}
	pass()
	if marked(app) || marked(pg) || marked(rd) {
		t.Error("an activated workspace's marks must go")
	}
	if n := redisReplicas("app-acme-shop"); n != 1 {
		t.Errorf("an activated workspace's Redis must come back, got %d replicas", n)
	}
	if hibernated() {
		t.Error("an activated workspace's database must wake")
	}
}
