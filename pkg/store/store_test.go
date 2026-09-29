package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shpyrd-io/shpyrd/pkg/ids"
)

// The same behaviour for every implementation. Postgres runs when
// SHPYRD_TEST_DATABASE_URL points at an empty database (CI provides one).
func implementations(t *testing.T) map[string]func(t *testing.T) Store {
	impls := map[string]func(t *testing.T) Store{
		"memory": func(t *testing.T) Store { return NewMemory() },
	}
	if url := os.Getenv("SHPYRD_TEST_DATABASE_URL"); url != "" {
		impls["postgres"] = func(t *testing.T) Store {
			ctx := context.Background()
			p, err := Open(ctx, url)
			if err != nil {
				t.Fatal(err)
			}
			dropAll(t, p) // a clean slate per test
			if err := p.Migrate(ctx, DefaultWorkspaceSpec{Slug: DefaultWorkspace, Name: "test platform", Address: "example.test"}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(p.Close)
			return p
		}
	}
	return impls
}

// dropAll empties the test database: every table the migrations create,
// dependents first.
func dropAll(t *testing.T, p *Postgres) {
	t.Helper()
	ctx := context.Background()
	for _, table := range []string{"projects", "sleep_events", "cogs_buckets", "invoice_lines", "usage_hourly", "usage_buckets", "workspace_plans", "plans", "oauth_tokens", "oauth_codes", "oauth_clients", "workspace_hosts", "invitations", "memberships", "api_tokens", "domain_claims", "edge_codes", "sessions", "grants", "teams", "identities", "workspaces", "schema_migrations"} {
		if _, err := p.pool.Exec(ctx, "DROP TABLE IF EXISTS "+table); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStoreConformance(t *testing.T) {
	for name, open := range implementations(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := open(t)
			if err := s.Migrate(ctx, DefaultWorkspaceSpec{Name: "ignored on second run"}); err != nil {
				t.Fatal(err)
			}

			// The implicit workspace exists and can be renamed.
			w, err := s.Workspace(ctx, DefaultWorkspace)
			if err != nil || w.Slug != DefaultWorkspace || w.Name == "" {
				t.Fatalf("workspace: %+v %v", w, err)
			}
			if _, err := s.Workspace(ctx, "nope"); !errors.Is(err, ErrNotFound) {
				t.Errorf("unknown workspace: %v", err)
			}
			if w, err := s.UpdateWorkspace(ctx, DefaultWorkspace, "Acme"); err != nil || w.Name != "Acme" {
				t.Errorf("rename: %+v %v", w, err)
			}
			if w, err := s.UpdateWorkspaceSettings(ctx, DefaultWorkspace, WorkspaceSettings{JoinPolicy: JoinListed}); err != nil || w.Settings.JoinPolicy != JoinListed {
				t.Errorf("settings: %+v %v", w, err)
			}
			if w, _ := s.Workspace(ctx, DefaultWorkspace); w.Settings.JoinPolicy != JoinListed {
				t.Errorf("settings not persisted: %+v", w)
			}
			// Domain claims: created with a token, verified later, connector updatable.
			claim, err := s.PutDomainClaim(ctx, DefaultWorkspace, "Acme.com", "google")
			if err != nil || claim.Domain != "acme.com" || claim.Token == "" || claim.VerifiedAt != nil {
				t.Fatalf("claim: %+v %v", claim, err)
			}
			again, _ := s.PutDomainClaim(ctx, DefaultWorkspace, "acme.com", "okta")
			if again.Token != claim.Token || again.Connector != "okta" {
				t.Errorf("claim update: %+v", again)
			}
			if v, err := s.MarkDomainVerified(ctx, DefaultWorkspace, "acme.com", time.Now()); err != nil || v.VerifiedAt == nil {
				t.Errorf("verify: %+v %v", v, err)
			}
			if claims, _ := s.ListDomainClaims(ctx, DefaultWorkspace); len(claims) != 1 || claims[0].VerifiedAt == nil {
				t.Errorf("claims = %+v", claims)
			}
			if err := s.DeleteDomainClaim(ctx, DefaultWorkspace, "nope.com"); !errors.Is(err, ErrNotFound) {
				t.Errorf("delete unknown claim: %v", err)
			}

			// Teams: create, update in place, normalised emails.
			team, created, err := s.PutTeam(ctx, DefaultWorkspace, Team{Name: "platform", Description: "ops", Members: []string{" Ops@Example.test ", "ops@example.test", "dev@example.test"}, Groups: []string{"g1", "g1"}, PlatformRole: "platform-admin"})
			if err != nil || !created || team.ID == "" || len(team.Members) != 2 || team.Members[0] != "ops@example.test" || len(team.Groups) != 1 {
				t.Fatalf("put team: %+v created=%v %v", team, created, err)
			}
			team2, created, err := s.PutTeam(ctx, DefaultWorkspace, Team{Name: "platform", Members: []string{"ops@example.test"}})
			if err != nil || created || team2.ID != team.ID || len(team2.Members) != 1 || team2.PlatformRole != "" {
				t.Fatalf("update team: %+v created=%v %v", team2, created, err)
			}
			if _, _, err := s.PutTeam(ctx, DefaultWorkspace, Team{Name: "finance", Members: []string{"fin@example.test"}}); err != nil {
				t.Fatal(err)
			}
			teams, err := s.ListTeams(ctx, DefaultWorkspace)
			if err != nil || len(teams) != 3 || teams[0].Name != "everyone" || !teams[0].Everyone || teams[1].Name != "finance" {
				t.Fatalf("list teams: %+v %v", teams, err)
			}
			// The built-in team is neither editable nor deletable, but grantable.
			if _, _, err := s.PutTeam(ctx, DefaultWorkspace, Team{Name: TeamEveryone, Members: []string{"x@example.test"}}); !errors.Is(err, ErrBuiltIn) {
				t.Errorf("put everyone: %v", err)
			}
			if err := s.DeleteTeam(ctx, DefaultWorkspace, TeamEveryone); !errors.Is(err, ErrBuiltIn) {
				t.Errorf("delete everyone: %v", err)
			}
			if _, err := s.AddGrant(ctx, DefaultWorkspace, Grant{Project: "intranet", Role: "user", Team: TeamEveryone}); err != nil {
				t.Errorf("grant everyone: %v", err)
			}
			_ = s.DeleteProjectGrants(ctx, DefaultWorkspace, "intranet")
			if _, err := s.GetTeam(ctx, DefaultWorkspace, "nope"); !errors.Is(err, ErrNotFound) {
				t.Errorf("get unknown team: %v", err)
			}

			// Grants: by user and by team, conflicts, unknown team.
			g1, err := s.AddGrant(ctx, DefaultWorkspace, Grant{Project: "shop", Role: "developer", User: "Dev@Example.test"})
			if err != nil || g1.User != "dev@example.test" || g1.ID == "" {
				t.Fatalf("add grant: %+v %v", g1, err)
			}
			if _, err := s.AddGrant(ctx, DefaultWorkspace, Grant{Project: "shop", Role: "developer", User: "dev@example.test"}); !errors.Is(err, ErrConflict) {
				t.Errorf("duplicate grant: %v", err)
			}
			if _, err := s.AddGrant(ctx, DefaultWorkspace, Grant{Project: "shop", Role: "admin", Team: "nope"}); !errors.Is(err, ErrNotFound) {
				t.Errorf("unknown team grant: %v", err)
			}
			g2, err := s.AddGrant(ctx, DefaultWorkspace, Grant{Project: "shop", Role: "admin", Team: "platform"})
			if err != nil || g2.Team != "platform" {
				t.Fatalf("team grant: %+v %v", g2, err)
			}
			if _, err := s.AddGrant(ctx, DefaultWorkspace, Grant{Project: "blog", Role: "viewer", Team: "finance"}); err != nil {
				t.Fatal(err)
			}
			all, _ := s.ListGrants(ctx, DefaultWorkspace)
			shop, _ := s.ListProjectGrants(ctx, DefaultWorkspace, "shop")
			if len(all) != 3 || len(shop) != 2 || all[0].Project != "blog" {
				t.Errorf("grants: all=%d shop=%d first=%s", len(all), len(shop), all[0].Project)
			}

			// Deleting a team removes its grants; deleting a grant by id.
			if err := s.DeleteTeam(ctx, DefaultWorkspace, "finance"); err != nil {
				t.Fatal(err)
			}
			all, _ = s.ListGrants(ctx, DefaultWorkspace)
			if len(all) != 2 {
				t.Errorf("grants after team delete = %d", len(all))
			}
			if err := s.DeleteGrant(ctx, DefaultWorkspace, g1.ID); err != nil {
				t.Fatal(err)
			}
			if err := s.DeleteGrant(ctx, DefaultWorkspace, g1.ID); !errors.Is(err, ErrNotFound) {
				t.Errorf("delete twice: %v", err)
			}
			if err := s.DeleteTeam(ctx, DefaultWorkspace, "nope"); !errors.Is(err, ErrNotFound) {
				t.Errorf("delete unknown team: %v", err)
			}
			if err := s.DeleteProjectGrants(ctx, DefaultWorkspace, "shop"); err != nil {
				t.Fatal(err)
			}
			if all, _ = s.ListGrants(ctx, DefaultWorkspace); len(all) != 0 {
				t.Errorf("grants after project delete = %d", len(all))
			}

			// Identities: first sight creates, later sights update.
			id1, err := s.TouchIdentity(ctx, DefaultWorkspace, Identity{Email: "Maria@Example.test", Name: "Maria", Provider: "google", Groups: []string{"finance"}})
			if err != nil || id1.Email != "maria@example.test" || id1.Realm != "workspace" || len(id1.Groups) != 1 {
				t.Fatalf("touch: %+v %v", id1, err)
			}
			time.Sleep(5 * time.Millisecond)
			id2, err := s.TouchIdentity(ctx, DefaultWorkspace, Identity{Email: "maria@example.test", Provider: "okta"})
			if err != nil || id2.ID != id1.ID || id2.Name != "Maria" || id2.Provider != "okta" || len(id2.Groups) != 1 || !id2.LastSeenAt.After(id1.FirstSeenAt) {
				t.Fatalf("touch again: %+v %v", id2, err)
			}
			ids, _ := s.ListIdentities(ctx, DefaultWorkspace)
			if len(ids) != 1 || ids[0].Status != StatusActive {
				t.Errorf("identities = %+v", ids)
			}
			if sus, err := s.SetIdentityStatus(ctx, DefaultWorkspace, "maria@example.test", StatusSuspended); err != nil || sus.Status != StatusSuspended {
				t.Errorf("suspend: %v %+v", err, sus)
			}
			if _, err := s.SetIdentityStatus(ctx, DefaultWorkspace, "nobody@example.test", StatusSuspended); !errors.Is(err, ErrNotFound) {
				t.Errorf("suspend unknown: %v", err)
			}
			if _, err := s.SetIdentityStatus(ctx, DefaultWorkspace, "maria@example.test", StatusActive); err != nil {
				t.Fatal(err)
			}

			// Export / import round trip into a fresh store.
			if _, _, err := s.PutTeam(ctx, DefaultWorkspace, Team{Name: "finance", Members: []string{"fin@example.test"}}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.AddGrant(ctx, DefaultWorkspace, Grant{Project: "blog", Role: "user", Team: "finance"}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.PutMembership(ctx, DefaultWorkspace, "Owner@example.test", WorkspaceRoleOwner); err != nil {
				t.Fatal(err)
			}
			dump, err := s.Export(ctx, DefaultWorkspace)
			if err != nil || dump.Version != DumpVersion || len(dump.Teams) != 3 || len(dump.Grants) != 1 || len(dump.Identities) != 1 || len(dump.Domains) != 1 || len(dump.Memberships) != 1 {
				t.Fatalf("export: %+v %v", dump, err)
			}
			fresh := NewMemory()
			res, err := fresh.Import(ctx, DefaultWorkspace, dump, false)
			if err != nil || res.Teams != 2 || res.Grants != 1 || res.Identities != 1 || res.Memberships != 1 {
				t.Fatalf("import: %+v %v", res, err)
			}
			if claims, _ := fresh.ListDomainClaims(ctx, DefaultWorkspace); len(claims) != 1 || claims[0].VerifiedAt == nil {
				t.Errorf("imported claims = %+v", claims)
			}
			if roles, _ := fresh.ListMemberships(ctx, DefaultWorkspace); len(roles) != 1 || roles[0].Email != "owner@example.test" || roles[0].Role != WorkspaceRoleOwner {
				t.Errorf("imported memberships = %+v", roles)
			}
			res, err = fresh.Import(ctx, DefaultWorkspace, dump, false)
			if err != nil || res.Teams != 0 || res.Memberships != 0 || res.Skipped != 4 {
				t.Errorf("import again without overwrite: %+v %v", res, err)
			}
			if err := s.DeleteIdentity(ctx, DefaultWorkspace, "maria@example.test"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMembershipsAndInvitations(t *testing.T) {
	for name, open := range implementations(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := open(t)
			ws := DefaultWorkspace

			// Roles: upsert by email, case-insensitive, validated.
			if _, err := s.PutMembership(ctx, ws, "ada@example.test", "king"); err == nil {
				t.Error("an unknown role was accepted")
			}
			if _, err := s.PutMembership(ctx, "nope", "ada@example.test", WorkspaceRoleOwner); !errors.Is(err, ErrNotFound) {
				t.Errorf("unknown workspace: %v", err)
			}
			mb, err := s.PutMembership(ctx, ws, " Ada@Example.test ", WorkspaceRoleOwner)
			if err != nil || mb.Email != "ada@example.test" || mb.Role != WorkspaceRoleOwner || mb.ID == "" {
				t.Fatalf("put: %+v %v", mb, err)
			}
			again, err := s.PutMembership(ctx, ws, "ada@example.test", WorkspaceRoleMember)
			if err != nil || again.ID != mb.ID || again.Role != WorkspaceRoleMember {
				t.Fatalf("update: %+v %v", again, err)
			}
			if _, err := s.PutMembership(ctx, ws, "bob@example.test", WorkspaceRoleAdmin); err != nil {
				t.Fatal(err)
			}
			roles, err := s.ListMemberships(ctx, ws)
			if err != nil || len(roles) != 2 || roles[0].Email != "ada@example.test" || roles[1].Role != WorkspaceRoleAdmin {
				t.Fatalf("list: %+v %v", roles, err)
			}
			if err := s.DeleteMembership(ctx, ws, "ADA@example.test"); err != nil {
				t.Fatal(err)
			}
			if err := s.DeleteMembership(ctx, ws, "ada@example.test"); !errors.Is(err, ErrNotFound) {
				t.Errorf("delete again: %v", err)
			}
			if roles, _ := s.ListMemberships(ctx, ws); len(roles) != 1 {
				t.Errorf("after delete: %+v", roles)
			}

			// Invitations: one per email, replaced by a re-invite, found by
			// the token's hash, team optional and checked.
			if _, err := s.CreateInvitation(ctx, ws, Invitation{Email: "eve@example.test", Role: WorkspaceRoleMember, Team: "ghosts"}, "h1"); !errors.Is(err, ErrNotFound) {
				t.Errorf("unknown team: %v", err)
			}
			if _, _, err := s.PutTeam(ctx, ws, Team{Name: "dev"}); err != nil {
				t.Fatal(err)
			}
			inv, err := s.CreateInvitation(ctx, ws, Invitation{Email: "Eve@example.test", Role: WorkspaceRoleMember, Team: "dev", InvitedBy: "bob@example.test"}, "h1")
			if err != nil || inv.Email != "eve@example.test" || inv.Team != "dev" || inv.ID == "" || inv.ExpiresAt.IsZero() || inv.Expired(time.Now()) {
				t.Fatalf("create: %+v %v", inv, err)
			}
			if got, err := s.InvitationByToken(ctx, "h1"); err != nil || got.ID != inv.ID || got.InvitedBy != "bob@example.test" {
				t.Fatalf("by token: %+v %v", got, err)
			}
			if _, err := s.InvitationByToken(ctx, "nope"); !errors.Is(err, ErrNotFound) {
				t.Errorf("unknown token: %v", err)
			}
			replaced, err := s.CreateInvitation(ctx, ws, Invitation{Email: "eve@example.test", Role: WorkspaceRoleAdmin, ExpiresAt: time.Now().Add(-time.Hour)}, "h2")
			if err != nil || replaced.ID == inv.ID || replaced.Role != WorkspaceRoleAdmin || !replaced.Expired(time.Now()) {
				t.Fatalf("re-invite: %+v %v", replaced, err)
			}
			if _, err := s.InvitationByToken(ctx, "h1"); !errors.Is(err, ErrNotFound) {
				t.Errorf("old token still works: %v", err)
			}
			if _, err := s.CreateInvitation(ctx, ws, Invitation{Email: "fin@example.test", Role: WorkspaceRoleMember, Team: "dev"}, "h3"); err != nil {
				t.Fatal(err)
			}
			list, err := s.ListInvitations(ctx, ws)
			if err != nil || len(list) != 2 || list[0].Email != "eve@example.test" || list[1].Team != "dev" {
				t.Fatalf("list: %+v %v", list, err)
			}
			// Deleting the team leaves the invitation, without the team.
			if err := s.DeleteTeam(ctx, ws, "dev"); err != nil {
				t.Fatal(err)
			}
			if list, _ := s.ListInvitations(ctx, ws); len(list) != 2 || list[1].Team != "" {
				t.Errorf("after team delete: %+v", list)
			}
			if err := s.DeleteInvitation(ctx, ws, replaced.ID); err != nil {
				t.Fatal(err)
			}
			if err := s.DeleteInvitation(ctx, ws, replaced.ID); !errors.Is(err, ErrNotFound) {
				t.Errorf("delete again: %v", err)
			}
			if list, _ := s.ListInvitations(ctx, ws); len(list) != 1 {
				t.Errorf("after delete: %+v", list)
			}
		})
	}
}

// Workspace hosts (RFC-0033 names): custom domains and moved addresses,
// unique across the platform, one primary at most; the address changes.
func TestWorkspaceHosts(t *testing.T) {
	for name, open := range implementations(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := open(t)
			acme, err := s.CreateWorkspace(ctx, Workspace{Slug: "acme", Name: "Acme", Address: "acme.shpyrd.test"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.CreateWorkspace(ctx, Workspace{Slug: "beta", Name: "Beta", Address: "beta.shpyrd.test"}); err != nil {
				t.Fatal(err)
			}
			h, err := s.PutWorkspaceHost(ctx, "acme", WorkspaceHost{Host: "Intranet.Acme.com.", Kind: HostCustom})
			if err != nil || h.Host != "intranet.acme.com" || h.Token == "" || h.VerifiedAt != nil || h.Primary || h.WorkspaceID != acme.ID {
				t.Fatalf("put: %+v %v", h, err)
			}
			if _, err := s.PutWorkspaceHost(ctx, "acme", WorkspaceHost{Host: "x", Kind: "weird"}); err == nil {
				t.Error("bad kind accepted")
			}
			// Another workspace's address, or a host of another workspace, is taken.
			if _, err := s.PutWorkspaceHost(ctx, "acme", WorkspaceHost{Host: "beta.shpyrd.test", Kind: HostCustom}); !errors.Is(err, ErrConflict) {
				t.Errorf("another address: %v", err)
			}
			if _, err := s.PutWorkspaceHost(ctx, "beta", WorkspaceHost{Host: "intranet.acme.com", Kind: HostCustom}); !errors.Is(err, ErrConflict) {
				t.Errorf("another's host: %v", err)
			}
			if _, err := s.PutWorkspaceHost(ctx, "acme", WorkspaceHost{Host: "acme.shpyrd.test", Kind: HostCustom}); !errors.Is(err, ErrConflict) {
				t.Errorf("own address as host: %v", err)
			}
			// Verify and make primary; the token stays; a second primary clears the first.
			now := time.Now().Truncate(time.Second)
			v, err := s.PutWorkspaceHost(ctx, "acme", WorkspaceHost{Host: "intranet.acme.com", Kind: HostCustom, Primary: true, VerifiedAt: &now})
			if err != nil || v.ID != h.ID || v.Token != h.Token || v.VerifiedAt == nil || !v.Primary {
				t.Fatalf("update: %+v %v", v, err)
			}
			if _, err := s.PutWorkspaceHost(ctx, "acme", WorkspaceHost{Host: "apps.acme.com", Kind: HostCustom, Primary: true, VerifiedAt: &now}); err != nil {
				t.Fatal(err)
			}
			list, _ := s.ListWorkspaceHosts(ctx, "acme")
			if len(list) != 2 || list[0].Host != "apps.acme.com" || !list[0].Primary || list[1].Primary {
				t.Errorf("list: %+v", list)
			}
			w, rec, err := s.WorkspaceByHost(ctx, "INTRANET.acme.com")
			if err != nil || w.Slug != "acme" || rec.Host != "intranet.acme.com" {
				t.Errorf("by host: %+v %+v %v", w, rec, err)
			}
			if _, _, err := s.WorkspaceByHost(ctx, "nobody.example"); !errors.Is(err, ErrNotFound) {
				t.Errorf("unknown host: %v", err)
			}
			// The address changes; the old one may become a moved host; taken ones are refused.
			if _, err := s.UpdateWorkspaceAddress(ctx, "acme", "beta.shpyrd.test"); !errors.Is(err, ErrConflict) {
				t.Errorf("address of another: %v", err)
			}
			if _, err := s.UpdateWorkspaceAddress(ctx, "acme", "apps.acme.com"); !errors.Is(err, ErrConflict) {
				t.Errorf("address equal to a host: %v", err)
			}
			moved, err := s.UpdateWorkspaceAddress(ctx, "acme", "Acme-Corp.shpyrd.test")
			if err != nil || moved.Address != "acme-corp.shpyrd.test" {
				t.Fatalf("move: %+v %v", moved, err)
			}
			exp := now.Add(30 * 24 * time.Hour)
			if _, err := s.PutWorkspaceHost(ctx, "acme", WorkspaceHost{Host: "acme.shpyrd.test", Kind: HostMoved, ExpiresAt: &exp}); err != nil {
				t.Fatalf("moved host: %v", err)
			}
			if _, err := s.CreateWorkspace(ctx, Workspace{Slug: "gamma", Name: "G", Address: "acme.shpyrd.test"}); err == nil {
				t.Error("a moved host must not become another workspace's address")
			}
			// Export carries the hosts; import restores custom ones, skips moved.
			dump, _ := s.Export(ctx, "acme")
			if len(dump.Hosts) != 3 || dump.Version != DumpVersion {
				t.Errorf("export hosts: %+v", dump.Hosts)
			}
			fresh := NewMemory()
			if _, err := fresh.CreateWorkspace(ctx, Workspace{Slug: "acme", Name: "Acme", Address: "acme-corp.shpyrd.test"}); err != nil {
				t.Fatal(err)
			}
			res, err := fresh.Import(ctx, "acme", dump, false)
			if err != nil || res.Hosts != 2 {
				t.Errorf("import hosts: %+v %v", res, err)
			}
			if err := s.DeleteWorkspaceHost(ctx, "acme", "apps.acme.com"); err != nil {
				t.Fatal(err)
			}
			if err := s.DeleteWorkspaceHost(ctx, "acme", "apps.acme.com"); !errors.Is(err, ErrNotFound) {
				t.Errorf("delete again: %v", err)
			}
		})
	}
}

// The OAuth 2.1 server's records (RFC-0032): clients, one-use codes,
// refresh tokens that rotate and expire.
func TestOAuthRecords(t *testing.T) {
	for name, open := range implementations(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := open(t)
			ws := DefaultWorkspace
			w, _ := s.Workspace(ctx, ws)
			c, err := s.CreateOAuthClient(ctx, ws, OAuthClient{ClientID: "cl_1", Name: "Claude", RedirectURIs: []string{"https://claude.ai/cb"}})
			if err != nil || c.ID == "" || c.WorkspaceID != w.ID {
				t.Fatalf("client: %+v %v", c, err)
			}
			if _, err := s.CreateOAuthClient(ctx, ws, OAuthClient{ClientID: "cl_1"}); !errors.Is(err, ErrConflict) {
				t.Errorf("duplicate client: %v", err)
			}
			if got, err := s.OAuthClientByID(ctx, "cl_1"); err != nil || got.Name != "Claude" || len(got.RedirectURIs) != 1 {
				t.Errorf("by id: %+v %v", got, err)
			}
			if _, err := s.OAuthClientByID(ctx, "nope"); !errors.Is(err, ErrNotFound) {
				t.Errorf("unknown client: %v", err)
			}
			// Codes: taken once.
			if err := s.PutOAuthCode(ctx, OAuthCode{Hash: "h1", WorkspaceID: w.ID, ClientID: "cl_1", Email: "Ada@x.test", RedirectURI: "https://claude.ai/cb", CodeChallenge: "ch", Scope: "projects:read", ExpiresAt: time.Now().Add(time.Minute)}); err != nil {
				t.Fatal(err)
			}
			code, err := s.TakeOAuthCode(ctx, "h1")
			if err != nil || code.Email != "ada@x.test" || code.ClientID != "cl_1" || code.CodeChallenge != "ch" {
				t.Fatalf("take: %+v %v", code, err)
			}
			if _, err := s.TakeOAuthCode(ctx, "h1"); !errors.Is(err, ErrNotFound) {
				t.Errorf("code twice: %v", err)
			}
			_ = s.PutOAuthCode(ctx, OAuthCode{Hash: "h2", WorkspaceID: w.ID, ClientID: "cl_1", Email: "a@x.test", RedirectURI: "r", CodeChallenge: "c", ExpiresAt: time.Now().Add(-time.Second)})
			if _, err := s.TakeOAuthCode(ctx, "h2"); !errors.Is(err, ErrNotFound) {
				t.Errorf("expired code: %v", err)
			}
			// Refresh tokens: found by hash while live, rotated, listed per person, deleted.
			tok, err := s.CreateOAuthToken(ctx, ws, OAuthToken{ClientID: "cl_1", Email: "Ada@x.test", Scope: "projects:read", Hash: "r1", ExpiresAt: time.Now().Add(time.Hour)})
			if err != nil || tok.ID == "" || tok.Email != "ada@x.test" {
				t.Fatalf("token: %+v %v", tok, err)
			}
			if got, err := s.OAuthTokenByHash(ctx, "r1"); err != nil || got.ID != tok.ID || got.ClientName != "Claude" {
				t.Errorf("by hash: %+v %v", got, err)
			}
			if err := s.RotateOAuthToken(ctx, tok.ID, "r2", time.Now().Add(2*time.Hour)); err != nil {
				t.Fatal(err)
			}
			if _, err := s.OAuthTokenByHash(ctx, "r1"); !errors.Is(err, ErrNotFound) {
				t.Errorf("old hash after rotation: %v", err)
			}
			if got, err := s.OAuthTokenByHash(ctx, "r2"); err != nil || got.LastUsedAt == nil {
				t.Errorf("rotated: %+v %v", got, err)
			}
			if _, err := s.CreateOAuthToken(ctx, ws, OAuthToken{ClientID: "cl_1", Email: "bob@x.test", Hash: "r3", ExpiresAt: time.Now().Add(-time.Minute)}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.OAuthTokenByHash(ctx, "r3"); !errors.Is(err, ErrNotFound) {
				t.Errorf("expired token: %v", err)
			}
			if list, _ := s.ListOAuthTokens(ctx, ws, "ADA@x.test"); len(list) != 1 || list[0].ClientName != "Claude" {
				t.Errorf("list: %+v", list)
			}
			if all, _ := s.ListOAuthTokens(ctx, ws, ""); len(all) != 2 {
				t.Errorf("list all: %+v", all)
			}
			if err := s.DeleteOAuthToken(ctx, ws, tok.ID); err != nil {
				t.Fatal(err)
			}
			if err := s.DeleteOAuthToken(ctx, ws, tok.ID); !errors.Is(err, ErrNotFound) {
				t.Errorf("delete twice: %v", err)
			}
		})
	}
}

func TestBillingStore(t *testing.T) {
	for name, open := range implementations(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := open(t)
			// Plans: create, list, get, assign to workspace, history.
			if _, err := s.CreatePlan(ctx, Plan{Name: "starter", CPUHour: 0.02, MemoryGiBHour: 0.003, StorageGiBMonth: 0.10, MinMonthly: 5.0, SleepAfter: "15m0s", SleepResuming: "page"}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.CreatePlan(ctx, Plan{Name: "starter"}); !errors.Is(err, ErrConflict) {
				t.Error("duplicate plan: want conflict")
			}
			plans, _ := s.ListPlans(ctx)
			if len(plans) != 1 || plans[0].Name != "starter" {
				t.Fatalf("list plans: %+v", plans)
			}
			pl, err := s.GetPlan(ctx, "starter")
			if err != nil || pl.CPUHour != 0.02 || pl.SleepAfter != "15m0s" || pl.SleepResuming != "page" {
				t.Fatalf("get plan: %+v %v", pl, err)
			}
			wp, err := s.AssignPlan(ctx, DefaultWorkspace, "starter")
			if err != nil || wp.PlanName != "starter" {
				t.Fatalf("assign plan: %+v %v", wp, err)
			}
			cur, err := s.WorkspacePlan(ctx, DefaultWorkspace)
			if err != nil || cur.PlanName != "starter" {
				t.Fatalf("current plan: %+v %v", cur, err)
			}
			// Assign again replaces.
			if _, err := s.CreatePlan(ctx, Plan{Name: "grow", CPUHour: 0.015}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.AssignPlan(ctx, DefaultWorkspace, "grow"); err != nil {
				t.Fatal(err)
			}
			hist, _ := s.WorkspacePlanHistory(ctx, DefaultWorkspace)
			if len(hist) != 2 {
				t.Errorf("history: %+v", hist)
			}
			// Usage buckets: write + query (dedup on conflict). The metering
			// loop writes with the workspace slug (off the namespace label);
			// the store resolves it to the id.
			now := time.Now().UTC().Truncate(5 * time.Minute)
			qty := 300.0
			buckets := []UsageBucket{
				{WorkspaceID: DefaultWorkspace, Project: "shop", Component: "web", Metric: MetricCPUUsed, PeriodStart: now, PeriodEnd: now.Add(5 * time.Minute), Quantity: &qty, Unit: UnitCoreSeconds, Quality: QualityComplete, Revision: 1, Source: "prom_v1"},
				{WorkspaceID: "no-such-workspace", Project: "ghost", Component: "web", Metric: MetricCPUUsed, PeriodStart: now, PeriodEnd: now.Add(5 * time.Minute), Quantity: &qty, Unit: UnitCoreSeconds, Quality: QualityComplete, Revision: 1, Source: "prom_v1"},
			}
			ws, _ := s.Workspace(ctx, DefaultWorkspace)
			if err := s.WriteBuckets(ctx, buckets); err != nil {
				t.Fatal(err)
			}
			// Writing by id is accepted too, and is idempotent.
			buckets[0].WorkspaceID = ws.ID
			if err := s.WriteBuckets(ctx, buckets[:1]); err != nil {
				t.Fatal(err)
			}
			got, err := s.QueryBuckets(ctx, DefaultWorkspace, "", now.Add(-time.Minute), now.Add(10*time.Minute))
			if err != nil || len(got) != 1 || *got[0].Quantity != 300.0 || got[0].WorkspaceID != ws.ID {
				t.Errorf("query buckets: %+v %v", got, err)
			}

			// Rollup: an old hour with 12 complete buckets and one with 3
			// buckets (one missing) folds into usage_hourly; recent rows stay.
			old := now.Add(-72 * time.Hour).Truncate(time.Hour)
			var olds []UsageBucket
			for i := 0; i < 12; i++ {
				q := 60.0
				olds = append(olds, UsageBucket{WorkspaceID: DefaultWorkspace, Project: "shop", Component: "web", Metric: MetricCPUUsed,
					PeriodStart: old.Add(time.Duration(i) * 5 * time.Minute), PeriodEnd: old.Add(time.Duration(i+1) * 5 * time.Minute),
					Quantity: &q, Unit: UnitCoreSeconds, Quality: QualityComplete, Revision: 1, Source: "prom_v1"})
			}
			gap := old.Add(time.Hour)
			for i := 0; i < 3; i++ {
				var q *float64
				if i != 1 {
					v := 10.0
					q = &v
				}
				qual := QualityComplete
				if q == nil {
					qual = QualityMissing
				}
				olds = append(olds, UsageBucket{WorkspaceID: DefaultWorkspace, Project: "shop", Component: "web", Metric: MetricMemoryUsed,
					PeriodStart: gap.Add(time.Duration(i) * 5 * time.Minute), PeriodEnd: gap.Add(time.Duration(i+1) * 5 * time.Minute),
					Quantity: q, Unit: UnitGiBSeconds, Quality: qual, Revision: 1, Source: "prom_v1"})
			}
			if err := s.WriteBuckets(ctx, olds); err != nil {
				t.Fatal(err)
			}
			n, err := s.RollupHourly(ctx, now.Add(-48*time.Hour))
			if err != nil || n != 2 {
				t.Fatalf("rollup: hours=%d err=%v", n, err)
			}
			if n, err := s.RollupHourly(ctx, now.Add(-48*time.Hour)); err != nil || n != 0 {
				t.Errorf("rollup is not idempotent: hours=%d err=%v", n, err)
			}
			hourly, err := s.QueryBuckets(ctx, DefaultWorkspace, "shop", old.Add(-time.Minute), gap.Add(2*time.Hour))
			if err != nil || len(hourly) != 2 {
				t.Fatalf("hourly rows: %+v %v", hourly, err)
			}
			for _, h := range hourly {
				switch h.Metric {
				case MetricCPUUsed:
					if h.Quantity == nil || *h.Quantity != 720 || h.Quality != QualityComplete || !h.PeriodEnd.Equal(old.Add(time.Hour)) {
						t.Errorf("cpu hour: %+v", h)
					}
				case MetricMemoryUsed:
					if h.Quantity != nil || h.Quality != QualityMissing {
						t.Errorf("memory hour with a gap: %+v", h)
					}
				}
			}
			// The recent bucket is untouched.
			if recent, _ := s.QueryBuckets(ctx, DefaultWorkspace, "", now.Add(-time.Minute), now.Add(10*time.Minute)); len(recent) != 1 {
				t.Errorf("recent buckets after rollup: %+v", recent)
			}
			// Invoice lines: upsert.
			line := InvoiceLine{WorkspaceID: ws.ID, PeriodStart: now, PeriodEnd: now.Add(time.Hour), Component: "web", Metric: MetricCPUUsed, Quantity: 3600, Unit: UnitCoreSeconds, UnitPrice: 0.02, GrossAmount: 0.02, Quality: QualityComplete, Revision: 1}
			if err := s.UpsertInvoiceLine(ctx, line); err != nil {
				t.Fatal(err)
			}
			lines, _ := s.QueryInvoiceLines(ctx, DefaultWorkspace, now.Add(-time.Minute), now.Add(2*time.Hour), nil)
			if len(lines) != 1 || lines[0].GrossAmount != 0.02 {
				t.Errorf("invoice lines: %+v", lines)
			}
			// COGS bucket.
			cogs := COGSBucket{WorkspaceID: ws.ID, Project: "shop", PeriodStart: now, PeriodEnd: now.Add(time.Hour), CPUCost: 0.005, TotalCost: 0.005, Currency: "USD", Quality: QualityComplete}
			if err := s.WriteCOGSBucket(ctx, cogs); err != nil {
				t.Fatal(err)
			}
			cb, _ := s.QueryCOGSBuckets(ctx, DefaultWorkspace, now.Add(-time.Minute), now.Add(2*time.Hour))
			if len(cb) != 1 || cb[0].CPUCost != 0.005 {
				t.Errorf("cogs: %+v", cb)
			}
			// Sleep events.
			dur := 42
			ev := SleepEvent{WorkspaceID: ws.ID, Project: "shop", Component: "web", Event: "wake", At: now, DurationSeconds: &dur}
			if err := s.WriteSleepEvent(ctx, ev); err != nil {
				t.Fatal(err)
			}
			evs, _ := s.QuerySleepEvents(ctx, DefaultWorkspace, "shop", now.Add(-time.Minute), now.Add(time.Minute))
			if len(evs) != 1 || *evs[0].DurationSeconds != 42 {
				t.Errorf("sleep events: %+v", evs)
			}
		})
	}
}

func TestSessionsAndCodes(t *testing.T) {
	for name, open := range implementations(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := open(t)
			now := time.Now().UTC().Truncate(time.Millisecond)
			sess := Session{ID: "sid-1", Identity: json.RawMessage(`{"email":"maria@acme.test"}`), CSRF: "c1", CreatedAt: now, LastSeenAt: now}
			if err := s.PutSession(ctx, DefaultWorkspace, sess); err != nil {
				t.Fatal(err)
			}
			got, err := s.GetSession(ctx, "sid-1")
			if err != nil || got.CSRF != "c1" || got.WorkspaceID == "" || got.Realm != RealmWorkspace || !strings.Contains(string(got.Identity), "maria") {
				t.Fatalf("get: %v %+v", err, got)
			}
			// A console session has no workspace (RFC-0080).
			if err := s.PutSession(ctx, "", Session{ID: "sid-console", Identity: json.RawMessage(`{"email":"op@shpyrd.test"}`), CSRF: "c2", CreatedAt: now, LastSeenAt: now}); err != nil {
				t.Fatal(err)
			}
			if got, err := s.GetSession(ctx, "sid-console"); err != nil || got.Realm != RealmConsole || got.WorkspaceID != "" {
				t.Fatalf("console session: %v %+v", err, got)
			}
			if err := s.TouchSession(ctx, "sid-1", now.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			got, _ = s.GetSession(ctx, "sid-1")
			if !got.LastSeenAt.After(now) {
				t.Errorf("touch did not move last_seen: %v", got.LastSeenAt)
			}
			if err := s.TouchSession(ctx, "nope", now); !errors.Is(err, ErrNotFound) {
				t.Errorf("touch unknown: %v", err)
			}
			if n, _ := s.CountSessions(ctx, DefaultWorkspace); n != 1 {
				t.Errorf("count = %d", n)
			}
			// Purge by idle time and by age.
			_ = s.PutSession(ctx, DefaultWorkspace, Session{ID: "old", Identity: json.RawMessage(`{}`), CSRF: "c", CreatedAt: now.Add(-48 * time.Hour), LastSeenAt: now.Add(-2 * time.Hour)})
			n, err := s.PurgeSessions(ctx, now.Add(-24*time.Hour), now.Add(-12*time.Hour))
			if err != nil || n != 1 {
				t.Errorf("purge = %d %v", n, err)
			}
			if err := s.DeleteSession(ctx, "sid-1"); err != nil {
				t.Fatal(err)
			}
			if _, err := s.GetSession(ctx, "sid-1"); !errors.Is(err, ErrNotFound) {
				t.Errorf("deleted session: %v", err)
			}

			// Codes: once, and not after expiry.
			if err := s.PutCode(ctx, Code{Code: "k1", Host: "app.acme.test", Claims: json.RawMessage(`{"sid":"sid-1"}`), ExpiresAt: now.Add(time.Minute)}); err != nil {
				t.Fatal(err)
			}
			c, err := s.TakeCode(ctx, "k1")
			if err != nil || c.Host != "app.acme.test" || !strings.Contains(string(c.Claims), "sid-1") {
				t.Fatalf("take: %v %+v", err, c)
			}
			if _, err := s.TakeCode(ctx, "k1"); !errors.Is(err, ErrNotFound) {
				t.Errorf("second take: %v", err)
			}
			_ = s.PutCode(ctx, Code{Code: "k2", Host: "h", Claims: json.RawMessage(`{}`), ExpiresAt: now.Add(-time.Second)})
			if _, err := s.TakeCode(ctx, "k2"); !errors.Is(err, ErrNotFound) {
				t.Errorf("expired take: %v", err)
			}
		})
	}
}

func TestTokens(t *testing.T) {
	for name, open := range implementations(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := open(t)
			tok, err := s.CreateToken(ctx, DefaultWorkspace, APIToken{
				Name: "ci", OwnerEmail: "joao@acme.test", ProjectRoles: map[string]string{"shop": "developer"},
			}, "hash-1")
			if err != nil || tok.ID == "" || tok.WorkspaceID == "" || tok.CreatedAt.IsZero() {
				t.Fatalf("create: %+v %v", tok, err)
			}
			if _, err := s.CreateToken(ctx, DefaultWorkspace, APIToken{Name: "ci", OwnerEmail: "Joao@acme.test"}, "hash-2"); !errors.Is(err, ErrConflict) {
				t.Errorf("duplicate name for the same owner: %v", err)
			}
			// Names are per person: someone else may also call theirs "ci".
			if _, err := s.CreateToken(ctx, DefaultWorkspace, APIToken{Name: "ci", OwnerEmail: "ana@acme.test"}, "hash-3"); err != nil {
				t.Errorf("same name, other owner: %v", err)
			}
			// Lookup by hash works, records first use, and misses on unknown hashes.
			got, err := s.LookupToken(ctx, "hash-1")
			if err != nil || got == nil || got.ID != tok.ID || got.ProjectRoles["shop"] != "developer" {
				t.Fatalf("lookup: %+v %v", got, err)
			}
			if got.LastUsedAt == nil {
				t.Errorf("first use did not record last_used_at")
			}
			// The write must be visible to a pure read, not only on the returned struct.
			if listed, _ := s.ListTokens(ctx, DefaultWorkspace, "joao@acme.test"); len(listed) != 1 || listed[0].LastUsedAt == nil {
				t.Errorf("last_used_at not persisted: %+v", listed)
			}
			if miss, err := s.LookupToken(ctx, "nope"); err != nil || miss != nil {
				t.Errorf("unknown hash: %+v %v", miss, err)
			}
			// Expired tokens are invisible to lookup but still listed.
			past := time.Now().Add(-time.Hour)
			if _, err := s.CreateToken(ctx, DefaultWorkspace, APIToken{Name: "old", OwnerEmail: "joao@acme.test", ExpiresAt: &past}, "hash-old"); err != nil {
				t.Fatal(err)
			}
			if exp, _ := s.LookupToken(ctx, "hash-old"); exp != nil {
				t.Errorf("expired token accepted: %+v", exp)
			}
			mine, err := s.ListTokens(ctx, DefaultWorkspace, "joao@acme.test")
			if err != nil || len(mine) != 2 {
				t.Fatalf("list mine: %d %v", len(mine), err)
			}
			if all, err := s.ListTokens(ctx, DefaultWorkspace, ""); err != nil || len(all) != 3 {
				t.Errorf("list all: %d %v", len(all), err)
			}
			if none, err := s.ListTokens(ctx, DefaultWorkspace, "other@acme.test"); err != nil || len(none) != 0 {
				t.Errorf("list other: %d %v", len(none), err)
			}
			if err := s.DeleteToken(ctx, DefaultWorkspace, tok.ID); err != nil {
				t.Fatal(err)
			}
			if err := s.DeleteToken(ctx, DefaultWorkspace, tok.ID); !errors.Is(err, ErrNotFound) {
				t.Errorf("delete twice: %v", err)
			}
			if gone, _ := s.LookupToken(ctx, "hash-1"); gone != nil {
				t.Errorf("revoked token accepted: %+v", gone)
			}
		})
	}
}

func TestGetIdentity(t *testing.T) {
	for name, open := range implementations(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := open(t)
			if _, err := s.TouchIdentity(ctx, DefaultWorkspace, Identity{Email: "Maria@acme.test", Name: "Maria", Provider: "google", Groups: []string{"eng"}}); err != nil {
				t.Fatal(err)
			}
			got, err := s.GetIdentity(ctx, DefaultWorkspace, "maria@acme.test")
			if err != nil || got.Provider != "google" || len(got.Groups) != 1 || got.Groups[0] != "eng" {
				t.Fatalf("get: %+v %v", got, err)
			}
			if _, err := s.GetIdentity(ctx, DefaultWorkspace, "nobody@acme.test"); !errors.Is(err, ErrNotFound) {
				t.Errorf("unknown: %v", err)
			}
		})
	}
}

