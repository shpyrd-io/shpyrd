package store

import (
	"context"
	"errors"
	"testing"
)

func TestPrincipalsKeepPermissionsAcrossEmailChanges(t *testing.T) {
	for name, open := range implementations(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := open(t)
			ws := DefaultWorkspace
			original := Identity{Email: "one@example.test", Issuer: "https://idp.test", Subject: "subject-one", Connector: "company"}
			p, err := s.TouchIdentity(ctx, ws, original)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.PutMembership(ctx, ws, p.ID, WorkspaceRoleOwner); err != nil {
				t.Fatal(err)
			}
			if _, _, err = s.PutTeam(ctx, ws, Team{Name: "engineering", PrincipalIDs: []string{p.ID}}); err != nil {
				t.Fatal(err)
			}
			if _, err = s.AddGrant(ctx, ws, Grant{Project: "project", Role: "admin", User: p.Email, PrincipalID: p.ID}); err != nil {
				t.Fatal(err)
			}
			token, err := s.CreateToken(ctx, ws, APIToken{Name: "cli", OwnerEmail: p.Email, PrincipalID: p.ID}, "token-hash")
			if err != nil {
				t.Fatal(err)
			}
			original.Email = "changed@example.test"
			changed, err := s.TouchIdentity(ctx, ws, original)
			if err != nil || changed.ID != p.ID {
				t.Fatalf("identity changed: %+v %v", changed, err)
			}
			memberships, _ := s.ListMemberships(ctx, ws)
			teams, _ := s.ListTeams(ctx, ws)
			grants, _ := s.ListGrants(ctx, ws)
			tokens, _ := s.ListTokens(ctx, ws, p.ID)
			if len(memberships) != 1 || memberships[0].PrincipalID != p.ID || len(grants) != 1 || grants[0].PrincipalID != p.ID || len(tokens) != 1 || tokens[0].ID != token.ID {
				t.Fatal("principal references were lost")
			}
			found := false
			for _, team := range teams {
				if team.Name == "engineering" {
					found = len(team.PrincipalIDs) == 1 && team.PrincipalIDs[0] == p.ID
				}
			}
			if !found {
				t.Fatal("team reference lost")
			}
			recycled := original
			recycled.Subject = "another-person"
			q, err := s.TouchIdentity(ctx, ws, recycled)
			if err != nil || q.ID == p.ID {
				t.Fatal("email merged two identities", err)
			}
			if _, err = s.GetIdentity(ctx, ws, original.Email); !errors.Is(err, ErrConflict) {
				t.Fatal("ambiguous email did not fail closed", err)
			}
			if err = s.DeleteIdentity(ctx, ws, p.ID); err != nil {
				t.Fatal(err)
			}
			suspended, err := s.TouchIdentity(ctx, ws, original)
			if err != nil || suspended.ID != p.ID || suspended.Status != StatusSuspended {
				t.Fatal("sign-in resurrected removed principal", err)
			}
		})
	}
}

func TestLegacyPrincipalNeedsExplicitIdentityBinding(t *testing.T) {
	for name, open := range implementations(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := open(t)
			ws := DefaultWorkspace
			old, err := s.TouchIdentity(ctx, ws, Identity{Email: "old@example.test"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.PutMembership(ctx, ws, old.ID, WorkspaceRoleOwner); err != nil {
				t.Fatal(err)
			}
			other, err := s.TouchIdentity(ctx, ws, Identity{Email: old.Email, Issuer: "https://other.test", Subject: "external"})
			if err != nil || other.ID == old.ID {
				t.Fatal("legacy identity claimed by email", err)
			}
			if err = s.BindIdentity(ctx, ws, old.ID, "https://trusted.test", "owner", "company"); err != nil {
				t.Fatal(err)
			}
			owner, err := s.TouchIdentity(ctx, ws, Identity{Email: "new@example.test", Issuer: "https://trusted.test", Subject: "owner", Connector: "company"})
			if err != nil || owner.ID != old.ID {
				t.Fatal("explicit binding did not preserve owner", err)
			}
			if err = s.BindIdentity(ctx, ws, old.ID, "https://evil.test", "replacement", ""); !errors.Is(err, ErrConflict) {
				t.Fatal("already-bound identity overwritten", err)
			}
		})
	}
}

