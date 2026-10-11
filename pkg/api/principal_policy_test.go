package api

import (
	"context"
	"testing"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

func TestExistingPrincipalLosesAccessWhenMethodIsDisabled(t *testing.T) {
	s, _ := newTestServer(t, nil, nil)
	ctx := context.Background()
	id, err := principalFixtureIdentity(s, ctx, store.RealmWorkspace, store.DefaultWorkspace, ext.Identity{Email: "owner@example.test", Provider: "central"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.store.PutMembership(ctx, store.DefaultWorkspace, id.Subject, store.WorkspaceRoleOwner); err != nil {
		t.Fatal(err)
	}
	s.authz.Invalidate()
	ws, err := s.store.Workspace(ctx, store.DefaultWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	roles, err := s.rolesInWorkspace(ctx, ws, id)
	if err != nil || roles.Workspace != "owner" {
		t.Fatalf("owner: %+v %v", roles, err)
	}
	ws.Settings.OwnMethodsOnly = true
	roles, err = s.rolesInWorkspace(ctx, ws, id)
	if err != nil || roles.Workspace != "" {
		t.Fatalf("central identity bypassed company-only policy: %+v %v", roles, err)
	}
	ws.Settings.OwnMethodsOnly = false
	s.rp.RemoveOIDC("central")
	roles, err = s.rolesInWorkspace(ctx, ws, id)
	if err != nil || roles.Workspace != "" {
		t.Fatalf("removed provider retained access: %+v %v", roles, err)
	}
}
