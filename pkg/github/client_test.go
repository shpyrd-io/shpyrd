package github_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
	"github.com/shpyrd-io/shpyrd/pkg/github"
	"github.com/shpyrd-io/shpyrd/pkg/github/githubtest"
)

var t0 = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

func newClient(t *testing.T) (*github.Client, *githubtest.Server) {
	t.Helper()
	fake := githubtest.NewServer(t)
	fake.SetNow(t0)
	c, err := github.New(fake.App(), fake.Options())
	if err != nil {
		t.Fatal(err)
	}
	return c, fake
}

func TestTheAppsJWTIsSignedWithItsKeyAndLastsNineMinutes(t *testing.T) {
	c, _ := newClient(t)
	raw, err := c.AppJWT()
	if err != nil {
		t.Fatal(err)
	}
	claims := &jwt.RegisteredClaims{}
	tok, err := jwt.ParseWithClaims(raw, claims, func(*jwt.Token) (any, error) { return &githubtest.Key().PublicKey, nil },
		jwt.WithValidMethods([]string{"RS256"}), jwt.WithTimeFunc(func() time.Time { return t0 }))
	if err != nil || !tok.Valid {
		t.Fatalf("parse: %v", err)
	}
	if claims.Issuer != "1234" || !claims.IssuedAt.Time.Equal(t0.Add(-60*time.Second)) || !claims.ExpiresAt.Time.Equal(t0.Add(9*time.Minute)) {
		t.Errorf("iss %q iat %v exp %v", claims.Issuer, claims.IssuedAt, claims.ExpiresAt)
	}
}

func TestAnInstallationTokenIsForOneRepositoryAndKeptUntilItAges(t *testing.T) {
	c, fake := newClient(t)
	ctx := context.Background()
	tok, err := c.InstallationToken(ctx, 42, "shop", github.ContentsRead)
	if err != nil || tok.Value != "ghs_42_1" || !tok.MintedAt.Equal(t0) || !tok.ExpiresAt.Equal(t0.Add(time.Hour)) {
		t.Fatalf("token = %+v, %v", tok, err)
	}
	body := fake.LastTokenRequest()
	if fmt.Sprint(body["repositories"]) != "[shop]" || fmt.Sprint(body["permissions"]) != "map[contents:read]" {
		t.Errorf("asked for %v", body)
	}
	fake.SetNow(t0.Add(49 * time.Minute))
	if again, _ := c.InstallationToken(ctx, 42, "Shop", github.ContentsRead); again.Value != "ghs_42_1" || fake.MintedFor(42) != 1 {
		t.Errorf("a token 49 minutes old is kept, whatever the name's case: %+v, minted %d", again, fake.MintedFor(42))
	}
	fake.SetNow(t0.Add(50 * time.Minute))
	if fresh, _ := c.InstallationToken(ctx, 42, "shop", github.ContentsRead); fresh.Value != "ghs_42_2" {
		t.Errorf("a token 50 minutes old is renewed: %+v", fresh)
	}
}

func TestARepositoryTheInstallationDoesNotGrantIsSaidSo(t *testing.T) {
	c, _ := newClient(t)
	if _, err := c.InstallationToken(context.Background(), 42, "vault", github.ContentsRead); !errors.Is(err, github.ErrRepoNotGranted) {
		t.Errorf("not granted: %v", err)
	}
	_, err := c.InstallationToken(context.Background(), 7, "", github.ContentsRead)
	var apiErr *github.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 404 {
		t.Errorf("an unknown installation: %v", err)
	}
}

func TestRepositoriesPageThroughEverythingTheInstallationGrants(t *testing.T) {
	c, fake := newClient(t)
	var repos []githubtest.Repo
	for i := range 150 {
		repos = append(repos, githubtest.Repo{FullName: fmt.Sprintf("Acme/r%03d", i), Private: i%2 == 0, DefaultBranch: "main"})
	}
	fake.SetRepos(42, repos...)
	got, err := c.Repositories(context.Background(), 42)
	if err != nil || len(got) != 150 || got[149].FullName != "Acme/r149" || got[0].Installation != 42 || !got[0].Private || got[0].DefaultBranch != "main" {
		t.Fatalf("got %d repositories, %v", len(got), err)
	}
	if body := fake.LastTokenRequest(); body["repositories"] != nil {
		t.Errorf("the listing's token names no repository: %v", body)
	}
}

func TestBranchesOfARepository(t *testing.T) {
	c, _ := newClient(t)
	got, err := c.Branches(context.Background(), 42, "Acme", "shop")
	if err != nil || strings.Join(got, ",") != "dev,main" {
		t.Fatalf("%v %v", got, err)
	}
}

func TestTheOAuthCodeTellsWhichInstallationsThePersonSees(t *testing.T) {
	c, _ := newClient(t)
	ctx := context.Background()
	if _, err := c.ExchangeCode(ctx, "stale"); err == nil || !strings.Contains(err.Error(), "incorrect or expired") {
		t.Errorf("a bad code: %v", err)
	}
	token, err := c.ExchangeCode(ctx, githubtest.Code)
	if err != nil || token != githubtest.UserToken {
		t.Fatalf("%q %v", token, err)
	}
	list, err := c.UserInstallations(ctx, token)
	if err != nil || len(list) != 1 || list[0].ID != 42 || list[0].Account != "Acme" || list[0].AccountType != "Organization" || list[0].SuspendedAt != nil {
		t.Fatalf("%+v %v", list, err)
	}
	if _, err := c.UserInstallations(ctx, "someone else's"); err == nil {
		t.Error("another token lists nothing")
	}
}