func TestWorkspaces(t *testing.T) {
	for name, open := range implementations(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := open(t)
			// The default workspace is the operator's, with an address of its
			// own (RFC-0080); Migrate gave it the one the installer derived.
			def, _ := s.Workspace(ctx, DefaultWorkspace)
			if def == nil || !def.OwnedByOperator() || def.Status != WorkspaceActive {
				t.Fatalf("default workspace: %+v", def)
			}
			if DefaultWorkspaceSlug(ctx, s) != DefaultWorkspace {
				t.Errorf("default slug setting = %q", DefaultWorkspaceSlug(ctx, s))
			}
			acme, err := s.CreateWorkspace(ctx, Workspace{Slug: "acme", Name: "Acme", Address: "Acme.shpyrd.app"})
			if err != nil || acme.ID == "" || acme.Address != "acme.shpyrd.app" || acme.Status != WorkspaceActive || acme.OwnedByOperator() {
				t.Fatalf("create: %+v %v", acme, err)
			}
			// Slug and address are unique.
			if _, err := s.CreateWorkspace(ctx, Workspace{Slug: "acme", Name: "Other", Address: "other.shpyrd.app"}); !errors.Is(err, ErrConflict) {
				t.Errorf("duplicate slug: %v", err)
			}
			if _, err := s.CreateWorkspace(ctx, Workspace{Slug: "other", Name: "Other", Address: "acme.shpyrd.app"}); !errors.Is(err, ErrConflict) {
				t.Errorf("duplicate address: %v", err)
			}
			// Several workspaces may have no address.
			if _, err := s.CreateWorkspace(ctx, Workspace{Slug: "beta", Name: "Beta"}); err != nil {
				t.Errorf("second address-less workspace: %v", err)
			}
			// A new workspace has its built-in team.
			teams, _ := s.ListTeams(ctx, "acme")
			if len(teams) != 1 || !teams[0].Everyone {
				t.Errorf("acme teams: %+v", teams)
			}
			// By address, case-insensitive; never the empty address.
			if got, err := s.WorkspaceByAddress(ctx, "ACME.shpyrd.app"); err != nil || got.Slug != "acme" {
				t.Errorf("by address: %+v %v", got, err)
			}
			if _, err := s.WorkspaceByAddress(ctx, ""); !errors.Is(err, ErrNotFound) {
				t.Errorf("empty address: %v", err)
			}
			if _, err := s.WorkspaceByAddress(ctx, "nobody.shpyrd.app"); !errors.Is(err, ErrNotFound) {
				t.Errorf("unknown address: %v", err)
			}
			all, err := s.ListWorkspaces(ctx)
			if err != nil || len(all) != 3 || all[0].Slug != DefaultWorkspace {
				t.Errorf("list: %d %v %+v", len(all), err, all)
			}
			if w, err := s.SetWorkspaceStatus(ctx, "acme", WorkspaceSuspended); err != nil || w.Status != WorkspaceSuspended {
				t.Errorf("suspend: %+v %v", w, err)
			}
			if _, err := s.SetWorkspaceStatus(ctx, "nope", WorkspaceSuspended); !errors.Is(err, ErrNotFound) {
				t.Errorf("suspend unknown: %v", err)
			}
			// Objects of one workspace are invisible from another.
			if _, err := s.TouchIdentity(ctx, "acme", Identity{Email: "ana@acme.test"}); err != nil {
				t.Fatal(err)
			}
			if people, _ := s.ListIdentities(ctx, DefaultWorkspace); len(people) != 0 {
				t.Errorf("acme's person leaked into default: %+v", people)
			}
		})
	}
}

