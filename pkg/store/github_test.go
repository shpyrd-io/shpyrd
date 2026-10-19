package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func acme(id int64, by string) GitHubInstallation {
	return GitHubInstallation{ID: id, AccountLogin: "Acme", AccountType: "Organization", ConnectedBy: by}
}

// connections are an installation's connections, by workspace.
func connections(t *testing.T, s Store, id int64) map[string]GitHubInstallation {
	t.Helper()
	list, err := s.GitHubInstallationConnections(context.Background(), id)
	if err != nil {
		t.Fatalf("connections of %d: %v", id, err)
	}
	out := map[string]GitHubInstallation{}
	for _, g := range list {
		out[g.Workspace] = g
	}
	return out
}

func TestGitHubInstallations(t *testing.T) {
	for name, open := range implementations(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := open(t)
			if _, err := s.CreateWorkspace(ctx, Workspace{Slug: "beta", Name: "Beta"}); err != nil {
				t.Fatal(err)
			}
			if err := s.AddGitHubInstallation(ctx, DefaultWorkspace, acme(42, "ana@acme.test")); err != nil {
				t.Fatalf("add: %v", err)
			}
			list, err := s.GitHubInstallations(ctx, DefaultWorkspace)
			if err != nil || len(list) != 1 || list[0].ID != 42 || list[0].Workspace != DefaultWorkspace || list[0].AccountLogin != "Acme" ||
				list[0].AccountType != "Organization" || list[0].ConnectedBy != "ana@acme.test" || list[0].CreatedAt.IsZero() || list[0].SuspendedAt != nil {
				t.Fatalf("list: %+v %v", list, err)
			}
			if got := connections(t, s, 42); len(got) != 1 || got[DefaultWorkspace].ID != 42 {
				t.Errorf("by id: %+v", got)
			}
			if got := connections(t, s, 7); len(got) != 0 {
				t.Errorf("unknown id: %+v", got)
			}
			// Suspended on GitHub, then unsuspended.
			if err := s.SetGitHubInstallationState(ctx, 42, true, false); err != nil {
				t.Fatal(err)
			}
			if got := connections(t, s, 42)[DefaultWorkspace]; got.SuspendedAt == nil {
				t.Errorf("suspended: %+v", got)
			}
			if err := s.SetGitHubInstallationState(ctx, 42, false, false); err != nil {
				t.Fatal(err)
			}
			if got := connections(t, s, 42)[DefaultWorkspace]; got.ID != 42 || got.SuspendedAt != nil {
				t.Errorf("unsuspended: %+v", got)
			}
			if err := s.SetGitHubInstallationState(ctx, 7, true, false); !errors.Is(err, ErrNotFound) {
				t.Errorf("unknown id: %v", err)
			}
			// Deleted on GitHub: gone from the lists; a new connect revives it.
			if err := s.SetGitHubInstallationState(ctx, 42, false, true); err != nil {
				t.Fatal(err)
			}
			if list, _ := s.GitHubInstallations(ctx, DefaultWorkspace); len(list) != 0 {
				t.Errorf("deleted is not listed: %+v", list)
			}
			if got := connections(t, s, 42); len(got) != 0 {
				t.Errorf("deleted by id: %+v", got)
			}
			if err := s.AddGitHubInstallation(ctx, DefaultWorkspace, acme(42, "bob@acme.test")); err != nil {
				t.Fatalf("revive: %v", err)
			}
			if list, _ := s.GitHubInstallations(ctx, DefaultWorkspace); len(list) != 1 || list[0].ConnectedBy != "bob@acme.test" || list[0].DeletedAt != nil {
				t.Errorf("revived: %+v", list)
			}
			// Disconnect: only from the workspace it belongs to.
			if err := s.DeleteGitHubInstallation(ctx, "beta", 42); !errors.Is(err, ErrNotFound) {
				t.Errorf("another workspace's: %v", err)
			}
			if err := s.DeleteGitHubInstallation(ctx, DefaultWorkspace, 42); err != nil {
				t.Fatal(err)
			}
			if list, _ := s.GitHubInstallations(ctx, DefaultWorkspace); len(list) != 0 {
				t.Errorf("disconnected: %+v", list)
			}
		})
	}
}

