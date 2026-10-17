package github_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shpyrd-io/shpyrd/pkg/github"
	"github.com/shpyrd-io/shpyrd/pkg/github/githubtest"
)

func fullNames(list []github.Repository) string {
	var out []string
	for _, r := range list {
		out = append(out, r.FullName)
	}
	return strings.Join(out, ",")
}

func TestALinkExchangesTheCodeForTokensThatRotate(t *testing.T) {
	c, fake := newClient(t)
	ctx := context.Background()
	tok, err := c.ExchangeUserCode(ctx, githubtest.Code)
	if err != nil || tok.Access != githubtest.UserToken || !strings.HasPrefix(tok.Refresh, "ghr_") ||
		!tok.AccessExpiresAt.Equal(t0.Add(8*time.Hour)) || !tok.RefreshExpiresAt.Equal(t0.Add(15897600*time.Second)) {
		t.Fatalf("%+v %v", tok, err)
	}
	who, err := c.UserProfile(ctx, tok.Access)
	if err != nil || who.ID != githubtest.UserID || who.Login != githubtest.UserLogin {
		t.Fatalf("%+v %v", who, err)
	}
	next, err := c.RefreshUserToken(ctx, tok.Refresh)
	if err != nil || next.Access == tok.Access || next.Refresh == tok.Refresh || next.Refresh == "" {
		t.Fatalf("refresh: %+v %v", next, err)
	}
	if _, err := c.RefreshUserToken(ctx, tok.Refresh); !errors.Is(err, github.ErrUserTokenRefused) {
		t.Errorf("a refresh token is taken once: %v", err)
	}
	if _, err := c.ExchangeUserCode(ctx, "stale"); err == nil || !strings.Contains(err.Error(), "incorrect or expired") {
		t.Errorf("a bad code: %v", err)
	}
	fake.SetExpiringUserTokens(false)
	if plain, err := c.ExchangeUserCode(ctx, githubtest.Code); err != nil || plain.Refresh != "" {
		t.Errorf("tokens that do not expire have no refresh token: %+v %v", plain, err)
	}
	fake.ExpireUserTokens()
	if _, err := c.UserProfile(ctx, next.Access); err == nil {
		t.Error("an expired access token still reads")
	}
}

func TestAPersonReadsOnlyWhatTheyMay(t *testing.T) {
	c, fake := newClient(t)
	ctx := context.Background()
	fake.AddUser("bob-code", githubtest.User{ID: 1002, Login: "bob", Installations: []int64{42}, Repos: []string{"Acme/site"}})
	bob, err := c.ExchangeUserCode(ctx, "bob-code")
	if err != nil {
		t.Fatal(err)
	}
	if who, _ := c.UserProfile(ctx, bob.Access); who.Login != "bob" || who.ID != 1002 {
		t.Errorf("bob is %+v", who)
	}
	repos, err := c.UserRepositories(ctx, bob.Access, 42)
	if err != nil || fullNames(repos) != "Acme/site" || repos[0].Installation != 42 || repos[0].DefaultBranch != "trunk" {
		t.Fatalf("bob's repositories: %+v %v", repos, err)
	}
	if _, err := c.UserRepository(ctx, bob.Access, "Acme", "shop"); !github.IsNotFound(err) {
		t.Errorf("bob reads shop: %v", err)
	}
	if r, err := c.UserRepository(ctx, bob.Access, "acme", "SITE"); err != nil || r.Private || r.FullName != "Acme/site" {
		t.Errorf("site: %+v %v", r, err)
	}
	if _, err := c.UserBranches(ctx, bob.Access, "Acme", "shop"); !github.IsNotFound(err) {
		t.Errorf("bob lists shop's branches: %v", err)
	}
	if names, err := c.UserBranches(ctx, bob.Access, "Acme", "site"); err != nil || strings.Join(names, ",") != "trunk" {
		t.Errorf("site's branches: %v %v", names, err)
	}
	if _, err := c.UserRepositories(ctx, bob.Access, 7); !github.IsNotFound(err) {
		t.Errorf("an installation bob cannot see: %v", err)
	}
	ana, _ := c.ExchangeUserCode(ctx, githubtest.Code)
	if repos, err := c.UserRepositories(ctx, ana.Access, 42); err != nil || fullNames(repos) != "Acme/shop,Acme/site" {
		t.Errorf("ana's repositories: %+v %v", repos, err)
	}
	if r, err := c.UserRepository(ctx, ana.Access, "Acme", "shop"); err != nil || !r.Private {
		t.Errorf("ana reads shop: %+v %v", r, err)
	}
}

func TestTheAppReadsItsRepositoriesAndPublicOnes(t *testing.T) {
	c, fake := newClient(t)
	ctx := context.Background()
	fake.SetPublicRepos(githubtest.Repo{FullName: "octo/hello", DefaultBranch: "main"})
	if r, err := c.RepositoryAsApp(ctx, 42, "Acme", "shop"); err != nil || !r.Private {
		t.Errorf("shop: %+v %v", r, err)
	}
	if r, err := c.RepositoryAsApp(ctx, 42, "octo", "hello"); err != nil || r.Private {
		t.Errorf("a public repository elsewhere: %+v %v", r, err)
	}
	if _, err := c.RepositoryAsApp(ctx, 42, "Acme", "vault"); !github.IsNotFound(err) {
		t.Errorf("a private repository the installation does not include: %v", err)
	}
}

func TestUnlinkRevokesTheToken(t *testing.T) {
	c, fake := newClient(t)
	ctx := context.Background()
	tok, _ := c.ExchangeUserCode(ctx, githubtest.Code)
	if err := c.RevokeUserToken(ctx, tok.Access); err != nil {
		t.Fatal(err)
	}
	if got := fake.Revoked(); len(got) != 1 || got[0] != tok.Access {
		t.Errorf("revoked %v", got)
	}
	if _, err := c.UserProfile(ctx, tok.Access); err == nil {
		t.Error("a revoked token still reads")
	}
	if err := c.RevokeUserToken(ctx, tok.Access); err != nil {
		t.Errorf("a token GitHub no longer knows: %v", err)
	}
}
