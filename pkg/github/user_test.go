package github_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
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
		t.Fatalf("exchange: access ok %v refresh set %v expiries %v %v: %v", tok.Access == githubtest.UserToken, tok.Refresh != "", tok.AccessExpiresAt, tok.RefreshExpiresAt, err)
	}
	who, err := c.UserProfile(ctx, tok.Access)
	if err != nil || who.ID != githubtest.UserID || who.Login != githubtest.UserLogin {
		t.Fatalf("%+v %v", who, err)
	}
	next, err := c.RefreshUserToken(ctx, tok.Refresh)
	if err != nil || next.Access == tok.Access || next.Refresh == tok.Refresh || next.Refresh == "" {
		t.Fatalf("refresh: rotated %v %v: %v", next.Access != tok.Access, next.Refresh != tok.Refresh && next.Refresh != "", err)
	}
	if _, err := c.RefreshUserToken(ctx, tok.Refresh); !errors.Is(err, github.ErrUserTokenRefused) {
		t.Errorf("a refresh token is taken once: %v", err)
	}
	if _, err := c.ExchangeUserCode(ctx, "stale"); err == nil || !strings.Contains(err.Error(), "incorrect or expired") {
		t.Errorf("a bad code: %v", err)
	}
	fake.SetExpiringUserTokens(false)
	if plain, err := c.ExchangeUserCode(ctx, githubtest.Code); err != nil || plain.Refresh != "" {
		t.Errorf("tokens that do not expire have no refresh token: refresh set %v: %v", plain.Refresh != "", err)
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

func TestAUserTokenNeverPrintsItsSecrets(t *testing.T) {
	c, _ := newClient(t)
	tok, err := c.ExchangeUserCode(context.Background(), githubtest.Code)
	if err != nil {
		t.Fatal(err)
	}
	got := fmt.Sprintf("%v %+v %#v %s", tok, tok, tok, &tok)
	if strings.Contains(got, tok.Access) || strings.Contains(got, tok.Refresh) || tok.Refresh == "" {
		t.Error("a user token printed one of its tokens")
	}
}

func TestAListThatIsCutShortSaysSo(t *testing.T) {
	c, fake := newClient(t)
	ctx := context.Background()
	var repos []githubtest.Repo
	for i := range 150 {
		repos = append(repos, githubtest.Repo{FullName: fmt.Sprintf("Acme/r%03d", i), DefaultBranch: "main"})
	}
	fake.SetRepos(42, repos...)
	fake.SetBranches("Acme/r000", branchNames(150)...)
	tok, _ := c.ExchangeUserCode(ctx, githubtest.Code)

	list, err := c.UserRepositories(ctx, tok.Access, 42)
	if err != nil || len(list) != 150 {
		t.Fatalf("within the cap: %d %v", len(list), err)
	}
	if names, err := c.UserBranches(ctx, tok.Access, "Acme", "r000"); err != nil || len(names) != 150 {
		t.Fatalf("branches within the cap: %d %v", len(names), err)
	}
	defer github.SetPageCaps(1, 1)()
	list, err = c.UserRepositories(ctx, tok.Access, 42)
	if !errors.Is(err, github.ErrTruncated) || len(list) != 100 {
		t.Errorf("a cut-short list: %d %v", len(list), err)
	}
	if all, err := c.Repositories(ctx, 42); !errors.Is(err, github.ErrTruncated) || len(all) != 100 {
		t.Errorf("the installation's own list: %d %v", len(all), err)
	}
	names, err := c.UserBranches(ctx, tok.Access, "Acme", "r000")
	if !errors.Is(err, github.ErrTruncated) || len(names) != 100 {
		t.Errorf("a cut-short branch list: %d %v", len(names), err)
	}
}

func branchNames(n int) []string {
	var out []string
	for i := range n {
		out = append(out, fmt.Sprintf("b%03d", i))
	}
	return out
}

func TestARefusalIsToldFromARateLimit(t *testing.T) {
	c, fake := newClient(t)
	ctx := context.Background()
	fake.AddUser("bob-code", githubtest.User{ID: 1002, Login: "bob"})
	bob, _ := c.ExchangeUserCode(ctx, "bob-code")
	fake.ForbidRepo("bob", "Acme/shop")
	_, err := c.UserRepository(ctx, bob.Access, "Acme", "shop")
	if !github.IsForbidden(err) || github.IsNotFound(err) {
		t.Errorf("a repository bob is refused: %v", err)
	}
	if _, err := c.UserBranches(ctx, bob.Access, "Acme", "shop"); !github.IsForbidden(err) {
		t.Errorf("its branches: %v", err)
	}
	if _, err := c.UserRepository(ctx, bob.Access, "Acme", "site"); err != nil {
		t.Errorf("another repository: %v", err)
	}
	fake.SetRateLimited(time.Minute)
	if _, err := c.UserRepository(ctx, bob.Access, "Acme", "site"); github.IsForbidden(err) || err == nil {
		t.Errorf("a rate limit is no refusal: %v", err)
	}
	fake.SetRateLimited(0)
	fake.SetForbidden(true)
	if _, err := c.UserRepository(ctx, bob.Access, "Acme", "site"); !github.IsForbidden(err) {
		t.Errorf("the App refused: %v", err)
	}
}

func TestAnEmptyAnswerWhereOneIsDueIsAnError(t *testing.T) {
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(empty.Close)
	fake := githubtest.NewServer(t)
	c, err := github.New(fake.App(), github.Options{APIURL: empty.URL, HTTP: empty.Client(), Now: fake.Time})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.UserRepository(context.Background(), "tok", "o", "r"); err == nil {
		t.Error("an empty 200 read as a repository")
	}
	if err := c.RevokeUserToken(context.Background(), "tok"); err != nil {
		t.Errorf("a 204 with no body: %v", err)
	}
}

func TestAGitHubThatCannotAnswerIsNotARefusedRefresh(t *testing.T) {
	c, fake := newClient(t)
	ctx := context.Background()
	tok, _ := c.ExchangeUserCode(ctx, githubtest.Code)
	fake.SetDown(true)
	if _, err := c.RefreshUserToken(ctx, tok.Refresh); err == nil || errors.Is(err, github.ErrUserTokenRefused) {
		t.Errorf("GitHub down: %v", err)
	}
	fake.SetDown(false)
	fake.SetRateLimited(time.Minute)
	if _, err := c.RefreshUserToken(ctx, tok.Refresh); err == nil || errors.Is(err, github.ErrUserTokenRefused) {
		t.Errorf("rate limited: %v", err)
	}
	fake.SetRateLimited(0)
	next, err := c.RefreshUserToken(ctx, tok.Refresh)
	if err != nil {
		t.Fatalf("the refresh token still works: %v", err)
	}
	if !next.AccessExpiresAt.Equal(t0.Add(8*time.Hour)) || !next.RefreshExpiresAt.Equal(t0.Add(15897600*time.Second)) {
		t.Errorf("a refresh gives both expiries: %v %v", next.AccessExpiresAt, next.RefreshExpiresAt)
	}
	fake.ForgetRefreshTokens()
	if _, err := c.RefreshUserToken(ctx, next.Refresh); !errors.Is(err, github.ErrUserTokenRefused) {
		t.Errorf("a forgotten refresh token: %v", err)
	}
}

func TestOtherRefusalsOfARefreshAreNamedAsSuch(t *testing.T) {
	odd := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"error":"unauthorized_client","error_description":"nope","access_token":"ghu_x"}`)
	}))
	t.Cleanup(odd.Close)
	fake := githubtest.NewServer(t)
	c, err := github.New(fake.App(), github.Options{APIURL: odd.URL, WebURL: odd.URL, HTTP: odd.Client(), Now: fake.Time})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.RefreshUserToken(context.Background(), "ghr_x")
	if err == nil || !strings.Contains(err.Error(), "GitHub refused to renew the GitHub sign-in") || errors.Is(err, github.ErrUserTokenRefused) {
		t.Errorf("a refusal with another code: %v", err)
	}
	if _, err := c.ExchangeUserCode(context.Background(), "c"); err == nil || !strings.Contains(err.Error(), "did not accept the sign-in") {
		t.Errorf("an exchange: %v", err)
	}
}

func TestTheFakeAnswersPerPerson(t *testing.T) {
	c, fake := newClient(t)
	ctx := context.Background()
	fake.AddUser("bob-code", githubtest.User{ID: 1002, Login: "bob"})
	fake.SetUserOrgRole("bob", "Acme", "member", "active")
	bob, _ := c.ExchangeUserCode(ctx, "bob-code")
	if role, _, err := c.OrgMembership(ctx, bob.Access, "Acme"); err != nil || role != "member" {
		t.Errorf("bob in Acme: %q %v", role, err)
	}
	if role, _, err := c.OrgMembership(ctx, githubtest.UserToken, "Acme"); err != nil || role != "admin" {
		t.Errorf("ana in Acme: %q %v", role, err)
	}
	fake.AddUser("eve-code", githubtest.User{ID: 1003, Login: "eve"})
	eve, _ := c.ExchangeUserCode(ctx, "eve-code")
	if role, _, err := c.OrgMembership(ctx, eve.Access, "Acme"); err != nil || role != "" {
		t.Errorf("eve is no member: %q %v", role, err)
	}

	fake.SetExpiringUserTokens(false)
	plain, _ := c.ExchangeUserCode(ctx, "bob-code")
	fake.ExpireUserTokens()
	if _, err := c.UserProfile(ctx, plain.Access); err != nil {
		t.Errorf("a token that does not expire expired: %v", err)
	}
	if _, err := c.UserProfile(ctx, bob.Access); err == nil {
		t.Error("an expiring token did not expire")
	}
}

func TestAnInstallationTokenReadsOnlyTheRepositoriesItWasAskedFor(t *testing.T) {
	c, fake := newClient(t)
	ctx := context.Background()
	fake.SetRepos(42, githubtest.Repo{FullName: "Acme/shop", Private: true, DefaultBranch: "main"}, githubtest.Repo{FullName: "Acme/vault", Private: true, DefaultBranch: "main"})
	scoped, err := c.InstallationToken(ctx, 42, "shop", github.ContentsRead())
	if err != nil {
		t.Fatal(err)
	}
	status := func(token, path string) int {
		req, _ := http.NewRequest("GET", fake.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := fake.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if got := status(scoped.Value, "/repos/Acme/shop"); got != 200 {
		t.Errorf("shop: %d", got)
	}
	if got := status(scoped.Value, "/repos/Acme/vault"); got != 404 {
		t.Errorf("vault, outside the token's scope: %d", got)
	}
	whole, _ := c.InstallationToken(ctx, 42, "", github.ContentsRead())
	if got := status(whole.Value, "/repos/Acme/vault"); got != 200 {
		t.Errorf("vault with the whole installation: %d", got)
	}
}
