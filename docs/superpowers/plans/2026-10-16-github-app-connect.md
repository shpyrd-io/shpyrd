# Connect GitHub, the GitHub App part 3 (RFC-0054) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A workspace connects GitHub once; anyone in it then makes a
project from a repository picked from a list, private repositories build
with hour-long tokens scoped to one repository, and every push deploys
through the App's own webhook, with nothing to paste per project.

**Architecture:** A small `pkg/github` client (the App's JWT, installation
tokens with a cache, listings, the OAuth exchange, the manifest
conversion) dials through `pkg/gitauth`'s checks and reads the App's
credentials from the Secret `shpyrd-system/shpyrd-github-app`, which an
operator writes with `shpyrd-ctl github app create` (or the cloud repository
writes for its one public App). The control-plane database records which
installation belongs to which workspace (`github_installations`, migration
000028). The API connects an installation through GitHub's install page and
a signed, single-use state, after checking with the person's own OAuth token
that they can see it; it lists repositories and branches for the picker, and
takes the App's webhook, which routes a push to every App of the
installation's workspace on that repository through PR 2's `git-push`
annotation. The controller's `lookupGitBuild` tries the App first, so kpack,
BuildKit and the source watcher clone and list with a repository-scoped
token; `<app>-git` is rewritten every 50 minutes. The CLI adds
`shpyrd github`; the dashboard adds Workspace settings → Git, From GitHub in
New project, and the App line in the project's Source block.

**Tech Stack:** Go, gin, cobra, controller-runtime v0.25.1,
`github.com/golang-jwt/jwt/v5` v5.3.1 (already in `go.mod`, made direct),
PostgreSQL through pgx and golang-migrate; Next 16, React 19, TanStack Query,
`@shpyrd/ui`, Vitest.

**Spec:** `docs/superpowers/specs/2026-10-16-github-app-connect-design.md`
(binding), building on
`docs/superpowers/specs/2026-10-16-private-git-repositories-design.md` §3,
PR #223 (git credentials) and PR #225 (the source watcher). Branch:
`feat/github-app-connect`, stacked on PR 2 (#225). Part 4 (checks,
Deployments, watch paths, wait-for-CI) follows separately.

## Global Constraints

- The brand is written `shpyrd`, lowercase, everywhere, including docs, CLI
  output and dashboard copy. `X-Shpyrd-*` headers keep their capitals.
- The spec is binding; where the code disagreed, the deviations below say
  how it was resolved.
- Out of scope, not even as fields or routes: checks, Deployments, statuses
  written back, watch paths, wait-for-CI, pull request previews, GitHub
  Enterprise beyond the API base URL (`SHPYRD_GITHUB_API_URL`), a relay for
  several clusters, and any server-side registration endpoint.
- The App's Secret is `shpyrd-github-app` in `shpyrd-system` with exactly the
  keys `app-id`, `slug`, `private-key`, `webhook-secret`, `client-id`,
  `client-secret`, `html-url`.
- Credential values (the private key, the client secret, the webhook
  secret, installation tokens, the person's OAuth token) never appear in a
  log line, an audit detail, an error or an API answer. `shpyrd-ctl github
  app show` prints the slug, the id and the URL only. The person's OAuth
  token is dropped as soon as `/user/installations` has answered.
- Every call the server makes to GitHub dials through
  `gitauth.SafeHTTPClient` (loopback, unspecified, link-local and metadata
  addresses refused after the name resolves). Tests reach the fake GitHub on
  127.0.0.1 by injecting its client through `github.Options.HTTP`.
- Installation tokens: `POST /app/installations/{id}/access_tokens` with
  `{"repositories":[name],"permissions":{"contents":"read"}}`; kept until 10
  minutes before they expire, or 50 minutes after they were minted, whichever
  comes first. `<app>-git` is rewritten when its token is 50 minutes old.
- The lookup order: the GitHub App (an `https://github.com/...` URL, the App
  Secret present, a live, unsuspended installation of the App's workspace
  whose account equals the repository owner, compared case-insensitively),
  then the project's deploy key, then the workspace's token. Any failure of
  the App falls through.
- The connect state: base64url JSON `{ws, actor, exp, n}` (15 minutes) with
  an HMAC-SHA256 tag keyed by `client-secret`, compared in constant time,
  used once.
- The App's webhook: a per-IP limiter of its own, a 5 MiB body cap,
  `X-Hub-Signature-256` only, 404 while no App Secret exists, 401 for a bad
  signature, 200 `pong` for a ping, 202 for everything that is not applied.
- Audit actions are exactly `github.connect`, `github.disconnect`,
  `github.suspend`, `github.unsuspend` and `github.move`, recorded against
  the cluster with `workspace <slug>` in the detail.
- Messages are plain sentences that name the fix, in the style of
  `internal/controller/build_failure.go` and PR 2's
  `describeSourceError`.
- The dashboard uses only `@shpyrd/ui` components and tokens, calls only
  through `api`, and the Mock answers every new call (`apps/AGENTS.md`).
  `npm run lint`, `npm run typecheck` and `npm run test` pass in
  `apps/workspace`. Vitest tests are named as sentences.
- Every commit ends with these two lines, the first naming the model that
  wrote the commit (for example `Claude Opus 5.5 <noreply@anthropic.com>`):
  ```
  Co-Authored-By: <your model name> <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb
  ```
- The PR carries screenshots of the feature in use (Task 10).

## Deviations, deliberate, and where the code disagreed with the spec

1. **The picker's routes are gated by `authz.ClusterCreate`, not
   `ProjectView`.** `Roles.Can(ProjectView, "")` (`pkg/authz/authz.go:87`)
   is false for every workspace role: with no project in the path only
   platform roles pass. A workspace member, who may create projects
   (`ClusterCreate`), would see an empty picker. `ClusterCreate` is the
   action of `POST /api/projects`, the thing the picker feeds.
2. **Workspace settings → Git is a page for workspace admins** (`needs:
   "admin"`), because `GET /api/workspace/github` is `ClusterAdmin` as the
   spec says. New project learns whether GitHub is connected from the
   repositories listing (`ClusterCreate`): From GitHub is shown when it lists
   at least one repository, or when GitHub failed (502), so the picker can
   show the error.
3. **The state carries a nonce `n` and is used once.** The spec's
   `{ws, actor, exp}` lets a leaked install link connect again for 15
   minutes; the server remembers used nonces until they expire, in memory
   (the server runs one replica, `pkg/api/server.go:225`).
4. **`GET /api/workspace/github` answers 200 with `configured: false` and a
   `note`** when the App Secret is missing, so the page can say it; connect,
   repositories and branches answer 409 as the spec decides. The view also
   carries each installation's `manageURL`.
5. **The token cache's 50-minute bound.** "Until 10 minutes before they
   expire" alone depends on GitHub's clock; with the second bound (50 minutes
   after minting, on shpyrd's clock) the controller's rewrite at 50 minutes
   always gets a new token.
6. **No second test hook.** `allowLoopbackForTests` is unexported in
   `pkg/gitauth`; rather than export a global, `gitauth` exports
   `SafeHTTPClient(timeout)`, `pkg/github` uses it by default, and tests pass
   the fake's own client in `github.Options.HTTP`. One test proves the default
   refuses loopback. `shpyrd-ctl` runs on the operator's machine and uses a
   plain client with a timeout for the one conversion call.
7. **The App's webhook checks `X-Hub-Signature-256` alone**
   (`gitauth.VerifyGitHubDelivery`, new). `VerifyDelivery` would also take a
   GitLab token equal to the secret or a Gitea signature.
8. **The views read what builds clone with.** `GET
   /api/projects/:slug/git-auth` says `using: "github-app"` and
   `githubAccount` from the annotations the controller writes on `<app>-git`;
   `shpyrd git-auth show`, `shpyrd git-hooks show` and the dashboard's Source
   block read it there. No CRD field is added.
9. **Why the App could not read a repository is said.** When the App was
   tried and the listing then fails for access, the watcher's message starts
   with the App's reason (`Build.AppRefused`): not granted, suspended, or no
   token from GitHub. A suspended installation is still skipped by the
   lookup, as decided.
10. **`request_oauth_on_install` sends the browser to `callback_urls[0]`,
    not `setup_url`.** Both are `<dashboard>/api/github/installed` in the
    manifest, as the spec has them, so either way it arrives. An
    `setup_action=update` arrives from GitHub's own settings with no state:
    it changes nothing and only redirects.
11. **GitHub's redirect to `/created` is a GET** (it sends the browser), so
    the `shpyrd-ctl` test simulates the browser with GETs to the local server.
    `shpyrd-ctl github app create` refuses when the Secret already exists,
    naming `shpyrd-ctl github app delete`.
12. **Disconnect removes the row; GitHub's `installation.deleted` marks
    `deleted_at`.** A row marked deleted is revived by a new connect.
13. **The webhook's per-IP limit is 3000 a minute**, not PR 2's 300: every
    workspace's pushes arrive from GitHub's few hook addresses.
14. **The OAuth host is the `html-url`'s scheme and host** (`github.com`,
    the fake in tests), so nothing else is configured.
15. **A fake GitHub, `pkg/github/githubtest`,** is shared by the tests of
    `pkg/github`, `pkg/api`, `internal/controller` and `internal/cli`.
    `pkg/github`'s own tests are an external test package (`github_test`):
    `githubtest` imports `github`, and an internal test importing it would be
    an import cycle.
16. **`shpyrd github connect` waits for an installation that was not there
    when it started.** Installing again one already connected here changes
    nothing to wait for.

## Review Focus

1. **A state replayed, or an installation id forged in the callback URL.**
   Anyone can open `/api/github/installed?installation_id=…` with a state
   copied from a browser history. Expected: only an installation the person's
   own GitHub account can see is connected, and a state connects once.
   Tests: `TestGitHubCallbackRefusesAnInstallationThePersonCannotSee` and
   `TestGitHubStateIsUsedOnceAndExpires` (Task 4).
2. **An installation moved between workspaces.** An admin of workspace B
   connects the installation workspace A had. Expected: it moves, the move is
   audited (`github.move`), A's builds stop using it and fall through, A's
   pushes are no longer routed. Tests:
   `TestGitHubInstallationMovesBetweenWorkspaces` (Task 2),
   `TestGitHubConnectMovesAnInstallationAndAudits` (Task 4),
   `TestGitHubAppIsSkippedForAnInstallationMovedAway` (Task 6).
3. **A token expiring during kpack's own rebuild.** kpack rebuilds when its
   stack or buildpacks change, reading `<app>-git` whenever it starts.
   Expected: the Secret never holds a token in its last 10 minutes. Tests:
   `TestAnInstallationTokenIsForOneRepositoryAndKeptUntilItAges` (Task 1),
   `TestGitHubTokenIsRewrittenFiftyMinutesAfterItWasMinted` and
   `TestAnAppCloningThroughGitHubIsRequeuedBeforeItsTokenAges` (Task 6).
4. **An organisation name in another case.** GitHub logins and repository
   names are case-insensitive: the URL says `acme/shop`, GitHub says
   `Acme/Shop`. Expected: the build uses the App and the push deploys.
   Tests: `TestGitHubAppClonesWithATokenForThatRepositoryAlone` (Task 6) and
   `TestGitHubWebhookRoutesAPushToTheProjectsOnThatRepositoryWhateverTheCase`
   (Task 5).
5. **The webhook arriving before the setup callback.** GitHub sends
   `installation.created` and may send a push before the person's browser
   comes back. Expected: answered 202 and nothing written; once connected,
   the next push deploys. Test:
   `TestGitHubWebhookBeforeTheCallbackIsAcceptedAndIgnored` (Task 5).

Also pinned by tests, outside the five: a suspended installation
(`TestGitHubAppIsSkippedWhileSuspended`, Task 6;
`TestGitHubWebhookRecordsInstallationEvents`, Task 5), GitHub down during the
callback (`TestGitHubCallbackSaysToTryAgainWhenGitHubIsDown`, Task 4), and a
token credential replacing the App
(`TestATokenReplacingTheGitHubAppDropsItsMarks`, Task 6).

---

## File structure

| File | Responsibility |
|---|---|
| `pkg/gitauth/dial.go` (modify, `newHTTPClient` ~96-107), `dial_test.go` (modify) | `SafeHTTPClient`, the dial checks for any caller |
| `pkg/gitauth/resolve.go` (modify, kinds ~14-17, annotations ~20-41, `Build` ~70-84) | `KindGitHubApp`, the App's annotations on `<app>-git`, `Build.AppRefused` |
| `pkg/gitauth/webhook.go` (modify, after `VerifyDelivery` ~52), `webhook_test.go` (modify) | `VerifyGitHubDelivery` |
| `pkg/github/app.go` (new) | The App's credentials: Secret keys, `AppFromSecret`, `SecretData`, `WebURL`, `InstallURL` |
| `pkg/github/client.go` (new) | `Client`: the JWT, installation tokens and their cache, repositories, branches, the OAuth exchange, a person's installations, `ConvertManifest` |
| `pkg/github/source.go` (new) | `Source`: the Secret read at most every 30 s, one `Client` per version of it |
| `pkg/github/githubtest/fake.go` (new) | A fake GitHub for every package's tests |
| `pkg/github/client_test.go`, `source_test.go` (new) | Tests, package `github_test` |
| `go.mod`, `go.sum` | `golang-jwt/jwt/v5` direct |
| `pkg/store/github.go` (new) | `GitHubInstallation`, the `GitHub` sub-interface |
| `pkg/store/github_memory.go`, `github_postgres.go` (new) | The two implementations |
| `pkg/store/migrations/000028_github_installations.up.sql`, `.down.sql` (new) | The table |
| `pkg/store/store.go` (modify, `Store` ~988), `memory.go` (modify, `Memory` 29-53), `one.go` (modify, after `WorkspaceByHost` ~60) | Embed `GitHub`; memory field; the single-workspace override |
| `pkg/store/github_test.go` (new), `store_test.go` (modify, `dropAll` ~42) | Both backends, the move, the single-workspace filter |
| `internal/cli/ctl_github.go` (new), `ctl_github_test.go` (new), `ctl.go` (modify, ~45) | `shpyrd-ctl github app create|show|delete` |
| `pkg/api/github.go` (new), `github_test.go` (new) | Connect, the state, the callback, list, disconnect, repositories, branches |
| `pkg/api/github_webhook.go` (new), `github_webhook_test.go` (new) | The App's webhook |
| `pkg/api/server.go` (modify: fields ~284, init ~388, routes ~586, ~600, ~665) | Wire them |
| `internal/controller/github.go` (new), `github_test.go` (new) | `GitHubApp`, the App's credential, the 50-minute refresh |
| `internal/controller/gitcreds.go` (modify, 26-60, 120-140), `source_controller.go` (modify, `describeSourceError` 447-468, `poll` 471-494), `app_controller.go` (modify, Reconcile ~278), `desired.go` (modify, `Config` ~88) | Use it |
| `internal/controller/source_controller_test.go` (modify, 416-420) | The new `lookupGitBuild` signature |
| `pkg/server/server.go` (modify, ~527 and ~545-555) | Give the controllers the App |
| `pkg/api/gitauth.go` (modify, `ProjectGitAuthView` 54-59, `showProjectGitAuth` 190-224), `gitauth_test.go` (modify) | `using: "github-app"` |
| `internal/cli/gitauth.go` (modify, show ~167-180), `gitauth_test.go` (modify) | `git-auth show` says so |
| `internal/cli/github.go` (new), `github_test.go` (new), `root.go` (modify, ~132) | `shpyrd github connect|list|disconnect|repos` |
| `internal/cli/githooks.go` (modify, show and `printGitHooks`), `githooks_test.go` (modify) | "Pushes arrive through the GitHub App" |
| `apps/workspace/src/api/types.ts`, `api.ts`, `backend.ts`, `mock.ts`, `mock/things.json` (modify) | Types, calls, Mock, fixtures |
| `apps/workspace/src/lib/go.ts` (new) | Sending the browser to GitHub, replaceable in tests |
| `apps/workspace/src/screens/workspace/git.tsx`, `git.test.tsx` (new); `index.tsx` (modify) | Workspace settings → Git |
| `apps/workspace/src/screens/github-picker.tsx`, `github-picker.test.tsx` (new); `launcher.tsx` (modify, `NewProject` 132-233) | From GitHub in New project |
| `apps/workspace/src/screens/project/source.tsx`, `source.test.tsx` (modify) | "Through the GitHub App (acme)" |
| `apps/workspace/src/api/api.test.ts` (modify) | The Mock connects and lists |
| `content/docs/deploying.mdx`, `installation.mdx`, `cli.mdx` (modify) | Connect GitHub; the operator's App; the commands |
| `rfcs/0054-github-app.md`, `rfcs/README.md:98` (modify) | In progress |

---
### Task 1: `pkg/github`: the App's JWT, installation tokens, listings, the OAuth exchange

**Files:**
- Modify: `pkg/gitauth/dial.go` (`newHTTPClient`, ~96-107), `pkg/gitauth/dial_test.go`
- Create: `pkg/github/app.go`, `pkg/github/client.go`, `pkg/github/source.go`
- Create: `pkg/github/githubtest/fake.go`
- Test: `pkg/github/client_test.go`, `pkg/github/source_test.go` (package `github_test`)
- Modify: `go.mod`, `go.sum` (`go mod tidy`)

**Interfaces:**
- Consumes: `gitauth.ErrRefusedAddress`; the unexported `newDialer`, `checkRedirect` of `pkg/gitauth/dial.go`.
- Produces:
  - `func gitauth.SafeHTTPClient(timeout time.Duration) *http.Client`
  - `const github.SecretName = "shpyrd-github-app"`, `github.KeyAppID = "app-id"`, `KeySlug = "slug"`, `KeyPrivateKey = "private-key"`, `KeyWebhookSecret = "webhook-secret"`, `KeyClientID = "client-id"`, `KeyClientSecret = "client-secret"`, `KeyHTMLURL = "html-url"`, `EnvAPIURL = "SHPYRD_GITHUB_API_URL"`, `DefaultAPIURL = "https://api.github.com"`
  - `var github.ErrNotConfigured`, `github.ErrRepoNotGranted`
  - `type github.App struct { ID int64; Slug, PrivateKey, WebhookSecret, ClientID, ClientSecret, HTMLURL string }`, `func github.AppFromSecret(*corev1.Secret) (App, error)`, `func (App) SecretData() map[string][]byte`, `func (App) WebURL() string`, `func (App) InstallURL() string`
  - `type github.Options struct { APIURL, WebURL string; HTTP *http.Client; Now func() time.Time }`
  - `func github.New(App, Options) (*Client, error)`; methods `App() App`, `AppJWT() (string, error)`, `InstallationToken(ctx, installation int64, repo string) (Token, error)`, `Repositories(ctx, installation int64) ([]Repository, error)`, `Branches(ctx, installation int64, owner, repo string) ([]string, error)`, `ExchangeCode(ctx, code string) (string, error)`, `UserInstallations(ctx, userToken string) ([]Installation, error)`
  - `type github.Token struct { Value string; ExpiresAt, MintedAt time.Time }`
  - `type github.Repository struct { Installation int64; FullName string; Private bool; DefaultBranch string }`
  - `type github.Installation struct { ID int64; Account, AccountType string; SuspendedAt *time.Time }`
  - `type github.APIError struct { Status int; Message string }`
  - `type github.Registration struct { App App; Name, Owner string }`, `func github.ConvertManifest(ctx, apiURL string, hc *http.Client, code string) (Registration, error)`
  - `type github.Source struct { Reader client.Reader; Namespace string; Options Options }`, `func (*Source) Client(ctx) (*Client, error)`
  - `githubtest`: `NewServer(testing.TB) *Server`; constants `AppID = 1234`, `Slug = "shpyrd-test"`, `WebhookSecret`, `ClientID`, `ClientSecret`, `Code = "good-code"`, `UserToken = "gho_person"`, `ConversionCode = "c0de"`; `Key() *rsa.PrivateKey`, `KeyPEM() string`; types `Repo{FullName string; Private bool; DefaultBranch string}`, `Installation{ID int64; Login, Type string; Suspended bool}`; methods `App() github.App`, `Secret(namespace) *corev1.Secret`, `Options() github.Options`, `Source(client.Reader, namespace) *github.Source`, `Time() time.Time`, `SetNow(time.Time)`, `SetDown(bool)`, `SetRepos(int64, ...Repo)`, `SetUserInstallations(...Installation)`, `MintedFor(int64) int`, `LastTokenRequest() map[string]any`, `Calls() []string`. Installation 42 on `Acme` (Organization) grants `Acme/shop` (private, `main`; branches `dev`, `main`) and `Acme/site` (`trunk`); tokens are `ghs_<installation>_<n>`, expiring an hour after the fake's clock.

- [ ] **Step 1: Write the failing tests**

Append to `pkg/gitauth/dial_test.go`:

```go
func TestSafeHTTPClientRefusesLoopback(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit = true }))
	defer srv.Close()
	refuseLoopback(t)
	_, err := SafeHTTPClient(time.Second).Get(srv.URL)
	if !errors.Is(err, ErrRefusedAddress) || hit {
		t.Errorf("err = %v, reached %v", err, hit)
	}
}
```

(add `"time"` to its imports).

`pkg/github/client_test.go`:

```go
package github_test

import (
	"context"
	"errors"
	"fmt"
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
	tok, err := c.InstallationToken(ctx, 42, "shop")
	if err != nil || tok.Value != "ghs_42_1" || !tok.MintedAt.Equal(t0) || !tok.ExpiresAt.Equal(t0.Add(time.Hour)) {
		t.Fatalf("token = %+v, %v", tok, err)
	}
	body := fake.LastTokenRequest()
	if fmt.Sprint(body["repositories"]) != "[shop]" || fmt.Sprint(body["permissions"]) != "map[contents:read]" {
		t.Errorf("asked for %v", body)
	}
	fake.SetNow(t0.Add(49 * time.Minute))
	if again, _ := c.InstallationToken(ctx, 42, "Shop"); again.Value != "ghs_42_1" || fake.MintedFor(42) != 1 {
		t.Errorf("a token 49 minutes old is kept, whatever the name's case: %+v, minted %d", again, fake.MintedFor(42))
	}
	fake.SetNow(t0.Add(50 * time.Minute))
	if fresh, _ := c.InstallationToken(ctx, 42, "shop"); fresh.Value != "ghs_42_2" {
		t.Errorf("a token 50 minutes old is renewed: %+v", fresh)
	}
}

func TestARepositoryTheInstallationDoesNotGrantIsSaidSo(t *testing.T) {
	c, _ := newClient(t)
	if _, err := c.InstallationToken(context.Background(), 42, "vault"); !errors.Is(err, github.ErrRepoNotGranted) {
		t.Errorf("not granted: %v", err)
	}
	_, err := c.InstallationToken(context.Background(), 7, "")
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
	if _, err := c.InstallationToken(context.Background(), 42, "shop"); !errors.Is(err, gitauth.ErrRefusedAddress) {
		t.Errorf("err = %v", err)
	}
	if calls := fake.Calls(); len(calls) != 0 {
		t.Errorf("the fake was reached: %v", calls)
	}
}
```

`pkg/github/source_test.go`:

```go
package github_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	crfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/shpyrd-io/shpyrd/pkg/github"
	"github.com/shpyrd-io/shpyrd/pkg/github/githubtest"
)

func TestTheSourceKeepsOneClientUntilTheSecretChanges(t *testing.T) {
	fake := githubtest.NewServer(t)
	fake.SetNow(t0)
	c := crfake.NewClientBuilder().WithObjects(fake.Secret("shpyrd-system")).Build()
	src := fake.Source(c, "shpyrd-system")
	ctx := context.Background()
	first, err := src.Client(ctx)
	if err != nil || first.App().Slug != githubtest.Slug {
		t.Fatalf("first: %v", err)
	}
	if again, _ := src.Client(ctx); again != first {
		t.Error("the same Secret, the same client, and its tokens")
	}
	key := types.NamespacedName{Namespace: "shpyrd-system", Name: github.SecretName}
	sec := &corev1.Secret{}
	if err := c.Get(ctx, key, sec); err != nil {
		t.Fatal(err)
	}
	sec.Data[github.KeySlug] = []byte("renamed")
	if err := c.Update(ctx, sec); err != nil {
		t.Fatal(err)
	}
	if soon, _ := src.Client(ctx); soon != first {
		t.Error("the Secret is read again at most every 30 s")
	}
	fake.SetNow(t0.Add(31 * time.Second))
	changed, err := src.Client(ctx)
	if err != nil || changed == first || changed.App().Slug != "renamed" {
		t.Fatalf("a changed Secret, a new client: %v", err)
	}
	if err := c.Delete(ctx, sec); err != nil {
		t.Fatal(err)
	}
	fake.SetNow(t0.Add(62 * time.Second))
	if _, err := src.Client(ctx); !errors.Is(err, github.ErrNotConfigured) {
		t.Errorf("deleted: %v", err)
	}
}

func TestAnIncompleteSecretIsNoApp(t *testing.T) {
	fake := githubtest.NewServer(t)
	sec := fake.Secret("shpyrd-system")
	delete(sec.Data, github.KeyPrivateKey)
	if _, err := github.AppFromSecret(sec); !errors.Is(err, github.ErrNotConfigured) || !strings.Contains(err.Error(), "private-key") {
		t.Errorf("err = %v", err)
	}
	if _, err := (&github.Source{}).Client(context.Background()); !errors.Is(err, github.ErrNotConfigured) {
		t.Errorf("no reader: %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./pkg/gitauth/ ./pkg/github/...`
Expected: FAIL to compile: `undefined: SafeHTTPClient`, `no required module provides package github.com/shpyrd-io/shpyrd/pkg/github`.

- [ ] **Step 3: Export the safe client from `pkg/gitauth`**

In `pkg/gitauth/dial.go`, replace `newHTTPClient` with:

```go
// SafeHTTPClient is an HTTP client for hosts a customer or a setting
// names: it dials only addresses shpyrd connects to, follows a redirect
// only on the same host and never from https to http, and gives up after
// timeout. The GitHub App's calls go through it too (RFC-0054).
func SafeHTTPClient(timeout time.Duration) *http.Client {
	tr := &http.Transport{
		Proxy:                 nil,
		DialContext:           newDialer().DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          20,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	return &http.Client{Timeout: timeout, Transport: tr, CheckRedirect: checkRedirect}
}

func newHTTPClient() *http.Client { return SafeHTTPClient(lsRemoteTimeout) }
```

- [ ] **Step 4: Write `pkg/github/app.go`**

```go
// Package github is shpyrd's GitHub App (RFC-0054): the App's JWT,
// installation tokens scoped to one repository, the repositories and
// branches an installation grants, the OAuth exchange that says which
// installations a person can see, and the conversion of a registration
// manifest. Every call the server makes dials through gitauth's checks.
package github

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// The App's Secret in the system namespace: written by
// `shpyrd-ctl github app create`, or by the cloud for its one public App.
const (
	SecretName       = "shpyrd-github-app"
	KeyAppID         = "app-id"
	KeySlug          = "slug"
	KeyPrivateKey    = "private-key"
	KeyWebhookSecret = "webhook-secret"
	KeyClientID      = "client-id"
	KeyClientSecret  = "client-secret"
	KeyHTMLURL       = "html-url"

	// EnvAPIURL names GitHub's API; DefaultAPIURL when unset.
	EnvAPIURL     = "SHPYRD_GITHUB_API_URL"
	DefaultAPIURL = "https://api.github.com"
)

var (
	// ErrNotConfigured: the platform has no GitHub App, or its Secret
	// cannot be used.
	ErrNotConfigured = errors.New("GitHub is not set up on this platform")
	// ErrRepoNotGranted: the installation does not include the repository.
	ErrRepoNotGranted = errors.New("the GitHub App's installation does not include this repository")
)

// App is the App's credentials, as its Secret holds them.
type App struct {
	ID            int64
	Slug          string
	PrivateKey    string
	WebhookSecret string
	ClientID      string
	ClientSecret  string
	HTMLURL       string
}

// AppFromSecret reads the App's Secret. A Secret that lacks a key is no
// App: the error wraps ErrNotConfigured and names the key.
func AppFromSecret(sec *corev1.Secret) (App, error) {
	get := func(k string) string { return strings.TrimSpace(string(sec.Data[k])) }
	id, err := strconv.ParseInt(get(KeyAppID), 10, 64)
	if err != nil || id <= 0 {
		return App{}, fmt.Errorf("%w: the Secret %s lacks %s", ErrNotConfigured, SecretName, KeyAppID)
	}
	app := App{ID: id, Slug: get(KeySlug), PrivateKey: string(sec.Data[KeyPrivateKey]), WebhookSecret: get(KeyWebhookSecret),
		ClientID: get(KeyClientID), ClientSecret: get(KeyClientSecret), HTMLURL: strings.TrimSuffix(get(KeyHTMLURL), "/")}
	if err := app.validate(); err != nil {
		return App{}, fmt.Errorf("%w: the Secret %s %v", ErrNotConfigured, SecretName, err)
	}
	return app, nil
}

func (a App) validate() error {
	if a.ID <= 0 {
		return errors.New("lacks " + KeyAppID)
	}
	for _, k := range []struct{ name, value string }{
		{KeySlug, a.Slug}, {KeyPrivateKey, a.PrivateKey}, {KeyWebhookSecret, a.WebhookSecret},
		{KeyClientID, a.ClientID}, {KeyClientSecret, a.ClientSecret}, {KeyHTMLURL, a.HTMLURL},
	} {
		if strings.TrimSpace(k.value) == "" {
			return errors.New("lacks " + k.name)
		}
	}
	return nil
}

// SecretData is the App as its Secret's data.
func (a App) SecretData() map[string][]byte {
	return map[string][]byte{
		KeyAppID: []byte(strconv.FormatInt(a.ID, 10)), KeySlug: []byte(a.Slug), KeyPrivateKey: []byte(a.PrivateKey),
		KeyWebhookSecret: []byte(a.WebhookSecret), KeyClientID: []byte(a.ClientID), KeyClientSecret: []byte(a.ClientSecret),
		KeyHTMLURL: []byte(a.HTMLURL),
	}
}

// WebURL is the GitHub the App lives on, from its html-url: where people
// sign in, and where the OAuth exchange goes.
func (a App) WebURL() string {
	u, err := url.Parse(a.HTMLURL)
	if err != nil || u.Host == "" {
		return "https://github.com"
	}
	return u.Scheme + "://" + u.Host
}

// InstallURL is GitHub's page that installs the App on an account.
func (a App) InstallURL() string { return a.HTMLURL + "/installations/new" }
```

