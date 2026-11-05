package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/project"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// Scheduling a deletion (#274): what is refused, and what a schedule does
// to an active workspace.
func TestScheduleWorkspaceDeletion(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	for _, w := range []store.Workspace{
		{Slug: "acme", Name: "Acme", Address: "acme.example.test"},
		{Slug: "platform", Name: "Platform", Address: "platform.example.test", Owner: store.WorkspaceOwnerOperator},
		{Slug: "held", Name: "Held", Address: "held.example.test"},
	} {
		if _, err := st.CreateWorkspace(ctx, w); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.SetWorkspaceProtected(ctx, "held", true); err != nil {
		t.Fatal(err)
	}
	refused := func(slug string, wantStatus int, wantWords string) {
		t.Helper()
		ws, _ := st.Workspace(ctx, slug)
		_, err := ScheduleWorkspaceDeletion(ctx, st, ws, time.Hour, "")
		var ae *apiError
		if !errors.As(err, &ae) || ae.status != wantStatus || !strings.Contains(ae.msg, wantWords) {
			t.Errorf("schedule %s = %v, want %d saying %q", slug, err, wantStatus, wantWords)
		}
	}
	refused(store.DefaultWorkspace, http.StatusBadRequest, "default workspace")
	refused("platform", http.StatusBadRequest, "operator")
	refused("held", http.StatusConflict, "protected")

	ws, _ := st.Workspace(ctx, "acme")
	out, err := ScheduleWorkspaceDeletion(ctx, st, ws, 7*24*time.Hour, "")
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != store.WorkspaceSuspended || out.SuspendReason != store.SuspendReasonOperator || out.DeleteReason != store.DeleteReasonOperator || out.DeleteAfter == nil {
		t.Fatalf("scheduled = %+v", out)
	}
	if until := time.Until(*out.DeleteAfter); until < 7*24*time.Hour-time.Minute || until > 7*24*time.Hour {
		t.Errorf("deleteAfter in %v, want about a week", until)
	}
	// Once gone, no more scheduling.
	if _, err := st.BeginWorkspaceDeletion(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	refused("acme", http.StatusConflict, "already deleting")
}

// An owner schedules the deletion of their own workspace at its door; a
// member cannot; the platform may refuse first.
func TestOwnerDeletesOwnWorkspace(t *testing.T) {
	s, _, st := newTenantServer(t)
	ctx := context.Background()
	for email, role := range map[string]string{"ana@acme.test": store.WorkspaceRoleOwner, "bob@acme.test": store.WorkspaceRoleMember} {
		if _, err := st.TouchIdentity(ctx, "acme", store.Identity{Email: email, Provider: "local"}); err != nil {
			t.Fatal(err)
		}
		if _, err := st.PutMembership(ctx, "acme", email, role); err != nil {
			t.Fatal(err)
		}
	}
	s.authz.Invalidate()
	ana, _ := principalFixtureSession(s, ctx, store.RealmWorkspace, "acme", ext.Identity{Email: "ana@acme.test", Provider: "local"}, "")
	bob, _ := principalFixtureSession(s, ctx, store.RealmWorkspace, "acme", ext.Identity{Email: "bob@acme.test", Provider: "local"}, "")
	del := func(sess *session, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("DELETE", "https://acme.shpyrd.test/api/workspace", strings.NewReader(body))
		req.Host = "acme.shpyrd.test"
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sess.ID})
		req.Header.Set(csrfHeader, sess.CSRF)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec
	}
	if rec := del(bob, ""); rec.Code != http.StatusForbidden {
		t.Errorf("a member deleting the workspace = %d, want 403", rec.Code)
	}
	// The platform's say first (the cloud: cancel the subscription).
	s.opts.WorkspaceDeleteAllowed = func(context.Context, *store.Workspace) error { return errors.New("cancel the subscription first") }
	if rec := del(ana, ""); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "subscription") {
		t.Errorf("refused by the platform = %d %s, want 409", rec.Code, rec.Body.String())
	}
	s.opts.WorkspaceDeleteAllowed = nil
	rec := del(ana, `{"after":"3d"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("owner deleting = %d %s", rec.Code, rec.Body.String())
	}
	ws, _ := st.Workspace(ctx, "acme")
	if ws.Status != store.WorkspaceSuspended || ws.SuspendReason != store.SuspendReasonOwner || ws.DeleteReason != store.DeleteReasonOwner || ws.DeleteAfter == nil {
		t.Fatalf("after the owner's delete = %+v", ws)
	}
	if until := time.Until(*ws.DeleteAfter); until < 3*24*time.Hour-time.Minute || until > 3*24*time.Hour {
		t.Errorf("deleteAfter in %v, want about three days", until)
	}
	// Activating cancels it.
	if w, err := st.ActivateWorkspace(ctx, "acme"); err != nil || w.DeleteAfter != nil || w.Status != store.WorkspaceActive {
		t.Errorf("activate = %+v %v", w, err)
	}
}

// A workspace waiting to be claimed is claimed by the first sign-in.
func TestSignInClaimsWorkspace(t *testing.T) {
	s, _, st := newTenantServer(t)
	ctx := context.Background()
	in := time.Now().Add(time.Hour)
	if _, err := st.CreateWorkspace(ctx, store.Workspace{Slug: "bot", Name: "Bot", Address: "bot.shpyrd.test", Origin: store.OriginAgent, CreatedBy: "agent:planner", ExpiresAt: &in}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.TouchIdentity(ctx, "bot", store.Identity{Email: "ana@acme.test", Provider: "local"}); err != nil {
		t.Fatal(err)
	}
	if _, err := principalFixtureSession(s, ctx, store.RealmWorkspace, "bot", ext.Identity{Email: "ana@acme.test", Provider: "local"}, ""); err != nil {
		t.Fatal(err)
	}
	if ws, _ := st.Workspace(ctx, "bot"); ws.ExpiresAt != nil {
		t.Errorf("after a sign-in the workspace still expires at %v", ws.ExpiresAt)
	}
	// A console sign-in claims nothing.
	in2 := time.Now().Add(time.Hour)
	if _, err := st.CreateWorkspace(ctx, store.Workspace{Slug: "bot2", Name: "Bot 2", Address: "bot2.shpyrd.test", Origin: store.OriginAgent, ExpiresAt: &in2}); err != nil {
		t.Fatal(err)
	}
	if _, err := principalFixtureSession(s, ctx, store.RealmConsole, "", ext.Identity{Email: "maria@example.test", Provider: "local"}, ""); err != nil {
		t.Fatal(err)
	}
	if ws, _ := st.Workspace(ctx, "bot2"); ws.ExpiresAt == nil {
		t.Errorf("a console sign-in claimed bot2")
	}
}

// The unclaimed quota: per creator and in all.
func TestUnclaimedQuota(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	in := time.Now().Add(time.Hour)
	for _, slug := range []string{"a1", "a2", "a3"} {
		if _, err := st.CreateWorkspace(ctx, store.Workspace{Slug: slug, Name: slug, Origin: store.OriginAgent, CreatedBy: "agent:x", ExpiresAt: &in}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.CreateWorkspace(ctx, store.Workspace{Slug: "b1", Name: "b1", Origin: store.OriginAgent, CreatedBy: "agent:y", ExpiresAt: &in}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateWorkspace(ctx, store.Workspace{Slug: "claimed", Name: "claimed", Origin: store.OriginAgent, CreatedBy: "agent:x"}); err != nil {
		t.Fatal(err)
	}
	if err := CheckUnclaimedQuota(ctx, st, "agent:x", UnclaimedQuota{PerCreator: 3, Total: 10}); err == nil || !strings.Contains(err.Error(), "agent:x has 3") {
		t.Errorf("per creator = %v", err)
	}
	if err := CheckUnclaimedQuota(ctx, st, "agent:y", UnclaimedQuota{PerCreator: 3, Total: 10}); err != nil {
		t.Errorf("another creator under the bound = %v", err)
	}
	if err := CheckUnclaimedQuota(ctx, st, "agent:z", UnclaimedQuota{PerCreator: 3, Total: 4}); err == nil || !strings.Contains(err.Error(), "4 workspaces wait") {
		t.Errorf("total = %v", err)
	}
	var ae *apiError
	if err := CheckUnclaimedQuota(ctx, st, "agent:x", DefaultUnclaimedQuota); err != nil {
		t.Errorf("default quota = %v", err)
	} else if errors.As(err, &ae) {
		t.Error("no error, no apiError")
	}
	if exp, err := ParseExpiry("7d"); err != nil || exp == nil || time.Until(*exp) < 7*24*time.Hour-time.Minute {
		t.Errorf("ParseExpiry(7d) = %v %v", exp, err)
	}
	if exp, err := ParseExpiry(""); err != nil || exp != nil {
		t.Errorf("ParseExpiry() = %v %v", exp, err)
	}
	if _, err := ParseExpiry("now"); err == nil {
		t.Error("ParseExpiry(now) accepted")
	}
}

// The orphan report names a project namespace of a workspace that is gone
// and a front door of one, and leaves live things alone.
func TestOrphanReport(t *testing.T) {
	s, cr, _ := newTenantServer(t)
	ctx := context.Background()
	for _, o := range []interface {
		GetName() string
	}{
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: project.NamespaceIn("acme", "shop"), Labels: project.NamespaceLabels("acme", "shop")}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: project.NamespaceIn("ghost", "shop"), Labels: project.NamespaceLabels("ghost", "shop")}},
		&networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: "workspace-acme", Namespace: "shpyrd-system", Labels: map[string]string{shpyrdv1.LabelManagedBy: "shpyrd", "shpyrd.io/workspace-front-door": "true", shpyrdv1.LabelWorkspace: "acme"}}},
		&networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: "workspace-ghost", Namespace: "shpyrd-system", Labels: map[string]string{shpyrdv1.LabelManagedBy: "shpyrd", "shpyrd.io/workspace-front-door": "true", shpyrdv1.LabelWorkspace: "ghost"}}},
	} {
		switch obj := o.(type) {
		case *corev1.Namespace:
			if err := cr.Create(ctx, obj); err != nil {
				t.Fatal(err)
			}
		case *networkingv1.Ingress:
			if err := cr.Create(ctx, obj); err != nil {
				t.Fatal(err)
			}
		}
	}
	rec := at(t, s, "shpyrd.example.test", "GET", "/api/cluster/orphans", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("orphans = %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"name":"app-ghost-shop"`) || !strings.Contains(body, "its workspace is gone") {
		t.Errorf("ghost's namespace missing: %s", body)
	}
	if strings.Contains(body, `"name":"app-acme-shop"`) {
		t.Errorf("acme's live namespace reported: %s", body)
	}
	if !strings.Contains(body, `"workspace-ghost"`) || strings.Contains(body, `"workspace-acme"`) {
		t.Errorf("front doors: %s", body)
	}
	// Not for a workspace's own door.
	if rec := at(t, s, "acme.shpyrd.test", "GET", "/api/cluster/orphans", ""); rec.Code == http.StatusOK {
		t.Errorf("orphans at a workspace host = %d, want refused", rec.Code)
	}
}