func TestWorkspacesShareAGitHubInstallation(t *testing.T) {
	for name, open := range implementations(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := open(t)
			if _, err := s.CreateWorkspace(ctx, Workspace{Slug: "beta", Name: "Beta"}); err != nil {
				t.Fatal(err)
			}
			if err := s.AddGitHubInstallation(ctx, DefaultWorkspace, acme(42, "ana@acme.test")); err != nil {
				t.Fatal(err)
			}
			if err := s.AddGitHubInstallation(ctx, "beta", acme(42, "bob@beta.test")); err != nil {
				t.Fatalf("beta connects it too: %v", err)
			}
			got := connections(t, s, 42)
			if len(got) != 2 || got[DefaultWorkspace].ConnectedBy != "ana@acme.test" || got["beta"].ConnectedBy != "bob@beta.test" {
				t.Fatalf("both: %+v", got)
			}
			for _, ws := range []string{DefaultWorkspace, "beta"} {
				if list, _ := s.GitHubInstallations(ctx, ws); len(list) != 1 || list[0].ID != 42 || list[0].Workspace != ws {
					t.Errorf("%s lists: %+v", ws, list)
				}
			}
			// A connection made again keeps its time.
			first := got[DefaultWorkspace].CreatedAt
			if err := s.AddGitHubInstallation(ctx, DefaultWorkspace, acme(42, "ana@acme.test")); err != nil {
				t.Fatal(err)
			}
			if again := connections(t, s, 42)[DefaultWorkspace]; !again.CreatedAt.Equal(first) {
				t.Errorf("created at moved: %v, was %v", again.CreatedAt, first)
			}
			// GitHub's state is the installation's: every connection has it.
			if err := s.SetGitHubInstallationState(ctx, 42, true, false); err != nil {
				t.Fatal(err)
			}
			for ws, g := range connections(t, s, 42) {
				if g.SuspendedAt == nil {
					t.Errorf("%s's is not suspended", ws)
				}
			}
			// A disconnect is one workspace's.
			if err := s.DeleteGitHubInstallation(ctx, "beta", 42); err != nil {
				t.Fatal(err)
			}
			if got := connections(t, s, 42); len(got) != 1 || got[DefaultWorkspace].ID != 42 {
				t.Errorf("after beta's disconnect: %+v", got)
			}
			// Uninstalled on GitHub: gone from every workspace.
			if err := s.AddGitHubInstallation(ctx, "beta", acme(42, "bob@beta.test")); err != nil {
				t.Fatal(err)
			}
			if err := s.SetGitHubInstallationState(ctx, 42, false, true); err != nil {
				t.Fatal(err)
			}
			if got := connections(t, s, 42); len(got) != 0 {
				t.Errorf("after the uninstall: %+v", got)
			}
		})
	}
}

func TestGitHubInstallationConnectsRaceAllWin(t *testing.T) {
	for name, open := range implementations(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := open(t)
			slugs := []string{DefaultWorkspace}
			for _, slug := range []string{"w1", "w2", "w3", "w4", "w5"} {
				if _, err := s.CreateWorkspace(ctx, Workspace{Slug: slug, Name: slug}); err != nil {
					t.Fatal(err)
				}
				slugs = append(slugs, slug)
			}
			errs := make(chan error, 2*len(slugs))
			var wg sync.WaitGroup
			for _, slug := range append(slugs, slugs...) {
				wg.Add(1)
				go func() {
					defer wg.Done()
					errs <- s.AddGitHubInstallation(ctx, slug, acme(77, slug))
				}()
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				if err != nil {
					t.Errorf("unexpected: %v", err)
				}
			}
			got := connections(t, s, 77)
			if len(got) != len(slugs) {
				t.Fatalf("%d connections: %+v", len(got), got)
			}
			for ws, g := range got {
				if g.ConnectedBy != ws {
					t.Errorf("%s: %+v", ws, g)
				}
			}
		})
	}
}