- [ ] **Step 5: Write `pkg/github/client.go`**

```go
package github

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
)

const (
	callTimeout = 20 * time.Second
	// maxBody caps what one answer of GitHub may be.
	maxBody  = 8 << 20
	perPage  = 100
	maxPages = 100
	// A token is kept until 10 minutes before it expires, or 50 minutes
	// after it was minted, whichever comes first: a build reading it
	// never gets one in its last 10 minutes.
	tokenMargin = 10 * time.Minute
	tokenLife   = 50 * time.Minute
)

// Options says where GitHub is and how to reach it.
type Options struct {
	// APIURL is GitHub's API, DefaultAPIURL when empty.
	APIURL string
	// WebURL is where the OAuth exchange goes; empty is the App's WebURL.
	WebURL string
	// HTTP makes the calls; nil is gitauth.SafeHTTPClient. Tests pass the
	// fake GitHub's own client.
	HTTP *http.Client
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// Client speaks to GitHub as the App.
type Client struct {
	app  App
	key  *rsa.PrivateKey
	api  string
	web  string
	http *http.Client
	now  func() time.Time

	mu     sync.Mutex
	tokens map[tokenKey]Token
}

type tokenKey struct {
	installation int64
	repo         string
}

// Token is an installation token: for one repository, or for the whole
// installation when it was asked for none.
type Token struct {
	Value     string
	ExpiresAt time.Time
	MintedAt  time.Time
}

func (t Token) renewAt() time.Time {
	byExpiry, byAge := t.ExpiresAt.Add(-tokenMargin), t.MintedAt.Add(tokenLife)
	if byAge.Before(byExpiry) {
		return byAge
	}
	return byExpiry
}

// Repository is a repository an installation grants.
type Repository struct {
	Installation  int64
	FullName      string
	Private       bool
	DefaultBranch string
}

// Installation is an installation of the App a person can see.
type Installation struct {
	ID          int64
	Account     string
	AccountType string
	SuspendedAt *time.Time
}

// APIError is GitHub refusing a call: its status and its words.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return fmt.Sprintf("GitHub answered %d: %s", e.Status, e.Message) }

// New makes a client of the App. A private key that is not an RSA key in
// PEM is no App (ErrNotConfigured).
func New(app App, opts Options) (*Client, error) {
	key, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(app.PrivateKey))
	if err != nil {
		return nil, fmt.Errorf("%w: the Secret %s's %s is not an RSA key in PEM", ErrNotConfigured, SecretName, KeyPrivateKey)
	}
	c := &Client{
		app: app, key: key,
		api:  strings.TrimSuffix(firstNonEmpty(opts.APIURL, DefaultAPIURL), "/"),
		web:  strings.TrimSuffix(firstNonEmpty(opts.WebURL, app.WebURL()), "/"),
		http: opts.HTTP, now: opts.Now, tokens: map[tokenKey]Token{},
	}
	if c.http == nil {
		c.http = gitauth.SafeHTTPClient(callTimeout)
	}
	if c.now == nil {
		c.now = time.Now
	}
	return c, nil
}

// App is the App this client speaks as.
func (c *Client) App() App { return c.app }

// AppJWT is the App's own credential: RS256, issued a minute ago against
// clock drift, valid nine minutes (GitHub takes ten at most).
func (c *Client) AppJWT() (string, error) {
	now := c.now()
	claims := jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(now.Add(-60 * time.Second)),
		ExpiresAt: jwt.NewNumericDate(now.Add(9 * time.Minute)),
		Issuer:    strconv.FormatInt(c.app.ID, 10),
	}
	return jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(c.key)
}

// InstallationToken is a token of the installation that reads one
// repository (by its name, without the owner), or lists the whole
// installation when repo is "". It is kept and given again until it ages.
// A repository the installation does not include is ErrRepoNotGranted.
func (c *Client) InstallationToken(ctx context.Context, installation int64, repo string) (Token, error) {
	key := tokenKey{installation, strings.ToLower(repo)}
	c.mu.Lock()
	if t, ok := c.tokens[key]; ok && c.now().Before(t.renewAt()) {
		c.mu.Unlock()
		return t, nil
	}
	c.mu.Unlock()
	bearer, err := c.AppJWT()
	if err != nil {
		return Token{}, err
	}
	body := map[string]any{"permissions": map[string]string{"contents": "read"}}
	if repo != "" {
		body["repositories"] = []string{repo}
	}
	var out struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	err = c.call(ctx, "POST", fmt.Sprintf("%s/app/installations/%d/access_tokens", c.api, installation), bearer, body, &out)
	if apiErr, ok := err.(*APIError); ok && repo != "" && (apiErr.Status == http.StatusNotFound || apiErr.Status == http.StatusUnprocessableEntity) {
		return Token{}, ErrRepoNotGranted
	}
	if err != nil {
		return Token{}, err
	}
	t := Token{Value: out.Token, ExpiresAt: out.ExpiresAt, MintedAt: c.now()}
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, old := range c.tokens {
		if !t.MintedAt.Before(old.ExpiresAt) {
			delete(c.tokens, k)
		}
	}
	c.tokens[key] = t
	return t, nil
}

// Repositories pages through everything the installation grants.
func (c *Client) Repositories(ctx context.Context, installation int64) ([]Repository, error) {
	tok, err := c.InstallationToken(ctx, installation, "")
	if err != nil {
		return nil, err
	}
	out := []Repository{}
	for page := 1; page <= maxPages; page++ {
		var res struct {
			Total        int `json:"total_count"`
			Repositories []struct {
				FullName      string `json:"full_name"`
				Private       bool   `json:"private"`
				DefaultBranch string `json:"default_branch"`
			} `json:"repositories"`
		}
		if err := c.call(ctx, "GET", fmt.Sprintf("%s/installation/repositories?per_page=%d&page=%d", c.api, perPage, page), tok.Value, nil, &res); err != nil {
			return nil, err
		}
		for _, r := range res.Repositories {
			out = append(out, Repository{Installation: installation, FullName: r.FullName, Private: r.Private, DefaultBranch: r.DefaultBranch})
		}
		if len(res.Repositories) == 0 || len(out) >= res.Total {
			break
		}
	}
	return out, nil
}

// Branches are a repository's branch names, as GitHub orders them.
func (c *Client) Branches(ctx context.Context, installation int64, owner, repo string) ([]string, error) {
	tok, err := c.InstallationToken(ctx, installation, repo)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for page := 1; page <= 10; page++ {
		var res []struct {
			Name string `json:"name"`
		}
		endpoint := fmt.Sprintf("%s/repos/%s/%s/branches?per_page=%d&page=%d", c.api, url.PathEscape(owner), url.PathEscape(repo), perPage, page)
		if err := c.call(ctx, "GET", endpoint, tok.Value, nil, &res); err != nil {
			return nil, err
		}
		for _, b := range res {
			out = append(out, b.Name)
		}
		if len(res) < perPage {
			break
		}
	}
	return out, nil
}

// ExchangeCode turns the code GitHub gave the browser into the person's
// own token, which only says who they are: the caller drops it once it has
// asked.
func (c *Client) ExchangeCode(ctx context.Context, code string) (string, error) {
	raw, _ := json.Marshal(map[string]string{"client_id": c.app.ClientID, "client_secret": c.app.ClientSecret, "code": code})
	req, err := http.NewRequestWithContext(ctx, "POST", c.web+"/login/oauth/access_token", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "shpyrd")
	var res struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if err := c.send(req, &res); err != nil {
		return "", err
	}
	if res.AccessToken == "" {
		return "", fmt.Errorf("GitHub did not accept the sign-in: %s", firstNonEmpty(res.Description, res.Error, "it gave no token"))
	}
	return res.AccessToken, nil
}

// UserInstallations are the installations of the App the person behind
// userToken can see.
func (c *Client) UserInstallations(ctx context.Context, userToken string) ([]Installation, error) {
	out := []Installation{}
	for page := 1; page <= 10; page++ {
		var res struct {
			Total         int `json:"total_count"`
			Installations []struct {
				ID      int64 `json:"id"`
				Account struct {
					Login string `json:"login"`
					Type  string `json:"type"`
				} `json:"account"`
				SuspendedAt *time.Time `json:"suspended_at"`
			} `json:"installations"`
		}
		if err := c.call(ctx, "GET", fmt.Sprintf("%s/user/installations?per_page=%d&page=%d", c.api, perPage, page), userToken, nil, &res); err != nil {
			return nil, err
		}
		for _, i := range res.Installations {
			out = append(out, Installation{ID: i.ID, Account: i.Account.Login, AccountType: i.Account.Type, SuspendedAt: i.SuspendedAt})
		}
		if len(res.Installations) == 0 || len(out) >= res.Total {
			break
		}
	}
	return out, nil
}

// Registration is a GitHub App GitHub created from a manifest.
type Registration struct {
	App   App
	Name  string
	Owner string
}

// ConvertManifest asks GitHub for the credentials of the App it created
// from a manifest (no authentication: the code is the credential, once).
// hc nil is a plain client with a timeout: this runs on the operator's
// machine.
func ConvertManifest(ctx context.Context, apiURL string, hc *http.Client, code string) (Registration, error) {
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	c := &Client{api: strings.TrimSuffix(firstNonEmpty(apiURL, DefaultAPIURL), "/"), http: hc, now: time.Now}
	var res struct {
		ID    int64  `json:"id"`
		Slug  string `json:"slug"`
		Name  string `json:"name"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
		HTMLURL       string `json:"html_url"`
		ClientID      string `json:"client_id"`
		ClientSecret  string `json:"client_secret"`
		WebhookSecret string `json:"webhook_secret"`
		PEM           string `json:"pem"`
	}
	if err := c.call(ctx, "POST", c.api+"/app-manifests/"+url.PathEscape(code)+"/conversions", "", nil, &res); err != nil {
		return Registration{}, err
	}
	app := App{ID: res.ID, Slug: res.Slug, PrivateKey: res.PEM, WebhookSecret: res.WebhookSecret,
		ClientID: res.ClientID, ClientSecret: res.ClientSecret, HTMLURL: strings.TrimSuffix(res.HTMLURL, "/")}
	if err := app.validate(); err != nil {
		return Registration{}, fmt.Errorf("GitHub's answer %v", err)
	}
	return Registration{App: app, Name: res.Name, Owner: res.Owner.Login}, nil
}

// call sends a JSON request to GitHub's API.
func (c *Client) call(ctx context.Context, method, endpoint, bearer string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "shpyrd")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	return c.send(req, out)
}

// send makes the call and reads the answer; a status that is not 2xx is
// an APIError with GitHub's words.
func (c *Client) send(req *http.Request, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		var e struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &e)
		return &APIError{Status: resp.StatusCode, Message: firstNonEmpty(e.Message, http.StatusText(resp.StatusCode))}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
```

- [ ] **Step 6: Write `pkg/github/source.go`**

```go
package github

import (
	"context"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// recheck is how long the Secret read last stands: a new App or a removed
// one is seen within it.
const recheck = 30 * time.Second

// Source is the App as its Secret says, for the server and the
// controllers: the Secret read at most every 30 s, and one Client per
// version of it, so its tokens are kept.
type Source struct {
	Reader    client.Reader
	Namespace string
	Options   Options

	mu      sync.Mutex
	client  *Client
	version string
	missing bool
	checked time.Time
}

// Client is the App's client; ErrNotConfigured while there is no usable
// Secret.
func (s *Source) Client(ctx context.Context) (*Client, error) {
	if s == nil || s.Reader == nil {
		return nil, ErrNotConfigured
	}
	now := time.Now()
	if s.Options.Now != nil {
		now = s.Options.Now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.checked.IsZero() && now.Sub(s.checked) < recheck {
		if s.missing {
			return nil, ErrNotConfigured
		}
		if s.client != nil {
			return s.client, nil
		}
	}
	sec := &corev1.Secret{}
	err := s.Reader.Get(ctx, types.NamespacedName{Namespace: s.Namespace, Name: SecretName}, sec)
	if apierrors.IsNotFound(err) {
		s.client, s.version, s.missing, s.checked = nil, "", true, now
		return nil, ErrNotConfigured
	}
	if err != nil {
		return nil, err
	}
	s.missing, s.checked = false, now
	if s.client != nil && sec.ResourceVersion == s.version {
		return s.client, nil
	}
	app, err := AppFromSecret(sec)
	if err != nil {
		return nil, err
	}
	c, err := New(app, s.Options)
	if err != nil {
		return nil, err
	}
	s.client, s.version = c, sec.ResourceVersion
	return c, nil
}
```

- [ ] **Step 7: Write the fake GitHub, `pkg/github/githubtest/fake.go`**

```go
// Package githubtest is a fake GitHub for shpyrd's tests: the App's token
// endpoint (which checks the App's JWT), an installation's repositories and
// branches, the OAuth exchange, a person's installations and the manifest
// conversion. Its state is read and set through methods, under its lock.
package githubtest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/shpyrd-io/shpyrd/pkg/github"
)

// The App the fake knows, and the codes it accepts.
const (
	AppID          = 1234
	Slug           = "shpyrd-test"
	WebhookSecret  = "whsec-test"
	ClientID       = "Iv1.test"
	ClientSecret   = "cs-test"
	Code           = "good-code"
	UserToken      = "gho_person"
	ConversionCode = "c0de"
)

var (
	keyOnce sync.Once
	key     *rsa.PrivateKey
)

// Key is the App's private key: one per test binary.
func Key() *rsa.PrivateKey {
	keyOnce.Do(func() {
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(err)
		}
		key = k
	})
	return key
}

// KeyPEM is Key as GitHub hands it over.
func KeyPEM() string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(Key())}))
}

// Repo is a repository an installation grants.
type Repo struct {
	FullName      string
	Private       bool
	DefaultBranch string
}

// Installation is one a person can see.
type Installation struct {
	ID          int64
	Login, Type string
	Suspended   bool
}

// Server is the fake.
type Server struct {
	*httptest.Server

	mu       sync.Mutex
	now      time.Time
	down     bool
	repos    map[int64][]Repo
	branches map[string][]string // by full name, lower case
	people   []Installation
	minted   map[int64]int
	tokens   map[string]int64 // installation token -> installation
	lastBody map[string]any
	calls    []string
}

// NewServer starts the fake with installation 42 on Acme.
func NewServer(t testing.TB) *Server {
	f := &Server{
		repos: map[int64][]Repo{42: {
			{FullName: "Acme/shop", Private: true, DefaultBranch: "main"},
			{FullName: "Acme/site", DefaultBranch: "trunk"},
		}},
		branches: map[string][]string{"acme/shop": {"dev", "main"}, "acme/site": {"trunk"}},
		people:   []Installation{{ID: 42, Login: "Acme", Type: "Organization"}},
		minted:   map[int64]int{},
		tokens:   map[string]int64{},
	}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

// App is the App's credentials, its html-url on the fake.
func (f *Server) App() github.App {
	return github.App{ID: AppID, Slug: Slug, PrivateKey: KeyPEM(), WebhookSecret: WebhookSecret,
		ClientID: ClientID, ClientSecret: ClientSecret, HTMLURL: f.URL + "/apps/" + Slug}
}

// Secret is the App's Secret in namespace.
func (f *Server) Secret(namespace string) *corev1.Secret {
	return &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: github.SecretName, Namespace: namespace},
		Type: corev1.SecretTypeOpaque, Data: f.App().SecretData()}
}

// Options reach the fake, on its clock.
func (f *Server) Options() github.Options {
	return github.Options{APIURL: f.URL, HTTP: f.Client(), Now: f.Time}
}

// Source reads the App's Secret through r.
func (f *Server) Source(r client.Reader, namespace string) *github.Source {
	return &github.Source{Reader: r, Namespace: namespace, Options: f.Options()}
}

// Time is the fake's clock: SetNow's time, or the real one.
func (f *Server) Time() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.clock()
}

func (f *Server) clock() time.Time {
	if f.now.IsZero() {
		return time.Now()
	}
	return f.now
}

func (f *Server) SetNow(t time.Time) { f.mu.Lock(); f.now = t; f.mu.Unlock() }

// SetDown makes every call answer 503.
func (f *Server) SetDown(down bool) { f.mu.Lock(); f.down = down; f.mu.Unlock() }

func (f *Server) SetRepos(installation int64, repos ...Repo) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.repos[installation] = repos
}

func (f *Server) SetUserInstallations(list ...Installation) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.people = list
}

// MintedFor counts the tokens minted for an installation.
func (f *Server) MintedFor(installation int64) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.minted[installation]
}

// LastTokenRequest is the body of the last token request.
func (f *Server) LastTokenRequest() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastBody
}

// Calls are "METHOD /path" of every request, in order.
func (f *Server) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *Server) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	if f.down {
		reply(w, http.StatusServiceUnavailable, map[string]string{"message": "Service unavailable"})
		return
	}
	p := r.URL.Path
	switch {
	case r.Method == "POST" && strings.HasPrefix(p, "/app/installations/") && strings.HasSuffix(p, "/access_tokens"):
		f.accessToken(w, r, strings.TrimSuffix(strings.TrimPrefix(p, "/app/installations/"), "/access_tokens"))
	case r.Method == "GET" && p == "/installation/repositories":
		f.repositories(w, r)
	case r.Method == "GET" && strings.HasPrefix(p, "/repos/") && strings.HasSuffix(p, "/branches"):
		f.branchList(w, r, strings.TrimSuffix(strings.TrimPrefix(p, "/repos/"), "/branches"))
	case r.Method == "POST" && p == "/login/oauth/access_token":
		f.exchange(w, r)
	case r.Method == "GET" && p == "/user/installations":
		f.userInstallations(w, r)
	case r.Method == "POST" && strings.HasPrefix(p, "/app-manifests/") && strings.HasSuffix(p, "/conversions"):
		f.conversion(w, strings.TrimSuffix(strings.TrimPrefix(p, "/app-manifests/"), "/conversions"))
	default:
		reply(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
	}
}

