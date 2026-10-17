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
			if got, err := s.GitHubInstallation(ctx, 42); err != nil || got.Workspace != DefaultWorkspace {
				t.Errorf("by id: %+v %v", got, err)
			}
			if _, err := s.GitHubInstallation(ctx, 7); !errors.Is(err, ErrNotFound) {
				t.Errorf("unknown id: %v", err)
			}
			// Suspended on GitHub, then unsuspended.
			if err := s.SetGitHubInstallationState(ctx, 42, true, false); err != nil {
				t.Fatal(err)
			}
			if got, _ := s.GitHubInstallation(ctx, 42); got == nil || got.SuspendedAt == nil {
				t.Errorf("suspended: %+v", got)
			}
			if err := s.SetGitHubInstallationState(ctx, 42, false, false); err != nil {
				t.Fatal(err)
			}
			if got, _ := s.GitHubInstallation(ctx, 42); got == nil || got.SuspendedAt != nil {
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
			if _, err := s.GitHubInstallation(ctx, 42); !errors.Is(err, ErrNotFound) {
				t.Errorf("deleted by id: %v", err)
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

func TestGitHubInstallationIsNeverTakenFromAnotherWorkspace(t *testing.T) {
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
			err := s.AddGitHubInstallation(ctx, "beta", acme(42, "bob@beta.test"))
			var held *GitHubHeldError
			if !errors.Is(err, ErrGitHubInstallationHeld) || !errors.As(err, &held) || held.Workspace != DefaultWorkspace {
				t.Fatalf("taken: %v", err)
			}
			if got, err := s.GitHubInstallation(ctx, 42); err != nil || got.Workspace != DefaultWorkspace || got.ConnectedBy != "ana@acme.test" {
				t.Errorf("the default workspace keeps it: %+v %v", got, err)
			}
			if err := s.AddGitHubInstallation(ctx, DefaultWorkspace, acme(42, "ana@acme.test")); err != nil {
				t.Errorf("connecting again where it is: %v", err)
			}
			// Disconnected there, or uninstalled on GitHub: another may have it.
			if err := s.DeleteGitHubInstallation(ctx, DefaultWorkspace, 42); err != nil {
				t.Fatal(err)
			}
			if err := s.AddGitHubInstallation(ctx, "beta", acme(42, "bob@beta.test")); err != nil {
				t.Fatalf("after a disconnect: %v", err)
			}
			if err := s.SetGitHubInstallationState(ctx, 42, false, true); err != nil {
				t.Fatal(err)
			}
			if err := s.AddGitHubInstallation(ctx, DefaultWorkspace, acme(42, "ana@acme.test")); err != nil {
				t.Fatalf("after an uninstall: %v", err)
			}
			if got, err := s.GitHubInstallation(ctx, 42); err != nil || got.Workspace != DefaultWorkspace {
				t.Errorf("by id: %+v %v", got, err)
			}
		})
	}
}

func TestGitHubInstallationRaceHasOneWinner(t *testing.T) {
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
			errs := make(chan error, len(slugs))
			var wg sync.WaitGroup
			for _, slug := range slugs {
				wg.Add(1)
				go func() {
					defer wg.Done()
					errs <- s.AddGitHubInstallation(ctx, slug, acme(77, slug))
				}()
			}
			wg.Wait()
			close(errs)
			won := 0
			for err := range errs {
				switch {
				case err == nil:
					won++
				case !errors.Is(err, ErrGitHubInstallationHeld):
					t.Errorf("unexpected: %v", err)
				}
			}
			got, err := s.GitHubInstallation(ctx, 77)
			if won != 1 || err != nil || got.ConnectedBy != got.Workspace {
				t.Errorf("%d won; kept %+v %v", won, got, err)
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
			if _, err := one.GitHubInstallation(ctx, 9); !errors.Is(err, ErrNotFound) {
				t.Errorf("a workspace added by hand: %v", err)
			}
			if got, err := one.GitHubInstallation(ctx, 42); err != nil || got.ID != 42 {
				t.Errorf("the default one's: %+v %v", got, err)
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