func TestOneWorkspaceSeesOnlyItsGitHubInstallations(t *testing.T) {
	for name, open := range implementations(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			st := open(t)
			if _, err := st.CreateWorkspace(ctx, Workspace{Slug: "byhand", Name: "By hand"}); err != nil {
				t.Fatal(err)
			}
			if err := st.AddGitHubInstallation(ctx, "byhand", acme(9, "x@byhand.test")); err != nil {
				t.Fatal(err)
			}
			if err := st.AddGitHubInstallation(ctx, DefaultWorkspace, acme(42, "ana@acme.test")); err != nil {
				t.Fatal(err)
			}
			one := OneWorkspace(st)
			if err := st.AddGitHubInstallation(ctx, "byhand", acme(42, "x@byhand.test")); err != nil {
				t.Fatal(err)
			}
			if got := connections(t, one, 9); len(got) != 0 {
				t.Errorf("a workspace added by hand: %+v", got)
			}
			if got := connections(t, one, 42); len(got) != 1 || got[DefaultWorkspace].ID != 42 {
				t.Errorf("the default one's only: %+v", got)
			}
		})
	}
}

func TestGitHubUserLinks(t *testing.T) {
	for name, open := range implementations(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := open(t)
			if _, err := s.GitHubUserLink(ctx, "local", "p-1"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("before: %v", err)
			}
			exp := time.Now().Add(184 * 24 * time.Hour).UTC().Truncate(time.Second)
			if err := s.PutGitHubUserLink(ctx, GitHubUserLink{Provider: "local", Subject: "p-1", GitHubUserID: 1001, GitHubLogin: "ana",
				RefreshToken: "v1.sealed-1", RefreshExpiresAt: exp}); err != nil {
				t.Fatal(err)
			}
			got, err := s.GitHubUserLink(ctx, "local", "p-1")
			if err != nil || got.GitHubUserID != 1001 || got.GitHubLogin != "ana" || got.RefreshToken != "v1.sealed-1" ||
				!got.RefreshExpiresAt.Equal(exp) || got.LinkedAt.IsZero() || got.UpdatedAt.IsZero() {
				t.Fatalf("got %+v %v", got, err)
			}
			// The same subject under another provider is someone else.
			if _, err := s.GitHubUserLink(ctx, "okta", "p-1"); !errors.Is(err, ErrNotFound) {
				t.Errorf("another provider: %v", err)
			}
			// A refresh rotates the token, and a rename on GitHub shows; the
			// link keeps its time while the GitHub account is the same.
			if err := s.PutGitHubUserLink(ctx, GitHubUserLink{Provider: "local", Subject: "p-1", GitHubUserID: 1001, GitHubLogin: "ana-renamed",
				RefreshToken: "v1.sealed-2", RefreshExpiresAt: exp.Add(time.Hour)}); err != nil {
				t.Fatal(err)
			}
			again, err := s.GitHubUserLink(ctx, "local", "p-1")
			if err != nil || again.RefreshToken != "v1.sealed-2" || again.GitHubLogin != "ana-renamed" || !again.LinkedAt.Equal(got.LinkedAt) ||
				!again.RefreshExpiresAt.Equal(exp.Add(time.Hour)) {
				t.Errorf("rotated: %+v %v", again, err)
			}
			if err := s.DeleteGitHubUserLink(ctx, "local", "p-1"); err != nil {
				t.Fatal(err)
			}
			if _, err := s.GitHubUserLink(ctx, "local", "p-1"); !errors.Is(err, ErrNotFound) {
				t.Errorf("after delete: %v", err)
			}
			if err := s.DeleteGitHubUserLink(ctx, "local", "p-1"); !errors.Is(err, ErrNotFound) {
				t.Errorf("deleted twice: %v", err)
			}
		})
	}
}