func (f *Server) accessToken(w http.ResponseWriter, r *http.Request, id string) {
	claims := &jwt.RegisteredClaims{}
	_, err := jwt.ParseWithClaims(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), claims,
		func(*jwt.Token) (any, error) { return &Key().PublicKey, nil },
		jwt.WithValidMethods([]string{"RS256"}), jwt.WithTimeFunc(f.clock), jwt.WithIssuer(strconv.Itoa(AppID)))
	if err != nil {
		reply(w, http.StatusUnauthorized, map[string]string{"message": "A JSON web token could not be decoded"})
		return
	}
	installation, _ := strconv.ParseInt(id, 10, 64)
	repos, ok := f.repos[installation]
	if !ok {
		reply(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	raw, _ := io.ReadAll(r.Body)
	var body struct {
		Repositories []string `json:"repositories"`
	}
	_ = json.Unmarshal(raw, &body)
	f.lastBody = map[string]any{}
	_ = json.Unmarshal(raw, &f.lastBody)
	for _, name := range body.Repositories {
		if !grants(repos, name) {
			reply(w, http.StatusUnprocessableEntity, map[string]string{"message": "There is at least one repository that does not exist or is not accessible to the parent installation."})
			return
		}
	}
	f.minted[installation]++
	tok := fmt.Sprintf("ghs_%d_%d", installation, f.minted[installation])
	f.tokens[tok] = installation
	reply(w, http.StatusCreated, map[string]any{"token": tok, "expires_at": f.clock().Add(time.Hour).UTC().Format(time.RFC3339)})
}

func grants(repos []Repo, name string) bool {
	for _, r := range repos {
		if _, n, _ := strings.Cut(r.FullName, "/"); strings.EqualFold(n, name) {
			return true
		}
	}
	return false
}

// installationOf is the installation an installation token is of.
func (f *Server) installationOf(r *http.Request) (int64, bool) {
	id, ok := f.tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
	return id, ok
}

func (f *Server) repositories(w http.ResponseWriter, r *http.Request) {
	id, ok := f.installationOf(r)
	if !ok {
		reply(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
		return
	}
	var out []map[string]any
	for _, repo := range pageOf(f.repos[id], r) {
		out = append(out, map[string]any{"full_name": repo.FullName, "private": repo.Private, "default_branch": repo.DefaultBranch})
	}
	reply(w, http.StatusOK, map[string]any{"total_count": len(f.repos[id]), "repositories": out})
}

func (f *Server) branchList(w http.ResponseWriter, r *http.Request, fullName string) {
	id, ok := f.installationOf(r)
	if !ok || !grants(f.repos[id], fullName[strings.Index(fullName, "/")+1:]) {
		reply(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	out := []map[string]string{}
	for _, b := range pageOf(f.branches[strings.ToLower(fullName)], r) {
		out = append(out, map[string]string{"name": b})
	}
	reply(w, http.StatusOK, out)
}

func (f *Server) exchange(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		Code         string `json:"code"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	switch {
	case in.ClientID != ClientID || in.ClientSecret != ClientSecret:
		reply(w, http.StatusOK, map[string]string{"error": "incorrect_client_credentials", "error_description": "The client_id and/or client_secret passed are incorrect."})
	case in.Code != Code:
		reply(w, http.StatusOK, map[string]string{"error": "bad_verification_code", "error_description": "The code passed is incorrect or expired."})
	default:
		reply(w, http.StatusOK, map[string]string{"access_token": UserToken, "token_type": "bearer"})
	}
}

func (f *Server) userInstallations(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+UserToken {
		reply(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
		return
	}
	var out []map[string]any
	for _, i := range pageOf(f.people, r) {
		var suspended any
		if i.Suspended {
			suspended = f.clock().UTC().Format(time.RFC3339)
		}
		out = append(out, map[string]any{"id": i.ID, "account": map[string]string{"login": i.Login, "type": i.Type}, "suspended_at": suspended})
	}
	reply(w, http.StatusOK, map[string]any{"total_count": len(f.people), "installations": out})
}

func (f *Server) conversion(w http.ResponseWriter, code string) {
	if code != ConversionCode {
		reply(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	app := f.App()
	reply(w, http.StatusCreated, map[string]any{
		"id": app.ID, "slug": app.Slug, "name": "shpyrd test", "owner": map[string]string{"login": "acme"},
		"html_url": app.HTMLURL, "client_id": app.ClientID, "client_secret": app.ClientSecret,
		"webhook_secret": app.WebhookSecret, "pem": app.PrivateKey,
	})
}

// pageOf is one page of items, as GitHub's per_page and page say.
func pageOf[T any](items []T, r *http.Request) []T {
	per, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if per <= 0 {
		per = 30
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page <= 0 {
		page = 1
	}
	from := min((page-1)*per, len(items))
	return items[from:min(from+per, len(items))]
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
```

- [ ] **Step 8: Make the JWT module direct**

Run: `go mod tidy && git diff go.mod`
Expected: `github.com/golang-jwt/jwt/v5 v5.3.1` moves from the `// indirect` block into the direct `require` block, at the same version; nothing else changes.

- [ ] **Step 9: Run the tests to verify they pass**

Run: `go test ./pkg/gitauth/ ./pkg/github/... && go vet ./pkg/github/...`
Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add pkg/gitauth/dial.go pkg/gitauth/dial_test.go pkg/github go.mod go.sum
git commit -m "feat(github): the App's client: JWT, repository tokens, listings, OAuth, manifest conversion (RFC-0054)

Co-Authored-By: <your model name> <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 2: The store: `github_installations`, the `GitHub` sub-interface

**Files:**
- Create: `pkg/store/migrations/000028_github_installations.up.sql`, `000028_github_installations.down.sql`
- Create: `pkg/store/github.go`, `pkg/store/github_memory.go`, `pkg/store/github_postgres.go`
- Modify: `pkg/store/store.go` (the `Store` interface's embedded list, after `Costs` ~988), `pkg/store/memory.go` (`Memory` struct, 29-53), `pkg/store/one.go` (after `WorkspaceByHost`, ~60)
- Modify: `pkg/store/store_test.go` (`dropAll`, ~42)
- Test: `pkg/store/github_test.go`

**Interfaces:**
- Consumes: `Memory.wsAny`, `Memory.now`, `Postgres.wsID`, `ErrNotFound`, `implementations(t)`.
- Produces:
  - `type store.GitHubInstallation struct { ID int64; Workspace, AccountLogin, AccountType, ConnectedBy string; CreatedAt time.Time; SuspendedAt, DeletedAt *time.Time }` (JSON `id, workspace, accountLogin, accountType, connectedBy, createdAt, suspendedAt, deletedAt`)
  - `type store.GitHub interface` with
    - `AddGitHubInstallation(ctx, ws string, inst GitHubInstallation) (moved string, err error)`
    - `GitHubInstallations(ctx, ws string) ([]GitHubInstallation, error)`
    - `GitHubInstallation(ctx, id int64) (*GitHubInstallation, error)`
    - `SetGitHubInstallationState(ctx, id int64, suspended, deleted bool) error`
    - `DeleteGitHubInstallation(ctx, ws string, id int64) error`
  - `store.Store` embeds `GitHub`; `OneWorkspace(st).GitHubInstallation` answers only the default workspace's.

- [ ] **Step 1: Write the failing tests**

`pkg/store/github_test.go`:

```go
package store

import (
	"context"
	"errors"
	"testing"
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
			moved, err := s.AddGitHubInstallation(ctx, DefaultWorkspace, acme(42, "ana@acme.test"))
			if err != nil || moved != "" {
				t.Fatalf("add: %q %v", moved, err)
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
			if moved, err := s.AddGitHubInstallation(ctx, DefaultWorkspace, acme(42, "bob@acme.test")); err != nil || moved != "" {
				t.Fatalf("revive: %q %v", moved, err)
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

func TestGitHubInstallationMovesBetweenWorkspaces(t *testing.T) {
	for name, open := range implementations(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := open(t)
			if _, err := s.CreateWorkspace(ctx, Workspace{Slug: "beta", Name: "Beta"}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.AddGitHubInstallation(ctx, DefaultWorkspace, acme(42, "ana@acme.test")); err != nil {
				t.Fatal(err)
			}
			moved, err := s.AddGitHubInstallation(ctx, "beta", acme(42, "bob@beta.test"))
			if err != nil || moved != DefaultWorkspace {
				t.Fatalf("move: %q %v", moved, err)
			}
			if list, _ := s.GitHubInstallations(ctx, DefaultWorkspace); len(list) != 0 {
				t.Errorf("the old workspace keeps nothing: %+v", list)
			}
			if got, err := s.GitHubInstallation(ctx, 42); err != nil || got.Workspace != "beta" || got.ConnectedBy != "bob@beta.test" {
				t.Errorf("by id: %+v %v", got, err)
			}
			if moved, err := s.AddGitHubInstallation(ctx, "beta", acme(42, "bob@beta.test")); err != nil || moved != "" {
				t.Errorf("connecting again where it is moves nothing: %q %v", moved, err)
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
			if _, err := st.AddGitHubInstallation(ctx, "byhand", acme(9, "x@byhand.test")); err != nil {
				t.Fatal(err)
			}
			if _, err := st.AddGitHubInstallation(ctx, DefaultWorkspace, acme(42, "ana@acme.test")); err != nil {
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
```

In `store_test.go`'s `dropAll`, put `"github_installations"` first in the table list (it references `workspaces`).

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./pkg/store/ -run 'GitHub'`
Expected: FAIL to compile: `undefined: GitHubInstallation`, `s.AddGitHubInstallation undefined`.

- [ ] **Step 3: The migration**

`pkg/store/migrations/000028_github_installations.up.sql`:

```sql
-- GitHub App installations connected to workspaces (RFC-0054). The id is
-- GitHub's. A row marked deleted_at was uninstalled on GitHub; a disconnect
-- removes the row.
CREATE TABLE github_installations (
 id BIGINT PRIMARY KEY,
 workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 account_login TEXT NOT NULL,
 account_type TEXT NOT NULL,
 connected_by TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 suspended_at TIMESTAMPTZ,
 deleted_at TIMESTAMPTZ
);
CREATE INDEX github_installations_workspace ON github_installations(workspace_id) WHERE deleted_at IS NULL;
```

`pkg/store/migrations/000028_github_installations.down.sql`:

```sql
DROP TABLE IF EXISTS github_installations;
```

- [ ] **Step 4: The type and the interface, `pkg/store/github.go`**

```go
package store

import (
	"context"
	"time"
)

// GitHubInstallation is a GitHub App installation connected to a
// workspace (RFC-0054): GitHub's id, the account it is installed on, who
// connected it, and its state on GitHub.
type GitHubInstallation struct {
	ID int64 `json:"id"`
	// Workspace is the slug of the workspace it is connected to.
	Workspace    string `json:"workspace"`
	AccountLogin string `json:"accountLogin"`
	// AccountType is GitHub's: Organization or User.
	AccountType string     `json:"accountType"`
	ConnectedBy string     `json:"connectedBy"`
	CreatedAt   time.Time  `json:"createdAt"`
	SuspendedAt *time.Time `json:"suspendedAt,omitempty"`
	DeletedAt   *time.Time `json:"deletedAt,omitempty"`
}

// GitHub is the GitHub App part of the Store.
type GitHub interface {
	// AddGitHubInstallation connects an installation to a workspace (slug
	// or ID), by GitHub's id: a new one, or one connected before, to this
	// workspace or another, or uninstalled since, taken over. It answers
	// the slug of the workspace it was connected to before, "" when none
	// or the same.
	AddGitHubInstallation(ctx context.Context, ws string, inst GitHubInstallation) (moved string, err error)
	// GitHubInstallations are a workspace's, not uninstalled, oldest first.
	GitHubInstallations(ctx context.Context, ws string) ([]GitHubInstallation, error)
	// GitHubInstallation finds one by id across workspaces; ErrNotFound
	// when no workspace has it or it was uninstalled.
	GitHubInstallation(ctx context.Context, id int64) (*GitHubInstallation, error)
	// SetGitHubInstallationState records what GitHub said: suspended or
	// not (the time is kept while it stays suspended), uninstalled.
	// ErrNotFound when there is no such row.
	SetGitHubInstallationState(ctx context.Context, id int64, suspended, deleted bool) error
	// DeleteGitHubInstallation disconnects one from a workspace;
	// ErrNotFound when the workspace does not have it.
	DeleteGitHubInstallation(ctx context.Context, ws string, id int64) error
}
```

In `pkg/store/store.go`, the `Store` interface's embedded list becomes:

```go
	Sessions
	Tokens
	Memberships
	Hosts
	OAuth
	Usage
	Projects
	Settings
	ConsoleUsers
	Costs
	GitHub
```

- [ ] **Step 5: The memory implementation, `pkg/store/github_memory.go`**

In `pkg/store/memory.go`, add to the `Memory` struct after `cost *costsMemory`:

```go
	github      map[int64]*githubRow
```

`pkg/store/github_memory.go`:

```go
package store

import (
	"context"
	"sort"
)

// githubRow keeps the workspace by ID, as Postgres does: a slug may change.
type githubRow struct {
	GitHubInstallation
	workspaceID string
}

func (m *Memory) slugOfID(id string) string {
	for _, w := range m.workspaces {
		if w.ID == id {
			return w.Slug
		}
	}
	return ""
}

func (m *Memory) githubOut(r *githubRow) GitHubInstallation {
	out := r.GitHubInstallation
	out.Workspace = m.slugOfID(r.workspaceID)
	return out
}

func (m *Memory) AddGitHubInstallation(_ context.Context, ws string, inst GitHubInstallation) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.wsAny(ws)
	if err != nil {
		return "", err
	}
	if m.github == nil {
		m.github = map[int64]*githubRow{}
	}
	moved, created := "", m.now()
	if prev, ok := m.github[inst.ID]; ok && prev.DeletedAt == nil {
		if prev.workspaceID != w.ID {
			moved = m.slugOfID(prev.workspaceID)
		} else {
			created = prev.CreatedAt
		}
	}
	row := &githubRow{GitHubInstallation: inst, workspaceID: w.ID}
	row.CreatedAt, row.DeletedAt = created, nil
	m.github[inst.ID] = row
	return moved, nil
}

func (m *Memory) GitHubInstallations(_ context.Context, ws string) ([]GitHubInstallation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.wsAny(ws)
	if err != nil {
		return nil, err
	}
	out := []GitHubInstallation{}
	for _, r := range m.github {
		if r.workspaceID == w.ID && r.DeletedAt == nil {
			out = append(out, m.githubOut(r))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (m *Memory) GitHubInstallation(_ context.Context, id int64) (*GitHubInstallation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.github[id]
	if !ok || r.DeletedAt != nil {
		return nil, ErrNotFound
	}
	out := m.githubOut(r)
	return &out, nil
}

func (m *Memory) SetGitHubInstallationState(_ context.Context, id int64, suspended, deleted bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.github[id]
	if !ok {
		return ErrNotFound
	}
	now := m.now()
	switch {
	case !suspended:
		r.SuspendedAt = nil
	case r.SuspendedAt == nil:
		r.SuspendedAt = &now
	}
	if deleted && r.DeletedAt == nil {
		r.DeletedAt = &now
	}
	return nil
}

func (m *Memory) DeleteGitHubInstallation(_ context.Context, ws string, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.wsAny(ws)
	if err != nil {
		return err
	}
	r, ok := m.github[id]
	if !ok || r.workspaceID != w.ID {
		return ErrNotFound
	}
	delete(m.github, id)
	return nil
}
```

- [ ] **Step 6: The Postgres implementation, `pkg/store/github_postgres.go`**

```go
package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

const githubColumns = `g.id, w.slug, g.account_login, g.account_type, g.connected_by, g.created_at, g.suspended_at, g.deleted_at`

func scanGitHubInstallation(row pgx.Row) (GitHubInstallation, error) {
	var g GitHubInstallation
	err := row.Scan(&g.ID, &g.Workspace, &g.AccountLogin, &g.AccountType, &g.ConnectedBy, &g.CreatedAt, &g.SuspendedAt, &g.DeletedAt)
	return g, err
}

func (p *Postgres) AddGitHubInstallation(ctx context.Context, ws string, inst GitHubInstallation) (string, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	wsID, err := p.wsID(ctx, tx, ws)
	if err != nil {
		return "", err
	}
	var prevID, prevSlug string
	var prevDeleted *time.Time
	err = tx.QueryRow(ctx, `SELECT g.workspace_id::text, w.slug, g.deleted_at FROM github_installations g
		JOIN workspaces w ON w.id = g.workspace_id WHERE g.id = $1 FOR UPDATE OF g`, inst.ID).Scan(&prevID, &prevSlug, &prevDeleted)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	moved := ""
	if err == nil && prevDeleted == nil && prevID != wsID {
		moved = prevSlug
	}
	// SET sees the row as it was: created_at stays only for a connection
	// to the same workspace that was never uninstalled.
	if _, err := tx.Exec(ctx, `INSERT INTO github_installations (id, workspace_id, account_login, account_type, connected_by, suspended_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO UPDATE SET
		 created_at = CASE WHEN github_installations.workspace_id = EXCLUDED.workspace_id AND github_installations.deleted_at IS NULL
		  THEN github_installations.created_at ELSE now() END,
		 workspace_id = EXCLUDED.workspace_id, account_login = EXCLUDED.account_login, account_type = EXCLUDED.account_type,
		 connected_by = EXCLUDED.connected_by, suspended_at = EXCLUDED.suspended_at, deleted_at = NULL`,
		inst.ID, wsID, inst.AccountLogin, inst.AccountType, inst.ConnectedBy, inst.SuspendedAt); err != nil {
		return "", err
	}
	return moved, tx.Commit(ctx)
}

func (p *Postgres) GitHubInstallations(ctx context.Context, ws string) ([]GitHubInstallation, error) {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `SELECT `+githubColumns+` FROM github_installations g JOIN workspaces w ON w.id = g.workspace_id
		WHERE g.workspace_id = $1 AND g.deleted_at IS NULL ORDER BY g.created_at, g.id`, wsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GitHubInstallation{}
	for rows.Next() {
		g, err := scanGitHubInstallation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (p *Postgres) GitHubInstallation(ctx context.Context, id int64) (*GitHubInstallation, error) {
	g, err := scanGitHubInstallation(p.pool.QueryRow(ctx, `SELECT `+githubColumns+` FROM github_installations g
		JOIN workspaces w ON w.id = g.workspace_id WHERE g.id = $1 AND g.deleted_at IS NULL`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &g, nil
}

func (p *Postgres) SetGitHubInstallationState(ctx context.Context, id int64, suspended, deleted bool) error {
	tag, err := p.pool.Exec(ctx, `UPDATE github_installations SET
		suspended_at = CASE WHEN $2 THEN COALESCE(suspended_at, now()) ELSE NULL END,
		deleted_at = CASE WHEN $3 THEN COALESCE(deleted_at, now()) ELSE deleted_at END
		WHERE id = $1`, id, suspended, deleted)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) DeleteGitHubInstallation(ctx context.Context, ws string, id int64) error {
	wsID, err := p.wsID(ctx, p.pool, ws)
	if err != nil {
		return err
	}
	tag, err := p.pool.Exec(ctx, `DELETE FROM github_installations WHERE id = $1 AND workspace_id = $2`, id, wsID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
```

Check the pgx import path matches `pkg/store/postgres.go` (`grep -n 'jackc/pgx' pkg/store/postgres.go`); use the same.

- [ ] **Step 7: The single-workspace override**

In `pkg/store/one.go`, after `WorkspaceByHost`:

```go
// GitHubInstallation answers only the default workspace's: an
// installation a workspace added by hand holds is not there for the
// webhook (RFC-0054).
func (o *oneWorkspace) GitHubInstallation(ctx context.Context, id int64) (*GitHubInstallation, error) {
	inst, err := o.Store.GitHubInstallation(ctx, id)
	if err != nil {
		return nil, err
	}
	if inst.Workspace != o.defaultSlug(ctx) {
		return nil, ErrNotFound
	}
	return inst, nil
}
```

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go test ./pkg/store/ && go build ./...`
Expected: PASS (memory). With a database, also:
`SHPYRD_TEST_DATABASE_URL=postgres://postgres:postgres@localhost:5432/shpyrd_test?sslmode=disable go test ./pkg/store/ -run 'GitHub|Conformance|Migrate'`
Expected: PASS (postgres). Start one with
`docker run --rm -d -p 5432:5432 -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=shpyrd_test postgres:16` when none runs; CI provides one.

- [ ] **Step 9: Commit**

```bash
git add pkg/store
git commit -m "feat(store): GitHub App installations per workspace, migration 000028 (RFC-0054)

Co-Authored-By: <your model name> <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 3: `shpyrd-ctl github app create|show|delete`

**Files:**
- Create: `internal/cli/ctl_github.go`
- Modify: `internal/cli/ctl.go` (after `root.AddCommand(newCtlWorkspaceCmd(g))`, ~45)
- Test: `internal/cli/ctl_github_test.go`

**Interfaces:**
- Consumes: `github.ConvertManifest`, `github.SecretName`, `github.KeySlug`, `github.KeyAppID`, `github.KeyHTMLURL`, `github.EnvAPIURL`, `github.DefaultAPIURL`, `(github.App).SecretData()`, `(github.App).InstallURL()`, `githubtest.NewServer`, `githubtest.ConversionCode` (Task 1); `install.ReadInstallInfo`, `install.VarDashboardURL`, `install.BaseURL`, `install.DefaultSystemNamespace`, `install.InstallRecordName`; `kube.Connect`; `openBrowser` (`appcmds.go:610`); `signalContext`; `globalFlags.print`, `globalFlags.progress`.
- Produces:
  - `var ctlKube func(g *globalFlags) (*kube.Client, error)` (tests replace it)
  - `var browse func(url string) error = openBrowser` (tests replace it; Task 7 uses it too)
  - `var manifestWait = 10 * time.Minute`
  - `type appManifest struct`, `func newAppManifest(dashboard, redirect, name string, public bool) appManifest`, `func appName(dashboard, name string) string`
  - `func newCtlGitHubCmd(g *globalFlags) *cobra.Command`

- [ ] **Step 1: Write the failing tests**

`internal/cli/ctl_github_test.go`:

```go
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	kubefake "k8s.io/client-go/kubernetes/fake"

	"github.com/shpyrd-io/shpyrd/pkg/github"
	"github.com/shpyrd-io/shpyrd/pkg/github/githubtest"
	"github.com/shpyrd-io/shpyrd/pkg/install"
	"github.com/shpyrd-io/shpyrd/pkg/kube"
)

// onCluster points shpyrd-ctl at a fake cluster installed with the
// dashboard at https://shpyrd.example.com, and returns it.
func onCluster(t *testing.T, objs ...corev1.Secret) kubernetes.Interface {
	t.Helper()
	record := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: install.InstallRecordName, Namespace: install.DefaultSystemNamespace},
		Data:       map[string]string{"vars": install.VarDashboardURL + ": https://shpyrd.example.com\n"},
	}
	cs := kubefake.NewSimpleClientset(record)
	for i := range objs {
		if _, err := cs.CoreV1().Secrets(objs[i].Namespace).Create(context.Background(), &objs[i], metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	prev := ctlKube
	ctlKube = func(*globalFlags) (*kube.Client, error) { return &kube.Client{Kube: cs, Namespace: install.DefaultSystemNamespace}, nil }
	t.Cleanup(func() { ctlKube = prev })
	return cs
}

func runCtl(t *testing.T, args ...string) (string, error) {
	t.Helper()
	defer func(prev bool) { preferKubeconfig = prev }(preferKubeconfig)
	root := NewCtl()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

var formAction = regexp.MustCompile(`action="([^"]+)"`)
var manifestValue = regexp.MustCompile(`name="manifest" value="([^"]*)"`)

func TestCtlGitHubAppCreateRegistersTheAppFromItsManifest(t *testing.T) {
	fake := githubtest.NewServer(t)
	t.Setenv(github.EnvAPIURL, fake.URL)
	cs := onCluster(t)
	seen := make(chan error, 1)
	prevBrowse := browse
	browse = func(page string) error {
		go func() { seen <- actAsTheBrowser(t, page) }()
		return nil
	}
	t.Cleanup(func() { browse = prevBrowse })

	out, err := runCtl(t, "github", "app", "create", "--org", "acme")
	if err != nil {
		t.Fatalf("create: %v\n%s", err, out)
	}
	if err := <-seen; err != nil {
		t.Fatal(err)
	}
	sec, err := cs.CoreV1().Secrets(install.DefaultSystemNamespace).Get(context.Background(), github.SecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	app, err := github.AppFromSecret(sec)
	if err != nil || app != fake.App() {
		t.Fatalf("the Secret holds %+v, %v", app, err)
	}
	for _, want := range []string{"Registered shpyrd test", fake.App().HTMLURL, fake.App().InstallURL(), "shpyrd github connect"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, githubtest.ClientSecret) || strings.Contains(out, "PRIVATE KEY") {
		t.Errorf("a credential was printed:\n%s", out)
	}
}

// actAsTheBrowser opens the local page, checks the manifest it would send
// GitHub, then comes back as GitHub's redirect does: once with a state
// that is not the page's, then with it.
func actAsTheBrowser(t *testing.T, page string) error {
	res, err := http.Get(page)
	if err != nil {
		return err
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	action := html.UnescapeString(formAction.FindStringSubmatch(string(body))[1])
	if !strings.HasPrefix(action, "https://github.com/organizations/acme/settings/apps/new?state=") {
		t.Errorf("form action %q", action)
	}
	state := strings.TrimPrefix(action, "https://github.com/organizations/acme/settings/apps/new?state=")
	var m appManifest
	if err := json.Unmarshal([]byte(html.UnescapeString(manifestValue.FindStringSubmatch(string(body))[1])), &m); err != nil {
		return err
	}
	base := strings.TrimSuffix(page, "/")
	if m.Name != "shpyrd-shpyrd.example.com" || m.URL != "https://shpyrd.example.com" || m.HookAttributes.URL != "https://shpyrd.example.com/api/github/webhook" ||
		m.RedirectURL != base+"/created" || m.SetupURL != "https://shpyrd.example.com/api/github/installed" ||
		len(m.CallbackURLs) != 1 || m.CallbackURLs[0] != m.SetupURL || !m.RequestOAuthOnInstall || !m.SetupOnUpdate || m.Public {
		t.Errorf("manifest = %+v", m)
	}
	wantPerms := map[string]string{"contents": "read", "metadata": "read", "checks": "write", "deployments": "write", "statuses": "read"}
	for k, v := range wantPerms {
		if m.DefaultPermissions[k] != v {
			t.Errorf("permission %s = %q", k, m.DefaultPermissions[k])
		}
	}
	if strings.Join(m.DefaultEvents, ",") != "push,installation_repositories,check_suite,status" {
		t.Errorf("events = %v", m.DefaultEvents)
	}
	if res, err := http.Get(base + "/created?code=" + githubtest.ConversionCode + "&state=not-it"); err != nil || res.StatusCode != http.StatusBadRequest {
		t.Errorf("a state that is not the page's: %v %v", res, err)
	}
	res, err = http.Get(base + "/created?code=" + githubtest.ConversionCode + "&state=" + state)
	if err != nil {
		return err
	}
	done, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || !strings.Contains(string(done), "Registered") {
		t.Errorf("created: %d %s", res.StatusCode, done)
	}
	return nil
}

func TestCtlGitHubAppCreateRefusesASecondApp(t *testing.T) {
	fake := githubtest.NewServer(t)
	onCluster(t, *fake.Secret(install.DefaultSystemNamespace))
	prevBrowse := browse
	browse = func(string) error { t.Error("the browser must not open"); return nil }
	t.Cleanup(func() { browse = prevBrowse })
	_, err := runCtl(t, "github", "app", "create")
	if err == nil || !strings.Contains(err.Error(), "shpyrd-ctl github app delete") {
		t.Errorf("err = %v", err)
	}
}

func TestCtlGitHubAppCreateGivesUpAfterTheWait(t *testing.T) {
	onCluster(t)
	prevBrowse, prevWait := browse, manifestWait
	browse, manifestWait = func(string) error { return nil }, 50*time.Millisecond
	t.Cleanup(func() { browse, manifestWait = prevBrowse, prevWait })
	_, err := runCtl(t, "github", "app", "create")
	if err == nil || !strings.Contains(err.Error(), "run shpyrd-ctl github app create again") {
		t.Errorf("err = %v", err)
	}
}

func TestCtlGitHubAppShowNeverPrintsTheKeysAndDeleteRemovesTheSecret(t *testing.T) {
	fake := githubtest.NewServer(t)
	cs := onCluster(t, *fake.Secret(install.DefaultSystemNamespace))
	out, err := runCtl(t, "github", "app", "show")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{githubtest.Slug, "1234", fake.App().HTMLURL} {
		if !strings.Contains(out, want) {
			t.Errorf("show lacks %q:\n%s", want, out)
		}
	}
	for _, secret := range []string{githubtest.ClientSecret, githubtest.WebhookSecret, "PRIVATE KEY"} {
		if strings.Contains(out, secret) {
			t.Errorf("show printed a credential:\n%s", out)
		}
	}
	out, err = runCtl(t, "github", "app", "delete")
	if err != nil || !strings.Contains(out, "Delete the App on GitHub too") {
		t.Fatalf("delete: %v\n%s", err, out)
	}
	if _, err := cs.CoreV1().Secrets(install.DefaultSystemNamespace).Get(context.Background(), github.SecretName, metav1.GetOptions{}); err == nil {
		t.Error("the Secret is still there")
	}
	if _, err := runCtl(t, "github", "app", "show"); err == nil || !strings.Contains(err.Error(), "no GitHub App") {
		t.Errorf("show without one: %v", err)
	}
}

func TestTheAppsNameFitsGitHubsLimit(t *testing.T) {
	if got := appName("https://shpyrd.example.com", ""); got != "shpyrd-shpyrd.example.com" {
		t.Errorf("short: %q", got)
	}
	long := appName("https://dashboard.a-very-long-customer-name.example.com:8443", "")
	if len(long) > 34 || !strings.HasPrefix(long, "shpyrd-dashboard.") || strings.HasSuffix(long, ".") || strings.HasSuffix(long, "-") {
		t.Errorf("long: %q (%d)", long, len(long))
	}
	if got := appName("https://shpyrd.example.com", "Acme deploys"); got != "Acme deploys" {
		t.Errorf("--name: %q", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/ -run 'CtlGitHub|AppsName'`
Expected: FAIL to compile: `undefined: ctlKube`, `undefined: browse`, `undefined: appManifest`.

- [ ] **Step 3: Write `internal/cli/ctl_github.go`**

```go
package cli

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/shpyrd-io/shpyrd/pkg/github"
	"github.com/shpyrd-io/shpyrd/pkg/install"
	"github.com/shpyrd-io/shpyrd/pkg/kube"
)

// The cluster's GitHub App (RFC-0054). `shpyrd-ctl github app create`
// registers it on GitHub from a manifest, in one click in the browser, and
// keeps its credentials in the Secret shpyrd-system/shpyrd-github-app,
// which the server reads. The server has no registration endpoint.

var (
	// ctlKube connects to the cluster the kubeconfig names; tests replace it.
	ctlKube = func(g *globalFlags) (*kube.Client, error) {
		return kube.Connect(kube.Options{Kubeconfig: g.kubeconfig, Context: g.kubeCtx})
	}
	// browse opens a page in the person's browser; tests replace it.
	browse = openBrowser
	// manifestWait is how long the registration waits for GitHub.
	manifestWait = 10 * time.Minute
)

// githubNameLimit is the longest name GitHub takes for an App.
const githubNameLimit = 34

// appManifest is what GitHub creates an App from.
type appManifest struct {
	Name           string `json:"name"`
	URL            string `json:"url"`
	Description    string `json:"description"`
	HookAttributes struct {
		URL    string `json:"url"`
		Active bool   `json:"active"`
	} `json:"hook_attributes"`
	RedirectURL           string            `json:"redirect_url"`
	CallbackURLs          []string          `json:"callback_urls"`
	SetupURL              string            `json:"setup_url"`
	Public                bool              `json:"public"`
	RequestOAuthOnInstall bool              `json:"request_oauth_on_install"`
	SetupOnUpdate         bool              `json:"setup_on_update"`
	DefaultPermissions    map[string]string `json:"default_permissions"`
	DefaultEvents         []string          `json:"default_events"`
}

// newAppManifest is the cluster's App: its webhook and its install
// callback at the dashboard; every permission part 4 needs asked for now,
// so installations never approve again. With request_oauth_on_install
// GitHub sends the browser to callback_urls[0]; setup_url is the same.
func newAppManifest(dashboard, redirect, name string, public bool) appManifest {
	m := appManifest{
		Name:                  appName(dashboard, name),
		URL:                   dashboard,
		Description:           "Builds and deploys this account's repositories on shpyrd at " + dashboard + ".",
		RedirectURL:           redirect,
		SetupURL:              dashboard + "/api/github/installed",
		Public:                public,
		RequestOAuthOnInstall: true,
		SetupOnUpdate:         true,
		DefaultPermissions:    map[string]string{"contents": "read", "metadata": "read", "checks": "write", "deployments": "write", "statuses": "read"},
		DefaultEvents:         []string{"push", "installation_repositories", "check_suite", "status"},
	}
	m.HookAttributes.URL, m.HookAttributes.Active = dashboard+"/api/github/webhook", true
	m.CallbackURLs = []string{m.SetupURL}
	return m
}

// appName is --name, or shpyrd-<dashboard host> cut to GitHub's limit.
func appName(dashboard, name string) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	host := dashboard
	if u, err := url.Parse(dashboard); err == nil && u.Hostname() != "" {
		host = u.Hostname()
	}
	n := "shpyrd-" + host
	if len(n) > githubNameLimit {
		n = n[:githubNameLimit]
	}
	return strings.TrimRight(n, ".-")
}

func newCtlGitHubCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "github",
		Short: "The cluster's GitHub App, which workspaces connect",
	}
	app := &cobra.Command{
		Use:   "app",
		Short: "Register, show or remove the cluster's GitHub App",
		Long: `Workspaces connect GitHub through one App per cluster. Register it once:

  shpyrd-ctl github app create [--org ORG] [--name NAME] [--public] [--url DASHBOARD_URL]

A browser opens on GitHub with the App filled in; one click creates it, and
its credentials go into the Secret shpyrd-system/shpyrd-github-app.

  shpyrd-ctl github app show      its slug, id and page on GitHub (never its keys)
  shpyrd-ctl github app delete    remove the Secret (then delete the App on GitHub)`,
	}
	var org, name, dashboard string
	var public bool
	create := &cobra.Command{
		Use:   "create",
		Short: "Register the cluster's GitHub App on GitHub, in one click",
		RunE: func(cmd *cobra.Command, args []string) error {
			return createGitHubApp(cmd, g, org, name, dashboard, public)
		},
	}
	create.Flags().StringVar(&org, "org", "", "register the App under this GitHub organisation (default: your own account)")
	create.Flags().StringVar(&name, "name", "", "the App's name on GitHub (default: shpyrd-<dashboard host>)")
	create.Flags().StringVar(&dashboard, "url", "", "the dashboard's public URL (default: the cluster's, from its install settings)")
	create.Flags().BoolVar(&public, "public", false, "let any account install the App, not only its owner")

	show := &cobra.Command{
		Use:   "show",
		Short: "Show the cluster's GitHub App: slug, id and page (never its keys)",
		RunE: func(cmd *cobra.Command, args []string) error {
			sec, err := githubAppSecret(g)
			if err != nil {
				return err
			}
			v := map[string]string{"slug": string(sec.Data[github.KeySlug]), "id": string(sec.Data[github.KeyAppID]), "url": string(sec.Data[github.KeyHTMLURL])}
			return g.print(cmd, v, func(w io.Writer) {
				fmt.Fprintf(w, "GitHub App %s (id %s)\n  %s\n  Install it: %s/installations/new\n", v["slug"], v["id"], v["url"], v["url"])
			})
		},
	}
	del := &cobra.Command{
		Use:   "delete",
		Short: "Remove the App's Secret; shpyrd stops using the App",
		RunE: func(cmd *cobra.Command, args []string) error {
			sec, err := githubAppSecret(g)
			if err != nil {
				return err
			}
			k, err := ctlKube(g)
			if err != nil {
				return err
			}
			if err := k.Kube.CoreV1().Secrets(install.DefaultSystemNamespace).Delete(signalContext(), github.SecretName, metav1.DeleteOptions{}); err != nil {
				return err
			}
			slug := string(sec.Data[github.KeySlug])
			return g.print(cmd, map[string]string{"deleted": slug}, func(w io.Writer) {
				fmt.Fprintf(w, "Removed the Secret %s/%s: shpyrd stops using %s within a minute; builds fall back to deploy keys and tokens.\n", install.DefaultSystemNamespace, github.SecretName, slug)
				fmt.Fprintf(w, "Delete the App on GitHub too: the owner's Settings, Developer settings, GitHub Apps, %s, Advanced, Delete GitHub App.\n", slug)
			})
		},
	}
	app.AddCommand(create, show, del)
	cmd.AddCommand(app)
	return cmd
}

func githubAppSecret(g *globalFlags) (*corev1.Secret, error) {
	k, err := ctlKube(g)
	if err != nil {
		return nil, err
	}
	sec, err := k.Kube.CoreV1().Secrets(install.DefaultSystemNamespace).Get(signalContext(), github.SecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, errors.New("this cluster has no GitHub App: register one with shpyrd-ctl github app create")
	}
	return sec, err
}

var manifestPage = template.Must(template.New("manifest").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Register shpyrd's GitHub App</title></head>
<body style="font-family:system-ui,sans-serif;margin:3rem;max-width:36rem">
<p>Sending the App to GitHub. If nothing happens, continue by hand.</p>
<form id="manifest" method="post" action="{{.Action}}">
<input type="hidden" name="manifest" value="{{.Manifest}}">
<button type="submit">Continue to GitHub</button>
</form>
<script>document.getElementById("manifest").submit()</script>
</body></html>`))

var resultPage = template.Must(template.New("result").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>{{.Title}}</title></head>
<body style="font-family:system-ui,sans-serif;margin:3rem;max-width:36rem"><h1>{{.Title}}</h1><p>{{.Text}}</p></body></html>`))

func showResult(w http.ResponseWriter, status int, title, text string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = resultPage.Execute(w, map[string]string{"Title": title, "Text": text})
}

// createGitHubApp serves the manifest on 127.0.0.1, opens the browser on
// it, and waits for GitHub to send the browser back with a code, which it
// turns into the App's credentials and writes into the Secret.
func createGitHubApp(cmd *cobra.Command, g *globalFlags, org, name, dashboard string, public bool) error {
	ctx := signalContext()
	out := g.progress(cmd)
	k, err := ctlKube(g)
	if err != nil {
		return err
	}
	ns := install.DefaultSystemNamespace
	if sec, err := k.Kube.CoreV1().Secrets(ns).Get(ctx, github.SecretName, metav1.GetOptions{}); err == nil {
		return fmt.Errorf("this cluster already has a GitHub App (%s): shpyrd-ctl github app delete first, to register another", sec.Data[github.KeySlug])
	} else if !apierrors.IsNotFound(err) {
		return err
	}
	if dashboard == "" {
		info, err := install.ReadInstallInfo(ctx, k, "")
		if err != nil {
			return fmt.Errorf("could not read the cluster's install settings (%v): give the dashboard's public URL with --url", err)
		}
		dashboard = firstNonEmpty(info.Vars[install.VarDashboardURL], install.BaseURL(info.Vars)("shpyrd"))
	}
	dashboard = strings.TrimSuffix(dashboard, "/")
	if u, err := url.Parse(dashboard); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("%q is not a URL: give the dashboard's, such as https://shpyrd.example.com", dashboard)
	}
	nonce := make([]byte, 24)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	state := base64.RawURLEncoding.EncodeToString(nonce)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	base := "http://" + ln.Addr().String()
	manifest := newAppManifest(dashboard, base+"/created", name, public)
	raw, _ := json.Marshal(manifest)
	target := "https://github.com/settings/apps/new"
	if org != "" {
		target = "https://github.com/organizations/" + url.PathEscape(org) + "/settings/apps/new"
	}
	apiURL := firstNonEmpty(os.Getenv(github.EnvAPIURL), github.DefaultAPIURL)

	type result struct {
		reg github.Registration
		err error
	}
	done := make(chan result, 1)
	var once sync.Once
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = manifestPage.Execute(w, map[string]string{"Action": target + "?state=" + state, "Manifest": string(raw)})
	})
	mux.HandleFunc("GET /created", func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("state")), []byte(state)) != 1 {
			showResult(w, http.StatusBadRequest, "Not this registration", "This page was not opened by this run of shpyrd-ctl. Start again with shpyrd-ctl github app create.")
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			showResult(w, http.StatusBadRequest, "GitHub sent no code", "Start again with shpyrd-ctl github app create.")
			return
		}
		handled := false
		once.Do(func() {
			handled = true
			reg, err := github.ConvertManifest(r.Context(), apiURL, nil, code)
			if err == nil {
				sec := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: github.SecretName, Namespace: ns, Labels: map[string]string{"app.kubernetes.io/managed-by": "shpyrd"}},
					Type:       corev1.SecretTypeOpaque,
					Data:       reg.App.SecretData(),
				}
				_, err = k.Kube.CoreV1().Secrets(ns).Create(r.Context(), sec, metav1.CreateOptions{})
			}
			if err != nil {
				showResult(w, http.StatusBadGateway, "Not registered", "GitHub created the App, but shpyrd-ctl could not keep its credentials: "+err.Error()+". Delete the App on GitHub and run shpyrd-ctl github app create again.")
			} else {
				showResult(w, http.StatusOK, "Registered "+reg.Name, "The App is ready. You can close this tab; the terminal says what is next.")
			}
			done <- result{reg, err}
		})
		if !handled {
			showResult(w, http.StatusConflict, "Already registered", "This registration is finished. You can close this tab.")
		}
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		shut, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shut)
	}()

	fmt.Fprintf(out, "Registering %s on GitHub. Opening the browser (if nothing opens: %s/)\n", manifest.Name, base)
	if err := browse(base + "/"); err != nil {
		fmt.Fprintf(out, "Open %s/ in a browser on this machine.\n", base)
	}
	fmt.Fprint(out, "Waiting for GitHub... ")
	select {
	case <-ctx.Done():
		fmt.Fprintln(out)
		return errors.New("cancelled: nothing was registered")
	case <-time.After(manifestWait):
		fmt.Fprintln(out)
		return errors.New("GitHub did not come back within 10 minutes: run shpyrd-ctl github app create again")
	case r := <-done:
		fmt.Fprintln(out)
		if r.err != nil {
			return fmt.Errorf("the App was created on GitHub but not kept: %w; delete it there and run shpyrd-ctl github app create again", r.err)
		}
		app := r.reg.App
		return g.print(cmd, map[string]any{"slug": app.Slug, "id": app.ID, "url": app.HTMLURL, "install": app.InstallURL()}, func(w io.Writer) {
			fmt.Fprintf(w, "Registered %s: %s\n", r.reg.Name, app.HTMLURL)
			fmt.Fprintf(w, "Its credentials are in the Secret %s/%s.\n", ns, github.SecretName)
			fmt.Fprintf(w, "Install it on an account or organisation: %s\n", app.InstallURL())
			fmt.Fprintln(w, "Workspaces connect it from the dashboard (Workspace settings, Git) or with: shpyrd github connect")
		})
	}
}
```

In `internal/cli/ctl.go`, after `root.AddCommand(newCtlWorkspaceCmd(g))`:

```go
	root.AddCommand(newCtlGitHubCmd(g)) // RFC-0054: the cluster's GitHub App
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/cli/ -run 'CtlGitHub|AppsName|CtlIgnores' -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/ctl_github.go internal/cli/ctl_github_test.go internal/cli/ctl.go
git commit -m "feat(ctl): shpyrd-ctl github app create, show, delete: the cluster's App from a manifest (RFC-0054)

Co-Authored-By: <your model name> <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 4: The API: connect, the callback, list, disconnect, repositories and branches

**Files:**
- Create: `pkg/api/github.go`
- Modify: `pkg/api/server.go` (fields after `hookRefusals` ~284; init after `s.hookRefusals = …` ~388 and after the `systemNS` block ~398-405; routes: public after `pub.POST("/cli/device/token", …)` ~601, protected after the `git-auth` routes ~665)
- Test: `pkg/api/github_test.go`

**Interfaces:**
- Consumes: `github.Source`, `github.Client` (`App`, `ExchangeCode`, `UserInstallations`, `Repositories`, `Branches`), `github.ErrNotConfigured`, `github.ErrRepoNotGranted`, `githubtest` (Task 1); `store.GitHub` (Task 2); `s.doorWorkspace`, `actorOf`, `s.audit`, `s.auditActor`, `s.dashboardURLOf`, `s.hasCapability`, `randomHex`, `abort`, `pages.HTML`, `newRateLimiter`.
- Produces:
  - `type GitHubView struct { Configured bool; AppURL, Note string; Installations []GitHubInstallationView }` (JSON `configured, appURL, note, installations`)
  - `type GitHubInstallationView struct { ID int64; Account, Type, ConnectedBy string; CreatedAt time.Time; Suspended bool; ManageURL string }` (JSON `id, account, type, connectedBy, createdAt, suspended, manageURL`)
  - `type GitHubConnectView struct { InstallURL string }` (JSON `installURL`)
  - `type GitHubRepositoryView struct { Installation int64; FullName string; Private bool; DefaultBranch string }` (JSON `installation, fullName, private, defaultBranch`)
  - `type githubState struct { Workspace, Actor string; Exp int64; Nonce string }` (JSON `ws, actor, exp, n`), `func signGitHubState(key string, st githubState) string`, `func readGitHubState(key, raw string, now time.Time) (githubState, error)`
  - `Server.githubApp *github.Source` (tests replace it), `s.githubClient(c) (*github.Client, bool)` (409 when missing), `s.forgetGitHubRepos(ws string)`, `s.githubMissing() string`
  - Routes: `POST /api/workspace/github/connect`, `GET /api/workspace/github`, `DELETE /api/workspace/github/installations/:id` (ClusterAdmin); `GET /api/workspace/github/repositories`, `GET /api/workspace/github/repositories/:owner/:repo/branches` (ClusterCreate); `GET /api/github/installed` (public)

- [ ] **Step 1: Write the failing tests**

`pkg/api/github_test.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/shpyrd-io/shpyrd/pkg/audit"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/github/githubtest"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// githubServer is a test server on a platform with the fake GitHub's App.
func githubServer(t *testing.T, objs ...client.Object) (*Server, client.Client, *githubtest.Server) {
	t.Helper()
	fake := githubtest.NewServer(t)
	s, cr := newTestServer(t, nil, append([]client.Object{fake.Secret("shpyrd-system")}, objs...))
	s.githubApp = fake.Source(cr, "shpyrd-system")
	return s, cr, fake
}

// githubAudits collects what is audited from here on.
func githubAudits(t *testing.T) *[]audit.Entry {
	t.Helper()
	var heard []audit.Entry
	audit.Sink = func(_ audit.Ref, e audit.Entry) { heard = append(heard, e) }
	t.Cleanup(func() { audit.Sink = nil })
	return &heard
}

// connectState starts a connection as the admin token and returns the
// state GitHub carries back.
func connectState(t *testing.T, s *Server) string {
	t.Helper()
	rec := do(t, s, "POST", "/api/workspace/github/connect", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("connect: %d %s", rec.Code, rec.Body.String())
	}
	var v GitHubConnectView
	_ = json.Unmarshal(rec.Body.Bytes(), &v)
	u, err := url.Parse(v.InstallURL)
	if err != nil || !strings.HasSuffix(u.Path, "/apps/"+githubtest.Slug+"/installations/new") || u.Query().Get("state") == "" {
		t.Fatalf("install URL %q", v.InstallURL)
	}
	return u.Query().Get("state")
}

func installed(t *testing.T, s *Server, id, code, state string) *httptest.ResponseRecorder {
	t.Helper()
	q := url.Values{"installation_id": {id}, "setup_action": {"install"}, "code": {code}, "state": {state}}
	return do(t, s, "GET", "/api/github/installed?"+q.Encode(), "", false)
}

func TestGitHubConnectStoresTheInstallationThePersonCanSee(t *testing.T) {
	s, _, fake := githubServer(t)
	heard := githubAudits(t)
	rec := installed(t, s, "42", githubtest.Code, connectState(t, s))
	if rec.Code != http.StatusFound || !strings.HasSuffix(rec.Header().Get("Location"), "/workspace/git") {
		t.Fatalf("callback: %d %q %s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	list, _ := s.store.GitHubInstallations(context.Background(), store.DefaultWorkspace)
	if len(list) != 1 || list[0].ID != 42 || list[0].AccountLogin != "Acme" || list[0].AccountType != "Organization" || list[0].ConnectedBy != "admin-token" {
		t.Fatalf("stored %+v", list)
	}
	if len(*heard) != 1 || (*heard)[0].Action != "github.connect" || (*heard)[0].Actor != "admin-token" ||
		(*heard)[0].Target != "github installation 42 (Acme)" || (*heard)[0].Detail != "workspace default" {
		t.Errorf("audit %+v", *heard)
	}
	rec = do(t, s, "GET", "/api/workspace/github", "", true)
	var v GitHubView
	_ = json.Unmarshal(rec.Body.Bytes(), &v)
	if !v.Configured || v.AppURL != fake.App().HTMLURL || len(v.Installations) != 1 || v.Installations[0].Account != "Acme" ||
		v.Installations[0].ManageURL != fake.URL+"/organizations/Acme/settings/installations/42" || v.Installations[0].Suspended {
		t.Errorf("view %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), githubtest.ClientSecret) || strings.Contains(rec.Body.String(), githubtest.UserToken) {
		t.Errorf("a credential in the answer: %s", rec.Body.String())
	}
}

func TestGitHubCallbackRefusesAnInstallationThePersonCannotSee(t *testing.T) {
	s, _, _ := githubServer(t)
	rec := installed(t, s, "99", githubtest.Code, connectState(t, s))
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "cannot see installation 99") {
		t.Fatalf("forged id: %d %s", rec.Code, rec.Body.String())
	}
	if list, _ := s.store.GitHubInstallations(context.Background(), store.DefaultWorkspace); len(list) != 0 {
		t.Errorf("nothing is connected: %+v", list)
	}
	if rec := installed(t, s, "42", "stale-code", connectState(t, s)); rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "Try again") {
		t.Errorf("a code GitHub refuses: %d %s", rec.Code, rec.Body.String())
	}
}

func TestGitHubStateIsUsedOnceAndExpires(t *testing.T) {
	s, _, _ := githubServer(t)
	state := connectState(t, s)
	if rec := installed(t, s, "42", githubtest.Code, state); rec.Code != http.StatusFound {
		t.Fatalf("first: %d %s", rec.Code, rec.Body.String())
	}
	if rec := installed(t, s, "42", githubtest.Code, state); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "used") {
		t.Errorf("replayed: %d %s", rec.Code, rec.Body.String())
	}
	expired := signGitHubState(githubtest.ClientSecret, githubState{Workspace: "default", Actor: "x", Exp: time.Now().Add(-time.Minute).Unix(), Nonce: "n1"})
	otherKey := signGitHubState("not-the-client-secret", githubState{Workspace: "default", Actor: "x", Exp: time.Now().Add(time.Minute).Unix(), Nonce: "n2"})
	fresh := signGitHubState(githubtest.ClientSecret, githubState{Workspace: "default", Actor: "x", Exp: time.Now().Add(time.Minute).Unix(), Nonce: "n3"})
	body, tag, _ := strings.Cut(fresh, ".")
	tampered := body[:len(body)-2] + "AA." + tag
	for name, st := range map[string]string{"expired": expired, "another key": otherKey, "tampered": tampered, "none": "", "garbage": "x.y"} {
		if rec := installed(t, s, "42", githubtest.Code, st); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
}

func TestReadGitHubStateChecksTheTagAndTheTime(t *testing.T) {
	now := time.Now()
	st := githubState{Workspace: "acme", Actor: "ana@acme.test", Exp: now.Add(time.Minute).Unix(), Nonce: "n"}
	got, err := readGitHubState("k", signGitHubState("k", st), now)
	if err != nil || got != st {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := readGitHubState("k", signGitHubState("k", st), now.Add(2*time.Minute)); err == nil {
		t.Error("expired")
	}
	if _, err := readGitHubState("", signGitHubState("", st), now); err == nil {
		t.Error("no key, no state")
	}
}

func TestGitHubCallbackSaysToTryAgainWhenGitHubIsDown(t *testing.T) {
	s, _, fake := githubServer(t)
	state := connectState(t, s)
	fake.SetDown(true)
	rec := installed(t, s, "42", githubtest.Code, state)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "Try again") || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
		t.Errorf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestGitHubCallbackOfAnUpdateOnlyGoesBack(t *testing.T) {
	s, _, _ := githubServer(t)
	rec := do(t, s, "GET", "/api/github/installed?installation_id=42&setup_action=update", "", false)
	if rec.Code != http.StatusFound || !strings.HasSuffix(rec.Header().Get("Location"), "/workspace/git") {
		t.Errorf("%d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestGitHubConnectMovesAnInstallationAndAudits(t *testing.T) {
	s, _, _ := githubServer(t)
	ctx := context.Background()
	if _, err := s.store.CreateWorkspace(ctx, store.Workspace{Slug: "beta", Name: "Beta"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.AddGitHubInstallation(ctx, "beta", store.GitHubInstallation{ID: 42, AccountLogin: "Acme", AccountType: "Organization", ConnectedBy: "bob@beta.test"}); err != nil {
		t.Fatal(err)
	}
	heard := githubAudits(t)
	if rec := installed(t, s, "42", githubtest.Code, connectState(t, s)); rec.Code != http.StatusFound {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if got, _ := s.store.GitHubInstallation(ctx, 42); got == nil || got.Workspace != store.DefaultWorkspace {
		t.Errorf("moved to %+v", got)
	}
	if len(*heard) != 2 || (*heard)[0].Action != "github.move" || (*heard)[0].Detail != "from workspace beta to workspace default" || (*heard)[1].Action != "github.connect" {
		t.Errorf("audit %+v", *heard)
	}
}

func TestGitHubRepositoriesAndBranchesForThePicker(t *testing.T) {
	s, _, fake := githubServer(t)
	ctx := context.Background()
	if _, err := s.store.AddGitHubInstallation(ctx, store.DefaultWorkspace, store.GitHubInstallation{ID: 42, AccountLogin: "Acme", AccountType: "Organization", ConnectedBy: "ana"}); err != nil {
		t.Fatal(err)
	}
	rec := do(t, s, "GET", "/api/workspace/github/repositories", "", true)
	var repos []GitHubRepositoryView
	_ = json.Unmarshal(rec.Body.Bytes(), &repos)
	if rec.Code != http.StatusOK || len(repos) != 2 || repos[0].FullName != "Acme/shop" || !repos[0].Private || repos[0].DefaultBranch != "main" || repos[0].Installation != 42 || repos[1].FullName != "Acme/site" {
		t.Fatalf("repositories: %d %s", rec.Code, rec.Body.String())
	}
	// Kept a minute: a second listing does not ask GitHub again.
	calls := len(fake.Calls())
	do(t, s, "GET", "/api/workspace/github/repositories", "", true)
	if len(fake.Calls()) != calls {
		t.Errorf("listed again: %v", fake.Calls()[calls:])
	}
	rec = do(t, s, "GET", "/api/workspace/github/repositories/acme/shop/branches", "", true)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `["dev","main"]` {
		t.Errorf("branches: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, s, "GET", "/api/workspace/github/repositories/acme/vault/branches", "", true); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "does not include vault") {
		t.Errorf("not granted: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, s, "GET", "/api/workspace/github/repositories/other/shop/branches", "", true); rec.Code != http.StatusNotFound {
		t.Errorf("an owner no connection reaches: %d", rec.Code)
	}
	fake.SetDown(true)
	s.forgetGitHubRepos(store.DefaultWorkspace)
	if rec := do(t, s, "GET", "/api/workspace/github/repositories", "", true); rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "Acme") {
		t.Errorf("GitHub down: %d %s", rec.Code, rec.Body.String())
	}
}

func TestGitHubDisconnect(t *testing.T) {
	s, _, _ := githubServer(t)
	ctx := context.Background()
	if _, err := s.store.AddGitHubInstallation(ctx, store.DefaultWorkspace, store.GitHubInstallation{ID: 42, AccountLogin: "Acme", AccountType: "Organization", ConnectedBy: "ana"}); err != nil {
		t.Fatal(err)
	}
	heard := githubAudits(t)
	if rec := do(t, s, "DELETE", "/api/workspace/github/installations/42", "", true); rec.Code != http.StatusNoContent {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if list, _ := s.store.GitHubInstallations(ctx, store.DefaultWorkspace); len(list) != 0 {
		t.Errorf("still there: %+v", list)
	}
	if len(*heard) != 1 || (*heard)[0].Action != "github.disconnect" || (*heard)[0].Detail != "workspace default" || (*heard)[0].Target != "github installation 42 (Acme)" {
		t.Errorf("audit %+v", *heard)
	}
	if rec := do(t, s, "DELETE", "/api/workspace/github/installations/42", "", true); rec.Code != http.StatusNotFound {
		t.Errorf("twice: %d", rec.Code)
	}
	if rec := do(t, s, "DELETE", "/api/workspace/github/installations/nope", "", true); rec.Code != http.StatusBadRequest {
		t.Errorf("not an id: %d", rec.Code)
	}
}

func TestGitHubRoutesWithoutTheApp(t *testing.T) {
	s, cr := newTestServer(t, nil, nil)
	s.githubApp = githubtest.NewServer(t).Source(cr, "shpyrd-system") // no Secret in the cluster
	rec := do(t, s, "POST", "/api/workspace/github/connect", "", true)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "GitHub is not set up on this platform") || !strings.Contains(rec.Body.String(), "shpyrd-ctl github app create") {
		t.Errorf("connect: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(t, s, "GET", "/api/workspace/github", "", true)
	var v GitHubView
	_ = json.Unmarshal(rec.Body.Bytes(), &v)
	if rec.Code != http.StatusOK || v.Configured || !strings.Contains(v.Note, "shpyrd-ctl github app create") || v.Installations == nil {
		t.Errorf("view: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, s, "GET", "/api/workspace/github/repositories", "", true); rec.Code != http.StatusConflict {
		t.Errorf("repositories: %d", rec.Code)
	}
	if rec := do(t, s, "GET", "/api/github/installed?installation_id=42&setup_action=install&code=c&state=s", "", false); rec.Code != http.StatusNotFound {
		t.Errorf("callback: %d", rec.Code)
	}
}

func TestGitHubRoutesNeedTheirRoles(t *testing.T) {
	s, _, _ := githubServer(t)
	s.authz.TTL = 1
	ctx := context.Background()
	person := func(email, role string) (string, string) {
		p, err := s.store.TouchIdentity(ctx, store.DefaultWorkspace, store.Identity{Email: email, Provider: "local"})
		if err != nil {
			t.Fatal(err)
		}
		if role != "" {
			if _, err := s.store.PutMembership(ctx, store.DefaultWorkspace, p.ID, role); err != nil {
				t.Fatal(err)
			}
		}
		return signIn(t, s, ext.Identity{Subject: p.ID, Email: email, Provider: "local"})
	}
	adminSID, adminCSRF := person("admin@example.test", store.WorkspaceRoleAdmin)
	memberSID, memberCSRF := person("member@example.test", store.WorkspaceRoleMember)
	strangerSID, _ := person("stranger@example.test", "")
	if rec := doCookie(t, s, "GET", "/api/workspace/github", "", adminSID, ""); rec.Code != http.StatusOK {
		t.Errorf("admin reads: %d", rec.Code)
	}
	if rec := doCookie(t, s, "POST", "/api/workspace/github/connect", "", adminSID, adminCSRF); rec.Code != http.StatusOK {
		t.Errorf("admin connects: %d", rec.Code)
	}
	if rec := doCookie(t, s, "GET", "/api/workspace/github", "", memberSID, ""); rec.Code != http.StatusForbidden {
		t.Errorf("member reads the connections: %d", rec.Code)
	}
	if rec := doCookie(t, s, "POST", "/api/workspace/github/connect", "", memberSID, memberCSRF); rec.Code != http.StatusForbidden {
		t.Errorf("member connects: %d", rec.Code)
	}
	if rec := doCookie(t, s, "GET", "/api/workspace/github/repositories", "", memberSID, ""); rec.Code != http.StatusOK {
		t.Errorf("a member, who makes projects, lists repositories: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doCookie(t, s, "GET", "/api/workspace/github/repositories", "", strangerSID, ""); rec.Code != http.StatusForbidden {
		t.Errorf("stranger lists: %d", rec.Code)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./pkg/api/ -run 'GitHub'`
Expected: FAIL to compile: `s.githubApp undefined`, `undefined: GitHubConnectView`, `undefined: signGitHubState`.

- [ ] **Step 3: Write `pkg/api/github.go`**

```go
package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shpyrd-io/shpyrd/pkg/github"
	"github.com/shpyrd-io/shpyrd/pkg/pages"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// Connect GitHub (RFC-0054): a workspace admin sends the browser to
// GitHub's install page with a signed state; GitHub sends it back to
// /api/github/installed, where the person's own OAuth token says which
// installations they can see, and the one they chose is connected to the
// workspace the state names. Then anyone who makes projects lists its
// repositories and branches.

// GitHubView is the workspace's GitHub: whether the platform has an App,
// and the installations connected.
type GitHubView struct {
	Configured bool   `json:"configured"`
	AppURL     string `json:"appURL,omitempty"`
	// Note says why there is nothing to connect, when there is not.
	Note          string                   `json:"note,omitempty"`
	Installations []GitHubInstallationView `json:"installations"`
}

// GitHubInstallationView is one connection.
type GitHubInstallationView struct {
	ID          int64     `json:"id"`
	Account     string    `json:"account"`
	Type        string    `json:"type"`
	ConnectedBy string    `json:"connectedBy"`
	CreatedAt   time.Time `json:"createdAt"`
	Suspended   bool      `json:"suspended"`
	// ManageURL is the installation's settings on GitHub.
	ManageURL string `json:"manageURL"`
}

// GitHubConnectView is where to send the browser.
type GitHubConnectView struct {
	InstallURL string `json:"installURL"`
}

// GitHubRepositoryView is a repository a connection reaches.
type GitHubRepositoryView struct {
	Installation  int64  `json:"installation"`
	FullName      string `json:"fullName"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"defaultBranch"`
}

const (
	githubStateTTL = 15 * time.Minute
	githubReposTTL = 60 * time.Second
)

// githubState is what the install link carries through GitHub: the
// workspace, who started, until when, and a nonce that makes it single use.
type githubState struct {
	Workspace string `json:"ws"`
	Actor     string `json:"actor"`
	Exp       int64  `json:"exp"`
	Nonce     string `json:"n"`
}

var errGitHubState = errors.New("the link is not one shpyrd made, or it expired")

func githubStateTag(key, body string) []byte {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(body))
	return mac.Sum(nil)
}

// signGitHubState is base64url JSON, a dot, and its HMAC-SHA256 keyed by
// the App's client secret.
func signGitHubState(key string, st githubState) string {
	raw, _ := json.Marshal(st)
	body := base64.RawURLEncoding.EncodeToString(raw)
	return body + "." + base64.RawURLEncoding.EncodeToString(githubStateTag(key, body))
}

// readGitHubState checks the tag in constant time, then the time.
func readGitHubState(key, raw string, now time.Time) (githubState, error) {
	body, tag, ok := strings.Cut(raw, ".")
	if !ok || key == "" {
		return githubState{}, errGitHubState
	}
	got, err := base64.RawURLEncoding.DecodeString(tag)
	if err != nil || !hmac.Equal(got, githubStateTag(key, body)) {
		return githubState{}, errGitHubState
	}
	js, err := base64.RawURLEncoding.DecodeString(body)
	var st githubState
	if err != nil || json.Unmarshal(js, &st) != nil || st.Workspace == "" || st.Nonce == "" || now.Unix() > st.Exp {
		return githubState{}, errGitHubState
	}
	return st, nil
}

// nonceSet remembers the states used until they expire. The server runs
// one replica.
type nonceSet struct {
	mu   sync.Mutex
	used map[string]time.Time
}

// use says whether the nonce was unused, and marks it used.
func (n *nonceSet) use(nonce string, until, now time.Time) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.used == nil {
		n.used = map[string]time.Time{}
	}
	for k, t := range n.used {
		if now.After(t) {
			delete(n.used, k)
		}
	}
	if _, ok := n.used[nonce]; ok {
		return false
	}
	n.used[nonce] = until
	return true
}

// githubRepoCache keeps each workspace's listing a minute: the picker
// asks on every keystroke's render, GitHub counts every call.
type githubRepoCache struct {
	mu sync.Mutex
	by map[string]githubRepoEntry
}

type githubRepoEntry struct {
	at   time.Time
	list []GitHubRepositoryView
}

func (g *githubRepoCache) get(ws string, now time.Time) ([]GitHubRepositoryView, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	e, ok := g.by[ws]
	return e.list, ok && now.Sub(e.at) < githubReposTTL
}

func (g *githubRepoCache) put(ws string, list []GitHubRepositoryView, now time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.by == nil {
		g.by = map[string]githubRepoEntry{}
	}
	g.by[ws] = githubRepoEntry{at: now, list: list}
}

// forgetGitHubRepos drops a workspace's listing: its connections changed.
func (s *Server) forgetGitHubRepos(ws string) {
	s.githubRepos.mu.Lock()
	delete(s.githubRepos.by, ws)
	s.githubRepos.mu.Unlock()
}

// githubMissing says there is no App, and who sets one up on self-hosted.
func (s *Server) githubMissing() string {
	msg := "GitHub is not set up on this platform."
	if !s.hasCapability("workspaces") {
		msg += " An operator sets it up with `shpyrd-ctl github app create`."
	}
	return msg
}

// githubClient is the App's client, or a 409 when the platform has none.
func (s *Server) githubClient(c *gin.Context) (*github.Client, bool) {
	cl, err := s.githubApp.Client(c.Request.Context())
	if errors.Is(err, github.ErrNotConfigured) {
		abort(c, http.StatusConflict, errors.New(s.githubMissing()))
		return nil, false
	}
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return nil, false
	}
	return cl, true
}

func githubTarget(id int64, account string) string {
	return fmt.Sprintf("github installation %d (%s)", id, account)
}

// githubManageURL is the installation's settings page on GitHub.
func githubManageURL(web string, inst store.GitHubInstallation) string {
	if inst.AccountType == "Organization" {
		return fmt.Sprintf("%s/organizations/%s/settings/installations/%d", web, url.PathEscape(inst.AccountLogin), inst.ID)
	}
	return fmt.Sprintf("%s/settings/installations/%d", web, inst.ID)
}

// githubConnect is POST /api/workspace/github/connect.
func (s *Server) githubConnect(c *gin.Context) {
	cl, ok := s.githubClient(c)
	if !ok {
		return
	}
	nonce, err := randomHex(16)
	if err != nil {
		abort(c, http.StatusInternalServerError, err)
		return
	}
	app := cl.App()
	st := githubState{Workspace: s.doorWorkspace(c), Actor: firstNonEmpty(actorOf(c), "anonymous"), Exp: time.Now().Add(githubStateTTL).Unix(), Nonce: nonce}
	c.JSON(http.StatusOK, GitHubConnectView{InstallURL: app.InstallURL() + "?state=" + url.QueryEscape(signGitHubState(app.ClientSecret, st))})
}

// githubView is GET /api/workspace/github.
func (s *Server) githubView(c *gin.Context) {
	ctx := c.Request.Context()
	view := GitHubView{Installations: []GitHubInstallationView{}}
	web := "https://github.com"
	cl, err := s.githubApp.Client(ctx)
	switch {
	case errors.Is(err, github.ErrNotConfigured):
		view.Note = s.githubMissing()
	case err != nil:
		abort(c, http.StatusBadGateway, err)
		return
	default:
		view.Configured, view.AppURL, web = true, cl.App().HTMLURL, cl.App().WebURL()
	}
	list, err := s.store.GitHubInstallations(ctx, s.doorWorkspace(c))
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	for _, inst := range list {
		view.Installations = append(view.Installations, GitHubInstallationView{
			ID: inst.ID, Account: inst.AccountLogin, Type: inst.AccountType, ConnectedBy: inst.ConnectedBy,
			CreatedAt: inst.CreatedAt, Suspended: inst.SuspendedAt != nil, ManageURL: githubManageURL(web, inst),
		})
	}
	c.JSON(http.StatusOK, view)
}

// githubDisconnect is DELETE /api/workspace/github/installations/:id: the
// workspace forgets it; the App stays installed on GitHub.
func (s *Server) githubDisconnect(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		abort(c, http.StatusBadRequest, errors.New("give the installation's id, a number: shpyrd github list shows it"))
		return
	}
	ctx := c.Request.Context()
	ws := s.doorWorkspace(c)
	account := ""
	if list, err := s.store.GitHubInstallations(ctx, ws); err == nil {
		for _, inst := range list {
			if inst.ID == id {
				account = inst.AccountLogin
			}
		}
	}
	err = s.store.DeleteGitHubInstallation(ctx, ws, id)
	if errors.Is(err, store.ErrNotFound) {
		abort(c, http.StatusNotFound, errors.New("this workspace has no such GitHub connection"))
		return
	}
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	s.forgetGitHubRepos(ws)
	s.audit(c, "", "github.disconnect", githubTarget(id, account), "workspace "+ws)
	c.Status(http.StatusNoContent)
}

// githubRepositories is GET /api/workspace/github/repositories: what the
// workspace's live connections reach, merged, sorted, kept a minute.
func (s *Server) githubRepositories(c *gin.Context) {
	cl, ok := s.githubClient(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	ws := s.doorWorkspace(c)
	now := time.Now()
	if list, ok := s.githubRepos.get(ws, now); ok {
		c.JSON(http.StatusOK, list)
		return
	}
	insts, err := s.store.GitHubInstallations(ctx, ws)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	out := []GitHubRepositoryView{}
	for _, inst := range insts {
		if inst.SuspendedAt != nil {
			continue
		}
		repos, err := cl.Repositories(ctx, inst.ID)
		if err != nil {
			abort(c, http.StatusBadGateway, fmt.Errorf("GitHub did not list the repositories of %s: %w. Try again in a minute", inst.AccountLogin, err))
			return
		}
		for _, r := range repos {
			out = append(out, GitHubRepositoryView{Installation: r.Installation, FullName: r.FullName, Private: r.Private, DefaultBranch: r.DefaultBranch})
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].FullName) < strings.ToLower(out[j].FullName) })
	s.githubRepos.put(ws, out, now)
	c.JSON(http.StatusOK, out)
}

// githubBranches is GET /api/workspace/github/repositories/:owner/:repo/branches.
func (s *Server) githubBranches(c *gin.Context) {
	cl, ok := s.githubClient(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	owner, repo := c.Param("owner"), c.Param("repo")
	insts, err := s.store.GitHubInstallations(ctx, s.doorWorkspace(c))
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	for _, inst := range insts {
		if inst.SuspendedAt != nil || !strings.EqualFold(inst.AccountLogin, owner) {
			continue
		}
		names, err := cl.Branches(ctx, inst.ID, owner, repo)
		if errors.Is(err, github.ErrRepoNotGranted) {
			abort(c, http.StatusNotFound, fmt.Errorf("the GitHub connection on %s does not include %s: add it to the installation on GitHub", inst.AccountLogin, repo))
			return
		}
		if err != nil {
			abort(c, http.StatusBadGateway, fmt.Errorf("GitHub did not list the branches of %s/%s: %w", owner, repo, err))
			return
		}
		c.JSON(http.StatusOK, names)
		return
	}
	abort(c, http.StatusNotFound, fmt.Errorf("no GitHub connection of this workspace reaches %s", owner))
}

// githubInstalled is GET /api/github/installed, where GitHub sends the
// browser after an install (public: no session comes back with it).
func (s *Server) githubInstalled(c *gin.Context) {
	ctx := c.Request.Context()
	cl, err := s.githubApp.Client(ctx)
	if err != nil {
		s.githubPage(c, http.StatusNotFound, "GitHub is not set up here", "This platform has no GitHub App, so nothing was connected.", "")
		return
	}
	id, err := strconv.ParseInt(c.Query("installation_id"), 10, 64)
	if err != nil || id <= 0 {
		s.githubPage(c, http.StatusBadRequest, "GitHub sent no installation", "Nothing was connected. Start again from the workspace's Git settings: Connect GitHub.", "")
		return
	}
	if c.Query("setup_action") == "update" {
		// The installation's repositories changed on GitHub: nothing to
		// connect, and no state comes from GitHub's own settings page.
		ws := ""
		if inst, err := s.store.GitHubInstallation(ctx, id); err == nil {
			ws = inst.Workspace
		}
		c.Redirect(http.StatusFound, s.gitSettingsURL(ctx, ws))
		return
	}
	now := time.Now()
	st, err := readGitHubState(cl.App().ClientSecret, c.Query("state"), now)
	if err != nil {
		s.githubPage(c, http.StatusBadRequest, "This link has expired", "Nothing was connected. Start again from the workspace's Git settings: Connect GitHub.", "")
		return
	}
	back := s.gitSettingsURL(ctx, st.Workspace)
	if !s.githubNonces.use(st.Nonce, time.Unix(st.Exp, 0), now) {
		s.githubPage(c, http.StatusBadRequest, "This link was used already", "Each Connect GitHub link connects once. Start again from the workspace's Git settings.", back)
		return
	}
	code := c.Query("code")
	if code == "" {
		s.githubPage(c, http.StatusBadRequest, "GitHub did not say who you are", "Nothing was connected. Start again from the workspace's Git settings: Connect GitHub.", back)
		return
	}
	mine, err := personsInstallations(ctx, cl, code)
	if err != nil {
		s.log.Warn("github: could not check the person's installations", "err", err.Error())
		s.githubPage(c, http.StatusBadGateway, "GitHub did not answer", "shpyrd could not ask GitHub which installations you can see. Try again from the workspace's Git settings in a minute.", back)
		return
	}
	var inst *github.Installation
	for i := range mine {
		if mine[i].ID == id {
			inst = &mine[i]
		}
	}
	if inst == nil {
		s.githubPage(c, http.StatusForbidden, "Not an installation you can see",
			fmt.Sprintf("GitHub says your account cannot see installation %d. Install the App on an account or organisation you manage, then connect it.", id), back)
		return
	}
	moved, err := s.store.AddGitHubInstallation(ctx, st.Workspace, store.GitHubInstallation{
		ID: id, AccountLogin: inst.Account, AccountType: inst.AccountType, ConnectedBy: st.Actor, SuspendedAt: inst.SuspendedAt,
	})
	if err != nil {
		s.log.Warn("github: could not record the installation", "installation", id, "err", err.Error())
		s.githubPage(c, http.StatusBadGateway, "Not connected", "shpyrd could not record the connection. Try again from the workspace's Git settings in a minute.", back)
		return
	}
	s.forgetGitHubRepos(st.Workspace)
	target := githubTarget(id, inst.Account)
	if moved != "" {
		s.forgetGitHubRepos(moved)
		s.auditActor(c, st.Actor, "github.move", target, "from workspace "+moved+" to workspace "+st.Workspace)
	}
	s.auditActor(c, st.Actor, "github.connect", target, "workspace "+st.Workspace)
	c.Redirect(http.StatusFound, back)
}

// personsInstallations are the App's installations the person behind the
// OAuth code can see. Their token only says who they are: it never leaves
// this function.
func personsInstallations(ctx context.Context, cl *github.Client, code string) ([]github.Installation, error) {
	token, err := cl.ExchangeCode(ctx, code)
	if err != nil {
		return nil, err
	}
	return cl.UserInstallations(ctx, token)
}

// gitSettingsURL is the workspace's Git settings page; the console's
// dashboard when the workspace is unknown.
func (s *Server) gitSettingsURL(ctx context.Context, slug string) string {
	var ws *store.Workspace
	if slug != "" {
		ws, _ = s.store.Workspace(ctx, slug)
	}
	return s.dashboardURLOf(ws) + "/workspace/git"
}

// githubPage is what the browser GitHub sent back sees when nothing was
// connected: a plain page, the lock when it is refused.
func (s *Server) githubPage(c *gin.Context, status int, title, text, back string) {
	kind := pages.Nothing
	if status == http.StatusForbidden {
		kind = pages.NoAccess
	}
	var links []pages.Link
	if back != "" {
		links = []pages.Link{{Label: "Back to the Git settings", URL: back}}
	}
	c.Header("Cache-Control", "no-store")
	c.Data(status, "text/html; charset=utf-8", []byte(pages.HTML(kind, pages.Page{Title: title, Text: text, Links: links})))
}
```

- [ ] **Step 4: Wire the server**

In `pkg/api/server.go`, add to the `Server` struct after `hookRefusals *rateLimiter`:

```go
	// The GitHub App (RFC-0054): its credentials, read from its Secret;
	// the connect states used; each workspace's repositories, a minute;
	// and the App webhook's own limiter (Task 5).
	githubApp        *github.Source
	githubNonces     *nonceSet
	githubRepos      *githubRepoCache
	githubDeliveries *rateLimiter
```

In `newServer`, after `s.hookRefusals = newRateLimiter(hookRefusalsPerMinute)`:

```go
	s.githubNonces = &nonceSet{}
	s.githubRepos = &githubRepoCache{}
	s.githubDeliveries = newRateLimiter(githubHooksPerMinute)
```

and after the block that sets `systemNS` from `k` (~398-405):

```go
	s.githubApp = &github.Source{Reader: opts.Apps, Namespace: systemNS, Options: github.Options{APIURL: opts.Vars(github.EnvAPIURL)}}
```

(`opts.Apps` may be nil in tests that build no cluster: `Source.Client` then answers `ErrNotConfigured`). Add `"github.com/shpyrd-io/shpyrd/pkg/github"` to the imports, and in `pkg/api/github.go` the constant Task 5 also uses:

```go
// githubHooksPerMinute is how many App webhook deliveries one address may
// make: every workspace's pushes come from GitHub's few hook addresses.
const githubHooksPerMinute = 3000
```

Routes. After `pub.POST("/cli/device/token", login, s.cliDevicePoll)`:

```go
	// GitHub sends the browser here after an install (RFC-0054): no
	// session; the signed state and the person's own GitHub token decide.
	pub.GET("/github/installed", login, s.githubInstalled)
```

After the three `/workspace/git-auth` routes:

```go
	api.GET("/workspace/github", s.require(authz.ClusterAdmin), s.githubView) // RFC-0054
	api.POST("/workspace/github/connect", s.require(authz.ClusterAdmin), s.githubConnect)
	api.DELETE("/workspace/github/installations/:id", s.require(authz.ClusterAdmin), s.githubDisconnect)
	// Who makes projects picks from the repositories (ClusterCreate, the
	// action of POST /projects).
	api.GET("/workspace/github/repositories", s.require(authz.ClusterCreate), s.githubRepositories)
	api.GET("/workspace/github/repositories/:owner/:repo/branches", s.require(authz.ClusterCreate), s.githubBranches)
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./pkg/api/ -run 'GitHub' -race && go test ./pkg/api/`
Expected: PASS. If `TestGitHubRoutesNeedTheirRoles` finds roles not enforced (every signed-in person a platform admin while the workspace has no team), create a team first through the admin token as `TestAuthorization` does (`POST /api/teams` with `{"name":"ops","members":["ops@example.test"],"platformRole":"platform-admin"}`); the assertions stay.

- [ ] **Step 6: Commit**

```bash
git add pkg/api/github.go pkg/api/github_test.go pkg/api/server.go
git commit -m "feat(api): connect GitHub: signed single-use state, the install callback, connections, repositories and branches (RFC-0054)

Co-Authored-By: <your model name> <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 5: The App's webhook: pushes routed to apps, installation events

**Files:**
- Modify: `pkg/gitauth/webhook.go` (after `VerifyDelivery`, ~52), `pkg/gitauth/webhook_test.go`
- Create: `pkg/api/github_webhook.go`
- Modify: `pkg/api/server.go` (public route after `pub.POST("/hooks/git/:slug/:token", s.gitHook)`, ~586)
- Test: `pkg/api/github_webhook_test.go`

**Interfaces:**
- Consumes: `s.githubApp`, `s.githubDeliveries`, `s.forgetGitHubRepos`, `githubTarget`, `githubServer`, `githubAudits` (Task 4); `store.GitHub` (Task 2); PR 2's `gitauth.ParsePush`, `ignoredPush`, `s.hookRefusals`, `hmacHex` (`githooks_test.go`), `hookCommit`; `workspaceOf` (`edge.go:906`), `project.SlugOf`, `s.auditActor`.
- Produces:
  - `func gitauth.VerifyGitHubDelivery(h http.Header, body []byte, secret string) error`
  - `const githubHookBodyLimit = 5 << 20`
  - `func namesGitHubRepo(app *shpyrdv1.App, fullName string) bool`
  - Route: `POST /api/github/webhook` (public)

- [ ] **Step 1: Write the failing tests**

Append to `pkg/gitauth/webhook_test.go`:

```go
func TestVerifyGitHubDeliveryTakesOnlyGitHubsSignature(t *testing.T) {
	body := []byte(`{"zen":"Keep it logically awesome."}`)
	mac := hmac.New(sha256.New, []byte("whsec"))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))
	h := http.Header{}
	h.Set("X-Hub-Signature-256", "sha256="+sig)
	if err := VerifyGitHubDelivery(h, body, "whsec"); err != nil {
		t.Errorf("GitHub's signature: %v", err)
	}
	for name, h := range map[string]http.Header{
		"GitLab's token":    {"X-Gitlab-Token": {"whsec"}},
		"Gitea's signature": {"X-Gitea-Signature": {sig}},
		"none":              {},
	} {
		if err := VerifyGitHubDelivery(h, body, "whsec"); !errors.Is(err, ErrBadSignature) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := VerifyGitHubDelivery(h, body, ""); !errors.Is(err, ErrBadSignature) {
		t.Errorf("no secret: %v", err)
	}
}
```

(add whichever of `crypto/hmac`, `crypto/sha256`, `encoding/hex`, `errors`, `net/http` the file lacks).

`pkg/api/github_webhook_test.go`:

```go
package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
	"github.com/shpyrd-io/shpyrd/pkg/github/githubtest"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// deliverApp posts to the App's webhook, signed with secret, as GitHub does.
func deliverApp(t *testing.T, s *Server, event, delivery string, body []byte, secret string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/github/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", event)
	req.Header.Set("X-GitHub-Delivery", delivery)
	req.Header.Set("X-Hub-Signature-256", "sha256="+hmacHex(secret, body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func appPush(installation int64, fullName, ref string) []byte {
	return []byte(fmt.Sprintf(`{"ref":%q,"before":"0123456789abcdef0123456789abcdef01234567","after":%q,"installation":{"id":%d},"repository":{"full_name":%q},"head_commit":{"id":%q,"message":"fix login","author":{"name":"Alice","username":"alice"}}}`,
		ref, hookCommit, installation, fullName, hookCommit))
}

func installationEvent(action string, id int64) []byte {
	return []byte(fmt.Sprintf(`{"action":%q,"installation":{"id":%d,"account":{"login":"Acme","type":"Organization"}},"sender":{"login":"octo"}}`, action, id))
}

func onGitHub(name, url, rev, workspace string) *shpyrdv1.App {
	app := sampleApp(name, shpyrdv1.PhaseRunning)
	app.Spec.Source = &shpyrdv1.Source{Git: &shpyrdv1.GitSource{URL: url, Revision: rev}}
	if workspace != "" {
		app.Labels = map[string]string{shpyrdv1.LabelWorkspace: workspace}
	}
	return app
}

func connectAcme(t *testing.T, s *Server, ws string) {
	t.Helper()
	if _, err := s.store.AddGitHubInstallation(context.Background(), ws, store.GitHubInstallation{ID: 42, AccountLogin: "Acme", AccountType: "Organization", ConnectedBy: "ana"}); err != nil {
		t.Fatal(err)
	}
}

func pushOn(t *testing.T, cr client.Client, slug string) *gitauth.Push {
	t.Helper()
	p, err := gitauth.ReadPush(appNamed(t, cr, slug).Annotations[shpyrdv1.AnnotationGitPush])
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestGitHubWebhookRoutesAPushToTheProjectsOnThatRepositoryWhateverTheCase(t *testing.T) {
	s, cr, _ := githubServer(t,
		onGitHub("shop", "https://github.com/acme/shop", "main", ""),
		onGitHub("shopsh", "git@github.com:ACME/Shop.git", "", ""),
		onGitHub("shopdev", "https://github.com/acme/shop", "dev", ""),
		onGitHub("site", "https://github.com/acme/site", "main", ""),
		onGitHub("theirs", "https://github.com/acme/shop", "main", "beta"),
	)
	connectAcme(t, s, store.DefaultWorkspace)
	rec := deliverApp(t, s, "push", "d-1", appPush(42, "Acme/Shop", "refs/heads/main"), githubtest.WebhookSecret)
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"projects":["shop","shopsh"]`) {
		t.Fatalf("push: %d %s", rec.Code, rec.Body.String())
	}
	for _, slug := range []string{"shop", "shopsh"} {
		if p := pushOn(t, cr, slug); p == nil || p.SHA != hookCommit || p.Delivery != "d-1" || p.Author != "alice" || p.Ref != "refs/heads/main" {
			t.Errorf("%s: %+v", slug, p)
		}
	}
	for _, slug := range []string{"shopdev", "site", "theirs"} {
		if p := pushOn(t, cr, slug); p != nil {
			t.Errorf("%s took a push not for it: %+v", slug, p)
		}
	}
	// GitHub delivers the same push again: nothing is written twice.
	before := appNamed(t, cr, "shop").Annotations[shpyrdv1.AnnotationGitPush]
	if rec := deliverApp(t, s, "push", "d-1", appPush(42, "Acme/Shop", "refs/heads/main"), githubtest.WebhookSecret); rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"projects":[]`) {
		t.Errorf("again: %d %s", rec.Code, rec.Body.String())
	}
	if appNamed(t, cr, "shop").Annotations[shpyrdv1.AnnotationGitPush] != before {
		t.Error("a repeated delivery was applied again")
	}
}

func TestGitHubWebhookBeforeTheCallbackIsAcceptedAndIgnored(t *testing.T) {
	s, cr, _ := githubServer(t, onGitHub("shop", "https://github.com/acme/shop", "main", ""))
	if rec := deliverApp(t, s, "installation", "d-0", installationEvent("created", 42), githubtest.WebhookSecret); rec.Code != http.StatusAccepted {
		t.Errorf("created: %d", rec.Code)
	}
	rec := deliverApp(t, s, "push", "d-1", appPush(42, "Acme/shop", "refs/heads/main"), githubtest.WebhookSecret)
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), "no workspace has connected") {
		t.Fatalf("early push: %d %s", rec.Code, rec.Body.String())
	}
	if p := pushOn(t, cr, "shop"); p != nil {
		t.Fatalf("an installation no workspace connected wrote %+v", p)
	}
	if _, err := s.store.GitHubInstallation(context.Background(), 42); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("installation.created connects nothing: %v", err)
	}
	connectAcme(t, s, store.DefaultWorkspace)
	if rec := deliverApp(t, s, "push", "d-2", appPush(42, "Acme/shop", "refs/heads/main"), githubtest.WebhookSecret); rec.Code != http.StatusAccepted {
		t.Fatalf("after connecting: %d", rec.Code)
	}
	if p := pushOn(t, cr, "shop"); p == nil || p.Delivery != "d-2" {
		t.Errorf("after connecting: %+v", p)
	}
}

func TestGitHubWebhookRecordsInstallationEvents(t *testing.T) {
	s, _, _ := githubServer(t)
	connectAcme(t, s, store.DefaultWorkspace)
	heard := githubAudits(t)
	ctx := context.Background()
	if rec := deliverApp(t, s, "installation", "d-1", installationEvent("suspend", 42), githubtest.WebhookSecret); rec.Code != http.StatusAccepted {
		t.Fatalf("suspend: %d %s", rec.Code, rec.Body.String())
	}
	if got, _ := s.store.GitHubInstallation(ctx, 42); got == nil || got.SuspendedAt == nil {
		t.Errorf("suspended: %+v", got)
	}
	deliverApp(t, s, "installation", "d-2", installationEvent("unsuspend", 42), githubtest.WebhookSecret)
	if got, _ := s.store.GitHubInstallation(ctx, 42); got == nil || got.SuspendedAt != nil {
		t.Errorf("unsuspended: %+v", got)
	}
	deliverApp(t, s, "installation", "d-3", installationEvent("deleted", 42), githubtest.WebhookSecret)
	if _, err := s.store.GitHubInstallation(ctx, 42); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("deleted: %v", err)
	}
	var actions []string
	for _, e := range *heard {
		actions = append(actions, e.Action)
		if e.Actor != "github:octo" || e.Detail != "workspace default" || e.Target != "github installation 42 (Acme)" {
			t.Errorf("entry %+v", e)
		}
	}
	if strings.Join(actions, ",") != "github.suspend,github.unsuspend,github.disconnect" {
		t.Errorf("audited %v", actions)
	}
	if rec := deliverApp(t, s, "installation", "d-4", installationEvent("deleted", 42), githubtest.WebhookSecret); rec.Code != http.StatusAccepted {
		t.Errorf("deleted again: %d", rec.Code)
	}
	if rec := deliverApp(t, s, "installation_repositories", "d-5", []byte(`{"action":"added"}`), githubtest.WebhookSecret); rec.Code != http.StatusAccepted {
		t.Errorf("repositories changed: %d", rec.Code)
	}
}

func TestGitHubWebhookChecksTheSignatureAndAnswersAPing(t *testing.T) {
	s, _, _ := githubServer(t)
	body := []byte(`{"zen":"Design for failure."}`)
	if rec := deliverApp(t, s, "ping", "d-1", body, "not-the-secret"); rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong secret: %d", rec.Code)
	}
	req := httptest.NewRequest("POST", "/api/github/webhook", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "ping")
	req.Header.Set("X-Gitlab-Token", githubtest.WebhookSecret)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("a GitLab token is no GitHub signature: %d", rec.Code)
	}
	if rec := deliverApp(t, s, "ping", "d-2", body, githubtest.WebhookSecret); rec.Code != http.StatusOK || rec.Body.String() != "pong" {
		t.Errorf("ping: %d %s", rec.Code, rec.Body.String())
	}
	if rec := deliverApp(t, s, "check_suite", "d-3", body, githubtest.WebhookSecret); rec.Code != http.StatusAccepted {
		t.Errorf("another event: %d", rec.Code)
	}
	big := bytes.Repeat([]byte("x"), githubHookBodyLimit+1)
	if rec := deliverApp(t, s, "push", "d-4", big, githubtest.WebhookSecret); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("too large: %d", rec.Code)
	}
}

func TestGitHubWebhookIsNotThereWithoutTheApp(t *testing.T) {
	s, cr := newTestServer(t, nil, nil)
	s.githubApp = githubtest.NewServer(t).Source(cr, "shpyrd-system")
	if rec := deliverApp(t, s, "ping", "d-1", []byte(`{}`), githubtest.WebhookSecret); rec.Code != http.StatusNotFound {
		t.Errorf("%d", rec.Code)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./pkg/gitauth/ ./pkg/api/ -run 'GitHub'`
Expected: FAIL to compile: `undefined: VerifyGitHubDelivery`, `undefined: githubHookBodyLimit`.

- [ ] **Step 3: `gitauth.VerifyGitHubDelivery`**

In `pkg/gitauth/webhook.go`, after `VerifyDelivery`:

```go
// VerifyGitHubDelivery checks GitHub's X-Hub-Signature-256 alone: the
// GitHub App's webhook (RFC-0054) is only ever called by GitHub.
func VerifyGitHubDelivery(h http.Header, body []byte, secret string) error {
	if secret == "" {
		return ErrBadSignature
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	if sig, ok := strings.CutPrefix(h.Get("X-Hub-Signature-256"), "sha256="); ok && hexEqual(sig, mac.Sum(nil)) {
		return nil
	}
	return ErrBadSignature
}
```

- [ ] **Step 4: Write `pkg/api/github_webhook.go`**

```go
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
	"github.com/shpyrd-io/shpyrd/pkg/github"
	"github.com/shpyrd-io/shpyrd/pkg/project"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// The GitHub App's webhook (RFC-0054): one URL for every installation.
// A push goes to every App of the installation's workspace that builds
// that repository's branch, through the git-push annotation PR 2's source
// watcher reads; installation events keep the connection's state. Every
// answer that applies nothing is 2xx, so GitHub does not retry it.

// githubHookBodyLimit caps a delivery: GitHub sends at most 25 MB, a push
// of many commits stays far below this.
const githubHookBodyLimit = 5 << 20

func (s *Server) githubWebhook(c *gin.Context) {
	if !s.githubDeliveries.allow(c.ClientIP()) {
		c.Header("Retry-After", "60")
		abort(c, http.StatusTooManyRequests, errors.New("too many webhook deliveries from this address; shpyrd checks the branches by itself meanwhile"))
		return
	}
	ctx := c.Request.Context()
	cl, err := s.githubApp.Client(ctx)
	if errors.Is(err, github.ErrNotConfigured) {
		abort(c, http.StatusNotFound, errors.New("not found"))
		return
	}
	if err != nil {
		s.log.Warn("github webhook: could not read the App", "err", err.Error())
		abort(c, http.StatusBadGateway, errors.New("shpyrd could not take the delivery now; it reads the branches by itself meanwhile"))
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, githubHookBodyLimit))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		abort(c, http.StatusRequestEntityTooLarge, errors.New("the delivery is larger than shpyrd reads (5 MiB)"))
		return
	}
	if err != nil {
		abort(c, http.StatusBadRequest, fmt.Errorf("could not read the delivery: %w", err))
		return
	}
	if gitauth.VerifyGitHubDelivery(c.Request.Header, body, cl.App().WebhookSecret) != nil {
		if s.hookRefusals.allow("github-app") {
			s.log.Warn("github webhook refused: the signature is not the App's", "from", c.ClientIP())
		}
		abort(c, http.StatusUnauthorized, errors.New("the delivery's signature is not the GitHub App's"))
		return
	}
	switch event := c.GetHeader("X-GitHub-Event"); event {
	case "ping":
		c.String(http.StatusOK, "pong")
	case "push":
		s.githubPush(c, body, c.GetHeader("X-GitHub-Delivery"))
	case "installation":
		s.githubInstallationEvent(c, body)
	default:
		// installation_repositories: the picker reads live. check_suite,
		// status: part 4.
		c.JSON(http.StatusAccepted, gin.H{"ignored": event})
	}
}

// githubPush leaves the push on every App of the installation's workspace
// that builds the repository's branch.
func (s *Server) githubPush(c *gin.Context, body []byte, delivery string) {
	ctx := c.Request.Context()
	var meta struct {
		Installation struct {
			ID int64 `json:"id"`
		} `json:"installation"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(body, &meta); err != nil || meta.Installation.ID == 0 || meta.Repository.FullName == "" {
		abort(c, http.StatusBadRequest, errors.New("the push names no installation or no repository"))
		return
	}
	inst, err := s.store.GitHubInstallation(ctx, meta.Installation.ID)
	if errors.Is(err, store.ErrNotFound) {
		// Before the setup callback, or never connected here.
		c.JSON(http.StatusAccepted, gin.H{"ignored": "no workspace has connected this installation"})
		return
	}
	if err != nil {
		s.log.Warn("github webhook: could not look up the installation", "installation", meta.Installation.ID, "err", err.Error())
		abort(c, http.StatusBadGateway, errors.New("shpyrd could not take the delivery now; it reads the branches by itself meanwhile"))
		return
	}
	push, err := gitauth.ParsePush(body)
	if err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	push.Delivery, push.At = delivery, time.Now().UTC()
	var list shpyrdv1.AppList
	if err := s.apps.List(ctx, &list); err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	raw, _ := json.Marshal(push)
	patch, _ := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]string{shpyrdv1.AnnotationGitPush: string(raw)}}})
	accepted := []string{}
	for i := range list.Items {
		app := &list.Items[i]
		if workspaceOf(app) != inst.Workspace || !app.DeletionTimestamp.IsZero() || !namesGitHubRepo(app, meta.Repository.FullName) || ignoredPush(app, push) != "" {
			continue
		}
		if err := s.apps.Patch(ctx, app.DeepCopy(), client.RawPatch(types.MergePatchType, patch)); err != nil {
			abort(c, http.StatusBadGateway, err)
			return
		}
		accepted = append(accepted, project.SlugOf(app))
	}
	sort.Strings(accepted)
	c.JSON(http.StatusAccepted, gin.H{"accepted": push.SHA, "projects": accepted})
}

// namesGitHubRepo says the App builds the repository GitHub names
// (owner/name), in any case: GitHub's names are not case-sensitive.
func namesGitHubRepo(app *shpyrdv1.App, fullName string) bool {
	if app.Spec.Source == nil || app.Spec.Source.Git == nil {
		return false
	}
	r, err := gitauth.Parse(app.Spec.Source.Git.URL)
	return err == nil && r.Host == "github.com" && strings.EqualFold(r.Path, fullName)
}

// githubInstallationEvent records a suspension, its end, or an uninstall;
// "created" connects nothing (the setup callback does, for the person).
func (s *Server) githubInstallationEvent(c *gin.Context, body []byte) {
	ctx := c.Request.Context()
	var ev struct {
		Action       string `json:"action"`
		Installation struct {
			ID int64 `json:"id"`
		} `json:"installation"`
		Sender struct {
			Login string `json:"login"`
		} `json:"sender"`
	}
	if err := json.Unmarshal(body, &ev); err != nil || ev.Installation.ID == 0 {
		abort(c, http.StatusBadRequest, errors.New("the event names no installation"))
		return
	}
	var action string
	var suspended, deleted bool
	switch ev.Action {
	case "suspend":
		action, suspended = "github.suspend", true
	case "unsuspend":
		action = "github.unsuspend"
	case "deleted":
		action, deleted = "github.disconnect", true
	default:
		c.JSON(http.StatusAccepted, gin.H{"ignored": "installation " + ev.Action})
		return
	}
	inst, err := s.store.GitHubInstallation(ctx, ev.Installation.ID)
	if errors.Is(err, store.ErrNotFound) {
		c.JSON(http.StatusAccepted, gin.H{"ignored": "no workspace has connected this installation"})
		return
	}
	if err != nil {
		abort(c, http.StatusBadGateway, errors.New("shpyrd could not take the delivery now"))
		return
	}
	if deleted {
		suspended = inst.SuspendedAt != nil
	}
	if err := s.store.SetGitHubInstallationState(ctx, inst.ID, suspended, deleted); err != nil {
		abort(c, http.StatusBadGateway, errors.New("shpyrd could not take the delivery now"))
		return
	}
	s.forgetGitHubRepos(inst.Workspace)
	s.auditActor(c, "github:"+firstNonEmpty(ev.Sender.Login, "unknown"), action, githubTarget(inst.ID, inst.AccountLogin), "workspace "+inst.Workspace)
	c.JSON(http.StatusAccepted, gin.H{"recorded": ev.Action})
}
```

In `pkg/api/server.go`, after `pub.POST("/hooks/git/:slug/:token", s.gitHook)`:

```go
	// The GitHub App's webhook (RFC-0054): no session; the App's webhook
	// secret signs every delivery. 404 while the platform has no App.
	pub.POST("/github/webhook", s.githubWebhook)
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./pkg/gitauth/ ./pkg/api/ -race`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/gitauth/webhook.go pkg/gitauth/webhook_test.go pkg/api/github_webhook.go pkg/api/github_webhook_test.go pkg/api/server.go
git commit -m "feat(api): the GitHub App's webhook: pushes to every app on the repository, installation events (RFC-0054)

Co-Authored-By: <your model name> <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 6: The controller: builds and the watcher clone through the App; the views say so

**Files:**
- Modify: `pkg/gitauth/resolve.go` (kinds ~14-17, annotations ~20-41, `Build` ~70-84)
- Create: `internal/controller/github.go`
- Modify: `internal/controller/gitcreds.go` (`reconcileGitCredentials` 26-60, `lookupGitBuild` 120-140), `internal/controller/source_controller.go` (`SourceReconciler` fields ~78-95, `describeSourceError` 447-468, `poll` 471-494), `internal/controller/app_controller.go` (Reconcile, after the `domainsPending` requeue ~278), `internal/controller/desired.go` (`Config`, after `Projects` ~88)
- Modify: `internal/controller/source_controller_test.go` (`TestLookupGitBuildIsWhatTheBuildUses`, 413-423)
- Modify: `pkg/server/server.go` (`newManager`: before `rec := &controller.AppReconciler{` ~505; `Config` ~527; `SourceReconciler` ~545-550)
- Modify: `pkg/api/gitauth.go` (`ProjectGitAuthView` 54-59, `showProjectGitAuth` 190-224), `internal/cli/gitauth.go` (show, ~167-180)
- Test: `internal/controller/github_test.go`, `pkg/api/gitauth_test.go`, `internal/cli/gitauth_test.go`

**Interfaces:**
- Consumes: `github.Source`, `github.ErrNotConfigured`, `github.ErrRepoNotGranted`, `(*github.Client).InstallationToken`, `github.Token.MintedAt`, `githubtest` (Task 1); `store.GitHub`, `store.GitHubInstallation` (Task 2); `gitauth.Resolve`, `gitauth.Parse`, `workspaceOf` (`desired.go:356`), `r.now()` (`health.go:150`), test helpers `newTestReconciler`, `gitApp`, `workspaceToken`, `pinGit`, `sourceNow`, `fakeLsRemote`, `newSourceReconciler`, `reconcileSource`, `sourceRecord`, `runShpyrd`.
- Produces:
  - `const gitauth.KindGitHubApp = "github-app"`, `gitauth.AnnotationGitHubAccount = "shpyrd.io/github-account"`, `gitauth.AnnotationTokenMintedAt = "shpyrd.io/github-token-minted-at"`, `gitauth.Build.AppRefused string`
  - `type controller.GitHubApp struct { Source *github.Source; Installations store.GitHub }`
  - `controller.Config.GitHub *GitHubApp`, `controller.SourceReconciler.GitHub *GitHubApp`
  - `func lookupGitBuild(ctx, c client.Reader, systemNS string, gh *GitHubApp, app *shpyrdv1.App) (gitauth.Build, error)`
  - `func (r *AppReconciler) githubTokenRefresh(ctx, app) time.Duration`, `const githubTokenLife = 50 * time.Minute`
  - `api.ProjectGitAuthView.GitHubAccount string` (JSON `githubAccount`), `Using` may be `"github-app"`

- [ ] **Step 1: Write the failing tests**

`internal/controller/github_test.go`:

```go
package controller

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
	"github.com/shpyrd-io/shpyrd/pkg/github/githubtest"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// connectedGitHub is the fake GitHub, on sourceNow's clock, with its
// installation 42 on Acme connected to the default workspace.
func connectedGitHub(t *testing.T) (*githubtest.Server, store.Store) {
	t.Helper()
	fake := githubtest.NewServer(t)
	fake.SetNow(sourceNow)
	st := store.NewMemory()
	if _, err := st.AddGitHubInstallation(context.Background(), store.DefaultWorkspace,
		store.GitHubInstallation{ID: 42, AccountLogin: "Acme", AccountType: "Organization", ConnectedBy: "ana@acme.test"}); err != nil {
		t.Fatal(err)
	}
	return fake, st
}

// githubReconciler is an AppReconciler on a platform with the fake's App.
func githubReconciler(t *testing.T, objs ...client.Object) (*AppReconciler, client.Client, *githubtest.Server, store.Store) {
	t.Helper()
	fake, st := connectedGitHub(t)
	r, c := newTestReconciler(t, append([]client.Object{fake.Secret("shpyrd-system")}, objs...)...)
	r.Config.GitHub = &GitHubApp{Source: fake.Source(c, "shpyrd-system"), Installations: st}
	r.Now = fake.Time
	return r, c, fake, st
}

func buildSecretOf(t *testing.T, c client.Client, app *shpyrdv1.App) *corev1.Secret {
	t.Helper()
	sec := &corev1.Secret{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: app.Namespace, Name: gitauth.BuildSecretName(app.Name)}, sec); err != nil {
		t.Fatal(err)
	}
	return sec
}

func TestGitHubAppClonesWithATokenForThatRepositoryAlone(t *testing.T) {
	// The URL says acme; GitHub's account is Acme.
	app := gitApp("shop", "https://github.com/acme/shop")
	r, c, fake, _ := githubReconciler(t, app, workspaceToken("default", "github.com", "ghp_ours"))
	got, err := r.reconcileGitCredentials(context.Background(), app)
	if err != nil || got.Kind != gitauth.KindGitHubApp || got.CloneURL != "https://github.com/acme/shop" || got.AppRefused != "" {
		t.Fatalf("build = %+v, %v", got, err)
	}
	sec := buildSecretOf(t, c, app)
	if string(sec.Data[corev1.BasicAuthUsernameKey]) != "x-access-token" || string(sec.Data[corev1.BasicAuthPasswordKey]) != "ghs_42_1" {
		t.Errorf("the App's token comes before the workspace's: %q / %q", sec.Data[corev1.BasicAuthUsernameKey], sec.Data[corev1.BasicAuthPasswordKey])
	}
	if sec.Annotations[gitauth.AnnotationGitHubAccount] != "Acme" || sec.Annotations[gitauth.AnnotationTokenMintedAt] != sourceNow.Format(time.RFC3339) {
		t.Errorf("annotations %v", sec.Annotations)
	}
	if body := fake.LastTokenRequest(); fmt.Sprint(body["repositories"]) != "[shop]" || fmt.Sprint(body["permissions"]) != "map[contents:read]" {
		t.Errorf("asked GitHub for %v", body)
	}
}

func TestGitHubAppFallsThroughWhenTheRepositoryIsNotGranted(t *testing.T) {
	app := gitApp("vault", "https://github.com/acme/vault")
	r, _, _, _ := githubReconciler(t, app, workspaceToken("default", "github.com", "ghp_ours"))
	b, err := lookupGitBuild(context.Background(), r.Client, "shpyrd-system", r.Config.GitHub, app)
	if err != nil || b.Kind != gitauth.KindToken || b.AppRefused != "The GitHub App's installation on Acme does not include vault: add the repository to it on GitHub." {
		t.Errorf("build = %+v, %v", b, err)
	}
}

func TestGitHubAppIsSkippedWhileSuspended(t *testing.T) {
	app := gitApp("shop", "https://github.com/acme/shop")
	r, _, fake, st := githubReconciler(t, app)
	ctx := context.Background()
	if err := st.SetGitHubInstallationState(ctx, 42, true, false); err != nil {
		t.Fatal(err)
	}
	b, err := lookupGitBuild(ctx, r.Client, "shpyrd-system", r.Config.GitHub, app)
	if err != nil || b.Kind != "" || b.AppRefused != "The GitHub App's installation on Acme is suspended: an owner of Acme unsuspends it on GitHub." {
		t.Errorf("build = %+v, %v", b, err)
	}
	if fake.MintedFor(42) != 0 {
		t.Error("no token is asked for a suspended installation")
	}
}

func TestGitHubAppIsSkippedForAnInstallationMovedAway(t *testing.T) {
	app := gitApp("shop", "https://github.com/acme/shop")
	r, _, fake, st := githubReconciler(t, app)
	ctx := context.Background()
	if _, err := st.CreateWorkspace(ctx, store.Workspace{Slug: "beta", Name: "Beta"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddGitHubInstallation(ctx, "beta", store.GitHubInstallation{ID: 42, AccountLogin: "Acme", AccountType: "Organization", ConnectedBy: "bob"}); err != nil {
		t.Fatal(err)
	}
	if b, err := lookupGitBuild(ctx, r.Client, "shpyrd-system", r.Config.GitHub, app); err != nil || b.Kind != "" || b.AppRefused != "" || fake.MintedFor(42) != 0 {
		t.Errorf("the default workspace's app: %+v, %v", b, err)
	}
	theirs := gitApp("shop2", "https://github.com/acme/shop")
	theirs.Labels = map[string]string{shpyrdv1.LabelWorkspace: "beta"}
	if b, _ := lookupGitBuild(ctx, r.Client, "shpyrd-system", r.Config.GitHub, theirs); b.Kind != gitauth.KindGitHubApp {
		t.Errorf("beta's app: %+v", b)
	}
}

func TestGitHubAppIsNotTriedWhereItCannotApply(t *testing.T) {
	r, c, fake, _ := githubReconciler(t)
	ctx := context.Background()
	for _, url := range []string{"https://github.com/someone/shop", "https://gitlab.com/acme/shop", "https://github.com/acme/shop/extra"} {
		if b, err := lookupGitBuild(ctx, r.Client, "shpyrd-system", r.Config.GitHub, gitApp("x", url)); err != nil || b.Kind != "" || b.AppRefused != "" {
			t.Errorf("%s: %+v %v", url, b, err)
		}
	}
	if err := c.Delete(ctx, fake.Secret("shpyrd-system")); err != nil {
		t.Fatal(err)
	}
	fake.SetNow(sourceNow.Add(time.Minute)) // past the Source's 30 s
	if b, _ := lookupGitBuild(ctx, r.Client, "shpyrd-system", r.Config.GitHub, gitApp("shop", "https://github.com/acme/shop")); b.Kind != "" {
		t.Errorf("without the App's Secret: %+v", b)
	}
	if b, _ := lookupGitBuild(ctx, r.Client, "shpyrd-system", nil, gitApp("shop", "https://github.com/acme/shop")); b.Kind != "" {
		t.Errorf("without the App: %+v", b)
	}
	if calls := fake.Calls(); len(calls) != 0 {
		t.Errorf("GitHub was called: %v", calls)
	}
}

func TestGitHubTokenIsRewrittenFiftyMinutesAfterItWasMinted(t *testing.T) {
	app := gitApp("shop", "https://github.com/acme/shop")
	r, c, fake, _ := githubReconciler(t, app)
	ctx := context.Background()
	reconcileAt := func(at time.Time) *corev1.Secret {
		t.Helper()
		fake.SetNow(at)
		if _, err := r.reconcileGitCredentials(ctx, app); err != nil {
			t.Fatal(err)
		}
		return buildSecretOf(t, c, app)
	}
	if sec := reconcileAt(sourceNow); string(sec.Data[corev1.BasicAuthPasswordKey]) != "ghs_42_1" {
		t.Fatalf("first: %q", sec.Data[corev1.BasicAuthPasswordKey])
	}
	if d := r.githubTokenRefresh(ctx, app); d != githubTokenLife+time.Second {
		t.Errorf("requeue in %v", d)
	}
	if sec := reconcileAt(sourceNow.Add(49 * time.Minute)); string(sec.Data[corev1.BasicAuthPasswordKey]) != "ghs_42_1" {
		t.Errorf("49 minutes: %q", sec.Data[corev1.BasicAuthPasswordKey])
	}
	if d := r.githubTokenRefresh(ctx, app); d != time.Minute+time.Second {
		t.Errorf("requeue in %v", d)
	}
	sec := reconcileAt(sourceNow.Add(githubTokenLife))
	if string(sec.Data[corev1.BasicAuthPasswordKey]) != "ghs_42_2" || sec.Annotations[gitauth.AnnotationTokenMintedAt] != sourceNow.Add(githubTokenLife).Format(time.RFC3339) {
		t.Errorf("50 minutes: %q %v", sec.Data[corev1.BasicAuthPasswordKey], sec.Annotations)
	}
}

func TestAnAppCloningThroughGitHubIsRequeuedBeforeItsTokenAges(t *testing.T) {
	app := pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit)
	r, _, _, _ := githubReconciler(t, app)
	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(app)})
	if err != nil || res.RequeueAfter <= 0 || res.RequeueAfter > githubTokenLife+time.Second {
		t.Errorf("requeue after %v, %v", res.RequeueAfter, err)
	}
}

func TestATokenReplacingTheGitHubAppDropsItsMarks(t *testing.T) {
	app := gitApp("shop", "https://github.com/acme/shop")
	r, c, _, st := githubReconciler(t, app, workspaceToken("default", "github.com", "ghp_ours"))
	ctx := context.Background()
	if _, err := r.reconcileGitCredentials(ctx, app); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteGitHubInstallation(ctx, store.DefaultWorkspace, 42); err != nil {
		t.Fatal(err)
	}
	got, err := r.reconcileGitCredentials(ctx, app)
	if err != nil || got.Kind != gitauth.KindToken {
		t.Fatalf("build = %+v, %v", got, err)
	}
	sec := buildSecretOf(t, c, app)
	if string(sec.Data[corev1.BasicAuthPasswordKey]) != "ghp_ours" || sec.Annotations[gitauth.AnnotationGitHubAccount] != "" || sec.Annotations[gitauth.AnnotationTokenMintedAt] != "" {
		t.Errorf("secret %q %v", sec.Data[corev1.BasicAuthPasswordKey], sec.Annotations)
	}
	if d := r.githubTokenRefresh(ctx, app); d != 0 {
		t.Errorf("a token needs no refresh: %v", d)
	}
}

// githubWatcher is a SourceReconciler on a platform with the fake's App.
func githubWatcher(t *testing.T, ls *fakeLsRemote, objs ...client.Object) *SourceReconciler {
	t.Helper()
	fake, st := connectedGitHub(t)
	r, c := newSourceReconciler(t, ls, append([]client.Object{fake.Secret("shpyrd-system")}, objs...)...)
	r.GitHub = &GitHubApp{Source: fake.Source(c, "shpyrd-system"), Installations: st}
	return r
}

func TestSourceListsWithTheGitHubAppsToken(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": testCommit}}
	app := gitApp("shop", "https://github.com/acme/shop")
	_, got := reconcileSource(t, githubWatcher(t, ls, app), app)
	if ls.kind != gitauth.KindGitHubApp || ls.url != "https://github.com/acme/shop" || got.PinnedCommit() != testCommit {
		t.Errorf("listed %q with %q; pinned %q", ls.url, ls.kind, got.PinnedCommit())
	}
}

func TestSourceSaysWhyTheGitHubAppCouldNotReadTheRepository(t *testing.T) {
	ls := &fakeLsRemote{err: gitauth.ErrNoAccess}
	app := gitApp("vault", "https://github.com/acme/vault")
	_, got := reconcileSource(t, githubWatcher(t, ls, app), app)
	rec := sourceRecord(got)
	if !strings.HasPrefix(rec.Error, "The GitHub App's installation on Acme does not include vault: add the repository to it on GitHub. shpyrd could not read the repository") ||
		!strings.Contains(rec.Error, "shpyrd git-auth deploy-key") {
		t.Errorf("error %q", rec.Error)
	}
}
```

In `internal/controller/source_controller_test.go`, `TestLookupGitBuildIsWhatTheBuildUses` passes no App: both calls become `lookupGitBuild(context.Background(), c, "shpyrd-system", nil, …)`.

Append to `pkg/api/gitauth_test.go` (add `metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"` to its imports):

```go
func TestProjectGitAuthSaysTheGitHubApp(t *testing.T) {
	app := sampleApp("shop", shpyrdv1.PhaseRunning)
	app.Spec.Source = &shpyrdv1.Source{Git: &shpyrdv1.GitSource{URL: "https://github.com/acme/shop"}}
	build := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "shop-git", Namespace: "app-shop",
		Annotations: map[string]string{gitauth.AnnotationGitHubAccount: "Acme", gitauth.AnnotationTokenMintedAt: "2026-10-16T12:00:00Z"}}}
	s, _ := newTestServer(t, nil, []client.Object{app, build})
	rec := do(t, s, "GET", "/api/projects/shop/git-auth", "", true)
	var pv ProjectGitAuthView
	_ = json.Unmarshal(rec.Body.Bytes(), &pv)
	if rec.Code != http.StatusOK || pv.Using != gitauth.KindGitHubApp || pv.GitHubAccount != "Acme" {
		t.Errorf("%d %s", rec.Code, rec.Body.String())
	}
}
```

Append to `internal/cli/gitauth_test.go`:

```go
func TestGitAuthShowSaysTheGitHubApp(t *testing.T) {
	out, err := runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"url":"https://github.com/acme/shop","using":"github-app","githubAccount":"Acme"}`))
	}, "git-auth", "show", "--project", "shop")
	if err != nil || !strings.Contains(out, "shop clones https://github.com/acme/shop through the GitHub App (Acme)") {
		t.Errorf("%v\n%s", err, out)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/controller/ ./pkg/api/ ./internal/cli/ -run 'GitHub|LookupGitBuild|GitAuthShow'`
Expected: FAIL to compile: `undefined: GitHubApp`, `undefined: gitauth.KindGitHubApp`, `too many arguments in call to lookupGitBuild`, `unknown field GitHubAccount`.

- [ ] **Step 3: `pkg/gitauth/resolve.go`**

The kinds:

```go
// Credential kinds.
const (
	KindToken     = "token"
	KindDeployKey = "deploy-key"
	// KindGitHubApp is a token the GitHub App minted for the one
	// repository (RFC-0054).
	KindGitHubApp = "github-app"
)
```

In the annotations `const` block, after `AnnotationUpdatedBy`:

```go
	// AnnotationGitHubAccount is, on <app>-git, the account whose GitHub
	// App installation minted its token, and AnnotationTokenMintedAt when
	// (RFC3339). Only a GitHub App credential carries them (RFC-0054).
	AnnotationGitHubAccount = "shpyrd.io/github-account"
	AnnotationTokenMintedAt = "shpyrd.io/github-token-minted-at"
```

In `Build`, the `Kind` comment becomes `// Kind is KindGitHubApp, KindDeployKey, KindToken, or "" when the clone goes without credentials (a public repository).`, and after `Secret`:

```go
	// AppRefused says why the GitHub App could not give access, when it
	// was tried and the build fell through to another credential.
	AppRefused string
```

- [ ] **Step 4: `internal/controller/github.go`**

```go
package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
	"github.com/shpyrd-io/shpyrd/pkg/github"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// The GitHub App (RFC-0054): a build of a github.com repository clones
// with a token the App mints for that one repository, when the App's
// workspace has an installation on the repository's owner. Anything that
// stops it falls through to the deploy key, then the token.

// githubTokenLife is how old the App's token in <app>-git may get before
// it is written again: tokens last an hour, and kpack, which reads the
// Secret whenever one of its rebuilds starts, never gets one in its last
// ten minutes.
const githubTokenLife = 50 * time.Minute

// GitHubApp is the App's credentials and the workspaces' installations.
type GitHubApp struct {
	Source        *github.Source
	Installations store.GitHub
}

// githubMarks are the annotations only a GitHub App credential carries:
// another credential replacing it must not keep them.
var githubMarks = []string{gitauth.AnnotationGitHubAccount, gitauth.AnnotationTokenMintedAt}

// build is the App's credential for the App's repository; nil when the
// App does not apply. refused says why it did not when it was tried, for
// the watcher's message. A failure is logged, never returned: the other
// credentials are tried.
func (gh *GitHubApp) build(ctx context.Context, app *shpyrdv1.App, repo gitauth.Repo) (*gitauth.Build, string) {
	if gh == nil || gh.Source == nil || gh.Installations == nil || repo.Host != "github.com" {
		return nil, ""
	}
	owner, name, ok := strings.Cut(repo.Path, "/")
	if !ok || strings.Contains(name, "/") {
		return nil, ""
	}
	logger := log.FromContext(ctx)
	cl, err := gh.Source.Client(ctx)
	if err != nil {
		// A bare ErrNotConfigured is no Secret at all, the usual case on a
		// platform without the App; a broken Secret or a failed read is
		// worth a line.
		if err != github.ErrNotConfigured { //nolint:errorlint // identity, not a wrapped error, is the point
			logger.Info("the GitHub App cannot be used; building with the other credentials", "err", err.Error())
		}
		return nil, ""
	}
	list, err := gh.Installations.GitHubInstallations(ctx, workspaceOf(app))
	if err != nil {
		logger.Info("could not read the workspace's GitHub connections; building with the other credentials", "err", err.Error())
		return nil, ""
	}
	var inst *store.GitHubInstallation
	for i := range list {
		if strings.EqualFold(list[i].AccountLogin, owner) {
			inst = &list[i]
		}
	}
	switch {
	case inst == nil:
		return nil, ""
	case inst.SuspendedAt != nil:
		return nil, fmt.Sprintf("The GitHub App's installation on %s is suspended: an owner of %s unsuspends it on GitHub.", inst.AccountLogin, inst.AccountLogin)
	}
	tok, err := cl.InstallationToken(ctx, inst.ID, name)
	if errors.Is(err, github.ErrRepoNotGranted) {
		return nil, fmt.Sprintf("The GitHub App's installation on %s does not include %s: add the repository to it on GitHub.", inst.AccountLogin, name)
	}
	if err != nil {
		logger.Info("GitHub gave no token; building with the other credentials", "installation", inst.ID, "err", err.Error())
		return nil, "GitHub did not give shpyrd a token for this repository just now."
	}
	sec := &corev1.Secret{Data: map[string][]byte{gitauth.KeyToken: []byte(tok.Value), gitauth.KeyUsername: []byte("x-access-token")}}
	b, err := gitauth.Resolve(app.Spec.Source.Git.URL, app.Name, app.Namespace, nil, sec)
	if err != nil {
		return nil, ""
	}
	b.Kind = gitauth.KindGitHubApp
	b.Secret.Annotations[gitauth.AnnotationGitHubAccount] = inst.AccountLogin
	b.Secret.Annotations[gitauth.AnnotationTokenMintedAt] = tok.MintedAt.UTC().Format(time.RFC3339)
	return &b, ""
}

// githubTokenRefresh is how long until <app>-git must be written again:
// 50 minutes after its token was minted, and a second. 0 when the App's
// builds do not clone through the GitHub App.
func (r *AppReconciler) githubTokenRefresh(ctx context.Context, app *shpyrdv1.App) time.Duration {
	sec := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: gitauth.BuildSecretName(app.Name)}, sec); err != nil {
		return 0
	}
	minted, err := time.Parse(time.RFC3339, sec.Annotations[gitauth.AnnotationTokenMintedAt])
	if err != nil {
		return 0
	}
	return max(minted.Add(githubTokenLife).Sub(r.now())+time.Second, time.Second)
}

// appRefusedError is a listing that failed after the GitHub App could not
// give access: the watcher says the App's reason first.
type appRefusedError struct {
	why string
	err error
}

func (e *appRefusedError) Error() string { return e.err.Error() }
func (e *appRefusedError) Unwrap() error { return e.err }
```

- [ ] **Step 5: Use it in `gitcreds.go`, the watcher and the AppReconciler**

`internal/controller/gitcreds.go`:
- in `reconcileGitCredentials`, `b, err := lookupGitBuild(ctx, r.Client, r.Config.SystemNamespace, r.Config.GitHub, app)`;
- in its `CreateOrUpdate` mutate func, before `sec.Annotations = mergeMaps(sec.Annotations, want.Annotations)`:

```go
		for _, k := range githubMarks {
			if _, ok := want.Annotations[k]; !ok {
				delete(sec.Annotations, k)
			}
		}
```

- `lookupGitBuild` becomes:

```go
// lookupGitBuild finds the credential a build of the App's repository
// clones with, and the source watcher lists with: the GitHub App's token
// for the repository (RFC-0054), else the project's deploy key for the
// host, else the workspace's token for the host, else none.
func lookupGitBuild(ctx context.Context, c client.Reader, systemNS string, gh *GitHubApp, app *shpyrdv1.App) (gitauth.Build, error) {
	url := app.Spec.Source.Git.URL
	repo, err := gitauth.Parse(url)
	if err != nil {
		return gitauth.Build{}, errGitURL
	}
	viaApp, refused := gh.build(ctx, app, repo)
	if viaApp != nil {
		return *viaApp, nil
	}
	deployKey, err := optionalSecret(ctx, c, app.Namespace, gitauth.DeployKeySecretName)
	if err != nil {
		return gitauth.Build{}, err
	}
	token, err := optionalSecret(ctx, c, systemNS, gitauth.TokenSecretName(workspaceOf(app), repo.Host))
	if err != nil {
		return gitauth.Build{}, err
	}
	b, err := gitauth.Resolve(url, app.Name, app.Namespace, deployKey, token)
	if err != nil {
		return gitauth.Build{}, errGitURL
	}
	b.AppRefused = refused
	return b, nil
}
```

`internal/controller/desired.go`, in `Config` after `Projects store.Projects`:

```go
	// GitHub is the GitHub App (RFC-0054): builds of github.com
	// repositories clone with its tokens first. Nil: never.
	GitHub *GitHubApp
```

`internal/controller/source_controller.go`:
- in `SourceReconciler`, after `SystemNamespace string`:

```go
	// GitHub is the GitHub App (RFC-0054): listings use its token as
	// builds do. Nil: never.
	GitHub *GitHubApp
```

- in `poll`, `b, err := lookupGitBuild(ctx, r.Client, r.SystemNamespace, r.GitHub, app)`, and the listing's error check becomes:

```go
	if err != nil && !errors.Is(err, gitauth.ErrTooManyRefs) {
		if b.AppRefused != "" {
			return "", &appRefusedError{why: b.AppRefused, err: err}
		}
		return "", err
	}
```

- in `describeSourceError`, the `ErrNoAccess` case becomes:

```go
	case errors.Is(err, gitauth.ErrNoAccess):
		msg := fmt.Sprintf("shpyrd could not read the repository: it is private, or does not exist under that name. Give shpyrd access: `shpyrd git-auth deploy-key` prints a key to add to the repository as a read-only deploy key, or a workspace admin sets a token for the host with `shpyrd git-auth set --host %s --token -`. shpyrd tries again by itself.", host)
		var refused *appRefusedError
		if errors.As(err, &refused) {
			msg = refused.why + " " + msg
		}
		return msg
```

`internal/controller/app_controller.go`, in `Reconcile`, after the `if out.domainsPending && …` block:

```go
	// A build cloning through the GitHub App gets a new token before the
	// one in <app>-git is 50 minutes old (RFC-0054).
	if d := r.githubTokenRefresh(ctx, app); d > 0 && (out.result.RequeueAfter == 0 || d < out.result.RequeueAfter) {
		out.result.RequeueAfter = d
	}
```

- [ ] **Step 6: Give the controllers the App, `pkg/server/server.go`**

In `newManager`, before `rec := &controller.AppReconciler{`:

```go
	// The GitHub App (RFC-0054): its Secret through the manager's cache,
	// and the workspaces' installations.
	githubApp := &controller.GitHubApp{
		Source:        &github.Source{Reader: mgr.GetClient(), Namespace: k.Namespace, Options: github.Options{APIURL: os.Getenv(github.EnvAPIURL)}},
		Installations: memberships.Store,
	}
```

In the AppReconciler's `Config`, after `Projects: memberships.Store,`: `GitHub: githubApp,`. In `&controller.SourceReconciler{…}`, after `SystemNamespace: k.Namespace,`: `GitHub: githubApp,`. Import `"github.com/shpyrd-io/shpyrd/pkg/github"`.

- [ ] **Step 7: The views**

`pkg/api/gitauth.go`, `ProjectGitAuthView` gains:

```go
	// GitHubAccount is the account whose GitHub App installation builds
	// clone through, when Using is github-app (RFC-0054).
	GitHubAccount string `json:"githubAccount,omitempty"`
```

In `showProjectGitAuth`, before the `switch` that sets `view.Using`:

```go
	// What builds clone with: the controller marks <app>-git when it
	// holds the GitHub App's token.
	viaApp := ""
	build := &corev1.Secret{}
	if err := s.apps.Get(c.Request.Context(), types.NamespacedName{Namespace: app.Namespace, Name: gitauth.BuildSecretName(app.Name)}, build); err == nil {
		viaApp = build.Annotations[gitauth.AnnotationGitHubAccount]
	}
```

and the `switch` gains a first case:

```go
	case viaApp != "":
		view.Using, view.GitHubAccount = gitauth.KindGitHubApp, viaApp
```

`internal/cli/gitauth.go`, in `show`'s printing `switch`, a first case:

```go
				case v.Using == gitauth.KindGitHubApp:
					fmt.Fprintf(w, "%s clones %s through the GitHub App (%s): a token for this repository alone, renewed before the hour is out.\n", name, v.URL, v.GitHubAccount)
```

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go build ./... && go test ./pkg/gitauth/ ./internal/controller/ ./pkg/api/ ./internal/cli/ ./pkg/server/ -race`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add pkg/gitauth/resolve.go internal/controller pkg/server/server.go pkg/api/gitauth.go pkg/api/gitauth_test.go internal/cli/gitauth.go internal/cli/gitauth_test.go
git commit -m "feat(controller): builds and the watcher clone through the GitHub App first, tokens renewed at 50 minutes (RFC-0054)

Co-Authored-By: <your model name> <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 7: The CLI: `shpyrd github connect|list|disconnect|repos`; `git-hooks show` for App projects

**Files:**
- Create: `internal/cli/github.go`
- Modify: `internal/cli/root.go` (after `root.AddCommand(newGitHooksCmd(g))`, ~132)
- Modify: `internal/cli/githooks.go` (`show`'s RunE and `printGitHooks`), `internal/cli/githooks_test.go` (`TestGitHooksShowPrintsTheWebhookAndHowToAddIt`'s handler)
- Test: `internal/cli/github_test.go`

**Interfaces:**
- Consumes: `api.GitHubView`, `api.GitHubInstallationView`, `api.GitHubConnectView`, `api.GitHubRepositoryView` (Task 4); `api.ProjectGitAuthView.GitHubAccount`, `gitauth.KindGitHubApp` (Task 6); `browse` (Task 3); `signedInRequest` (`gitauth.go:197`), `runShpyrd` (`githooks_test.go:12`), `signalContext`, `firstNonEmpty`.
- Produces:
  - `func newGitHubCmd(g *globalFlags) *cobra.Command`
  - `var githubPollEvery = 3 * time.Second`, `var githubConnectWait = 10 * time.Minute`
  - `func githubRequest(g *globalFlags, cmd *cobra.Command, method, path string, out any) error`

- [ ] **Step 1: Write the failing tests**

`internal/cli/github_test.go`:

```go
package cli

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const acmeConnection = `{"id":42,"account":"Acme","type":"Organization","connectedBy":"ana@acme.test","createdAt":"2026-10-16T12:00:00Z","suspended":false,"manageURL":"https://github.com/organizations/Acme/settings/installations/42"}`

func fastGitHubPolls(t *testing.T) *[]string {
	t.Helper()
	opened := []string{}
	prevEvery, prevWait, prevBrowse := githubPollEvery, githubConnectWait, browse
	githubPollEvery, githubConnectWait = 5*time.Millisecond, time.Second
	browse = func(u string) error { opened = append(opened, u); return nil }
	t.Cleanup(func() { githubPollEvery, githubConnectWait, browse = prevEvery, prevWait, prevBrowse })
	return &opened
}

func TestGitHubConnectOpensTheInstallAndWaitsForTheNewConnection(t *testing.T) {
	opened := fastGitHubPolls(t)
	var reads atomic.Int32
	out, err := runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/workspace/github":
			list := `[{"id":7,"account":"old","type":"User","connectedBy":"x","createdAt":"2026-10-01T00:00:00Z","suspended":false,"manageURL":"u"}]`
			if reads.Add(1) > 3 {
				list = `[{"id":7,"account":"old","type":"User","connectedBy":"x","createdAt":"2026-10-01T00:00:00Z","suspended":false,"manageURL":"u"},` + acmeConnection + `]`
			}
			_, _ = w.Write([]byte(`{"configured":true,"appURL":"https://github.com/apps/shpyrd-test","installations":` + list + `}`))
		case "POST /api/workspace/github/connect":
			_, _ = w.Write([]byte(`{"installURL":"https://github.com/apps/shpyrd-test/installations/new?state=s"}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}, "github", "connect")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if len(*opened) != 1 || (*opened)[0] != "https://github.com/apps/shpyrd-test/installations/new?state=s" {
		t.Errorf("opened %v", *opened)
	}
	if !strings.Contains(out, "Connected Acme (organisation), installation 42") || strings.Contains(out, "Connected old") {
		t.Errorf("output:\n%s", out)
	}
}

func TestGitHubConnectSaysWhenGitHubIsNotSetUp(t *testing.T) {
	fastGitHubPolls(t)
	_, err := runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"configured":false,"note":"GitHub is not set up on this platform. An operator sets it up with ` + "`shpyrd-ctl github app create`" + `.","installations":[]}`))
	}, "github", "connect")
	if err == nil || !strings.Contains(err.Error(), "shpyrd-ctl github app create") {
		t.Errorf("err = %v", err)
	}
}