func TestInvitationCanBindOnlyOnePrincipal(t *testing.T) {
	for name, open := range implementations(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := open(t)
			ws := DefaultWorkspace
			a, err := s.TouchIdentity(ctx, ws, Identity{Email: "same@example.test", Issuer: "https://idp.test", Subject: "a"})
			if err != nil {
				t.Fatal(err)
			}
			b, err := s.TouchIdentity(ctx, ws, Identity{Email: a.Email, Issuer: "https://other.test", Subject: "b"})
			if err != nil {
				t.Fatal(err)
			}
			inv, err := s.CreateInvitation(ctx, ws, Invitation{Email: a.Email, Role: WorkspaceRoleMember}, "hash")
			if err != nil {
				t.Fatal(err)
			}
			results := make(chan error, 2)
			go func() { results <- s.AcceptInvitation(ctx, ws, inv.ID, a.ID) }()
			go func() { results <- s.AcceptInvitation(ctx, ws, inv.ID, b.ID) }()
			accepted := 0
			for i := 0; i < 2; i++ {
				if err := <-results; err == nil {
					accepted++
				} else if !errors.Is(err, ErrNotFound) {
					t.Fatal(err)
				}
			}
			roles, err := s.ListMemberships(ctx, ws)
			if err != nil || accepted != 1 || len(roles) != 1 {
				t.Fatalf("invitation accepted %d times, roles %d, err %v", accepted, len(roles), err)
			}
		})
	}
}
func TestOldBackupRestoresUnboundPermissions(t *testing.T) {
	for name, open := range implementations(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := open(t)
			ws := DefaultWorkspace
			dump := &Dump{Version: 3, Memberships: []Membership{{Email: "owner@example.test", Role: WorkspaceRoleOwner}}, Teams: []Team{{Name: "ops", Members: []string{"owner@example.test"}}}}
			if _, err := s.Import(ctx, ws, dump, false); err != nil {
				t.Fatal(err)
			}
			p, err := s.GetIdentity(ctx, ws, "owner@example.test")
			if err != nil || p.Subject != "" || p.Issuer != "" {
				t.Fatal("old backup fabricated an authenticated identity", err)
			}
			roles, err := s.ListMemberships(ctx, ws)
			if err != nil || len(roles) != 1 || roles[0].PrincipalID != p.ID {
				t.Fatal("old permission was not preserved for explicit binding", err)
			}
		})
	}
}

func TestConsolePermissionsDoNotMergeEqualEmails(t *testing.T) {
	for name, open := range implementations(t) {
		t.Run(name, func(t *testing.T) {
			s := open(t)
			ctx := context.Background()
			a, err := s.TouchIdentity(ctx, DefaultWorkspace, Identity{Realm: RealmConsole, Issuer: "https://one.test", Subject: "one", Email: "same@example.test"})
			if err != nil {
				t.Fatal(err)
			}
			b, err := s.TouchIdentity(ctx, DefaultWorkspace, Identity{Realm: RealmConsole, Issuer: "https://two.test", Subject: "two", Email: a.Email})
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range []*Identity{a, b} {
				if _, err := s.AddConsoleUser(ctx, p.ID, "operator"); err != nil {
					t.Fatal(err)
				}
			}
			users, err := s.ListConsoleUsers(ctx)
			if err != nil || len(users) != 2 {
				t.Fatalf("users: %+v %v", users, err)
			}
			if err := s.RemoveConsoleUser(ctx, a.Email); !errors.Is(err, ErrConflict) {
				t.Fatal("ambiguous email must fail closed", err)
			}
			if err := s.RemoveConsoleUser(ctx, a.ID); err != nil {
				t.Fatal(err)
			}
			users, err = s.ListConsoleUsers(ctx)
			if err != nil || len(users) != 1 || users[0].PrincipalID != b.ID {
				t.Fatalf("another principal lost access: %+v %v", users, err)
			}
		})
	}
}