func TestAManifestCodeBecomesTheAppsCredentials(t *testing.T) {
	fake := githubtest.NewServer(t)
	reg, err := github.ConvertManifest(context.Background(), fake.URL, fake.Client(), githubtest.ConversionCode)
	if err != nil {
		t.Fatal(err)
	}
	if reg.App != fake.App() || reg.Owner != "acme" || reg.Name == "" {
		t.Errorf("registration = %+v", reg)
	}
	if _, err := github.ConvertManifest(context.Background(), fake.URL, fake.Client(), "used"); err == nil {
		t.Error("an unknown code converts nothing")
	}
}

func TestCallsRefuseLoopbackWithoutAnInjectedClient(t *testing.T) {
	fake := githubtest.NewServer(t)
	opts := fake.Options()
	opts.HTTP = nil // the production client: gitauth's dial checks
	c, err := github.New(fake.App(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.InstallationToken(context.Background(), 42, "shop", github.ContentsRead); !errors.Is(err, gitauth.ErrRefusedAddress) {
		t.Errorf("err = %v", err)
	}
	if calls := fake.Calls(); len(calls) != 0 {
		t.Errorf("the fake was reached: %v", calls)
	}
}

func TestA404MeansTheInstallationIsGoneAndA422ThatTheRepositoryIsNotGranted(t *testing.T) {
	c, _ := newClient(t)
	ctx := context.Background()
	for _, repo := range []string{"shop", ""} {
		_, err := c.InstallationToken(ctx, 7, repo, github.ContentsRead)
		if !errors.Is(err, github.ErrInstallationGone) || errors.Is(err, github.ErrRepoNotGranted) {
			t.Errorf("installation 7, repo %q: %v", repo, err)
		}
	}
	_, err := c.InstallationToken(ctx, 42, "vault", github.ContentsRead)
	if !errors.Is(err, github.ErrRepoNotGranted) || errors.Is(err, github.ErrInstallationGone) {
		t.Errorf("a repository not granted: %v", err)
	}
}

func TestWhoThePersonIsAndTheirRoleInAnOrg(t *testing.T) {
	c, fake := newClient(t)
	ctx := context.Background()
	if login, err := c.UserLogin(ctx, githubtest.UserToken); err != nil || login != githubtest.UserLogin {
		t.Fatalf("login %q %v", login, err)
	}
	fake.SetUserLogin("bob")
	if login, _ := c.UserLogin(ctx, githubtest.UserToken); login != "bob" {
		t.Errorf("login %q", login)
	}
	if _, err := c.UserLogin(ctx, "someone else's"); err == nil {
		t.Error("a token GitHub refuses")
	}
	if role, state, err := c.OrgMembership(ctx, githubtest.UserToken, "Acme"); err != nil || role != "admin" || state != "active" {
		t.Errorf("owner of Acme: %q %q %v", role, state, err)
	}
	fake.SetOrgRole("Acme", "member", "active")
	if role, _, err := c.OrgMembership(ctx, githubtest.UserToken, "Acme"); err != nil || role != "member" {
		t.Errorf("member of Acme: %q %v", role, err)
	}
	if role, state, err := c.OrgMembership(ctx, githubtest.UserToken, "Other"); err != nil || role != "" || state != "" {
		t.Errorf("not a member: %q %q %v", role, state, err)
	}
	fake.SetDown(true)
	if _, _, err := c.OrgMembership(ctx, githubtest.UserToken, "Acme"); err == nil {
		t.Error("GitHub down is an error, not a stranger")
	}
}

func TestARateLimitSaysWhenToTryAgain(t *testing.T) {
	c, fake := newClient(t)
	fake.SetRateLimited(2 * time.Minute)
	_, err := c.InstallationToken(context.Background(), 42, "shop", github.ContentsRead)
	var apiErr *github.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusTooManyRequests || apiErr.RetryAfter != 2*time.Minute || !apiErr.RateLimited {
		t.Errorf("Retry-After: %v", err)
	}

	// Without Retry-After, a spent quota says when it resets.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(t0.Add(7*time.Minute).Unix(), 10))
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
	}))
	defer srv.Close()
	c2, err := github.New(fake.App(), github.Options{APIURL: srv.URL, HTTP: srv.Client(), Now: func() time.Time { return t0 }})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c2.InstallationToken(context.Background(), 42, "shop", github.ContentsRead)
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusForbidden || apiErr.RetryAfter != 7*time.Minute || !apiErr.RateLimited {
		t.Errorf("X-RateLimit-Reset: %v", err)
	}
}

func TestAForbiddenCallIsNotARateLimit(t *testing.T) {
	c, fake := newClient(t)
	fake.SetForbidden(true)
	_, err := c.InstallationToken(context.Background(), 42, "shop", github.ContentsRead)
	var apiErr *github.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusForbidden || apiErr.RateLimited {
		t.Errorf("%v", err)
	}
}

func TestCachedInstallationTokenNeverCallsGitHub(t *testing.T) {
	c, fake := newClient(t)
	if _, ok := c.CachedInstallationToken(42, "shop", github.ContentsRead); ok {
		t.Error("nothing is cached yet")
	}
	tok, err := c.InstallationToken(context.Background(), 42, "shop", github.ContentsRead)
	if err != nil {
		t.Fatal(err)
	}
	fake.SetDown(true)
	if got, ok := c.CachedInstallationToken(42, "Shop", github.ContentsRead); !ok || got.Value != tok.Value {
		t.Errorf("cached %+v %v", got, ok)
	}
	fake.SetNow(t0.Add(50 * time.Minute))
	if _, ok := c.CachedInstallationToken(42, "shop", github.ContentsRead); ok {
		t.Error("a token due for renewal is not given")
	}
	if n := len(fake.Calls()); n != 1 {
		t.Errorf("%d calls", n)
	}
}