func TestGitHubConnectGivesUpAfterTheWait(t *testing.T) {
	fastGitHubPolls(t)
	githubConnectWait = 30 * time.Millisecond
	_, err := runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			_, _ = w.Write([]byte(`{"installURL":"https://github.com/apps/shpyrd-test/installations/new?state=s"}`))
			return
		}
		_, _ = w.Write([]byte(`{"configured":true,"installations":[]}`))
	}, "github", "connect")
	if err == nil || !strings.Contains(err.Error(), "run shpyrd github connect again") {
		t.Errorf("err = %v", err)
	}
}

func TestGitHubListAndDisconnectByAccount(t *testing.T) {
	var deleted atomic.Value // the handler runs on the test server's goroutines
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			_, _ = w.Write([]byte(`{"configured":true,"installations":[` + acmeConnection + `]}`))
		case "DELETE":
			deleted.Store(r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		}
	}
	out, err := runShpyrd(t, handler, "github", "list")
	if err != nil || !strings.Contains(out, "Acme") || !strings.Contains(out, "organisation") || !strings.Contains(out, "ana@acme.test") || !strings.Contains(out, "42") {
		t.Fatalf("list: %v\n%s", err, out)
	}
	out, err = runShpyrd(t, handler, "github", "disconnect", "acme")
	if err != nil || deleted.Load() != "/api/workspace/github/installations/42" || !strings.Contains(out, "Disconnected Acme") || !strings.Contains(out, "settings/installations/42") {
		t.Fatalf("disconnect: %v %v\n%s", err, deleted.Load(), out)
	}
	if _, err := runShpyrd(t, handler, "github", "disconnect", "nobody"); err == nil || !strings.Contains(err.Error(), "shpyrd github list") {
		t.Errorf("unknown: %v", err)
	}
}

