# GitHub access by person, and webhooks before polling Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Each person links their own GitHub account and picks only the
repositories they can read, a project is made from a GitHub repository only
by someone who can read it, a repository the GitHub App covers is never
polled (a sweep redelivers what GitHub failed to deliver), and other hosts
are polled once per repository, under a cluster-wide cap, until their
webhook delivers.

**Architecture:** A new `pkg/tokenseal` seals secrets with AES-256-GCM
under a key in the Secret `shpyrd-token-key`. The store gains
`github_user_links` (migration 000029), one row per shpyrd identity, the
refresh token sealed. `pkg/github` gains the user-token calls (exchange,
refresh, `/user`, `/user/installations/{id}/repositories`, `/repos/{o}/{r}`,
branches, revoke) and the App's delivery log (`/app/hook/deliveries`,
redelivery); the fake GitHub answers all of them for several people. The
API links through the same callback as Connect (`/api/github/installed`, the
state says "link"), finishes on the workspace's address with the session
that started it, keeps each person's access token in memory, answers the
picker per person (`accounts`, `repositories?installation=`, `branches`),
and checks create and deploy. The source watcher stops listing what the App
covers (`WatchMode`), shares one listing per repository and credential
behind a token bucket, and stops listing a project once its own webhook has
delivered; a leader-only `GitHubSweep` asks GitHub to redeliver failed
deliveries every 5 minutes. The CLI gets `github link`, `github unlink`
and the GitHub line of `whoami`; the dashboard gets the link step, the
account switcher, the person menu's GitHub item and the Source block's
wording.

**Tech Stack:** Go, gin, cobra, controller-runtime v0.25.1, pgx; Next 16,
React 19, TanStack Query, `@shpyrd/ui`, Vitest.

**Spec:** `docs/superpowers/specs/2026-10-17-github-user-link-and-polling-design.md`
(binding; where it and the earlier specs differ, it wins), building on
`docs/superpowers/specs/2026-10-16-private-git-repositories-design.md` and
the three specs after it. Branch: `feat/private-git-repositories`, PR #223,
which holds the whole private-repositories feature.

## Global Constraints

- The brand is written `shpyrd`, lowercase, everywhere: docs, CLI output,
  pages, dashboard copy, log lines. `X-Shpyrd-*` headers keep their
  capitals.
- The spec is binding; where the code disagreed, or the spec left a choice
  open, the rulings below say how it was resolved.
- Out of scope, not even as fields or routes: Vercel Pro's check on commit
  authors, linking automatically on GitHub sign-in, GitLab and Bitbucket
  integrations.
- Secrets never appear in a log line, an audit detail, an error, an API
  answer, a page or a URL: the App's private key, client secret and webhook
  secret; installation tokens; a person's access and refresh tokens; the
  token key. Only the sealed refresh token is stored; the access token
  lives in memory only. A link ticket carries GitHub's one-time `code`,
  signed, never a token.
- Every call to GitHub dials through `gitauth.SafeHTTPClient` (the
  `github.Client` default). Tests reach the fake GitHub through
  `githubtest.Server.Options()` / `Source()`.
- Token scopes: installation tokens keep `ContentsRead()`, `WatchRead()`
  and `ReportWrite()` (functions; each call returns a fresh map). A user
  token can do only what both the person and the App may.
- The token key: AES-256-GCM, 32 random bytes in the Secret
  `shpyrd-token-key` (key `key`) in the system namespace, made by the
  server when absent, never logged. Each sealed value is bound to its row
  (provider and subject) as additional data.
- The link state: signed with the App's client secret, 15 minutes, single
  use, bound to provider and subject; the ticket to the finish: 5 minutes,
  single use, spent only when the same provider and subject present it on
  the workspace's address.
- Polling: `SHPYRD_GIT_POLL_INTERVAL`, default **5 minutes**, minimum 30
  seconds; `SHPYRD_GIT_POLL_RATE` listings a second, default 5. The held
  commit's fallback recheck is 2 minutes; the shared CI read is 90 seconds;
  the one-hour CI limit is unchanged. The sweep runs every 5 minutes and at
  start (24 hours back), at most 50 redeliveries a sweep, each delivery
  resent at most once a day, on the leader only.
- The AppReconciler is the only writer of App status. The watcher writes
  annotations only; the API reads the record annotation for its view.
