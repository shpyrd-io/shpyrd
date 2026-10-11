package authz

import (
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/store"
	"testing"
)

func TestEmailNeverTransfersPermissions(t *testing.T) {
	snap := Snapshot{Explicit: true, Principals: map[string]store.Identity{
		"owner-id":  {ID: "owner-id", Subject: "owner", Email: "shared@example.test"},
		"other-id":  {ID: "other-id", Subject: "other", Email: "shared@example.test"},
		"legacy-id": {ID: "legacy-id", Email: "legacy@example.test"},
	}, Memberships: []store.Membership{{PrincipalID: "owner-id", Email: "shared@example.test", Role: store.WorkspaceRoleOwner}, {PrincipalID: "legacy-id", Email: "legacy@example.test", Role: store.WorkspaceRoleOwner}}}
	for _, id := range []ext.Identity{{Subject: "other-id", Email: "shared@example.test"}, {Subject: "unseen", Email: "shared@example.test"}, {Subject: "legacy-id", Email: "legacy@example.test"}} {
		if roles := snap.RolesFor(id); roles.Can(WorkspaceOwner, "") || roles.Platform != "" {
			t.Fatalf("email or unbound account acquired permissions: %+v", id)
		}
	}
	if !snap.RolesFor(ext.Identity{Subject: "owner-id", Email: "changed@example.test"}).Can(WorkspaceOwner, "") {
		t.Fatal("email change removed stable ownership")
	}
}