func TestGitHubReposFilters(t *testing.T) {
	out, err := runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/workspace/github/repositories" {
			t.Errorf("unexpected %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"installation":42,"fullName":"Acme/shop","private":true,"defaultBranch":"main"},{"installation":42,"fullName":"Acme/site","private":false,"defaultBranch":"trunk"}]`))
	}, "github", "repos", "--filter", "SHOP")
	if err != nil || !strings.Contains(out, "Acme/shop") || strings.Contains(out, "Acme/site") || !strings.Contains(out, "private") {
		t.Errorf("%v\n%s", err, out)
	}
}

func TestGitHooksShowSaysTheGitHubAppNeedsNothing(t *testing.T) {
	out, err := runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/projects/shop/git-auth":
			_, _ = w.Write([]byte(`{"url":"https://github.com/acme/shop","using":"github-app","githubAccount":"Acme"}`))
		case "/api/projects/shop":
			_, _ = w.Write([]byte(`{"slug":"shop","spec":{"source":{"git":{"url":"https://github.com/acme/shop"}}},"status":{"phase":"Running","releases":[]}}`))
		default:
			t.Errorf("unexpected %s %s: an App project needs no webhook of its own", r.Method, r.URL.Path)
		}
	}, "git-hooks", "show", "--project", "shop")
	if err != nil || !strings.Contains(out, "Pushes arrive through the GitHub App (Acme); nothing to set up.") || strings.Contains(out, "Payload URL") {
		t.Errorf("%v\n%s", err, out)
	}
}
```

In `internal/cli/githooks_test.go`, `TestGitHooksShowPrintsTheWebhookAndHowToAddIt`'s handler gets a case:

```go
		case "GET /api/projects/shop/git-auth":
			_, _ = w.Write([]byte(`{"url":"https://github.com/acme/shop","using":"token"}`))
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/ -run 'GitHub|GitHooks'`
Expected: FAIL to compile: `undefined: githubPollEvery`, and `unknown command "github"`.

- [ ] **Step 3: Write `internal/cli/github.go`**

```go
package cli

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/shpyrd-io/shpyrd/pkg/api"
)