func TestGitHubUserLinksConditionalWrites(t *testing.T) {
	for name, open := range implementations(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := open(t)
			exp := time.Now().Add(184 * 24 * time.Hour).UTC().Truncate(time.Second)
			next := GitHubUserLink{GitHubUserID: 1001, GitHubLogin: "ana2", RefreshToken: "v1.sealed-2", RefreshExpiresAt: exp.Add(time.Hour)}
			// A missing row: nothing replaced, nothing deleted.
			if ok, err := s.ReplaceGitHubUserLinkToken(ctx, "local", "p-1", "v1.sealed-1", next); err != nil || ok {
				t.Fatalf("replace on a missing row: %v %v", ok, err)
			}
			if ok, err := s.DeleteGitHubUserLinkIf(ctx, "local", "p-1", "v1.sealed-1"); err != nil || ok {
				t.Fatalf("delete-if on a missing row: %v %v", ok, err)
			}
			if err := s.PutGitHubUserLink(ctx, GitHubUserLink{Provider: "local", Subject: "p-1", GitHubUserID: 1001, GitHubLogin: "ana",
				RefreshToken: "v1.sealed-1", RefreshExpiresAt: exp}); err != nil {
				t.Fatal(err)
			}
			before, _ := s.GitHubUserLink(ctx, "local", "p-1")
			time.Sleep(5 * time.Millisecond)
			// A stale old value changes nothing.
			if ok, err := s.ReplaceGitHubUserLinkToken(ctx, "local", "p-1", "v1.stale", next); err != nil || ok {
				t.Fatalf("stale replace: %v %v", ok, err)
			}
			if got, _ := s.GitHubUserLink(ctx, "local", "p-1"); got.RefreshToken != "v1.sealed-1" || got.GitHubLogin != "ana" || !got.UpdatedAt.Equal(before.UpdatedAt) {
				t.Errorf("stale replace changed the row: %+v", got)
			}
			if ok, err := s.DeleteGitHubUserLinkIf(ctx, "local", "p-1", "v1.stale"); err != nil || ok {
				t.Fatalf("stale delete-if: %v %v", ok, err)
			}
			if _, err := s.GitHubUserLink(ctx, "local", "p-1"); err != nil {
				t.Fatalf("stale delete-if removed the row: %v", err)
			}
			// The right old value replaces; linked_at stays, updated_at moves.
			if ok, err := s.ReplaceGitHubUserLinkToken(ctx, "local", "p-1", "v1.sealed-1", next); err != nil || !ok {
				t.Fatalf("replace: %v %v", ok, err)
			}
			got, _ := s.GitHubUserLink(ctx, "local", "p-1")
			if got.RefreshToken != "v1.sealed-2" || got.GitHubLogin != "ana2" || got.GitHubUserID != 1001 || !got.RefreshExpiresAt.Equal(exp.Add(time.Hour)) ||
				!got.LinkedAt.Equal(before.LinkedAt) || !got.UpdatedAt.After(before.UpdatedAt) {
				t.Errorf("replaced: %+v", got)
			}
			// The old value no longer matches; the new one deletes.
			if ok, err := s.DeleteGitHubUserLinkIf(ctx, "local", "p-1", "v1.sealed-1"); err != nil || ok {
				t.Fatalf("delete-if with the replaced value: %v %v", ok, err)
			}
			if ok, err := s.DeleteGitHubUserLinkIf(ctx, "local", "p-1", "v1.sealed-2"); err != nil || !ok {
				t.Fatalf("delete-if: %v %v", ok, err)
			}
			if _, err := s.GitHubUserLink(ctx, "local", "p-1"); !errors.Is(err, ErrNotFound) {
				t.Errorf("after delete-if: %v", err)
			}
		})
	}
}

func TestGitHubUserLinksAnotherAccountResetsLinkedAt(t *testing.T) {
	for name, open := range implementations(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := open(t)
			exp := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
			put := func(id int64, tok string) *GitHubUserLink {
				t.Helper()
				if err := s.PutGitHubUserLink(ctx, GitHubUserLink{Provider: "local", Subject: "p-1", GitHubUserID: id, GitHubLogin: "x", RefreshToken: tok, RefreshExpiresAt: exp}); err != nil {
					t.Fatal(err)
				}
				l, err := s.GitHubUserLink(ctx, "local", "p-1")
				if err != nil {
					t.Fatal(err)
				}
				return l
			}
			first := put(1001, "a")
			time.Sleep(5 * time.Millisecond)
			same := put(1001, "b")
			if !same.LinkedAt.Equal(first.LinkedAt) || !same.UpdatedAt.After(first.UpdatedAt) {
				t.Errorf("same account: linked %v -> %v, updated %v -> %v", first.LinkedAt, same.LinkedAt, first.UpdatedAt, same.UpdatedAt)
			}
			time.Sleep(5 * time.Millisecond)
			other := put(2002, "c")
			if !other.LinkedAt.After(first.LinkedAt) {
				t.Errorf("another account kept LinkedAt: %v -> %v", first.LinkedAt, other.LinkedAt)
			}
		})
	}
}
