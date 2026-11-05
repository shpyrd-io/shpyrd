package controller

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/project"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// A scheduled deletion whose date has come: the projects' namespaces go,
// the front doors come down, the rows go, the tombstone stays (#274).
func TestWorkspaceDeletionCascade(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	if _, err := st.CreateWorkspace(ctx, store.Workspace{Slug: "acme", Name: "Acme", Address: "acme.shpyrd.test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.TouchIdentity(ctx, "acme", store.Identity{Email: "owner@acme.test", Issuer: "https://idp.test", Subject: "owner"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutMembership(ctx, "acme", "owner@acme.test", store.WorkspaceRoleOwner); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SuspendWorkspace(ctx, "acme", store.SuspendReasonOperator); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ScheduleWorkspaceDeletion(ctx, "acme", time.Now().Add(-time.Minute), store.DeleteReasonOperator); err != nil {
		t.Fatal(err)
	}
	// Another workspace's project stays.
	if _, err := st.CreateWorkspace(ctx, store.Workspace{Slug: "beta", Name: "Beta", Address: "beta.shpyrd.test"}); err != nil {
		t.Fatal(err)
	}
	base, c := newTestReconciler(t,
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: project.NamespaceIn("acme", "shop"), Labels: project.NamespaceLabels("acme", "shop")}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: project.NamespaceIn("beta", "shop"), Labels: project.NamespaceLabels("beta", "shop")}},
		&networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: workspaceFrontDoorName("acme"), Namespace: "shpyrd-system", Labels: map[string]string{shpyrdv1.LabelManagedBy: "shpyrd", "shpyrd.io/workspace-front-door": "true", shpyrdv1.LabelWorkspace: "acme"}}},
	)
	r := &WorkspaceReconciler{Client: c, Scheme: base.Scheme, Store: st, Config: Config{
		Domain: "example.test", SystemNamespace: "shpyrd-system", ClusterIssuer: "letsencrypt", IngressClassExternal: "nginx", WildcardTLS: true,
	}}

	// First pass: deleting; the namespace and the door are gone; the pass
	// comes back soon.
	res, err := r.Reconcile(ctx, ctrl.Request{})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.RequeueAfter != deletionRetry {
		t.Errorf("requeue = %v, want %v while deleting", res.RequeueAfter, deletionRetry)
	}
	ws, err := st.Workspace(ctx, "acme")
	if err != nil || ws.Status != store.WorkspaceDeleting || ws.Deletion == nil {
		t.Fatalf("after one pass = %+v %v", ws, err)
	}
	if !ws.Deletion.Step(stepProjects).Done {
		t.Errorf("projects step = %+v", ws.Deletion.Step(stepProjects))
	}
	if err := c.Get(ctx, types.NamespacedName{Name: project.NamespaceIn("acme", "shop")}, &corev1.Namespace{}); !apierrors.IsNotFound(err) {
		t.Errorf("acme's namespace after one pass: %v, want gone", err)
	}
	if err := c.Get(ctx, types.NamespacedName{Name: project.NamespaceIn("beta", "shop")}, &corev1.Namespace{}); err != nil {
		t.Errorf("beta's namespace: %v, want kept", err)
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "shpyrd-system", Name: workspaceFrontDoorName("acme")}, &networkingv1.Ingress{}); !apierrors.IsNotFound(err) {
		t.Errorf("acme's front door after one pass: %v, want gone", err)
	}

	// Second pass: the doors are seen gone, the rows go, the tombstone
	// stays with the owners.
	res, err = r.Reconcile(ctx, ctrl.Request{})
	if err != nil {
		t.Fatalf("reconcile 2: %v", err)
	}
	ws, err = st.Workspace(ctx, "acme")
	if err != nil || ws.Status != store.WorkspaceDeleted || ws.DeletedAt == nil || !ws.Deletion.AllDone() {
		t.Fatalf("after two passes = %+v %v", ws, err)
	}
	if len(ws.TombstoneOwners) != 1 || ws.TombstoneOwners[0] != "owner@acme.test" {
		t.Errorf("tombstone owners = %v", ws.TombstoneOwners)
	}
	if ms, _ := st.ListMemberships(ctx, "acme"); len(ms) != 0 {
		t.Errorf("memberships after = %v", ms)
	}
	if res.RequeueAfter == deletionRetry {
		t.Errorf("still requeueing as deleting after the tombstone")
	}
	if all, _ := st.ListWorkspaces(ctx); len(all) != 2 { // default and beta
		t.Errorf("live workspaces = %d, want 2", len(all))
	}
}

// An active workspace past its expiry that nobody claimed is suspended
// and scheduled a day ahead; a protected one is left alone.
func TestWorkspaceExpiry(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	past := time.Now().Add(-time.Hour)
	if _, err := st.CreateWorkspace(ctx, store.Workspace{Slug: "bot1", Name: "Bot 1", Address: "bot1.shpyrd.test", Origin: store.OriginAgent, ExpiresAt: &past}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateWorkspace(ctx, store.Workspace{Slug: "bot2", Name: "Bot 2", Address: "bot2.shpyrd.test", Origin: store.OriginAgent, ExpiresAt: &past}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetWorkspaceProtected(ctx, "bot2", true); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour)
	if _, err := st.CreateWorkspace(ctx, store.Workspace{Slug: "bot3", Name: "Bot 3", Address: "bot3.shpyrd.test", Origin: store.OriginAgent, ExpiresAt: &future}); err != nil {
		t.Fatal(err)
	}
	base, c := newTestReconciler(t)
	r := &WorkspaceReconciler{Client: c, Scheme: base.Scheme, Store: st, Config: Config{
		Domain: "example.test", SystemNamespace: "shpyrd-system", ClusterIssuer: "letsencrypt", IngressClassExternal: "nginx", WildcardTLS: true,
	}}
	if _, err := r.Reconcile(ctx, ctrl.Request{}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	ws, _ := st.Workspace(ctx, "bot1")
	if ws.Status != store.WorkspaceSuspended || ws.SuspendReason != store.SuspendReasonExpired || ws.DeleteAfter == nil || ws.DeleteReason != store.DeleteReasonExpired {
		t.Fatalf("expired = %+v", ws)
	}
	if until := time.Until(*ws.DeleteAfter); until < store.ExpiredGrace-time.Minute || until > store.ExpiredGrace {
		t.Errorf("deleteAfter in %v, want about %v", until, store.ExpiredGrace)
	}
	for _, slug := range []string{"bot2", "bot3"} {
		if ws, _ := st.Workspace(ctx, slug); ws.Status != store.WorkspaceActive || ws.DeleteAfter != nil {
			t.Errorf("%s = %s %v, want active and unscheduled", slug, ws.Status, ws.DeleteAfter)
		}
	}
}