// Connect GitHub (RFC-0054): `shpyrd github`. A workspace admin connects
// an account or organisation where the cluster's GitHub App is installed;
// then whoever makes projects lists its repositories. Signed in only.

var (
	// githubPollEvery and githubConnectWait pace `shpyrd github connect`.
	githubPollEvery   = 3 * time.Second
	githubConnectWait = 10 * time.Minute
)

func githubRequest(g *globalFlags, cmd *cobra.Command, method, path string, out any) error {
	return signedInRequest(g, cmd, "GitHub is connected signed in: run shpyrd login first", method, path, nil, out)
}

// kindOf says GitHub's account type in words.
func kindOf(accountType string) string {
	if accountType == "Organization" {
		return "organisation"
	}
	return "personal account"
}

func newGitHubCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "github",
		Short: "Connect GitHub: pick repositories, private ones build, every push deploys",
		Long: `Connect an account or organisation once (workspace admins). Then anyone who
makes projects picks one of its repositories: private ones build without keys
or tokens, and every push to a project's branch deploys.

  shpyrd github connect               install the App on GitHub and connect it here
  shpyrd github list                  the workspace's connections
  shpyrd github disconnect <account>  forget one (the App stays installed on GitHub)
  shpyrd github repos [--filter TEXT] the repositories the connections reach

Then: shpyrd deploy --git https://github.com/<owner>/<repo> --ref <branch>`,
	}
	var noBrowser bool
	connect := &cobra.Command{
		Use:   "connect",
		Short: "Install the GitHub App on an account or organisation, and connect it",
		RunE: func(cmd *cobra.Command, args []string) error {
			var before api.GitHubView
			if err := githubRequest(g, cmd, "GET", "api/workspace/github", &before); err != nil {
				return err
			}
			if !before.Configured {
				return errors.New(firstNonEmpty(before.Note, "GitHub is not set up on this platform."))
			}
			var start api.GitHubConnectView
			if err := githubRequest(g, cmd, "POST", "api/workspace/github/connect", &start); err != nil {
				return err
			}
			out := g.progress(cmd)
			if !noBrowser && browse(start.InstallURL) == nil {
				fmt.Fprintf(out, "Choose where to install the App in the browser (if nothing opened: %s)\n", start.InstallURL)
			} else {
				fmt.Fprintf(out, "Open %s and choose where to install the App.\n", start.InstallURL)
			}
			known := map[int64]bool{}
			for _, i := range before.Installations {
				known[i.ID] = true
			}
			ctx := signalContext()
			deadline := time.Now().Add(githubConnectWait)
			fmt.Fprint(out, "Waiting for GitHub... ")
			for {
				select {
				case <-ctx.Done():
					fmt.Fprintln(out)
					return errors.New("cancelled; an install finished on GitHub still connects when the browser comes back")
				case <-time.After(githubPollEvery):
				}
				var now api.GitHubView
				if err := githubRequest(g, cmd, "GET", "api/workspace/github", &now); err != nil {
					return err
				}
				for _, inst := range now.Installations {
					if known[inst.ID] {
						continue
					}
					fmt.Fprintln(out)
					return g.print(cmd, inst, func(w io.Writer) {
						fmt.Fprintf(w, "Connected %s (%s), installation %d. Its repositories: shpyrd github repos\n", inst.Account, kindOf(inst.Type), inst.ID)
					})
				}
				if time.Now().After(deadline) {
					fmt.Fprintln(out)
					return errors.New("no new connection after 10 minutes: run shpyrd github connect again")
				}
			}
		},
	}
	connect.Flags().BoolVar(&noBrowser, "no-browser", false, "print the install link instead of opening it")

	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the workspace's GitHub connections",
		RunE: func(cmd *cobra.Command, args []string) error {
			var v api.GitHubView
			if err := githubRequest(g, cmd, "GET", "api/workspace/github", &v); err != nil {
				return err
			}
			return g.print(cmd, v, func(w io.Writer) {
				switch {
				case !v.Configured:
					fmt.Fprintln(w, v.Note)
				case len(v.Installations) == 0:
					fmt.Fprintln(w, "No GitHub connections. A workspace admin connects one: shpyrd github connect")
				default:
					tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
					fmt.Fprintln(tw, "ACCOUNT\tKIND\tID\tCONNECTED BY\tSTATE")
					for _, i := range v.Installations {
						state := "ok"
						if i.Suspended {
							state = "suspended on GitHub"
						}
						fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\n", i.Account, kindOf(i.Type), i.ID, i.ConnectedBy, state)
					}
					_ = tw.Flush()
				}
			})
		},
	}

	disconnect := &cobra.Command{
		Use:   "disconnect <account|id>",
		Short: "Forget a connection; the App stays installed on GitHub",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var v api.GitHubView
			if err := githubRequest(g, cmd, "GET", "api/workspace/github", &v); err != nil {
				return err
			}
			var found *api.GitHubInstallationView
			for i := range v.Installations {
				inst := &v.Installations[i]
				if strings.EqualFold(inst.Account, args[0]) || strconv.FormatInt(inst.ID, 10) == args[0] {
					found = inst
				}
			}
			if found == nil {
				return fmt.Errorf("this workspace has no GitHub connection named %s: shpyrd github list", args[0])
			}
			if err := githubRequest(g, cmd, "DELETE", "api/workspace/github/installations/"+url.PathEscape(strconv.FormatInt(found.ID, 10)), nil); err != nil {
				return err
			}
			return g.print(cmd, map[string]any{"disconnected": found.ID}, func(w io.Writer) {
				fmt.Fprintf(w, "Disconnected %s: its repositories no longer build through the App and its pushes no longer deploy. The App stays installed on GitHub; remove it at %s\n", found.Account, found.ManageURL)
			})
		},
	}

	var filter string
	repos := &cobra.Command{
		Use:   "repos [--filter TEXT]",
		Short: "List the repositories the workspace's connections reach",
		RunE: func(cmd *cobra.Command, args []string) error {
			var all []api.GitHubRepositoryView
			if err := githubRequest(g, cmd, "GET", "api/workspace/github/repositories", &all); err != nil {
				return err
			}
			shown := []api.GitHubRepositoryView{}
			for _, r := range all {
				if strings.Contains(strings.ToLower(r.FullName), strings.ToLower(filter)) {
					shown = append(shown, r)
				}
			}
			return g.print(cmd, map[string]any{"repositories": shown}, func(w io.Writer) {
				if len(shown) == 0 {
					fmt.Fprintln(w, "No repository matches. A connection reaches what its installation includes: change it on GitHub.")
					return
				}
				tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
				fmt.Fprintln(tw, "REPOSITORY\tVISIBILITY\tDEFAULT BRANCH")
				for _, r := range shown {
					visibility := "public"
					if r.Private {
						visibility = "private"
					}
					fmt.Fprintf(tw, "%s\t%s\t%s\n", r.FullName, visibility, r.DefaultBranch)
				}
				_ = tw.Flush()
				fmt.Fprintln(w, "\nDeploy one: shpyrd deploy --git https://github.com/<repository> --ref <branch> --project <name>")
			})
		},
	}
	repos.Flags().StringVar(&filter, "filter", "", "only repositories whose name contains this")

	cmd.AddCommand(connect, list, disconnect, repos)
	return cmd
}
```

In `internal/cli/root.go`, after `root.AddCommand(newGitHooksCmd(g))`:

```go
	root.AddCommand(newGitHubCmd(g)) // RFC-0054