- Messages are plain sentences that name the fix, in the style of
  `describeSourceError` and the existing GitHub messages. The spec's
  sentences are used verbatim where it gives them ("You can't read
  <owner>/<repo> on GitHub", "Checked every 5 min. Set up the webhook to
  deploy at once and stop the checks.", "Deploys on push through its
  webhook (last delivery …)").
- Audit actions: `github.link`, `github.unlink` (target `github user
  <login>`, no detail). Nothing else new.
- The dashboard uses only `@shpyrd/ui` components and tokens, calls only
  through `api`, and the Mock answers every new call (`apps/AGENTS.md`;
  `design/DESIGN_FOR_AGENT.md` for any design session). `npm run lint`,
  `npm run typecheck` and `npm run test` pass in `apps/workspace`. Vitest
  tests are named as sentences.
- Every `go build`, `go test` and `go vet` runs with
  `GOTMPDIR=/home/nkr/.cache/gotmp TMPDIR=/home/nkr/.cache/gotmp` (create
  the directory once with `mkdir -p /home/nkr/.cache/gotmp`). Postgres store
  tests run when `SHPYRD_TEST_DATABASE_URL` is set; run them once before
  Task 13.
- Every commit ends with these two lines, the first naming the model that
  wrote the commit (for example `Claude Opus 5.5 <noreply@anthropic.com>`):
  ```
  Co-Authored-By: <your model name> <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb
  ```
- Commits go on `feat/private-git-repositories`. Do not push until Task 13
  says so. PR #223 gets screenshots from both sides (Task 13).

## Rulings: where the spec was open, and where the code disagreed

1. **The picker's routes stay under `/api/workspace/github/`.** The spec
   names `GET /api/github/accounts` and `GET /api/github/repositories`; the
   picker's routes are `/api/workspace/github/repositories` and
   `/api/workspace/github/repositories/:owner/:repo/branches` today, and
   `/api/github/` is the session-less prefix (the App's webhook, the
   install callback). So: `GET /api/workspace/github/accounts` (new),
   `GET /api/workspace/github/repositories[?installation=<id>]` (per person;
   without `installation`, every account the person sees, for the CLI's
   `github repos`), and the branches route, all `ClusterCreate`.
2. **`PATCH /api/projects/:slug` cannot change `git.url`** (`UpdateAppRequest`
   has no git field). The check runs on `POST /api/projects` and on
   `POST /api/projects/:slug/deploy` when the URL names another repository
   (`sameRepository`: host and path, any case, any form).
3. **"422, as today" has no today.** Nothing refused a repository outside
   the installations before. Now: an owner the workspace has a live
   installation on, and a private repository that installation does not
   include, is 422 ("The GitHub App's installation on acme does not include
   acme/vault, or it does not exist: …"). An owner the workspace has no
   installation on is not checked at all: it builds with a deploy key or a
   token, as before (PR 1).
4. **Linking finishes on the workspace's address, like Connect.** GitHub
   returns to the registered callback (`/api/github/installed`), which may be
   another host than the workspace's, with no session. The callback checks
   and spends the state, then hands GitHub's `code` in a signed, 5-minute,
   single-use ticket to `/api/me/github/finish` on the workspace's address,
   where the session must be the one that started; only then is the code
   exchanged. No token is held for an unchecked identity.
5. **The admin token is no person.** `POST/GET/DELETE /api/me/github`
   refuse it (400/404); the picker answers it 409 `github_link_required`; the
   create and deploy check lets it (and a token the admin token made, and a
   server without sign-in) through: the operator holds the App's key
   anyway.
6. **An App without expiring user tokens** gives no refresh token: the link
   is refused with a page naming GitHub's setting ("Expire user
   authorization tokens"), the access token is revoked, nothing is stored.
7. **A refresh GitHub refuses** (`bad_refresh_token`: expired, revoked, or a
   key rotation leaving the row unreadable) removes the link; the person sees
   **Continue with GitHub** again.
8. **Unlink revokes the current access token** with
   `DELETE /applications/{client_id}/token` (refreshing first when none is
   kept in memory). When GitHub cannot be reached, the row is removed all the
   same and the log says the revoke failed: shpyrd can no longer use the
   token either way.
9. **Accounts answer `{accounts, connected}`.** A workspace with no
   installation answers `connected: 0` without asking for a link, so New
   project never asks someone to link where nothing can be picked.
10. **Add GitHub account or organisation is Connect** (workspace admins:
    `POST /api/workspace/github/connect` is `ClusterAdmin`). Others see "A
    workspace admin adds accounts in Workspace settings → Git."
11. **"Its webhook has delivered" means the project's own webhook.** A push
    left by the GitHub App's webhook carries `Push.App`; only another push
    sets the record's `ProjectHook`. A repository the App stops covering is
    listed again even though App pushes arrived before.
12. **A push-driven branch** (covered by the App, or its own webhook
    delivered) is reconciled once an hour (`pushDrivenRecheck`) to look at
    its cover again, and is never listed on a timer. A failed read (a
    deploy's read while the host was down) keeps the existing backoff, so it
    is retried.
13. **The shared listing serves timer checks only.** A read a person caused
    (a new project, a deploy, a push that does not follow) lists fresh, and
    may borrow up to a burst ahead of the cap, so it never waits behind the
    timer's.
14. **The webhook already ignores a delivery id it applied or holds**
    (`seenDelivery`: the push waiting, or the record's last). An older
    delivery redelivered reaches the watcher, which lists the branch and
    pins nothing new. Both pinned by tests (Task 9).
15. **The sweep's memory is the leader's.** A new leader looks back 24 hours
    again and may resend a delivery once more; the webhook and the watcher
    ignore it (14).
16. **A project made through a link records `shpyrd.io/github-linked-by`**
    (the GitHub login), shown as "Connected by <login>" in the Source block.
    A deploy to another repository without a link (a public one) removes it.

## Review Focus

1. **A link state used by another identity.** Ana starts a link; Bob's
   session, or the admin token, presents the ticket. Expected: refused with
   "started by someone else", nothing stored, and Ana can still finish.
   Test: `TestALinkStartedBySomeoneElseIsRefused` (Task 4).
2. **The picker shows a repository the person cannot read.** The
   installation covers `Acme/shop` and `Acme/site`; Bob can read only
   `Acme/site`. Expected: Bob's accounts, repositories (one account or all)
   and branches never show `Acme/shop`. Test:
   `TestThePickerListsOnlyWhatThePersonCanRead` (Task 5).
3. **An API token acting as its maker.** An agent creates a project with
   Ana's `shp_` token. Expected: Ana's link is used, both for
   `GET /api/me/github` and for the create; a token of an unlinked person is
   asked to link. Tests: `TestAnAPITokenUsesItsMakersGitHubLink` (Task 4),
   `TestAnAPITokenCreatesWithItsMakersLink` (Task 6).
4. **A public repository with no link.** Expected: created, with no call to
   GitHub as the person. Test: `TestAPublicRepositoryNeedsNoLink` (Task 6).
5. **A redelivered push.** The sweep redelivers a push the webhook already
   took, or an older one. Expected: not left on the App again; an older one
   pins nothing new. Tests: `TestARedeliveredPushIsNotAppliedTwice`,
   `TestAnOlderPushDeliveredAgainPinsNothing` (Task 9).
6. **A thousand projects on other hosts.** Expected: never more than
   `SHPYRD_GIT_POLL_RATE` listings in any second, and every one listed
   within 1000 / rate seconds. Test: `TestAThousandProjectsStayUnderTheCap`
   (Task 11).
7. **A covered repository on a timer.** Days pass. Expected: no listing,
   ever; a deploy reads once; uncovered, it is listed again. Test:
   `TestACoveredRepositoryIsNeverListedOnATimer` (Task 10).

Also pinned, outside the seven: the sealed token opening only with its key
and row (`TestASealedValueOpensWithItsKeyAndRowOnly`, Task 1), a refresh
token used once and rotated in the store
(`TestAPersonsTokenIsRefreshedOnceAndTheNewRefreshTokenStored`, Task 4),
three projects on one repository listed once and not after their webhook
(`TestARepositoryIsListedOnceForItsProjectsThenNotAfterItsWebhook`, Task 11),
the sweep's selection (`TestTheSweepPicksFailedDeliveriesOnce`, Task 9).

---

## File structure

| File | Responsibility |
|---|---|
| `pkg/tokenseal/tokenseal.go`, `tokenseal_test.go` (new) | AES-256-GCM `Box`, `Row`, `LoadOrCreate` of `shpyrd-token-key` |
| `pkg/api/server.go` (modify: `Server` fields ~291-302, `newServer` ~407-445, `routes` ~636-704) | The box, the user-token cache, the new routes |
| `pkg/store/github.go`, `github_memory.go`, `github_postgres.go`, `memory.go` (modify), `migrations/000029_github_user_links.{up,down}.sql` (new), `github_test.go`, `store_test.go` (`dropAll`) | `GitHubUserLink` and its three methods |
| `pkg/github/client.go` (modify: `ExchangeCode`, `Repositories`, `Branches`, `call`, `send`) | Refactors the new calls share |
| `pkg/github/user.go`, `user_test.go` (new) | `UserToken`, `User`, exchange, refresh, profile, user repositories, repository, branches, revoke |
| `pkg/github/hooks.go`, `hooks_test.go` (new) | `HookDelivery`, `HookDeliveries`, `Redeliver` |
| `pkg/github/githubtest/fake.go` (modify), `githubtest/users.go`, `githubtest/hooks.go` (new) | Several people, user tokens, public repositories, revocation, deliveries |
| `pkg/api/github.go` (modify: `githubState`, `readGitHubState`, `githubInstalled`, `githubRepoCache`, `forgetGitHubRepos`, `githubRepositories`, `githubBranches`) | The link's state, the per-person picker |
| `pkg/api/github_link.go`, `github_link_test.go` (new) | Link start, callback, finish, me, unlink; the user-token cache |
| `pkg/api/github_picker_test.go` (new), `github_test.go` (modify: drop two admin-token picker tests) | The per-person picker |
| `pkg/api/tokens.go` (modify: `identifyWithToken`) | The token's maker on the request |
| `pkg/api/github_access.go`, `github_access_test.go` (new), `apps.go` (modify: `createApp`, `deployApp`), `github_features.go` (modify: `projectDetail`, `AppDetail` fields) | The check on create and deploy; `sourceWatch`, `githubLinkedBy` |
| `api/v1alpha1/app_types.go` (modify: after `AnnotationGitHubReport`) | `AnnotationGitHubLinkedBy` |
| `pkg/api/github_webhook.go` (modify: `githubPush`), `github_webhook_test.go` | `Push.App`; redelivery |
| `pkg/gitauth/webhook.go` (modify: `Push`, `SourceRecord`) | `Push.App`, `SourceRecord.ProjectHook` |
| `internal/controller/source_controller.go` (modify), `source_poll.go`, `source_poll_test.go` (new), `source_gates.go` (modify: constants) | `WatchMode`, no timer for push-driven branches, shared listings, the bucket |
| `internal/controller/github_sweep.go`, `github_sweep_test.go` (new) | The catch-up sweep |
| `pkg/server/server.go` (modify: ~576-592) | `PollRate`; register the sweep |
| `internal/cli/github.go`, `login.go` (`newWhoAmICmd`), `registry.go` (`serverRequestDirect`), `github_link_test.go` (new) | `github link`, `github unlink`, whoami's GitHub line, the link-required error |
| `apps/shared/src/api/error.ts`, `http.ts` | `ApiError.code` |
| `apps/workspace/src/api/types.ts`, `api.ts`, `backend.ts`, `mock.ts`, `mock/things.json`, `mock/projects.json`, `api.test.ts` | Types, calls, Mock |
| `apps/workspace/src/lib/github-link.ts`, `github-link.test.ts` (new) | The popup link |
| `apps/workspace/src/screens/github-picker.tsx`, `github-picker.test.tsx`, `launcher.tsx`, `launcher.test.tsx` | Link step, account switcher, below-the-list links |
| `apps/workspace/src/shell/person.tsx`, `person.test.tsx` (new) | The person menu's GitHub item |
| `apps/workspace/src/screens/project/source.tsx`, `source.test.tsx` | How the branch is watched |
| `content/docs/deploying.mdx`, `content/docs/cli.mdx` | Docs |
| `rfcs/0054-github-app.md` | Implementation History |

---

### Task 1: The token key, and sealing with it

**Files:**
- Create: `pkg/tokenseal/tokenseal.go`, `pkg/tokenseal/tokenseal_test.go`
- Modify: `pkg/api/server.go` (`Server` fields beside `githubApp` ~294; `newServer` after `s.githubApp = …` ~427)
- Test: `pkg/api/github_link_test.go` (new; this task adds its first test)

**Interfaces:**
- Consumes: nothing new.
- Produces:
  - `const tokenseal.SecretName = "shpyrd-token-key"`, `tokenseal.KeyName = "key"`
  - `var tokenseal.ErrCannotOpen error`
  - `type tokenseal.Box`; `func tokenseal.New(key []byte) (*Box, error)`; `func tokenseal.Generate() (*Box, error)`
  - `func (*Box) Seal(plain, row string) (string, error)` (`"v1." + base64`), `func (*Box) Open(sealed, row string) (string, error)`
  - `func tokenseal.Row(parts ...string) string` (joined with `\x00`)
  - `func tokenseal.LoadOrCreate(ctx context.Context, kube kubernetes.Interface, namespace string) (*Box, error)`
  - `Server.githubBox *tokenseal.Box`

- [ ] **Step 1: Write the failing tests**

`pkg/tokenseal/tokenseal_test.go`:

```go
package tokenseal

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func TestASealedValueOpensWithItsKeyAndRowOnly(t *testing.T) {
	box, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	row := Row("github-user-link", "local", "p-1")
	sealed, err := box.Seal("ghr_secret", row)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, "ghr_secret") || !strings.HasPrefix(sealed, "v1.") {
		t.Fatalf("sealed %q", sealed)
	}
	if got, err := box.Open(sealed, row); err != nil || got != "ghr_secret" {
		t.Fatalf("open: %q %v", got, err)
	}
	if again, _ := box.Seal("ghr_secret", row); again == sealed {
		t.Error("two seals of one value are alike: the nonce is not fresh")
	}
	if _, err := box.Open(sealed, Row("github-user-link", "local", "p-2")); !errors.Is(err, ErrCannotOpen) {
		t.Errorf("another row's value opened: %v", err)
	}
	other, _ := Generate()
	if _, err := other.Open(sealed, row); !errors.Is(err, ErrCannotOpen) {
		t.Errorf("a wrong key opened it: %v", err)
	}
	for _, bad := range []string{"", "v1.", "v1.!!!", "ghr_secret", sealed[:len(sealed)-4]} {
		if _, err := box.Open(bad, row); !errors.Is(err, ErrCannotOpen) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

func TestAKeyIsThirtyTwoBytes(t *testing.T) {
	if _, err := New(make([]byte, 16)); err == nil || !strings.Contains(err.Error(), "32 bytes") {
		t.Errorf("16 bytes: %v", err)
	}
	if _, err := New(make([]byte, 32)); err != nil {
		t.Errorf("32 bytes: %v", err)
	}
}

func TestTheKeyIsMadeOnceInTheCluster(t *testing.T) {
	ctx := context.Background()
	cs := k8sfake.NewSimpleClientset()
	box, err := LoadOrCreate(ctx, cs, "shpyrd-system")
	if err != nil {
		t.Fatal(err)
	}
	sec, err := cs.CoreV1().Secrets("shpyrd-system").Get(ctx, SecretName, metav1.GetOptions{})
	if err != nil || len(sec.Data[KeyName]) != 32 || sec.Labels["app.kubernetes.io/managed-by"] != "shpyrd" {
		t.Fatalf("secret %+v %v", sec, err)
	}
	sealed, _ := box.Seal("x", "r")
	again, err := LoadOrCreate(ctx, cs, "shpyrd-system")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := again.Open(sealed, "r"); err != nil || got != "x" {
		t.Errorf("the key read back opens nothing: %q %v", got, err)
	}
	if list, _ := cs.CoreV1().Secrets("shpyrd-system").List(ctx, metav1.ListOptions{}); len(list.Items) != 1 {
		t.Errorf("%d secrets", len(list.Items))
	}
}

func TestABrokenKeySecretIsAnError(t *testing.T) {
	cs := k8sfake.NewSimpleClientset(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: SecretName, Namespace: "shpyrd-system"},
		Data: map[string][]byte{KeyName: []byte("short")}})
	if _, err := LoadOrCreate(context.Background(), cs, "shpyrd-system"); err == nil || !strings.Contains(err.Error(), SecretName) {
		t.Errorf("err = %v", err)
	}
}
```

Create `pkg/api/github_link_test.go`:

```go
package api

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/shpyrd-io/shpyrd/pkg/tokenseal"
)

func TestTheServerMakesTheTokenKey(t *testing.T) {
	s, _, _ := githubServer(t)
	if s.githubBox == nil {
		t.Fatal("no token key")
	}
	sec, err := s.kube.Kube.CoreV1().Secrets("shpyrd-system").Get(context.Background(), tokenseal.SecretName, metav1.GetOptions{})
	if err != nil || len(sec.Data[tokenseal.KeyName]) != 32 {
		t.Fatalf("secret %v", err)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `mkdir -p /home/nkr/.cache/gotmp && GOTMPDIR=/home/nkr/.cache/gotmp TMPDIR=/home/nkr/.cache/gotmp go test ./pkg/tokenseal/ ./pkg/api/ -run 'Sealed|ThirtyTwo|MadeOnce|BrokenKey|MakesTheTokenKey'`
Expected: FAIL to compile: `package github.com/shpyrd-io/shpyrd/pkg/tokenseal` has no Go files, `s.githubBox undefined`.

- [ ] **Step 3: The package**

`pkg/tokenseal/tokenseal.go`:

```go
// Package tokenseal seals the secrets shpyrd keeps in its database (a
// person's GitHub refresh token, RFC-0054): AES-256-GCM, under a key in
// the cluster Secret shpyrd-token-key, which the server makes when it is
// absent. A sealed value names the row it belongs to (its additional
// data), so a value copied to another row does not open. A stolen database
// still needs the key; rotating the key means people link again.
package tokenseal

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// SecretName is the key's Secret in the system namespace.
	SecretName = "shpyrd-token-key"
	// KeyName is the key's entry in it: 32 random bytes.
	KeyName = "key"
	keySize = 32
	prefix  = "v1."
)

// ErrCannotOpen: the value was sealed under another key, for another row,
// or is no sealed value.
var ErrCannotOpen = errors.New("the sealed value does not open with this key: it was sealed under another key, or for another row")

// Box seals and opens with one key.
type Box struct{ aead cipher.AEAD }

// New is a box of a 32-byte key.
func New(key []byte) (*Box, error) {
	if len(key) != keySize {
		return nil, fmt.Errorf("the token key must be %d bytes, not %d", keySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Generate is a box of a fresh key: tests, and runs without a cluster.
func Generate() (*Box, error) {
	key, err := newKey()
	if err != nil {
		return nil, err
	}
	return New(key)
}

func newKey() ([]byte, error) {
	key := make([]byte, keySize)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return key, nil
}

// Row names what a value belongs to, as its additional data.
func Row(parts ...string) string { return strings.Join(parts, "\x00") }

// Seal encrypts plain for row: "v1." and base64 of the nonce and the
// ciphertext.
func (b *Box) Seal(plain, row string) (string, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return prefix + base64.RawStdEncoding.EncodeToString(b.aead.Seal(nonce, nonce, []byte(plain), []byte(row))), nil
}

// Open decrypts a value sealed for row; ErrCannotOpen when it does not.
func (b *Box) Open(sealed, row string) (string, error) {
	raw, ok := strings.CutPrefix(sealed, prefix)
	if !ok {
		return "", ErrCannotOpen
	}
	data, err := base64.RawStdEncoding.DecodeString(raw)
	n := b.aead.NonceSize()
	if err != nil || len(data) < n {
		return "", ErrCannotOpen
	}
	plain, err := b.aead.Open(nil, data[:n], data[n:], []byte(row))
	if err != nil {
		return "", ErrCannotOpen
	}
	return string(plain), nil
}

// LoadOrCreate reads the key's Secret, making it first when it is absent.
// Another replica making it at the same time wins, and its key is read.
func LoadOrCreate(ctx context.Context, kube kubernetes.Interface, namespace string) (*Box, error) {
	sec, err := kube.CoreV1().Secrets(namespace).Get(ctx, SecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		key, kerr := newKey()
		if kerr != nil {
			return nil, kerr
		}
		sec = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: SecretName, Namespace: namespace, Labels: map[string]string{"app.kubernetes.io/managed-by": "shpyrd"}},
			Type:       corev1.SecretTypeOpaque,
			Data:       map[string][]byte{KeyName: key},
		}
		created, cerr := kube.CoreV1().Secrets(namespace).Create(ctx, sec, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(cerr) {
			return LoadOrCreate(ctx, kube, namespace)
		}
		if cerr != nil {
			return nil, fmt.Errorf("could not create the Secret %s: %w", SecretName, cerr)
		}
		sec = created
	} else if err != nil {
		return nil, fmt.Errorf("could not read the Secret %s: %w", SecretName, err)
	}
	box, err := New(sec.Data[KeyName])
	if err != nil {
		return nil, fmt.Errorf("the Secret %s: %w", SecretName, err)
	}
	return box, nil
}
```

- [ ] **Step 4: The server holds the box**

In `pkg/api/server.go`, add the import `"github.com/shpyrd-io/shpyrd/pkg/tokenseal"`, and in `Server`, after `githubDeliveries *rateLimiter`:

```go
	// githubBox seals the people's GitHub refresh tokens (RFC-0054): its
	// key is the Secret shpyrd-token-key, made at start when absent.
	githubBox *tokenseal.Box
```

In `newServer`, right after `s.githubApp = &github.Source{…}`:

```go
	if kubeIface != nil {
		box, err := tokenseal.LoadOrCreate(context.Background(), kubeIface, systemNS)
		if err != nil {
			return nil, fmt.Errorf("token key: %w", err)
		}
		s.githubBox = box
	} else {
		box, err := tokenseal.Generate()
		if err != nil {
			return nil, err
		}
		s.githubBox = box
	}
```

- [ ] **Step 5: Run the tests**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp TMPDIR=/home/nkr/.cache/gotmp go build ./... && GOTMPDIR=/home/nkr/.cache/gotmp TMPDIR=/home/nkr/.cache/gotmp go test ./pkg/tokenseal/ ./pkg/api/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/tokenseal pkg/api/server.go pkg/api/github_link_test.go
git commit -m "feat(tokenseal): the token key, a Secret the server makes, and AES-256-GCM sealing bound to the row

Co-Authored-By: <your model name> <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 2: The store's GitHub links

**Files:**
- Create: `pkg/store/migrations/000029_github_user_links.up.sql`, `pkg/store/migrations/000029_github_user_links.down.sql`
- Modify: `pkg/store/github.go` (type and the `GitHub` interface), `pkg/store/github_memory.go`, `pkg/store/github_postgres.go`, `pkg/store/memory.go` (`Memory` field `githubLinks`, after `github` ~52)
- Test: `pkg/store/github_test.go`, `pkg/store/store_test.go` (`dropAll`'s list, line 44)

**Interfaces:**
- Consumes: nothing new.
- Produces:
  - `type store.GitHubUserLink struct { Provider, Subject string; GitHubUserID int64; GitHubLogin string; RefreshToken string; RefreshExpiresAt, LinkedAt, UpdatedAt time.Time }` (`RefreshToken` is `json:"-"`, sealed)
  - `store.GitHub` gains `PutGitHubUserLink(ctx, l GitHubUserLink) error`, `GitHubUserLink(ctx, provider, subject string) (*GitHubUserLink, error)` (`ErrNotFound`), `DeleteGitHubUserLink(ctx, provider, subject string) error` (`ErrNotFound`)

- [ ] **Step 1: Write the failing test**

Append to `pkg/store/github_test.go` (add `"time"` to its imports):

```go
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
```

In `pkg/store/store_test.go`, `dropAll`'s list starts `"github_user_links", "github_installations", …`.

- [ ] **Step 2: Run it to see it fail**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp TMPDIR=/home/nkr/.cache/gotmp go test ./pkg/store/ -run GitHubUserLinks`
Expected: FAIL to compile: `s.GitHubUserLink undefined`, `undefined: GitHubUserLink`.

- [ ] **Step 3: The migration**

`pkg/store/migrations/000029_github_user_links.up.sql`:

```sql
-- A person's GitHub account, linked through the GitHub App's user
-- authorization (RFC-0054): one row per shpyrd identity (the provider and
-- subject a session or the Connect ticket carries). refresh_token is sealed
-- with AES-256-GCM under the key in the Secret shpyrd-token-key; the access
-- token is never stored.
CREATE TABLE github_user_links (
 provider TEXT NOT NULL,
 subject TEXT NOT NULL,
 github_user_id BIGINT NOT NULL,
 github_login TEXT NOT NULL,
 refresh_token TEXT NOT NULL,
 refresh_expires_at TIMESTAMPTZ NOT NULL,
 linked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY (provider, subject)
);
```

`pkg/store/migrations/000029_github_user_links.down.sql`:

```sql
DROP TABLE IF EXISTS github_user_links;
```

- [ ] **Step 4: The type and the interface**

In `pkg/store/github.go`, after `GitHubInstallation`:

```go
// GitHubUserLink is a person's GitHub account, linked through the GitHub
// App's user authorization (RFC-0054): one per shpyrd identity, its
// provider and subject. RefreshToken is sealed (pkg/tokenseal); the access
// token is never stored.
type GitHubUserLink struct {
	Provider         string    `json:"provider"`
	Subject          string    `json:"subject"`
	GitHubUserID     int64     `json:"githubUserId"`
	GitHubLogin      string    `json:"githubLogin"`
	RefreshToken     string    `json:"-"`
	RefreshExpiresAt time.Time `json:"refreshExpiresAt"`
	LinkedAt         time.Time `json:"linkedAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}
```

At the end of the `GitHub` interface:

```go
	// PutGitHubUserLink links an identity, or links it again: the refresh
	// token rotated, the login renamed, or another GitHub account.
	// LinkedAt stays while the GitHub account (its id) is the same.
	PutGitHubUserLink(ctx context.Context, l GitHubUserLink) error
	// GitHubUserLink is the identity's link; ErrNotFound when it has none.
	GitHubUserLink(ctx context.Context, provider, subject string) (*GitHubUserLink, error)
	// DeleteGitHubUserLink unlinks; ErrNotFound when there is no link.
	DeleteGitHubUserLink(ctx context.Context, provider, subject string) error
```

- [ ] **Step 5: Memory**

In `pkg/store/memory.go`, `Memory` gains, after `github map[int64]*githubRow`:

```go
	githubLinks map[string]GitHubUserLink
```

Append to `pkg/store/github_memory.go`:

```go
func githubLinkKey(provider, subject string) string { return provider + "\x00" + subject }

func (m *Memory) PutGitHubUserLink(_ context.Context, l GitHubUserLink) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.githubLinks == nil {
		m.githubLinks = map[string]GitHubUserLink{}
	}
	now := m.now()
	key := githubLinkKey(l.Provider, l.Subject)
	l.LinkedAt, l.UpdatedAt = now, now
	if prev, ok := m.githubLinks[key]; ok && prev.GitHubUserID == l.GitHubUserID {
		l.LinkedAt = prev.LinkedAt
	}
	m.githubLinks[key] = l
	return nil
}

func (m *Memory) GitHubUserLink(_ context.Context, provider, subject string) (*GitHubUserLink, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.githubLinks[githubLinkKey(provider, subject)]
	if !ok {
		return nil, ErrNotFound
	}
	return &l, nil
}

func (m *Memory) DeleteGitHubUserLink(_ context.Context, provider, subject string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := githubLinkKey(provider, subject)
	if _, ok := m.githubLinks[key]; !ok {
		return ErrNotFound
	}
	delete(m.githubLinks, key)
	return nil
}
```

- [ ] **Step 6: Postgres**

Append to `pkg/store/github_postgres.go`:

```go
func (p *Postgres) PutGitHubUserLink(ctx context.Context, l GitHubUserLink) error {
	_, err := p.pool.Exec(ctx, `INSERT INTO github_user_links (provider, subject, github_user_id, github_login, refresh_token, refresh_expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (provider, subject) DO UPDATE SET
		 linked_at = CASE WHEN github_user_links.github_user_id = EXCLUDED.github_user_id THEN github_user_links.linked_at ELSE now() END,
		 github_user_id = EXCLUDED.github_user_id, github_login = EXCLUDED.github_login,
		 refresh_token = EXCLUDED.refresh_token, refresh_expires_at = EXCLUDED.refresh_expires_at, updated_at = now()`,
		l.Provider, l.Subject, l.GitHubUserID, l.GitHubLogin, l.RefreshToken, l.RefreshExpiresAt)
	return err
}

func (p *Postgres) GitHubUserLink(ctx context.Context, provider, subject string) (*GitHubUserLink, error) {
	l := GitHubUserLink{Provider: provider, Subject: subject}
	err := p.pool.QueryRow(ctx, `SELECT github_user_id, github_login, refresh_token, refresh_expires_at, linked_at, updated_at
		FROM github_user_links WHERE provider = $1 AND subject = $2`, provider, subject).
		Scan(&l.GitHubUserID, &l.GitHubLogin, &l.RefreshToken, &l.RefreshExpiresAt, &l.LinkedAt, &l.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

func (p *Postgres) DeleteGitHubUserLink(ctx context.Context, provider, subject string) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM github_user_links WHERE provider = $1 AND subject = $2`, provider, subject)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
```

- [ ] **Step 7: Run the tests**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp TMPDIR=/home/nkr/.cache/gotmp go build ./... && GOTMPDIR=/home/nkr/.cache/gotmp TMPDIR=/home/nkr/.cache/gotmp go test ./pkg/store/`
Expected: PASS (memory). With a database: `SHPYRD_TEST_DATABASE_URL=<url> GOTMPDIR=/home/nkr/.cache/gotmp TMPDIR=/home/nkr/.cache/gotmp go test ./pkg/store/ -run 'GitHub'` passes too. `go build ./...` finds every other `store.Store` wrapper (`go vet ./...` lists any that embeds the interface without `*Memory`); they get the three methods by delegation.

- [ ] **Step 8: Commit**

```bash
git add pkg/store
git commit -m "feat(store): github_user_links, one GitHub account per identity, the refresh token sealed

Co-Authored-By: <your model name> <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 3: A person's GitHub token: the calls, and the fake's people

**Files:**
- Create: `pkg/github/user.go`, `pkg/github/user_test.go`, `pkg/github/githubtest/users.go`
- Modify: `pkg/github/client.go` (`ExchangeCode`, `Repositories`, `Branches`, `UserLogin`, `call`, `send`), `pkg/github/githubtest/fake.go` (`Server` fields, `NewServer`, `SetUserLogin`, `serve`, `branchList`, `exchange`, `userInstallations`, `user`, `membership`)

**Interfaces:**
- Consumes: nothing new.
- Produces:
  - `type github.UserToken struct { Access string; AccessExpiresAt time.Time; Refresh string; RefreshExpiresAt time.Time }`
  - `type github.User struct { ID int64; Login string }`
  - `var github.ErrNoRefreshToken`, `github.ErrUserTokenRefused`
  - `func (*Client) ExchangeUserCode(ctx, code string) (UserToken, error)`, `RefreshUserToken(ctx, refresh string) (UserToken, error)`
  - `func (*Client) UserProfile(ctx, userToken string) (User, error)`
  - `func (*Client) UserRepositories(ctx, userToken string, installation int64) ([]Repository, error)`
  - `func (*Client) UserRepository(ctx, userToken, owner, repo string) (Repository, error)`, `RepositoryAsApp(ctx, installation int64, owner, repo string) (Repository, error)`
  - `func (*Client) UserBranches(ctx, userToken, owner, repo string) ([]string, error)`
  - `func (*Client) RevokeUserToken(ctx, access string) error`
  - `func (*Client) callHeaders(ctx, method, endpoint, bearer string, in, out any) (http.Header, error)` (Task 9 uses it)
  - fake: `const githubtest.UserID = 1001`; `type githubtest.User struct { ID int64; Login string; Installations []int64; Repos []string }`; `(*Server).AddUser(code string, u User)`, `SetExpiringUserTokens(on bool)`, `ExpireUserTokens()`, `ForgetRefreshTokens()`, `Revoked() []string`, `SetPublicRepos(repos ...Repo)`. The default person's code `Code` gives the access token `UserToken` and a refresh token `ghr_default_<n>`; a refresh gives `ghu_<handle>_<n>` and `ghr_<handle>_<n>`.

- [ ] **Step 1: Write the failing tests**

`pkg/github/user_test.go`:

```go
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
```

- [ ] **Step 2: Run them to see them fail**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp TMPDIR=/home/nkr/.cache/gotmp go test ./pkg/github/ -run 'ALinkExchanges|ReadsOnlyWhat|TheAppReads|UnlinkRevokes'`
Expected: FAIL to compile: `c.ExchangeUserCode undefined`, `undefined: githubtest.User`.

- [ ] **Step 3: The client's shared pieces**

In `pkg/github/client.go`, replace `call` and `send` with:

```go
// call sends a JSON request to GitHub's API.
func (c *Client) call(ctx context.Context, method, endpoint, bearer string, in, out any) error {
	_, err := c.callHeaders(ctx, method, endpoint, bearer, in, out)
	return err
}

// callHeaders is call, with the answer's headers: a page's Link.
func (c *Client) callHeaders(ctx context.Context, method, endpoint, bearer string, in, out any) (http.Header, error) {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
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
	return c.sendHeaders(req, out)
}

// send makes the call and reads the answer; a status that is not 2xx is
// an APIError with GitHub's words.
func (c *Client) send(req *http.Request, out any) error {
	_, err := c.sendHeaders(req, out)
	return err
}

func (c *Client) sendHeaders(req *http.Request, out any) (http.Header, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		var e struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &e)
		apiErr := &APIError{Status: resp.StatusCode, Message: firstNonEmpty(e.Message, http.StatusText(resp.StatusCode)), RetryAfter: c.retryAfter(resp.Header)}
		apiErr.RateLimited = resp.StatusCode == http.StatusTooManyRequests || apiErr.RetryAfter > 0 ||
			(resp.StatusCode == http.StatusForbidden && (resp.Header.Get("X-RateLimit-Remaining") == "0" || strings.Contains(strings.ToLower(e.Message), "rate limit")))
		return resp.Header, apiErr
	}
	if out == nil || len(raw) == 0 {
		return resp.Header, nil
	}
	return resp.Header, json.Unmarshal(raw, out)
}
```

Replace `Repositories` and `Branches` with:

```go
// Repositories pages through everything the installation grants.
func (c *Client) Repositories(ctx context.Context, installation int64) ([]Repository, error) {
	tok, err := c.InstallationToken(ctx, installation, "", ContentsRead())
	if err != nil {
		return nil, err
	}
	return c.repositoryPages(ctx, c.api+"/installation/repositories", tok.Value, installation)
}

// repositoryPages pages through a list of an installation's repositories:
// the installation's own, or what a person reads of it.
func (c *Client) repositoryPages(ctx context.Context, endpoint, token string, installation int64) ([]Repository, error) {
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
		if err := c.call(ctx, "GET", fmt.Sprintf("%s?per_page=%d&page=%d", endpoint, perPage, page), token, nil, &res); err != nil {
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
	tok, err := c.InstallationToken(ctx, installation, repo, ContentsRead())
	if err != nil {
		return nil, err
	}
	return c.branches(ctx, tok.Value, owner, repo)
}

func (c *Client) branches(ctx context.Context, token, owner, repo string) ([]string, error) {
	out := []string{}
	for page := 1; page <= 10; page++ {
		var res []struct {
			Name string `json:"name"`
		}
		endpoint := fmt.Sprintf("%s/repos/%s/%s/branches?per_page=%d&page=%d", c.api, url.PathEscape(owner), url.PathEscape(repo), perPage, page)
		if err := c.call(ctx, "GET", endpoint, token, nil, &res); err != nil {
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
```

Replace `ExchangeCode` and `UserLogin` with:

```go
// ExchangeCode turns the code GitHub gave the browser into the person's
// own access token, which Connect only asks who they are with: the caller
// drops it once it has asked.
func (c *Client) ExchangeCode(ctx context.Context, code string) (string, error) {
	t, err := c.ExchangeUserCode(ctx, code)
	return t.Access, err
}

// UserLogin is the login of the person behind userToken (GET /user).
func (c *Client) UserLogin(ctx context.Context, userToken string) (string, error) {
	u, err := c.UserProfile(ctx, userToken)
	return u.Login, err
}
```

- [ ] **Step 4: The user calls**

`pkg/github/user.go`:

```go
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// A person's GitHub App user token (RFC-0054, GitHub access by person):
// it reads what both the person and the App may, nothing more. shpyrd
// keeps the refresh token sealed in its database and the access token in
// memory; both rotate on every refresh.

// UserToken is GitHub's answer to an exchange or a refresh.
type UserToken struct {
	Access           string
	AccessExpiresAt  time.Time
	Refresh          string
	RefreshExpiresAt time.Time
}

// User is who a user token belongs to.
type User struct {
	ID    int64
	Login string
}

var (
	// ErrNoRefreshToken: the App's user tokens do not expire, so GitHub
	// gives no refresh token, and shpyrd cannot act for the person later.
	ErrNoRefreshToken = errors.New("GitHub gave no refresh token: the GitHub App's user tokens do not expire. An owner of the App turns on “Expire user authorization tokens” in the App's settings on GitHub (Optional features), then you link again")
	// ErrUserTokenRefused: GitHub no longer takes the refresh token: it
	// expired after six months unused, was used already, or the person
	// revoked the App's authorization.
	ErrUserTokenRefused = errors.New("GitHub no longer accepts this GitHub authorization: it expired or was revoked")
)

// ExchangeUserCode turns the code GitHub gave the browser into the
// person's tokens.
func (c *Client) ExchangeUserCode(ctx context.Context, code string) (UserToken, error) {
	return c.userToken(ctx, map[string]string{"code": code})
}

// RefreshUserToken trades a refresh token, once, for new tokens.
func (c *Client) RefreshUserToken(ctx context.Context, refresh string) (UserToken, error) {
	return c.userToken(ctx, map[string]string{"grant_type": "refresh_token", "refresh_token": refresh})
}

func (c *Client) userToken(ctx context.Context, grant map[string]string) (UserToken, error) {
	grant["client_id"], grant["client_secret"] = c.app.ClientID, c.app.ClientSecret
	raw, _ := json.Marshal(grant)
	req, err := http.NewRequestWithContext(ctx, "POST", c.web+"/login/oauth/access_token", bytes.NewReader(raw))
	if err != nil {
		return UserToken{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "shpyrd")
	var res struct {
		AccessToken      string `json:"access_token"`
		ExpiresIn        int64  `json:"expires_in"`
		RefreshToken     string `json:"refresh_token"`
		RefreshExpiresIn int64  `json:"refresh_token_expires_in"`
		Error            string `json:"error"`
		Description      string `json:"error_description"`
	}
	if err := c.send(req, &res); err != nil {
		return UserToken{}, err
	}
	switch {
	case res.Error == "bad_refresh_token":
		return UserToken{}, ErrUserTokenRefused
	case res.AccessToken == "":
		return UserToken{}, fmt.Errorf("GitHub did not accept the sign-in: %s", firstNonEmpty(res.Description, res.Error, "it gave no token"))
	}
	now := c.now()
	t := UserToken{Access: res.AccessToken, Refresh: res.RefreshToken}
	if res.ExpiresIn > 0 {
		t.AccessExpiresAt = now.Add(time.Duration(res.ExpiresIn) * time.Second)
	}
	if res.RefreshExpiresIn > 0 {
		t.RefreshExpiresAt = now.Add(time.Duration(res.RefreshExpiresIn) * time.Second)
	}
	return t, nil
}

// UserProfile is who the person behind userToken is (GET /user).
func (c *Client) UserProfile(ctx context.Context, userToken string) (User, error) {
	var res struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	}
	if err := c.call(ctx, "GET", c.api+"/user", userToken, nil, &res); err != nil {
		return User{}, err
	}
	if res.Login == "" {
		return User{}, errors.New("GitHub did not say who the person is")
	}
	return User{ID: res.ID, Login: res.Login}, nil
}

// UserRepositories are the repositories of an installation that the
// person behind userToken can read (GET
// /user/installations/{id}/repositories); a 404 APIError for an
// installation they cannot see.
func (c *Client) UserRepositories(ctx context.Context, userToken string, installation int64) ([]Repository, error) {
	return c.repositoryPages(ctx, fmt.Sprintf("%s/user/installations/%d/repositories", c.api, installation), userToken, installation)
}

// UserRepository is a repository as the person behind userToken reads it
// (GET /repos/{owner}/{repo}): a 404 or 403 APIError when they cannot.
func (c *Client) UserRepository(ctx context.Context, userToken, owner, repo string) (Repository, error) {
	return c.repository(ctx, userToken, owner, repo)
}

// RepositoryAsApp is a repository as the installation reads it, with a
// token for the whole installation: one it includes, or any public one. A
// 404 APIError for a private one it does not include.
func (c *Client) RepositoryAsApp(ctx context.Context, installation int64, owner, repo string) (Repository, error) {
	tok, err := c.InstallationToken(ctx, installation, "", ContentsRead())
	if err != nil {
		return Repository{}, err
	}
	r, err := c.repository(ctx, tok.Value, owner, repo)
	r.Installation = installation
	return r, err
}

func (c *Client) repository(ctx context.Context, token, owner, repo string) (Repository, error) {
	var res struct {
		FullName      string `json:"full_name"`
		Private       bool   `json:"private"`
		DefaultBranch string `json:"default_branch"`
	}
	if err := c.call(ctx, "GET", fmt.Sprintf("%s/repos/%s/%s", c.api, url.PathEscape(owner), url.PathEscape(repo)), token, nil, &res); err != nil {
		return Repository{}, err
	}
	return Repository{FullName: res.FullName, Private: res.Private, DefaultBranch: res.DefaultBranch}, nil
}

// UserBranches are a repository's branches as the person reads them.
func (c *Client) UserBranches(ctx context.Context, userToken, owner, repo string) ([]string, error) {
	return c.branches(ctx, userToken, owner, repo)
}

// RevokeUserToken revokes a person's user token at GitHub (DELETE
// /applications/{client_id}/token, the App's client id and secret as Basic
// credentials). A token GitHub no longer knows (404) is no error.
func (c *Client) RevokeUserToken(ctx context.Context, access string) error {
	raw, _ := json.Marshal(map[string]string{"access_token": access})
	req, err := http.NewRequestWithContext(ctx, "DELETE", c.api+"/applications/"+url.PathEscape(c.app.ClientID)+"/token", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.app.ClientID, c.app.ClientSecret)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "shpyrd")
	if err := c.send(req, nil); err != nil && !IsNotFound(err) {
		return err
	}
	return nil
}
```

- [ ] **Step 5: The fake's people**

`pkg/github/githubtest/users.go`:

```go
package githubtest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// People on the fake GitHub (RFC-0054, GitHub access by person): the
// default person (Code gives UserToken; UserLogin, UserID) and those
// AddUser adds, the GitHub App user tokens their codes give, refreshed,
// expired and revoked as GitHub does, and what each may read.

const (
	// UserID is the default person's GitHub id.
	UserID      = 1001
	defaultUser = "default"
)

// User is a person on the fake GitHub. Installations are the ids, among
// SetUserInstallations' list, they see (nil: all of it); Repos are the
// full names they can read in those installations (nil: every one).
type User struct {
	ID            int64
	Login         string
	Installations []int64
	Repos         []string
}

// userGrant is a user access token: whose, and until when.
type userGrant struct {
	handle string
	exp    time.Time
}

// AddUser makes a person whose OAuth code is code.
func (f *Server) AddUser(code string, u User) {
	f.mu.Lock()
	defer f.mu.Unlock()
	handle := strings.ToLower(u.Login)
	f.users[handle] = &u
	f.codes[code] = handle
}

// SetExpiringUserTokens says whether the App's user tokens expire, as
// GitHub's default is: off, an exchange gives no refresh token.
func (f *Server) SetExpiringUserTokens(on bool) { f.mu.Lock(); f.noExpiring = !on; f.mu.Unlock() }

// ExpireUserTokens makes every user access token issued so far expired.
func (f *Server) ExpireUserTokens() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for tok, g := range f.access {
		g.exp = f.clock()
		f.access[tok] = g
	}
}

// ForgetRefreshTokens refuses every refresh token issued so far, as GitHub
// does after six months unused or once the person revoked the App.
func (f *Server) ForgetRefreshTokens() { f.mu.Lock(); f.refresh = map[string]string{}; f.mu.Unlock() }

// Revoked are the user tokens revoked through
// DELETE /applications/{client_id}/token, in order.
func (f *Server) Revoked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.revoked...)
}

// SetPublicRepos are public repositories outside the installations:
// anyone reads them.
func (f *Server) SetPublicRepos(repos ...Repo) { f.mu.Lock(); f.public = repos; f.mu.Unlock() }

// userOf is the person whose live user token the request carries.
func (f *Server) userOf(r *http.Request) (*User, bool) {
	g, ok := f.access[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
	if !ok || !f.clock().Before(g.exp) {
		return nil, false
	}
	u, ok := f.users[g.handle]
	return u, ok
}

func (f *Server) installationsOf(u *User) []Installation {
	if u.Installations == nil {
		return f.people
	}
	var out []Installation
	for _, i := range f.people {
		if slices.Contains(u.Installations, i.ID) {
			out = append(out, i)
		}
	}
	return out
}

func (f *Server) sees(u *User, id int64) bool {
	return slices.ContainsFunc(f.installationsOf(u), func(i Installation) bool { return i.ID == id })
}

func readsRepo(u *User, fullName string) bool {
	return u.Repos == nil || slices.ContainsFunc(u.Repos, func(n string) bool { return strings.EqualFold(n, fullName) })
}

func repoNamed(repos []Repo, fullName string) (Repo, bool) {
	for _, r := range repos {
		if strings.EqualFold(r.FullName, fullName) {
			return r, true
		}
	}
	return Repo{}, false
}

// publicRepo is a public repository: one SetPublicRepos named, or one an
// installation has that is not private.
func (f *Server) publicRepo(fullName string) (Repo, bool) {
	if r, ok := repoNamed(f.public, fullName); ok {
		return r, true
	}
	for _, repos := range f.repos {
		if r, ok := repoNamed(repos, fullName); ok && !r.Private {
			return r, true
		}
	}
	return Repo{}, false
}

// readable is the repository as the request's token reads it: an
// installation token, one its installation has; a user token, one of an
// installation they see that they may read; either, a public one.
func (f *Server) readable(r *http.Request, fullName string) (Repo, bool) {
	if g, ok := f.tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]; ok {
		if repo, ok := repoNamed(f.repos[g.installation], fullName); ok {
			return repo, true
		}
		return f.publicRepo(fullName)
	}
	u, ok := f.userOf(r)
	if !ok {
		return Repo{}, false
	}
	for _, i := range f.installationsOf(u) {
		if repo, ok := repoNamed(f.repos[i.ID], fullName); ok && readsRepo(u, fullName) {
			return repo, true
		}
	}
	return f.publicRepo(fullName)
}

// exchange is POST /login/oauth/access_token: a code, or a refresh token
// taken once.
func (f *Server) exchange(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		Code         string `json:"code"`
		GrantType    string `json:"grant_type"`
		RefreshToken string `json:"refresh_token"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	if in.ClientID != ClientID || in.ClientSecret != ClientSecret {
		reply(w, http.StatusOK, map[string]string{"error": "incorrect_client_credentials", "error_description": "The client_id and/or client_secret passed are incorrect."})
		return
	}
	var handle string
	if in.GrantType == "refresh_token" {
		h, ok := f.refresh[in.RefreshToken]
		if !ok {
			reply(w, http.StatusOK, map[string]string{"error": "bad_refresh_token", "error_description": "The refresh token passed is incorrect or expired."})
			return
		}
		delete(f.refresh, in.RefreshToken)
		handle = h
	} else {
		h, ok := f.codes[in.Code]
		if !ok {
			reply(w, http.StatusOK, map[string]string{"error": "bad_verification_code", "error_description": "The code passed is incorrect or expired."})
			return
		}
		handle = h
	}
	f.issued++
	access := fmt.Sprintf("ghu_%s_%d", handle, f.issued)
	if handle == defaultUser && in.GrantType == "" {
		access = UserToken
	}
	f.access[access] = userGrant{handle: handle, exp: f.clock().Add(8 * time.Hour)}
	out := map[string]any{"access_token": access, "token_type": "bearer"}
	if !f.noExpiring {
		refresh := fmt.Sprintf("ghr_%s_%d", handle, f.issued)
		f.refresh[refresh] = handle
		out["expires_in"], out["refresh_token"], out["refresh_token_expires_in"] = 28800, refresh, 15897600
	}
	reply(w, http.StatusOK, out)
}

func (f *Server) userInstallations(w http.ResponseWriter, r *http.Request) {
	u, ok := f.userOf(r)
	if !ok {
		reply(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
		return
	}
	mine := f.installationsOf(u)
	var out []map[string]any
	for _, i := range pageOf(mine, r) {
		var suspended any
		if i.Suspended {
			suspended = f.clock().UTC().Format(time.RFC3339)
		}
		out = append(out, map[string]any{"id": i.ID, "account": map[string]string{"login": i.Login, "type": i.Type}, "suspended_at": suspended})
	}
	reply(w, http.StatusOK, map[string]any{"total_count": len(mine), "installations": out})
}

// userInstallationRepos is GET /user/installations/{id}/repositories.
func (f *Server) userInstallationRepos(w http.ResponseWriter, r *http.Request, idText string) {
	u, ok := f.userOf(r)
	if !ok {
		reply(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
		return
	}
	id, _ := strconv.ParseInt(idText, 10, 64)
	if !f.sees(u, id) {
		reply(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	var mine []Repo
	for _, repo := range f.repos[id] {
		if readsRepo(u, repo.FullName) {
			mine = append(mine, repo)
		}
	}
	var out []map[string]any
	for _, repo := range pageOf(mine, r) {
		out = append(out, map[string]any{"full_name": repo.FullName, "private": repo.Private, "default_branch": repo.DefaultBranch})
	}
	reply(w, http.StatusOK, map[string]any{"total_count": len(mine), "repositories": out})
}

func (f *Server) user(w http.ResponseWriter, r *http.Request) {
	u, ok := f.userOf(r)
	if !ok {
		reply(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
		return
	}
	reply(w, http.StatusOK, map[string]any{"login": u.Login, "id": u.ID, "type": "User"})
}

// repoInfo is GET /repos/{owner}/{repo}.
func (f *Server) repoInfo(w http.ResponseWriter, r *http.Request, fullName string) {
	repo, ok := f.readable(r, fullName)
	if !ok {
		reply(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	reply(w, http.StatusOK, map[string]any{"full_name": repo.FullName, "private": repo.Private, "default_branch": repo.DefaultBranch})
}

// revoke is DELETE /applications/{client_id}/token.
func (f *Server) revoke(w http.ResponseWriter, r *http.Request) {
	id, secret, ok := r.BasicAuth()
	if !ok || id != ClientID || secret != ClientSecret {
		reply(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
		return
	}
	var in struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	if _, ok := f.access[in.AccessToken]; !ok {
		reply(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	delete(f.access, in.AccessToken)
	f.revoked = append(f.revoked, in.AccessToken)
	w.WriteHeader(http.StatusNoContent)
}
```

In `pkg/github/githubtest/fake.go`:

- `Server`: remove `login string`; add after `people []Installation`:

```go
	users      map[string]*User     // by handle: the login in lower case, "default" for the default person
	codes      map[string]string    // OAuth code -> handle
	access     map[string]userGrant // user access token -> whose, until when
	refresh    map[string]string    // live refresh token -> handle
	revoked    []string
	issued     int
	noExpiring bool
	public     []Repo
```

- `NewServer`: replace `login: UserLogin,` with

```go
		users:   map[string]*User{defaultUser: {ID: UserID, Login: UserLogin}},
		codes:   map[string]string{Code: defaultUser},
		access:  map[string]userGrant{},
		refresh: map[string]string{},
```

- `SetUserLogin`: `func (f *Server) SetUserLogin(login string) { f.mu.Lock(); f.users[defaultUser].Login = login; f.mu.Unlock() }`.
- Delete the old `exchange`, `userInstallations` and `user` (now in `users.go`).
- `membership`: its first check becomes `if _, ok := f.userOf(r); !ok {` (same 401 answer).
- `branchList` becomes:

```go
func (f *Server) branchList(w http.ResponseWriter, r *http.Request, fullName string) {
	allowed := false
	if id, ok := f.installationOf(r); ok {
		allowed = grants(f.repos[id], fullName[strings.Index(fullName, "/")+1:])
	} else if _, ok := f.userOf(r); ok {
		_, allowed = f.readable(r, fullName)
	}
	if !allowed {
		reply(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	out := []map[string]string{}
	for _, b := range pageOf(f.branches[strings.ToLower(fullName)], r) {
		out = append(out, map[string]string{"name": b})
	}
	reply(w, http.StatusOK, out)
}
```

- `serve`: before the `case r.Method == "GET" && strings.HasPrefix(p, "/repos/") && strings.HasSuffix(p, "/branches"):` line, add

```go
	case r.Method == "GET" && strings.HasPrefix(p, "/repos/") && strings.Count(p, "/") == 3:
		f.repoInfo(w, r, strings.TrimPrefix(p, "/repos/"))
	case r.Method == "GET" && strings.HasPrefix(p, "/user/installations/") && strings.HasSuffix(p, "/repositories"):
		f.userInstallationRepos(w, r, strings.TrimSuffix(strings.TrimPrefix(p, "/user/installations/"), "/repositories"))
	case r.Method == "DELETE" && p == "/applications/"+ClientID+"/token":
		f.revoke(w, r)
```

- The package comment adds: "people with GitHub App user tokens (exchanged, refreshed, revoked) and what each may read".

- [ ] **Step 6: Run the tests**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp TMPDIR=/home/nkr/.cache/gotmp go build ./... && GOTMPDIR=/home/nkr/.cache/gotmp TMPDIR=/home/nkr/.cache/gotmp go test ./pkg/github/... ./pkg/api/ ./internal/controller/`
Expected: PASS, the Connect tests (`pkg/api/github_test.go`) unchanged: `Code` still gives `UserToken`, `SetUserLogin` still renames the default person.

- [ ] **Step 7: Commit**

```bash
git add pkg/github
git commit -m "feat(github): a person's user token: exchange, refresh, profile, what they read, revoke; the fake's people

Co-Authored-By: <your model name> <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 4: Linking a GitHub account: the API

**Files:**
- Create: `pkg/api/github_link.go`
- Modify: `pkg/api/github.go` (`githubState`, `readGitHubState`, `githubInstalled`), `pkg/api/tokens.go` (`identifyWithToken`), `pkg/api/server.go` (`Server` field `githubUsers`; `newServer`; routes after `api.GET("/me", s.me)` ~648 and beside the finish route ~700)
- Test: `pkg/api/github_link_test.go`

**Interfaces:**
- Consumes: `tokenseal.Box`, `tokenseal.Row` (Task 1); `store.GitHubUserLink` and its methods (Task 2); `github.Client.ExchangeUserCode`, `RefreshUserToken`, `UserProfile`, `RevokeUserToken`, `github.ErrNoRefreshToken`, `github.ErrUserTokenRefused`, `githubtest.AddUser`, `UserID`, `Revoked`, `ExpireUserTokens`, `ForgetRefreshTokens`, `SetExpiringUserTokens` (Task 3).
- Produces:
  - `type api.GitHubLinkView struct { Login string; LinkedAt time.Time }` (json `login`, `linkedAt`), `type api.GitHubLinkStartView struct { AuthorizeURL string }` (json `authorizeURL`)
  - Routes: `GET /api/me/github` (200 or 404), `POST /api/me/github/link` (body `{"return":"/"}`, 200 `GitHubLinkStartView`), `DELETE /api/me/github` (204), `GET /api/me/github/link?return=` (302 to GitHub, the CLI's), `GET /api/me/github/finish?ticket=` (the page)
  - `const githubLinkRequiredCode = "github_link_required"`, `var errGitHubLinkRequired`, `errGitHubNoPerson`
  - `func githubPersonOf(c *gin.Context) (provider, subject string, ok bool)`
  - `func githubLinkRow(provider, subject string) string`
  - `func (s *Server) githubAccessToken(ctx context.Context, cl *github.Client, link *store.GitHubUserLink) (string, error)`
  - `func (s *Server) githubPersonToken(c *gin.Context, cl *github.Client) (token, login string, err error)`
  - `func githubLinkAnswer(c *gin.Context, err error)` (409 with `"code":"github_link_required"`, else 502)
  - `githubState.Link bool` (json `link`), `githubState.Return string` (json `ret`)
  - `const tokenOwnerKey = "shpyrd.tokenOwner"` (the token's maker, an `ext.Identity`, on the request)
  - Test helpers (in `github_link_test.go`): `githubPerson(t, s, email, role string) (sid, csrf, subject string)`, `linkTicket(t, s, sid, csrf, code string) string`, `linkAs(t, s, sid, csrf, code string) *httptest.ResponseRecorder`, `doBearer(t, s, method, path, body, token string) *httptest.ResponseRecorder`, `personToken(t, s, sid, csrf string) string`, `exchanges(fake *githubtest.Server) int`

- [ ] **Step 1: Write the failing tests**

Replace `pkg/api/github_link_test.go` with (it keeps Task 1's test):

```go
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/github/githubtest"
	"github.com/shpyrd-io/shpyrd/pkg/store"
	"github.com/shpyrd-io/shpyrd/pkg/tokenseal"
)

func TestTheServerMakesTheTokenKey(t *testing.T) {
	s, _, _ := githubServer(t)
	if s.githubBox == nil {
		t.Fatal("no token key")
	}
	sec, err := s.kube.Kube.CoreV1().Secrets("shpyrd-system").Get(context.Background(), tokenseal.SecretName, metav1.GetOptions{})
	if err != nil || len(sec.Data[tokenseal.KeyName]) != 32 {
		t.Fatalf("secret %v", err)
	}
}

// githubPerson signs a person in, with a role in the default workspace
// (store.WorkspaceRoleMember or store.WorkspaceRoleAdmin), and returns
// their session, CSRF token and subject.
func githubPerson(t *testing.T, s *Server, email, role string) (string, string, string) {
	t.Helper()
	ctx := context.Background()
	p, err := s.store.TouchIdentity(ctx, store.DefaultWorkspace, store.Identity{Email: email, Provider: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.PutMembership(ctx, store.DefaultWorkspace, p.ID, role); err != nil {
		t.Fatal(err)
	}
	s.authz.Invalidate()
	sid, csrf := signIn(t, s, ext.Identity{Subject: p.ID, Email: email, Provider: "local"})
	return sid, csrf, p.ID
}

// linkTicket starts a link as the person and plays GitHub's callback with
// code: the finish link the callback sends the browser to.
func linkTicket(t *testing.T, s *Server, sid, csrf, code string) string {
	t.Helper()
	rec := doCookie(t, s, "POST", "/api/me/github/link", `{"return":"/"}`, sid, csrf)
	var v GitHubLinkStartView
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &v) != nil {
		t.Fatalf("start: %d %s", rec.Code, rec.Body.String())
	}
	u, err := url.Parse(v.AuthorizeURL)
	if err != nil || u.Path != "/login/oauth/authorize" || u.Query().Get("client_id") != githubtest.ClientID || u.Query().Get("state") == "" {
		t.Fatalf("authorize URL %q", v.AuthorizeURL)
	}
	cb := do(t, s, "GET", "/api/github/installed?"+url.Values{"code": {code}, "state": {u.Query().Get("state")}}.Encode(), "", false)
	loc, err := url.Parse(cb.Header().Get("Location"))
	if cb.Code != http.StatusFound || err != nil || loc.Path != "/api/me/github/finish" || loc.Query().Get("ticket") == "" {
		t.Fatalf("callback: %d %q %s", cb.Code, cb.Header().Get("Location"), cb.Body.String())
	}
	if strings.Contains(loc.RawQuery, code) {
		t.Errorf("GitHub's code is in the clear in the finish link: %s", loc.RawQuery)
	}
	return loc.RequestURI()
}

// linkAs runs the whole link as the person.
func linkAs(t *testing.T, s *Server, sid, csrf, code string) *httptest.ResponseRecorder {
	t.Helper()
	return doCookie(t, s, "GET", linkTicket(t, s, sid, csrf, code), "", sid, "")
}

func doBearer(t *testing.T, s *Server, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// personToken is an API token the person makes, as an agent gets one.
func personToken(t *testing.T, s *Server, sid, csrf string) string {
	t.Helper()
	rec := doCookie(t, s, "POST", "/api/tokens", `{"name":"agent"}`, sid, csrf)
	var created TokenCreateView
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.Token == "" {
		t.Fatalf("token: %d %s", rec.Code, rec.Body.String())
	}
	return created.Token
}

// exchanges counts the code exchanges and refreshes GitHub saw.
func exchanges(fake *githubtest.Server) int {
	n := 0
	for _, c := range fake.Calls() {
		if c == "POST /login/oauth/access_token" {
			n++
		}
	}
	return n
}

func TestLinkingGitHubStoresTheAccountSealed(t *testing.T) {
	s, _, fake := githubServer(t)
	heard := githubAudits(t)
	ctx := context.Background()
	sid, csrf, sub := githubPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	finish := linkTicket(t, s, sid, csrf, githubtest.Code)
	if _, err := s.store.GitHubUserLink(ctx, "local", sub); !errors.Is(err, store.ErrNotFound) || exchanges(fake) != 0 {
		t.Fatalf("the callback alone links nothing: %v, %d exchanges", err, exchanges(fake))
	}
	rec := doCookie(t, s, "GET", finish, "", sid, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `postMessage({github:"linked"}`) || !strings.Contains(rec.Body.String(), githubtest.UserLogin) {
		t.Fatalf("finish: %d %s", rec.Code, rec.Body.String())
	}
	for _, secret := range []string{githubtest.UserToken, "ghr_", githubtest.ClientSecret} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Errorf("the page shows %q", secret)
		}
	}
	link, err := s.store.GitHubUserLink(ctx, "local", sub)
	if err != nil || link.GitHubLogin != githubtest.UserLogin || link.GitHubUserID != githubtest.UserID || strings.Contains(link.RefreshToken, "ghr_") {
		t.Fatalf("stored %+v %v", link, err)
	}
	if refresh, err := s.githubBox.Open(link.RefreshToken, githubLinkRow("local", sub)); err != nil || !strings.HasPrefix(refresh, "ghr_") {
		t.Errorf("the sealed token opens to %q %v", refresh, err)
	}
	if len(*heard) != 1 || (*heard)[0].Action != "github.link" || (*heard)[0].Target != "github user "+githubtest.UserLogin || (*heard)[0].Detail != "" {
		t.Errorf("audit %+v", *heard)
	}
	me := doCookie(t, s, "GET", "/api/me/github", "", sid, "")
	var v GitHubLinkView
	if me.Code != http.StatusOK || json.Unmarshal(me.Body.Bytes(), &v) != nil || v.Login != githubtest.UserLogin || v.LinkedAt.IsZero() {
		t.Errorf("me: %d %s", me.Code, me.Body.String())
	}
	if again := doCookie(t, s, "GET", finish, "", sid, ""); again.Code == http.StatusOK || !strings.Contains(again.Body.String(), "started by someone else, or has expired") {
		t.Errorf("replayed: %d %s", again.Code, again.Body.String())
	}
}

func TestALinkStartedBySomeoneElseIsRefused(t *testing.T) {
	s, _, _ := githubServer(t)
	ctx := context.Background()
	anaSID, anaCSRF, anaSub := githubPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	bobSID, _, bobSub := githubPerson(t, s, "bob@example.test", store.WorkspaceRoleMember)
	refused := func(name string, rec *httptest.ResponseRecorder) {
		t.Helper()
		if rec.Code == http.StatusOK || !strings.Contains(rec.Body.String(), "started by someone else, or has expired") {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
	finish := linkTicket(t, s, anaSID, anaCSRF, githubtest.Code)
	refused("bob's session", doCookie(t, s, "GET", finish, "", bobSID, ""))
	refused("the admin token", do(t, s, "GET", finish, "", true))
	for _, sub := range []string{anaSub, bobSub} {
		if _, err := s.store.GitHubUserLink(ctx, "local", sub); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("linked %s: %v", sub, err)
		}
	}
	key := githubtest.ClientSecret
	ticket := func(sub string, exp time.Time, nonce string) string {
		return "/api/me/github/finish?ticket=" + url.QueryEscape(signGitHubLinkTicket(key, githubLinkTicket{
			Workspace: "default", Provider: "local", Subject: sub, Code: githubtest.Code, Return: "/", Exp: exp.Unix(), Nonce: nonce}))
	}
	refused("expired", doCookie(t, s, "GET", ticket(anaSub, time.Now().Add(-time.Second), "l1"), "", anaSID, ""))
	refused("signed with another key", doCookie(t, s, "GET", "/api/me/github/finish?ticket="+url.QueryEscape(signGitHubLinkTicket("not-the-key",
		githubLinkTicket{Workspace: "default", Provider: "local", Subject: anaSub, Code: githubtest.Code, Exp: time.Now().Add(time.Minute).Unix(), Nonce: "l2"})), "", anaSID, ""))
	connectTicket := signGitHubTicket(key, githubTicket{Workspace: "default", Provider: "local", Subject: anaSub, Installation: 42, Exp: time.Now().Add(time.Minute).Unix(), Nonce: "l3"})
	refused("a Connect ticket", doCookie(t, s, "GET", "/api/me/github/finish?ticket="+url.QueryEscape(connectTicket), "", anaSID, ""))
	if _, err := readGitHubState(key, signGitHubLinkTicket(key, githubLinkTicket{Workspace: "default", Subject: "x", Code: "c", Exp: time.Now().Add(time.Minute).Unix(), Nonce: "l4"}), time.Now()); err == nil {
		t.Error("a link ticket reads as a state")
	}
	// The refusals spent nothing: ana finishes her own.
	if rec := doCookie(t, s, "GET", finish, "", anaSID, ""); rec.Code != http.StatusOK {
		t.Errorf("ana: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := s.store.GitHubUserLink(ctx, "local", bobSub); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("bob is linked to ana's GitHub: %v", err)
	}
}

func TestALinkStateIsUsedOnce(t *testing.T) {
	s, _, _ := githubServer(t)
	sid, csrf, _ := githubPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	rec := doCookie(t, s, "POST", "/api/me/github/link", `{}`, sid, csrf)
	var v GitHubLinkStartView
	_ = json.Unmarshal(rec.Body.Bytes(), &v)
	u, _ := url.Parse(v.AuthorizeURL)
	q := "/api/github/installed?" + url.Values{"code": {githubtest.Code}, "state": {u.Query().Get("state")}}.Encode()
	if first := do(t, s, "GET", q, "", false); first.Code != http.StatusFound {
		t.Fatalf("first: %d %s", first.Code, first.Body.String())
	}
	if again := do(t, s, "GET", q, "", false); again.Code != http.StatusBadRequest || !strings.Contains(again.Body.String(), "used already") {
		t.Errorf("replayed: %d %s", again.Code, again.Body.String())
	}
}

func TestAnAPITokenUsesItsMakersGitHubLink(t *testing.T) {
	s, _, _ := githubServer(t)
	sid, csrf, _ := githubPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	if rec := linkAs(t, s, sid, csrf, githubtest.Code); rec.Code != http.StatusOK {
		t.Fatalf("link: %d %s", rec.Code, rec.Body.String())
	}
	token := personToken(t, s, sid, csrf)
	rec := doBearer(t, s, "GET", "/api/me/github", "", token)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"login":"`+githubtest.UserLogin+`"`) {
		t.Errorf("the token's GitHub: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, s, "GET", "/api/me/github", "", true); rec.Code != http.StatusNotFound {
		t.Errorf("the admin token has a GitHub: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, s, "POST", "/api/me/github/link", `{}`, true); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "sign in as yourself") {
		t.Errorf("the admin token links: %d %s", rec.Code, rec.Body.String())
	}
}

func TestUnlinkRevokesTheTokenAndForgetsTheLink(t *testing.T) {
	s, _, fake := githubServer(t)
	ctx := context.Background()
	sid, csrf, sub := githubPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	linkAs(t, s, sid, csrf, githubtest.Code)
	heard := githubAudits(t)
	if rec := doCookie(t, s, "DELETE", "/api/me/github", "", sid, csrf); rec.Code != http.StatusNoContent {
		t.Fatalf("unlink: %d %s", rec.Code, rec.Body.String())
	}
	if got := fake.Revoked(); len(got) != 1 || got[0] != githubtest.UserToken {
		t.Errorf("revoked %v", got)
	}
	if _, err := s.store.GitHubUserLink(ctx, "local", sub); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("still linked: %v", err)
	}
	if len(*heard) != 1 || (*heard)[0].Action != "github.unlink" || (*heard)[0].Target != "github user "+githubtest.UserLogin {
		t.Errorf("audit %+v", *heard)
	}
	if rec := doCookie(t, s, "GET", "/api/me/github", "", sid, ""); rec.Code != http.StatusNotFound {
		t.Errorf("me after unlink: %d", rec.Code)
	}
	if rec := doCookie(t, s, "DELETE", "/api/me/github", "", sid, csrf); rec.Code != http.StatusNotFound {
		t.Errorf("unlinked twice: %d", rec.Code)
	}
	// GitHub down: the link goes all the same.
	linkAs(t, s, sid, csrf, githubtest.Code)
	fake.SetDown(true)
	if rec := doCookie(t, s, "DELETE", "/api/me/github", "", sid, csrf); rec.Code != http.StatusNoContent {
		t.Errorf("unlink with GitHub down: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := s.store.GitHubUserLink(ctx, "local", sub); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("still linked: %v", err)
	}
}

func TestAPersonsTokenIsRefreshedOnceAndTheNewRefreshTokenStored(t *testing.T) {
	s, _, fake := githubServer(t)
	ctx := context.Background()
	sid, csrf, sub := githubPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	linkAs(t, s, sid, csrf, githubtest.Code)
	before, _ := s.store.GitHubUserLink(ctx, "local", sub)
	cl, err := s.githubApp.Client(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if tok, err := s.githubAccessToken(ctx, cl, before); err != nil || tok != githubtest.UserToken || exchanges(fake) != 1 {
		t.Fatalf("kept from the link: %q %v, %d exchanges", tok, err, exchanges(fake))
	}
	// After a restart nothing is kept in memory: the refresh token is used,
	// once, and the new one stored.
	s.githubUsers.forget("local", sub)
	tok, err := s.githubAccessToken(ctx, cl, before)
	if err != nil || !strings.HasPrefix(tok, "ghu_") || exchanges(fake) != 2 {
		t.Fatalf("refreshed: %q %v, %d exchanges", tok, err, exchanges(fake))
	}
	after, _ := s.store.GitHubUserLink(ctx, "local", sub)
	if after.RefreshToken == before.RefreshToken {
		t.Error("the rotated refresh token was not stored")
	}
	if again, _ := s.githubAccessToken(ctx, cl, before); again != tok || exchanges(fake) != 2 {
		t.Errorf("refreshed twice: %q, %d exchanges", again, exchanges(fake))
	}
	// GitHub no longer takes it: the link goes, and the person is asked to
	// link again.
	s.githubUsers.forget("local", sub)
	fake.ForgetRefreshTokens()
	if _, err := s.githubAccessToken(ctx, cl, after); !errors.Is(err, errGitHubLinkRequired) {
		t.Errorf("refused refresh: %v", err)
	}
	if _, err := s.store.GitHubUserLink(ctx, "local", sub); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a dead link stays: %v", err)
	}
}

func TestALinkWithoutExpiringTokensSaysWhatToTurnOn(t *testing.T) {
	s, _, fake := githubServer(t)
	fake.SetExpiringUserTokens(false)
	sid, csrf, sub := githubPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	rec := linkAs(t, s, sid, csrf, githubtest.Code)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "Expire user authorization tokens") {
		t.Errorf("%d %s", rec.Code, rec.Body.String())
	}
	if _, err := s.store.GitHubUserLink(context.Background(), "local", sub); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("stored: %v", err)
	}
	if got := fake.Revoked(); len(got) != 1 {
		t.Errorf("the token GitHub gave is not revoked: %v", got)
	}
}

func TestTheCLIsLinkPageGoesToGitHub(t *testing.T) {
	s, _, _ := githubServer(t)
	sid, _, _ := githubPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	rec := doCookie(t, s, "GET", "/api/me/github/link", "", sid, "")
	u, err := url.Parse(rec.Header().Get("Location"))
	if rec.Code != http.StatusFound || err != nil || u.Path != "/login/oauth/authorize" || u.Query().Get("state") == "" {
		t.Errorf("%d %q", rec.Code, rec.Header().Get("Location"))
	}
	// A browser not signed in is asked to sign in first.
	if rec := do(t, s, "GET", "/api/me/github/link", "", false); rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "then open this link again") {
		t.Errorf("not signed in: %d %s", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp TMPDIR=/home/nkr/.cache/gotmp go test ./pkg/api/ -run 'Linking|LinkStarted|LinkState|APITokenUses|UnlinkRevokes|RefreshedOnce|WithoutExpiring|CLIsLinkPage'`
Expected: FAIL to compile: `undefined: GitHubLinkStartView`, `undefined: signGitHubLinkTicket`, `s.githubUsers undefined`.

- [ ] **Step 3: The state says "link"**

In `pkg/api/github.go`, `githubState` gains, after `Subject`:

```go
	// Link: the state starts a link of the person's own GitHub account
	// (RFC-0054), not a connection; Return is the dashboard path the
	// person was on.
	Link   bool   `json:"link,omitempty"`
	Return string `json:"ret,omitempty"`
```

`readGitHubState`'s first condition becomes:

```go
	if strings.HasPrefix(raw, githubTicketPrefix) || strings.HasPrefix(raw, githubLinkTicketPrefix) || !githubRead(key, "", raw, &st) || st.Workspace == "" || st.Nonce == "" || now.Unix() > st.Exp {
```

In `githubInstalled`, right after the `cl, err := s.githubApp.Client(ctx)` block (before `setup_action == "request"`):

```go
	// A link of a person's GitHub account comes back here too: its state
	// says so (RFC-0054).
	if st, err := readGitHubState(cl.App().ClientSecret, c.Query("state"), time.Now()); err == nil && st.Link {
		s.githubLinkCallback(c, cl, st)
		return
	}
```

- [ ] **Step 4: The token's maker on the request**

In `pkg/api/tokens.go`, `identifyWithToken`, after `c.Set("shpyrd.roles", tc.roles)`:

```go
	// The person the token acts for: its GitHub link is theirs (RFC-0054).
	c.Set(tokenOwnerKey, tc.owner)
```

- [ ] **Step 5: The link**

`pkg/api/github_link.go`:

```go
package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/github"
	"github.com/shpyrd-io/shpyrd/pkg/pages"
	"github.com/shpyrd-io/shpyrd/pkg/store"
	"github.com/shpyrd-io/shpyrd/pkg/tokenseal"
)

// A person's GitHub account (RFC-0054, GitHub access by person): linked
// once through the GitHub App's user authorization, in a popup that keeps
// New project as it was. The picker then lists what the person can read,
// and a project is made from a repository only by someone who can read it.
// Builds, webhooks, checks and Deployments stay with the installation: a
// person who leaves or unlinks breaks nothing.

// GitHubLinkView is the person's linked GitHub account.
type GitHubLinkView struct {
	Login    string    `json:"login"`
	LinkedAt time.Time `json:"linkedAt"`
}

// GitHubLinkStartView is where the popup goes: GitHub's authorize page.
type GitHubLinkStartView struct {
	AuthorizeURL string `json:"authorizeURL"`
}

const (
	// githubLinkRequiredCode is the code of the 409 that asks the person to
	// link their GitHub account first.
	githubLinkRequiredCode = "github_link_required"
	// githubLinkTicketPrefix sets a link ticket apart from a state and a
	// Connect ticket.
	githubLinkTicketPrefix = "l."
	// githubUserTokenMargin: an access token is used until 5 minutes
	// before it expires.
	githubUserTokenMargin = 5 * time.Minute
	// tokenOwnerKey is where identifyWithToken leaves the token's maker.
	tokenOwnerKey = "shpyrd.tokenOwner"
)

var (
	errGitHubLinkRequired = errors.New("Link your GitHub account first: Continue with GitHub in New project, or run shpyrd github link")
	errGitHubNoPerson     = errors.New("the admin token is no person and has no GitHub account: sign in as yourself to link GitHub")
)

// githubLinkTicket is what the callback hands the workspace's finish page:
// who started the link, where they were, and GitHub's one-time code,
// exchanged only once the session that started presents it.
type githubLinkTicket struct {
	Workspace string `json:"ws"`
	Provider  string `json:"prov"`
	Subject   string `json:"sub"`
	Code      string `json:"code"`
	Return    string `json:"ret,omitempty"`
	Exp       int64  `json:"exp"`
	Nonce     string `json:"n"`
}

func signGitHubLinkTicket(key string, tk githubLinkTicket) string {
	return githubSign(key, githubLinkTicketPrefix, tk)
}

// readGitHubLinkTicket checks the tag in constant time, then the time.
func readGitHubLinkTicket(key, raw string, now time.Time) (githubLinkTicket, error) {
	var tk githubLinkTicket
	if !githubRead(key, githubLinkTicketPrefix, raw, &tk) || tk.Workspace == "" || tk.Subject == "" || tk.Code == "" || tk.Nonce == "" || now.Unix() > tk.Exp {
		return githubLinkTicket{}, errGitHubTicket
	}
	return tk, nil
}

// githubPersonOf is whose GitHub link a request uses: the person signed
// in, or, for an API token, the person who made it. Not ok for the admin
// token, a token the admin token made, the operator's kubeconfig, and a
// server without sign-in.
func githubPersonOf(c *gin.Context) (provider, subject string, ok bool) {
	id, signedIn := ext.IdentityFrom(c)
	if v, has := c.Get(tokenOwnerKey); has {
		if owner, isIdentity := v.(ext.Identity); isIdentity {
			id, signedIn = owner, true
		}
	}
	switch {
	case !signedIn || id.Subject == "" || id.Provider == "":
		return "", "", false
	case id.Provider == "token" || id.Provider == "api-token" || id.Provider == "kubeconfig" || id.Provider == "none":
		return "", "", false
	}
	return id.Provider, id.Subject, true
}

// githubLinkRow binds a sealed refresh token to its link.
func githubLinkRow(provider, subject string) string {
	return tokenseal.Row("github-user-link", provider, subject)
}

// githubLinkStart is POST /api/me/github/link: GitHub's authorize URL, with
// a state bound to the person, for the dashboard's popup.
func (s *Server) githubLinkStart(c *gin.Context) {
	var req struct {
		Return string `json:"return"`
	}
	_ = c.ShouldBindJSON(&req)
	if to, ok := s.githubLinkURL(c, req.Return); ok {
		c.JSON(http.StatusOK, GitHubLinkStartView{AuthorizeURL: to})
	}
}

// githubLinkBrowser is GET /api/me/github/link: `shpyrd github link` opens
// it in the browser, signed in there, and it goes on to GitHub.
func (s *Server) githubLinkBrowser(c *gin.Context) {
	if to, ok := s.githubLinkURL(c, c.Query("return")); ok {
		c.Header("Cache-Control", "no-store")
		c.Redirect(http.StatusFound, to)
	}
}

func (s *Server) githubLinkURL(c *gin.Context, ret string) (string, bool) {
	provider, subject, ok := githubPersonOf(c)
	if !ok {
		abort(c, http.StatusBadRequest, errGitHubNoPerson)
		return "", false
	}
	cl, ok := s.githubClient(c)
	if !ok {
		return "", false
	}
	app := cl.App()
	actor, _, _ := githubWho(c)
	state, err := newGitHubState(app, githubState{Workspace: s.doorWorkspace(c), Actor: actor, Provider: provider, Subject: subject,
		Link: true, Return: safeNextOrEmpty(ret)}, time.Now().Add(githubStateTTL))
	if err != nil {
		abort(c, http.StatusInternalServerError, err)
		return "", false
	}
	// No redirect_uri: GitHub returns to the App's registered callback,
	// /api/github/installed, which tells a link by its state.
	return app.WebURL() + "/login/oauth/authorize?" + url.Values{"client_id": {app.ClientID}, "state": {state}}.Encode(), true
}

// githubLinkCallback is GitHub's return from a link's authorize step, on
// /api/github/installed: the state is spent, and GitHub's code goes on in
// a ticket to the workspace's finish, where the session that started must
// present it.
func (s *Server) githubLinkCallback(c *gin.Context, cl *github.Client, st githubState) {
	now := time.Now()
	if !s.githubNonces.use(st.Nonce, time.Unix(st.Exp, 0), now) {
		s.githubPage(c, http.StatusBadRequest, "This link was used already", "Each Continue with GitHub is used once. Start again from New project, or run shpyrd github link.", "")
		return
	}
	code := c.Query("code")
	if code == "" {
		s.githubPage(c, http.StatusBadRequest, "GitHub did not say who you are", "Nothing was linked. Start again from New project, or run shpyrd github link.", "")
		return
	}
	nonce, err := randomHex(16)
	if err != nil {
		abort(c, http.StatusInternalServerError, err)
		return
	}
	tk := githubLinkTicket{Workspace: st.Workspace, Provider: st.Provider, Subject: st.Subject, Code: code, Return: st.Return,
		Exp: now.Add(githubTicketTTL).Unix(), Nonce: nonce}
	c.Header("Cache-Control", "no-store")
	c.Redirect(http.StatusFound, s.workspaceDashboardURL(c.Request.Context(), st.Workspace)+"/api/me/github/finish?ticket="+url.QueryEscape(signGitHubLinkTicket(cl.App().ClientSecret, tk)))
}

// githubLinkFinish is GET /api/me/github/finish, on the workspace's own
// address: the ticket, presented by the session that started the link,
// is exchanged for the person's tokens, and the link is stored.
func (s *Server) githubLinkFinish(c *gin.Context) {
	ctx := c.Request.Context()
	refuse := func(status int) {
		s.githubPage(c, status, "Not linked", "This link was started by someone else, or has expired: start it again from New project, or run shpyrd github link.", "")
	}
	cl, err := s.githubApp.Client(ctx)
	if err != nil {
		refuse(http.StatusNotFound)
		return
	}
	now := time.Now()
	tk, err := readGitHubLinkTicket(cl.App().ClientSecret, c.Query("ticket"), now)
	if err != nil {
		refuse(http.StatusBadRequest)
		return
	}
	provider, subject, ok := githubPersonOf(c)
	if !ok || tk.Workspace != s.doorWorkspace(c) || tk.Provider != provider || tk.Subject != subject {
		refuse(http.StatusForbidden)
		return
	}
	// Spent only once the right person presents it.
	if !s.githubTickets.use(tk.Nonce, time.Unix(tk.Exp, 0), now) {
		refuse(http.StatusBadRequest)
		return
	}
	tok, err := cl.ExchangeUserCode(ctx, tk.Code)
	if err != nil {
		s.log.Warn("github: could not exchange a link's code", "err", err.Error())
		s.githubPage(c, http.StatusBadGateway, "GitHub did not answer", "shpyrd could not finish linking your GitHub account. Start again in a minute.", "")
		return
	}
	if tok.Refresh == "" {
		if err := cl.RevokeUserToken(ctx, tok.Access); err != nil {
			s.log.Warn("github: could not revoke a token that is not kept", "err", err.Error())
		}
		s.githubPage(c, http.StatusConflict, "GitHub needs a setting", github.ErrNoRefreshToken.Error()+".", "")
		return
	}
	who, err := cl.UserProfile(ctx, tok.Access)
	if err != nil {
		s.log.Warn("github: could not ask GitHub who linked", "err", err.Error())
		s.githubPage(c, http.StatusBadGateway, "GitHub did not answer", "shpyrd could not finish linking your GitHub account. Start again in a minute.", "")
		return
	}
	sealed, err := s.githubBox.Seal(tok.Refresh, githubLinkRow(provider, subject))
	if err != nil {
		abort(c, http.StatusInternalServerError, err)
		return
	}
	link := store.GitHubUserLink{Provider: provider, Subject: subject, GitHubUserID: who.ID, GitHubLogin: who.Login,
		RefreshToken: sealed, RefreshExpiresAt: tok.RefreshExpiresAt}
	if err := s.store.PutGitHubUserLink(ctx, link); err != nil {
		s.log.Warn("github: could not record a link", "err", err.Error())
		s.githubPage(c, http.StatusBadGateway, "Not linked", "shpyrd could not record the link. Start again in a minute.", "")
		return
	}
	s.githubUsers.keep(provider, subject, tok)
	s.audit(c, "", "github.link", "github user "+who.Login, "")
	s.githubLinkedPage(c, who.Login, firstNonEmpty(tk.Return, "/"))
}

// githubLinkedScript tells the dashboard under the popup, and closes it.
// Opened by the CLI, there is no opener: the page stays and says so.
const githubLinkedScript = `if(window.opener){try{window.opener.postMessage({github:"linked"},location.origin)}catch(e){}window.close()}`

// githubLinkedPage is the end of a link.
func (s *Server) githubLinkedPage(c *gin.Context, login, back string) {
	html := pages.HTML(pages.Nothing, pages.Page{
		Title: "GitHub linked",
		Text:  fmt.Sprintf("Your GitHub account %s is linked. You can close this window.", login),
		Links: []pages.Link{{Label: "Back to shpyrd", URL: back}},
	})
	if i := strings.LastIndex(html, "</body>"); i >= 0 {
		html = html[:i] + "<script>" + githubLinkedScript + "</script>" + html[i:]
	}
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}

// githubMe is GET /api/me/github.
func (s *Server) githubMe(c *gin.Context) {
	provider, subject, ok := githubPersonOf(c)
	if !ok {
		abort(c, http.StatusNotFound, errors.New("no GitHub account is linked: the admin token is no person"))
		return
	}
	link, err := s.store.GitHubUserLink(c.Request.Context(), provider, subject)
	if errors.Is(err, store.ErrNotFound) {
		abort(c, http.StatusNotFound, errors.New("no GitHub account is linked: shpyrd github link"))
		return
	}
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	c.JSON(http.StatusOK, GitHubLinkView{Login: link.GitHubLogin, LinkedAt: link.LinkedAt})
}

// githubUnlink is DELETE /api/me/github: the token is revoked at GitHub
// and the link removed. The person's projects keep deploying.
func (s *Server) githubUnlink(c *gin.Context) {
	provider, subject, ok := githubPersonOf(c)
	if !ok {
		abort(c, http.StatusBadRequest, errGitHubNoPerson)
		return
	}
	ctx := c.Request.Context()
	link, err := s.store.GitHubUserLink(ctx, provider, subject)
	if errors.Is(err, store.ErrNotFound) {
		abort(c, http.StatusNotFound, errors.New("no GitHub account is linked"))
		return
	}
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	if cl, err := s.githubApp.Client(ctx); err == nil {
		access, err := s.githubAccessToken(ctx, cl, link)
		if err == nil {
			err = cl.RevokeUserToken(ctx, access)
		}
		if err != nil && !errors.Is(err, errGitHubLinkRequired) {
			s.log.Warn("github: could not revoke the person's GitHub token; the link is removed all the same", "login", link.GitHubLogin, "err", err.Error())
		}
	}
	if err := s.store.DeleteGitHubUserLink(ctx, provider, subject); err != nil && !errors.Is(err, store.ErrNotFound) {
		abort(c, http.StatusBadGateway, err)
		return
	}
	s.githubUsers.forget(provider, subject)
	s.audit(c, "", "github.unlink", "github user "+link.GitHubLogin, "")
	c.Status(http.StatusNoContent)
}

// githubUserTokens keeps each person's access token in memory, never
// stored, and one lock per person: GitHub takes each refresh token once.
type githubUserTokens struct {
	mu    sync.Mutex
	by    map[string]github.UserToken
	locks map[string]*sync.Mutex
}

func personKey(provider, subject string) string { return provider + "\x00" + subject }

func (g *githubUserTokens) get(key string, now time.Time) (string, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	t, ok := g.by[key]
	if !ok || !now.Add(githubUserTokenMargin).Before(t.AccessExpiresAt) {
		return "", false
	}
	return t.Access, true
}

func (g *githubUserTokens) keep(provider, subject string, t github.UserToken) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.by == nil {
		g.by = map[string]github.UserToken{}
	}
	g.by[personKey(provider, subject)] = github.UserToken{Access: t.Access, AccessExpiresAt: t.AccessExpiresAt}
}

func (g *githubUserTokens) forget(provider, subject string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.by, personKey(provider, subject))
}

func (g *githubUserTokens) lock(key string) *sync.Mutex {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.locks == nil {
		g.locks = map[string]*sync.Mutex{}
	}
	if g.locks[key] == nil {
		g.locks[key] = &sync.Mutex{}
	}
	return g.locks[key]
}

// githubAccessToken is the person's access token: the one in memory while
// it has 5 minutes left, else a refresh with the stored refresh token,
// whose successor is sealed and stored. errGitHubLinkRequired when GitHub
// no longer takes it, or it no longer opens (the key changed): the link is
// then removed.
func (s *Server) githubAccessToken(ctx context.Context, cl *github.Client, link *store.GitHubUserLink) (string, error) {
	key := personKey(link.Provider, link.Subject)
	if t, ok := s.githubUsers.get(key, time.Now()); ok {
		return t, nil
	}
	mu := s.githubUsers.lock(key)
	mu.Lock()
	defer mu.Unlock()
	if t, ok := s.githubUsers.get(key, time.Now()); ok {
		return t, nil
	}
	// Read again under the lock: another request may have rotated it.
	fresh, err := s.store.GitHubUserLink(ctx, link.Provider, link.Subject)
	if errors.Is(err, store.ErrNotFound) {
		return "", errGitHubLinkRequired
	}
	if err != nil {
		return "", err
	}
	row := githubLinkRow(fresh.Provider, fresh.Subject)
	refresh, err := s.githubBox.Open(fresh.RefreshToken, row)
	if err != nil {
		s.log.Warn("github: a stored GitHub link does not open with the token key; it is removed and the person links again", "login", fresh.GitHubLogin)
		_ = s.store.DeleteGitHubUserLink(ctx, fresh.Provider, fresh.Subject)
		return "", errGitHubLinkRequired
	}
	tok, err := cl.RefreshUserToken(ctx, refresh)
	if errors.Is(err, github.ErrUserTokenRefused) {
		_ = s.store.DeleteGitHubUserLink(ctx, fresh.Provider, fresh.Subject)
		return "", errGitHubLinkRequired
	}
	if err != nil {
		return "", err
	}
	sealed, err := s.githubBox.Seal(tok.Refresh, row)
	if err != nil {
		return "", err
	}
	fresh.RefreshToken, fresh.RefreshExpiresAt = sealed, tok.RefreshExpiresAt
	if err := s.store.PutGitHubUserLink(ctx, *fresh); err != nil {
		return "", err
	}
	s.githubUsers.keep(fresh.Provider, fresh.Subject, tok)
	return tok.Access, nil
}

// githubPersonToken is the GitHub token of whoever the request acts as,
// and their login; errGitHubLinkRequired when they have no link (or are no
// person).
func (s *Server) githubPersonToken(c *gin.Context, cl *github.Client) (string, string, error) {
	provider, subject, ok := githubPersonOf(c)
	if !ok {
		return "", "", errGitHubLinkRequired
	}
	link, err := s.store.GitHubUserLink(c.Request.Context(), provider, subject)
	if errors.Is(err, store.ErrNotFound) {
		return "", "", errGitHubLinkRequired
	}
	if err != nil {
		return "", "", err
	}
	tok, err := s.githubAccessToken(c.Request.Context(), cl, link)
	return tok, link.GitHubLogin, err
}

// githubLinkAnswer answers a request that needed the person's GitHub: 409
// github_link_required, or 502.
func githubLinkAnswer(c *gin.Context, err error) {
	if errors.Is(err, errGitHubLinkRequired) {
		c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": err.Error(), "code": githubLinkRequiredCode})
		return
	}
	abort(c, http.StatusBadGateway, fmt.Errorf("shpyrd could not reach GitHub with your account: %w. Try again in a minute", err))
}
```

- [ ] **Step 6: The server's fields and routes**

In `pkg/api/server.go`, `Server` gains after `githubBox`:

```go
	// githubUsers keeps the people's GitHub access tokens in memory.
	githubUsers *githubUserTokens
```

`newServer`, after `s.githubRepos = &githubRepoCache{}`: `s.githubUsers = &githubUserTokens{}`.

`routes`, after `api.GET("/me", s.me)`:

```go
	// The person's own GitHub account (RFC-0054).
	api.GET("/me/github", s.githubMe)
	api.POST("/me/github/link", s.githubLinkStart)
	api.DELETE("/me/github", s.githubUnlink)
```

and after the `s.engine.GET("/api/workspace/github/finish", …)` line:

```go
	// A link of the person's GitHub: the CLI opens its start in the
	// browser, and it finishes on the workspace's own address with the
	// session that started it; a browser without one signs in first.
	s.engine.GET("/api/me/github/link", tenant, login, s.githubSignInFirst, s.auth(), s.githubLinkBrowser)
	s.engine.GET("/api/me/github/finish", tenant, login, s.githubSignInFirst, s.auth(), s.githubLinkFinish)
```

- [ ] **Step 7: Run the tests**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp TMPDIR=/home/nkr/.cache/gotmp go build ./... && GOTMPDIR=/home/nkr/.cache/gotmp TMPDIR=/home/nkr/.cache/gotmp go test ./pkg/api/`
Expected: PASS, the Connect tests included.

- [ ] **Step 8: Commit**

```bash
git add pkg/api/github_link.go pkg/api/github_link_test.go pkg/api/github.go pkg/api/tokens.go pkg/api/server.go
git commit -m "feat(api): link your GitHub account: a popup through the App's callback, finished by the session that started, the refresh token sealed

Co-Authored-By: <your model name> <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 5: The picker, per person: accounts, repositories, branches

**Files:**
- Modify: `pkg/api/github.go` (`githubRepoCache` comment, `forgetGitHubRepos`, `githubRepositories`, `githubBranches`; new views and `githubAccounts`), `pkg/api/github_link.go` (`githubLinkFinish`, `githubUnlink` forget the person's listings), `pkg/api/server.go` (the accounts route ~703)
- Create: `pkg/api/github_picker_test.go`
- Modify: `pkg/api/github_test.go` (delete `TestGitHubRepositoriesAndBranchesForThePicker` and `TestGitHubRepositoriesSkipAnInstallationThatIsGone`: the admin token no longer picks; the new file covers both)

**Interfaces:**
- Consumes: `githubPersonToken`, `githubPersonOf`, `githubLinkAnswer`, `linkAs`, `githubPerson` (Task 4); `Client.UserInstallations` (exists), `UserRepositories`, `UserBranches` (Task 3); `connectAcme(t, s, ws)` (exists in `github_webhook_test.go`).
- Produces:
  - `type api.GitHubAccountView struct { Installation int64; Account, Type string; Suspended bool; ManageURL string }` (json `installation`, `account`, `type`, `suspended`, `manageURL`)
  - `type api.GitHubAccountsView struct { Accounts []GitHubAccountView; Connected int }` (json `accounts`, `connected`)
  - `GET /api/workspace/github/accounts` (`ClusterCreate`)
  - `GET /api/workspace/github/repositories[?installation=<id>]` answers what the person reads; 409 `github_link_required`
  - `func (s *Server) forgetPersonRepos(provider, subject string)`

- [ ] **Step 1: Write the failing tests**

`pkg/api/github_picker_test.go`:

```go
package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/shpyrd-io/shpyrd/pkg/github/githubtest"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

func repoNames(list []GitHubRepositoryView) string {
	var out []string
	for _, r := range list {
		out = append(out, r.FullName)
	}
	return strings.Join(out, ",")
}

func TestThePickerListsOnlyWhatThePersonCanRead(t *testing.T) {
	s, _, fake := githubServer(t)
	connectAcme(t, s, store.DefaultWorkspace)
	fake.AddUser("bob-code", githubtest.User{ID: 1002, Login: "bob", Installations: []int64{42}, Repos: []string{"Acme/site"}})
	fake.AddUser("cleo-code", githubtest.User{ID: 1003, Login: "cleo", Installations: []int64{}})
	anaSID, anaCSRF, _ := githubPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	bobSID, bobCSRF, _ := githubPerson(t, s, "bob@example.test", store.WorkspaceRoleMember)
	cleoSID, cleoCSRF, _ := githubPerson(t, s, "cleo@example.test", store.WorkspaceRoleMember)
	for _, path := range []string{"/api/workspace/github/accounts", "/api/workspace/github/repositories?installation=42", "/api/workspace/github/repositories/acme/shop/branches"} {
		if rec := doCookie(t, s, "GET", path, "", anaSID, ""); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"code":"github_link_required"`) {
			t.Errorf("not linked, %s: %d %s", path, rec.Code, rec.Body.String())
		}
	}
	linkAs(t, s, anaSID, anaCSRF, githubtest.Code)
	linkAs(t, s, bobSID, bobCSRF, "bob-code")
	linkAs(t, s, cleoSID, cleoCSRF, "cleo-code")

	accounts := func(sid string) GitHubAccountsView {
		t.Helper()
		rec := doCookie(t, s, "GET", "/api/workspace/github/accounts", "", sid, "")
		var v GitHubAccountsView
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &v) != nil {
			t.Fatalf("accounts: %d %s", rec.Code, rec.Body.String())
		}
		return v
	}
	if a := accounts(anaSID); a.Connected != 1 || len(a.Accounts) != 1 || a.Accounts[0].Installation != 42 || a.Accounts[0].Account != "Acme" ||
		a.Accounts[0].Type != "Organization" || a.Accounts[0].ManageURL != fake.URL+"/organizations/Acme/settings/installations/42" {
		t.Errorf("ana's accounts %+v", a)
	}
	if a := accounts(cleoSID); a.Connected != 1 || len(a.Accounts) != 0 {
		t.Errorf("cleo sees an account: %+v", a)
	}

	repos := func(sid, query string) []GitHubRepositoryView {
		t.Helper()
		rec := doCookie(t, s, "GET", "/api/workspace/github/repositories"+query, "", sid, "")
		var v []GitHubRepositoryView
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &v) != nil {
			t.Fatalf("repositories%s: %d %s", query, rec.Code, rec.Body.String())
		}
		return v
	}
	if got := repoNames(repos(anaSID, "?installation=42")); got != "Acme/shop,Acme/site" {
		t.Errorf("ana: %s", got)
	}
	for _, q := range []string{"?installation=42", ""} {
		if got := repoNames(repos(bobSID, q)); got != "Acme/site" {
			t.Errorf("bob%s: %s", q, got)
		}
	}
	if got := repos(cleoSID, ""); len(got) != 0 {
		t.Errorf("cleo: %+v", got)
	}
	// Kept a minute, per person: bob asks again, GitHub is not asked.
	calls := len(fake.Calls())
	repos(bobSID, "?installation=42")
	if len(fake.Calls()) != calls {
		t.Errorf("listed again: %v", fake.Calls()[calls:])
	}

	branches := func(sid, path string) (int, string) {
		rec := doCookie(t, s, "GET", "/api/workspace/github/repositories/"+path+"/branches", "", sid, "")
		return rec.Code, strings.TrimSpace(rec.Body.String())
	}
	if code, body := branches(anaSID, "acme/shop"); code != http.StatusOK || body != `["dev","main"]` {
		t.Errorf("ana's branches: %d %s", code, body)
	}
	if code, body := branches(bobSID, "acme/shop"); code != http.StatusNotFound || !strings.Contains(body, "You can't read acme/shop on GitHub") {
		t.Errorf("bob's branches of shop: %d %s", code, body)
	}
	if code, _ := branches(anaSID, "other/shop"); code != http.StatusNotFound {
		t.Errorf("an owner no connection reaches: %d", code)
	}
	if rec := doCookie(t, s, "GET", "/api/workspace/github/repositories?installation=7", "", anaSID, ""); rec.Code != http.StatusNotFound {
		t.Errorf("another workspace's installation: %d", rec.Code)
	}
	if rec := doCookie(t, s, "GET", "/api/workspace/github/repositories?installation=abc", "", anaSID, ""); rec.Code != http.StatusBadRequest {
		t.Errorf("not a number: %d", rec.Code)
	}
	fake.SetDown(true)
	s.forgetPersonRepos("local", "")
	s.forgetGitHubRepos(store.DefaultWorkspace)
	if rec := doCookie(t, s, "GET", "/api/workspace/github/repositories", "", anaSID, ""); rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "Acme") {
		t.Errorf("GitHub down: %d %s", rec.Code, rec.Body.String())
	}
}

func TestAWorkspaceWithoutGitHubAsksNoLink(t *testing.T) {
	s, _, _ := githubServer(t)
	sid, _, _ := githubPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	rec := doCookie(t, s, "GET", "/api/workspace/github/accounts", "", sid, "")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"accounts":[],"connected":0}` {
		t.Errorf("accounts: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doCookie(t, s, "GET", "/api/workspace/github/repositories", "", sid, ""); rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `[]` {
		t.Errorf("repositories: %d %s", rec.Code, rec.Body.String())
	}
}

func TestThePickerSkipsAnInstallationThatIsGone(t *testing.T) {
	s, _, fake := githubServer(t)
	connectAcme(t, s, store.DefaultWorkspace)
	if err := s.store.AddGitHubInstallation(t.Context(), store.DefaultWorkspace, store.GitHubInstallation{ID: 7, AccountLogin: "Gone", AccountType: "Organization", ConnectedBy: "ana"}); err != nil {
		t.Fatal(err)
	}
	fake.Uninstall(7)
	sid, csrf, _ := githubPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	linkAs(t, s, sid, csrf, githubtest.Code)
	rec := doCookie(t, s, "GET", "/api/workspace/github/repositories", "", sid, "")
	var v []GitHubRepositoryView
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &v) != nil || repoNames(v) != "Acme/shop,Acme/site" {
		t.Errorf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestThePickerAsksToLinkAgainWhenGitHubForgotTheLink(t *testing.T) {
	s, _, fake := githubServer(t)
	connectAcme(t, s, store.DefaultWorkspace)
	sid, csrf, sub := githubPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	linkAs(t, s, sid, csrf, githubtest.Code)
	s.githubUsers.forget("local", sub)
	fake.ForgetRefreshTokens()
	if rec := doCookie(t, s, "GET", "/api/workspace/github/accounts", "", sid, ""); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "github_link_required") {
		t.Errorf("accounts: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doCookie(t, s, "GET", "/api/me/github", "", sid, ""); rec.Code != http.StatusNotFound {
		t.Errorf("me: %d", rec.Code)
	}
}

func TestThePickerRefreshesAnExpiredToken(t *testing.T) {
	s, _, fake := githubServer(t)
	connectAcme(t, s, store.DefaultWorkspace)
	sid, csrf, sub := githubPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	linkAs(t, s, sid, csrf, githubtest.Code)
	fake.ExpireUserTokens()
	s.githubUsers.forget("local", sub)
	if rec := doCookie(t, s, "GET", "/api/workspace/github/accounts", "", sid, ""); rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if n := exchanges(fake); n != 2 {
		t.Errorf("%d exchanges: the code, then one refresh", n)
	}
}
```

In `pkg/api/github_test.go`, delete `TestGitHubRepositoriesAndBranchesForThePicker` and `TestGitHubRepositoriesSkipAnInstallationThatIsGone` whole.

- [ ] **Step 2: Run them to see them fail**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp TMPDIR=/home/nkr/.cache/gotmp go test ./pkg/api/ -run 'ThePicker|WithoutGitHubAsksNoLink'`
Expected: FAIL to compile: `undefined: GitHubAccountsView`, `s.forgetPersonRepos undefined`.

- [ ] **Step 3: The views and the cache keys**

In `pkg/api/github.go`, after `GitHubRepositoryView`:

```go
// GitHubAccountView is an account of the workspace's GitHub that the
// person sees on GitHub: the picker's account switcher (RFC-0054).
type GitHubAccountView struct {
	Installation int64  `json:"installation"`
	Account      string `json:"account"`
	Type         string `json:"type"`
	Suspended    bool   `json:"suspended"`
	// ManageURL is the installation's settings on GitHub, where
	// repositories are added: "Can't find a repository?".
	ManageURL string `json:"manageURL"`
}

// GitHubAccountsView is the person's accounts, and how many installations
// the workspace has: none, and New project offers no GitHub.
type GitHubAccountsView struct {
	Accounts  []GitHubAccountView `json:"accounts"`
	Connected int                 `json:"connected"`
}
```

The cache keeps the person's listings now. Replace the `githubRepoCache` comment with "// githubRepoCache keeps each person's listings a minute, by workspace, person and installation: the picker asks on every render, GitHub counts every call." and `forgetGitHubRepos` with:

```go
// githubRepoKey is a person's listing in a workspace: one installation,
// or 0 for all they see.
func githubRepoKey(ws, provider, subject string, installation int64) string {
	return strings.Join([]string{ws, provider, subject, strconv.FormatInt(installation, 10)}, "\x00")
}

// forgetGitHubRepos drops a workspace's listings: its connections changed.
func (s *Server) forgetGitHubRepos(ws string) {
	s.githubRepos.mu.Lock()
	defer s.githubRepos.mu.Unlock()
	for k := range s.githubRepos.by {
		if strings.HasPrefix(k, ws+"\x00") {
			delete(s.githubRepos.by, k)
		}
	}
}

// forgetPersonRepos drops a person's listings: they linked or unlinked.
// An empty subject drops every listing of the provider's people.
func (s *Server) forgetPersonRepos(provider, subject string) {
	s.githubRepos.mu.Lock()
	defer s.githubRepos.mu.Unlock()
	for k := range s.githubRepos.by {
		parts := strings.Split(k, "\x00")
		if len(parts) == 4 && parts[1] == provider && (subject == "" || parts[2] == subject) {
			delete(s.githubRepos.by, k)
		}
	}
}
```

- [ ] **Step 4: The handlers**

Replace `githubRepositories` and `githubBranches` in `pkg/api/github.go` with:

```go
// githubAccounts is GET /api/workspace/github/accounts: the workspace's
// installations the person sees on GitHub, with their own token. A
// workspace without installations answers without asking for a link.
func (s *Server) githubAccounts(c *gin.Context) {
	cl, ok := s.githubClient(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	insts, err := s.store.GitHubInstallations(ctx, s.doorWorkspace(c))
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	view := GitHubAccountsView{Accounts: []GitHubAccountView{}, Connected: len(insts)}
	if len(insts) == 0 {
		c.JSON(http.StatusOK, view)
		return
	}
	tok, _, err := s.githubPersonToken(c, cl)
	if err != nil {
		githubLinkAnswer(c, err)
		return
	}
	mine, err := cl.UserInstallations(ctx, tok)
	if err != nil {
		abort(c, http.StatusBadGateway, fmt.Errorf("GitHub did not list your accounts: %w. Try again in a minute", err))
		return
	}
	sees := map[int64]bool{}
	for _, m := range mine {
		sees[m.ID] = true
	}
	web := cl.App().WebURL()
	for _, inst := range insts {
		if sees[inst.ID] {
			view.Accounts = append(view.Accounts, GitHubAccountView{Installation: inst.ID, Account: inst.AccountLogin, Type: inst.AccountType,
				Suspended: inst.SuspendedAt != nil, ManageURL: githubManageURL(web, inst)})
		}
	}
	c.JSON(http.StatusOK, view)
}

// githubRepositories is GET /api/workspace/github/repositories: what the
// person can read in the workspace's live installations (one, with
// ?installation=, or all they see), merged, sorted, kept a minute.
func (s *Server) githubRepositories(c *gin.Context) {
	cl, ok := s.githubClient(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	ws := s.doorWorkspace(c)
	insts, err := s.store.GitHubInstallations(ctx, ws)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	var only int64
	if raw := c.Query("installation"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			abort(c, http.StatusBadRequest, errors.New("installation is a number: an account's installation from /api/workspace/github/accounts"))
			return
		}
		found := false
		for _, inst := range insts {
			found = found || inst.ID == id
		}
		if !found {
			abort(c, http.StatusNotFound, fmt.Errorf("this workspace has no GitHub connection %d", id))
			return
		}
		only = id
	}
	if len(insts) == 0 {
		c.JSON(http.StatusOK, []GitHubRepositoryView{})
		return
	}
	tok, _, err := s.githubPersonToken(c, cl)
	if err != nil {
		githubLinkAnswer(c, err)
		return
	}
	provider, subject, _ := githubPersonOf(c)
	key := githubRepoKey(ws, provider, subject, only)
	now := time.Now()
	if list, ok := s.githubRepos.get(key, now); ok {
		c.JSON(http.StatusOK, list)
		return
	}
	out := []GitHubRepositoryView{}
	for _, inst := range insts {
		if inst.SuspendedAt != nil || (only != 0 && inst.ID != only) {
			continue
		}
		repos, err := cl.UserRepositories(ctx, tok, inst.ID)
		if github.IsNotFound(err) {
			// An installation the person cannot see, or one uninstalled on
			// GitHub before its webhook arrived: the others still list.
			continue
		}
		if err != nil {
			abort(c, http.StatusBadGateway, fmt.Errorf("GitHub did not list the repositories of %s: %w. Try again in a minute", inst.AccountLogin, err))
			return
		}
		for _, r := range repos {
			out = append(out, GitHubRepositoryView{Installation: r.Installation, FullName: r.FullName, Private: r.Private, DefaultBranch: r.DefaultBranch})
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].FullName) < strings.ToLower(out[j].FullName) })
	s.githubRepos.put(key, out, now)
	c.JSON(http.StatusOK, out)
}

// githubBranches is GET /api/workspace/github/repositories/:owner/:repo/branches,
// read with the person's token.
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
	reaches := false
	for _, inst := range insts {
		reaches = reaches || (inst.SuspendedAt == nil && strings.EqualFold(inst.AccountLogin, owner))
	}
	if !reaches {
		abort(c, http.StatusNotFound, fmt.Errorf("no GitHub connection of this workspace reaches %s", owner))
		return
	}
	tok, _, err := s.githubPersonToken(c, cl)
	if err != nil {
		githubLinkAnswer(c, err)
		return
	}
	names, err := cl.UserBranches(ctx, tok, owner, repo)
	var apiErr *github.APIError
	if errors.As(err, &apiErr) && (apiErr.Status == http.StatusNotFound || (apiErr.Status == http.StatusForbidden && !apiErr.RateLimited)) {
		abort(c, http.StatusNotFound, fmt.Errorf("You can't read %s/%s on GitHub, or the GitHub App's installation on %s does not include it", owner, repo, owner))
		return
	}
	if err != nil {
		abort(c, http.StatusBadGateway, fmt.Errorf("GitHub did not list the branches of %s/%s: %w", owner, repo, err))
		return
	}
	c.JSON(http.StatusOK, names)
}
```

In `pkg/api/server.go`, before `api.GET("/workspace/github/repositories", …)`:

```go
	api.GET("/workspace/github/accounts", s.require(authz.ClusterCreate), s.githubAccounts)
```

In `pkg/api/github_link.go`, `githubLinkFinish` calls `s.forgetPersonRepos(provider, subject)` right after `s.githubUsers.keep(…)`, and `githubUnlink` calls it right after `s.githubUsers.forget(provider, subject)`.

- [ ] **Step 5: Run the tests**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp TMPDIR=/home/nkr/.cache/gotmp go build ./... && GOTMPDIR=/home/nkr/.cache/gotmp TMPDIR=/home/nkr/.cache/gotmp go test ./pkg/api/`
Expected: PASS. `grep -n 'workspace/github/repositories' pkg/api/*_test.go` shows only `github_picker_test.go`.

- [ ] **Step 6: Commit**

```bash
git add pkg/api/github.go pkg/api/github_link.go pkg/api/server.go pkg/api/github_picker_test.go pkg/api/github_test.go
git commit -m "feat(api): the picker per person: their accounts, the repositories they read, branches with their token

Co-Authored-By: <your model name> <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 6: The check on create and on deploy

**Files:**
- Create: `pkg/api/github_access.go`, `pkg/api/github_access_test.go`
- Modify: `pkg/api/apps.go` (`createApp` before `s.admit` ~515; `deployApp` before `s.mutateApp` ~757 and its `case hasGit:`), `pkg/api/github_features.go` (`projectDetail`), `pkg/api/apps.go` (`AppDetail`, after `SourceNote`), `api/v1alpha1/app_types.go` (after `AnnotationGitHubReport`)

**Interfaces:**
- Consumes: `githubPersonOf`, `githubPersonToken`, `githubLinkAnswer`, `githubPerson`, `linkAs`, `personToken`, `doBearer` (Task 4); `Client.RepositoryAsApp`, `UserRepository` (Task 3).
- Produces:
  - `const shpyrdv1.AnnotationGitHubLinkedBy = "shpyrd.io/github-linked-by"`
  - `func (s *Server) githubAccess(c *gin.Context, gitURL string) (login string, ok bool)`
  - `func sameRepository(a, b string) bool`
  - `func markGitHubLinkedBy(a *shpyrdv1.App, login string)`
  - `AppDetail.GitHubLinkedBy string` (json `githubLinkedBy,omitempty`)

- [ ] **Step 1: Write the failing tests**

`pkg/api/github_access_test.go`:

```go
package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/github/githubtest"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

func createBody(slug, url string) string {
	return fmt.Sprintf(`{"name":%q,"slug":%q,"git":{"url":%q,"revision":"main"}}`, slug, slug, url)
}

func linkedBy(t *testing.T, s *Server, slug string) string {
	t.Helper()
	app, err := s.findApp(context.Background(), store.DefaultWorkspace, slug)
	if err != nil {
		t.Fatalf("%s: %v", slug, err)
	}
	return app.Annotations[shpyrdv1.AnnotationGitHubLinkedBy]
}

func TestCreatingAProjectFromAPrivateRepositoryNeedsSomeoneWhoCanReadIt(t *testing.T) {
	s, _, fake := githubServer(t)
	connectAcme(t, s, store.DefaultWorkspace)
	fake.AddUser("bob-code", githubtest.User{ID: 1002, Login: "bob", Installations: []int64{42}, Repos: []string{"Acme/site"}})
	anaSID, anaCSRF, _ := githubPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	bobSID, bobCSRF, _ := githubPerson(t, s, "bob@example.test", store.WorkspaceRoleMember)
	if rec := doCookie(t, s, "POST", "/api/projects", createBody("shop", "https://github.com/acme/shop"), anaSID, anaCSRF); rec.Code != http.StatusConflict ||
		!strings.Contains(rec.Body.String(), `"code":"github_link_required"`) {
		t.Fatalf("no link: %d %s", rec.Code, rec.Body.String())
	}
	linkAs(t, s, anaSID, anaCSRF, githubtest.Code)
	linkAs(t, s, bobSID, bobCSRF, "bob-code")
	if rec := doCookie(t, s, "POST", "/api/projects", createBody("shop", "https://github.com/acme/shop"), bobSID, bobCSRF); rec.Code != http.StatusForbidden ||
		!strings.Contains(rec.Body.String(), "You can't read acme/shop on GitHub") {
		t.Errorf("bob: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doCookie(t, s, "POST", "/api/projects", createBody("vault", "https://github.com/acme/vault"), anaSID, anaCSRF); rec.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(rec.Body.String(), "does not include acme/vault") {
		t.Errorf("a repository outside the installation: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doCookie(t, s, "POST", "/api/projects", createBody("shop", "git@github.com:Acme/shop.git"), anaSID, anaCSRF); rec.Code != http.StatusCreated {
		t.Fatalf("ana: %d %s", rec.Code, rec.Body.String())
	}
	if got := linkedBy(t, s, "shop"); got != githubtest.UserLogin {
		t.Errorf("linked by %q", got)
	}
	// Not GitHub's to check: another host, and an owner the workspace has
	// no connection on (a deploy key or token reads it, as before).
	for slug, url := range map[string]string{"gl": "https://gitlab.com/acme/shop", "other": "https://github.com/other/private"} {
		if rec := doCookie(t, s, "POST", "/api/projects", createBody(slug, url), bobSID, bobCSRF); rec.Code != http.StatusCreated {
			t.Errorf("%s: %d %s", url, rec.Code, rec.Body.String())
		}
	}
}

func TestAPublicRepositoryNeedsNoLink(t *testing.T) {
	s, _, fake := githubServer(t)
	connectAcme(t, s, store.DefaultWorkspace)
	sid, csrf, _ := githubPerson(t, s, "cleo@example.test", store.WorkspaceRoleMember)
	if rec := doCookie(t, s, "POST", "/api/projects", createBody("site", "https://github.com/acme/site"), sid, csrf); rec.Code != http.StatusCreated {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if got := linkedBy(t, s, "site"); got != "" {
		t.Errorf("linked by %q", got)
	}
	for _, call := range fake.Calls() {
		if strings.HasPrefix(call, "GET /user") || call == "POST /login/oauth/access_token" {
			t.Errorf("asked GitHub as the person: %s", call)
		}
	}
}

func TestAnAPITokenCreatesWithItsMakersLink(t *testing.T) {
	s, _, _ := githubServer(t)
	connectAcme(t, s, store.DefaultWorkspace)
	anaSID, anaCSRF, _ := githubPerson(t, s, "ana@example.test", store.WorkspaceRoleMember)
	bobSID, bobCSRF, _ := githubPerson(t, s, "bob@example.test", store.WorkspaceRoleMember)
	linkAs(t, s, anaSID, anaCSRF, githubtest.Code)
	if rec := doBearer(t, s, "POST", "/api/projects", createBody("shop", "https://github.com/acme/shop"), personToken(t, s, anaSID, anaCSRF)); rec.Code != http.StatusCreated {
		t.Fatalf("ana's agent: %d %s", rec.Code, rec.Body.String())
	}
	if got := linkedBy(t, s, "shop"); got != githubtest.UserLogin {
		t.Errorf("linked by %q", got)
	}
	if rec := doBearer(t, s, "POST", "/api/projects", createBody("shop2", "https://github.com/acme/shop"), personToken(t, s, bobSID, bobCSRF)); rec.Code != http.StatusConflict ||
		!strings.Contains(rec.Body.String(), "github_link_required") {
		t.Errorf("bob's agent, bob unlinked: %d %s", rec.Code, rec.Body.String())
	}
	// The admin token is the operator: not checked.
	if rec := do(t, s, "POST", "/api/projects", createBody("shop3", "https://github.com/acme/shop"), true); rec.Code != http.StatusCreated {
		t.Errorf("the admin token: %d %s", rec.Code, rec.Body.String())
	}
}

func TestADeployChecksOnlyAChangedRepository(t *testing.T) {
	s, _, fake := githubServer(t)
	connectAcme(t, s, store.DefaultWorkspace)
	fake.AddUser("bob-code", githubtest.User{ID: 1002, Login: "bob", Installations: []int64{42}, Repos: []string{"Acme/site"}})
	anaSID, anaCSRF, _ := githubPerson(t, s, "ana@example.test", store.WorkspaceRoleAdmin)
	bobSID, bobCSRF, _ := githubPerson(t, s, "bob@example.test", store.WorkspaceRoleMember)
	linkAs(t, s, anaSID, anaCSRF, githubtest.Code)
	linkAs(t, s, bobSID, bobCSRF, "bob-code")
	if rec := doCookie(t, s, "POST", "/api/projects", createBody("site", "https://github.com/acme/site"), bobSID, bobCSRF); rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	deploy := func(sid, csrf, url string) *httptest.ResponseRecorder {
		return doCookie(t, s, "POST", "/api/projects/site/deploy", fmt.Sprintf(`{"git":{"url":%q,"revision":"main"}}`, url), sid, csrf)
	}
	if rec := deploy(bobSID, bobCSRF, "https://github.com/acme/shop"); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "You can't read acme/shop on GitHub") {
		t.Errorf("bob moves it to shop: %d %s", rec.Code, rec.Body.String())
	}
	if rec := deploy(anaSID, anaCSRF, "https://github.com/acme/shop"); rec.Code != http.StatusAccepted {
		t.Fatalf("ana moves it to shop: %d %s", rec.Code, rec.Body.String())
	}
	if got := linkedBy(t, s, "site"); got != githubtest.UserLogin {
		t.Errorf("linked by %q", got)
	}
	// A redeploy of the same repository is not checked: bob cannot read
	// shop, and redeploys it.
	if rec := deploy(bobSID, bobCSRF, "https://github.com/ACME/shop.git"); rec.Code != http.StatusAccepted {
		t.Errorf("bob redeploys: %d %s", rec.Code, rec.Body.String())
	}
	if got := linkedBy(t, s, "site"); got != githubtest.UserLogin {
		t.Errorf("a redeploy changed who linked it: %q", got)
	}
	// Back to the public site, no link needed: the record goes.
	if rec := deploy(bobSID, bobCSRF, "https://github.com/acme/site"); rec.Code != http.StatusAccepted {
		t.Errorf("bob back to site: %d %s", rec.Code, rec.Body.String())
	}
	if got := linkedBy(t, s, "site"); got != "" {
		t.Errorf("linked by %q after a public repository", got)
	}
}

func TestSameRepositoryIgnoresFormAndCase(t *testing.T) {
	for _, c := range []struct {
		a, b string
		same bool
	}{
		{"https://github.com/acme/shop", "git@github.com:ACME/Shop.git", true},
		{"https://github.com/acme/shop", "ssh://git@github.com/acme/shop.git", true},
		{"https://github.com/acme/shop", "https://github.com/acme/site", false},
		{"https://github.com/acme/shop", "https://gitlab.com/acme/shop", false},
	} {
		if got := sameRepository(c.a, c.b); got != c.same {
			t.Errorf("%s ~ %s: %v", c.a, c.b, got)
		}
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp TMPDIR=/home/nkr/.cache/gotmp go test ./pkg/api/ -run 'NeedsSomeoneWhoCanReadIt|PublicRepositoryNeedsNoLink|CreatesWithItsMakersLink|ChecksOnlyAChanged|SameRepository'`
Expected: FAIL to compile: `undefined: shpyrdv1.AnnotationGitHubLinkedBy`, `undefined: sameRepository`.

- [ ] **Step 3: The annotation**

In `api/v1alpha1/app_types.go`, after `AnnotationGitHubReport = "shpyrd.io/github-report"`:

```go
	// AnnotationGitHubLinkedBy is the GitHub login whose access picked the
	// App's repository (RFC-0054, GitHub access by person): for the trail
	// and the dashboard ("Connected by …"); nothing depends on it.
	AnnotationGitHubLinkedBy = "shpyrd.io/github-linked-by"
```

- [ ] **Step 4: The check**

`pkg/api/github_access.go`:

```go
package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
	"github.com/shpyrd-io/shpyrd/pkg/github"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// githubAccessTimeout bounds the two calls the check makes.
const githubAccessTimeout = 15 * time.Second

// githubAccess checks that whoever the request acts as (the person, or
// for an API token its maker) can read the GitHub repository a project is
// made from or moved to (RFC-0054). It answers the request itself when
// they cannot, and says not ok. login is the GitHub account that could
// read it; "" when no check applied: a repository not on github.com, an
// owner the workspace has no live connection on, a platform without the
// App, a public repository, the admin token.
func (s *Server) githubAccess(c *gin.Context, gitURL string) (string, bool) {
	repo, err := gitauth.Parse(gitURL)
	if err != nil || !strings.EqualFold(repo.Host, "github.com") {
		return "", true
	}
	owner, name, found := strings.Cut(strings.TrimSuffix(repo.Path, ".git"), "/")
	if !found || strings.Contains(name, "/") {
		return "", true
	}
	ctx := c.Request.Context()
	cl, err := s.githubApp.Client(ctx)
	if err != nil {
		// A bare ErrNotConfigured is no App at all, the usual case; a broken
		// Secret wraps it, and is worth a line. Builds then use the deploy
		// key or the token, so nothing is reached through the App.
		if err != github.ErrNotConfigured { //nolint:errorlint // identity, not a wrapped error, is the point
			s.log.Warn("github: the App cannot be used; the repository is not checked", "err", err.Error())
		}
		return "", true
	}
	insts, err := s.store.GitHubInstallations(ctx, s.doorWorkspace(c))
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return "", false
	}
	var inst *store.GitHubInstallation
	for i := range insts {
		if strings.EqualFold(insts[i].AccountLogin, owner) && insts[i].SuspendedAt == nil {
			inst = &insts[i]
		}
	}
	if inst == nil {
		return "", true
	}
	if _, _, person := githubPersonOf(c); !person {
		return "", true
	}
	callCtx, cancel := context.WithTimeout(ctx, githubAccessTimeout)
	defer cancel()
	info, err := cl.RepositoryAsApp(callCtx, inst.ID, owner, name)
	switch {
	case github.IsNotFound(err):
		abort(c, http.StatusUnprocessableEntity, fmt.Errorf("The GitHub App's installation on %s does not include %s/%s, or it does not exist: add it to the installation on GitHub, or give shpyrd a deploy key (shpyrd git-auth deploy-key)", inst.AccountLogin, owner, name))
		return "", false
	case err != nil:
		abort(c, http.StatusBadGateway, fmt.Errorf("shpyrd could not ask GitHub about %s/%s: %w. Try again in a minute", owner, name, err))
		return "", false
	case !info.Private:
		return "", true
	}
	tok, login, err := s.githubPersonToken(c, cl)
	if err != nil {
		githubLinkAnswer(c, err)
		return "", false
	}
	_, err = cl.UserRepository(callCtx, tok, owner, name)
	var apiErr *github.APIError
	if errors.As(err, &apiErr) && (apiErr.Status == http.StatusNotFound || (apiErr.Status == http.StatusForbidden && !apiErr.RateLimited)) {
		abort(c, http.StatusForbidden, fmt.Errorf("You can't read %s/%s on GitHub", owner, name))
		return "", false
	}
	if err != nil {
		abort(c, http.StatusBadGateway, fmt.Errorf("shpyrd could not ask GitHub about %s/%s: %w. Try again in a minute", owner, name, err))
		return "", false
	}
	return login, true
}

// sameRepository says two git URLs name one repository: the same host and
// path, in any case, in any form.
func sameRepository(a, b string) bool {
	ra, errA := gitauth.Parse(a)
	rb, errB := gitauth.Parse(b)
	if errA != nil || errB != nil {
		return strings.TrimSpace(a) == strings.TrimSpace(b)
	}
	return strings.EqualFold(ra.Host, rb.Host) && strings.EqualFold(ra.Path, rb.Path)
}

// markGitHubLinkedBy records whose GitHub access picked the repository;
// "" removes the record.
func markGitHubLinkedBy(a *shpyrdv1.App, login string) {
	if login == "" {
		delete(a.Annotations, shpyrdv1.AnnotationGitHubLinkedBy)
		return
	}
	if a.Annotations == nil {
		a.Annotations = map[string]string{}
	}
	a.Annotations[shpyrdv1.AnnotationGitHubLinkedBy] = login
}
```

- [ ] **Step 5: Create and deploy use it**

In `pkg/api/apps.go`, `createApp`, after the `!validAccess(access)` check:

```go
	// From a GitHub repository: someone who can read it (RFC-0054).
	var linkedBy string
	if req.Git != nil && req.Git.URL != "" {
		var ok bool
		if linkedBy, ok = s.githubAccess(c, req.Git.URL); !ok {
			return
		}
	}
```

and after `project.SetDisplayName(app, req.Name)`: `markGitHubLinkedBy(app, linkedBy)`.

`deployApp`, after the `if req.Allow != nil { … }` block:

```go
	// Moving the project to another repository: someone who can read it
	// (RFC-0054). A redeploy of the same repository is not checked.
	var linkedBy string
	moved := false
	if hasGit {
		cur, ok := s.loadApp(c)
		if !ok {
			return
		}
		if cur.Spec.Source == nil || cur.Spec.Source.Git == nil || !sameRepository(cur.Spec.Source.Git.URL, req.Git.URL) {
			if linkedBy, ok = s.githubAccess(c, req.Git.URL); !ok {
				return
			}
			moved = true
		}
	}
```

and inside `s.mutateApp`, `case hasGit:`, after `a.Spec.Source = &shpyrdv1.Source{Git: req.Git, SubPath: req.SubPath}`:

```go
			if moved {
				markGitHubLinkedBy(a, linkedBy)
			}
```

`AppDetail` (after `SourceNote`):

```go
	// GitHubLinkedBy is the GitHub account whose access picked the
	// repository (RFC-0054): "Connected by …".
	GitHubLinkedBy string `json:"githubLinkedBy,omitempty"`
```

and in `pkg/api/github_features.go`, `projectDetail`, before `return d`: `d.GitHubLinkedBy = app.Annotations[shpyrdv1.AnnotationGitHubLinkedBy]`.

- [ ] **Step 6: Run the tests**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp TMPDIR=/home/nkr/.cache/gotmp go build ./... && GOTMPDIR=/home/nkr/.cache/gotmp TMPDIR=/home/nkr/.cache/gotmp go test ./pkg/api/ ./api/...`
Expected: PASS. Tests that create projects from github.com URLs with the admin token, or on a platform without the App, still pass: neither is checked.

- [ ] **Step 7: Commit**

```bash
git add pkg/api/github_access.go pkg/api/github_access_test.go pkg/api/apps.go pkg/api/github_features.go api/v1alpha1/app_types.go
git commit -m "feat(api): a project is made from a GitHub repository only by someone who can read it; public ones need no link

Co-Authored-By: <your model name> <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 7: The CLI: `github link`, `github unlink`, `whoami`, and what to run when a link is needed

**Files:**
- Modify: `internal/cli/github.go` (`newGitHubCmd`: `link`, `unlink`, the `Long` text; `githubLinkRequired`), `internal/cli/login.go` (`newWhoAmICmd`; new `githubOf`), `internal/cli/registry.go` (`serverRequestDirect`)
- Create: `internal/cli/github_link_test.go`

**Interfaces:**
- Consumes: `GET/DELETE /api/me/github`, `GET /api/me/github/link` (Task 4); `api.GitHubLinkView` (Task 4); the 409 `{"code":"github_link_required"}` (Tasks 4-6); `fastGitHubPolls`, `runShpyrd` (exist in `github_test.go`, `githooks_test.go`).
- Produces: `shpyrd github link [--no-browser]`, `shpyrd github unlink`; `shpyrd whoami` prints `GitHub: <login>` or `GitHub: not linked (shpyrd github link)` (JSON field `github`); `func githubLinkRequired(status int, raw []byte) error`; `func githubOf(ctx context.Context, wsURL, token string) (string, bool)`; `const githubNotLinked = "no GitHub account is linked"`.

- [ ] **Step 1: Write the failing tests**

`internal/cli/github_link_test.go`:

```go
package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const paezaoLinked = `{"login":"paezao","linkedAt":"2026-10-17T10:00:00Z"}`

func notLinked(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(`{"error":"no GitHub account is linked: shpyrd github link"}`))
}

func TestGitHubLinkOpensTheLinkPageAndWaitsForIt(t *testing.T) {
	opened := fastGitHubPolls(t)
	var reads atomic.Int32
	out, err := runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/me/github":
			if reads.Add(1) <= 3 {
				notLinked(w)
				return
			}
			_, _ = w.Write([]byte(paezaoLinked))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}, "github", "link")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if len(*opened) != 1 || !strings.HasSuffix((*opened)[0], "/api/me/github/link") || !strings.HasPrefix((*opened)[0], "http://127.0.0.1:") {
		t.Errorf("opened %v", *opened)
	}
	if !strings.Contains(out, "Continue with GitHub there; waiting for the link") || !strings.Contains(out, "Linked GitHub account paezao") {
		t.Errorf("output:\n%s", out)
	}
}

func TestGitHubLinkSaysWhenAnAccountIsLinkedAlready(t *testing.T) {
	opened := fastGitHubPolls(t)
	out, err := runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(paezaoLinked))
	}, "github", "link")
	if err != nil || !strings.Contains(out, "Your GitHub account is linked: paezao") || len(*opened) != 0 {
		t.Errorf("%v %v\n%s", err, *opened, out)
	}
}

func TestGitHubLinkGivesUpAfterTheWait(t *testing.T) {
	fastGitHubPolls(t)
	githubConnectWait = 30 * time.Millisecond
	_, err := runShpyrd(t, func(w http.ResponseWriter, r *http.Request) { notLinked(w) }, "github", "link")
	if err == nil || !strings.Contains(err.Error(), "run shpyrd github link again") {
		t.Errorf("err = %v", err)
	}
}

func TestGitHubUnlink(t *testing.T) {
	var deleted atomic.Bool
	out, err := runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/me/github":
			_, _ = w.Write([]byte(paezaoLinked))
		case "DELETE /api/me/github":
			deleted.Store(true)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}, "github", "unlink")
	if err != nil || !deleted.Load() || !strings.Contains(out, "Unlinked paezao") || !strings.Contains(out, "keep deploying") {
		t.Errorf("%v\n%s", err, out)
	}
	if _, err := runShpyrd(t, func(w http.ResponseWriter, r *http.Request) { notLinked(w) }, "github", "unlink"); err == nil || !strings.Contains(err.Error(), "no GitHub account is linked") {
		t.Errorf("nothing linked: %v", err)
	}
}

func TestALinkRequiredAnswerSaysToRunGitHubLink(t *testing.T) {
	_, err := runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"Link your GitHub account first: Continue with GitHub in New project, or run shpyrd github link","code":"github_link_required"}`))
	}, "github", "repos")
	if err == nil || err.Error() != "link your GitHub account first: run shpyrd github link" {
		t.Errorf("err = %v", err)
	}
	// Another 409 keeps the server's words.
	_, err = runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"GitHub is not set up on this platform."}`))
	}, "github", "repos")
	if err == nil || !strings.Contains(err.Error(), "GitHub is not set up") {
		t.Errorf("err = %v", err)
	}
}

func TestWhoAmIShowsTheLinkedGitHubAccount(t *testing.T) {
	for _, c := range []struct {
		name   string
		github func(w http.ResponseWriter)
		want   string
	}{
		{"linked", func(w http.ResponseWriter) { _, _ = w.Write([]byte(paezaoLinked)) }, "GitHub: paezao"},
		{"not linked", notLinked, "GitHub: not linked (shpyrd github link)"},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/me":
					_, _ = w.Write([]byte(`{"email":"ana@acme.test"}`))
				case "/api/me/github":
					c.github(w)
				default:
					t.Errorf("unexpected %s", r.URL.Path)
				}
			}))
			defer srv.Close()
			t.Setenv("HOME", t.TempDir())
			t.Setenv("KUBECONFIG", "")
			t.Setenv("SHPYRD_URL", srv.URL)
			t.Setenv("SHPYRD_TOKEN", "")
			norm, err := normaliseURL(srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			s := &loginSessions{Current: norm, Sessions: map[string]*loginSession{norm: {URL: norm, Token: "shp_test", SavedAt: time.Now()}}}
			if err := s.save(); err != nil {
				t.Fatal(err)
			}
			root := New()
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&out)
			root.SetArgs([]string{"whoami"})
			if err := root.Execute(); err != nil {
				t.Fatalf("%v\n%s", err, out.String())
			}
			if !strings.Contains(out.String(), "ana@acme.test") || !strings.Contains(out.String(), c.want) {
				t.Errorf("output:\n%s", out.String())
			}
		})
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp TMPDIR=/home/nkr/.cache/gotmp go test ./internal/cli/ -run 'GitHubLink|GitHubUnlink|LinkRequiredAnswer|WhoAmIShowsTheLinked'`
Expected: FAIL: `unknown command "link" for "shpyrd github"`, the whoami output lacks `GitHub:`.

- [ ] **Step 3: The link-required error**

In `internal/cli/github.go`, add the imports `"encoding/json"` and `"net/http"`, and:

```go
// githubNotLinked starts the server's 404 when no GitHub account is linked.
const githubNotLinked = "no GitHub account is linked"

// githubLinkRequired turns the server's github_link_required answer into
// what to run; nil for any other answer.
func githubLinkRequired(status int, raw []byte) error {
	if status != http.StatusConflict {
		return nil
	}
	var body struct {
		Code string `json:"code"`
	}
	if json.Unmarshal(raw, &body) != nil || body.Code != "github_link_required" {
		return nil
	}
	return errors.New("link your GitHub account first: run shpyrd github link")
}
```

In `internal/cli/registry.go`, `serverRequestDirect`, before `if resp.StatusCode >= 400 {`:

```go
	if err := githubLinkRequired(resp.StatusCode, raw); err != nil {
		return nil, err
	}
```

- [ ] **Step 4: `github link` and `github unlink`**

In `newGitHubCmd`, the `Short` becomes `"GitHub: link your account, connect accounts, pick repositories; every push deploys"` and the `Long`:

```go
		Long: `Link your own GitHub account once: New project, the CLI and your agents then
list and use the repositories you can read on GitHub. A workspace admin
connects an account or organisation once; private repositories then build
without keys or tokens, and every push to a project's branch deploys.

  shpyrd github link                  open the browser to link your GitHub account, and wait for it
  shpyrd github unlink                unlink it (your projects keep deploying)
  shpyrd github connect               open the dashboard to connect, and wait for it
  shpyrd github list                  the workspace's connections
  shpyrd github disconnect <account>  forget one (the App stays installed on GitHub)
  shpyrd github repos [--filter TEXT] the repositories you can read through the connections

Then: shpyrd deploy --git https://github.com/<owner>/<repo> --ref <branch>`,
```

Before `cmd.AddCommand(connect, list, disconnect, repos)`:

```go
	var linkNoBrowser bool
	link := &cobra.Command{
		Use:   "link",
		Short: "Link your GitHub account in the browser, and wait for it",
		RunE: func(cmd *cobra.Command, args []string) error {
			var me api.GitHubLinkView
			err := githubRequest(g, cmd, "GET", "api/me/github", &me)
			if err == nil {
				return g.print(cmd, me, func(w io.Writer) {
					fmt.Fprintf(w, "Your GitHub account is linked: %s. To link another, run shpyrd github unlink first.\n", me.Login)
				})
			}
			if !strings.Contains(err.Error(), githubNotLinked) {
				return err
			}
			// The link finishes in the browser, signed in there: the page
			// goes on to GitHub, and the CLI waits for the link to appear.
			page := workspaceURL() + "/api/me/github/link"
			out := g.progress(cmd)
			if !linkNoBrowser && browse(page) == nil {
				fmt.Fprintf(out, "Opened %s (if nothing opened, open it yourself).\n", page)
			} else {
				fmt.Fprintf(out, "Open %s\n", page)
			}
			fmt.Fprintln(out, "Continue with GitHub there; waiting for the link…")
			ctx := signalContext()
			deadline := time.Now().Add(githubConnectWait)
			failures := 0
			for {
				select {
				case <-ctx.Done():
					return errors.New("cancelled; a link made in the browser still counts: shpyrd whoami")
				case <-time.After(githubPollEvery):
				}
				var now api.GitHubLinkView
				err := githubRequest(g, cmd, "GET", "api/me/github", &now)
				switch {
				case err == nil:
					return g.print(cmd, now, func(w io.Writer) {
						fmt.Fprintf(w, "Linked GitHub account %s. The repositories it can read: shpyrd github repos\n", now.Login)
					})
				case strings.Contains(err.Error(), githubNotLinked):
					failures = 0
				default:
					// A blip does not end a long wait: three in a row do.
					if failures++; failures > 3 {
						return err
					}
				}
				if time.Now().After(deadline) {
					return errors.New("not linked after 10 minutes: run shpyrd github link again")
				}
			}
		},
	}
	link.Flags().BoolVar(&linkNoBrowser, "no-browser", false, "print the page link instead of opening it")

	unlink := &cobra.Command{
		Use:   "unlink",
		Short: "Unlink your GitHub account; your projects keep deploying",
		RunE: func(cmd *cobra.Command, args []string) error {
			var me api.GitHubLinkView
			if err := githubRequest(g, cmd, "GET", "api/me/github", &me); err != nil {
				if strings.Contains(err.Error(), githubNotLinked) {
					return errors.New(githubNotLinked)
				}
				return err
			}
			if err := githubRequest(g, cmd, "DELETE", "api/me/github", nil); err != nil {
				return err
			}
			return g.print(cmd, map[string]string{"unlinked": me.Login}, func(w io.Writer) {
				fmt.Fprintf(w, "Unlinked %s; its token is revoked on GitHub. Your projects keep deploying; picking a private repository needs a link again.\n", me.Login)
			})
		},
	}
```

and the line becomes `cmd.AddCommand(link, unlink, connect, list, disconnect, repos)`. The `repos` command's `Short` becomes "List the repositories you can read through the workspace's connections", and its empty answer: "No repository matches. You see what your GitHub account can read in the connected accounts: shpyrd github link, or ask for access on GitHub."

- [ ] **Step 5: whoami**

In `internal/cli/login.go`, after `whoAmI`:

```go
// githubOf is the GitHub account linked to the token's person (RFC-0054):
// its login, or "" when none is linked; known is false when the server
// did not say.
func githubOf(ctx context.Context, wsURL, token string) (string, bool) {
	req, err := http.NewRequestWithContext(ctx, "GET", wsURL+"/api/me/github", nil)
	if err != nil {
		return "", false
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNotFound:
		return "", true
	case http.StatusOK:
		var v struct {
			Login string `json:"login"`
		}
		if json.NewDecoder(resp.Body).Decode(&v) != nil {
			return "", false
		}
		return v.Login, true
	}
	return "", false
}
```

In `newWhoAmICmd`'s `RunE`, replace from `me := whoAmI(ctx, norm, sess.Token)` to the end of the function with:

```go
			me := whoAmI(ctx, norm, sess.Token)
			if me == "" {
				// The admin token has no person behind it.
				me = "admin token"
			}
			view := map[string]string{"url": norm, "user": me}
			ghLogin, known := "", false
			if me != "admin token" {
				ghLogin, known = githubOf(ctx, norm, sess.Token)
			}
			if known {
				view["github"] = ghLogin
			}
			return g.print(cmd, view, func(w io.Writer) {
				fmt.Fprintln(w, me)
				switch {
				case !known:
				case ghLogin == "":
					fmt.Fprintln(w, "GitHub: not linked (shpyrd github link)")
				default:
					fmt.Fprintf(w, "GitHub: %s\n", ghLogin)
				}
			})
```

- [ ] **Step 6: Run the tests**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp TMPDIR=/home/nkr/.cache/gotmp go build ./... && GOTMPDIR=/home/nkr/.cache/gotmp TMPDIR=/home/nkr/.cache/gotmp go test ./internal/cli/`
Expected: PASS, the existing `github` command tests included.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/github.go internal/cli/login.go internal/cli/registry.go internal/cli/github_link_test.go
git commit -m "feat(cli): shpyrd github link and unlink, whoami's GitHub line, and what to run when a link is needed

Co-Authored-By: <your model name> <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 8: The dashboard: the link step, the account switcher, the person menu

**Files:**
- Modify: `apps/shared/src/api/error.ts`, `apps/shared/src/api/http.ts` (`handle`)
- Modify: `apps/workspace/src/api/types.ts` (after `GitHubRepository`), `api.ts` (the GitHub calls ~165-173), `backend.ts` (~282-289), `mock.ts` (`Things`, the GitHub handlers ~788-822), `mock/things.json` (`githubLink`, a repository of acme-labs), `api.test.ts` (the GitHub Mock test)
- Create: `apps/workspace/src/lib/github-link.ts`, `apps/workspace/src/lib/github-link.test.ts`, `apps/workspace/src/api/http-error.test.ts`, `apps/workspace/src/shell/person.test.tsx`
- Modify: `apps/workspace/src/screens/github-picker.tsx`, `github-picker.test.tsx`, `launcher.tsx` (`NewProject`), `launcher.test.tsx`, `apps/workspace/src/shell/person.tsx`

**Interfaces:**
- Consumes: `GET/POST/DELETE /api/me/github[/link]` (Task 4), `GET /api/workspace/github/accounts`, `repositories?installation=` (Task 5), the 409 code (Tasks 4-6).
- Produces:
  - `ApiError.code?: string`
  - Types `GitHubAccount = { installation: number; account: string; type: "Organization" | "User"; suspended: boolean; manageURL: string }`, `GitHubAccounts = { accounts: GitHubAccount[]; connected: number }`, `GitHubLink = { login: string; linkedAt: string }`
  - `Api.githubMe(): Promise<GitHubLink | null>`, `linkGitHub(returnTo: string): Promise<{ authorizeURL: string }>` (an empty `authorizeURL`: linked already, the Mock's answer), `unlinkGitHub(): Promise<void>`, `githubAccounts(): Promise<GitHubAccounts>`, `githubRepositories(installation?: number): Promise<GitHubRepository[]>`
  - `linkGitHub(): Promise<void>` in `@/lib/github-link`; `const LINK_REQUIRED = "github_link_required"`
  - `GitHubPicker({ accounts, value, onChange, canAdd })`, `GitHubLinkStep({ onLinked })`
  - Query keys: `["github-me"]`, `["github-accounts"]`, `["github-repositories", installation]`

- [ ] **Step 1: Write the failing tests**

`apps/workspace/src/api/http-error.test.ts`:

```ts
import { afterEach, expect, it, vi } from "vitest";
import { ApiError } from "@shpyrd/shared/api/error";
import { request } from "@shpyrd/shared/api/http";

afterEach(() => vi.unstubAllGlobals());

it("carries the server's code with its words on a refusal", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response(JSON.stringify({ error: "Link your GitHub account first", code: "github_link_required" }), { status: 409 })),
  );
  const err = await request("/api/workspace/github/accounts").catch((e) => e);
  expect(err).toBeInstanceOf(ApiError);
  expect(err.status).toBe(409);
  expect(err.code).toBe("github_link_required");
  expect(err.message).toBe("Link your GitHub account first");
});
```

`apps/workspace/src/lib/github-link.test.ts`:

```ts
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { linkGitHub } from "./github-link";

const fake = vi.hoisted(() => ({ linkGitHub: vi.fn(), githubMe: vi.fn() }));
vi.mock("@/api/api", () => ({ api: fake }));

beforeEach(() => fake.githubMe.mockResolvedValue(null));
afterEach(() => {
  vi.restoreAllMocks();
  vi.resetAllMocks();
  vi.useRealTimers();
});

it("is done at once when the server says the account is linked already", async () => {
  fake.linkGitHub.mockResolvedValue({ authorizeURL: "" });
  const open = vi.spyOn(window, "open");
  await linkGitHub();
  expect(open).not.toHaveBeenCalled();
});

it("opens GitHub in a popup and is done when the popup says so", async () => {
  fake.linkGitHub.mockResolvedValue({ authorizeURL: "https://github.test/login/oauth/authorize?state=s" });
  const open = vi.spyOn(window, "open").mockReturnValue({ closed: false } as Window);
  const done = linkGitHub();
  await vi.waitFor(() => expect(open).toHaveBeenCalledWith("https://github.test/login/oauth/authorize?state=s", "shpyrd-github-link", expect.stringContaining("popup")));
  window.dispatchEvent(new MessageEvent("message", { data: { github: "linked" }, origin: window.location.origin }));
  await expect(done).resolves.toBeUndefined();
});

it("ignores a message from another origin, and is done once the server shows the link", async () => {
  vi.useFakeTimers();
  fake.linkGitHub.mockResolvedValue({ authorizeURL: "https://github.test/authorize" });
  vi.spyOn(window, "open").mockReturnValue({ closed: false } as Window);
  let settled = false;
  const done = linkGitHub().then(() => (settled = true));
  await vi.advanceTimersByTimeAsync(0);
  window.dispatchEvent(new MessageEvent("message", { data: { github: "linked" }, origin: "https://evil.test" }));
  await vi.advanceTimersByTimeAsync(2_000);
  expect(settled).toBe(false);
  fake.githubMe.mockResolvedValue({ login: "paezao", linkedAt: "2026-10-17T10:00:00Z" });
  await vi.advanceTimersByTimeAsync(2_000);
  await done;
  expect(settled).toBe(true);
});
```

Replace `apps/workspace/src/screens/github-picker.test.tsx` with:

```tsx
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState } from "react";
import type { GitHubAccount, GitHubRepository } from "@/api/types";
import { branchesInOrder, GitHubLinkStep, GitHubPicker, type Picked, slugFromRepo } from "./github-picker";

const fake = vi.hoisted(() => ({ githubBranches: vi.fn(), githubRepositories: vi.fn(), connectGitHub: vi.fn(), linkGitHub: vi.fn(), githubMe: vi.fn() }));
vi.mock("@/api/api", () => ({ api: fake }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

const acme: GitHubAccount = { installation: 42, account: "acme", type: "Organization", suspended: false, manageURL: "https://github.com/organizations/acme/settings/installations/42" };
const labs: GitHubAccount = { installation: 77, account: "acme-labs", type: "Organization", suspended: false, manageURL: "https://github.com/organizations/acme-labs/settings/installations/77" };
const repos: Record<number, GitHubRepository[]> = {
  42: [
    { installation: 42, fullName: "acme/hello-world", private: false, defaultBranch: "main" },
    { installation: 42, fullName: "acme/billing", private: true, defaultBranch: "trunk" },
  ],
  77: [{ installation: 77, fullName: "acme-labs/ml", private: true, defaultBranch: "main" }],
};

let queries: QueryClient;
let last: Picked | undefined;
function Harness({ accounts, canAdd = true }: { accounts: GitHubAccount[]; canAdd?: boolean }) {
  const [value, setValue] = useState<Picked>();
  last = value;
  return <GitHubPicker accounts={accounts} value={value} onChange={setValue} canAdd={canAdd} />;
}
function show(accounts: GitHubAccount[] = [acme], canAdd = true) {
  fake.githubRepositories.mockImplementation(async (installation: number) => repos[installation] ?? []);
  queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queries}>
      <Harness accounts={accounts} canAdd={canAdd} />
    </QueryClientProvider>,
  );
}
// Radix's Select scrolls its options into view; jsdom has no scrolling.
beforeEach(() => {
  Object.defineProperty(HTMLElement.prototype, "scrollIntoView", { configurable: true, value: vi.fn() });
});
afterEach(() => {
  cleanup();
  queries?.clear();
  vi.restoreAllMocks();
  vi.resetAllMocks();
  last = undefined;
});

it("lists the repositories of the account and filters them as one types, in any case", async () => {
  show();
  expect(await screen.findByRole("button", { name: /acme\/billing/ })).toBeTruthy();
  expect(fake.githubRepositories).toHaveBeenCalledWith(42);
  fireEvent.change(screen.getByRole("textbox", { name: "Filter the repositories" }), { target: { value: "BILL" } });
  expect(screen.queryByRole("button", { name: /acme\/hello-world/ })).toBeNull();
  fireEvent.change(screen.getByRole("textbox", { name: "Filter the repositories" }), { target: { value: "nothing" } });
  expect(screen.getByText("No repository matches.")).toBeTruthy();
});

it("picks a repository on its default branch, then offers its branches with the default first", async () => {
  fake.githubBranches.mockResolvedValue(["feature", "trunk", "hotfix"]);
  show();
  fireEvent.click(await screen.findByRole("button", { name: /acme\/billing/ }));
  expect(last).toEqual({ fullName: "acme/billing", branch: "trunk" });
  await waitFor(() => expect(fake.githubBranches).toHaveBeenCalledWith("acme/billing"));
  expect(await screen.findByRole("combobox", { name: "Branch" })).toBeTruthy();
});

it("switches the account, and lists that account's repositories with nothing picked", async () => {
  show([acme, labs]);
  fireEvent.click(await screen.findByRole("button", { name: /acme\/billing/ }));
  expect(last?.fullName).toBe("acme/billing");
  fireEvent.click(screen.getByRole("combobox", { name: "GitHub account" }));
  fireEvent.click(await screen.findByRole("option", { name: "acme-labs" }));
  expect(await screen.findByRole("button", { name: /acme-labs\/ml/ })).toBeTruthy();
  expect(fake.githubRepositories).toHaveBeenCalledWith(77);
  expect(last).toBeUndefined();
});

it("sends one who cannot find a repository to the installation's settings on GitHub", async () => {
  const open = vi.spyOn(window, "open").mockReturnValue(null);
  show();
  fireEvent.click(await screen.findByRole("button", { name: "Can't find a repository?" }));
  expect(open).toHaveBeenCalledWith(acme.manageURL, "_blank", "noopener");
});

it("adds an account through Connect for a workspace admin, and says who adds them to anyone else", async () => {
  fake.connectGitHub.mockResolvedValue({ installURL: "https://github.test/apps/shpyrd/installations/new?state=s", authorizeURL: "" });
  const open = vi.spyOn(window, "open").mockReturnValue(null);
  show([acme], true);
  fireEvent.click(await screen.findByRole("button", { name: "Add GitHub account or organisation" }));
  await waitFor(() => expect(open).toHaveBeenCalledWith("https://github.test/apps/shpyrd/installations/new?state=s", "shpyrd-github-install", expect.stringContaining("popup")));
  cleanup();
  show([acme], false);
  expect(await screen.findByText("A workspace admin adds accounts in Workspace settings → Git.")).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Add GitHub account or organisation" })).toBeNull();
});

it("says so when the person's GitHub reads none of the workspace's accounts", async () => {
  show([]);
  expect(await screen.findByText(/Your GitHub account cannot read any of this workspace's GitHub accounts/)).toBeTruthy();
});

it("asks to continue with GitHub, and says when the link is done", async () => {
  fake.linkGitHub.mockResolvedValue({ authorizeURL: "" });
  const onLinked = vi.fn();
  queries = new QueryClient();
  render(
    <QueryClientProvider client={queries}>
      <GitHubLinkStep onLinked={onLinked} />
    </QueryClientProvider>,
  );
  fireEvent.click(screen.getByRole("button", { name: "Continue with GitHub" }));
  await waitFor(() => expect(onLinked).toHaveBeenCalled());
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

Replace `apps/workspace/src/screens/launcher.test.tsx` with:

```tsx
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { ApiError } from "@shpyrd/shared/api/error";
import type { GitHubRepository } from "@/api/types";
import { NewProject } from "./launcher";

const fake = vi.hoisted(() => ({
  config: vi.fn(),
  me: vi.fn(),
  githubAccounts: vi.fn(),
  githubRepositories: vi.fn(),
  githubBranches: vi.fn(),
  githubMe: vi.fn(),
  linkGitHub: vi.fn(),
  createProject: vi.fn(),
}));
vi.mock("@/api/api", async (actual) => ({ ...(await actual<typeof import("@/api/api")>()), api: fake }));
vi.mock("@/lib/networking", () => ({ useInternalExposure: () => false }));
vi.mock("@/lib/perms", async (actual) => ({ ...(await actual<typeof import("@/lib/perms")>()), usePerms: () => ({ admin: false, create: true }) }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

const repos: GitHubRepository[] = [
  { installation: 42, fullName: "acme/hello-world", private: false, defaultBranch: "main" },
  { installation: 42, fullName: "acme/billing", private: true, defaultBranch: "trunk" },
];
const accounts = { connected: 1, accounts: [{ installation: 42, account: "acme", type: "Organization", suspended: false, manageURL: "https://github.com/organizations/acme/settings/installations/42" }] };

let queries: QueryClient;
async function open() {
  queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={queries}>
      <MemoryRouter>
        <NewProject asButton />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  fireEvent.click(screen.getByRole("button", { name: "New project" }));
  await screen.findByRole("tab", { name: "From GitHub" });
}
const create = () => screen.getByRole("button", { name: "Create" }) as HTMLButtonElement;
// Radix tabs switch on the mouse going down.
const tab = (name: string) => fireEvent.mouseDown(screen.getByRole("tab", { name }), { button: 0 });

beforeEach(() => {
  fake.config.mockResolvedValue({ domain: "example.test" });
  fake.githubAccounts.mockResolvedValue(accounts);
  fake.githubRepositories.mockResolvedValue(repos);
  fake.githubBranches.mockResolvedValue(["main"]);
  fake.createProject.mockImplementation(async (p: { slug: string }) => ({ slug: p.slug }));
});
afterEach(() => {
  cleanup();
  queries?.clear();
  vi.resetAllMocks();
});

it("keeps Create off on From GitHub until a repository is picked", async () => {
  await open();
  fireEvent.change(screen.getByPlaceholderText("hello-world"), { target: { value: "shop" } });
  expect(create().disabled).toBe(true);
  fireEvent.click(await screen.findByRole("button", { name: /acme\/billing/ }));
  expect(create().disabled).toBe(false);
});

it("sends the directory only from the Git URL tab", async () => {
  await open();
  tab("Git URL");
  fireEvent.change(await screen.findByPlaceholderText("https://github.com/acme/hello-world"), { target: { value: "https://github.com/acme/other" } });
  fireEvent.change(await screen.findByPlaceholderText("services/api"), { target: { value: "services/api" } });
  tab("From GitHub");
  fireEvent.click(await screen.findByRole("button", { name: /acme\/billing/ }));
  fireEvent.click(create());
  await waitFor(() => expect(fake.createProject).toHaveBeenCalled());
  const sent = fake.createProject.mock.calls[0][0];
  expect(sent.git).toEqual({ url: "https://github.com/acme/billing", revision: "trunk" });
  expect(sent.subPath).toBeUndefined();
});

it("sends the directory from the Git URL tab", async () => {
  await open();
  tab("Git URL");
  fireEvent.change(screen.getByPlaceholderText("hello-world"), { target: { value: "shop" } });
  fireEvent.change(await screen.findByPlaceholderText("https://github.com/acme/hello-world"), { target: { value: "https://github.com/acme/other" } });
  fireEvent.change(await screen.findByPlaceholderText("services/api"), { target: { value: "services/api" } });
  fireEvent.click(create());
  await waitFor(() => expect(fake.createProject).toHaveBeenCalled());
  expect(fake.createProject.mock.calls[0][0]).toMatchObject({ git: { url: "https://github.com/acme/other", revision: "main" }, subPath: "services/api" });
});

it("forgets a picked repository the list no longer has", async () => {
  await open();
  fireEvent.click(await screen.findByRole("button", { name: /acme\/billing/ }));
  expect(create().disabled).toBe(false);
  queries.setQueryData(["github-repositories", 42], [repos[0]]);
  await waitFor(() => expect(create().disabled).toBe(true));
  // Back in the list, it is not picked again by itself.
  queries.setQueryData(["github-repositories", 42], repos);
  await waitFor(() => expect(screen.getByRole("button", { name: /acme\/billing/ })).toBeTruthy());
  expect(create().disabled).toBe(true);
});

it("asks to continue with GitHub without a link, and keeps what was typed once linked", async () => {
  fake.githubAccounts.mockRejectedValueOnce(new ApiError(409, "Link your GitHub account first", "github_link_required"));
  fake.linkGitHub.mockResolvedValue({ authorizeURL: "" });
  await open();
  fireEvent.change(screen.getByPlaceholderText("hello-world"), { target: { value: "shop" } });
  fireEvent.click(await screen.findByRole("button", { name: "Continue with GitHub" }));
  expect(await screen.findByRole("button", { name: /acme\/billing/ })).toBeTruthy();
  expect((screen.getByPlaceholderText("hello-world") as HTMLInputElement).value).toBe("shop");
});

it("offers no GitHub where the workspace has no GitHub account", async () => {
  fake.githubAccounts.mockResolvedValue({ connected: 0, accounts: [] });
  queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={queries}>
      <MemoryRouter>
        <NewProject asButton />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  fireEvent.click(screen.getByRole("button", { name: "New project" }));
  expect(await screen.findByPlaceholderText("https://github.com/acme/hello-world")).toBeTruthy();
  expect(screen.queryByRole("tab", { name: "From GitHub" })).toBeNull();
});
```

`apps/workspace/src/shell/person.test.tsx`:

```tsx
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { PersonMenu } from "./person";

const fake = vi.hoisted(() => ({ me: vi.fn(), config: vi.fn(), githubMe: vi.fn(), linkGitHub: vi.fn(), unlinkGitHub: vi.fn(), logout: vi.fn() }));
vi.mock("@/api/api", () => ({ api: fake }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

let queries: QueryClient;
function show() {
  queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={queries}>
      <PersonMenu />
    </QueryClientProvider>,
  );
}
async function openMenu() {
  const trigger = await screen.findByRole("button", { name: "Ana Souza" });
  fireEvent.keyDown(trigger, { key: "Enter" });
}
beforeEach(() => {
  fake.me.mockResolvedValue({ subject: "p-1", email: "ana@acme.com", name: "Ana Souza", provider: "github" });
  fake.config.mockResolvedValue({});
});
afterEach(() => {
  cleanup();
  queries?.clear();
  vi.resetAllMocks();
});

it("shows the linked GitHub account and unlinks it", async () => {
  fake.githubMe.mockResolvedValue({ login: "paezao", linkedAt: "2026-10-17T10:00:00Z" });
  fake.unlinkGitHub.mockResolvedValue(undefined);
  show();
  await openMenu();
  expect(await screen.findByText("GitHub: paezao")).toBeTruthy();
  fireEvent.click(screen.getByRole("menuitem", { name: /Unlink GitHub/ }));
  await waitFor(() => expect(fake.unlinkGitHub).toHaveBeenCalled());
});

it("offers to link GitHub when no account is linked", async () => {
  fake.githubMe.mockResolvedValue(null);
  fake.linkGitHub.mockResolvedValue({ authorizeURL: "" });
  show();
  await openMenu();
  fireEvent.click(await screen.findByRole("menuitem", { name: /Link GitHub/ }));
  await waitFor(() => expect(fake.linkGitHub).toHaveBeenCalled());
});

it("offers nothing of GitHub to the admin token", async () => {
  fake.me.mockResolvedValue({ subject: "admin-token", provider: "token", name: "Ana Souza" });
  show();
  await openMenu();
  await screen.findByRole("menuitem", { name: /Sign out/ });
  expect(screen.queryByRole("menuitem", { name: /GitHub/ })).toBeNull();
  expect(fake.githubMe).not.toHaveBeenCalled();
});
```

In `apps/workspace/src/api/api.test.ts`, the test "connects GitHub in the Mock, lists what it reaches and forgets a connection" becomes:

```ts
  it("links the person's GitHub in the Mock, connects an account, lists what it reaches and forgets a connection", async () => {
    await expect(api.githubAccounts()).rejects.toMatchObject({ status: 409, code: "github_link_required" });
    expect(await api.githubMe()).toBeNull();
    expect((await api.linkGitHub("/")).authorizeURL).toBe("");
    expect((await api.githubMe())?.login).toBe("ana-souza");
    const before = await api.github();
    expect(before.configured).toBe(true);
    const { installURL, authorizeURL } = await api.connectGitHub();
    expect(installURL).toBe("/workspace/git");
    expect(authorizeURL).toBe("/workspace/git");
    const accounts = await api.githubAccounts();
    expect(accounts.connected).toBe(before.installations.length + 1);
    expect(accounts.accounts.map((a) => a.account)).toEqual(["acme", "acme-labs"]);
    expect((await api.githubRepositories(42)).map((r) => r.fullName)).toContain("acme/hello-world");
    expect((await api.githubRepositories(77)).map((r) => r.fullName)).toEqual(["acme-labs/ml-pipeline"]);
    expect(await api.githubBranches("acme/hello-world")).toContain("main");
    await expect(api.githubBranches("someone/else")).rejects.toThrow(/No GitHub connection/);
    await api.disconnectGitHub(77);
    expect((await api.github()).installations.length).toBe(before.installations.length);
    expect((await api.projectGitAuth("hello-world")).using).toBe("github-app");
    await api.unlinkGitHub();
    expect(await api.githubMe()).toBeNull();
  }, 15_000);
```

- [ ] **Step 2: Run them to see them fail**

Run: `npm run test --workspace apps/workspace -- http-error github-link github-picker launcher person api.test`
Expected: FAIL: `err.code` undefined, `Failed to resolve import "./github-link"`, `GitHubLinkStep` not exported, `api.githubAccounts is not a function`.

- [ ] **Step 3: The error's code**

`apps/shared/src/api/error.ts`:

```ts
// What a call answers when it fails: the status and the words of the
// server, or of the mock, and the code of a refusal the screens act on
// ("github_link_required").
export class ApiError extends Error {
  status: number;
  code?: string;
  constructor(status: number, message: string, code?: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}
```

In `apps/shared/src/api/http.ts`, `handle` becomes:

```ts
async function handle(res: Response): Promise<Response> {
  if (res.ok) return res;
  let message = `${res.status} ${res.statusText}`;
  let code: string | undefined;
  try {
    const body = await res.json();
    if (body?.error) message = body.error;
    if (typeof body?.code === "string") code = body.code;
  } catch {
    // not json
  }
  throw new ApiError(res.status, message, code);
}
```

- [ ] **Step 4: Types, calls, Backend, Mock**

`types.ts`, after `GitHubRepository`:

```ts
// An account of the workspace's GitHub the person sees on GitHub: the
// picker's account switcher. manageURL is its settings on GitHub, where
// repositories are added.
export type GitHubAccount = {
  installation: number;
  account: string;
  type: "Organization" | "User";
  suspended: boolean;
  manageURL: string;
};

// The person's accounts, and how many the workspace has: none, and New
// project offers no GitHub.
export type GitHubAccounts = { accounts: GitHubAccount[]; connected: number };

// The person's own GitHub account (RFC-0054).
export type GitHubLink = { login: string; linkedAt: string };
```

`api.ts`: import `GitHubAccounts` and `GitHubLink` from `./types`, and replace the `githubRepositories` line of `Api` with:

```ts
  // The person's own GitHub account (RFC-0054): null when none is linked.
  // linkGitHub answers GitHub's authorize page for a popup; an empty one
  // means linked already (the Mock links at once).
  githubMe: () => Promise<GitHubLink | null>;
  linkGitHub: (returnTo: string) => Promise<{ authorizeURL: string }>;
  unlinkGitHub: () => Promise<void>;
  // What the person reads through the workspace's GitHub: a 409 with the
  // code "github_link_required" while they have no link.
  githubAccounts: () => Promise<GitHubAccounts>;
  githubRepositories: (installation?: number) => Promise<GitHubRepository[]>;
```

`backend.ts`: add `import { ApiError } from "@shpyrd/shared/api/error";` and replace `githubRepositories: () => request("/api/workspace/github/repositories"),` with:

```ts
  githubMe: async () => {
    try {
      return await request("/api/me/github");
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) return null;
      throw e;
    }
  },
  linkGitHub: (returnTo) => request("/api/me/github/link", json("POST", { return: returnTo })),
  unlinkGitHub: () => request("/api/me/github", gone),
  githubAccounts: () => request("/api/workspace/github/accounts"),
  githubRepositories: (installation) =>
    request(`/api/workspace/github/repositories${installation ? `?installation=${installation}` : ""}`),
```

`mock/things.json`: add `"githubLink": null,` beside `"github"`, and to `githubRepositories` the entry `{ "installation": 77, "fullName": "acme-labs/ml-pipeline", "private": true, "defaultBranch": "main" }`, and to `githubBranches` `"acme-labs/ml-pipeline": ["main"]`.

`mock.ts`: `Things` gains `githubLink: GitHubLink | null;` (import the types `GitHubAccounts`, `GitHubLink`). Replace the `githubRepositories` handler with:

```ts
  githubMe: async () => (await thingsOf.get()).githubLink,
  // GitHub would ask the person to authorize the App in a popup: the Mock
  // links Ana at once.
  linkGitHub: async () => {
    const all = await thingsOf.get();
    all.githubLink = { login: "ana-souza", linkedAt: now() };
    await thingsOf.set(all);
    return { authorizeURL: "" };
  },
  unlinkGitHub: async () => {
    const all = await thingsOf.get();
    all.githubLink = null;
    await thingsOf.set(all);
  },
  githubAccounts: async () => {
    const all = await thingsOf.get();
    if (!all.github.configured) throw new ApiError(409, all.github.note ?? "GitHub is not set up on this platform.");
    const connected = all.github.installations.length;
    if (connected > 0 && !all.githubLink)
      throw new ApiError(409, "Link your GitHub account first: Continue with GitHub in New project, or run shpyrd github link", "github_link_required");
    const accounts: GitHubAccounts["accounts"] = all.github.installations.map((i) => ({
      installation: i.id,
      account: i.account,
      type: i.type,
      suspended: i.suspended,
      manageURL: i.manageURL,
    }));
    return { accounts, connected };
  },
  githubRepositories: async (installation) => {
    const all = await thingsOf.get();
    if (!all.github.configured) throw new ApiError(409, all.github.note ?? "GitHub is not set up on this platform.");
    if (!all.github.installations.length) return [];
    if (!all.githubLink)
      throw new ApiError(409, "Link your GitHub account first: Continue with GitHub in New project, or run shpyrd github link", "github_link_required");
    const live = new Set(all.github.installations.map((i) => i.id));
    return all.githubRepositories.filter((r) => live.has(r.installation) && (installation === undefined || r.installation === installation));
  },
```

- [ ] **Step 5: The popup link**

`apps/workspace/src/lib/github-link.ts`:

```ts
import { api } from "@/api/api";

// The code of the refusal that asks the person to link GitHub first.
export const LINK_REQUIRED = "github_link_required";

const every = 2_000;
const wait = 10 * 60_000;

// Links the person's GitHub account in a popup, so the page under it keeps
// what was typed (RFC-0054). Done when the popup says so, or when the
// server shows the link (GitHub's pages may cut the popup from its opener).
// The Mock links at once: an empty authorizeURL.
export async function linkGitHub(): Promise<void> {
  const { authorizeURL } = await api.linkGitHub(window.location.pathname + window.location.search);
  if (!authorizeURL) return;
  const popup = window.open(authorizeURL, "shpyrd-github-link", "popup,width=720,height=800");
  if (!popup) {
    // Popups blocked: the page itself goes, and comes back when linked.
    window.location.assign(authorizeURL);
    return new Promise<void>(() => {});
  }
  return new Promise<void>((resolve, reject) => {
    const started = Date.now();
    const finish = (err?: Error) => {
      window.removeEventListener("message", onMessage);
      clearInterval(timer);
      if (err) reject(err);
      else resolve();
    };
    const onMessage = (e: MessageEvent) => {
      if (e.origin === window.location.origin && e.data?.github === "linked") finish();
    };
    const timer = setInterval(async () => {
      if (Date.now() - started > wait) return finish(new Error("GitHub was not linked within 10 minutes. Try again."));
      try {
        if (await api.githubMe()) finish();
      } catch {
        // asked again at the next tick
      }
    }, every);
    window.addEventListener("message", onMessage);
  });
}
```

- [ ] **Step 6: The picker**

Replace `apps/workspace/src/screens/github-picker.tsx` with:

```tsx
"use client";

import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Lock } from "lucide-react";
import { toast } from "sonner";
import { Alert, AlertDescription } from "@shpyrd/ui/components/alert";
import { Button } from "@shpyrd/ui/components/button";
import { Field } from "@shpyrd/ui/components/field";
import { Input } from "@shpyrd/ui/components/input";
import { ScrollArea } from "@shpyrd/ui/components/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@shpyrd/ui/components/select";
import { Skeleton } from "@shpyrd/ui/components/skeleton";
import { api } from "@/api/api";
import type { GitHubAccount } from "@/api/types";
import { linkGitHub } from "@/lib/github-link";

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

// Before the picker: the person links their own GitHub account, in a
// popup, and the dialog under it keeps what was typed (RFC-0054).
export function GitHubLinkStep({ onLinked }: { onLinked: () => void }) {
  const link = useMutation({ mutationFn: linkGitHub, onSuccess: onLinked, onError: (e: Error) => toast.error(e.message) });
  return (
    <div className="grid justify-items-start gap-3 rounded-md border p-4">
      <p className="text-sm text-muted-foreground">
        Link your GitHub account to see the repositories you can read. What you typed stays here.
      </p>
      <Button type="button" onClick={() => link.mutate()}>
        Continue with GitHub
      </Button>
      {link.isPending && <p className="text-xs text-muted-foreground">Waiting for GitHub…</p>}
    </div>
  );
}

// The repositories the person can read in the workspace's GitHub accounts
// (RFC-0054): an account, a filter over a list that scrolls, then the
// branch; below, where to find a missing repository and how to add an
// account.
export function GitHubPicker({
  accounts,
  value,
  onChange,
  canAdd,
}: {
  accounts: GitHubAccount[];
  value?: Picked;
  onChange: (p: Picked | undefined) => void;
  canAdd: boolean;
}) {
  const [installation, setInstallation] = useState<number | undefined>(accounts[0]?.installation);
  const account = accounts.find((a) => a.installation === installation) ?? accounts[0];
  const [filter, setFilter] = useState("");
  const repos = useQuery({
    queryKey: ["github-repositories", account?.installation],
    queryFn: () => api.githubRepositories(account!.installation),
    enabled: !!account,
    retry: false,
    staleTime: 60_000,
  });
  const list = useMemo(() => repos.data ?? [], [repos.data]);
  const shown = useMemo(() => {
    const f = filter.trim().toLowerCase();
    return f ? list.filter((r) => r.fullName.toLowerCase().includes(f)) : list;
  }, [filter, list]);
  const chosen = list.find((r) => r.fullName === value?.fullName);
  // A repository the list no longer has is not picked: GitHub took it
  // away meanwhile.
  useEffect(() => {
    if (value && repos.data && !repos.data.some((r) => r.fullName === value.fullName)) onChange(undefined);
  }, [value, repos.data, onChange]);
  const branches = useQuery({
    queryKey: ["github-branches", value?.fullName],
    queryFn: () => api.githubBranches(value!.fullName),
    enabled: !!chosen,
    staleTime: 60_000,
  });
  const addAccount = async () => {
    try {
      const { installURL } = await api.connectGitHub();
      window.open(installURL, "shpyrd-github-install", "popup,width=1020,height=760");
    } catch (e) {
      toast.error((e as Error).message);
    }
  };
  const below = (
    <div className="flex flex-wrap items-center justify-between gap-2">
      {account && (
        <Button type="button" variant="link" size="xs" onClick={() => window.open(account.manageURL, "_blank", "noopener")}>
          Can't find a repository?
        </Button>
      )}
      {canAdd ? (
        <Button type="button" variant="link" size="xs" onClick={addAccount}>
          Add GitHub account or organisation
        </Button>
      ) : (
        <p className="text-xs text-muted-foreground">A workspace admin adds accounts in Workspace settings → Git.</p>
      )}
    </div>
  );
  if (!account) {
    return (
      <div className="grid gap-3">
        <p className="text-sm text-muted-foreground">
          Your GitHub account cannot read any of this workspace's GitHub accounts. Ask for access on GitHub, or add your own.
        </p>
        {below}
      </div>
    );
  }
  return (
    <div className="grid gap-3">
      <Field label="Account">
        <Select
          value={String(account.installation)}
          onValueChange={(v) => {
            setInstallation(Number(v));
            onChange(undefined);
          }}
        >
          <SelectTrigger aria-label="GitHub account" className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {accounts.map((a) => (
              <SelectItem key={a.installation} value={String(a.installation)}>
                {a.account}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </Field>
      <Field label="Repository">
        <Input value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Filter" aria-label="Filter the repositories" />
      </Field>
      {repos.error ? (
        <Alert variant="destructive">
          <AlertDescription>{(repos.error as Error).message}</AlertDescription>
        </Alert>
      ) : (
        <ScrollArea className="h-48 rounded-md border">
          <div className="grid p-1">
            {repos.isLoading
              ? [0, 1, 2].map((i) => <Skeleton key={i} className="m-1 h-6" />)
              : shown.map((r) => (
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
            {!repos.isLoading && shown.length === 0 && <p className="px-2 py-1.5 text-sm text-muted-foreground">No repository matches.</p>}
          </div>
        </ScrollArea>
      )}
      {below}
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

- [ ] **Step 7: New project uses it**

In `apps/workspace/src/screens/launcher.tsx`: add `import { usePerms, useUserOnly } from "@/lib/perms";` (already there) and `import { LINK_REQUIRED } from "@/lib/github-link";`, import `GitHubLinkStep` beside `GitHubPicker`, and in `NewProject` replace the block from `const [picked, setPicked] = useState<Picked>();` through `if (picked && repos.data && …) setPicked(undefined);` with:

```tsx
  const [picked, setPicked] = useState<Picked>();
  const perms = usePerms();
  const accounts = useQuery({ queryKey: ["github-accounts"], queryFn: api.githubAccounts, enabled: open, retry: false, staleTime: 60_000 });
  const needsLink = accounts.error instanceof ApiError && accounts.error.code === LINK_REQUIRED;
  // Offered when the workspace has a GitHub account, when the person must
  // link first, or when GitHub failed (so the error shows).
  const githubShown = accounts.isSuccess
    ? accounts.data.connected > 0
    : needsLink || (accounts.error instanceof ApiError && accounts.error.status === 502);
  const fromGitHub = githubShown && from === "github";
```

and the `TabsContent value="github"` body becomes:

```tsx
                <TabsContent value="github">
                  {needsLink ? (
                    <GitHubLinkStep
                      onLinked={() => {
                        queries.invalidateQueries({ queryKey: ["github-me"] });
                        queries.invalidateQueries({ queryKey: ["github-accounts"] });
                      }}
                    />
                  ) : accounts.error ? (
                    <Alert variant="destructive">
                      <AlertDescription>{accounts.error.message}</AlertDescription>
                    </Alert>
                  ) : accounts.data ? (
                    <GitHubPicker
                      accounts={accounts.data.accounts}
                      value={picked}
                      canAdd={perms.admin}
                      onChange={(p) => {
                        setPicked(p);
                        if (p && !slug.trim()) setSlug(slugFromRepo(p.fullName));
                      }}
                    />
                  ) : (
                    <Skeleton className="h-48 w-full" />
                  )}
                </TabsContent>
```

(`queries` is `NewProject`'s `useQueryClient()`; `perms` shadows nothing in `NewProject`.)

- [ ] **Step 8: The person menu**

In `apps/workspace/src/shell/person.tsx`: imports gain `Link2, Unlink` from `lucide-react`, `toast` from `sonner`, and `linkGitHub` from `@/lib/github-link`. In `PersonMenu`, after `const config = …`:

```tsx
  // The person's own GitHub account (RFC-0054); the admin token has none.
  const person = me.data !== undefined && me.data.provider !== "token";
  const github = useQuery({ queryKey: ["github-me"], queryFn: api.githubMe, staleTime: 60_000, enabled: person });
  const githubChanged = () => {
    queries.invalidateQueries({ queryKey: ["github-me"] });
    queries.invalidateQueries({ queryKey: ["github-accounts"] });
    queries.invalidateQueries({ queryKey: ["github-repositories"] });
  };
  const link = async () => {
    try {
      await linkGitHub();
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      githubChanged();
    }
  };
  const unlink = async () => {
    try {
      await api.unlinkGitHub();
      toast.success("GitHub unlinked", { description: "Your projects keep deploying." });
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      githubChanged();
    }
  };
```

and between the `consoleUrl` item and the Sign out item:

```tsx
        {person &&
          (github.data ? (
            <>
              <DropdownMenuLabel className="text-xs font-normal text-muted-foreground">GitHub: {github.data.login}</DropdownMenuLabel>
              <DropdownMenuItem onClick={unlink}>
                <Unlink /> Unlink GitHub
              </DropdownMenuItem>
            </>
          ) : (
            github.isSuccess && (
              <DropdownMenuItem onClick={link}>
                <Link2 /> Link GitHub
              </DropdownMenuItem>
            )
          ))}
        {person && <DropdownMenuSeparator />}
```

- [ ] **Step 9: Run the gates**

Run: `npm run test --workspace apps/workspace && npm run lint --workspace apps/workspace && npm run typecheck --workspace apps/workspace`
Expected: PASS. Then `npm run design --workspace apps/workspace`, open New project in the browser: **Continue with GitHub**, then the picker with the account switcher, **Can't find a repository?** and **Add GitHub account or organisation**; the person menu shows **GitHub: ana-souza** with **Unlink GitHub**. Light and dark.

- [ ] **Step 10: Commit**

```bash
git add apps/shared/src/api apps/workspace/src apps/workspace/mock
git commit -m "feat(workspace): Continue with GitHub, the account switcher, and GitHub in the person menu

Co-Authored-By: <your model name> <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 9: The catch-up sweep: deliveries GitHub could not make, asked for again

**Files:**
- Create: `pkg/github/hooks.go`, `pkg/github/hooks_test.go`, `pkg/github/githubtest/hooks.go`, `internal/controller/github_sweep.go`, `internal/controller/github_sweep_test.go`
- Modify: `pkg/github/githubtest/fake.go` (`Server` fields, `serve`, `accessToken` uses `appJWT`), `pkg/server/server.go` (after the reporter ~592)
- Test: `pkg/api/github_webhook_test.go` (redelivery), `internal/controller/source_controller_test.go` (an older push again)

**Interfaces:**
- Consumes: `Client.callHeaders` (Task 3); `GitHubApp.Source` (exists).
- Produces:
  - `type github.HookDelivery struct { ID int64; GUID string; DeliveredAt time.Time; Redelivery bool; StatusCode int; Event, Action string; Installation int64 }`, `func (HookDelivery) Delivered() bool`
  - `func (*Client) HookDeliveries(ctx, since time.Time) ([]HookDelivery, error)` (newest first), `Redeliver(ctx, id int64) error`
  - fake: `type githubtest.Delivery struct { ID int64; GUID string; DeliveredAt time.Time; Redelivery bool; StatusCode int; Event, Action string; InstallationID int64 }`; `(*Server).AddDelivery(d Delivery) int64`, `Redelivered() []int64`, `SetRedeliveryStatus(code int)`
  - `type controller.GitHubSweep struct { GitHub *GitHubApp; Every time.Duration; Now func() time.Time }`; `Start(ctx) error`, `NeedLeaderElection() bool`, `Sweep(ctx) (int, error)`
  - `func redeliveries(list []github.HookDelivery, resent map[string]time.Time, now time.Time) []github.HookDelivery`

- [ ] **Step 1: Write the failing tests**

`pkg/github/hooks_test.go`:

```go
package github_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/shpyrd-io/shpyrd/pkg/github/githubtest"
)

func TestTheAppsDeliveriesArePagedBackToATime(t *testing.T) {
	c, fake := newClient(t)
	ctx := context.Background()
	for i := 0; i < 150; i++ {
		fake.AddDelivery(githubtest.Delivery{GUID: fmt.Sprintf("g-%d", i), Event: "push", StatusCode: 202, DeliveredAt: t0.Add(-time.Duration(i) * time.Minute)})
	}
	list, err := c.HookDeliveries(ctx, t0.Add(-120*time.Minute))
	if err != nil || len(list) != 121 || list[0].GUID != "g-0" || list[120].GUID != "g-120" || !list[0].Delivered() {
		t.Fatalf("%d deliveries, %v", len(list), err)
	}
	pages := 0
	for _, call := range fake.Calls() {
		if call == "GET /app/hook/deliveries" {
			pages++
		}
	}
	if pages != 2 {
		t.Errorf("%d pages read: the second page reaches the time, no third is read", pages)
	}
}

func TestADeliveryIsAskedForAgain(t *testing.T) {
	c, fake := newClient(t)
	id := fake.AddDelivery(githubtest.Delivery{GUID: "g-1", Event: "push", StatusCode: 502, DeliveredAt: t0.Add(-time.Minute)})
	if err := c.Redeliver(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if got := fake.Redelivered(); len(got) != 1 || got[0] != id {
		t.Errorf("redelivered %v", got)
	}
	list, _ := c.HookDeliveries(context.Background(), t0.Add(-time.Hour))
	if len(list) != 2 || list[0].GUID != "g-1" || !list[0].Redelivery || !list[0].Delivered() || list[1].Delivered() {
		t.Errorf("attempts %+v", list)
	}
	if err := c.Redeliver(context.Background(), 999999); err == nil {
		t.Error("an unknown delivery")
	}
}
```

`internal/controller/github_sweep_test.go`:

```go
package controller

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/shpyrd-io/shpyrd/pkg/github"
	"github.com/shpyrd-io/shpyrd/pkg/github/githubtest"
)

func ids(list []github.HookDelivery) []int64 {
	var out []int64
	for _, d := range list {
		out = append(out, d.ID)
	}
	return out
}

func TestTheSweepPicksFailedDeliveriesOnce(t *testing.T) {
	now := sourceNow
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	list := []github.HookDelivery{
		{ID: 1, GUID: "push-failed", Event: "push", StatusCode: 502, DeliveredAt: ago(4 * time.Minute)},
		{ID: 2, GUID: "push-ok", Event: "push", StatusCode: 202, DeliveredAt: ago(3 * time.Minute)},
		{ID: 3, GUID: "push-later-ok", Event: "push", StatusCode: 0, DeliveredAt: ago(10 * time.Minute)},
		{ID: 4, GUID: "push-later-ok", Event: "push", StatusCode: 202, Redelivery: true, DeliveredAt: ago(2 * time.Minute)},
		{ID: 5, GUID: "ci-failed", Event: "check_suite", StatusCode: 404, DeliveredAt: ago(6 * time.Minute)},
		{ID: 6, GUID: "status-failed", Event: "status", StatusCode: 500, DeliveredAt: ago(time.Minute)},
		{ID: 7, GUID: "install-failed", Event: "installation_repositories", StatusCode: 503, DeliveredAt: ago(30 * time.Second)},
		{ID: 8, GUID: "ping-failed", Event: "ping", StatusCode: 500, DeliveredAt: ago(time.Minute)},
		{ID: 9, GUID: "pr-failed", Event: "pull_request", StatusCode: 500, DeliveredAt: ago(time.Minute)},
		{ID: 10, GUID: "resent", Event: "push", StatusCode: 500, DeliveredAt: ago(5 * time.Minute)},
		{ID: 11, GUID: "redirected", Event: "push", StatusCode: 302, DeliveredAt: ago(time.Minute)},
		{ID: 12, GUID: "failed-twice", Event: "push", StatusCode: 502, DeliveredAt: ago(8 * time.Minute)},
		{ID: 13, GUID: "failed-twice", Event: "push", StatusCode: 502, Redelivery: true, DeliveredAt: ago(7 * time.Minute)},
	}
	resent := map[string]time.Time{"resent": now.Add(-time.Hour)}
	if got := fmt.Sprint(ids(redeliveries(list, resent, now))); got != "[12 5 1 6 7]" {
		t.Errorf("picked %s: the oldest first, the first attempt of each, none delivered, none resent today", got)
	}
	resent["resent"] = now.Add(-25 * time.Hour)
	if got := fmt.Sprint(ids(redeliveries(list, resent, now))); got != "[12 5 10 1 6 7]" {
		t.Errorf("a resend over a day ago no longer counts: %s", got)
	}
	var many []github.HookDelivery
	for i := 0; i < 60; i++ {
		many = append(many, github.HookDelivery{ID: int64(100 + i), GUID: fmt.Sprintf("g-%d", i), Event: "push", StatusCode: 0, DeliveredAt: ago(time.Duration(60-i) * time.Minute)})
	}
	got := redeliveries(many, map[string]time.Time{}, now)
	if len(got) != maxRedeliveries || got[0].ID != 100 || got[len(got)-1].ID != 149 {
		t.Errorf("%d picked, from %d to %d", len(got), got[0].ID, got[len(got)-1].ID)
	}
}

func TestTheSweepAsksGitHubToDeliverAgainWhatFailed(t *testing.T) {
	fake, st := connectedGitHub(t)
	_, c := newTestReconciler(t, fake.Secret("shpyrd-system"))
	sweep := &GitHubSweep{GitHub: &GitHubApp{Source: fake.Source(c, "shpyrd-system"), Installations: st}, Now: fake.Time}
	ctx := context.Background()
	failed := fake.AddDelivery(githubtest.Delivery{GUID: "g-push", Event: "push", StatusCode: 502, DeliveredAt: sourceNow.Add(-2 * time.Minute), InstallationID: 42})
	fake.AddDelivery(githubtest.Delivery{GUID: "g-ok", Event: "push", StatusCode: 202, DeliveredAt: sourceNow.Add(-time.Minute), InstallationID: 42})
	old := fake.AddDelivery(githubtest.Delivery{GUID: "g-day-old", Event: "push", StatusCode: 0, DeliveredAt: sourceNow.Add(-23 * time.Hour), InstallationID: 42})
	fake.AddDelivery(githubtest.Delivery{GUID: "g-too-old", Event: "push", StatusCode: 0, DeliveredAt: sourceNow.Add(-25 * time.Hour), InstallationID: 42})
	if n, err := sweep.Sweep(ctx); err != nil || n != 2 || fmt.Sprint(fake.Redelivered()) != fmt.Sprint([]int64{old, failed}) {
		t.Fatalf("first sweep: %d %v, redelivered %v", n, err, fake.Redelivered())
	}
	// The next sweep reads back to the last one: nothing failed since, and
	// what was resent is not sent again, even when its resend failed too.
	fake.SetRedeliveryStatus(502)
	advance(fake, 5*time.Minute)
	if n, err := sweep.Sweep(ctx); err != nil || n != 0 || len(fake.Redelivered()) != 2 {
		t.Errorf("second sweep: %d %v, redelivered %v", n, err, fake.Redelivered())
	}
	late := fake.AddDelivery(githubtest.Delivery{GUID: "g-late", Event: "status", StatusCode: 404, DeliveredAt: fake.Time().Add(-time.Minute), InstallationID: 42})
	advance(fake, 5*time.Minute)
	if n, _ := sweep.Sweep(ctx); n != 1 || fake.Redelivered()[2] != late {
		t.Errorf("third sweep: %d, redelivered %v", n, fake.Redelivered())
	}
	// GitHub down: an error, and the window is kept for the next sweep.
	fake.SetDown(true)
	if _, err := sweep.Sweep(ctx); err == nil {
		t.Error("GitHub down is no error")
	}
}

func TestTheSweepDoesNothingWithoutTheApp(t *testing.T) {
	_, c := newTestReconciler(t)
	sweep := &GitHubSweep{GitHub: &GitHubApp{Source: &github.Source{Reader: c, Namespace: "shpyrd-system"}}}
	if n, err := sweep.Sweep(context.Background()); n != 0 || err != nil {
		t.Errorf("%d %v", n, err)
	}
	if !sweep.NeedLeaderElection() {
		t.Error("the sweep runs on every replica")
	}
}
```

Append to `pkg/api/github_webhook_test.go`:

```go
// The sweep redelivers what GitHub could not deliver; a delivery shpyrd
// took already is not left on the App again.
func TestARedeliveredPushIsNotAppliedTwice(t *testing.T) {
	s, cr, _ := githubServer(t, onGitHub("shop", "https://github.com/acme/shop", "main", ""))
	connectAcme(t, s, store.DefaultWorkspace)
	if rec := deliverApp(t, s, "push", "d-1", appPush(42, "Acme/shop", "refs/heads/main"), githubtest.WebhookSecret); rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"shop"`) {
		t.Fatalf("first: %d %s", rec.Code, rec.Body.String())
	}
	before := appNamed(t, cr, "shop")
	rec := deliverApp(t, s, "push", "d-1", appPush(42, "Acme/shop", "refs/heads/main"), githubtest.WebhookSecret)
	if rec.Code != http.StatusAccepted || strings.Contains(rec.Body.String(), `"shop"`) {
		t.Errorf("redelivered while waiting: %d %s", rec.Code, rec.Body.String())
	}
	if after := appNamed(t, cr, "shop"); after.ResourceVersion != before.ResourceVersion {
		t.Error("the redelivery wrote the App again")
	}
	// Once the watcher took it, its record remembers the delivery.
	app := appNamed(t, cr, "shop")
	delete(app.Annotations, shpyrdv1.AnnotationGitPush)
	recordOn(app, gitauth.SourceRecord{Ref: "main", Latest: hookCommit, Delivery: "d-1"})
	if err := cr.Update(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	if rec := deliverApp(t, s, "push", "d-1", appPush(42, "Acme/shop", "refs/heads/main"), githubtest.WebhookSecret); strings.Contains(rec.Body.String(), `"shop"`) {
		t.Errorf("redelivered after the watcher took it: %s", rec.Body.String())
	}
	if _, ok := appNamed(t, cr, "shop").Annotations[shpyrdv1.AnnotationGitPush]; ok {
		t.Error("left on the App again")
	}
}
```

Append to `internal/controller/source_controller_test.go`:

```go
// An older push delivered again (the sweep resends what failed, in any
// order) pins nothing new: the branch says its head is the newer commit.
func TestAnOlderPushDeliveredAgainPinsNothing(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": otherCommit}}
	app := withPush(withRecord(pinGit(gitApp("shop", "https://gitlab.com/acme/shop"), otherCommit),
		gitauth.SourceRecord{Ref: "main", Latest: otherCommit, Delivery: "d-2", CheckedAt: at(sourceNow.Add(-time.Minute))}),
		gitauth.Push{Ref: "refs/heads/main", SHA: testCommit, Before: "fedcba9876543210fedcba9876543210fedcba98", Delivery: "d-1", Author: "alice", At: sourceNow.Add(-10 * time.Minute)})
	r, _ := newSourceReconciler(t, ls, app)
	heard := heardAudits(t)
	_, got := reconcileSource(t, r, app)
	if got.PinnedCommit() != otherCommit || ls.calls != 1 || len(*heard) != 0 {
		t.Errorf("pin %s after %d listings, audits %+v", got.PinnedCommit(), ls.calls, *heard)
	}
	if _, ok := got.Annotations[shpyrdv1.AnnotationGitPush]; ok {
		t.Error("the push stays on the App")
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp TMPDIR=/home/nkr/.cache/gotmp go test ./pkg/github/ ./internal/controller/ ./pkg/api/ -run 'Deliveries|AskedForAgain|TheSweep|RedeliveredPush|OlderPushDeliveredAgain'`
Expected: FAIL to compile: `c.HookDeliveries undefined`, `undefined: githubtest.Delivery`, `undefined: redeliveries`. (`TestARedeliveredPushIsNotAppliedTwice` and `TestAnOlderPushDeliveredAgainPinsNothing` pin what already holds; they pass once the package compiles.)

- [ ] **Step 3: The client**

`pkg/github/hooks.go`:

```go
package github

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// The App's webhook deliveries (RFC-0054, webhooks before polling): what
// GitHub sent and how shpyrd answered, kept three days, and a redelivery
// of one that failed. Read with the App's JWT.

// HookDelivery is one attempt of a delivery: its GUID is the delivery's
// (the X-GitHub-Delivery header), the same on every attempt.
type HookDelivery struct {
	ID           int64
	GUID         string
	DeliveredAt  time.Time
	Redelivery   bool
	StatusCode   int
	Event        string
	Action       string
	Installation int64
}

// Delivered says the attempt reached shpyrd and was taken: a 2xx or 3xx
// answer. 0 is no answer at all.
func (d HookDelivery) Delivered() bool { return d.StatusCode >= 200 && d.StatusCode < 400 }

// HookDeliveries are the App's delivery attempts since a time, newest
// first.
func (c *Client) HookDeliveries(ctx context.Context, since time.Time) ([]HookDelivery, error) {
	bearer, err := c.AppJWT()
	if err != nil {
		return nil, err
	}
	out := []HookDelivery{}
	next := fmt.Sprintf("%s/app/hook/deliveries?per_page=%d", c.api, perPage)
	for page := 0; page < maxPages && next != ""; page++ {
		var res []struct {
			ID             int64     `json:"id"`
			GUID           string    `json:"guid"`
			DeliveredAt    time.Time `json:"delivered_at"`
			Redelivery     bool      `json:"redelivery"`
			StatusCode     int       `json:"status_code"`
			Event          string    `json:"event"`
			Action         string    `json:"action"`
			InstallationID int64     `json:"installation_id"`
		}
		h, err := c.callHeaders(ctx, "GET", next, bearer, nil, &res)
		if err != nil {
			return nil, err
		}
		older := false
		for _, d := range res {
			if d.DeliveredAt.Before(since) {
				older = true
				continue
			}
			out = append(out, HookDelivery{ID: d.ID, GUID: d.GUID, DeliveredAt: d.DeliveredAt, Redelivery: d.Redelivery,
				StatusCode: d.StatusCode, Event: d.Event, Action: d.Action, Installation: d.InstallationID})
		}
		if older || len(res) == 0 {
			break
		}
		next = c.nextPage(h)
	}
	return out, nil
}

// Redeliver asks GitHub to deliver a delivery again.
func (c *Client) Redeliver(ctx context.Context, id int64) error {
	bearer, err := c.AppJWT()
	if err != nil {
		return err
	}
	return c.call(ctx, "POST", fmt.Sprintf("%s/app/hook/deliveries/%d/attempts", c.api, id), bearer, nil, nil)
}

var linkNext = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

// nextPage is the next page GitHub's Link header names, when it is on
// GitHub's API: a link elsewhere is not followed.
func (c *Client) nextPage(h http.Header) string {
	m := linkNext.FindStringSubmatch(h.Get("Link"))
	if m == nil || !strings.HasPrefix(m[1], c.api+"/") {
		return ""
	}
	return m[1]
}
```

- [ ] **Step 4: The fake's deliveries**

`pkg/github/githubtest/hooks.go`:

```go
package githubtest

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// The App's delivery log (RFC-0054, the sweep): attempts added with
// AddDelivery, listed newest first with a Link to the next page, and
// redelivered, which adds an attempt that answers SetRedeliveryStatus's
// code (202 unless set).

// Delivery is one attempt of a webhook delivery.
type Delivery struct {
	ID             int64
	GUID           string
	DeliveredAt    time.Time
	Redelivery     bool
	StatusCode     int
	Event, Action  string
	InstallationID int64
}

// AddDelivery records an attempt; its ID is given when it has none.
func (f *Server) AddDelivery(d Delivery) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	if d.ID == 0 {
		f.nextID++
		d.ID = f.nextID
	}
	f.deliveries = append(f.deliveries, d)
	return d.ID
}

// Redelivered are the ids asked to be delivered again, in order.
func (f *Server) Redelivered() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int64(nil), f.redelivered...)
}

// SetRedeliveryStatus is how shpyrd answers a redelivery: its status code.
func (f *Server) SetRedeliveryStatus(code int) { f.mu.Lock(); f.redeliveryStatus = code; f.mu.Unlock() }

// appJWT says the request carries the App's JWT.
func (f *Server) appJWT(r *http.Request) bool {
	_, err := jwt.ParseWithClaims(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), &jwt.RegisteredClaims{},
		func(*jwt.Token) (any, error) { return &Key().PublicKey, nil },
		jwt.WithValidMethods([]string{"RS256"}), jwt.WithTimeFunc(f.clock), jwt.WithIssuer(strconv.Itoa(AppID)))
	return err == nil
}

// hookDeliveries is GET /app/hook/deliveries: newest first, cursor pages.
func (f *Server) hookDeliveries(w http.ResponseWriter, r *http.Request) {
	if !f.appJWT(r) {
		reply(w, http.StatusUnauthorized, map[string]string{"message": "A JSON web token could not be decoded"})
		return
	}
	list := append([]Delivery(nil), f.deliveries...)
	sort.SliceStable(list, func(i, j int) bool {
		if !list[i].DeliveredAt.Equal(list[j].DeliveredAt) {
			return list[i].DeliveredAt.After(list[j].DeliveredAt)
		}
		return list[i].ID > list[j].ID
	})
	per, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if per <= 0 {
		per = 30
	}
	from, _ := strconv.Atoi(r.URL.Query().Get("cursor"))
	from = min(max(from, 0), len(list))
	to := min(from+per, len(list))
	if to < len(list) {
		w.Header().Set("Link", fmt.Sprintf(`<%s/app/hook/deliveries?per_page=%d&cursor=%d>; rel="next"`, f.URL, per, to))
	}
	out := []map[string]any{}
	for _, d := range list[from:to] {
		out = append(out, map[string]any{"id": d.ID, "guid": d.GUID, "delivered_at": d.DeliveredAt.UTC().Format(time.RFC3339),
			"redelivery": d.Redelivery, "status_code": d.StatusCode, "status": http.StatusText(d.StatusCode), "event": d.Event,
			"action": d.Action, "installation_id": d.InstallationID})
	}
	reply(w, http.StatusOK, out)
}

// redeliver is POST /app/hook/deliveries/{id}/attempts.
func (f *Server) redeliver(w http.ResponseWriter, r *http.Request, idText string) {
	if !f.appJWT(r) {
		reply(w, http.StatusUnauthorized, map[string]string{"message": "A JSON web token could not be decoded"})
		return
	}
	id, _ := strconv.ParseInt(idText, 10, 64)
	for _, d := range f.deliveries {
		if d.ID != id {
			continue
		}
		f.redelivered = append(f.redelivered, id)
		f.nextID++
		again := d
		again.ID, again.Redelivery, again.DeliveredAt = f.nextID, true, f.clock()
		again.StatusCode = f.redeliveryStatus
		if again.StatusCode == 0 {
			again.StatusCode = http.StatusAccepted
		}
		f.deliveries = append(f.deliveries, again)
		reply(w, http.StatusAccepted, map[string]any{})
		return
	}
	reply(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
}
```

In `pkg/github/githubtest/fake.go`, `Server` gains:

```go
	deliveries       []Delivery
	redelivered      []int64
	redeliveryStatus int
```

`accessToken`'s JWT check becomes `if !f.appJWT(r) { reply(w, http.StatusUnauthorized, map[string]string{"message": "A JSON web token could not be decoded"}); return }` (the claims are not used further; drop the `claims` variable, and the `jwt` import of `fake.go`, now unused there), and `serve` gains, before `default:`:

```go
	case r.Method == "GET" && p == "/app/hook/deliveries":
		f.hookDeliveries(w, r)
	case r.Method == "POST" && strings.HasPrefix(p, "/app/hook/deliveries/") && strings.HasSuffix(p, "/attempts"):
		f.redeliver(w, r, strings.TrimSuffix(strings.TrimPrefix(p, "/app/hook/deliveries/"), "/attempts"))
```

- [ ] **Step 5: The sweep**

`internal/controller/github_sweep.go`:

```go
package controller

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/shpyrd-io/shpyrd/pkg/github"
)

// The catch-up sweep (RFC-0054, webhooks before polling). A repository the
// GitHub App covers is never listed on a timer, so a webhook GitHub failed
// to deliver (shpyrd down, a network fault) would be lost. Every 5 minutes,
// and at start, on the leader, the sweep reads the App's delivery attempts
// since the last sweep (a day back at start; GitHub keeps three) and asks
// GitHub to deliver again each push, check_suite, status and installation
// event that never reached shpyrd. One or two calls a sweep for the whole
// cluster, however many projects. The webhook ignores a delivery it took
// already, and the watcher one that does not follow. A delivery GitHub
// never made is not caught: a push or a Redeploy fixes it.

const (
	githubSweepEvery   = 5 * time.Minute
	githubSweepStart   = 24 * time.Hour
	githubSweepOverlap = time.Minute
	// maxRedeliveries a sweep, the oldest first.
	maxRedeliveries = 50
	// redeliveredFor: a delivery resent is not resent again for a day.
	redeliveredFor = 24 * time.Hour
)

// GitHubSweep asks GitHub to deliver again what did not reach shpyrd.
type GitHubSweep struct {
	GitHub *GitHubApp
	// Every is the wait between sweeps; 0 is 5 minutes.
	Every time.Duration
	// Now is the clock; nil is time.Now.
	Now func() time.Time

	mu sync.Mutex
	// last is when the last sweep read the deliveries completely.
	last time.Time
	// resent is when each delivery (its GUID) was resent.
	resent map[string]time.Time
}

// NeedLeaderElection makes the manager run the sweep on the leader only.
func (s *GitHubSweep) NeedLeaderElection() bool { return true }

// Start sweeps at once, then every Every, until ctx ends.
func (s *GitHubSweep) Start(ctx context.Context) error {
	every := s.Every
	if every <= 0 {
		every = githubSweepEvery
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if _, err := s.Sweep(ctx); err != nil {
			s.logger().Info("could not read the GitHub App's deliveries; the next sweep tries again", "err", err.Error())
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

// Sweep is one pass: how many deliveries it asked for again.
func (s *GitHubSweep) Sweep(ctx context.Context) (int, error) {
	if s == nil || s.GitHub == nil || s.GitHub.Source == nil {
		return 0, nil
	}
	cl, err := s.GitHub.Source.Client(ctx)
	if err == github.ErrNotConfigured { //nolint:errorlint // a bare ErrNotConfigured is no App at all
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	now := s.now()
	s.mu.Lock()
	since := now.Add(-githubSweepStart)
	if !s.last.IsZero() {
		since = s.last.Add(-githubSweepOverlap)
	}
	s.mu.Unlock()
	list, err := cl.HookDeliveries(ctx, since)
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	if s.resent == nil {
		s.resent = map[string]time.Time{}
	}
	picks := redeliveries(list, s.resent, now)
	s.mu.Unlock()
	sent, failed := 0, false
	for _, d := range picks {
		if err := cl.Redeliver(ctx, d.ID); err != nil {
			failed = true
			s.logger().Info("GitHub did not deliver again; the next sweep tries", "delivery", d.ID, "event", d.Event, "err", err.Error())
			var apiErr *github.APIError
			if errors.As(err, &apiErr) && apiErr.RateLimited {
				break
			}
			continue
		}
		s.mu.Lock()
		s.resent[d.GUID] = now
		s.mu.Unlock()
		sent++
	}
	s.mu.Lock()
	// A failed resend keeps the window: the next sweep reads it again.
	if !failed {
		s.last = now
	}
	for guid, at := range s.resent {
		if now.Sub(at) >= redeliveredFor {
			delete(s.resent, guid)
		}
	}
	s.mu.Unlock()
	if sent > 0 {
		s.logger().Info("asked GitHub to deliver again what did not reach shpyrd", "deliveries", sent)
	}
	return sent, nil
}

// redeliveries picks what a sweep asks for again: by delivery (its GUID),
// one shpyrd needs (push, check_suite, status, installation*) whose every
// attempt failed (no answer, or 4xx and 5xx), not resent in the last day;
// its first attempt, the oldest first, at most maxRedeliveries.
func redeliveries(list []github.HookDelivery, resent map[string]time.Time, now time.Time) []github.HookDelivery {
	type attempts struct {
		first     github.HookDelivery
		delivered bool
	}
	by := map[string]*attempts{}
	for _, d := range list {
		if !sweptEvent(d.Event) {
			continue
		}
		a := by[d.GUID]
		if a == nil {
			a = &attempts{first: d}
			by[d.GUID] = a
		}
		if d.DeliveredAt.Before(a.first.DeliveredAt) {
			a.first = d
		}
		a.delivered = a.delivered || d.Delivered()
	}
	var out []github.HookDelivery
	for guid, a := range by {
		if at, ok := resent[guid]; a.delivered || (ok && now.Sub(at) < redeliveredFor) {
			continue
		}
		out = append(out, a.first)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].DeliveredAt.Equal(out[j].DeliveredAt) {
			return out[i].DeliveredAt.Before(out[j].DeliveredAt)
		}
		return out[i].ID < out[j].ID
	})
	if len(out) > maxRedeliveries {
		out = out[:maxRedeliveries]
	}
	return out
}

func sweptEvent(event string) bool {
	return event == "push" || event == "check_suite" || event == "status" || strings.HasPrefix(event, "installation")
}

func (s *GitHubSweep) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *GitHubSweep) logger() logr.Logger { return log.Log.WithName("github-sweep") }
```

In `pkg/server/server.go`, after the reporter's `SetupWithManager`:

```go
	// The catch-up sweep (RFC-0054): deliveries GitHub could not make,
	// asked for again every 5 minutes, on the leader.
	if err := mgr.Add(&controller.GitHubSweep{GitHub: githubApp}); err != nil {
		return nil, fmt.Errorf("github sweep: %w", err)
	}
```

- [ ] **Step 6: Run the tests**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp TMPDIR=/home/nkr/.cache/gotmp go build ./... && GOTMPDIR=/home/nkr/.cache/gotmp TMPDIR=/home/nkr/.cache/gotmp go test ./pkg/github/... ./internal/controller/ ./pkg/api/ ./pkg/server/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add pkg/github internal/controller/github_sweep.go internal/controller/github_sweep_test.go internal/controller/source_controller_test.go pkg/api/github_webhook_test.go pkg/server/server.go
git commit -m "feat(controller): the catch-up sweep asks GitHub to redeliver what failed, every 5 minutes on the leader

Co-Authored-By: <your model name> <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 10: No timer for a repository the App covers; 2 minutes and 90 seconds for CI

**Files:**
- Modify: `pkg/gitauth/webhook.go` (`Push.App`, `SourceRecord.ProjectHook`), `pkg/api/github_webhook.go` (`githubPush`), `internal/controller/source_controller.go` (constants 37-61; `Reconcile` 190-306; `decideSource` 457-464; `requeue`, `due`, `wait`, `untilDue` 683-758; new `WatchMode`, `covered`, `deployAsked`), `internal/controller/source_gates.go` (`heldRecheck`, `ciShareFor`)
- Test: `internal/controller/source_controller_test.go`, `internal/controller/source_gates_test.go`, `pkg/api/github_webhook_test.go`, `pkg/gitauth/webhook_test.go`

**Interfaces:**
- Consumes: `GitHubApp.coverQuietly` (exists).
- Produces:
  - `gitauth.Push.App bool` (json `app,omitempty`): the push came through the GitHub App's webhook
  - `gitauth.SourceRecord.ProjectHook bool` (json `projectHook,omitempty`): the project's own webhook has delivered
  - `const controller.WatchGitHubApp = "github-app"`, `WatchWebhook = "webhook"`, `WatchPoll = "poll"`; `func controller.WatchMode(covered bool, rec gitauth.SourceRecord) string`
  - `const pushDrivenRecheck = time.Hour`
  - `func (r *SourceReconciler) covered(ctx, app) bool`; `due(app, rec, covered bool, now) bool`; `wait(rec) time.Duration`; `untilDue(rec, covered bool, now) time.Duration`; `requeue(uid, rec, covered, nudged bool, now) time.Duration`; `interval() time.Duration`; `func deployAsked(app, rec) bool`
  - `heldRecheck = 2 * time.Minute`, `ciShareFor = 90 * time.Second`

- [ ] **Step 1: Write the failing tests**

Append to `pkg/gitauth/webhook_test.go`:

```go
func TestARecordKeepsThatTheProjectsWebhookDelivered(t *testing.T) {
	raw, _ := json.Marshal(SourceRecord{Ref: "main", ProjectHook: true})
	if !strings.Contains(string(raw), `"projectHook":true`) || !ReadSourceRecord(string(raw)).ProjectHook {
		t.Errorf("%s", raw)
	}
	pushRaw, _ := json.Marshal(Push{Ref: "refs/heads/main", SHA: "a", App: true})
	if p, err := ReadPush(string(pushRaw)); err != nil || !p.App {
		t.Errorf("%s: %+v %v", pushRaw, p, err)
	}
}
```

Append to `pkg/api/github_webhook_test.go`:

```go
func TestAPushThroughTheAppSaysSo(t *testing.T) {
	s, cr, _ := githubServer(t, onGitHub("shop", "https://github.com/acme/shop", "main", ""))
	connectAcme(t, s, store.DefaultWorkspace)
	deliverApp(t, s, "push", "d-1", appPush(42, "Acme/shop", "refs/heads/main"), githubtest.WebhookSecret)
	if p := pushOn(t, cr, "shop"); p == nil || !p.App {
		t.Errorf("push %+v", p)
	}
}
```

Append to `internal/controller/source_controller_test.go` (its imports gain `ctrl` already there, `"github.com/shpyrd-io/shpyrd/pkg/github/githubtest"` and `"github.com/shpyrd-io/shpyrd/pkg/store"`):

```go
func TestWatchModeSaysHowABranchsPushesArrive(t *testing.T) {
	for _, c := range []struct {
		covered bool
		rec     gitauth.SourceRecord
		want    string
	}{
		{true, gitauth.SourceRecord{}, WatchGitHubApp},
		{true, gitauth.SourceRecord{ProjectHook: true}, WatchGitHubApp},
		{false, gitauth.SourceRecord{ProjectHook: true}, WatchWebhook},
		{false, gitauth.SourceRecord{LastWebhookAt: at(sourceNow)}, WatchPoll},
		{false, gitauth.SourceRecord{}, WatchPoll},
	} {
		if got := WatchMode(c.covered, c.rec); got != c.want {
			t.Errorf("%v %+v: %s", c.covered, c.rec, got)
		}
	}
}

// Review Focus 7.
func TestACoveredRepositoryIsNeverListedOnATimer(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": testCommit}}
	app := shopWith(shpyrdv1.GitSource{Revision: "main"})
	r, fake := gatedWatcher(t, ls, app)
	got := app
	for i := 0; i < 6; i++ {
		var res ctrl.Result
		res, got = reconcileSource(t, r, got)
		if res.RequeueAfter != pushDrivenRecheck {
			t.Fatalf("pass %d: requeue %v", i, res.RequeueAfter)
		}
		advance(fake, 4*time.Hour)
	}
	if ls.calls != 0 {
		t.Fatalf("a repository the GitHub App covers was listed %d times on a timer", ls.calls)
	}
	// A person's deploy reads the branch, once.
	got.Annotations[configvars.AnnotationDeployRequested] = fake.Time().Format(time.RFC3339Nano)
	if err := r.Update(context.Background(), got); err != nil {
		t.Fatal(err)
	}
	advance(fake, time.Second)
	if _, got = reconcileSource(t, r, got); ls.calls != 1 {
		t.Fatalf("the deploy: %d listings", ls.calls)
	}
	if _, got = reconcileSource(t, r, got); ls.calls != 1 {
		t.Fatalf("the deploy read twice: %d listings", ls.calls)
	}
	// Taken out of the installation: once its token ages, the App no longer
	// covers it, and the timer lists it again although App pushes arrived.
	fake.SetRepos(42, githubtest.Repo{FullName: "Acme/site", DefaultBranch: "trunk"})
	r.GitHub.Forget(store.DefaultWorkspace)
	advance(fake, 2*time.Hour)
	if _, _ = reconcileSource(t, r, got); ls.calls != 2 {
		t.Errorf("no longer covered: %d listings", ls.calls)
	}
}

func TestAProjectsOwnWebhookStopsItsChecksButTheAppsDoesNot(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": otherCommit}}
	viaApp := withPush(pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit),
		gitauth.Push{Ref: "refs/heads/main", SHA: otherCommit, Delivery: "d-1", Author: "alice", At: sourceNow, App: true})
	viaHook := withPush(pinGit(gitApp("blog", "https://gitlab.com/acme/blog"), testCommit),
		gitauth.Push{Ref: "refs/heads/main", SHA: otherCommit, Delivery: "d-2", Author: "alice", At: sourceNow})
	r, _ := newSourceReconciler(t, ls, viaApp, viaHook)
	clock := sourceNow
	r.Now = func() time.Time { return clock }
	_, app := reconcileSource(t, r, viaApp)
	_, hook := reconcileSource(t, r, viaHook)
	if sourceRecord(app).ProjectHook || !sourceRecord(hook).ProjectHook || ls.calls != 0 {
		t.Fatalf("records %+v %+v, %d listings", sourceRecord(app), sourceRecord(hook), ls.calls)
	}
	clock = sourceNow.Add(time.Hour)
	reconcileSource(t, r, hook)
	if ls.calls != 0 {
		t.Errorf("a project whose webhook delivered was listed: %d", ls.calls)
	}
	// Not covered (no App here): a push through the App does not stop the
	// checks.
	reconcileSource(t, r, app)
	if ls.calls != 1 {
		t.Errorf("an uncovered repository with App pushes only: %d listings", ls.calls)
	}
}
```

In `internal/controller/source_controller_test.go`, `TestSourcePollsOnlyWhenDue`'s first check becomes `if ls.calls != 0 || res.RequeueAfter != DefaultPollInterval-30*time.Second {`; `TestSourceDeploysAPushToTheBranch`'s last requeue check reads:

```go
	if res.RequeueAfter != pushDrivenRecheck {
		t.Errorf("a project whose webhook delivered is never listed on a timer; its next pass looks at its cover: %v", res.RequeueAfter)
	}
	if !rec.ProjectHook {
		t.Error("the project's own webhook delivered")
	}
```

In `internal/controller/source_gates_test.go`, the advances that stood for the 30-second recheck become the recheck:
- `TestAHeldCommitDeploysOnceItsCIPasses`: `advance(fake, 25*time.Second)` → `advance(fake, heldRecheck)`;
- `TestACommitWithNoCIDeploysAfterAMinute`: the first `advance(fake, 30*time.Second)` stays (nothing is read yet); the second becomes `advance(fake, heldRecheck-30*time.Second)`, with the comment `// No CI: decided at the first read after a minute, the 2-minute recheck.`;
- `TestAHeldCommitStaysHeldWhileGitHubIsDown`, `TestAHeldCommitStaysHeldWhenShpyrdCannotTellWhetherTheAppCoversTheRepository` (both loops, and the `advance(fake, 30*time.Second)` before "pending"), `TestAHeldCommitKeepsItsWatchPathsNoteWhenItDeploys`, `TestAHeldCommitsWatchPathsAreCheckedOnceTheCoverIsKnown`, `TestAHeldCommitStaysHeldWhenTheAppNoLongerCoversTheRepository`: each `advance(fake, 30*time.Second)` → `advance(fake, heldRecheck)`;
- `TestARerunsNudgeIsReadWhenTheBranchIsDue`: its first case's name becomes `"two hours passed"` (the nudge is read without a listing now).

- [ ] **Step 2: Run them to see them fail**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp TMPDIR=/home/nkr/.cache/gotmp go test ./pkg/gitauth/ ./pkg/api/ ./internal/controller/ -run 'ProjectsWebhookDelivered|PushThroughTheApp|WatchMode|NeverListedOnATimer|OwnWebhookStops|PollsOnlyWhenDue|DeploysAPushToTheBranch|HeldCommit|NoCI'`
Expected: FAIL to compile: `unknown field ProjectHook`, `unknown field App`, `undefined: WatchMode`, `undefined: pushDrivenRecheck`.

- [ ] **Step 3: The push and the record**

In `pkg/gitauth/webhook.go`, `Push` gains after `Skip`:

```go
	// App: the push came through the GitHub App's webhook (RFC-0054), not
	// the project's own.
	App bool `json:"app,omitempty"`
```

and `SourceRecord`, after `Delivery`:

```go
	// ProjectHook: the project's own webhook has delivered (RFC-0054): a
	// repository the GitHub App does not cover is then no longer listed on
	// a timer.
	ProjectHook bool `json:"projectHook,omitempty"`
```

In `pkg/api/github_webhook.go`, `githubPush`: `push.Delivery, push.At = delivery, time.Now().UTC()` becomes `push.Delivery, push.At, push.App = delivery, time.Now().UTC(), true`.

- [ ] **Step 4: How the watcher learns of pushes**

In `internal/controller/source_controller.go`, the package comment's first paragraph ends: "A repository the GitHub App covers is never listed on a timer (its webhook, and the sweep's redeliveries, are the signal); another is listed until its own webhook has delivered." The constants become:

```go
const (
	// DefaultPollInterval is how often a branch whose pushes do not arrive
	// by webhook is read (SHPYRD_GIT_POLL_INTERVAL).
	DefaultPollInterval = 5 * time.Minute
	minPollInterval     = 30 * time.Second
	// pushDrivenRecheck is how often a branch whose pushes arrive by
	// webhook is reconciled: its cover is looked at again, and nothing is
	// listed.
	pushDrivenRecheck = time.Hour
	// recordRefresh is how stale the record's checkedAt may get: a check
	// that finds nothing new writes nothing (each write of the App wakes
	// the App controller) until the written time is this old.
	recordRefresh = time.Hour
	// forgetChecks drops a remembered check this old: its App is gone, or
	// the written record has caught up with it.
	forgetChecks = 2 * time.Hour
	// errorBackoff is the first wait after a failed listing; it doubles
	// up to the interval.
	errorBackoff = 30 * time.Second
	// maxFailures caps the failures counted: the backoff has reached the
	// interval by then, and a record that stops changing is not written on
	// each attempt.
	maxFailures = 6
)

// How shpyrd learns of a branch's pushes (WatchMode).
const (
	WatchGitHubApp = "github-app"
	WatchWebhook   = "webhook"
	WatchPoll      = "poll"
)

// WatchMode says how a branch's pushes reach shpyrd: through the GitHub
// App when it covers the repository; through the project's own webhook
// once it has delivered; else by listing the branch on a timer.
func WatchMode(covered bool, rec gitauth.SourceRecord) string {
	switch {
	case covered:
		return WatchGitHubApp
	case rec.ProjectHook:
		return WatchWebhook
	}
	return WatchPoll
}
```

`PollInterval`'s comment: "// PollInterval reads SHPYRD_GIT_POLL_INTERVAL ("5m", "30s"): empty or unreadable is the default, and nothing below 30 s."

In `Reconcile`:
- the reset of another branch's record keeps the hook: `rec = gitauth.SourceRecord{LastWebhookAt: rec.LastWebhookAt, Delivery: rec.Delivery, ProjectHook: rec.ProjectHook}`;
- right after `ev := sourceEvent{nudge: nudge}`: `covered := r.covered(ctx, app)`;
- the listing case reads `case push != nil || badPush || (app.PinnedCommit() == "" && rec.Failures == 0 && !remembered) || r.due(app, last, covered, now):`;
- the `default:` branch returns `ctrl.Result{RequeueAfter: r.requeue(app.UID, last, covered, nudge != nil, now)}`;
- the last line returns `ctrl.Result{RequeueAfter: r.requeue(app.UID, u.record, covered, false, now)}`.

In `decideSource`, the push block becomes:

```go
	if p := ev.push; p != nil {
		sha, via, message, author = strings.ToLower(p.SHA), "push", p.Message, p.Author
		if !p.At.IsZero() {
			seen = p.At.UTC()
		}
		u.record.LastWebhookAt = &seen
		u.record.Delivery = p.Delivery
		if !p.App {
			u.record.ProjectHook = true
		}
	}
```

`requeue` gains the cover:

```go
func (r *SourceReconciler) requeue(uid types.UID, rec gitauth.SourceRecord, covered, nudged bool, now time.Time) time.Duration {
	d := r.untilDue(rec, covered, now)
```

(the rest unchanged). Replace `due`, `wait` and `untilDue` with:

```go
// covered says the GitHub App covers the App's repository: cover's own
// test, an answer it cannot give being "no" (such a branch is listed, as
// before).
func (r *SourceReconciler) covered(ctx context.Context, app *shpyrdv1.App) bool {
	if r.GitHub == nil {
		return false
	}
	_, ok, _ := r.GitHub.coverQuietly(ctx, app)
	return ok
}

// deployAsked says a person deployed after the last check.
func deployAsked(app *shpyrdv1.App, rec gitauth.SourceRecord) bool {
	asked, err := time.Parse(time.RFC3339Nano, app.Annotations[configvars.AnnotationDeployRequested])
	return err == nil && rec.CheckedAt != nil && asked.After(*rec.CheckedAt)
}

// due says the branch should be read now: never read, a deploy asked
// after the last check, a failed read due again, or, for a branch whose
// pushes do not arrive by webhook, the wait since the last check has
// passed (90 % of it: requeues are spread ±10 %). A branch the App covers,
// or whose own webhook has delivered, is never read on a timer.
func (r *SourceReconciler) due(app *shpyrdv1.App, rec gitauth.SourceRecord, covered bool, now time.Time) bool {
	if rec.CheckedAt == nil || deployAsked(app, rec) {
		return true
	}
	if rec.Failures == 0 && WatchMode(covered, rec) != WatchPoll {
		return false
	}
	return now.Sub(*rec.CheckedAt) >= r.wait(rec)*9/10
}

// wait is the time between timer listings: the interval, or after
// failures a backoff from 30 s that doubles up to it.
func (r *SourceReconciler) wait(rec gitauth.SourceRecord) time.Duration {
	every := r.interval()
	if rec.Failures > 0 {
		return min(every, errorBackoff<<min(rec.Failures-1, 10))
	}
	return every
}

// interval is the wait between timer listings.
func (r *SourceReconciler) interval() time.Duration {
	if r.PollInterval <= 0 {
		return DefaultPollInterval
	}
	return r.PollInterval
}

// untilDue is the wait until the next pass: for a branch whose pushes
// arrive by webhook, pushDrivenRecheck, which only looks at the cover
// again; else until the next listing is due.
func (r *SourceReconciler) untilDue(rec gitauth.SourceRecord, covered bool, now time.Time) time.Duration {
	spread := r.Jitter
	if spread == nil {
		spread = jitter
	}
	if rec.Failures == 0 && WatchMode(covered, rec) != WatchPoll {
		return spread(pushDrivenRecheck)
	}
	left := r.wait(rec)
	if rec.CheckedAt != nil {
		left -= now.Sub(*rec.CheckedAt)
	}
	return spread(max(left, time.Second))
}
```

(`webhookInterval` and `webhookRecent` are gone.)

`TestPollIntervalAndJitter`'s table becomes `{"": 5 * time.Minute, "30s": 30 * time.Second, "nonsense": 5 * time.Minute, "1s": 30 * time.Second, "10m": 10 * time.Minute}`.

- [ ] **Step 5: CI's fallback and the shared read**

In `internal/controller/source_gates.go`:

```go
// heldRecheck is how often a held commit's checks are read when no
// webhook says they changed: the net under missed deliveries, which the
// sweep redelivers within about 5 minutes.
const heldRecheck = 2 * time.Minute
```

and in the budget block `ciShareFor = 90 * time.Second`, its comment line "a commit's checks and statuses, read for one App, serve the others on the repository this long".

- [ ] **Step 6: Run the tests**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp TMPDIR=/home/nkr/.cache/gotmp go build ./... && GOTMPDIR=/home/nkr/.cache/gotmp TMPDIR=/home/nkr/.cache/gotmp go test ./pkg/gitauth/ ./pkg/api/ ./internal/controller/`
Expected: PASS. A test still failing is one that expected a covered repository (`shopWith`, `gatedWatcher`) to be listed after time passed, or a read at 30 seconds: give it a deploy request (`configvars.AnnotationDeployRequested`) or `advance(fake, heldRecheck)`, keep its name, and list it in the commit message.

- [ ] **Step 7: Commit**

```bash
git add pkg/gitauth pkg/api/github_webhook.go pkg/api/github_webhook_test.go internal/controller/source_controller.go internal/controller/source_controller_test.go internal/controller/source_gates.go internal/controller/source_gates_test.go
git commit -m "feat(controller): a repository the GitHub App covers is never listed on a timer; CI's fallback every 2 minutes, shared 90 seconds

Co-Authored-By: <your model name> <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 11: Other hosts: one listing per repository, under a cap; how the Source block says it

**Files:**
- Create: `internal/controller/source_poll.go`, `internal/controller/source_poll_test.go`
- Modify: `internal/controller/source_controller.go` (`SourceReconciler` fields; `Reconcile`'s listing case; `poll`), `internal/controller/source_controller_test.go` (`newSourceReconciler`), `pkg/server/server.go` (`SourceReconciler` ~576), `pkg/api/github_features.go` (`SourceWatchView`, `projectDetail`), `pkg/api/apps.go` (`AppDetail`)
- Modify: `apps/workspace/src/api/types.ts` (`Project`), `apps/workspace/mock/projects.json`, `apps/workspace/src/screens/project/source.tsx`, `source.test.tsx`
- Test: `pkg/api/github_access_test.go` (the view)

**Interfaces:**
- Consumes: `WatchMode`, `WatchPoll`, `deployAsked`, `interval()`, `PollInterval` (Task 10).
- Produces:
  - `const controller.DefaultPollRate = 5.0`; `func controller.PollRate(v string) float64`; `SourceReconciler.PollRate float64`
  - `type pollBucket struct`; `func (b *pollBucket) take(now time.Time, asked bool) (time.Duration, bool)`
  - `type pollDeferred struct { wait time.Duration }` (an `error`)
  - `func listingKey(cloneURL string, b gitauth.Build) string`
  - `func (r *SourceReconciler) poll(ctx, app, timer bool, now time.Time) (string, error)`
  - `type api.SourceWatchView struct { Mode string; PollEvery int }` (json `mode`, `pollEvery,omitempty`); `AppDetail.SourceWatch *SourceWatchView` (json `sourceWatch,omitempty`)
  - dashboard: `Project.sourceWatch?: { mode: "github-app" | "webhook" | "poll"; pollEvery?: number }`, `Project.githubLinkedBy?: string`

- [ ] **Step 1: Write the failing tests**

`internal/controller/source_poll_test.go`:

```go
package controller

import (
	"fmt"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
)

func TestTheBucketCapsListingsAndLetsAPersonsReadThrough(t *testing.T) {
	b := &pollBucket{rate: 5}
	now := sourceNow
	for i := 0; i < 5; i++ {
		if _, ok := b.take(now, false); !ok {
			t.Fatalf("take %d of the burst", i)
		}
	}
	w1, ok1 := b.take(now, false)
	w2, ok2 := b.take(now, false)
	if ok1 || ok2 || w1 != 200*time.Millisecond || w2 != 400*time.Millisecond {
		t.Errorf("over the cap: %v %v, %v %v", ok1, w1, ok2, w2)
	}
	// A read a person caused borrows up to a burst ahead.
	for i := 0; i < 5; i++ {
		if _, ok := b.take(now, true); !ok {
			t.Fatalf("a person's read %d", i)
		}
	}
	if _, ok := b.take(now, true); ok {
		t.Error("borrowed past a burst")
	}
	// Three seconds later: the debt is paid, and the queue has moved.
	if _, ok := b.take(now.Add(3*time.Second), false); !ok {
		t.Error("refilled")
	}
}

func TestPollRateIsReadFromItsVariable(t *testing.T) {
	for in, want := range map[string]float64{"": 5, "2": 2, "0.5": 0.5, "0": 5, "-1": 5, "fast": 5, "NaN": 5} {
		if got := PollRate(in); got != want {
			t.Errorf("PollRate(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestAListingIsSharedByRepositoryAndCredential(t *testing.T) {
	key := func(url string, b gitauth.Build) string { return listingKey(url, b) }
	plain := gitauth.Build{}
	if key("https://gitlab.com/Acme/Mono", plain) != key("https://gitlab.com/acme/mono.git", plain) {
		t.Error("one repository in two forms is two listings")
	}
	if key("https://gitlab.com/acme/mono", plain) == key("https://gitlab.com/acme/other", plain) {
		t.Error("two repositories share a listing")
	}
	keyA := gitauth.Build{Kind: gitauth.KindDeployKey, Secret: &corev1.Secret{Data: map[string][]byte{"ssh-privatekey": []byte("a")}}}
	keyB := gitauth.Build{Kind: gitauth.KindDeployKey, Secret: &corev1.Secret{Data: map[string][]byte{"ssh-privatekey": []byte("b")}}}
	if key("git@gitlab.com:acme/mono.git", keyA) == key("git@gitlab.com:acme/mono.git", keyB) {
		t.Error("two deploy keys share a listing")
	}
	if k := key("git@gitlab.com:acme/mono.git", keyA); strings.Contains(k, "ssh-privatekey") || strings.HasSuffix(k, ":a") {
		t.Errorf("the key shows the credential: %q", k)
	}
}

func TestARepositoryIsListedOnceForItsProjectsThenNotAfterItsWebhook(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": otherCommit}}
	names := []string{"api", "web", "worker"}
	var objs []client.Object
	for _, name := range names {
		objs = append(objs, withRecord(pinGit(gitApp(name, "https://gitlab.com/acme/mono"), testCommit),
			gitauth.SourceRecord{Ref: "main", Latest: testCommit, CheckedAt: at(sourceNow.Add(-10 * time.Minute))}))
	}
	r, c := newSourceReconciler(t, ls, objs...)
	clock := sourceNow
	r.Now = func() time.Time { return clock }
	for _, o := range objs {
		if _, got := reconcileSource(t, r, o.(*shpyrdv1.App)); got.PinnedCommit() != otherCommit {
			t.Fatalf("%s: pin %s", o.GetName(), got.PinnedCommit())
		}
	}
	if ls.calls != 1 {
		t.Fatalf("three projects on one repository: %d listings", ls.calls)
	}
	// Each project's own webhook delivers once: none is listed again.
	const newest = "fedcba9876543210fedcba9876543210fedcba98"
	for i, o := range objs {
		got := &shpyrdv1.App{}
		if err := c.Get(t.Context(), client.ObjectKeyFromObject(o), got); err != nil {
			t.Fatal(err)
		}
		withPush(got, gitauth.Push{Ref: "refs/heads/main", SHA: newest, Before: otherCommit, Author: "alice", Delivery: fmt.Sprintf("d-%d", i), At: clock})
		if err := c.Update(t.Context(), got); err != nil {
			t.Fatal(err)
		}
		if _, got = reconcileSource(t, r, got); got.PinnedCommit() != newest || !sourceRecord(got).ProjectHook {
			t.Fatalf("%s: pin %s, record %+v", o.GetName(), got.PinnedCommit(), sourceRecord(got))
		}
	}
	for _, d := range []time.Duration{10 * time.Minute, time.Hour, 24 * time.Hour} {
		clock = sourceNow.Add(d)
		for _, o := range objs {
			if res, _ := reconcileSource(t, r, o.(*shpyrdv1.App)); res.RequeueAfter != pushDrivenRecheck {
				t.Errorf("%s after %v: requeue %v", o.GetName(), d, res.RequeueAfter)
			}
		}
	}
	if ls.calls != 1 {
		t.Errorf("listed after the webhooks delivered: %d listings", ls.calls)
	}
}

// Review Focus 6.
func TestAThousandProjectsStayUnderTheCap(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": testCommit}}
	var objs []client.Object
	for i := 0; i < 1000; i++ {
		objs = append(objs, withRecord(pinGit(gitApp(fmt.Sprintf("p%d", i), fmt.Sprintf("https://gitlab.com/acme/p%d", i)), testCommit),
			gitauth.SourceRecord{Ref: "main", Latest: testCommit, CheckedAt: at(sourceNow.Add(-time.Hour))}))
	}
	r, _ := newSourceReconciler(t, ls, objs...)
	r.PollRate = 5
	clock := sourceNow
	r.Now = func() time.Time { return clock }
	next := map[string]time.Time{}
	for _, o := range objs {
		next[o.GetName()] = sourceNow
	}
	for s := 0; s <= 240; s++ {
		clock = sourceNow.Add(time.Duration(s) * time.Second)
		before := ls.calls
		for _, o := range objs {
			when, ok := next[o.GetName()]
			if !ok || when.After(clock) {
				continue
			}
			res, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(o)})
			if err != nil {
				t.Fatal(err)
			}
			next[o.GetName()] = clock.Add(res.RequeueAfter)
		}
		if n := ls.calls - before; n > 5 {
			t.Fatalf("second %d: %d listings, over the cap of 5", s, n)
		}
	}
	if ls.calls != 1000 {
		t.Errorf("%d listings in 240 s: each of the thousand once, within 1000 / 5 = 200 s", ls.calls)
	}
}
```

Append to `pkg/api/github_access_test.go`:

```go
func TestTheProjectSaysHowItLearnsOfPushes(t *testing.T) {
	s, _, _ := githubServer(t,
		onGitHub("shop", "https://github.com/acme/shop", "main", ""),
		recordOn(onGitHub("blog", "https://gitlab.com/acme/blog", "main", ""), gitauth.SourceRecord{Ref: "main", ProjectHook: true}),
		recordOn(onGitHub("docs", "https://gitlab.com/acme/docs", "main", ""), gitauth.SourceRecord{Ref: "main", LastWebhookAt: &time.Time{}}),
	)
	connectAcme(t, s, store.DefaultWorkspace)
	for slug, want := range map[string]string{"shop": `"sourceWatch":{"mode":"github-app"}`, "blog": `"sourceWatch":{"mode":"webhook"}`, "docs": `"sourceWatch":{"mode":"poll","pollEvery":300}`} {
		rec := do(t, s, "GET", "/api/projects/"+slug, "", true)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%s: %d %s", slug, rec.Code, rec.Body.String())
		}
	}
}
```

(the file's imports gain `"time"` and `"github.com/shpyrd-io/shpyrd/pkg/gitauth"`.)

Append to `apps/workspace/src/screens/project/source.test.tsx`:

```tsx
it("says a repository the GitHub App covers deploys on push through it, and who connected it", () => {
  const p = projectWith(repo, { commit, latest: commit, checkedAt: minutesAgo(3) });
  p.sourceWatch = { mode: "github-app" };
  p.githubLinkedBy = "paezao";
  show(p);
  expect(screen.getByText("Deploys on push through the GitHub App. Connected by paezao.")).toBeTruthy();
});

it("says a project whose webhook delivered deploys on push through it, and when it last did", () => {
  const p = projectWith({ url: "https://gitlab.com/acme/shop", revision: "main" }, { commit, latest: commit, lastWebhookAt: minutesAgo(3 * 24 * 60) });
  p.sourceWatch = { mode: "webhook" };
  show(p);
  expect(screen.getByText("Deploys on push through its webhook (last delivery 3 days ago).")).toBeTruthy();
});

it("says how often a polled branch is checked, and how to stop the checks", () => {
  const p = projectWith({ url: "https://gitlab.com/acme/shop", revision: "main" }, { commit, latest: commit, checkedAt: minutesAgo(1) });
  p.sourceWatch = { mode: "poll", pollEvery: 300 };
  show(p);
  expect(screen.getByText("Checked every 5 min. Set up the webhook to deploy at once and stop the checks.")).toBeTruthy();
});
```

- [ ] **Step 2: Run them to see them fail**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp TMPDIR=/home/nkr/.cache/gotmp go test ./internal/controller/ ./pkg/api/ -run 'Bucket|PollRate|SharedByRepository|ListedOnceForItsProjects|AThousandProjects|HowItLearnsOfPushes'` and `npm run test --workspace apps/workspace -- source`
Expected: FAIL to compile: `undefined: pollBucket`, `undefined: PollRate`, `r.PollRate undefined`; the Source block lacks the three sentences.

- [ ] **Step 3: The bucket and the shared listings**

`internal/controller/source_poll.go`:

```go
package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
)

// Listings of repositories the GitHub App does not cover (RFC-0054,
// webhooks before polling): one listing answers every project on a
// repository read with the same credential, for the interval; and the
// cluster lists at most SHPYRD_GIT_POLL_RATE repositories a second, from a
// token bucket. A read a person caused (a new project, a deploy, a push
// that does not follow) lists fresh and may borrow a burst ahead.

// DefaultPollRate is how many listings a second the cluster makes at most
// (SHPYRD_GIT_POLL_RATE).
const DefaultPollRate = 5.0

// PollRate reads SHPYRD_GIT_POLL_RATE: listings a second; empty,
// unreadable or not above 0 is the default.
func PollRate(v string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || f <= 0 || math.IsNaN(f) || math.IsInf(f, 0) {
		return DefaultPollRate
	}
	return f
}

// pollBucket is the cluster-wide cap: rate listings a second, a burst of
// as many (at least one). A listing over the cap is told how long to
// wait: its place in the queue, so a crowd spreads over time.
type pollBucket struct {
	mu     sync.Mutex
	rate   float64
	tokens float64
	queued float64
	last   time.Time
}

// take spends a listing now, or says how long until this one's turn. asked
// is a read a person caused: it may borrow up to a burst ahead.
func (b *pollBucket) take(now time.Time, asked bool) (time.Duration, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	burst := math.Max(1, b.rate)
	if b.last.IsZero() {
		b.tokens, b.last = burst, now
	}
	if now.After(b.last) {
		elapsed := now.Sub(b.last).Seconds()
		b.tokens = math.Min(burst, b.tokens+elapsed*b.rate)
		b.queued = math.Max(0, b.queued-elapsed*b.rate)
		b.last = now
	}
	floor := 1.0
	if asked {
		floor = 1 - burst
	}
	if b.tokens >= floor {
		b.tokens--
		return 0, true
	}
	b.queued++
	wait := time.Duration((floor - b.tokens + b.queued - 1) / b.rate * float64(time.Second))
	return max(wait, time.Millisecond), false
}

// pollDeferred is a listing over the cap: the App's turn comes after wait.
type pollDeferred struct{ wait time.Duration }

func (e *pollDeferred) Error() string {
	return fmt.Sprintf("over the cluster's cap of listings: the turn comes in %v", e.wait)
}

// repoListing is what one listing of a repository found, and when.
type repoListing struct {
	refs map[string]string
	at   time.Time
}

// listingKey is a repository as one listing answers it: its host and path
// in lower case, and the credential it is read with (its kind, and a
// fingerprint of the Secret's data, never the data). Projects with the
// same key share a listing.
func listingKey(cloneURL string, b gitauth.Build) string {
	where := strings.ToLower(strings.TrimSpace(cloneURL))
	if repo, err := gitauth.Parse(cloneURL); err == nil {
		where = strings.ToLower(repo.Host + "/" + repo.Path)
	}
	cred := b.Kind
	if b.Secret != nil {
		if b.Kind == gitauth.KindGitHubApp {
			cred += ":" + strings.ToLower(b.Secret.Annotations[gitauth.AnnotationGitHubAccount])
		} else {
			cred += ":" + secretFingerprint(b.Secret)
		}
	}
	return where + "|" + cred
}

func secretFingerprint(sec *corev1.Secret) string {
	keys := make([]string, 0, len(sec.Data))
	for k := range sec.Data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte{0})
		h.Write(sec.Data[k])
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// bucket is the watcher's cap, made at first use.
func (r *SourceReconciler) bucket() *pollBucket {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.polls == nil {
		rate := r.PollRate
		if rate <= 0 {
			rate = DefaultPollRate
		}
		r.polls = &pollBucket{rate: rate}
	}
	return r.polls
}

// sharedListing is a listing of the key made less than 90 % of the
// interval ago.
func (r *SourceReconciler) sharedListing(key string, now time.Time) (repoListing, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	l, ok := r.listings[key]
	if !ok || now.Before(l.at) || now.Sub(l.at) >= r.interval()*9/10 {
		return repoListing{}, false
	}
	return l, true
}

// keepListing keeps a listing that answered, and drops those past the
// interval.
func (r *SourceReconciler) keepListing(key string, l repoListing) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.listings == nil {
		r.listings = map[string]repoListing{}
	}
	for k, old := range r.listings {
		if l.at.Sub(old.at) >= r.interval() {
			delete(r.listings, k)
		}
	}
	r.listings[key] = l
}
```

In `internal/controller/source_controller.go`, `SourceReconciler` gains after `PollInterval`:

```go
	// PollRate is the cluster's cap on listings a second; zero is
	// DefaultPollRate.
	PollRate float64
```

and after `ciShared map[ciKey]ciInputs`:

```go
	// polls is the cap; listings are the repositories' last listings, by
	// listingKey, shared by the projects on them.
	polls    *pollBucket
	listings map[string]repoListing
```

`poll` becomes:

```go
// poll lists the repository with the build's credential and finds the
// revision's commit. A timer's check uses a listing of the same repository
// and credential made within the interval; any listing waits its turn
// under the cluster's cap (a *pollDeferred). A listing cut at the cap
// still answers when the revision is among what was read.
func (r *SourceReconciler) poll(ctx context.Context, app *shpyrdv1.App, timer bool, now time.Time) (string, error) {
	b, err := lookupGitBuild(ctx, r.Client, r.SystemNamespace, r.GitHub, app)
	if err != nil {
		return "", err
	}
	// The URL the build clones, in the form the credential fits: an
	// https URL with a deploy key is read over SSH.
	cloneURL := firstNonEmpty(b.CloneURL, app.Spec.Source.Git.URL)
	key := listingKey(cloneURL, b)
	var refs map[string]string
	if l, ok := r.sharedListing(key, now); ok && timer {
		refs = l.refs
	} else {
		if wait, ok := r.bucket().take(now, !timer); !ok {
			return "", &pollDeferred{wait: wait}
		}
		list := r.LsRemote
		if list == nil {
			list = gitauth.LsRemote
		}
		refs, err = list(ctx, cloneURL, b)
		if err == nil {
			// Only a complete listing is shared: a failed one is read again
			// by the backoff, a cut one by each project.
			r.keepListing(key, repoListing{refs: refs, at: now})
		}
	}
	if err != nil && !errors.Is(err, gitauth.ErrTooManyRefs) {
		if b.AppRefused != "" {
			return "", &appRefusedError{why: b.AppRefused, err: err}
		}
		return "", err
	}
	if sha, ok := gitauth.ResolveBranch(refs, app.GitRevision()); ok {
		return strings.ToLower(sha), nil
	}
	if err != nil {
		return "", err
	}
	return "", errNoSuchRevision
}
```

In `Reconcile`, the listing case starts:

```go
	case push != nil || badPush || (app.PinnedCommit() == "" && rec.Failures == 0 && !remembered) || r.due(app, last, covered, now):
		// A push that does not follow the commit last seen may be late,
		// or delivered again: the branch says what its head is. Only a
		// check the timer asked for shares a listing.
		timer := push == nil && !badPush && app.PinnedCommit() != "" && last.CheckedAt != nil && !deployAsked(app, last)
		ev.latest, ev.err = r.poll(ctx, app, timer, now)
		var deferred *pollDeferred
		if errors.As(ev.err, &deferred) {
			// Over the cluster's cap: this App's turn comes after the wait;
			// nothing is written or dropped meanwhile.
			return ctrl.Result{RequeueAfter: deferred.wait}, nil
		}
```

(the rest of the case as it is).

- [ ] **Step 4: The cap from the environment, and out of the other tests' way**

In `pkg/server/server.go`, the `SourceReconciler` literal gains `PollRate: controller.PollRate(os.Getenv("SHPYRD_GIT_POLL_RATE")),` after `PollInterval`.

In `internal/controller/source_controller_test.go`, `newSourceReconciler`'s literal gains `PollRate: 1000,` after `PollInterval: DefaultPollInterval,`: the watcher's other tests list many times at one instant of the fake clock and are not about the cap; the cap's own tests set `r.PollRate = 5`.

- [ ] **Step 5: The project says how its pushes arrive**

In `pkg/api/github_features.go` (imports gain `"github.com/shpyrd-io/shpyrd/pkg/gitauth"`):

```go
// SourceWatchView says how shpyrd learns of a branch's pushes (RFC-0018,
// RFC-0054): "github-app", "webhook", or "poll" every PollEvery seconds.
type SourceWatchView struct {
	Mode      string `json:"mode"`
	PollEvery int    `json:"pollEvery,omitempty"`
}
```

and in `projectDetail`, before `return d`:

```go
	if gitauth.Watched(app) {
		rec := gitauth.ReadSourceRecord(app.Annotations[shpyrdv1.AnnotationGitSource])
		w := &SourceWatchView{Mode: controller.WatchMode(d.GitHubFeatures, rec)}
		if w.Mode == controller.WatchPoll {
			w.PollEvery = int(controller.PollInterval(s.opts.Vars("SHPYRD_GIT_POLL_INTERVAL")).Seconds())
		}
		d.SourceWatch = w
	}
```

`AppDetail` gains after `GitHubLinkedBy`:

```go
	// SourceWatch says how shpyrd learns of the branch's pushes.
	SourceWatch *SourceWatchView `json:"sourceWatch,omitempty"`
```

- [ ] **Step 6: The Source block**

`types.ts`, `Project` gains after `sourceNote?: string;`:

```ts
  // How shpyrd learns of the branch's pushes: the GitHub App, the
  // project's own webhook, or a check every pollEvery seconds.
  sourceWatch?: { mode: "github-app" | "webhook" | "poll"; pollEvery?: number };
  // The GitHub account whose access picked the repository.
  githubLinkedBy?: string;
```

`mock/projects.json`: `hello-world` gains `"sourceWatch": { "mode": "github-app" }, "githubLinkedBy": "ana-souza"`; `docs` gains `"sourceWatch": { "mode": "github-app" }`; `billing` and `reports` gain `"sourceWatch": { "mode": "poll", "pollEvery": 300 }`; `metrics` gains `"sourceWatch": { "mode": "webhook" }`.

`source.tsx`, after `minutesSince`:

```tsx
// "3 days ago", "2 hours ago", "5 min ago", since an ISO time.
const longAgo = (iso: string) => {
  const m = Math.max(0, Math.floor((Date.now() - Date.parse(iso)) / 60_000));
  if (m < 1) return "just now";
  if (m < 60) return `${m} min ago`;
  const h = Math.floor(m / 60);
  if (h < 48) return `${h} hour${h === 1 ? "" : "s"} ago`;
  return `${Math.floor(h / 24)} days ago`;
};

// How the branch's pushes reach shpyrd (RFC-0054): the GitHub App, the
// project's own webhook, or a check on a timer, and how to stop it.
function WatchLine({ project }: { project: Project }) {
  const watch = project.sourceWatch;
  if (!watch) return null;
  let words: string;
  switch (watch.mode) {
    case "github-app":
      words = `Deploys on push through the GitHub App.${project.githubLinkedBy ? ` Connected by ${project.githubLinkedBy}.` : ""}`;
      break;
    case "webhook": {
      const last = project.status.source?.lastWebhookAt;
      words = last ? `Deploys on push through its webhook (last delivery ${longAgo(last)}).` : "Deploys on push through its webhook.";
      break;
    }
    default: {
      const every = watch.pollEvery ?? 300;
      words = `Checked every ${every >= 60 ? `${Math.round(every / 60)} min` : `${every} s`}. Set up the webhook to deploy at once and stop the checks.`;
    }
  }
  return <p className="text-xs text-muted-foreground">{words}</p>;
}
```

and in `GitSource`, right after the closing `</InfoTable>` of the branch view: `<WatchLine project={project} />`.

- [ ] **Step 7: Run the tests**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp TMPDIR=/home/nkr/.cache/gotmp go build ./... && GOTMPDIR=/home/nkr/.cache/gotmp TMPDIR=/home/nkr/.cache/gotmp go test ./internal/controller/ ./pkg/api/ ./pkg/server/ && npm run test --workspace apps/workspace && npm run lint --workspace apps/workspace && npm run typecheck --workspace apps/workspace`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/controller/source_poll.go internal/controller/source_poll_test.go internal/controller/source_controller.go internal/controller/source_controller_test.go pkg/server/server.go pkg/api/github_features.go pkg/api/apps.go pkg/api/github_access_test.go apps/workspace/src apps/workspace/mock
git commit -m "feat(controller): other hosts listed once per repository under a cluster-wide cap, every 5 minutes until their webhook delivers

Co-Authored-By: <your model name> <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 12: Docs

**Files:**
- Modify: `content/docs/deploying.mdx` (Deploying on push, "Without one" ~137-142; Connect GitHub ~178-204; Wait for CI ~237), `content/docs/cli.mdx` (whoami row ~40; the GitHub section ~124-133), `content/docs/installation.mdx` (Connect GitHub ~199)

**Interfaces:**
- Consumes: the behaviour of Tasks 1-11.
- Produces: nothing for code.

- [ ] **Step 1: Deploying on push**

In `content/docs/deploying.mdx`, replace the paragraph that starts `**Without one: within minutes.**` with:

```mdx
**Without one: within 5 minutes.** shpyrd checks the branch every 5 minutes
(the operator sets it with `SHPYRD_GIT_POLL_INTERVAL`, 30 seconds at least).
Projects on one repository share one check, and the platform makes at most
5 checks a second however many projects there are
(`SHPYRD_GIT_POLL_RATE`). Once the project's webhook has delivered, the
checks stop: pushes arrive by webhook from then on. The Source block says
which: "Checked every 5 min. Set up the webhook to deploy at once and stop
the checks.", or "Deploys on push through its webhook (last delivery 3 days
ago)." A repository shpyrd cannot read says why on the project, with the
`shpyrd git-auth` command that fixes it, and shpyrd tries again by itself.
A deploy reads the branch at once, webhook or not.
```

- [ ] **Step 2: Connect GitHub, and linking your own account**

Replace the paragraph that starts `**Make a project from a repository:**` and its code block with:

````mdx
**Link your GitHub account.** Each person links their own GitHub account
once, as on Vercel. In New project, **From GitHub** shows **Continue with
GitHub**: GitHub asks you to authorize the App in a small window, and the
dialog keeps what you typed. From a terminal, `shpyrd github link` opens the
browser and waits; `shpyrd whoami` shows the account. The person menu
shows **GitHub: <login>**, with **Unlink GitHub**. An API token acts as the
person who made it, so an agent with your token picks what you can pick,
without a browser.

**Make a project from a repository:** New project, **From GitHub**, pick
the account (the workspace's accounts your GitHub account can see), filter
the list, pick the repository and the branch (its default branch first).
The list shows only the repositories you can read on GitHub. **Can't find a
repository?** opens the account's installation on GitHub, where repositories
are added; **Add GitHub account or organisation** connects another
(workspace admins). Or find it with `shpyrd github repos --filter shop` and
deploy it as any git repository:

```shell
shpyrd deploy --git https://github.com/acme/shop --ref main --project shop
```

**Who can pick what.** A project is made from a private repository on
GitHub, or moved to one, only by someone who can read it on GitHub (for a
token, the person who made it): otherwise shpyrd answers "You can't read
acme/shop on GitHub", or asks you to link your account first. A public
repository needs no link. A redeploy of the same repository is not checked.
The project shows who connected it ("Connected by paezao"). Builds,
webhooks, checks and Deployments go through the workspace's installation,
never through your account: when you unlink, or leave the workspace, your
projects keep deploying.

**What shpyrd keeps.** GitHub's user token for the App can do only what
both you and the App may: read contents and metadata, checks and
Deployments. Its access token lasts 8 hours and lives in the platform's
memory only; its refresh token lasts 6 months, is rotated on every use, and
is stored encrypted (AES-256-GCM) under a key in the Secret
`shpyrd-token-key`, apart from the database. Unlinking revokes it on GitHub.
A refresh token unused for 6 months expires, and New project asks you to
link again; so does a new token key, which an operator makes by deleting
that Secret.
````

In the paragraph that starts `**Pushes** arrive through the App's own webhook`, append: "A repository the App covers is never checked on a timer: every 5 minutes shpyrd asks GitHub to deliver again whatever push, CI result or installation event did not reach it (GitHub keeps them three days), so a missed delivery deploys within about 5 minutes. A delivery GitHub never made is not caught: push again, or Redeploy."

- [ ] **Step 3: Wait for CI**

In the paragraph that starts `shpyrd learns of results through the App's webhook`, replace "and checks a waiting commit every 30 seconds in case a delivery is missed" with "and checks a waiting commit every 2 minutes in case a delivery is missed (the projects on one repository share what GitHub said for 90 seconds)".

- [ ] **Step 4: The CLI reference**

In `content/docs/cli.mdx`, the `shpyrd whoami` row becomes:

```mdx
| `shpyrd whoami` | Who you are at the current workspace, checked live, and your linked GitHub account (`GitHub: paezao`, or `GitHub: not linked`); fails when the token expired or was revoked. |
```

The GitHub section's sentence becomes "Link your own GitHub account; connect an account or organisation once; then pick the repositories you can read. Signed in only. See [Connect GitHub](/docs/deploying#connect-github)." Its table starts with:

```mdx
| `shpyrd github link [--no-browser]` | Open the browser to link your GitHub account, and wait until it is linked. New projects from GitHub, the CLI and your agents (with your API token) then reach the repositories you can read. A command that needs it says "link your GitHub account first: run shpyrd github link". |
| `shpyrd github unlink` | Unlink your GitHub account and revoke its token on GitHub. Your projects keep deploying. |
```

and the `repos` row becomes:

```mdx
| `shpyrd github repos [--filter TEXT]` | The repositories you can read through the workspace's connections, with their default branch. |
```

- [ ] **Step 5: Installation**

In `content/docs/installation.mdx`, after the paragraph that starts `A browser opens on GitHub with the App filled in`, add:

```mdx
People link their own GitHub accounts through the same App, at its setup page: the App asks for user authorization during installation, and its user tokens must expire (GitHub's default for new Apps: **Optional features**, "User-to-server token expiration"). An App registered before GitHub made it the default may have it off: turn it on in the App's settings, or linking says so. The server keeps the key that encrypts the people's GitHub tokens in the Secret `shpyrd-system/shpyrd-token-key`, made at its first start; deleting it means everyone links again.
```

- [ ] **Step 6: Check the site builds**

The docs are rendered by the website (`apps/website`, which reads `@shpyrd/content`).
Run: `npm run build --workspace apps/website && git diff --stat content/`
Expected: the build passes; three files changed. Then `npm run dev --workspace apps/website` and read the three sections at `http://localhost:4324/docs/deploying`, `/docs/cli` and `/docs/installation`: the lowercase brand, the code blocks, the links.

- [ ] **Step 7: Commit**

```bash
git add content/docs/deploying.mdx content/docs/cli.mdx content/docs/installation.mdx
git commit -m "docs: link your GitHub account, who can pick what, the catch-up, and polling for other hosts

Co-Authored-By: <your model name> <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 13: The real run on kind, screenshots, the PR

**Done after the final review of the whole branch, not before.** Tasks 1 to
12 are reviewed one by one and then together; this task runs the result
against the real test GitHub App. A fault found here is fixed in the task
that owns the code, with a test, and reviewed again.

**Who clicks what.** Everything on GitHub's pages is the maintainer's:
authorizing the App, its settings, the delivery log, a second GitHub
account. The agent may, with the maintainer's leave already given, push
commits to `paezao/shpyrd-e2e-hello` from a clone in the scratchpad and set
that repository's Actions variables. Ask the maintainer to confirm at the
start: the test App is registered and installed on
`paezao/shpyrd-e2e-hello`, connected to the default workspace of the kind
cluster; ngrok is configured (`~/.config/ngrok/ngrok.yml`) with a static
domain, or the App's webhook URL may be changed during the run; and whether
a second GitHub account without access to the test repository is available
for Run 2. Wait for the answer.

**Files:**
- Modify: `rfcs/0054-github-app.md` (Implementation History)

- [ ] **Step 1: Full test run**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp TMPDIR=/home/nkr/.cache/gotmp go build ./... && GOTMPDIR=/home/nkr/.cache/gotmp TMPDIR=/home/nkr/.cache/gotmp go vet ./... && GOTMPDIR=/home/nkr/.cache/gotmp TMPDIR=/home/nkr/.cache/gotmp go test ./... && SHPYRD_TEST_DATABASE_URL=<the dev database> GOTMPDIR=/home/nkr/.cache/gotmp TMPDIR=/home/nkr/.cache/gotmp go test ./pkg/store/ && npm run test --workspace apps/workspace && npm run lint --workspace apps/workspace && npm run typecheck --workspace apps/workspace`
Expected: all pass.

- [ ] **Step 2: The cluster and the build**

Kind on 80/443 (the dashboard `https://127.0.0.1.nip.io`; 8443 breaks the local sign-in). `make ui`, then `GOTMPDIR=/home/nkr/.cache/gotmp TMPDIR=/home/nkr/.cache/gotmp make dev-deploy`; never `shpyrd extensions enable` after it (it reverts to the published image). Check the image with `kubectl -n shpyrd-system get deploy shpyrd-server -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="SHPYRD_VERSION")].value}'`. The migration runs at start: `kubectl -n shpyrd-system logs deploy/shpyrd-server | grep -c 000029` prints at least `1`. The key exists and nothing prints it: `kubectl -n shpyrd-system get secret shpyrd-token-key -o jsonpath='{.metadata.creationTimestamp}'` (never `-o yaml`).

- [ ] **Step 3: The App's settings**

The maintainer opens the test App's settings on GitHub and checks, with a screenshot for the PR: **Request user authorization (OAuth) during installation** on; **Optional features**, "User-to-server token expiration" on (opt in if it is off); the callback URL `https://127.0.0.1.nip.io/api/github/installed` (a browser redirect: the local browser reaches it, no tunnel needed); the events `push`, `check_suite`, `status`, `installation`, `installation_repositories`.

- [ ] **Step 4: The webhook tunnel, the webhook path only**

As in part 4 (`docs/superpowers/plans/2026-10-16-github-checks.md`, Task 12, Step 3): an ngrok traffic policy in the scratchpad that answers 404 to everything but `POST /api/github/webhook`, `ngrok http https://127.0.0.1:443 --host-header=127.0.0.1.nip.io --traffic-policy-file <scratchpad>/webhook-only.yml` (with the static domain: `--url <domain>`), and the checks from outside: `GET /api/projects` through the tunnel prints `404`, an unsigned `POST /api/github/webhook` prints `401`.

- [ ] **Step 5: Run 1, link from the dialog**

As Ana (signed in through the local provider as `paezao@…`, the maintainer's test person), open New project, type the slug `hello-linked` and a name, then **From GitHub**: **Continue with GitHub**. The popup goes to GitHub; the maintainer clicks **Authorize**. Expect: the popup closes (or says "GitHub linked … You can close this window."), the dialog still holds `hello-linked` and the name, and the picker shows the account switcher (`paezao`), only paezao's repositories, **Can't find a repository?** and **Add GitHub account or organisation**. Note whether `window.opener` survived GitHub's pages (the popup closed by itself) or the dashboard found the link by asking the server: either is right; say which in the PR. Create the project from `paezao/shpyrd-e2e-hello`: it builds and runs, and its Source block says "Deploys on push through the GitHub App. Connected by paezao." The person menu shows **GitHub: paezao**. `kubectl -n shpyrd-system logs deploy/shpyrd-server | grep -cE 'ghu_|ghr_|ghs_'` prints `0`.

- [ ] **Step 6: Run 2, someone who cannot read it**

Make a second person, `tester@…`, a member (`shpyrd` CLI as the admin, or the dashboard's People). As them:
- Without a link: New project, From GitHub shows **Continue with GitHub**; with an API token they make, `shpyrd projects create denied` then `shpyrd deploy --git https://github.com/paezao/shpyrd-e2e-hello --project denied --no-wait` (both with `SHPYRD_TOKEN=<their token>`) answers "link your GitHub account first: run shpyrd github link".
- If the maintainer has a second GitHub account without access: link it (the maintainer authorizes in the popup). Expect the picker to show no account ("Your GitHub account cannot read any of this workspace's GitHub accounts…") and the same deploy to answer "You can't read paezao/shpyrd-e2e-hello on GitHub". Otherwise say in the PR that this half was covered by `TestThePickerListsOnlyWhatThePersonCanRead` and `TestCreatingAProjectFromAPrivateRepositoryNeedsSomeoneWhoCanReadIt` only.

- [ ] **Step 7: Run 3, the CLI and an agent**

As Ana: `shpyrd github link` says "Your GitHub account is linked: paezao."; `shpyrd whoami` prints `GitHub: paezao`. Ana makes an API token named `agent` on the dashboard's **Workspace → API tokens** page, with no role of its own (it acts with hers). With it, and no browser: `SHPYRD_TOKEN=<token> shpyrd projects create hello-agent` then `SHPYRD_TOKEN=<token> shpyrd deploy --git https://github.com/paezao/shpyrd-e2e-hello --project hello-agent --no-wait` points it at the private repository and deploys it, and `kubectl get app -A -o jsonpath='{range .items[*]}{.metadata.annotations.shpyrd\.io/github-linked-by}{"\n"}{end}'` shows `paezao` for it. Keep the transcripts for the PR.

- [ ] **Step 8: Run 4, a missed push and the sweep**

Stop ngrok. Push a commit "sweep test" to `paezao/shpyrd-e2e-hello` from a clone in the scratchpad (allowed). Expect: GitHub's delivery fails (the maintainer may show the App's **Advanced → Recent Deliveries**: a failed `push`), nothing deploys, and the server lists nothing on a timer: `kubectl -n shpyrd-system logs deploy/shpyrd-server --since=5m | grep -c 'could not read the branch'` prints `0` and the project's `status.source.checkedAt` does not move. Start ngrok again on the same domain. Within 5 minutes the log says `asked GitHub to deliver again what did not reach shpyrd` with `"deliveries": 1` (or more), the Recent Deliveries show a redelivery answered `202`, and the project deploys `Push <sha> by …: sweep test`. Time it, for the PR.

- [ ] **Step 9: Run 5, another host, polled then not**

Make a project from a public repository on another host that builds, which the maintainer names (a public copy of the test app on GitLab or Codeberg does): its Source block says "Checked every 5 min. Set up the webhook to deploy at once and stop the checks." The server lists it once per 5 minutes. Then deliver its webhook as the host would, from the terminal: `shpyrd git-hooks show --project <slug>` gives the URL and secret; send a GitLab-style push to it with `curl -X POST -H 'X-Gitlab-Event: Push Hook' -H "X-Gitlab-Token: <secret>" -H 'Content-Type: application/json' --data '{"ref":"refs/heads/main","before":"<the running commit>","after":"<the running commit>","commits":[]}' <url>`. Expect the Source block to say "Deploys on push through its webhook (last delivery just now)." and the server to list it no more over the next 15 minutes (`status.source.checkedAt` stays).

- [ ] **Step 10: Run 6, unlink, and the project keeps deploying**

As Ana, the person menu, **Unlink GitHub**: the menu shows **Link GitHub**; the maintainer sees the token gone from GitHub (Settings → Applications → Authorized GitHub Apps shows the App's last use; the revoke is in the server's log as no error). Push a commit "after unlink" (allowed): `hello-linked` deploys it.

- [ ] **Step 11: Screenshots**

With the browser tools, into the scratchpad (never the repository), light theme and one in dark:
- shpyrd: New project with **Continue with GitHub**; the popup on GitHub's authorize page (the maintainer's screen, or theirs to take); the picker with the account switcher, the list, **Can't find a repository?** and **Add GitHub account or organisation**; the second person's empty picker (if Run 2 had a second account); the person menu with **GitHub: paezao** and after unlinking **Link GitHub**; the Source block of `hello-linked` ("Deploys on push through the GitHub App. Connected by paezao."), of the other host's project while polled, and after its webhook.
- GitHub (the maintainer's): the App's settings (user authorization, token expiration); Recent Deliveries with the failed push and its redelivery.
- As text: `shpyrd whoami`, `shpyrd github link`, the agent's deploy, the refused deploy, the sweep's log line.

- [ ] **Step 12: RFC-0054's history**

Under `## Implementation History` in `rfcs/0054-github-app.md`:

```markdown
- 2026-10-17: GitHub access by person, and webhooks before polling: each person links their own GitHub account through the App's user authorization (a popup from New project, `shpyrd github link`), the refresh token sealed with AES-256-GCM under the Secret shpyrd-token-key; the picker lists only what the person can read, and a project is made from or moved to a private repository only by someone who can read it (an API token as its maker; public repositories need no link). A repository the App covers is never listed on a timer: a leader-only sweep redelivers failed deliveries every 5 minutes; other hosts are listed once per repository every 5 minutes under a cluster-wide cap until their own webhook delivers; CI's fallback recheck is 2 minutes. Verified on a kind cluster against the test App. See docs/superpowers/specs/2026-10-17-github-user-link-and-polling-design.md.
```

```bash
git add rfcs/0054-github-app.md
git commit -m "docs(rfc): RFC-0054, GitHub access by person and webhooks before polling, verified on a kind cluster

Co-Authored-By: <your model name> <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

- [ ] **Step 13: Push, and the PR**

When the maintainer says so: `git push origin feat/private-git-repositories`. PR #223's description gains a section "GitHub access by person, and webhooks before polling": what changed, the rulings at the top of this plan, what an existing App needs (expiring user tokens; the events), the new Secret, the new variables (`SHPYRD_GIT_POLL_INTERVAL` default 5m, `SHPYRD_GIT_POLL_RATE`), how it was tested (the tunnel's policy, the sweep's timing), the screenshots and the transcripts in code blocks. `gh` cannot attach images: give the maintainer the image paths to drop in, or upload them through the browser tools on the PR page. A PR body written by the agent ends with:

```
🤖 Generated with [Claude Code](https://claude.com/claude-code)

https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb
```