// A database migrated by the runner shpyrd had before v0.9.11 (a
// schema_migrations table of file names) is bridged: golang-migrate takes
// over at the version those files reached, the uuid migration runs, the
// rows survive with their ids, and a second Migrate is a no-op.
func TestMigrateBridgesLegacyRunner(t *testing.T) {
	url := os.Getenv("SHPYRD_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SHPYRD_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	p, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	dropAll(t, p)
	// The world before: the six files applied by hand, recorded by name.
	if _, err := p.pool.Exec(ctx, `CREATE TABLE schema_migrations (name TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"000001_init", "000002_sessions", "000003_everyone_status", "000004_domains_settings", "000005_tokens", "000006_workspace_address"} {
		sql, err := migrationFiles.ReadFile("migrations/" + f + ".up.sql")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.pool.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		legacyName := "0" + strings.TrimPrefix(strings.Split(f, "_")[0], "000") + "_" + strings.SplitN(f, "_", 2)[1] + ".sql"
		if _, err := p.pool.Exec(ctx, `INSERT INTO schema_migrations (name) VALUES ($1)`, legacyName); err != nil {
			t.Fatal(err)
		}
	}
	wsID, teamID := newID(), newID()
	if _, err := p.pool.Exec(ctx, `INSERT INTO workspaces (id, slug, name) VALUES ($1, 'acme', 'Acme')`, wsID); err != nil {
		t.Fatal(err)
	}
	if _, err := p.pool.Exec(ctx, `INSERT INTO teams (id, workspace_id, name, members) VALUES ($1, $2, 'ops', '["ana@acme.test"]')`, teamID, wsID); err != nil {
		t.Fatal(err)
	}
	if _, err := p.pool.Exec(ctx, `INSERT INTO grants (id, workspace_id, project, role, team_id) VALUES ($1, $2, 'shop', 'user', $3)`, newID(), wsID, teamID); err != nil {
		t.Fatal(err)
	}

	if err := p.Migrate(ctx, DefaultWorkspaceSpec{Name: "platform"}); err != nil {
		t.Fatalf("bridge + migrate: %v", err)
	}
	version, dirty, err := p.SchemaVersion()
	if err != nil || dirty || version < 7 {
		t.Fatalf("schema version = %d dirty=%v err=%v, want >= 7", version, dirty, err)
	}
	var typ string
	if err := p.pool.QueryRow(ctx, `SELECT data_type FROM information_schema.columns WHERE table_name = 'workspaces' AND column_name = 'id'`).Scan(&typ); err != nil || typ != "uuid" {
		t.Errorf("workspaces.id type = %q %v, want uuid", typ, err)
	}
	if err := p.pool.QueryRow(ctx, `SELECT data_type FROM information_schema.columns WHERE table_name = 'grants' AND column_name = 'team_id'`).Scan(&typ); err != nil || typ != "uuid" {
		t.Errorf("grants.team_id type = %q %v, want uuid", typ, err)
	}
	ws, err := p.Workspace(ctx, "acme")
	if err != nil || ws.ID != wsID {
		t.Fatalf("acme after the bridge: %+v %v (want id %s)", ws, err, wsID)
	}
	grants, err := p.ListGrants(ctx, "acme")
	if err != nil || len(grants) != 1 || grants[0].Team != "ops" {
		t.Errorf("grants after the bridge: %+v %v", grants, err)
	}
	// The default workspace was seeded; a second run changes nothing.
	if _, err := p.Workspace(ctx, DefaultWorkspace); err != nil {
		t.Errorf("default workspace: %v", err)
	}
	if err := p.Migrate(ctx, DefaultWorkspaceSpec{Name: "platform"}); err != nil {
		t.Errorf("second migrate: %v", err)
	}
	// The store keeps working on uuid columns: ids in, ids out.
	if _, _, err := p.PutTeam(ctx, "acme", Team{Name: "finance", Members: []string{"joao@acme.test"}}); err != nil {
		t.Errorf("put team on uuid columns: %v", err)
	}
	if _, err := p.AddGrant(ctx, "acme", Grant{Project: "shop", Role: "user", Team: "finance"}); err != nil {
		t.Errorf("grant with a uuid team id: %v", err)
	}
	if _, err := p.AddGrant(ctx, "acme", Grant{Project: "shop", Role: "user", Team: "finance"}); !errors.Is(err, ErrConflict) {
		t.Errorf("the grants uniqueness index must still hold: %v", err)
	}
}

// RFC-0076: projects live in the store keyed by ID; the ledger is re-keyed
// from legacy slugs once, rows already under the ID winning.
func TestProjectsStore(t *testing.T) {
	for name, open := range implementations(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := open(t)
			ws, err := s.Workspace(ctx, DefaultWorkspace)
			if err != nil {
				t.Fatal(err)
			}
			const id = "0b1e6c7a-9d6e-4c2f-8a1b-2f3e4d5c6b7a"
			pr, err := s.UpsertProject(ctx, Project{ID: id, WorkspaceID: DefaultWorkspace, Slug: "shop", Name: "Shop", Namespace: "app-shop"})
			if err != nil || pr.WorkspaceID != ws.ID || pr.Short() != ids.Short(id) || pr.CreatedAt.IsZero() {
				t.Fatalf("upsert: %+v %v", pr, err)
			}
			// Same ID again: rename in place, namespace kept when omitted.
			pr, err = s.UpsertProject(ctx, Project{ID: id, WorkspaceID: ws.ID, Slug: "store", Name: "The Store"})
			if err != nil || pr.Slug != "store" || pr.Name != "The Store" || pr.Namespace != "app-shop" {
				t.Fatalf("rename: %+v %v", pr, err)
			}
			// Another live project cannot take the slug.
			const other = "7f0d9e2c-1111-4222-8333-444455556666"
			if _, err := s.UpsertProject(ctx, Project{ID: other, WorkspaceID: DefaultWorkspace, Slug: "store"}); !errors.Is(err, ErrConflict) {
				t.Fatalf("slug conflict: %v", err)
			}
			if _, err := s.UpsertProject(ctx, Project{ID: other, WorkspaceID: DefaultWorkspace, Slug: "blog", Namespace: "p-" + ids.Short(other)}); err != nil {
				t.Fatal(err)
			}
			got, err := s.ProjectBySlug(ctx, DefaultWorkspace, "store")
			if err != nil || got.ID != id {
				t.Fatalf("by slug: %+v %v", got, err)
			}
			list, err := s.ListProjects(ctx, DefaultWorkspace, false)
			if err != nil || len(list) != 2 || list[0].ID != id {
				t.Fatalf("list: %+v %v", list, err)
			}
			if err := s.DeleteProject(ctx, other); err != nil {
				t.Fatal(err)
			}
			if err := s.DeleteProject(ctx, other); !errors.Is(err, ErrNotFound) {
				t.Fatalf("second delete: %v", err)
			}
			if list, _ = s.ListProjects(ctx, DefaultWorkspace, false); len(list) != 1 {
				t.Fatalf("live after delete: %+v", list)
			}
			if list, _ = s.ListProjects(ctx, DefaultWorkspace, true); len(list) != 2 || list[1].DeletedAt == nil {
				t.Fatalf("with deleted: %+v", list)
			}
			// The slug of a deleted project is free again.
			if _, err := s.UpsertProject(ctx, Project{ID: "9a9a9a9a-0000-4000-8000-000000000009", WorkspaceID: DefaultWorkspace, Slug: "blog"}); err != nil {
				t.Fatalf("slug reuse after delete: %v", err)
			}

			// Ledger rekey: three legacy rows, one of them colliding with a
			// row already keyed by the ID.
			t0 := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
			row := func(project string, start time.Time, qty float64) UsageBucket {
				return UsageBucket{WorkspaceID: DefaultWorkspace, Project: project, Component: "web", Metric: MetricCPUUsed,
					PeriodStart: start, PeriodEnd: start.Add(5 * time.Minute), Quantity: &qty, Unit: UnitCoreSeconds, Quality: QualityComplete, Revision: 1}
			}
			short := ids.Short(id)
			if err := s.WriteBuckets(ctx, []UsageBucket{row("shop", t0, 1), row("shop", t0.Add(5*time.Minute), 2), row("shop", t0.Add(10*time.Minute), 3), row(short, t0.Add(10*time.Minute), 30)}); err != nil {
				t.Fatal(err)
			}
			if err := s.WriteCOGSBucket(ctx, COGSBucket{WorkspaceID: DefaultWorkspace, Project: "shop", PeriodStart: t0, PeriodEnd: t0.Add(time.Hour), TotalCost: 1, Currency: "USD"}); err != nil {
				t.Fatal(err)
			}
			if err := s.WriteSleepEvent(ctx, SleepEvent{WorkspaceID: DefaultWorkspace, Project: "shop", Component: "web", Event: "sleep", At: t0}); err != nil {
				t.Fatal(err)
			}
			moved, err := s.RekeyProject(ctx, DefaultWorkspace, "shop", id)
			if err != nil || moved != 5 {
				t.Fatalf("rekey moved %d, err %v", moved, err)
			}
			buckets, err := s.QueryBuckets(ctx, DefaultWorkspace, short, t0, t0.Add(time.Hour))
			if err != nil || len(buckets) != 3 {
				t.Fatalf("buckets under the id: %d %v", len(buckets), err)
			}
			for _, b := range buckets {
				if b.PeriodStart.Equal(t0.Add(10*time.Minute)) && (b.Quantity == nil || *b.Quantity != 30) {
					t.Errorf("the row already keyed by the id must win, got quantity %v", b.Quantity)
				}
			}
			if left, _ := s.QueryBuckets(ctx, DefaultWorkspace, "shop", t0, t0.Add(time.Hour)); len(left) != 0 {
				t.Errorf("legacy rows left: %d", len(left))
			}
			cogs, _ := s.QueryCOGSBuckets(ctx, DefaultWorkspace, t0, t0.Add(time.Hour))
			if len(cogs) != 1 || cogs[0].Project != short {
				t.Errorf("cogs after rekey: %+v", cogs)
			}
			ev, _ := s.QuerySleepEvents(ctx, DefaultWorkspace, short, t0.Add(-time.Minute), t0.Add(time.Hour))
			if len(ev) != 1 {
				t.Errorf("sleep events after rekey: %+v", ev)
			}
			// Rekeying again moves nothing.
			if moved, err := s.RekeyProject(ctx, DefaultWorkspace, "shop", id); err != nil || moved != 0 {
				t.Errorf("second rekey: %d %v", moved, err)
			}
		})
	}
}