```

- [ ] **Step 4: `git-hooks show` for a project on the App**

In `internal/cli/githooks.go`, `show`'s `RunE` becomes:

```go
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := resolveAppName(showApp)
			if err != nil {
				return err
			}
			var auth api.ProjectGitAuthView
			if err := gitHooksRequest(g, cmd, "GET", "api/projects/"+url.PathEscape(name)+"/git-auth", &auth); err != nil {
				return err
			}
			var d api.AppDetail
			if err := gitHooksRequest(g, cmd, "GET", "api/projects/"+url.PathEscape(name), &d); err != nil {
				return err
			}
			if auth.Using == gitauth.KindGitHubApp {
				// The App's own webhook brings the pushes (RFC-0054): no
				// hook Secret is made for this project.
				return g.print(cmd, map[string]string{"through": "github-app", "account": auth.GitHubAccount}, func(w io.Writer) {
					fmt.Fprintf(w, "Pushes arrive through the GitHub App (%s); nothing to set up.\n", auth.GitHubAccount)
					printAutoDeploy(w, name, d)
				})
			}
			var v api.GitHooksView
			if err := gitHooksRequest(g, cmd, "GET", "api/projects/"+url.PathEscape(name)+"/git-hooks", &v); err != nil {
				return err
			}
			return g.print(cmd, v, func(w io.Writer) { printGitHooks(w, name, v, d) })
		},
```

and `printGitHooks` ends by calling a new `printAutoDeploy`, which takes over its `var git *shpyrdv1.GitSource … switch {…}` tail unchanged:

```go
// printGitHooks says where the webhook goes on each host, and what a push
// does now.
func printGitHooks(w io.Writer, name string, v api.GitHooksView, d api.AppDetail) {
	fmt.Fprintf(w, "Deploy %s on push: add this webhook to its repository.\n\n  URL     %s\n  Secret  %s\n\n", name, v.URL, v.Secret)
	fmt.Fprintln(w, `GitHub: the repository's Settings, Webhooks, Add webhook. Payload URL: the URL; Content type: application/json; Secret: the secret; "Just the push event".`)
	fmt.Fprintln(w, "GitLab: the project's Settings, Webhooks, Add new webhook. URL: the URL; Secret token: the secret; Trigger: Push events.")
	fmt.Fprintln(w, "Gitea: the repository's Settings, Webhooks, Add webhook, Gitea. Target URL: the URL; Content type: application/json; Secret: the secret; Trigger on: Push events.")
	fmt.Fprintln(w)
	printAutoDeploy(w, name, d)
}

// printAutoDeploy says what a push to the project's branch does now.
func printAutoDeploy(w io.Writer, name string, d api.AppDetail) {
	var git *shpyrdv1.GitSource
	if d.Spec.Source != nil {
		git = d.Spec.Source.Git
	}
	switch {
	case git == nil:
		fmt.Fprintf(w, "%s does not deploy from a git repository yet: shpyrd deploy --git <url> --project %s\n", name, name)
	case shpyrdv1.IsCommit(git.Revision):
		fmt.Fprintf(w, "%s is pinned to commit %s: a push deploys nothing. Deploy a branch to follow it.\n", name, git.Revision)
	case git.AutoDeploy != nil && !*git.AutoDeploy:
		branch := firstNonEmpty(git.Revision, "main")
		fmt.Fprintf(w, "Auto-deploy is off: pushes to %s are recorded, not deployed. Switch it on in the dashboard, or: shpyrd deploy --git %s --ref %s --auto-deploy --project %s\n", branch, git.URL, branch, name)
	default:
		fmt.Fprintf(w, "Auto-deploy is on: every push to %s deploys. Without the webhook, shpyrd checks the branch every few minutes.\n", firstNonEmpty(git.Revision, "main"))
	}
}
```

Add `"github.com/shpyrd-io/shpyrd/pkg/gitauth"` to `githooks.go`'s imports.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/cli/ -race`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/github.go internal/cli/github_test.go internal/cli/root.go internal/cli/githooks.go internal/cli/githooks_test.go
git commit -m "feat(cli): shpyrd github connect, list, disconnect, repos; git-hooks show for App projects (RFC-0054)

Co-Authored-By: <your model name> <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 8: The dashboard: Workspace settings → Git, From GitHub in New project, the Source block

**Files:**
- Modify: `apps/workspace/src/api/types.ts` (after `GitHooks`, ~270), `api.ts` (the `Api` type after `projectGitHooks` ~155, and its imports), `backend.ts` (after `projectGitHooks` ~280), `mock.ts` (`Things` ~68-81, the methods after `projectGitHooks` ~702), `mock/things.json`
- Create: `apps/workspace/src/lib/go.ts`
- Create: `apps/workspace/src/screens/workspace/git.tsx`, `git.test.tsx`; modify `src/screens/workspace/index.tsx`
- Create: `apps/workspace/src/screens/github-picker.tsx`, `github-picker.test.tsx`; modify `src/screens/launcher.tsx` (`NewProject`, 132-233)
- Modify: `apps/workspace/src/screens/project/source.tsx` (the `DeployOnPush` line ~92), `source.test.tsx`
- Modify: `apps/workspace/src/api/api.test.ts`

**Interfaces:**
- Consumes: `GET/POST/DELETE /api/workspace/github…` (Task 4), `GET /api/projects/:slug/git-auth` with `using: "github-app"` and `githubAccount` (Task 6).
- Produces (TypeScript):
  - `GitHubInstallation = { id: number; account: string; type: "Organization" | "User"; connectedBy: string; createdAt: string; suspended: boolean; manageURL: string }`
  - `GitHubConnections = { configured: boolean; appURL?: string; note?: string; installations: GitHubInstallation[] }`
  - `GitHubRepository = { installation: number; fullName: string; private: boolean; defaultBranch: string }`
  - `ProjectGitAuth = { url?: string; using: "none" | "token" | "deploy-key" | "github-app"; githubAccount?: string }`
  - `Api.github()`, `Api.connectGitHub()`, `Api.disconnectGitHub(id)`, `Api.githubRepositories()`, `Api.githubBranches(fullName)`, `Api.projectGitAuth(slug)`
  - `go(url: string): void` (`@/lib/go`)
  - `Git` (`screens/workspace/git.tsx`); `GitHubPicker`, `Picked`, `slugFromRepo`, `branchesInOrder` (`screens/github-picker.tsx`)

- [ ] **Step 1: Write the failing tests**

`apps/workspace/src/screens/workspace/git.test.tsx`:

```tsx
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { GitHubConnections } from "@/api/types";
import { Git } from "./git";

const fake = vi.hoisted(() => ({ github: vi.fn(), connectGitHub: vi.fn(), disconnectGitHub: vi.fn() }));
vi.mock("@/api/api", () => ({ api: fake }));
const went = vi.hoisted(() => ({ go: vi.fn() }));
vi.mock("@/lib/go", () => went);
const perms = vi.hoisted(() => ({ admin: true }));
vi.mock("@/lib/perms", () => ({ usePerms: () => ({ loaded: true, enforced: true, admin: perms.admin }) }));
const toasts = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));
vi.mock("sonner", () => ({ toast: toasts }));

const connected: GitHubConnections = {
  configured: true,
  appURL: "https://github.com/apps/shpyrd-acme",
  installations: [
    { id: 42, account: "acme", type: "Organization", connectedBy: "ana@acme.test", createdAt: new Date().toISOString(), suspended: false, manageURL: "https://github.com/organizations/acme/settings/installations/42" },
    { id: 7, account: "ana", type: "User", connectedBy: "ana@acme.test", createdAt: new Date().toISOString(), suspended: true, manageURL: "https://github.com/settings/installations/7" },
  ],
};

let queries: QueryClient;
function show() {
  queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queries}>
      <Git />
    </QueryClientProvider>,
  );
}
beforeEach(() => {
  perms.admin = true;
  fake.github.mockResolvedValue(connected);
});
afterEach(() => {
  cleanup();
  queries?.clear();
  vi.resetAllMocks();
});

it("lists each connection with its kind, who connected it, whether it is suspended, and a way to manage it on GitHub", async () => {
  show();
  expect(await screen.findByText("acme")).toBeTruthy();
  expect(screen.getByText("Organisation")).toBeTruthy();
  expect(screen.getByText("Personal account")).toBeTruthy();
  expect(screen.getByText("Suspended")).toBeTruthy();
  expect(screen.getAllByText(/ana@acme\.test/)).toHaveLength(2);
  const manage = screen.getAllByRole("link", { name: "Manage on GitHub" });
  expect(manage[0].getAttribute("href")).toBe("https://github.com/organizations/acme/settings/installations/42");
});

it("sends an admin's browser to GitHub's install page from Connect GitHub", async () => {
  fake.connectGitHub.mockResolvedValue({ installURL: "https://github.com/apps/shpyrd-acme/installations/new?state=s" });
  show();
  fireEvent.click(await screen.findByRole("button", { name: "Connect GitHub" }));
  await waitFor(() => expect(went.go).toHaveBeenCalledWith("https://github.com/apps/shpyrd-acme/installations/new?state=s"));
});

it("asks before disconnecting, then disconnects", async () => {
  fake.disconnectGitHub.mockResolvedValue(undefined);
  show();
  fireEvent.click(await screen.findByRole("button", { name: "Disconnect acme" }));
  expect(fake.disconnectGitHub).not.toHaveBeenCalled();
  fireEvent.click(await screen.findByRole("button", { name: "Disconnect" }));
  await waitFor(() => expect(fake.disconnectGitHub).toHaveBeenCalledWith(42));
  await waitFor(() => expect(toasts.success).toHaveBeenCalled());
});

it("says GitHub is not set up, in the server's words, and offers nothing to click", async () => {
  fake.github.mockResolvedValue({ configured: false, note: "GitHub is not set up on this platform. An operator sets it up with `shpyrd-ctl github app create`.", installations: [] });
  show();
  expect(await screen.findByText(/An operator sets it up with/)).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Connect GitHub" })).toBeNull();
});

it("shows someone who is not an admin the connections without Connect or Disconnect", async () => {
  perms.admin = false;
  show();
  expect(await screen.findByText("acme")).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Connect GitHub" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Disconnect acme" })).toBeNull();
});
```

`apps/workspace/src/screens/github-picker.test.tsx`:

```tsx
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState } from "react";
import type { GitHubRepository } from "@/api/types";
import { branchesInOrder, GitHubPicker, type Picked, slugFromRepo } from "./github-picker";

const fake = vi.hoisted(() => ({ githubBranches: vi.fn() }));
vi.mock("@/api/api", () => ({ api: fake }));

const repos: GitHubRepository[] = [
  { installation: 42, fullName: "acme/hello-world", private: false, defaultBranch: "main" },
  { installation: 42, fullName: "acme/billing", private: true, defaultBranch: "trunk" },
];

let queries: QueryClient;
let last: Picked | undefined;
function Harness() {
  const [value, setValue] = useState<Picked>();
  last = value;
  return <GitHubPicker repositories={repos} value={value} onChange={setValue} />;
}
function show() {
  queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queries}>
      <Harness />
    </QueryClientProvider>,
  );
}
afterEach(() => {
  cleanup();
  queries?.clear();
  vi.resetAllMocks();
  last = undefined;
});

it("filters the repositories as one types, in any case", () => {
  show();
  fireEvent.change(screen.getByRole("textbox", { name: "Filter the repositories" }), { target: { value: "BILL" } });
  expect(screen.getByRole("button", { name: /acme\/billing/ })).toBeTruthy();
  expect(screen.queryByRole("button", { name: /acme\/hello-world/ })).toBeNull();
  fireEvent.change(screen.getByRole("textbox", { name: "Filter the repositories" }), { target: { value: "nothing" } });
  expect(screen.getByText("No repository matches.")).toBeTruthy();
});

it("picks a repository on its default branch, then offers its branches with the default first", async () => {
  fake.githubBranches.mockResolvedValue(["feature", "trunk", "hotfix"]);
  show();
  fireEvent.click(screen.getByRole("button", { name: /acme\/billing/ }));
  expect(last).toEqual({ fullName: "acme/billing", branch: "trunk" });
  await waitFor(() => expect(fake.githubBranches).toHaveBeenCalledWith("acme/billing"));
  expect(await screen.findByRole("combobox", { name: "Branch" })).toBeTruthy();
});

it("shows why the branches cannot be listed", async () => {
  fake.githubBranches.mockRejectedValue(new Error("The GitHub connection on acme does not include billing: add it to the installation on GitHub"));
  show();
  fireEvent.click(screen.getByRole("button", { name: /acme\/billing/ }));
  expect(await screen.findByText(/does not include billing/)).toBeTruthy();
});

it("orders the branches with the default first, once", () => {
  expect(branchesInOrder(["dev", "main", "release"], "main")).toEqual(["main", "dev", "release"]);
  expect(branchesInOrder([], "main")).toEqual(["main"]);
});

it("makes a slug from the repository's name", () => {
  expect(slugFromRepo("Acme/Hello_World.js")).toBe("hello-world-js");
  expect(slugFromRepo("acme/shop")).toBe("shop");
});
```

In `apps/workspace/src/screens/project/source.test.tsx`:
- `fake` becomes `vi.hoisted(() => ({ updateProject: vi.fn(), projectGitHooks: vi.fn(), projectGitAuth: vi.fn() }))`;
- import `beforeEach` from vitest, and add
  `beforeEach(() => fake.projectGitAuth.mockResolvedValue({ using: "none" }));`;
- add:

```tsx
it("says pushes arrive through the GitHub App and offers no webhook to set up", async () => {
  fake.projectGitAuth.mockResolvedValue({ url: repo.url, using: "github-app", githubAccount: "acme" });
  show(projectWith(repo, { commit, latest: commit, via: "push", checkedAt: minutesAgo(1) }));
  expect(await screen.findByText("Through the GitHub App (acme): every push arrives by itself, nothing to set up.")).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Deploy on push" })).toBeNull();
});
```

Append to `apps/workspace/src/api/api.test.ts`, inside the `describe`:

```ts
  it("connects GitHub in the Mock, lists what it reaches and forgets a connection", async () => {
    const before = await api.github();
    expect(before.configured).toBe(true);
    const { installURL } = await api.connectGitHub();
    expect(installURL).toBe("/workspace/git");
    const after = await api.github();
    expect(after.installations.length).toBe(before.installations.length + 1);
    const repos = await api.githubRepositories();
    expect(repos.map((r) => r.fullName)).toContain("acme/hello-world");
    expect(await api.githubBranches("acme/hello-world")).toContain("main");
    await expect(api.githubBranches("someone/else")).rejects.toThrow(/No GitHub connection/);
    await api.disconnectGitHub(77);
    expect((await api.github()).installations.length).toBe(before.installations.length);
    expect((await api.projectGitAuth("hello-world")).using).toBe("github-app");
  }, 15_000);
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd apps/workspace && npm run test`
Expected: FAIL: `Failed to resolve import "./git"`, `"./github-picker"`, and `api.github is not a function`.

- [ ] **Step 3: Types, calls, Backend**

`src/api/types.ts`, after `GitHooks`:

```ts
// A GitHub App installation connected to the workspace (RFC-0054).
export type GitHubInstallation = {
  id: number;
  account: string;
  type: "Organization" | "User";
  connectedBy: string;
  createdAt: string;
  suspended: boolean;
  // The installation's settings on GitHub.
  manageURL: string;
};

// The workspace's GitHub: whether the platform has an App, why not, and
// the connections.
export type GitHubConnections = {
  configured: boolean;
  appURL?: string;
  note?: string;
  installations: GitHubInstallation[];
};

// A repository a connection reaches, for New project.
export type GitHubRepository = {
  installation: number;
  fullName: string;
  private: boolean;
  defaultBranch: string;
};

// What the project's builds clone with.
export type ProjectGitAuth = {
  url?: string;
  using: "none" | "token" | "deploy-key" | "github-app";
  githubAccount?: string;
};
```

`src/api/api.ts`: import the four types, and in `Api` after `projectGitHooks`:

```ts
  // What the project's builds clone with: a GitHub App, a deploy key, a token.
  projectGitAuth: (slug: string) => Promise<ProjectGitAuth>;
  // The workspace's GitHub (RFC-0054): its connections; Connect sends the
  // browser to GitHub's install page, which sends it back to the Git page.
  github: () => Promise<GitHubConnections>;
  connectGitHub: () => Promise<{ installURL: string }>;
  disconnectGitHub: (id: number) => Promise<void>;
  githubRepositories: () => Promise<GitHubRepository[]>;
  githubBranches: (fullName: string) => Promise<string[]>;
```

`src/api/backend.ts`, after `projectGitHooks`:

```ts
  projectGitAuth: (slug) => request(`${project(slug)}/git-auth`),
  github: () => request("/api/workspace/github"),
  connectGitHub: () => request("/api/workspace/github/connect", json("POST", {})),
  disconnectGitHub: (id) => request(`/api/workspace/github/installations/${id}`, gone),
  githubRepositories: () => request("/api/workspace/github/repositories"),
  githubBranches: (fullName) => {
    const [owner, repo] = fullName.split("/");
    return request(`/api/workspace/github/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repo)}/branches`);
  },
```

- [ ] **Step 4: The Mock**

`mock/things.json` gains three keys (the seed fills them into what a browser kept, so `shape` stays):

```json
  "github": {
    "configured": true,
    "appURL": "https://github.com/apps/shpyrd-acme",
    "installations": [
      { "id": 42, "account": "acme", "type": "Organization", "connectedBy": "ana@acme.test", "createdAt": "2026-10-01T09:00:00Z", "suspended": false, "manageURL": "https://github.com/organizations/acme/settings/installations/42" }
    ]
  },
  "githubRepositories": [
    { "installation": 42, "fullName": "acme/billing", "private": true, "defaultBranch": "main" },
    { "installation": 42, "fullName": "acme/docs", "private": false, "defaultBranch": "trunk" },
    { "installation": 42, "fullName": "acme/hello-world", "private": false, "defaultBranch": "main" },
    { "installation": 42, "fullName": "acme/reports", "private": true, "defaultBranch": "main" }
  ],
  "githubBranches": {
    "acme/billing": ["main", "hotfix"],
    "acme/docs": ["trunk"],
    "acme/hello-world": ["main", "dev", "release/2026"],
    "acme/reports": ["main"]
  }
```

`src/api/mock.ts`: import `GitHubConnections` and `GitHubRepository`; `Things` gains

```ts
  github: GitHubConnections;
  githubRepositories: GitHubRepository[];
  githubBranches: Record<string, string[]>;
```

and the Mock's methods, after `projectGitHooks`:

```ts
  projectGitAuth: async (slug) => {
    const url = (await projectsOf.find(slug)).spec.source?.git?.url;
    // The fixtures' URLs come with and without the scheme.
    const owner = url?.match(/^(?:https:\/\/)?github\.com\/([^/]+)\//i)?.[1]?.toLowerCase();
    return owner === "acme" ? { url, using: "github-app", githubAccount: "acme" } : { url, using: "none" };
  },
  github: async () => (await thingsOf.get()).github,
  // GitHub would ask where to install, then send the browser back to the
  // Git page: the Mock connects acme-labs and sends it there at once.
  connectGitHub: async () => {
    const all = await thingsOf.get();
    if (!all.github.configured) throw new ApiError(409, all.github.note ?? "GitHub is not set up on this platform.");
    if (!all.github.installations.some((i) => i.id === 77)) {
      all.github.installations.push({
        id: 77,
        account: "acme-labs",
        type: "Organization",
        connectedBy: "you@acme.test",
        createdAt: now(),
        suspended: false,
        manageURL: "https://github.com/organizations/acme-labs/settings/installations/77",
      });
      await thingsOf.set(all);
    }
    return { installURL: "/workspace/git" };
  },
  disconnectGitHub: async (id) => {
    const all = await thingsOf.get();
    all.github.installations = all.github.installations.filter((i) => i.id !== id);
    await thingsOf.set(all);
  },
  githubRepositories: async () => {
    const all = await thingsOf.get();
    if (!all.github.configured) throw new ApiError(409, all.github.note ?? "GitHub is not set up on this platform.");
    return all.github.installations.length ? all.githubRepositories : [];
  },
  githubBranches: async (fullName) => {
    const branches = (await thingsOf.get()).githubBranches[fullName];
    if (!branches) throw new ApiError(404, `No GitHub connection of this workspace reaches ${fullName.split("/")[0]}.`);
    return branches;
  },
```

- [ ] **Step 5: `src/lib/go.ts`**

```ts
// go sends the browser to another address (GitHub's install page, which
// sends it back). A module of its own so tests stand in for it.
export function go(url: string) {
  window.location.assign(url);
}
```

- [ ] **Step 6: Workspace settings → Git, `src/screens/workspace/git.tsx`**

```tsx
"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Unplug } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@shpyrd/ui/components/badge";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { ConfirmDialog } from "@shpyrd/ui/components/confirm-dialog";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { api } from "@/api/api";
import type { GitHubInstallation } from "@/api/types";
import { go } from "@/lib/go";
import { usePerms } from "@/lib/perms";
import { ago } from "@/lib/project";
import { Failed, Loading } from "../project/shared";

// The workspace's GitHub (RFC-0054): an account or organisation connected
// once, and anyone here picks its repositories when making a project;
// private ones build, and every push deploys through the App.
export function Git() {
  const perms = usePerms();
  const queries = useQueryClient();
  const github = useQuery({ queryKey: ["github"], queryFn: api.github });
  const connect = useMutation({
    mutationFn: api.connectGitHub,
    // GitHub asks where to install, then sends the browser back here.
    onSuccess: ({ installURL }) => go(installURL),
    onError: (e: Error) => toast.error(e.message),
  });
  const disconnect = useMutation({
    mutationFn: (i: GitHubInstallation) => api.disconnectGitHub(i.id),
    onSuccess: (_, i) => {
      queries.invalidateQueries({ queryKey: ["github"] });
      queries.invalidateQueries({ queryKey: ["github-repositories"] });
      toast.success(`${i.account} disconnected`, { description: "The App stays installed on GitHub until it is removed there." });
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const data = github.data;
  return (
    <Card>
      <CardHeader>
        <CardTitle>GitHub</CardTitle>
        <CardDescription>
          Connect an account or organisation once. Anyone in the workspace then picks one of its repositories when making a project: private ones build, and every push deploys. Nothing to paste per project.
        </CardDescription>
      </CardHeader>
      <CardContent className="grid gap-4">
        {github.isLoading ? (
          <Loading />
        ) : github.error ? (
          <Failed what="the GitHub connections" error={github.error} />
        ) : !data?.configured ? (
          <p className="text-sm text-muted-foreground">{data?.note ?? "GitHub is not set up on this platform."}</p>
        ) : (
          <>
            {perms.admin && (
              <div>
                <Button onClick={() => connect.mutate()} disabled={connect.isPending}>
                  Connect GitHub
                </Button>
              </div>
            )}
            {data.installations.length === 0 ? (
              <p className="text-sm text-muted-foreground">Nothing connected yet.</p>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Account</TableHead>
                    <TableHead>Kind</TableHead>
                    <TableHead>Connected by</TableHead>
                    <TableHead />
                    <TableHead className="text-right" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {data.installations.map((i) => (
                    <TableRow key={i.id}>
                      <TableCell className="font-medium">
                        <span className="flex items-center gap-2">
                          {i.account}
                          {i.suspended && <Badge variant="destructive">Suspended</Badge>}
                        </span>
                      </TableCell>
                      <TableCell>{i.type === "Organization" ? "Organisation" : "Personal account"}</TableCell>
                      <TableCell className="text-xs text-muted-foreground">
                        {i.connectedBy} · {ago(i.createdAt)}
                      </TableCell>
                      <TableCell>
                        <Button asChild variant="link" size="sm" className="px-0">
                          <a href={i.manageURL} target="_blank" rel="noreferrer">
                            Manage on GitHub
                          </a>
                        </Button>
                      </TableCell>
                      <TableCell className="text-right">
                        {perms.admin && (
                          <ConfirmDialog
                            trigger={<Button variant="ghost" size="icon-xs" icon={<Unplug />} aria-label={`Disconnect ${i.account}`} />}
                            variant="destructive"
                            title={`Disconnect ${i.account}?`}
                            description="Its repositories stop building through the App and its pushes stop deploying; projects with a deploy key or a token go on with it. The App stays installed on GitHub."
                            action="Disconnect"
                            onConfirm={() => disconnect.mutate(i)}
                          />
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </>
        )}
      </CardContent>
    </Card>
  );
}
```

`src/screens/workspace/index.tsx`: import `GitBranch` from `lucide-react` and `{ Git } from "./git"`; in `pages`, after the `drains` entry:

```tsx
  { group: "Platform", slug: "git", title: "Git", icon: <GitBranch />, needs: "admin" },
```

and among the routes, after `drains`: `<Route path="git" element={<Git />} />`.

- [ ] **Step 7: The picker, `src/screens/github-picker.tsx`**

```tsx
"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Lock } from "lucide-react";
import { Alert, AlertDescription } from "@shpyrd/ui/components/alert";
import { Button } from "@shpyrd/ui/components/button";
import { Field } from "@shpyrd/ui/components/field";
import { Input } from "@shpyrd/ui/components/input";
import { ScrollArea } from "@shpyrd/ui/components/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@shpyrd/ui/components/select";
import { Skeleton } from "@shpyrd/ui/components/skeleton";
import { api } from "@/api/api";
import type { GitHubRepository } from "@/api/types";

// What New project makes from GitHub: a repository and its branch.
export type Picked = { fullName: string; branch: string };

// The slug a project made from a repository gets unless one is typed:
// its name, in the letters a slug takes.
export function slugFromRepo(fullName: string): string {
  const name = fullName.split("/").pop() ?? "";
  return name
    .toLowerCase()
    .replace(/[^a-z0-9-]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

// The branches with the repository's default first, each once.
export function branchesInOrder(branches: string[], defaultBranch: string): string[] {
  return [defaultBranch, ...branches].filter((b, i, all) => b && all.indexOf(b) === i);
}

// The repositories the workspace's GitHub connections reach (RFC-0054):
// a filter over a list that scrolls, then the branch.
export function GitHubPicker({
  repositories,
  value,
  onChange,
}: {
  repositories: GitHubRepository[];
  value?: Picked;
  onChange: (p: Picked) => void;
}) {
  const [filter, setFilter] = useState("");
  const shown = useMemo(() => {
    const f = filter.trim().toLowerCase();
    return f ? repositories.filter((r) => r.fullName.toLowerCase().includes(f)) : repositories;
  }, [filter, repositories]);
  const chosen = repositories.find((r) => r.fullName === value?.fullName);
  const branches = useQuery({
    queryKey: ["github-branches", value?.fullName],
    queryFn: () => api.githubBranches(value!.fullName),
    enabled: !!chosen,
    staleTime: 60_000,
  });
  return (
    <div className="grid gap-3">
      <Field label="Repository">
        <Input value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Filter" aria-label="Filter the repositories" />
      </Field>
      <ScrollArea className="h-48 rounded-md border">
        <div className="grid p-1">
          {shown.map((r) => (
            <Button
              key={r.fullName}
              type="button"
              variant="ghost"
              size="sm"
              aria-pressed={r.fullName === value?.fullName}
              className="w-full justify-between font-normal aria-pressed:bg-accent aria-pressed:text-accent-foreground"
              onClick={() => onChange({ fullName: r.fullName, branch: r.defaultBranch })}
            >
              <span className="truncate font-mono text-xs">{r.fullName}</span>
              {r.private && <Lock className="size-3 shrink-0 text-muted-foreground" aria-label="Private" />}
            </Button>
          ))}
          {shown.length === 0 && <p className="px-2 py-1.5 text-sm text-muted-foreground">No repository matches.</p>}
        </div>
      </ScrollArea>
      {chosen && value && (
        <Field label="Branch">
          {branches.isLoading ? (
            <Skeleton className="h-9 w-full" />
          ) : branches.error ? (
            <Alert variant="destructive">
              <AlertDescription>{(branches.error as Error).message}</AlertDescription>
            </Alert>
          ) : (
            <Select value={value.branch} onValueChange={(branch) => onChange({ ...value, branch })}>
              <SelectTrigger aria-label="Branch">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {branchesInOrder(branches.data ?? [], chosen.defaultBranch).map((b) => (
                  <SelectItem key={b} value={b}>
                    {b}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
        </Field>
      )}
    </div>
  );
}
```

- [ ] **Step 8: From GitHub in New project, `src/screens/launcher.tsx`**

Imports: `Tabs, TabsContent, TabsList, TabsTrigger` from `@shpyrd/ui/components/tabs`; `{ GitHubPicker, type Picked, slugFromRepo }` from `./github-picker`.

In `NewProject`, after `const [path, setPath] = useState("");`:

```tsx
  // From GitHub (RFC-0054): offered when the workspace's connections reach
  // a repository, or when GitHub failed, so the error shows; asked only
  // while the dialog is open.
  const [from, setFrom] = useState<"github" | "url">("github");
  const [picked, setPicked] = useState<Picked>();
  const repos = useQuery({ queryKey: ["github-repositories"], queryFn: api.githubRepositories, enabled: open, retry: false, staleTime: 60_000 });
  const githubShown = repos.isSuccess ? repos.data.length > 0 : repos.error instanceof ApiError && repos.error.status === 502;
  const fromGitHub = githubShown && from === "github";
```

The mutation's `git:` line becomes:

```tsx
        git: fromGitHub
          ? picked && { url: `https://github.com/${picked.fullName}`, revision: picked.branch }
          : git.trim()
            ? { url: git.trim(), revision: ref.trim() || "main" }
            : undefined,
```

The `Field label="Git repository"` and the `{git.trim() && (…)}` grid after it move, unchanged, into a constant before `return`:

```tsx
  const gitURLFields = (
    <>
      <Field label="Git repository" hint="Optional: it is built and released right away. Without one, deploy later, from Git or from a checkout with the CLI.">
        <Input value={git} onChange={(e) => setGit(e.target.value)} placeholder="https://github.com/acme/hello-world" className="font-mono text-xs" />
      </Field>
      {git.trim() && (
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Branch, tag or commit">
            <Input value={ref} onChange={(e) => setRef(e.target.value)} placeholder="main" className="font-mono text-xs" />
          </Field>
          <Field label="Directory">
            <Input value={path} onChange={(e) => setPath(e.target.value)} placeholder="services/api" className="font-mono text-xs" />
          </Field>
        </div>
      )}
    </>
  );
```

and where they were, in the `Stack`:

```tsx
          {githubShown ? (
            <Tabs value={from} onValueChange={(v) => setFrom(v as "github" | "url")}>
              <TabsList>
                <TabsTrigger value="github">From GitHub</TabsTrigger>
                <TabsTrigger value="url">Git URL</TabsTrigger>
              </TabsList>
              <TabsContent value="github">
                {repos.error ? (
                  <Alert variant="destructive">
                    <AlertDescription>{repos.error.message}</AlertDescription>
                  </Alert>
                ) : (
                  <GitHubPicker
                    repositories={repos.data ?? []}
                    value={picked}
                    onChange={(p) => {
                      setPicked(p);
                      if (!slug.trim()) setSlug(slugFromRepo(p.fullName));
                    }}
                  />
                )}
              </TabsContent>
              <TabsContent value="url" className="grid gap-4">
                {gitURLFields}
              </TabsContent>
            </Tabs>
          ) : (
            gitURLFields
          )}
```

- [ ] **Step 9: The Source block, `src/screens/project/source.tsx`**

Replace `{perms.config && <DeployOnPush slug={project.slug} />}` with `<SourceAccess slug={project.slug} canConfig={perms.config} />`, and add:

```tsx
// How pushes reach shpyrd: through the GitHub App, with nothing to set
// up, or through the project's own webhook (RFC-0054).
function SourceAccess({ slug, canConfig }: { slug: string; canConfig: boolean }) {
  const auth = useQuery({ queryKey: ["git-auth", slug], queryFn: () => api.projectGitAuth(slug), staleTime: 60_000 });
  if (auth.data?.using === "github-app")
    return (
      <p className="text-xs text-muted-foreground">
        Through the GitHub App ({auth.data.githubAccount}): every push arrives by itself, nothing to set up.
      </p>
    );
  return canConfig ? <DeployOnPush slug={slug} /> : null;
}
```

- [ ] **Step 10: Run the tests and the gates**

Run: `cd apps/workspace && npm run test && npm run lint && npm run typecheck`
Expected: all pass.

Then look at it: `npm run design` in `apps/workspace` (port 4325; the Mock). Open `/workspace/git`, the launcher's New project (From GitHub, filter, pick, branch), and `/projects/hello-world` (the App line), each in light and dark and at a phone's width, with the browser tools. Check: nothing overflows, the list scrolls inside its frame, the Suspended badge and the Disconnect dialog read as the tests say. Fix what does not, then stop the server.

- [ ] **Step 11: Commit**

```bash
git add apps/workspace/src apps/workspace/mock/things.json
git commit -m "feat(workspace): Workspace settings, Git; From GitHub in New project; the App in the Source block (RFC-0054)

Co-Authored-By: <your model name> <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 9: Docs, and RFC-0054 in progress

**Files:**
- Modify: `content/docs/deploying.mdx` (the start of "Private repositories" ~76; the "Connect GitHub is coming" callout ~176-180 becomes a section)
- Modify: `content/docs/installation.mdx` (a section after "Install on an existing cluster", before "Upgrading" ~191)
- Modify: `content/docs/cli.mdx` (the "Cluster" table ~70-92; a "GitHub" section after "Deploy on push" ~121)
- Modify: `rfcs/0054-github-app.md` (header, Implementation History), `rfcs/README.md:98`

**Interfaces:**
- Consumes: the behaviour of Tasks 1 to 8 as built. Commands, messages and labels are copied from the code, not paraphrased.

- [ ] **Step 1: The deploying page**

At the start of "Private repositories", after its first paragraph, add:

```mdx
On GitHub, the simplest way is to [connect GitHub](#connect-github) once for the workspace: no key or token per project. The two ways below work on any git host, and on GitHub too when a repository is not in a connected account.
```

Replace the "Connect GitHub is coming" callout with:

````mdx
### Connect GitHub

A workspace connects GitHub once. Then anyone who makes projects picks a repository from a list: private ones build without deploy keys or tokens, and every push to a project's branch deploys, with no webhook to add.

**Connect an account or organisation** (workspace admins): in the dashboard, Workspace settings, **Git**, **Connect GitHub**. GitHub asks where to install shpyrd's App and which repositories it may read, then sends you back. Or from a terminal:

```shell
shpyrd github connect            # opens GitHub, waits until you are back
shpyrd github list               # the workspace's connections
shpyrd github disconnect acme    # forget one; the App stays installed on GitHub
```

shpyrd connects an installation only when your own GitHub account can see it, so a link copied from someone else's browser connects nothing. **Manage on GitHub** opens the installation's settings, where you add repositories, suspend it or remove it.

**Make a project from a repository:** New project, **From GitHub**, filter the list, pick the repository and the branch (its default branch first). Or find it with `shpyrd github repos --filter shop` and deploy it as any git repository:

```shell
shpyrd deploy --git https://github.com/acme/shop --ref main --project shop
```

**How builds reach it.** For a repository on github.com whose owner the workspace has connected, every build and every check of the branch uses a token the App gives for that one repository, read-only, that lasts an hour; shpyrd renews it every 50 minutes. That comes before the project's deploy key and the workspace's token, which still work when the App does not reach a repository. `shpyrd git-auth show --project shop` says `through the GitHub App (acme)`, and so does the project's Summary.

**Pushes** arrive through the App's own webhook: `shpyrd git-hooks show` says there is nothing to set up. `[skip deploy]`, auto-deploy and the checks of the branch work as in [Deploying on push](#deploying-on-push).

**When the App cannot read a repository.** A repository the installation does not include, or a suspended installation, makes shpyrd try the deploy key and the token. When nothing gives access, the project says why, starting with the App's reason ("The GitHub App's installation on acme does not include shop: add the repository to it on GitHub."), and what runs keeps running.

**On a self-hosted platform**, an operator registers the cluster's App once first: see [Installation](/docs/installation#connect-github). On shpyrd cloud, it is there.
````

- [ ] **Step 2: The installation page**

After "Install on an existing cluster", add:

````mdx
## Connect GitHub

Workspaces connect GitHub through one GitHub App per cluster, which the operator registers once:

```shell
shpyrd-ctl github app create --org acme      # or without --org, under your own account
```

A browser opens on GitHub with the App filled in: its name (`shpyrd-<dashboard host>`, or `--name`), its webhook at `<dashboard>/api/github/webhook`, its install callback at `<dashboard>/api/github/installed`, and the permissions it asks for (contents, metadata and statuses read; checks and deployments write, for what comes next). One click creates it. `shpyrd-ctl` keeps its credentials in the Secret `shpyrd-system/shpyrd-github-app` and prints the link that installs it. The App is private unless you pass `--public`: then any account can install it, not only its owner.

GitHub must reach the dashboard's address for the webhook: a cluster that only answers on a private network gets pushes by checking the branch instead, every few minutes. The dashboard's address comes from the cluster's install settings; `--url` gives another. The command waits ten minutes for GitHub; Ctrl-C stops it.

```shell
shpyrd-ctl github app show      # slug, id and page on GitHub; never the keys
shpyrd-ctl github app delete     # remove the Secret; then delete the App on GitHub
```

For GitHub Enterprise Server, set `SHPYRD_GITHUB_API_URL` on the server to its API's address; nothing else is supported yet.
````

- [ ] **Step 3: The CLI page**

In the "Cluster" table, after the `shpyrd-ctl cluster unpin` row:

```mdx
| `shpyrd-ctl github app create [--org ORG] [--name NAME] [--public] [--url DASHBOARD_URL]` | Register the cluster's GitHub App on GitHub from a manifest, in one click in the browser; its credentials go into the Secret `shpyrd-system/shpyrd-github-app`. `github app show` prints its slug, id and page (never the keys); `github app delete` removes the Secret. See [Connect GitHub](/docs/installation#connect-github). |
```

After the "Deploy on push" section:

```mdx
## GitHub

Connect an account or organisation once; then pick its repositories. Signed in only. See [Connect GitHub](/docs/deploying#connect-github).

| Command | What it does |
| --- | --- |
| `shpyrd github connect [--no-browser]` | Open GitHub's install page for the platform's App and wait until the new connection appears (workspace admins). |
| `shpyrd github list` | The workspace's connections: account, kind, id, who connected it, and whether it is suspended on GitHub. |
| `shpyrd github disconnect <account\|id>` | Forget a connection (workspace admins). Its repositories stop building through the App; the App stays installed on GitHub. |
| `shpyrd github repos [--filter TEXT]` | The repositories the connections reach, private or public, with their default branch. |
```

and in the "Git credentials" table, the `shpyrd git-auth show` row's text becomes: `Which credential the project's builds clone with: the GitHub App, a deploy key, a token, or none.`

- [ ] **Step 4: RFC-0054 in progress**

`rfcs/0054-github-app.md` header:

```markdown
**Status:** in progress

**Owner:** Marcelo Paez Sequeira

**Depends on:** RFC-0017 (implemented), RFC-0018 (implemented)

**Creation date:** 2026-09-22

**Last update:** 2026-10-16
```

Under `## Implementation History`, after `- 2026-09-22: RFC written.`:

```markdown
- 2026-10-16: part 3, Connect GitHub: `shpyrd-ctl github app create` registers the cluster's App from a manifest; workspaces connect installations (checked against the person's own GitHub account) from the dashboard or `shpyrd github connect`; From GitHub in New project; builds and the source watcher clone with repository-scoped installation tokens before deploy keys and tokens; the App's webhook routes pushes; installation suspensions and removals are recorded. See docs/superpowers/specs/2026-10-16-github-app-connect-design.md. Part 4 (checks, Deployments, watch paths, wait-for-CI) follows.
```

`rfcs/README.md` line 98: the status column `implementable` becomes `in progress`.

- [ ] **Step 5: Check the MDX**

`content/` has no build or check of its own; the website renders it. Check by eye: every fence closed (the two new sections use four-backtick outer fences), the new rows with the same column count as their tables (a `|` inside a cell is escaped `\|`), and the anchors `#connect-github` match the headings on both pages.

- [ ] **Step 6: Commit**

```bash
git add content/docs/deploying.mdx content/docs/installation.mdx content/docs/cli.mdx rfcs/0054-github-app.md rfcs/README.md
git commit -m "docs: connect GitHub; the cluster's GitHub App; shpyrd github; RFC-0054 in progress

Co-Authored-By: <your model name> <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 10: The real run, screenshots, the PR

**Done after the final review of the whole branch, not before.** Tasks 1 to
9 are reviewed one by one and then together; this task runs the result
against a real GitHub App and opens the PR. A fault found here is fixed in
the task that owns the code, with a test, and reviewed again.

**It needs the maintainer.** They provide: a GitHub test organisation where
they may register an App, a private test repository in it holding a tiny
Node app (PR 1's will do), and a way for GitHub to reach the kind cluster's
webhook (below). The agent asks for these before starting and waits.

**Files:**
- Modify: `rfcs/0054-github-app.md` (Implementation History)

- [ ] **Step 1: Full test run**

Run: `go build ./... && go vet ./... && go test ./... && make lint && npm run test --workspace apps/workspace && npm run lint --workspace apps/workspace && npm run typecheck --workspace apps/workspace`
Expected: all pass.

- [ ] **Step 2: The cluster**

Follow the local cluster memory: kind on 80/443 (8443 breaks the local sign-in), load the built server image (`grep -n "^kind\|^dev\|load" Makefile` names the targets), never `extensions enable`. Sign in to the dashboard as an admin of the default workspace.

- [ ] **Step 3: Reaching the cluster from GitHub**

The App's URLs are fixed when it is registered, so this comes first. The maintainer chooses one:

1. **A tunnel** to the cluster's front door that keeps the console's Host:
   `cloudflared tunnel --url https://<console host> --http-host-header <console host> --no-tls-verify`
   gives `https://<random>.trycloudflare.com`. Register the App with
   `--url https://<random>.trycloudflare.com` (step 4). The browser comes
   back through the tunnel too, and the callback then sends it to the
   dashboard's own address.
2. **No tunnel:** register with the dashboard's own URL; the install
   callback works from the maintainer's browser, which reaches the cluster.
   GitHub's webhook deliveries fail; after each push, open the App's
   Advanced settings on GitHub, copy the delivery's payload and headers, and
   post them with `curl` to `https://<console host>/api/github/webhook`
   (GitHub's "Redeliver" needs GitHub to reach the cluster). Say in the PR
   which way was used.

- [ ] **Step 4: Register the App**

```shell
shpyrd-ctl github app create --org <test org> [--url https://<tunnel host>]
```

Click "Create GitHub App" on the page that opens. Expect "Registered …" in
the browser and the terminal, the install link printed, and
`kubectl -n shpyrd-system get secret shpyrd-github-app -o jsonpath='{.data}' | jq 'keys'`
listing the seven keys. `shpyrd-ctl github app show` prints no key.

- [ ] **Step 5: The runs**

1. Dashboard, Workspace settings, **Git**: **Connect GitHub**; on GitHub,
   install on the test organisation with only the private test repository;
   expect to land back on the Git page with the organisation listed.
   Repeat with `shpyrd github connect` from a terminal once the first
   connection is disconnected, to see the CLI wait and report it.
2. Open the callback URL from the browser's history again: expect "This
   link was used already".
3. New project, **From GitHub**: filter, pick the private repository, its
   default branch first in Branch; Create. Expect a build that clones
   (`kubectl get secret -n <ns> <app>-git -o jsonpath='{.metadata.annotations}'`
   shows `shpyrd.io/github-account` and a minted-at time), a release, and the
   app answering at its URL. `shpyrd git-auth show --project <slug>` says
   "through the GitHub App"; `shpyrd git-hooks show` says there is nothing to
   set up; the project's Summary says the same.
4. Push a commit "change the greeting". Expect a release
   `Push <sha> by <user>: change the greeting` within seconds (with the
   tunnel) or after posting the delivery (without), and the new greeting.
5. Suspend the installation on GitHub (its settings, Suspend). Expect the
   Git page's Suspended badge (a `github.suspend` entry on the dashboard's
   Activity page), and after the next check of the branch the Summary's
   error starting "The GitHub App's installation on <org> is suspended"; a
   build started then fails with PR 1's no-access reason, and what runs keeps
   running. Unsuspend: within the poll interval the error clears.
6. When the run has lasted more than 50 minutes since step 3, check that
   `<app>-git`'s minted-at time moved forward without a push, and that
   `shpyrd redeploy --project <slug>` then builds: the token is never the
   first one.
7. Disconnect on the Git page: the next build falls through (no
   credential: the clone is refused with PR 1's message).

- [ ] **Step 6: Screenshots**

With the browser tools, from the dashboard: the Git page with a connection,
and with the Suspended badge; the Disconnect dialog; New project's From
GitHub tab with the filter and the branch Select; the project's Summary with
"Through the GitHub App (…)"; the release named `Push … by …`; the Git page of
a platform without the App (the operator's note), from the Mock if easier.
Light theme, and one in dark. As text: the `shpyrd-ctl github app create`
transcript (no keys in it), `shpyrd github list`, `shpyrd github repos`,
`shpyrd git-hooks show`. Save images in the scratchpad, not the repository.

- [ ] **Step 7: RFC-0054's history**

Under `## Implementation History`:

```markdown
- 2026-10-16: part 3 verified end to end on a kind cluster against a real GitHub App on a test organisation and a private repository: registered with shpyrd-ctl, connected from the dashboard and the CLI, a project made From GitHub builds with a repository token and deploys on push, suspension recorded and explained.
```

The status stays `in progress`: part 4 follows.

```bash
git add rfcs/0054-github-app.md
git commit -m "docs(rfc): RFC-0054 part 3 verified end to end on a kind cluster

Co-Authored-By: <your model name> <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

- [ ] **Step 8: Push and open the PR**

```bash
git push -u origin feat/github-app-connect
```

`gh pr create --base feat/private-git-watcher` (stacked on PR 2, #225; after
#225 merges, retarget to `main`):
- Title: `Private repositories, part 3: Connect GitHub (RFC-0054)`.
- Body: what it does; the deviations at the top of this plan; what an
  operator does once (`shpyrd-ctl github app create`) and what the cloud
  repository must do (write the Secret `shpyrd-system/shpyrd-github-app` with
  the seven keys for its public App, its webhook at the cloud's dashboard);
  migration 000028; how it was tested, and which way GitHub reached the
  cluster; the screenshots; the CLI transcripts in code blocks; that part 4
  follows.
- End the body with:
  ```
  🤖 Generated with [Claude Code](https://claude.com/claude-code)

  https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb
  ```
- `gh` cannot attach images: open the PR first, then give the maintainer
  the image paths to drop in, or upload them through the browser tools on
  the PR page.
