# Private repositories, PR 2: the source watcher (RFC-0018) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every push to a project's branch deploys, within seconds through a
generic webhook (GitHub, GitLab, Gitea) or within minutes by polling, for
buildpack and Dockerfile builds alike; `[skip deploy]` and an auto-deploy
switch say when not to; the dashboard shows which commit runs.

**Architecture:** A new `SourceReconciler` watches Apps whose git source
follows a branch or tag. It reads the branch's commit with a small built-in
ls-remote (`pkg/gitauth/lsremote.go`, smart HTTP or SSH, no git binary) or
takes it from a push the webhook left on the App, and pins it in two
annotations (`shpyrd.io/git-commit`, `shpyrd.io/git-commit-ref`) with the
release note and attribution a deploy uses. The `AppReconciler` builds the
pinned SHA, never the branch: kpack gets the SHA as its revision, and the
Dockerfile build key and fetch use it. What the watcher saw travels in a
`shpyrd.io/git-source` annotation that the AppReconciler, the only writer of
status, copies into `status.source`. The API adds the webhook route, the
per-project hook Secret, the auto-deploy setting and the deploy changes; the
CLI adds `shpyrd git-hooks` and `--auto-deploy`/`--no-auto-deploy`; the
dashboard shows the source in the project's Summary card.

**Tech Stack:** Go, controller-runtime v0.25.1, gin, cobra,
`golang.org/x/crypto/ssh` v0.55.0 (already in `go.mod`), kpack, rootless
BuildKit; Next 16, React 19, TanStack Query, `@shpyrd/ui`, Vitest.

**Spec:** `docs/superpowers/specs/2026-10-16-private-git-repositories-design.md`
(section 2) and
`docs/superpowers/specs/2026-10-16-private-git-watcher-decisions.md`, which
is binding and wins where the two differ. This plan is delivery item 2 of 4.
Branch: `feat/private-git-watcher`, stacked on PR 1 (#223).

## Global Constraints

- The brand is written `shpyrd`, lowercase, everywhere, including docs, CLI
  output and dashboard copy.
- Where the spec and the decisions file differ, the decisions file wins.
- No git binary and no go-git in the server: a branch is read by
  `gitauth.LsRemote`, protocol v0/v1 only (no `Git-Protocol: version=2`
  header), 20 s per call, at most 4 MiB read.
- Out of scope, not even as CRD fields: `WatchPaths`, `WaitForChecks`, the
  GitHub App webhook, checks and Deployments (PR 3 and PR 4).
- The AppReconciler is the only writer of `status`. Every other write to the
  App's annotations is a merge patch, never a full update; a one-shot
  annotation (`git-push`, `release-note`, `rollback-to`, `deployed-*`) is
  removed only while it still holds the value that was read (a JSON patch
  `test` + `remove`).
- Credential values and the hook secret never appear in a log line, an
  audit detail or an error. The hook secret is returned only to
  `authz.ProjectConfig`, by `GET /git-hooks` and `POST /git-hooks/rotate`.
- The webhook: the URL token compared in constant time; a 1 MiB body cap; a
  per-IP limiter of its own; 401 for a bad token or signature; 202 for an
  event that is not a push, a push not for the branch, and an unknown
  project; 200 `pong` for a GitHub ping; 400 for an unreadable push.
- Polling: `SHPYRD_GIT_POLL_INTERVAL`, default `2m`; `1h` once a webhook
  arrived in the last 24 h; ±10 % jitter.
- Messages are plain sentences that name the fix, in the style of
  `internal/controller/build_failure.go`; a refused listing names the same
  `shpyrd git-auth` commands PR 1's build failure does.
- The dashboard uses only `@shpyrd/ui` components and tokens, calls only
  through `api`, and the Mock answers every new call (`apps/AGENTS.md`).
  `npm run lint`, `npm run typecheck` and `npm run test` pass in
  `apps/workspace`.
- Every commit ends with:
  ```
  Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb
  ```
- The PR carries screenshots of the feature in use (Task 10).

## Deviations, deliberate, and where the code disagreed with the decisions

1. **The phase while no commit is pinned is `Building`**, not "stays as it
   is". The only times a watched app has no pin are its first deploy, a
   deploy by a person (the API removes the pin), a branch change, and the
   upgrade to this version, and all four are deploys. The message is the
   decided one, "Reading the branch's latest commit." When the watcher
   recorded an error instead, the build counts as failed with that error,
   so the reason shows where a build failure does.
2. **A skipped commit stays skipped when the next poll finds it** at the
   head of the branch. The decisions say a `[skip deploy]` push is not
   pinned; without this the poll an hour later would deploy it.
3. **The watcher reads the branch only when a check is due.** It runs on
   every App event, including its own patches and every status write of the
   AppReconciler. A check is due when nothing is pinned, when the interval
   (or, after failures, a backoff of 30 s, 1 m, 2 m... up to the interval)
   has passed since the last one, or when a deploy was requested after it.
   The `git-source` record carries three fields beyond the decided ones for
   this: `ref` (the revision it is about), `delivery` (the last delivery id
   applied, for the duplicate check) and `failures`.
4. **The API also drops pushes that are not for the branch** (another
   branch, a deleted branch, an app pinned to a commit or not on git)
   before writing anything: each write is an App event for both
   controllers. The watcher still drops a push for another branch, in case
   the branch changed between the two.
5. **A git deploy that says nothing about auto-deploy keeps the project's
   setting.** `DeployRequest.Git` is the CRD's `GitSource`, and `deployApp`
   replaces `spec.source` whole: without this, `shpyrd deploy --git` after
   switching auto-deploy off in the dashboard switches it back on.
6. **`configvars.DeployToken` ignores `autoDeploy` and includes the pin.**
   The token hashes `spec.source`; switching auto-deploy would otherwise
   apply saved config edits and release them. A new pin is a deploy and
   does apply them. The pin joins the hash only when there is one, so the
   tokens of other apps do not change.
7. **A git URL shpyrd cannot parse (`git://...`) is not watched** and builds
   its revision as given, as PR 1 left it.
8. **Two AppReconciler writes become conditional**: `clearReleaseNote`, and
   the removal of `deployed-by`/`-subject`/`-client` after a release is
   audited, use the `test` + `remove` patch. Both used a merge patch built
   from a copy read before the status patch, and would delete a note or an
   attribution the watcher wrote in between.
9. **Attribution**: `deployed-by` is `git:<author>` (or `git` when the
   author is unknown, after a poll), and the existing
   `deployed-client` annotation carries `push` or `poll`. That is the
   decisions' "`git:<author> via push|poll`" on the two annotations a deploy
   already has.
10. **A `SourceReady` condition** is added (True "Pinned", Unknown
    "Reading", False "Unreadable"). PR 1's plan deferred it to this PR; it
    costs a few lines.
11. **No watch on credential Secrets in the watcher.** After access is
    given, the next check (within the backoff, at most the interval) finds
    it. The AppReconciler keeps its own watch.
12. **Code facts the decisions assumed otherwise:**
    - The Summary card is one `InfoTable`; the git rows are two of its
      items. The source block is a component of its own rendered at the top
      of the Summary card's content, above that table.
    - The Mock is `src/api/mock.ts` with JSON under `apps/workspace/mock/`,
      not a mock folder; changing `projects.json` needs the Mock's `shape`
      bumped, or browsers keep the old copy.
    - `@shpyrd/ui` has `Dialog` and `Switch` but no copy button: copying is
      a ghost icon `Button` and the clipboard, as `screens/project/domains.tsx`
      does.
    - `Overview` receives no `perms`; `screens/project/index.tsx` passes
      them now.
    - Controllers are registered in `newManager` (`pkg/server/server.go`),
      which `runControllers` calls; the watcher is registered there.
    - Controller tests in six files build git apps on a branch with no
      pin. They get one (Task 5 lists them), or they stop building.

## Review Focus

1. **A webhook racing the poller, and an App event storm.** The watcher
   reconciles on every App event, and a listing can lag a push by a moment.
   Expected: a push wins over a stale listing, and status writes never turn
   into listings. Tests: `TestSourceWebhookBeatsAStalePoll` and
   `TestSourcePollsOnlyWhenDue` (Task 4).
2. **A `[skip deploy]` commit at the head of the branch.** Expected: it is
   not deployed by the push, nor by any later poll that finds it there,
   until a newer commit comes. Test: `TestSourceKeepsASkippedCommitSkipped`
   (Task 4).
3. **Two writers of App annotations.** The watcher writes a note and a pin
   while the AppReconciler clears the previous note, and the webhook writes
   a newer push while the watcher consumes the last one. Expected: nothing
   newer is lost. Tests: `TestRemoveAnnotationIfUnchangedKeepsANewerValue`
   (Task 4) and `TestClearReleaseNoteKeepsANewerNote` (Task 5).
4. **A revision that is a tag.** An annotated tag's ref names a tag object,
   not a commit. Expected: the commit it points at is pinned and built, and
   a push to the tag is not taken for the branch. Tests:
   `TestResolveBranchPrefersTheBranchThenThePeeledTag` (Task 2) and
   `TestSourcePinsTheCommitATagPointsAt` (Task 4).
5. **A big monorepo's ref advertisement.** GitHub advertises every pull
   request's refs; past 4 MiB the listing is cut. Expected: a branch is
   still found (branches sort before `refs/pull/`), and a tag that is not
   says why in words. Tests: `TestLsRemoteFindsTheBranchPastTheCap`
   (Task 2) and `TestSourcePinsFromATruncatedListing` (Task 4).

Also pinned by tests, outside the five: a force-pushed or deleted branch
(`TestSourceFollowsAForcePushAndKeepsThePinWhenTheBranchGoes`, Task 4), and
auto-deploy surviving a deploy
(`TestAGitDeployKeepsAutoDeployAndReadsTheBranchAgain`, Task 6).

---

## File structure

| File | Responsibility |
|---|---|
| `api/v1alpha1/app_types.go` (modify: annotations ~line 114, condition ~136, `GitSource` 302-309, `AppStatus` ~548) | `AutoDeploy`, `SourceStatus`, `status.source`, the pin and record annotation names, `SourceReady` |
| `api/v1alpha1/git_source.go` (new), `git_source_test.go` (new) | `IsCommit`, `GitRevision`, `WatchesGit`, `PinnedCommit`, `BuildRevision`, `AutoDeploys` |
| `api/v1alpha1/zz_generated.deepcopy.go`, `deploy/components/shpyrd/base/crds/shpyrd.io_apps.yaml` (generated) | `make generate` |
| `pkg/configvars/pending.go` (modify, line 22-26), `pending_test.go` (new) | The deploy token ignores auto-deploy, follows the pin |
| `pkg/gitauth/lsremote.go` (new), `lsremote_test.go` (new) | ls-remote over smart HTTP and SSH; `ResolveBranch` |
| `pkg/gitauth/webhook.go` (new), `webhook_test.go` (new) | Verifying a delivery; reading a push; `[skip deploy]`; the `Push` and `SourceRecord` the annotations hold |
| `internal/controller/gitcreds.go` (modify, 24-60, 124-133) | `lookupGitBuild`, shared by both reconcilers |
| `internal/controller/annotations.go` (new) | `removeAnnotationIfUnchanged` |
| `internal/controller/source_controller.go` (new), `source_controller_test.go` (new) | `SourceReconciler`: decide, pin, poll timing, errors in words |
| `pkg/server/server.go` (modify, after line 557) | Register the watcher |
| `internal/controller/source_build.go` (new), `source_build_test.go` (new) | `waitingForCommit`, `sourceStatus`, `setSourceCondition` |
| `internal/controller/desired.go` (590-596), `dockerfile.go` (232-250, 518), `status.go` (192-194), `app_controller.go` (379-384, 466, 495, 637, 1128-1153) (modify) | Build the pin; status.source; conditional removals |
| Existing controller tests (modify) | Pin the git apps they build |
| `pkg/api/githooks.go` (new), `githooks_test.go` (new) | Webhook route, hook Secret routes |
| `pkg/api/server.go` (modify: fields ~280, init ~382, routes ~577 and ~799) | Wire them |
| `pkg/api/apps.go` (modify: `AppDetailStatus` ~183, `detail` ~240, `UpdateAppRequest` 543-553, `updateApp` 558-617, `deployApp` 703-709) | Auto-deploy setting, deploy changes, project view |
| `internal/cli/githooks.go` (new), `githooks_test.go` (new) | `shpyrd git-hooks show|rotate` |
| `internal/cli/gitauth.go` (modify, 189-215), `deploy.go` (modify, 26-36, 66, 196-199, 299-310), `root.go` (modify, line 131) | Shared signed-in request; deploy flags; register |
| `apps/workspace/src/api/types.ts`, `api.ts`, `backend.ts`, `mock.ts`, `mock/projects.json` (modify) | Types, calls, Mock, fixtures |
| `apps/workspace/src/screens/project/source.tsx` (new), `source.test.tsx` (new) | The Source block of the Summary card |
| `apps/workspace/src/screens/project/overview.tsx`, `index.tsx` (modify) | Render it; pass `perms` |
| `content/docs/deploying.mdx`, `content/docs/cli.mdx` (modify) | "Deploying on push"; `git-hooks`; the deploy flags |
| `rfcs/0018-repository-monitoring.md`, `rfcs/README.md` (modify) | Status and history |

---

### Task 1: The CRD: auto-deploy, the pin, `status.source`

**Files:**
- Modify: `api/v1alpha1/app_types.go` (the annotation `const` block, after `AnnotationRebuildAt` ~line 114; the condition `const` block ~136; `GitSource` 302-309; `AppStatus` before `Conditions` ~548; a new type after `DomainStatus`)
- Create: `api/v1alpha1/git_source.go`
- Modify: `pkg/configvars/pending.go:20-26`
- Generated: `api/v1alpha1/zz_generated.deepcopy.go`, `deploy/components/shpyrd/base/crds/shpyrd.io_apps.yaml`
- Test: `api/v1alpha1/git_source_test.go`, `pkg/configvars/pending_test.go`

**Interfaces:**
- Produces:
  - `GitSource.AutoDeploy *bool` (`json:"autoDeploy,omitempty"`)
  - `type SourceStatus struct { Commit, Latest string; LatestAt *metav1.Time; Message, Author, Via, Skipped string; LastWebhookAt, CheckedAt *metav1.Time; Error string }` with JSON names `commit, latest, latestAt, message, author, via, skipped, lastWebhookAt, checkedAt, error`
  - `AppStatus.Source *SourceStatus` (`json:"source,omitempty"`)
  - `const AnnotationGitCommit = "shpyrd.io/git-commit"`, `AnnotationGitCommitRef = "shpyrd.io/git-commit-ref"`, `AnnotationGitPush = "shpyrd.io/git-push"`, `AnnotationGitSource = "shpyrd.io/git-source"`
  - `const ConditionSourceReady = "SourceReady"`
  - `func IsCommit(rev string) bool`
  - `func (a *App) GitRevision() string` ("main" when unset, "" without git)
  - `func (a *App) WatchesGit() bool`
  - `func (a *App) PinnedCommit() string`
  - `func (a *App) BuildRevision() string`
  - `func (a *App) AutoDeploys() bool`
  - `configvars.DeployToken` unchanged in signature

- [ ] **Step 1: Write the failing tests**

`api/v1alpha1/git_source_test.go`:

```go
package v1alpha1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const testSHA = "0123456789abcdef0123456789abcdef01234567"

func gitApp(rev string, ann map[string]string) *App {
	return &App{
		ObjectMeta: metav1.ObjectMeta{Annotations: ann},
		Spec:       AppSpec{Source: &Source{Git: &GitSource{URL: "https://github.com/acme/shop", Revision: rev}}},
	}
}

func TestIsCommit(t *testing.T) {
	for rev, want := range map[string]bool{
		"main": false, "v1.2": false, "release/2026": false, "": false,
		"a1b2c3d": true, "A1B2C3D": true, testSHA: true,
		"a1b2c3": false, "a1b2c3dx": false,
	} {
		if got := IsCommit(rev); got != want {
			t.Errorf("IsCommit(%q) = %v, want %v", rev, got, want)
		}
	}
}

func TestPinnedCommitHoldsOnlyForItsBranch(t *testing.T) {
	app := gitApp("", map[string]string{AnnotationGitCommit: testSHA, AnnotationGitCommitRef: "main"})
	if app.GitRevision() != "main" || !app.WatchesGit() || app.PinnedCommit() != testSHA || app.BuildRevision() != testSHA {
		t.Fatalf("revision %q watched %v pin %q build %q", app.GitRevision(), app.WatchesGit(), app.PinnedCommit(), app.BuildRevision())
	}
	app.Spec.Source.Git.Revision = "release"
	if app.PinnedCommit() != "" || app.BuildRevision() != "release" {
		t.Errorf("a pin made for main must not build release: pin %q build %q", app.PinnedCommit(), app.BuildRevision())
	}
	bad := gitApp("main", map[string]string{AnnotationGitCommit: "not-a-sha", AnnotationGitCommitRef: "main"})
	if bad.PinnedCommit() != "" {
		t.Errorf("a pin that is not a full SHA is no pin: %q", bad.PinnedCommit())
	}
}

func TestACommitRevisionIsBuiltAsGiven(t *testing.T) {
	app := gitApp("a1b2c3d", map[string]string{AnnotationGitCommit: testSHA, AnnotationGitCommitRef: "a1b2c3d"})
	if app.WatchesGit() || app.PinnedCommit() != "" || app.BuildRevision() != "a1b2c3d" {
		t.Errorf("watched %v pin %q build %q", app.WatchesGit(), app.PinnedCommit(), app.BuildRevision())
	}
	blob := &App{Spec: AppSpec{Source: &Source{Blob: &BlobSource{URL: "http://x/a.tgz"}}}}
	if blob.WatchesGit() || blob.GitRevision() != "" || blob.AutoDeploys() {
		t.Error("an archive has no branch to watch")
	}
}

func TestAutoDeploysUnlessSwitchedOff(t *testing.T) {
	app := gitApp("main", nil)
	if !app.AutoDeploys() {
		t.Error("unset means on")
	}
	off := false
	app.Spec.Source.Git.AutoDeploy = &off
	if app.AutoDeploys() {
		t.Error("false means off")
	}
}
```

`pkg/configvars/pending_test.go`:

```go
package configvars

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

func TestDeployTokenIgnoresAutoDeployAndFollowsThePin(t *testing.T) {
	app := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}},
		Spec:       shpyrdv1.AppSpec{Source: &shpyrdv1.Source{Git: &shpyrdv1.GitSource{URL: "https://github.com/acme/shop", Revision: "main"}}},
	}
	before := DeployToken(app)
	off := false
	app.Spec.Source.Git.AutoDeploy = &off
	if DeployToken(app) != before {
		t.Error("switching auto-deploy is a setting, not a deploy")
	}
	app.Annotations[shpyrdv1.AnnotationGitCommit] = "0123456789abcdef0123456789abcdef01234567"
	app.Annotations[shpyrdv1.AnnotationGitCommitRef] = "main"
	if DeployToken(app) == before {
		t.Error("a new pinned commit is a deploy")
	}
}

func TestDeployTokenOfAnAppWithoutAPinIsUnchanged(t *testing.T) {
	for _, app := range []*shpyrdv1.App{
		{Spec: shpyrdv1.AppSpec{Image: "ghcr.io/acme/shop:1"}},
		{Spec: shpyrdv1.AppSpec{Source: &shpyrdv1.Source{Git: &shpyrdv1.GitSource{URL: "https://github.com/acme/shop", Revision: "a1b2c3d"}}}},
	} {
		// The formula before RFC-0018: a token that moved would apply
		// every saved config edit on the upgrade.
		b, _ := json.Marshal([]any{app.Spec.Image, app.Spec.Source, app.Spec.Build, "", "", ""})
		h := sha256.Sum256(b)
		if got := DeployToken(app); got != hex.EncodeToString(h[:]) {
			t.Errorf("%+v: the token of an app with no pin changed", app.Spec)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./api/v1alpha1/ ./pkg/configvars/`
Expected: FAIL to compile: `undefined: IsCommit`, `AnnotationGitCommit`,
`unknown field AutoDeploy`.

- [ ] **Step 3: Write the implementation**

In `api/v1alpha1/app_types.go`, in the annotation `const` block, after
`AnnotationRebuildAt = "shpyrd.io/rebuild-at"`:

```go
	// AnnotationGitCommit and AnnotationGitCommitRef pin the commit a git
	// branch builds (RFC-0018): the full SHA, and the revision it was read
	// from. The pin holds only while the ref is spec.source.git's
	// revision. The source watcher writes them; a git deploy removes them.
	AnnotationGitCommit    = "shpyrd.io/git-commit"
	AnnotationGitCommitRef = "shpyrd.io/git-commit-ref"
	// AnnotationGitPush is a push the webhook received, as JSON, for the
	// source watcher to consume.
	AnnotationGitPush = "shpyrd.io/git-push"
	// AnnotationGitSource is what the source watcher last saw of the
	// branch, as JSON; the App controller copies it into status.source.
	AnnotationGitSource = "shpyrd.io/git-source"
```

In the condition `const` block:

```go
const (
	ConditionReady = "Ready"
	ConditionBuilt = "Built"
	// ConditionSourceReady says whether the branch an App follows could be
	// read and a commit is pinned for it (RFC-0018).
	ConditionSourceReady = "SourceReady"
)
```

Replace `GitSource`:

```go
// GitSource builds from a Git repository. A branch or tag is watched: the
// source watcher pins its latest commit and builds use that commit
// (RFC-0018). A commit is built as given.
type GitSource struct {
	URL string `json:"url"`
	// Revision is a branch, tag or commit. Defaults to "main".
	// +optional
	Revision string `json:"revision,omitempty"`
	// AutoDeploy deploys every new commit of the branch; false records
	// them without deploying. Unset means true.
	// +optional
	AutoDeploy *bool `json:"autoDeploy,omitempty"`
}
```

In `AppStatus`, before `Conditions`:

```go
	// Source is what the source watcher knows of the branch a git source
	// follows, and the commit pinned for it (RFC-0018).
	// +optional
	Source *SourceStatus `json:"source,omitempty"`
```

After `DomainStatus`:

```go
// SourceStatus is what the source watcher knows of a git branch.
type SourceStatus struct {
	// Commit is the commit pinned for the branch: what is built and runs.
	// +optional
	Commit string `json:"commit,omitempty"`
	// Latest is the newest commit seen on the branch.
	// +optional
	Latest string `json:"latest,omitempty"`
	// +optional
	LatestAt *metav1.Time `json:"latestAt,omitempty"`
	// Message and Author describe Latest when a push said them.
	// +optional
	Message string `json:"message,omitempty"`
	// +optional
	Author string `json:"author,omitempty"`
	// Via is how Latest was learnt: push, poll or deploy.
	// +optional
	Via string `json:"via,omitempty"`
	// Skipped is a commit whose message asked not to deploy it.
	// +optional
	Skipped string `json:"skipped,omitempty"`
	// +optional
	LastWebhookAt *metav1.Time `json:"lastWebhookAt,omitempty"`
	// +optional
	CheckedAt *metav1.Time `json:"checkedAt,omitempty"`
	// Error says why the branch could not be read, in words.
	// +optional
	Error string `json:"error,omitempty"`
}
```

`api/v1alpha1/git_source.go`:

```go
package v1alpha1

import "regexp"

// The source watcher (RFC-0018): a git source on a branch or tag is
// watched and builds the commit pinned for it; a commit is built as given.

var (
	commitRevision = regexp.MustCompile(`(?i)^[0-9a-f]{7,40}$`)
	fullCommit     = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)
)

// IsCommit says a revision names a commit: 7 to 40 hex digits.
func IsCommit(rev string) bool { return commitRevision.MatchString(rev) }

// GitRevision is the revision the git source follows: "main" when unset,
// "" without a git source.
func (a *App) GitRevision() string {
	if a.Spec.Source == nil || a.Spec.Source.Git == nil {
		return ""
	}
	if a.Spec.Source.Git.Revision == "" {
		return "main"
	}
	return a.Spec.Source.Git.Revision
}

// WatchesGit says the App follows a git branch or tag, and so builds the
// commit pinned for it.
func (a *App) WatchesGit() bool {
	rev := a.GitRevision()
	return rev != "" && !IsCommit(rev)
}

// PinnedCommit is the commit pinned for the revision the App follows; ""
// when none is, or the pin was made for another revision.
func (a *App) PinnedCommit() string {
	if !a.WatchesGit() || a.Annotations[AnnotationGitCommitRef] != a.GitRevision() {
		return ""
	}
	if sha := a.Annotations[AnnotationGitCommit]; fullCommit.MatchString(sha) {
		return sha
	}
	return ""
}

// BuildRevision is what a build of the git source checks out: the pinned
// commit, else the revision as given.
func (a *App) BuildRevision() string {
	if sha := a.PinnedCommit(); sha != "" {
		return sha
	}
	return a.GitRevision()
}

// AutoDeploys says a new commit of the branch deploys: true unless
// spec.source.git.autoDeploy is false.
func (a *App) AutoDeploys() bool {
	if a.Spec.Source == nil || a.Spec.Source.Git == nil {
		return false
	}
	return a.Spec.Source.Git.AutoDeploy == nil || *a.Spec.Source.Git.AutoDeploy
}
```

`pkg/configvars/pending.go`, replace `DeployToken`:

```go
// DeployToken changes only for a deployment request, not a resize, sleep,
// health update or saved config edit. Include source/image for direct CR
// users. Switching auto-deploy is a setting and leaves it alone; a new
// commit pinned for the branch is a deploy and changes it (RFC-0018).
func DeployToken(app *shpyrdv1.App) string {
	source := app.Spec.Source
	if source != nil && source.Git != nil && source.Git.AutoDeploy != nil {
		s, g := *source, *source.Git
		g.AutoDeploy = nil
		s.Git = &g
		source = &s
	}
	parts := []any{app.Spec.Image, source, app.Spec.Build, app.Annotations[AnnotationDeployRequested], app.Annotations[shpyrdv1.AnnotationRestartedAt], app.Annotations[shpyrdv1.AnnotationRebuildAt]}
	if pin := app.PinnedCommit(); pin != "" {
		parts = append(parts, pin)
	}
	b, _ := json.Marshal(parts)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
```

Then regenerate:

Run: `make generate`
Expected: `zz_generated.deepcopy.go` gains `DeepCopy` for `SourceStatus`
and copies `AutoDeploy` and `Source`; `grep -n "autoDeploy\|lastWebhookAt" deploy/components/shpyrd/base/crds/shpyrd.io_apps.yaml`
prints both.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go build ./... && go test ./api/v1alpha1/ ./pkg/configvars/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add api/v1alpha1 pkg/configvars deploy/components/shpyrd/base/crds/shpyrd.io_apps.yaml
git commit -m "feat(api): GitSource.autoDeploy, the pinned commit and status.source (RFC-0018)

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 2: `pkg/gitauth`: a small ls-remote over smart HTTP and SSH

**Files:**
- Create: `pkg/gitauth/lsremote.go`
- Test: `pkg/gitauth/lsremote_test.go`

**Interfaces:**
- Consumes (PR 1): `gitauth.Parse(raw string) (Repo, error)`, `Repo{Scheme, Host, Port, Path}`, `Repo.HTTPS()`, `Build{Kind, CloneURL, Host string; Secret *corev1.Secret}`, `KindToken`, `KindDeployKey`, `KeyKnownHosts`; the token Secret's data is `corev1.BasicAuthUsernameKey`/`BasicAuthPasswordKey`, the deploy key's `corev1.SSHAuthPrivateKey`/`KeyKnownHosts`.
- Produces:
  - `func LsRemote(ctx context.Context, cloneURL string, b Build) (map[string]string, error)`: ref name to SHA, only `refs/heads/*` and `refs/tags/*` (peeled `^{}` entries included)
  - `func ResolveBranch(refs map[string]string, revision string) (string, bool)`
  - `var ErrNoAccess, ErrHostKeyChanged, ErrTooManyRefs error` (with `ErrTooManyRefs`, `LsRemote` also returns the refs read before the cap)
  - `const MaxAdvertisement = 4 << 20`
  - `func firstNonEmpty(vals ...string) string`, `func isHexSHA(s string) bool` (package-private; Task 3 uses them)

- [ ] **Step 1: Write the failing tests**

`pkg/gitauth/lsremote_test.go`:

```go
package gitauth

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	corev1 "k8s.io/api/core/v1"
)

const (
	mainSHA = "0123456789abcdef0123456789abcdef01234567"
	tagObj  = "f00df00df00df00df00df00df00df00df00df00d"
	tagSHA  = "89abcdef0123456789abcdef0123456789abcdef"
)

// pkt is one pkt-line: four hex digits of length, then the data.
func pkt(s string) string { return fmt.Sprintf("%04x%s", len(s)+4, s) }

// refLines are the refs a server advertises, the first with capabilities,
// and the flush that ends them.
func refLines(refs ...string) string {
	var b strings.Builder
	for i, r := range refs {
		if i == 0 {
			r += "\x00multi_ack thin-pack side-band-64k ofs-delta agent=git/2.47.0"
		}
		b.WriteString(pkt(r + "\n"))
	}
	b.WriteString("0000")
	return b.String()
}

var sampleRefs = []string{
	mainSHA + " HEAD",
	mainSHA + " refs/heads/main",
	tagSHA + " refs/heads/release",
	"1111111111111111111111111111111111111111 refs/pull/7/head",
	tagObj + " refs/tags/v1.2",
	tagSHA + " refs/tags/v1.2^{}",
}

// smartHTTP serves an advertisement the way a git host does.
func smartHTTP(t *testing.T, check func(*http.Request), body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if check != nil {
			check(r)
		}
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestLsRemoteHTTPWithAToken(t *testing.T) {
	srv := smartHTTP(t, func(r *http.Request) {
		if r.URL.Path != "/acme/shop.git/info/refs" || r.URL.Query().Get("service") != "git-upload-pack" {
			t.Errorf("asked %s", r.URL)
		}
		if r.Header.Get("Git-Protocol") != "" {
			t.Error("no protocol v2 header: the answer must be v0/v1")
		}
		if u, p, ok := r.BasicAuth(); !ok || u != "x-access-token" || p != "tok" {
			t.Errorf("basic auth %q %q %v", u, p, ok)
		}
	}, pkt("# service=git-upload-pack\n")+"0000"+refLines(sampleRefs...))
	b := Build{Kind: KindToken, Secret: &corev1.Secret{Data: map[string][]byte{
		corev1.BasicAuthUsernameKey: []byte("x-access-token"), corev1.BasicAuthPasswordKey: []byte("tok"),
	}}}
	refs, err := LsRemote(context.Background(), srv.URL+"/acme/shop", b)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"refs/heads/main": mainSHA, "refs/heads/release": tagSHA,
		"refs/tags/v1.2": tagObj, "refs/tags/v1.2^{}": tagSHA,
	}
	if len(refs) != len(want) {
		t.Fatalf("refs = %v", refs)
	}
	for k, v := range want {
		if refs[k] != v {
			t.Errorf("%s = %q, want %q", k, refs[k], v)
		}
	}
}

func TestLsRemoteHTTPRefused(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"401": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) },
		"403": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) },
		"404": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) },
		"sign-in page": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, "<html>Sign in</html>")
		},
	} {
		srv := httptest.NewServer(h)
		_, err := LsRemote(context.Background(), srv.URL+"/acme/shop", Build{})
		srv.Close()
		if !errors.Is(err, ErrNoAccess) {
			t.Errorf("%s: err = %v, want ErrNoAccess", name, err)
		}
	}
}

func TestLsRemoteRefusesWhatIsNotAnAdvertisement(t *testing.T) {
	for name, body := range map[string]string{
		"garbage":        "zzzz not git",
		"no service":     refLines(sampleRefs...),
		"bad sha":        pkt("# service=git-upload-pack\n") + "0000" + refLines("nothex refs/heads/main"),
		"cut short":      pkt("# service=git-upload-pack\n") + "0000" + pkt(mainSHA+" refs/heads/main\n"),
		"protocol v2":    pkt("# service=git-upload-pack\n") + "0000" + pkt("version 2\n") + "0000",
	} {
		srv := smartHTTP(t, nil, body)
		_, err := LsRemote(context.Background(), srv.URL+"/acme/shop", Build{})
		if err == nil || errors.Is(err, ErrNoAccess) {
			t.Errorf("%s: err = %v, want a malformed answer", name, err)
		}
	}
}

func TestLsRemoteOfAnEmptyRepository(t *testing.T) {
	srv := smartHTTP(t, nil, pkt("# service=git-upload-pack\n")+"0000"+refLines("0000000000000000000000000000000000000000 capabilities^{}"))
	refs, err := LsRemote(context.Background(), srv.URL+"/acme/empty", Build{})
	if err != nil || len(refs) != 0 {
		t.Errorf("refs %v err %v", refs, err)
	}
}

// Review Focus 5: GitHub advertises every pull request's refs. Branches
// sort before refs/pull/, so a branch is found in a listing cut at the cap.
func TestLsRemoteFindsTheBranchPastTheCap(t *testing.T) {
	lines := []string{mainSHA + " HEAD", mainSHA + " refs/heads/main"}
	for n := 0; len(lines)*60 < MaxAdvertisement+(1<<20); n++ {
		lines = append(lines, fmt.Sprintf("%s refs/pull/%d/head", tagSHA, n))
	}
	lines = append(lines, tagObj+" refs/tags/v9")
	srv := smartHTTP(t, nil, pkt("# service=git-upload-pack\n")+"0000"+refLines(lines...))
	refs, err := LsRemote(context.Background(), srv.URL+"/acme/monorepo", Build{})
	if !errors.Is(err, ErrTooManyRefs) {
		t.Fatalf("err = %v, want ErrTooManyRefs", err)
	}
	if sha, ok := ResolveBranch(refs, "main"); !ok || sha != mainSHA {
		t.Errorf("main = %q %v", sha, ok)
	}
	if _, ok := ResolveBranch(refs, "v9"); ok {
		t.Error("a tag past the cap cannot be found")
	}
}

// Review Focus 4: an annotated tag's ref names the tag object; the commit
// is its peeled ^{} entry.
func TestResolveBranchPrefersTheBranchThenThePeeledTag(t *testing.T) {
	refs := map[string]string{
		"refs/heads/main": mainSHA, "refs/tags/main": tagSHA,
		"refs/tags/v1.2": tagObj, "refs/tags/v1.2^{}": tagSHA,
		"refs/tags/light": mainSHA,
	}
	for rev, want := range map[string]string{
		"main": mainSHA, "v1.2": tagSHA, "light": mainSHA,
		"refs/tags/v1.2": tagSHA, "refs/heads/main": mainSHA,
	} {
		if got, ok := ResolveBranch(refs, rev); !ok || got != want {
			t.Errorf("%s = %q %v, want %q", rev, got, ok, want)
		}
	}
	if _, ok := ResolveBranch(refs, "gone"); ok {
		t.Error("gone must not resolve")
	}
}

// sshGitServer answers `git-upload-pack` with adv, as a git host does, to
// the one client key it knows. It returns its address, its host key and
// the commands it was asked to run.
func sshGitServer(t *testing.T, client ssh.PublicKey, adv string) (string, ssh.Signer, chan string) {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	host, _ := ssh.NewSignerFromKey(priv)
	cfg := &ssh.ServerConfig{PublicKeyCallback: func(_ ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
		if bytes.Equal(k.Marshal(), client.Marshal()) {
			return nil, nil
		}
		return nil, errors.New("unknown key")
	}}
	cfg.AddHostKey(host)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	cmds := make(chan string, 4)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go serveUploadPack(c, cfg, adv, cmds)
		}
	}()
	return ln.Addr().String(), host, cmds
}

func serveUploadPack(c net.Conn, cfg *ssh.ServerConfig, adv string, cmds chan<- string) {
	defer c.Close()
	_, chans, reqs, err := ssh.NewServerConn(c, cfg)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)
	for nc := range chans {
		if nc.ChannelType() != "session" {
			_ = nc.Reject(ssh.UnknownChannelType, "sessions only")
			continue
		}
		ch, chReqs, err := nc.Accept()
		if err != nil {
			return
		}
		go func() {
			for req := range chReqs {
				if req.Type != "exec" {
					_ = req.Reply(false, nil)
					continue
				}
				var p struct{ Command string }
				_ = ssh.Unmarshal(req.Payload, &p)
				cmds <- p.Command
				_ = req.Reply(true, nil)
				_, _ = io.WriteString(ch, adv)
				flush := make([]byte, 4)
				_, _ = io.ReadFull(ch, flush) // the client's "0000": it wants nothing
				_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
				_ = ch.Close()
			}
		}()
	}
}

func deployKeyBuild(t *testing.T, knownHosts string, key DeployKey) Build {
	t.Helper()
	return Build{Kind: KindDeployKey, Secret: &corev1.Secret{Data: map[string][]byte{
		corev1.SSHAuthPrivateKey: key.PrivatePEM, KeyKnownHosts: []byte(knownHosts),
	}}}
}

func TestLsRemoteSSHWithADeployKey(t *testing.T) {
	key, err := GenerateDeployKey("shpyrd shop")
	if err != nil {
		t.Fatal(err)
	}
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(key.Public))
	if err != nil {
		t.Fatal(err)
	}
	addr, host, cmds := sshGitServer(t, pub, refLines(sampleRefs...))
	line := knownhosts.Line([]string{knownhosts.Normalize(addr)}, host.PublicKey())
	refs, err := LsRemote(context.Background(), "ssh://git@"+addr+"/acme/shop.git", deployKeyBuild(t, line, key))
	if err != nil {
		t.Fatal(err)
	}
	if refs["refs/heads/main"] != mainSHA {
		t.Errorf("refs = %v", refs)
	}
	if cmd := <-cmds; cmd != "git-upload-pack '/acme/shop.git'" {
		t.Errorf("ran %q", cmd)
	}
}

func TestLsRemoteSSHRefusesAnotherHostKey(t *testing.T) {
	key, _ := GenerateDeployKey("shpyrd shop")
	pub, _, _, _, _ := ssh.ParseAuthorizedKey([]byte(key.Public))
	addr, _, _ := sshGitServer(t, pub, refLines(sampleRefs...))
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	otherSigner, _ := ssh.NewSignerFromKey(other)
	line := knownhosts.Line([]string{knownhosts.Normalize(addr)}, otherSigner.PublicKey())
	_, err := LsRemote(context.Background(), "ssh://git@"+addr+"/acme/shop.git", deployKeyBuild(t, line, key))
	if !errors.Is(err, ErrHostKeyChanged) {
		t.Errorf("err = %v, want ErrHostKeyChanged", err)
	}
}

func TestLsRemoteSSHWithAKeyTheHostDoesNotKnow(t *testing.T) {
	known, _ := GenerateDeployKey("known")
	pub, _, _, _, _ := ssh.ParseAuthorizedKey([]byte(known.Public))
	addr, host, _ := sshGitServer(t, pub, refLines(sampleRefs...))
	stranger, _ := GenerateDeployKey("stranger")
	line := knownhosts.Line([]string{knownhosts.Normalize(addr)}, host.PublicKey())
	_, err := LsRemote(context.Background(), "ssh://git@"+addr+"/acme/shop.git", deployKeyBuild(t, line, stranger))
	if !errors.Is(err, ErrNoAccess) {
		t.Errorf("err = %v, want ErrNoAccess", err)
	}
}

func TestSSHPathAndUser(t *testing.T) {
	for _, tc := range []struct{ url, path, user string }{
		{"git@github.com:acme/shop.git", "acme/shop.git", "git"},
		{"ssh://git@git.example.com:2222/team/app.git", "/team/app.git", "git"},
		{"ssh://gitea@git.example.com/team/app", "/team/app.git", "gitea"},
		{"forge@host.example:me/site", "me/site.git", "forge"},
	} {
		r, err := Parse(tc.url)
		if err != nil {
			t.Fatal(err)
		}
		if got := sshPath(tc.url, r); got != tc.path {
			t.Errorf("%s: path %q, want %q", tc.url, got, tc.path)
		}
		if got := sshUser(tc.url); got != tc.user {
			t.Errorf("%s: user %q, want %q", tc.url, got, tc.user)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./pkg/gitauth/ -run 'LsRemote|ResolveBranch|SSHPath'`
Expected: FAIL to compile: `undefined: LsRemote`, `ResolveBranch`,
`ErrNoAccess`, `sshPath`.

- [ ] **Step 3: Write the implementation**

`pkg/gitauth/lsremote.go`:

```go
package gitauth

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	corev1 "k8s.io/api/core/v1"
)

// Reading a branch's commit (RFC-0018). The server image has no git, so
// the ref advertisement a clone starts with is read here, over smart HTTP
// or SSH, in protocol v0/v1: no Git-Protocol header is sent.

var (
	// ErrNoAccess: the host refused the listing, or has no repository of
	// that name.
	ErrNoAccess = errors.New("the git host refused access to the repository, or it does not exist under that name")
	// ErrHostKeyChanged: the SSH host presented a key other than the one
	// recorded with the deploy key.
	ErrHostKeyChanged = errors.New("the git host's SSH host key does not match the one recorded with the deploy key")
	// ErrTooManyRefs: the advertisement passed MaxAdvertisement. The refs
	// read until then come with it; branches sort before pull requests'
	// and tags, so a branch is usually among them.
	ErrTooManyRefs = errors.New("the repository lists more refs than shpyrd reads")

	errMalformed = errors.New("the git host's answer is not a ref advertisement shpyrd can read")
)

const (
	// MaxAdvertisement caps what one listing reads.
	MaxAdvertisement = 4 << 20
	lsRemoteTimeout  = 20 * time.Second
)

var httpClient = &http.Client{Timeout: lsRemoteTimeout}

// LsRemote lists the branches and tags of the repository at cloneURL, as
// `git ls-remote` does, with the credential b carries: over SSH with a
// deploy key, else over HTTP(S) with the token or none. It returns ref
// name to commit, for refs/heads/ and refs/tags/ only.
func LsRemote(ctx context.Context, cloneURL string, b Build) (map[string]string, error) {
	r, err := Parse(cloneURL)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, lsRemoteTimeout)
	defer cancel()
	if r.Scheme == "ssh" && b.Kind == KindDeployKey && b.Secret != nil {
		return lsRemoteSSH(ctx, strings.TrimSpace(cloneURL), r, b.Secret)
	}
	// A token, or nothing (a public repository): HTTPS, whatever form the
	// URL was given in.
	return lsRemoteHTTP(ctx, r, b)
}

func lsRemoteHTTP(ctx context.Context, r Repo, b Build) (map[string]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.HTTPS()+"/info/refs?service=git-upload-pack", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "git/2.47.0 (shpyrd)")
	if b.Kind == KindToken && b.Secret != nil {
		req.SetBasicAuth(string(b.Secret.Data[corev1.BasicAuthUsernameKey]), string(b.Secret.Data[corev1.BasicAuthPasswordKey]))
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach %s: %w", r.Host, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden, resp.StatusCode == http.StatusNotFound:
		return nil, ErrNoAccess
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%s answered %s", r.Host, resp.Status)
	case strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html"):
		// A sign-in page the host redirected to.
		return nil, ErrNoAccess
	}
	return readAdvertisement(resp.Body, true)
}

func lsRemoteSSH(ctx context.Context, cloneURL string, r Repo, sec *corev1.Secret) (map[string]string, error) {
	signer, err := ssh.ParsePrivateKey(sec.Data[corev1.SSHAuthPrivateKey])
	if err != nil {
		return nil, fmt.Errorf("the deploy key does not parse: make a new one with shpyrd git-auth deploy-key: %w", err)
	}
	check, algorithms, err := knownHostsCheck(sec.Data[KeyKnownHosts])
	if err != nil {
		return nil, err
	}
	addr := net.JoinHostPort(r.Host, firstNonEmpty(r.Port, "22"))
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("could not reach %s: %w", addr, err)
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	cfg := &ssh.ClientConfig{
		User:              sshUser(cloneURL),
		Auth:              []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback:   check,
		HostKeyAlgorithms: algorithms,
	}
	cc, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		switch {
		case errors.Is(err, ErrHostKeyChanged) || strings.Contains(err.Error(), ErrHostKeyChanged.Error()):
			return nil, ErrHostKeyChanged
		case strings.Contains(err.Error(), "unable to authenticate"):
			return nil, ErrNoAccess
		}
		return nil, fmt.Errorf("could not reach %s: %w", addr, err)
	}
	client := ssh.NewClient(cc, chans, reqs)
	defer client.Close()
	sess, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("could not open a session on %s: %w", addr, err)
	}
	defer sess.Close()
	stdout, err := sess.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		return nil, err
	}
	if err := sess.Start("git-upload-pack " + quote(sshPath(cloneURL, r))); err != nil {
		return nil, fmt.Errorf("could not list the repository on %s: %w", addr, err)
	}
	refs, err := readAdvertisement(stdout, false)
	if err != nil && !errors.Is(err, ErrTooManyRefs) {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			// The host hung up without a ref: no such repository, or this
			// key may not read it.
			return nil, ErrNoAccess
		}
		return nil, err
	}
	// A flush: this client wants nothing, and the host hangs up.
	_, _ = io.WriteString(stdin, "0000")
	_ = stdin.Close()
	return refs, err
}

// knownHostsCheck accepts the host keys recorded with the deploy key, and
// asks the host for their algorithms so it presents one of them. The
// lines are parsed here: there is no known_hosts file.
func knownHostsCheck(data []byte) (ssh.HostKeyCallback, []string, error) {
	type entry struct {
		hosts []string
		key   ssh.PublicKey
	}
	var entries []entry
	var algorithms []string
	rest := data
	for {
		_, hosts, key, _, next, err := ssh.ParseKnownHosts(rest)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("the host key recorded with the deploy key does not parse: make a new deploy key with shpyrd git-auth deploy-key: %w", err)
		}
		entries = append(entries, entry{hosts, key})
		algorithms = append(algorithms, algorithmsFor(key.Type())...)
		rest = next
	}
	if len(entries) == 0 {
		return nil, nil, errors.New("no host key was recorded with the deploy key: make a new one with shpyrd git-auth deploy-key")
	}
	check := func(hostname string, _ net.Addr, key ssh.PublicKey) error {
		want := knownhosts.Normalize(hostname)
		for _, e := range entries {
			for _, h := range e.hosts {
				if knownhosts.Normalize(h) == want && bytes.Equal(e.key.Marshal(), key.Marshal()) {
					return nil
				}
			}
		}
		return ErrHostKeyChanged
	}
	return check, algorithms, nil
}

func algorithmsFor(keyType string) []string {
	if keyType == ssh.KeyAlgoRSA {
		return []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA}
	}
	return []string{keyType}
}

// sshPath is the repository's path as upload-pack wants it: org/repo.git
// for the scp form, /org/repo.git for an ssh:// URL.
func sshPath(cloneURL string, r Repo) string {
	if strings.Contains(cloneURL, "://") {
		return "/" + r.Path + ".git"
	}
	return r.Path + ".git"
}

// sshUser is the URL's user, git when it names none.
func sshUser(cloneURL string) string {
	if strings.Contains(cloneURL, "://") {
		if u, err := url.Parse(cloneURL); err == nil && u.User != nil && u.User.Username() != "" {
			return u.User.Username()
		}
		return "git"
	}
	if at := strings.Index(cloneURL, "@"); at > 0 {
		return cloneURL[:at]
	}
	return "git"
}

// quote single-quotes s for the remote shell. Parse already refused
// anything but [A-Za-z0-9._~-] and slashes in the path.
func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// readAdvertisement reads the refs a server advertises, up to the flush
// that ends them. Over smart HTTP they follow a "# service=" line and a
// flush.
func readAdvertisement(body io.Reader, smartHTTP bool) (map[string]string, error) {
	cr := &countingReader{r: io.LimitReader(body, MaxAdvertisement)}
	br := bufio.NewReader(cr)
	cut := func(err error) error {
		if cr.n >= MaxAdvertisement {
			return ErrTooManyRefs
		}
		return err
	}
	if smartHTTP {
		line, flush, err := readPkt(br)
		if err != nil {
			return nil, cut(err)
		}
		if flush || !strings.HasPrefix(string(line), "# service=git-upload-pack") {
			return nil, errMalformed
		}
		if _, flush, err = readPkt(br); err != nil || !flush {
			return nil, errMalformed
		}
	}
	refs := map[string]string{}
	seen := false
	for {
		line, flush, err := readPkt(br)
		switch {
		case err != nil && errors.Is(cut(err), ErrTooManyRefs):
			return refs, ErrTooManyRefs
		case err != nil && !seen && !smartHTTP:
			return nil, err // nothing at all: the SSH host hung up
		case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
			return nil, errMalformed // cut short, without the flush
		case err != nil:
			return nil, err
		case flush:
			return refs, nil
		}
		seen = true
		s := strings.TrimSuffix(string(line), "\n")
		if i := strings.IndexByte(s, 0); i >= 0 {
			s = s[:i] // the capabilities, after the first ref
		}
		switch {
		case strings.HasPrefix(s, "ERR "):
			return nil, fmt.Errorf("%w: %s", ErrNoAccess, strings.TrimPrefix(s, "ERR "))
		case s == "version 2":
			return nil, fmt.Errorf("%w: the host answered in protocol v2", errMalformed)
		case s == "version 1":
			continue
		}
		sha, name, ok := strings.Cut(s, " ")
		if !ok || !isHexSHA(sha) {
			return nil, errMalformed
		}
		if strings.HasPrefix(name, "refs/heads/") || strings.HasPrefix(name, "refs/tags/") {
			refs[name] = sha
		}
	}
}

// readPkt reads one pkt-line; flush is the "0000" that ends a section.
func readPkt(r *bufio.Reader) (line []byte, flush bool, err error) {
	var head [4]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return nil, false, err
	}
	n, err := strconv.ParseUint(string(head[:]), 16, 16)
	if err != nil {
		return nil, false, errMalformed
	}
	switch {
	case n == 0:
		return nil, true, nil
	case n < 4:
		// 0001 and 0002 are protocol v2's delimiters.
		return nil, false, errMalformed
	}
	line = make([]byte, n-4)
	if _, err := io.ReadFull(r, line); err != nil {
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return nil, false, err
	}
	return line, false, nil
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// ResolveBranch finds the commit revision names among refs: the branch,
// else the tag, its peeled commit (^{}) when the tag is annotated. A full
// ref name (refs/...) is looked up as given.
func ResolveBranch(refs map[string]string, revision string) (string, bool) {
	names := []string{"refs/heads/" + revision, "refs/tags/" + revision + "^{}", "refs/tags/" + revision}
	if strings.HasPrefix(revision, "refs/") {
		names = []string{revision + "^{}", revision}
	}
	for _, n := range names {
		if sha, ok := refs[n]; ok {
			return sha, true
		}
	}
	return "", false
}

// isHexSHA says s is a SHA-1 or SHA-256 object name.
func isHexSHA(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
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

Two notes for the implementer:
- `"cut short"` in `TestLsRemoteRefusesWhatIsNotAnAdvertisement` ends without a
  flush: `readPkt` gets `io.EOF` on the next header and the loop turns it into
  `errMalformed` over HTTP. Over SSH the same EOF before any ref is the host
  hanging up, which `lsRemoteSSH` turns into `ErrNoAccess`.
- `"no service"` sends refs without the `# service=` line: refused as malformed
  (a dumb HTTP server answers like that, and builds could not use it either).

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./pkg/gitauth/ -v -run 'LsRemote|ResolveBranch|SSHPath'`
Expected: PASS. Then the whole package: `go test ./pkg/gitauth/`.

- [ ] **Step 5: Commit**

```bash
git add pkg/gitauth/lsremote.go pkg/gitauth/lsremote_test.go
git commit -m "feat(gitauth): read a branch's commit over smart HTTP and SSH, without git (RFC-0018)

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 3: `pkg/gitauth`: checking a delivery, reading a push, `[skip deploy]`

**Files:**
- Create: `pkg/gitauth/webhook.go`
- Test: `pkg/gitauth/webhook_test.go`

**Interfaces:**
- Consumes (Task 2): `isHexSHA`, `firstNonEmpty`.
- Produces:
  - `const HookSecretName = "shpyrd-git-hook"`, `HookKeyToken = "token"`, `HookKeySecret = "secret"`
  - `var ErrBadSignature error`
  - `func VerifyDelivery(h http.Header, body []byte, secret string) error`
  - `type Hook struct { Event, Delivery string }` (`Event` is `push`, `ping` or `other`)
  - `func HookOf(h http.Header) Hook`
  - `type Push struct { Ref, SHA, Message, Author, Delivery string; At time.Time; Deleted bool }` (JSON `ref, sha, message, author, delivery, at`; `Deleted` is not serialised)
  - `func ParsePush(body []byte) (Push, error)`: a provider's payload
  - `func ReadPush(annotation string) (*Push, error)`: the `git-push` annotation; `nil, nil` when empty
  - `func SkipsDeploy(message string) bool`
  - `type SourceRecord struct { Ref, Latest string; LatestAt *time.Time; Message, Author, Via, Skipped string; LastWebhookAt, CheckedAt *time.Time; Error, Delivery string; Failures int }` (JSON `ref, latest, latestAt, message, author, via, skipped, lastWebhookAt, checkedAt, error, delivery, failures`)
  - `func ReadSourceRecord(annotation string) SourceRecord`

- [ ] **Step 1: Write the failing tests**

`pkg/gitauth/webhook_test.go`:

```go
package gitauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

const hookSecret = "9a41d0c7e2b85f369a41d0c7e2b85f36"

func hmacHex(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func headers(kv ...string) http.Header {
	h := http.Header{}
	for i := 0; i+1 < len(kv); i += 2 {
		h.Set(kv[i], kv[i+1])
	}
	return h
}

func TestVerifyDeliveryGitHubGiteaGitLab(t *testing.T) {
	body := []byte(`{"ref":"refs/heads/main"}`)
	for name, h := range map[string]http.Header{
		"github": headers("X-Hub-Signature-256", "sha256="+hmacHex(hookSecret, body)),
		"gitea":  headers("X-Gitea-Signature", hmacHex(hookSecret, body)),
		"gitlab": headers("X-Gitlab-Token", hookSecret),
	} {
		if err := VerifyDelivery(h, body, hookSecret); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, h := range map[string]http.Header{
		"another secret":   headers("X-Hub-Signature-256", "sha256="+hmacHex("other", body)),
		"another body":     headers("X-Hub-Signature-256", "sha256="+hmacHex(hookSecret, []byte("{}"))),
		"sha1 only":        headers("X-Hub-Signature", "sha1=abc"),
		"gitlab wrong":     headers("X-Gitlab-Token", "guess"),
		"nothing":          headers(),
		"not hex":          headers("X-Gitea-Signature", "zz"),
	} {
		if err := VerifyDelivery(h, body, hookSecret); !errors.Is(err, ErrBadSignature) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if err := VerifyDelivery(headers("X-Gitlab-Token", ""), body, ""); !errors.Is(err, ErrBadSignature) {
		t.Error("an empty secret verifies nothing")
	}
}

func TestHookOf(t *testing.T) {
	for _, tc := range []struct {
		h    http.Header
		want Hook
	}{
		{headers("X-GitHub-Event", "push", "X-GitHub-Delivery", "gh-1"), Hook{"push", "gh-1"}},
		{headers("X-GitHub-Event", "ping", "X-GitHub-Delivery", "gh-2"), Hook{"ping", "gh-2"}},
		{headers("X-GitHub-Event", "pull_request"), Hook{"other", ""}},
		// Gitea also sends GitHub's headers; its own win.
		{headers("X-Gitea-Event", "push", "X-Gitea-Delivery", "te-1", "X-GitHub-Event", "push", "X-GitHub-Delivery", "te-1"), Hook{"push", "te-1"}},
		{headers("X-Gitlab-Event", "Push Hook", "X-Gitlab-Event-UUID", "gl-1"), Hook{"push", "gl-1"}},
		{headers("X-Gitlab-Event", "Tag Push Hook", "X-Gitlab-Event-UUID", "gl-2"), Hook{"other", "gl-2"}},
	} {
		if got := HookOf(tc.h); got != tc.want {
			t.Errorf("%v: got %+v, want %+v", tc.h, got, tc.want)
		}
	}
}

const (
	beforeSHA = "0123456789abcdef0123456789abcdef01234567"
	afterSHA  = "89abcdef0123456789abcdef0123456789abcdef"
)

func TestParsePushGitHubAndGitea(t *testing.T) {
	body := `{"ref":"refs/heads/main","before":"` + beforeSHA + `","after":"` + afterSHA + `",
	  "head_commit":{"id":"` + afterSHA + `","message":"fix login\n\nThe redirect lost the session.","author":{"name":"Alice","username":"alice"}},
	  "pusher":{"name":"alice"}}`
	p, err := ParsePush([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if p.Ref != "refs/heads/main" || p.SHA != afterSHA || p.Author != "alice" || !strings.HasPrefix(p.Message, "fix login") || p.Deleted {
		t.Errorf("push = %+v", p)
	}
}

func TestParsePushGitLabTakesTheCheckedOutCommit(t *testing.T) {
	body := `{"object_kind":"push","ref":"refs/heads/main","before":"` + beforeSHA + `","after":"` + afterSHA + `",
	  "checkout_sha":"` + afterSHA + `","user_username":"bruno",
	  "commits":[{"id":"` + afterSHA + `","message":"bump deps","author":{"name":"Bruno"}},
	             {"id":"` + beforeSHA + `","message":"older","author":{"name":"Carla"}}]}`
	p, err := ParsePush([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if p.SHA != afterSHA || p.Message != "bump deps" || p.Author != "Bruno" {
		t.Errorf("push = %+v", p)
	}
}

func TestParsePushOfADeletedBranch(t *testing.T) {
	p, err := ParsePush([]byte(`{"ref":"refs/heads/main","before":"` + beforeSHA + `","after":"0000000000000000000000000000000000000000","head_commit":null}`))
	if err != nil || !p.Deleted {
		t.Errorf("push %+v err %v", p, err)
	}
}

func TestParsePushRefusesWhatItCannotRead(t *testing.T) {
	for _, body := range []string{`{"ref":`, `{}`, `{"ref":"refs/heads/main","after":"main"}`, `[]`} {
		if _, err := ParsePush([]byte(body)); err == nil {
			t.Errorf("%s: want an error", body)
		}
	}
}

func TestParsePushKeepsALongMessageShort(t *testing.T) {
	long := strings.Repeat("é", 5000)
	body, _ := json.Marshal(map[string]any{"ref": "refs/heads/main", "after": afterSHA, "head_commit": map[string]any{"message": long}})
	p, err := ParsePush(body)
	if err != nil || len([]rune(p.Message)) != maxMessage {
		t.Errorf("message of %d runes, err %v", len([]rune(p.Message)), err)
	}
}

func TestSkipsDeploy(t *testing.T) {
	for msg, want := range map[string]bool{
		"docs: typo [skip deploy]":   true,
		"[Deploy Skip] tidy":         true,
		"Fix\n\n[SKIP DEPLOY]":       true,
		"skip deploy, said nobody":   false,
		"[skip ci] tests only":       false,
		"":                           false,
	} {
		if got := SkipsDeploy(msg); got != want {
			t.Errorf("%q: %v, want %v", msg, got, want)
		}
	}
}

func TestPushAndRecordAnnotations(t *testing.T) {
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	raw, _ := json.Marshal(Push{Ref: "refs/heads/main", SHA: afterSHA, Author: "alice", Delivery: "d-1", At: at, Deleted: true})
	p, err := ReadPush(string(raw))
	if err != nil || p == nil || p.SHA != afterSHA || p.Delivery != "d-1" || !p.At.Equal(at) || p.Deleted {
		t.Errorf("push %+v err %v", p, err)
	}
	if p, err := ReadPush(""); p != nil || err != nil {
		t.Errorf("no annotation: %+v %v", p, err)
	}
	if _, err := ReadPush("{"); err == nil {
		t.Error("a broken annotation is an error")
	}
	rec := ReadSourceRecord(`{"ref":"main","latest":"` + afterSHA + `","failures":2,"checkedAt":"2026-10-16T12:00:00Z"}`)
	if rec.Ref != "main" || rec.Latest != afterSHA || rec.Failures != 2 || rec.CheckedAt == nil || !rec.CheckedAt.Equal(at) {
		t.Errorf("record %+v", rec)
	}
	if rec := ReadSourceRecord("not json"); rec.Latest != "" || rec.CheckedAt != nil {
		t.Errorf("an unreadable record is an empty one: %+v", rec)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./pkg/gitauth/ -run 'Verify|HookOf|ParsePush|Skips|Annotations'`
Expected: FAIL to compile: `undefined: VerifyDelivery`, `HookOf`, `ParsePush`.

- [ ] **Step 3: Write the implementation**

`pkg/gitauth/webhook.go`:

```go
package gitauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Deploy on push (RFC-0018): checking a delivery from GitHub, GitLab or
// Gitea against a project's webhook secret, reading the push it carries,
// and the records the source watcher keeps on the App.

const (
	// HookSecretName is a project's webhook Secret, in its namespace.
	HookSecretName = "shpyrd-git-hook"
	// HookKeyToken is the random part of the webhook's URL; HookKeySecret
	// signs deliveries (GitHub, Gitea) or is sent as is (GitLab).
	HookKeyToken  = "token"
	HookKeySecret = "secret"
)

// ErrBadSignature: no header of the delivery matches the secret.
var ErrBadSignature = errors.New("the delivery's signature does not match the webhook's secret")

// VerifyDelivery checks a delivery against the project's secret: GitHub's
// X-Hub-Signature-256 or Gitea's X-Gitea-Signature (HMAC-SHA256 of the
// body), or GitLab's X-Gitlab-Token (the secret itself). One must match.
func VerifyDelivery(h http.Header, body []byte, secret string) error {
	if secret == "" {
		return ErrBadSignature
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sum := mac.Sum(nil)
	if sig, ok := strings.CutPrefix(h.Get("X-Hub-Signature-256"), "sha256="); ok && hexEqual(sig, sum) {
		return nil
	}
	if sig := h.Get("X-Gitea-Signature"); sig != "" && hexEqual(sig, sum) {
		return nil
	}
	if tok := h.Get("X-Gitlab-Token"); tok != "" && subtle.ConstantTimeCompare([]byte(tok), []byte(secret)) == 1 {
		return nil
	}
	return ErrBadSignature
}

func hexEqual(given string, sum []byte) bool {
	got, err := hex.DecodeString(strings.TrimSpace(given))
	return err == nil && hmac.Equal(got, sum)
}

// Hook is what a delivery's headers say: its event (push, ping or other)
// and its id, which a host repeats when it delivers again.
type Hook struct {
	Event    string
	Delivery string
}

// HookOf reads a delivery's headers. Gitea sends GitHub's headers too;
// its own are read first.
func HookOf(h http.Header) Hook {
	switch {
	case h.Get("X-Gitlab-Event") != "":
		ev := "other"
		if h.Get("X-Gitlab-Event") == "Push Hook" {
			ev = "push"
		}
		return Hook{Event: ev, Delivery: h.Get("X-Gitlab-Event-UUID")}
	case h.Get("X-Gitea-Event") != "":
		return Hook{Event: eventOf(h.Get("X-Gitea-Event")), Delivery: h.Get("X-Gitea-Delivery")}
	}
	return Hook{Event: eventOf(h.Get("X-GitHub-Event")), Delivery: h.Get("X-GitHub-Delivery")}
}

func eventOf(e string) string {
	if e == "push" || e == "ping" {
		return e
	}
	return "other"
}

// Push is a push to a ref, as the webhook keeps it in the App's
// shpyrd.io/git-push annotation for the source watcher.
type Push struct {
	Ref      string    `json:"ref"`
	SHA      string    `json:"sha"`
	Message  string    `json:"message,omitempty"`
	Author   string    `json:"author,omitempty"`
	Delivery string    `json:"delivery,omitempty"`
	At       time.Time `json:"at"`
	// Deleted: the push removed the ref (its new commit is all zeros).
	Deleted bool `json:"-"`
}

// maxMessage caps the commit message kept: annotations are small.
const maxMessage = 1000

type payloadCommit struct {
	ID      string `json:"id"`
	Message string `json:"message"`
	Author  struct {
		Name     string `json:"name"`
		Username string `json:"username"`
	} `json:"author"`
}

// ParsePush reads a push event's payload: GitHub's and Gitea's
// head_commit, GitLab's checkout_sha among its commits.
func ParsePush(body []byte) (Push, error) {
	var p struct {
		Ref          string          `json:"ref"`
		After        string          `json:"after"`
		CheckoutSHA  string          `json:"checkout_sha"`
		HeadCommit   *payloadCommit  `json:"head_commit"`
		Commits      []payloadCommit `json:"commits"`
		UserUsername string          `json:"user_username"`
		Pusher       struct {
			Name     string `json:"name"`
			Login    string `json:"login"`
			Username string `json:"username"`
		} `json:"pusher"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return Push{}, fmt.Errorf("the push is not JSON shpyrd can read: %w", err)
	}
	if p.Ref == "" || p.After == "" {
		return Push{}, errors.New("the push names no ref or no commit")
	}
	out := Push{Ref: p.Ref, SHA: strings.ToLower(p.After)}
	if strings.Trim(out.SHA, "0") == "" {
		out.Deleted = true
		return out, nil
	}
	if !isHexSHA(out.SHA) {
		return Push{}, errors.New("the push's commit is not a SHA")
	}
	head := p.HeadCommit
	if head == nil {
		want := strings.ToLower(firstNonEmpty(p.CheckoutSHA, p.After))
		for i := range p.Commits {
			if strings.ToLower(p.Commits[i].ID) == want {
				head = &p.Commits[i]
			}
		}
		if head == nil && len(p.Commits) > 0 {
			head = &p.Commits[len(p.Commits)-1]
		}
	}
	if head != nil {
		out.Message = clip(head.Message, maxMessage)
		out.Author = firstNonEmpty(head.Author.Username, head.Author.Name)
	}
	out.Author = firstNonEmpty(out.Author, p.UserUsername, p.Pusher.Login, p.Pusher.Username, p.Pusher.Name)
	return out, nil
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// ReadPush reads the git-push annotation; nil when there is none.
func ReadPush(annotation string) (*Push, error) {
	if annotation == "" {
		return nil, nil
	}
	var p Push
	if err := json.Unmarshal([]byte(annotation), &p); err != nil {
		return nil, err
	}
	return &p, nil
}

var skipMarker = regexp.MustCompile(`(?i)\[(skip deploy|deploy skip)\]`)

// SkipsDeploy says a commit message asks not to deploy it.
func SkipsDeploy(message string) bool { return skipMarker.MatchString(message) }

// SourceRecord is what the source watcher last saw of a branch, kept in
// the App's shpyrd.io/git-source annotation; the App controller shows it
// as status.source.
type SourceRecord struct {
	// Ref is the revision the record is about.
	Ref string `json:"ref,omitempty"`
	// Latest is the newest commit seen on the branch, when, what it says
	// and who made it (when a push said), and how it was seen.
	Latest   string     `json:"latest,omitempty"`
	LatestAt *time.Time `json:"latestAt,omitempty"`
	Message  string     `json:"message,omitempty"`
	Author   string     `json:"author,omitempty"`
	Via      string     `json:"via,omitempty"`
	// Skipped is a commit whose message asked not to deploy it.
	Skipped       string     `json:"skipped,omitempty"`
	LastWebhookAt *time.Time `json:"lastWebhookAt,omitempty"`
	CheckedAt     *time.Time `json:"checkedAt,omitempty"`
	// Error says why the branch could not be read, in words.
	Error string `json:"error,omitempty"`
	// Delivery is the id of the last delivery applied: a host delivering
	// it again is ignored.
	Delivery string `json:"delivery,omitempty"`
	// Failures counts the listings that failed in a row, for the backoff.
	Failures int `json:"failures,omitempty"`
}

// ReadSourceRecord reads the git-source annotation; an unreadable one is
// an empty record.
func ReadSourceRecord(annotation string) SourceRecord {
	var r SourceRecord
	if annotation != "" {
		if err := json.Unmarshal([]byte(annotation), &r); err != nil {
			return SourceRecord{}
		}
	}
	return r
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./pkg/gitauth/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/gitauth/webhook.go pkg/gitauth/webhook_test.go
git commit -m "feat(gitauth): check GitHub, GitLab and Gitea deliveries; read a push; [skip deploy] (RFC-0018)

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 4: The controller: one credential lookup, and the `SourceReconciler`

**Files:**
- Modify: `internal/controller/gitcreds.go:24-60` (`reconcileGitCredentials`), `:124-133` (`optionalSecret`)
- Create: `internal/controller/annotations.go`, `internal/controller/source_controller.go`
- Modify: `pkg/server/server.go`, after the AppReconciler's `SetupWithManager` (line 557-559)
- Test: `internal/controller/source_controller_test.go`

**Interfaces:**
- Consumes: Task 1 (`AnnotationGit*`, `App.GitRevision/WatchesGit/PinnedCommit/AutoDeploys`), Task 2 (`gitauth.LsRemote`, `ResolveBranch`, `ErrNoAccess`, `ErrHostKeyChanged`, `ErrTooManyRefs`), Task 3 (`gitauth.Push`, `ReadPush`, `SkipsDeploy`, `SourceRecord`, `ReadSourceRecord`); PR 1's `gitauth.Parse`, `Resolve`, `DeployKeySecretName`, `TokenSecretName`; `workspaceOf(app)`; `configvars.AnnotationDeployRequested`; `audit.Record`, `audit.AppRefIn`, `audit.Entry`.
- Produces (Task 5 and Task 6 rely on these):
  - `func lookupGitBuild(ctx context.Context, c client.Reader, systemNS string, app *shpyrdv1.App) (gitauth.Build, error)`; `var errGitURL error`
  - `func removeAnnotationIfUnchanged(ctx context.Context, c client.Client, obj client.Object, key, value string) (bool, error)`
  - `func watchesGit(app *shpyrdv1.App) bool` (watched and the URL parses)
  - `type SourceReconciler struct { client.Client; Kube kubernetes.Interface; SystemNamespace string; PollInterval time.Duration; LsRemote func(context.Context, string, gitauth.Build) (map[string]string, error); Now func() time.Time; Jitter func(time.Duration) time.Duration }` with `Reconcile` and `SetupWithManager`
  - `const DefaultPollInterval = 2 * time.Minute`; `func PollInterval(v string) time.Duration`
  - `func shortCommit(sha string) string` (7 characters)
  - Test helpers in `source_controller_test.go`: `const testCommit, otherCommit`; `func pinGit(app *shpyrdv1.App, sha string) *shpyrdv1.App`

- [ ] **Step 1: Write the failing tests**

`internal/controller/source_controller_test.go`:

```go
package controller

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	k8sfake "k8s.io/client-go/kubernetes/fake"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/audit"
	"github.com/shpyrd-io/shpyrd/pkg/configvars"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
)

const (
	testCommit  = "0123456789abcdef0123456789abcdef01234567"
	otherCommit = "89abcdef0123456789abcdef0123456789abcdef"
)

var sourceNow = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

// pinGit pins sha for the revision app follows, as the source watcher does.
func pinGit(app *shpyrdv1.App, sha string) *shpyrdv1.App {
	if app.Annotations == nil {
		app.Annotations = map[string]string{}
	}
	app.Annotations[shpyrdv1.AnnotationGitCommit] = sha
	app.Annotations[shpyrdv1.AnnotationGitCommitRef] = app.GitRevision()
	return app
}

func withPush(app *shpyrdv1.App, p gitauth.Push) *shpyrdv1.App {
	if app.Annotations == nil {
		app.Annotations = map[string]string{}
	}
	raw, _ := json.Marshal(p)
	app.Annotations[shpyrdv1.AnnotationGitPush] = string(raw)
	return app
}

func withRecord(app *shpyrdv1.App, rec gitauth.SourceRecord) *shpyrdv1.App {
	if app.Annotations == nil {
		app.Annotations = map[string]string{}
	}
	raw, _ := json.Marshal(rec)
	app.Annotations[shpyrdv1.AnnotationGitSource] = string(raw)
	return app
}

func sourceRecord(app *shpyrdv1.App) gitauth.SourceRecord {
	return gitauth.ReadSourceRecord(app.Annotations[shpyrdv1.AnnotationGitSource])
}

func at(t time.Time) *time.Time { return &t }

// fakeLsRemote answers every listing with refs and err, and counts them.
type fakeLsRemote struct {
	refs  map[string]string
	err   error
	calls int
	url   string
}

func (f *fakeLsRemote) list(_ context.Context, url string, _ gitauth.Build) (map[string]string, error) {
	f.calls++
	f.url = url
	return f.refs, f.err
}

func newSourceReconciler(t *testing.T, ls *fakeLsRemote, objs ...client.Object) (*SourceReconciler, client.Client) {
	t.Helper()
	_, c := newTestReconciler(t, objs...)
	return &SourceReconciler{
		Client: c, Kube: k8sfake.NewSimpleClientset(), SystemNamespace: "shpyrd-system",
		PollInterval: DefaultPollInterval, LsRemote: ls.list,
		Now:    func() time.Time { return sourceNow },
		Jitter: func(d time.Duration) time.Duration { return d },
	}, c
}

func reconcileSource(t *testing.T, r *SourceReconciler, app *shpyrdv1.App) (ctrl.Result, *shpyrdv1.App) {
	t.Helper()
	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(app)})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	out := &shpyrdv1.App{}
	if err := r.Get(context.Background(), client.ObjectKeyFromObject(app), out); err != nil {
		t.Fatal(err)
	}
	return res, out
}

func heardAudits(t *testing.T) *[]audit.Entry {
	t.Helper()
	var heard []audit.Entry
	audit.Sink = func(_ audit.Ref, e audit.Entry) { heard = append(heard, e) }
	t.Cleanup(func() { audit.Sink = nil })
	return &heard
}

func TestSourcePinsTheBranchOfANewApp(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": testCommit, "refs/heads/dev": otherCommit}}
	app := gitApp("shop", "https://github.com/acme/shop")
	r, _ := newSourceReconciler(t, ls, app)
	heard := heardAudits(t)
	res, got := reconcileSource(t, r, app)
	if got.PinnedCommit() != testCommit || got.Annotations[shpyrdv1.AnnotationGitCommitRef] != "main" {
		t.Fatalf("pin %q ref %q", got.Annotations[shpyrdv1.AnnotationGitCommit], got.Annotations[shpyrdv1.AnnotationGitCommitRef])
	}
	if ls.url != "https://github.com/acme/shop" {
		t.Errorf("listed %q", ls.url)
	}
	if note := got.Annotations[shpyrdv1.AnnotationReleaseNote]; note != "Commit 0123456 (polled)" {
		t.Errorf("note %q", note)
	}
	if got.Annotations[shpyrdv1.AnnotationDeployedBy] != "git" || got.Annotations[shpyrdv1.AnnotationDeployedClient] != "poll" {
		t.Errorf("attribution %q via %q", got.Annotations[shpyrdv1.AnnotationDeployedBy], got.Annotations[shpyrdv1.AnnotationDeployedClient])
	}
	rec := sourceRecord(got)
	if rec.Ref != "main" || rec.Latest != testCommit || rec.Via != "poll" || rec.CheckedAt == nil || !rec.CheckedAt.Equal(sourceNow) || rec.Error != "" {
		t.Errorf("record %+v", rec)
	}
	if res.RequeueAfter != DefaultPollInterval {
		t.Errorf("requeue %v", res.RequeueAfter)
	}
	if len(*heard) != 1 || (*heard)[0].Action != "git.push" || (*heard)[0].Target != "0123456" || (*heard)[0].Actor != "git" {
		t.Errorf("audit %+v", *heard)
	}
}

func TestSourceLeavesAPersonsDeployToThem(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": testCommit}}
	app := gitApp("shop", "https://github.com/acme/shop")
	app.Annotations = map[string]string{shpyrdv1.AnnotationDeployedBy: "ana@acme.test", shpyrdv1.AnnotationReleaseNote: "Deploy from the dashboard"}
	r, _ := newSourceReconciler(t, ls, app)
	heard := heardAudits(t)
	_, got := reconcileSource(t, r, app)
	if got.PinnedCommit() != testCommit {
		t.Fatalf("pin %q", got.PinnedCommit())
	}
	if got.Annotations[shpyrdv1.AnnotationDeployedBy] != "ana@acme.test" || got.Annotations[shpyrdv1.AnnotationReleaseNote] != "Deploy from the dashboard" {
		t.Errorf("a person's deploy keeps their name and note: %v", got.Annotations)
	}
	if sourceRecord(got).Via != "deploy" || len(*heard) != 0 {
		t.Errorf("via %q, audits %+v", sourceRecord(got).Via, *heard)
	}
}

func TestSourceDeploysAPushToTheBranch(t *testing.T) {
	ls := &fakeLsRemote{}
	app := withPush(pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit), gitauth.Push{
		Ref: "refs/heads/main", SHA: otherCommit, Message: "fix login\n\nThe redirect lost the session.", Author: "alice", Delivery: "d-1", At: sourceNow,
	})
	r, _ := newSourceReconciler(t, ls, app)
	heard := heardAudits(t)
	res, got := reconcileSource(t, r, app)
	if got.PinnedCommit() != otherCommit {
		t.Fatalf("pin %q", got.PinnedCommit())
	}
	if note := got.Annotations[shpyrdv1.AnnotationReleaseNote]; note != "Push 89abcde by alice: fix login" {
		t.Errorf("note %q", note)
	}
	if got.Annotations[shpyrdv1.AnnotationDeployedBy] != "git:alice" || got.Annotations[shpyrdv1.AnnotationDeployedClient] != "push" {
		t.Errorf("attribution %v", got.Annotations)
	}
	if _, ok := got.Annotations[shpyrdv1.AnnotationGitPush]; ok || ls.calls != 0 {
		t.Errorf("the push is consumed, and nothing is listed: %d listings", ls.calls)
	}
	rec := sourceRecord(got)
	if rec.Via != "push" || rec.Delivery != "d-1" || rec.Author != "alice" || rec.LastWebhookAt == nil || !rec.LastWebhookAt.Equal(sourceNow) {
		t.Errorf("record %+v", rec)
	}
	if res.RequeueAfter != time.Hour {
		t.Errorf("with a webhook, polls are hourly: %v", res.RequeueAfter)
	}
	if len(*heard) != 1 || (*heard)[0].Actor != "git:alice" || (*heard)[0].Detail != "fix login" {
		t.Errorf("audit %+v", *heard)
	}
}

func TestSourceDropsAPushToAnotherBranch(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": testCommit}}
	app := withPush(withRecord(pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit),
		gitauth.SourceRecord{Ref: "main", Latest: testCommit, CheckedAt: at(sourceNow)}),
		gitauth.Push{Ref: "refs/heads/dev", SHA: otherCommit})
	r, _ := newSourceReconciler(t, ls, app)
	_, got := reconcileSource(t, r, app)
	if got.PinnedCommit() != testCommit || got.Annotations[shpyrdv1.AnnotationGitPush] != "" || ls.calls != 0 {
		t.Errorf("pin %q push %q listings %d", got.PinnedCommit(), got.Annotations[shpyrdv1.AnnotationGitPush], ls.calls)
	}
}

// Review Focus 2.
func TestSourceKeepsASkippedCommitSkipped(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": otherCommit}}
	app := withPush(pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit), gitauth.Push{
		Ref: "refs/heads/main", SHA: otherCommit, Message: "Fix a typo [Skip Deploy]", Author: "bruno", Delivery: "d-2",
	})
	r, _ := newSourceReconciler(t, ls, app)
	heard := heardAudits(t)
	_, got := reconcileSource(t, r, app)
	if got.PinnedCommit() != testCommit || sourceRecord(got).Skipped != otherCommit {
		t.Fatalf("pin %q skipped %q", got.PinnedCommit(), sourceRecord(got).Skipped)
	}
	if len(*heard) != 1 || (*heard)[0].Action != "git.skip" || (*heard)[0].Actor != "git:bruno" {
		t.Errorf("audit %+v", *heard)
	}
	// Three hours on, a poll finds the skipped commit at the head.
	r.Now = func() time.Time { return sourceNow.Add(3 * time.Hour) }
	_, got = reconcileSource(t, r, got)
	if ls.calls != 1 || got.PinnedCommit() != testCommit {
		t.Errorf("listings %d, pin %q: a poll must not deploy a skipped commit", ls.calls, got.PinnedCommit())
	}
}

func TestSourceWithAutoDeployOffRecordsTheCommitOnly(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": otherCommit}}
	off := false
	app := pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit)
	app.Spec.Source.Git.AutoDeploy = &off
	fresh := gitApp("fresh", "https://github.com/acme/fresh")
	fresh.Spec.Source.Git.AutoDeploy = &off
	r, _ := newSourceReconciler(t, ls, app, fresh)
	_, got := reconcileSource(t, r, app)
	if got.PinnedCommit() != testCommit || sourceRecord(got).Latest != otherCommit || got.Annotations[shpyrdv1.AnnotationReleaseNote] != "" {
		t.Errorf("pin %q latest %q note %q", got.PinnedCommit(), sourceRecord(got).Latest, got.Annotations[shpyrdv1.AnnotationReleaseNote])
	}
	// A new app, with nothing pinned yet, still gets its first commit.
	if _, got := reconcileSource(t, r, fresh); got.PinnedCommit() != otherCommit {
		t.Errorf("a new app builds once: pin %q", got.PinnedCommit())
	}
}

// Review Focus 1: a push wins over a listing that still shows the commit
// before it, and the watcher's own write does not bring a listing.
func TestSourceWebhookBeatsAStalePoll(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": testCommit}}
	app := withPush(withRecord(pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit),
		gitauth.SourceRecord{Ref: "main", Latest: testCommit, CheckedAt: at(sourceNow.Add(-time.Hour))}),
		gitauth.Push{Ref: "refs/heads/main", SHA: otherCommit, Author: "alice"})
	r, _ := newSourceReconciler(t, ls, app)
	_, got := reconcileSource(t, r, app)
	_, got = reconcileSource(t, r, got)
	if got.PinnedCommit() != otherCommit || ls.calls != 0 {
		t.Errorf("pin %q after %d listings", got.PinnedCommit(), ls.calls)
	}
}

// Review Focus 1: every status write of the App controller is an App
// event; none of them lists the repository before the check is due.
func TestSourcePollsOnlyWhenDue(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": testCommit}}
	app := withRecord(pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit),
		gitauth.SourceRecord{Ref: "main", Latest: testCommit, CheckedAt: at(sourceNow.Add(-30 * time.Second))})
	failing := withRecord(gitApp("other", "https://github.com/acme/other"),
		gitauth.SourceRecord{Ref: "main", Error: "could not reach github.com", Failures: 3, CheckedAt: at(sourceNow.Add(-10 * time.Second))})
	r, c := newSourceReconciler(t, ls, app, failing)
	var res ctrl.Result
	for i := 0; i < 3; i++ {
		res, _ = reconcileSource(t, r, app)
	}
	if ls.calls != 0 || res.RequeueAfter != 90*time.Second {
		t.Fatalf("listings %d, requeue %v", ls.calls, res.RequeueAfter)
	}
	// An app whose listings fail backs off (30 s, 1 m, 2 m: the third
	// failure waits two minutes)...
	if res, _ = reconcileSource(t, r, failing); ls.calls != 0 || res.RequeueAfter != 110*time.Second {
		t.Fatalf("listings %d, requeue %v", ls.calls, res.RequeueAfter)
	}
	// ...but a deploy asks for the branch at once.
	got := &shpyrdv1.App{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(failing), got); err != nil {
		t.Fatal(err)
	}
	got.Annotations[configvars.AnnotationDeployRequested] = sourceNow.Add(-time.Second).Format(time.RFC3339Nano)
	if err := c.Update(context.Background(), got); err != nil {
		t.Fatal(err)
	}
	if _, got = reconcileSource(t, r, got); ls.calls != 1 || got.PinnedCommit() != testCommit {
		t.Errorf("listings %d, pin %q", ls.calls, got.PinnedCommit())
	}
}

func TestSourceFollowsAForcePushAndKeepsThePinWhenTheBranchGoes(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": otherCommit}}
	app := withRecord(pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit),
		gitauth.SourceRecord{Ref: "main", Latest: testCommit, CheckedAt: at(sourceNow.Add(-time.Hour))})
	r, _ := newSourceReconciler(t, ls, app)
	_, got := reconcileSource(t, r, app)
	if got.PinnedCommit() != otherCommit {
		t.Fatalf("history rewritten: the branch's commit is what runs; pin %q", got.PinnedCommit())
	}
	ls.refs = map[string]string{"refs/heads/dev": testCommit}
	r.Now = func() time.Time { return sourceNow.Add(time.Hour) }
	res, got := reconcileSource(t, r, got)
	rec := sourceRecord(got)
	if got.PinnedCommit() != otherCommit {
		t.Errorf("a deleted branch keeps what runs: pin %q", got.PinnedCommit())
	}
	if !strings.Contains(rec.Error, "no branch or tag named main") || rec.Failures != 1 || res.RequeueAfter != 30*time.Second {
		t.Errorf("record %+v requeue %v", rec, res.RequeueAfter)
	}
}

func TestSourceSaysHowToGiveAccessAndBacksOff(t *testing.T) {
	ls := &fakeLsRemote{err: gitauth.ErrNoAccess}
	app := gitApp("shop", "https://github.com/acme/shop")
	r, _ := newSourceReconciler(t, ls, app)
	res, got := reconcileSource(t, r, app)
	rec := sourceRecord(got)
	if !strings.Contains(rec.Error, "shpyrd git-auth deploy-key") || !strings.Contains(rec.Error, "--host github.com") {
		t.Errorf("error %q", rec.Error)
	}
	if got.PinnedCommit() != "" || rec.Failures != 1 || res.RequeueAfter != 30*time.Second {
		t.Errorf("pin %q failures %d requeue %v", got.PinnedCommit(), rec.Failures, res.RequeueAfter)
	}
	r.Now = func() time.Time { return sourceNow.Add(31 * time.Second) }
	res, got = reconcileSource(t, r, got)
	if ls.calls != 2 || sourceRecord(got).Failures != 2 || res.RequeueAfter != time.Minute {
		t.Errorf("listings %d failures %d requeue %v", ls.calls, sourceRecord(got).Failures, res.RequeueAfter)
	}
}

// Review Focus 4.
func TestSourcePinsTheCommitATagPointsAt(t *testing.T) {
	tagObject := "f00df00df00df00df00df00df00df00df00df00d"
	ls := &fakeLsRemote{refs: map[string]string{"refs/tags/v1.2": tagObject, "refs/tags/v1.2^{}": testCommit}}
	app := gitApp("shop", "https://github.com/acme/shop")
	app.Spec.Source.Git.Revision = "v1.2"
	app = withPush(app, gitauth.Push{Ref: "refs/tags/v1.2", SHA: tagObject})
	r, _ := newSourceReconciler(t, ls, app)
	_, got := reconcileSource(t, r, app)
	if got.PinnedCommit() != testCommit || got.Annotations[shpyrdv1.AnnotationGitCommitRef] != "v1.2" {
		t.Errorf("pin %q ref %q: the tag's commit, not the tag object", got.PinnedCommit(), got.Annotations[shpyrdv1.AnnotationGitCommitRef])
	}
	if got.Annotations[shpyrdv1.AnnotationGitPush] != "" {
		t.Error("a push to a tag is not a push to a branch: dropped")
	}
}

// Review Focus 5.
func TestSourcePinsFromATruncatedListing(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": testCommit}, err: gitauth.ErrTooManyRefs}
	app := gitApp("shop", "https://github.com/acme/shop")
	tagged := gitApp("tagged", "https://github.com/acme/monorepo")
	tagged.Spec.Source.Git.Revision = "v9"
	r, _ := newSourceReconciler(t, ls, app, tagged)
	if _, got := reconcileSource(t, r, app); got.PinnedCommit() != testCommit || sourceRecord(got).Error != "" {
		t.Errorf("pin %q error %q", got.PinnedCommit(), sourceRecord(got).Error)
	}
	if _, got := reconcileSource(t, r, tagged); !strings.Contains(sourceRecord(got).Error, "more refs") {
		t.Errorf("error %q", sourceRecord(got).Error)
	}
}

func TestSourceLeavesCommitsAndUnreadableURLsAlone(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": testCommit}}
	commit := gitApp("pinned", "https://github.com/acme/shop")
	commit.Spec.Source.Git.Revision = "a1b2c3d"
	raw := gitApp("raw", "git://example.test/acme/shop")
	r, _ := newSourceReconciler(t, ls, commit, raw)
	for _, a := range []*shpyrdv1.App{commit, raw} {
		if res, got := reconcileSource(t, r, a); res.RequeueAfter != 0 || got.Annotations[shpyrdv1.AnnotationGitSource] != "" {
			t.Errorf("%s: requeue %v annotations %v", a.Name, res.RequeueAfter, got.Annotations)
		}
	}
	if ls.calls != 0 {
		t.Errorf("%d listings", ls.calls)
	}
}

// Review Focus 3: a newer value written meanwhile is kept.
func TestRemoveAnnotationIfUnchangedKeepsANewerValue(t *testing.T) {
	app := gitApp("shop", "https://github.com/acme/shop")
	app.Annotations = map[string]string{shpyrdv1.AnnotationGitPush: `{"sha":"b"}`}
	_, c := newTestReconciler(t, app)
	ctx := context.Background()
	stale := app.DeepCopy()
	if removed, err := removeAnnotationIfUnchanged(ctx, c, stale, shpyrdv1.AnnotationGitPush, `{"sha":"a"}`); removed || err == nil {
		t.Fatalf("removed %v err %v", removed, err)
	}
	got := &shpyrdv1.App{}
	_ = c.Get(ctx, client.ObjectKeyFromObject(app), got)
	if got.Annotations[shpyrdv1.AnnotationGitPush] != `{"sha":"b"}` {
		t.Fatalf("the newer push is gone: %v", got.Annotations)
	}
	if removed, err := removeAnnotationIfUnchanged(ctx, c, got, shpyrdv1.AnnotationGitPush, `{"sha":"b"}`); !removed || err != nil {
		t.Fatalf("removed %v err %v", removed, err)
	}
	_ = c.Get(ctx, client.ObjectKeyFromObject(app), got)
	if _, ok := got.Annotations[shpyrdv1.AnnotationGitPush]; ok {
		t.Error("an unchanged value is removed")
	}
}

func TestLookupGitBuildIsWhatTheBuildUses(t *testing.T) {
	app := gitApp("shop", "https://github.com/acme/shop")
	_, c := newTestReconciler(t, app, workspaceToken("default", "github.com", "ghp_ours"))
	b, err := lookupGitBuild(context.Background(), c, "shpyrd-system", app)
	if err != nil || b.Kind != gitauth.KindToken || b.Secret == nil || string(b.Secret.Data["password"]) != "ghp_ours" {
		t.Fatalf("build %+v err %v", b, err)
	}
	if _, err := lookupGitBuild(context.Background(), c, "shpyrd-system", gitApp("raw", "git://example.test/x")); err != errGitURL {
		t.Errorf("err = %v, want errGitURL", err)
	}
}

func TestPollIntervalAndJitter(t *testing.T) {
	for in, want := range map[string]time.Duration{"": 2 * time.Minute, "30s": 30 * time.Second, "nonsense": 2 * time.Minute, "1s": 10 * time.Second} {
		if got := PollInterval(in); got != want {
			t.Errorf("PollInterval(%q) = %v, want %v", in, got, want)
		}
	}
	for i := 0; i < 200; i++ {
		if d := jitter(100 * time.Second); d < 90*time.Second || d > 110*time.Second {
			t.Fatalf("jitter(100s) = %v", d)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/controller/ -run 'Source|RemoveAnnotation|LookupGitBuild|PollInterval'`
Expected: FAIL to compile: `undefined: SourceReconciler`, `removeAnnotationIfUnchanged`, `lookupGitBuild`.

- [ ] **Step 3: Write the implementation**

`internal/controller/gitcreds.go`: the lookup moves out of the method so the
watcher lists with the credential the build clones with. Replace the top of
`reconcileGitCredentials`, from `url := app.Spec.Source.Git.URL` to the
`if err != nil { return r.noGitCredential(ctx, app) }` after `Resolve`, with:

```go
	b, err := lookupGitBuild(ctx, r.Client, r.Config.SystemNamespace, app)
	if errors.Is(err, errGitURL) {
		return r.noGitCredential(ctx, app)
	}
	if err != nil {
		return gitauth.Build{}, err
	}
```

Replace the method `optionalSecret` with a function, and add the lookup:

```go
// errGitURL: the App's git URL is not one shpyrd can parse; it builds
// without credentials, as before credentials existed.
var errGitURL = errors.New("the git URL is not one shpyrd can parse")

// lookupGitBuild finds the credential a build of the App's repository
// clones with, and the source watcher lists with: the project's deploy key
// for the host, else the workspace's token for the host, else none.
func lookupGitBuild(ctx context.Context, c client.Reader, systemNS string, app *shpyrdv1.App) (gitauth.Build, error) {
	url := app.Spec.Source.Git.URL
	repo, err := gitauth.Parse(url)
	if err != nil {
		return gitauth.Build{}, errGitURL
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
	return b, nil
}

func optionalSecret(ctx context.Context, c client.Reader, ns, name string) (*corev1.Secret, error) {
	sec := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, sec); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return sec, nil
}
```

Add `"errors"` and `"sigs.k8s.io/controller-runtime/pkg/client"` to its imports.
The rest of `reconcileGitCredentials` (from `if b.Secret == nil`) is unchanged.

`internal/controller/annotations.go`:

```go
package controller

import (
	"context"
	"encoding/json"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// removeAnnotationIfUnchanged removes key from obj on the server only
// while it still holds value: a newer value another writer left meanwhile
// stays (a JSON patch test, then remove). It reports whether it removed
// it; a failed test is an error the caller logs and moves past.
func removeAnnotationIfUnchanged(ctx context.Context, c client.Client, obj client.Object, key, value string) (bool, error) {
	path := "/metadata/annotations/" + strings.NewReplacer("~", "~0", "/", "~1").Replace(key)
	patch, err := json.Marshal([]map[string]any{
		{"op": "test", "path": path, "value": value},
		{"op": "remove", "path": path},
	})
	if err != nil {
		return false, err
	}
	if err := c.Patch(ctx, obj, client.RawPatch(types.JSONPatchType, patch)); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
```

`internal/controller/source_controller.go`:

```go
package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/audit"
	"github.com/shpyrd-io/shpyrd/pkg/configvars"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
)

// The source watcher (RFC-0018). For each App that follows a git branch
// or tag it decides which commit runs: from a push the webhook left on the
// App, else from a listing of the repository when a check is due. It pins
// the commit in two annotations, with the release note and attribution a
// deploy uses, and records what it saw in AnnotationGitSource. It never
// writes status: the App controller copies the record there.

const (
	// DefaultPollInterval is how often a branch with no recent webhook is
	// read (SHPYRD_GIT_POLL_INTERVAL).
	DefaultPollInterval = 2 * time.Minute
	minPollInterval     = 10 * time.Second
	// webhookInterval is how often a branch whose webhook delivered in
	// the last day is read: the webhook is the signal, the listing the
	// net under it.
	webhookInterval = time.Hour
	webhookRecent   = 24 * time.Hour
	// errorBackoff is the first wait after a failed listing; it doubles
	// up to the interval.
	errorBackoff = 30 * time.Second
)

// PollInterval reads SHPYRD_GIT_POLL_INTERVAL ("2m", "30s"): empty or
// unreadable is the default, and nothing below 10 s.
func PollInterval(v string) time.Duration {
	d, err := time.ParseDuration(strings.TrimSpace(v))
	if err != nil || d <= 0 {
		return DefaultPollInterval
	}
	return max(d, minPollInterval)
}

// SourceReconciler pins the commit each watched App builds.
type SourceReconciler struct {
	client.Client
	// Kube writes the git.push and git.skip audit entries; nil records
	// nothing.
	Kube            kubernetes.Interface
	SystemNamespace string
	// PollInterval is the wait between listings; zero is the default.
	PollInterval time.Duration
	// LsRemote lists a repository; nil is gitauth.LsRemote. Tests inject one.
	LsRemote func(ctx context.Context, cloneURL string, b gitauth.Build) (map[string]string, error)
	// Now and Jitter are the clock and the spread of requeues; nil is
	// time.Now and ±10 %.
	Now    func() time.Time
	Jitter func(time.Duration) time.Duration
}

// SetupWithManager registers the watcher on the Apps that follow a branch.
func (r *SourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("source").
		For(&shpyrdv1.App{}, builder.WithPredicates(predicate.NewPredicateFuncs(func(o client.Object) bool {
			app, ok := o.(*shpyrdv1.App)
			return ok && watchesGit(app)
		}))).
		// A listing may take up to 20 s; a few at a time keep hundreds of
		// apps on their schedule.
		WithOptions(controller.Options{MaxConcurrentReconciles: 4}).
		Complete(r)
}

// watchesGit says the App follows a branch or tag of a repository shpyrd
// can read. A URL it cannot parse (git://) builds as given, as before.
func watchesGit(app *shpyrdv1.App) bool {
	if !app.WatchesGit() {
		return false
	}
	_, err := gitauth.Parse(app.Spec.Source.Git.URL)
	return err == nil
}

// sourceEvent is what one reconcile learnt: a push, or the branch's
// commit from a listing, or why the listing failed.
type sourceEvent struct {
	push   *gitauth.Push
	latest string
	err    error
}

// sourceUpdate is what one reconcile writes on the App.
type sourceUpdate struct {
	record gitauth.SourceRecord
	// pin is a commit to pin; "" keeps the pin.
	pin string
	// note, deployedBy and client go with a pin made for a push or a poll.
	note, deployedBy, client string
	// audit is "git.push" or "git.skip", in the name of actor.
	audit, actor string
}

var errNoSuchRevision = errors.New("no such branch or tag")

func (r *SourceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	app := &shpyrdv1.App{}
	if err := r.Get(ctx, req.NamespacedName, app); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !app.DeletionTimestamp.IsZero() || !watchesGit(app) {
		return ctrl.Result{}, nil
	}
	now := r.now()
	rev := app.GitRevision()
	rec := gitauth.ReadSourceRecord(app.Annotations[shpyrdv1.AnnotationGitSource])
	if rec.Ref != rev {
		// Another branch's record: only what says the webhook works
		// carries over.
		rec = gitauth.SourceRecord{LastWebhookAt: rec.LastWebhookAt, Delivery: rec.Delivery}
	}
	rawPush := app.Annotations[shpyrdv1.AnnotationGitPush]
	push, _ := gitauth.ReadPush(rawPush)
	if push != nil && push.Ref != "refs/heads/"+rev {
		push = nil // another branch's, or a tag's: dropped below
	}
	var ev sourceEvent
	switch {
	case push != nil:
		ev.push = push
	case (app.PinnedCommit() == "" && rec.Failures == 0) || r.due(app, rec, now):
		ev.latest, ev.err = r.poll(ctx, app)
	default:
		r.dropPush(ctx, app, rawPush)
		return ctrl.Result{RequeueAfter: r.untilDue(rec, now)}, nil
	}
	u := decideSource(app, rec, ev, now)
	if err := r.apply(ctx, app, u); err != nil {
		return ctrl.Result{}, err
	}
	r.dropPush(ctx, app, rawPush)
	r.auditUpdate(ctx, app, u)
	return ctrl.Result{RequeueAfter: r.untilDue(u.record, now)}, nil
}

// decideSource turns what was learnt into what is written: the record
// always, a pin when the branch moved and the App deploys it.
func decideSource(app *shpyrdv1.App, rec gitauth.SourceRecord, ev sourceEvent, now time.Time) sourceUpdate {
	u := sourceUpdate{record: rec}
	u.record.Ref = app.GitRevision()
	u.record.CheckedAt = &now
	if ev.err != nil {
		u.record.Error = describeSourceError(ev.err, app)
		u.record.Failures++
		return u
	}
	u.record.Error, u.record.Failures = "", 0
	sha, via, message, author := ev.latest, "poll", "", ""
	if p := ev.push; p != nil {
		sha, via, message, author = strings.ToLower(p.SHA), "push", p.Message, p.Author
		u.record.LastWebhookAt = &now
		u.record.Delivery = p.Delivery
	}
	if via == "push" || sha != rec.Latest {
		u.record.Latest, u.record.LatestAt = sha, &now
		u.record.Message, u.record.Author, u.record.Via = message, author, via
	}
	u.actor = "git"
	if u.record.Author != "" {
		u.actor = "git:" + u.record.Author
	}
	skip := ev.push != nil && gitauth.SkipsDeploy(message)
	if skip {
		u.record.Skipped = sha
		u.audit = "git.skip"
	}
	pin := app.PinnedCommit()
	switch {
	case pin == "":
		// Nothing pinned for this revision yet (a new app, a deploy, a
		// changed branch): its commit, whatever auto-deploy says. A
		// person's deploy keeps their note and name.
		if by := app.Annotations[shpyrdv1.AnnotationDeployedBy]; by != "" && by != "git" && !strings.HasPrefix(by, "git:") {
			u.pin, u.record.Via, u.record.Skipped, u.audit = sha, "deploy", "", ""
			return u
		}
	case sha == pin, skip, sha == rec.Skipped, !app.AutoDeploys():
		// Nothing new, a commit that asked not to deploy (now, or when a
		// push said so before this poll found it), or pushes recorded only.
		return u
	}
	u.pin, u.record.Skipped, u.audit = sha, "", "git.push"
	u.deployedBy, u.client = u.actor, u.record.Via
	if u.record.Via == "push" {
		u.note = pushNote(sha, u.record.Author, u.record.Message)
	} else {
		u.note = "Commit " + shortCommit(sha) + " (polled)"
	}
	return u
}

// pushNote is the release's description: "Push a1b2c3d by alice: fix login".
func pushNote(sha, author, message string) string {
	note := "Push " + shortCommit(sha)
	if author != "" {
		note += " by " + author
	}
	if line := firstLine(message); line != "" {
		note += ": " + line
	}
	return note
}

func shortCommit(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// firstLine is a commit message's subject, at most 72 characters.
func firstLine(message string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(message), "\n")
	if r := []rune(strings.TrimSpace(line)); len(r) > 72 {
		return string(r[:71]) + "…"
	}
	return strings.TrimSpace(line)
}

// describeSourceError says why the branch could not be read, and what to
// do, in the words of PR 1's refused clone.
func describeSourceError(err error, app *shpyrdv1.App) string {
	rev := app.GitRevision()
	host := "<host>"
	if r, perr := gitauth.Parse(app.Spec.Source.Git.URL); perr == nil {
		host = r.Host
	}
	switch {
	case errors.Is(err, errNoSuchRevision):
		return fmt.Sprintf("The repository has no branch or tag named %s: it was deleted or renamed. What runs keeps running; deploy another branch with `shpyrd deploy --git <url> --ref <branch>`.", rev)
	case errors.Is(err, gitauth.ErrTooManyRefs):
		return fmt.Sprintf("The repository lists more refs than shpyrd reads (%d MiB) before it reaches %s: follow a branch, or pin a commit with `shpyrd deploy --git <url> --ref <commit>`.", gitauth.MaxAdvertisement>>20, rev)
	case errors.Is(err, gitauth.ErrHostKeyChanged):
		return "shpyrd could not check the git host's identity: its SSH host key is not the one recorded with the deploy key. If the host changed its key, make a new deploy key with `shpyrd git-auth deploy-key` and add it to the repository."
	case errors.Is(err, gitauth.ErrNoAccess):
		return fmt.Sprintf("shpyrd could not read the repository: it is private, or does not exist under that name. Give shpyrd access: `shpyrd git-auth deploy-key` prints a key to add to the repository as a read-only deploy key, or a workspace admin sets a token for the host with `shpyrd git-auth set --host %s --token -`. shpyrd tries again by itself.", host)
	}
	return fmt.Sprintf("shpyrd could not read the branch's latest commit from %s (%v). It tries again by itself.", host, err)
}

// poll lists the repository with the build's credential and finds the
// revision's commit. A listing cut at the cap still answers when the
// revision is among what was read.
func (r *SourceReconciler) poll(ctx context.Context, app *shpyrdv1.App) (string, error) {
	b, err := lookupGitBuild(ctx, r.Client, r.SystemNamespace, app)
	if err != nil {
		return "", err
	}
	list := r.LsRemote
	if list == nil {
		list = gitauth.LsRemote
	}
	refs, err := list(ctx, firstNonEmpty(b.CloneURL, app.Spec.Source.Git.URL), b)
	if err != nil && !errors.Is(err, gitauth.ErrTooManyRefs) {
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

// apply writes the record, and the pin with its note and attribution, in
// one merge patch: the App controller's own writes are not touched.
func (r *SourceReconciler) apply(ctx context.Context, app *shpyrdv1.App, u sourceUpdate) error {
	raw, err := json.Marshal(u.record)
	if err != nil {
		return err
	}
	ann := map[string]any{shpyrdv1.AnnotationGitSource: string(raw)}
	if u.pin != "" {
		ann[shpyrdv1.AnnotationGitCommit] = u.pin
		ann[shpyrdv1.AnnotationGitCommitRef] = app.GitRevision()
		if u.note != "" {
			ann[shpyrdv1.AnnotationReleaseNote] = u.note
		}
		if u.deployedBy != "" {
			ann[shpyrdv1.AnnotationDeployedBy] = u.deployedBy
			ann[shpyrdv1.AnnotationDeployedClient] = u.client
			ann[shpyrdv1.AnnotationDeployedSubject] = nil
		}
	}
	patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": ann}})
	if err != nil {
		return err
	}
	return r.Patch(ctx, app, client.RawPatch(types.MergePatchType, patch))
}

// dropPush removes the push this reconcile read, unless a newer one
// arrived meanwhile: that one is read next.
func (r *SourceReconciler) dropPush(ctx context.Context, app *shpyrdv1.App, raw string) {
	if raw == "" {
		return
	}
	if _, err := removeAnnotationIfUnchanged(ctx, r.Client, app, shpyrdv1.AnnotationGitPush, raw); err != nil {
		log.FromContext(ctx).V(1).Info("kept a newer push for the next pass", "err", err.Error())
	}
}

func (r *SourceReconciler) auditUpdate(ctx context.Context, app *shpyrdv1.App, u sourceUpdate) {
	if r.Kube == nil || u.audit == "" {
		return
	}
	entry := audit.Entry{
		Actor: u.actor, Action: u.audit, Target: shortCommit(firstNonEmpty(u.pin, u.record.Skipped)),
		Detail: firstLine(u.record.Message), Via: "controller", Client: u.record.Via, Realm: "workspace",
	}
	if err := audit.Record(ctx, r.Kube, audit.AppRefIn(app.Namespace, app.Name), entry); err != nil {
		log.FromContext(ctx).Info("could not audit the push", "err", err.Error())
	}
}

// due says the branch should be read now: never read, a deploy asked
// after the last check, or the wait since the last check has passed (90 %
// of it: requeues are spread ±10 %).
func (r *SourceReconciler) due(app *shpyrdv1.App, rec gitauth.SourceRecord, now time.Time) bool {
	if rec.CheckedAt == nil {
		return true
	}
	if asked, err := time.Parse(time.RFC3339Nano, app.Annotations[configvars.AnnotationDeployRequested]); err == nil && asked.After(*rec.CheckedAt) {
		return true
	}
	return now.Sub(*rec.CheckedAt) >= r.wait(rec, now)*9/10
}

// wait is the time between checks: the interval (an hour while webhooks
// arrive), or after failures a backoff from 30 s that doubles up to it.
func (r *SourceReconciler) wait(rec gitauth.SourceRecord, now time.Time) time.Duration {
	every := r.PollInterval
	if every <= 0 {
		every = DefaultPollInterval
	}
	if rec.LastWebhookAt != nil && now.Sub(*rec.LastWebhookAt) < webhookRecent {
		every = max(every, webhookInterval)
	}
	if rec.Failures > 0 {
		return min(every, errorBackoff<<min(rec.Failures-1, 10))
	}
	return every
}

func (r *SourceReconciler) untilDue(rec gitauth.SourceRecord, now time.Time) time.Duration {
	left := r.wait(rec, now)
	if rec.CheckedAt != nil {
		left -= now.Sub(*rec.CheckedAt)
	}
	spread := r.Jitter
	if spread == nil {
		spread = jitter
	}
	return spread(max(left, time.Second))
}

// jitter spreads d by ±10 %, so apps checked together drift apart.
func jitter(d time.Duration) time.Duration {
	return d + time.Duration((rand.Float64()*0.2-0.1)*float64(d))
}

func (r *SourceReconciler) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}
```

Notes for the implementer:
- The failing app in `TestSourcePollsOnlyWhenDue` was checked 10 s ago after
  its third failure: the wait is `min(2m, 30s<<2)` = 2 m, so it is not due,
  and the requeue is 2 m - 10 s = 110 s.
- `decideSource`'s `case sha == pin, skip, ...` is evaluated only when a
  pin exists; with no pin the first case falls through to pinning.
- `firstNonEmpty` is the controller package's (`app_controller.go`).

Register it in `pkg/server/server.go`, right after
`if err := rec.SetupWithManager(mgr); err != nil {...}`:

```go
	// The source watcher (RFC-0018): pins the commit each App on a git
	// branch builds, from webhooks and polling.
	source := &controller.SourceReconciler{
		Client:          mgr.GetClient(),
		Kube:            k.Kube,
		SystemNamespace: k.Namespace,
		PollInterval:    controller.PollInterval(os.Getenv("SHPYRD_GIT_POLL_INTERVAL")),
	}
	if err := source.SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("source watcher: %w", err)
	}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go build ./... && go test ./internal/controller/ -run 'Source|RemoveAnnotation|LookupGitBuild|PollInterval|GitCredentials|Dockerfile'`
Expected: PASS. PR 1's credential tests pass unchanged: the refactor moves
code, not behaviour.

- [ ] **Step 5: Commit**

```bash
git add internal/controller/gitcreds.go internal/controller/annotations.go internal/controller/source_controller.go internal/controller/source_controller_test.go pkg/server/server.go
git commit -m "feat(controller): the source watcher pins each branch's commit from pushes and polling (RFC-0018)

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 5: The AppReconciler builds the pin and shows `status.source`

**Files:**
- Create: `internal/controller/source_build.go`
- Modify: `internal/controller/desired.go:590-596`, `internal/controller/dockerfile.go:239` and `:518`, `internal/controller/status.go:192-194`, `internal/controller/app_controller.go` (before `// 2. Image:` ~379; line 384; 466; 495; 637; 1128-1138; 1145-1152)
- Modify (tests that build a git app on a branch): `internal/controller/app_controller_test.go` (TestReconcileSourceBuild ~221, TestRunningKpackBuildIsTheLatest ~286, TestKpackImageRecreatedWhenRegistryChanges ~515), `project_builder_test.go` (~57), `build_env_test.go` (~171), `build_failure_test.go` (~71), `dockerfile_test.go` (~174-180), `gitcreds_test.go` (`dockerfileGitApp`, ~171-175)
- Test: `internal/controller/source_build_test.go`

**Interfaces:**
- Consumes: Task 1 (`BuildRevision`, `PinnedCommit`, `SourceStatus`, `ConditionSourceReady`), Task 3 (`gitauth.ReadSourceRecord`), Task 4 (`watchesGit`, `removeAnnotationIfUnchanged`, `shortCommit`, test helpers `pinGit`, `testCommit`, `otherCommit`).
- Produces:
  - `const reasonWaitingForCommit = "WaitingForCommit"`
  - `func waitingForCommit(app *shpyrdv1.App) (message string, failed bool)`
  - `func sourceStatus(app *shpyrdv1.App) *shpyrdv1.SourceStatus`
  - `func setSourceCondition(app *shpyrdv1.App)`
  - kpack's `spec.source.git.revision`, the Dockerfile build key and `fetchScript`'s `rev` are `app.BuildRevision()`

- [ ] **Step 1: Write the failing tests**

`internal/controller/source_build_test.go`:

```go
package controller

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
)

// kpackRevision is the revision of the App's kpack Image; false when there
// is no Image.
func kpackRevision(t *testing.T, c client.Client, app *shpyrdv1.App) (string, bool) {
	t.Helper()
	img := &unstructured.Unstructured{}
	img.SetGroupVersionKind(KpackImageGVK)
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(app), img); err != nil {
		return "", false
	}
	rev, _, _ := unstructured.NestedString(img.Object, "spec", "source", "git", "revision")
	return rev, true
}

func sourceCondition(app *shpyrdv1.App) *metav1.Condition {
	return meta.FindStatusCondition(app.Status.Conditions, shpyrdv1.ConditionSourceReady)
}

func TestABranchWaitsForItsCommit(t *testing.T) {
	app := gitApp("shop", "https://github.com/acme/shop")
	r, c := newTestReconciler(t, app)
	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(app)})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := kpackRevision(t, c, app); ok {
		t.Fatal("nothing is built before a commit is pinned")
	}
	got := &shpyrdv1.App{}
	_ = c.Get(context.Background(), client.ObjectKeyFromObject(app), got)
	if got.Status.Phase != shpyrdv1.PhaseBuilding || got.Status.Message != "Reading the branch's latest commit." {
		t.Errorf("phase %q %q", got.Status.Phase, got.Status.Message)
	}
	if res.RequeueAfter != 5*time.Second {
		t.Errorf("requeue %v: the watcher pins within seconds", res.RequeueAfter)
	}
	if cond := sourceCondition(got); cond == nil || cond.Status != metav1.ConditionUnknown {
		t.Errorf("SourceReady %+v", cond)
	}
}

func TestABranchThatCannotBeReadSaysWhy(t *testing.T) {
	why := "shpyrd could not read the repository: it is private, or does not exist under that name."
	app := withRecord(gitApp("shop", "https://github.com/acme/shop"), gitauth.SourceRecord{Ref: "main", Error: why, Failures: 1})
	r, _ := newTestReconciler(t, app)
	got := runReconcile(t, r, app)
	if got.Status.Phase != shpyrdv1.PhaseFailed || got.Status.Message != why {
		t.Errorf("phase %q %q", got.Status.Phase, got.Status.Message)
	}
	if got.Status.Source == nil || got.Status.Source.Error != why {
		t.Errorf("status.source %+v", got.Status.Source)
	}
	if cond := sourceCondition(got); cond == nil || cond.Status != metav1.ConditionFalse || cond.Message != why {
		t.Errorf("SourceReady %+v", cond)
	}
}

func TestKpackBuildsThePinnedCommit(t *testing.T) {
	app := withRecord(pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit),
		gitauth.SourceRecord{Ref: "main", Latest: testCommit, Message: "fix login", Author: "alice", Via: "push", CheckedAt: at(sourceNow)})
	r, c := newTestReconciler(t, app)
	got := runReconcile(t, r, app)
	if rev, ok := kpackRevision(t, c, app); !ok || rev != testCommit {
		t.Fatalf("kpack revision %q (%v): the pinned SHA, so kpack stops polling the branch", rev, ok)
	}
	s := got.Status.Source
	if s == nil || s.Commit != testCommit || s.Latest != testCommit || s.Author != "alice" || s.Via != "push" || s.CheckedAt == nil {
		t.Errorf("status.source %+v", s)
	}
	if cond := sourceCondition(got); cond == nil || cond.Status != metav1.ConditionTrue {
		t.Errorf("SourceReady %+v", cond)
	}
}

func TestAPinForAnotherBranchIsNotBuilt(t *testing.T) {
	app := pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit)
	app.Spec.Source.Git.Revision = "release"
	r, c := newTestReconciler(t, app)
	runReconcile(t, r, app)
	if _, ok := kpackRevision(t, c, app); ok {
		t.Error("main's commit must not build release")
	}
}

func TestACommitRevisionIsBuiltAsGiven(t *testing.T) {
	app := gitApp("shop", "https://github.com/acme/shop")
	app.Spec.Source.Git.Revision = "a1b2c3d"
	r, c := newTestReconciler(t, app)
	got := runReconcile(t, r, app)
	if rev, ok := kpackRevision(t, c, app); !ok || rev != "a1b2c3d" {
		t.Errorf("kpack revision %q (%v)", rev, ok)
	}
	if got.Status.Source != nil || sourceCondition(got) != nil {
		t.Errorf("a commit is not watched: %+v", got.Status.Source)
	}
}

func TestDockerfileBuildsThePinnedCommit(t *testing.T) {
	a := dockerfileGitApp("https://github.com/acme/dk")
	b := pinGit(dockerfileGitApp("https://github.com/acme/dk"), otherCommit)
	if buildKey(a) == buildKey(b) {
		t.Error("a new commit is a new build")
	}
	if script := (Config{}).fetchScript(a, gitauth.Build{}); !strings.Contains(script, "rev='"+testCommit+"'") {
		t.Errorf("the fetch checks out the pinned commit:\n%s", script)
	}
}

// Review Focus 3: the note the watcher wrote for the next push survives
// the clearing of the note before it.
func TestClearReleaseNoteKeepsANewerNote(t *testing.T) {
	app := sampleApp("shop")
	app.Annotations = map[string]string{shpyrdv1.AnnotationReleaseNote: "Push 89abcde by bob: second"}
	r, c := newTestReconciler(t, app)
	ctx := context.Background()
	stale := app.DeepCopy()
	stale.Annotations[shpyrdv1.AnnotationReleaseNote] = "Push 0123456 by alice: first"
	r.clearReleaseNote(ctx, stale)
	got := &shpyrdv1.App{}
	_ = c.Get(ctx, client.ObjectKeyFromObject(app), got)
	if got.Annotations[shpyrdv1.AnnotationReleaseNote] != "Push 89abcde by bob: second" {
		t.Fatalf("the newer note is gone: %v", got.Annotations)
	}
	r.clearReleaseNote(ctx, got)
	_ = c.Get(ctx, client.ObjectKeyFromObject(app), got)
	if _, ok := got.Annotations[shpyrdv1.AnnotationReleaseNote]; ok {
		t.Error("the note that was read is cleared")
	}
}

func TestStatusSourceIgnoresAnotherBranchsRecord(t *testing.T) {
	raw, _ := json.Marshal(gitauth.SourceRecord{Ref: "main", Error: "old"})
	app := pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit)
	app.Spec.Source.Git.Revision = "release"
	app = pinGit(app, otherCommit)
	app.Annotations[shpyrdv1.AnnotationGitSource] = string(raw)
	if s := sourceStatus(app); s == nil || s.Commit != otherCommit || s.Error != "" {
		t.Errorf("status.source %+v", s)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/controller/ -run 'Branch|PinnedCommit|ACommitRevision|ClearReleaseNote|StatusSource'`
Expected: FAIL to compile: `undefined: sourceStatus`. Once it compiles
(Step 3's first file), `TestABranchWaitsForItsCommit` fails until the
reconcile changes are in.

- [ ] **Step 3: Write the implementation**

`internal/controller/source_build.go`:

```go
package controller

import (
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
)

// The App controller's side of the source watcher (RFC-0018): a branch
// builds the commit pinned for it, and status.source shows what the
// watcher saw.

// reasonWaitingForCommit is the Built condition's reason while a branch
// has no commit pinned yet.
const reasonWaitingForCommit = "WaitingForCommit"

// waitingForCommit says why a watched App builds nothing yet: the watcher
// has not pinned a commit (a few seconds), or it cannot read the branch
// (failed, with its words). "" when a commit is pinned or nothing is
// watched.
func waitingForCommit(app *shpyrdv1.App) (string, bool) {
	if !watchesGit(app) || app.PinnedCommit() != "" {
		return "", false
	}
	if rec := gitauth.ReadSourceRecord(app.Annotations[shpyrdv1.AnnotationGitSource]); rec.Ref == app.GitRevision() && rec.Error != "" {
		return rec.Error, true
	}
	return "Reading the branch's latest commit.", false
}

// sourceStatus is status.source: the commit pinned, and what the watcher
// recorded of the revision the App follows. nil when nothing is watched.
func sourceStatus(app *shpyrdv1.App) *shpyrdv1.SourceStatus {
	if !watchesGit(app) {
		return nil
	}
	st := &shpyrdv1.SourceStatus{Commit: app.PinnedCommit()}
	rec := gitauth.ReadSourceRecord(app.Annotations[shpyrdv1.AnnotationGitSource])
	if rec.Ref != app.GitRevision() {
		return st
	}
	st.Latest, st.Message, st.Author, st.Via, st.Skipped, st.Error = rec.Latest, rec.Message, rec.Author, rec.Via, rec.Skipped, rec.Error
	st.LatestAt, st.LastWebhookAt, st.CheckedAt = metaTime(rec.LatestAt), metaTime(rec.LastWebhookAt), metaTime(rec.CheckedAt)
	return st
}

func metaTime(t *time.Time) *metav1.Time {
	if t == nil {
		return nil
	}
	m := metav1.NewTime(*t)
	return &m
}

// setSourceCondition mirrors status.source in SourceReady.
func setSourceCondition(app *shpyrdv1.App) {
	st := app.Status.Source
	switch {
	case st == nil:
		meta.RemoveStatusCondition(&app.Status.Conditions, shpyrdv1.ConditionSourceReady)
	case st.Error != "":
		setCondition(app, shpyrdv1.ConditionSourceReady, metav1.ConditionFalse, "Unreadable", st.Error)
	case st.Commit == "":
		setCondition(app, shpyrdv1.ConditionSourceReady, metav1.ConditionUnknown, "Reading", "Reading the branch's latest commit.")
	default:
		setCondition(app, shpyrdv1.ConditionSourceReady, metav1.ConditionTrue, "Pinned", "commit "+shortCommit(st.Commit))
	}
}
```

`internal/controller/desired.go`, in `desiredKpackImage`, replace

```go
		gitSrc := map[string]interface{}{"url": url}
		rev := app.Spec.Source.Git.Revision
		if rev == "" {
			rev = "main"
		}
		gitSrc["revision"] = rev
```

with

```go
		// A branch builds the commit the source watcher pinned, so kpack
		// does not poll it (RFC-0018); a commit builds as given.
		gitSrc := map[string]interface{}{"url": url, "revision": app.BuildRevision()}
```

`internal/controller/dockerfile.go`, in `buildKey`:

```go
		case src.Git != nil:
			// The pinned commit: a new commit of the branch is a new build.
			h.Write([]byte("git:" + src.Git.URL + "#" + app.BuildRevision()))
```

and in `fetchScript`, `rev := firstNonEmpty(src.Git.Revision, "main")` becomes:

```go
		// The pinned commit of a branch; the script already fetches a
		// commit that is not a branch or tag.
		rev := app.BuildRevision()
```

`internal/controller/status.go`, the end of `sourceID`:

```go
	if pin := app.PinnedCommit(); pin != "" {
		return short(pin)
	}
	if app.Spec.Source != nil && app.Spec.Source.Git != nil {
		return app.Spec.Source.Git.Revision
	}
	return ""
```

`internal/controller/app_controller.go`, five edits in `reconcile` and two
after it.

a) Before `	// 2. Image: pinned, or built from the source (kpack Image for`:

```go
	// What the source watcher saw of the branch (RFC-0018).
	app.Status.Source = sourceStatus(app)
	setSourceCondition(app)

```

b) Line 384, `		if app.BuildStrategy() == shpyrdv1.StrategyDockerfile {` becomes:

```go
		if msg, failed := waitingForCommit(app); msg != "" {
			// No commit pinned for the branch yet: nothing is built, and
			// what runs keeps running.
			build = buildState{Ready: "Unknown", Reason: reasonWaitingForCommit, Message: msg, LatestImage: app.Status.Image, LatestBuild: app.Status.LatestBuild}
			if failed {
				build.Ready = "False"
			}
		} else if app.BuildStrategy() == shpyrdv1.StrategyDockerfile {
```

c) Line 466, `			if build.Reason == "PreparingBuilder" {` becomes:

```go
			if build.Reason == "PreparingBuilder" || build.Reason == reasonWaitingForCommit {
```

d) Line 495, at the end of "3. Nothing to run yet", `		return requeue(30 * time.Second), nil` becomes:

```go
		if build.Reason == reasonWaitingForCommit {
			return requeue(5 * time.Second), nil
		}
		return requeue(30 * time.Second), nil
```

e) Line 637, in the `case app.HasSource() && app.Spec.Image == "" && build.Ready != "True" && build.Ready != "False":`
branch, after `		out.result = ctrl.Result{RequeueAfter: 30 * time.Second}`:

```go
		if build.Reason == reasonWaitingForCommit {
			out.result.RequeueAfter = 5 * time.Second
		}
```

f) `auditReleaseOutcome`, replace the block from `		patch := client.MergeFrom(app.DeepCopy())`
to the closing `}` of `if err := r.Patch(...)` with:

```go
		// Drop who asked for the release, unless someone (a push) asked
		// for the next one meanwhile.
		asked := map[string]string{}
		for _, k := range []string{shpyrdv1.AnnotationDeployedBy, shpyrdv1.AnnotationDeployedSubject, shpyrdv1.AnnotationDeployedClient} {
			if v, ok := app.Annotations[k]; ok {
				asked[k] = v
			}
		}
		patch := client.MergeFrom(app.DeepCopy())
		if app.Annotations == nil {
			app.Annotations = map[string]string{}
		}
		app.Annotations[shpyrdv1.AnnotationAuditedRelease] = strconv.Itoa(cur.Number)
		if err := r.Patch(ctx, app, patch); err != nil {
			log.FromContext(ctx).Info("could not mark the release audited", "err", err.Error())
			return
		}
		for k, v := range asked {
			if _, err := removeAnnotationIfUnchanged(ctx, r.Client, app, k, v); err != nil {
				log.FromContext(ctx).V(1).Info("kept who asked for the next release", "annotation", k, "err", err.Error())
			}
		}
```

g) Replace `clearReleaseNote`:

```go
// clearReleaseNote removes the one-shot annotations once consumed, each
// only while it holds the value this reconcile read: a note the source
// watcher wrote meanwhile is the next release's (RFC-0018).
func (r *AppReconciler) clearReleaseNote(ctx context.Context, app *shpyrdv1.App) {
	read := map[string]string{}
	for _, k := range []string{shpyrdv1.AnnotationReleaseNote, shpyrdv1.AnnotationRollbackTo} {
		if v, ok := app.Annotations[k]; ok {
			read[k] = v
		}
	}
	for k, v := range read {
		if _, err := removeAnnotationIfUnchanged(ctx, r.Client, app, k, v); err != nil {
			log.FromContext(ctx).V(1).Info("kept a newer one-shot annotation", "annotation", k, "err", err.Error())
		}
	}
}
```

The values are read before the first patch: a successful patch refreshes
`app` from the server, and the second test would then compare the server
with itself.

Now the tests that build git apps on a branch. Without a pin they no longer
build, which is the new behaviour; they get one:

- `app_controller_test.go`: in `TestReconcileSourceBuild`,
  `TestRunningKpackBuildIsTheLatest` and
  `TestKpackImageRecreatedWhenRegistryChanges`,
  `r, c := newTestReconciler(t, app)` becomes
  `r, c := newTestReconciler(t, pinGit(app, testCommit))`.
- `project_builder_test.go`: `r, c := newTestReconciler(t, app, b)` becomes
  `r, c := newTestReconciler(t, pinGit(app, testCommit), b)`.
- `build_env_test.go` (`TestGitImageBindsTheVariablesAtOnce`):
  `r, c := newTestReconciler(t, app)` becomes
  `r, c := newTestReconciler(t, pinGit(app, testCommit))`.
- `build_failure_test.go` (`TestKpackBuildFailureInWords`):
  `r, c := newTestReconciler(t, app, build, pod)` becomes
  `r, c := newTestReconciler(t, pinGit(app, testCommit), build, pod)`.
- `dockerfile_test.go` (`TestDockerfileGitFetchAndProcesses`): the revision
  is the tag `v1.2`, so `r, c := newTestReconciler(t, app)` becomes
  `r, c := newTestReconciler(t, pinGit(app, testCommit))`, and the check
  `!strings.Contains(fetch, "rev='v1.2'")` becomes
  `!strings.Contains(fetch, "rev='"+testCommit+"'")`.
- `gitcreds_test.go`: `dockerfileGitApp` ends with
  `return pinGit(app, testCommit)`.

`TestReconcileRollbackNote` needs nothing: its App runs a pinned image
(`spec.image`), so waiting for a commit changes no release.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/controller/`
Expected: PASS. If a test outside the list above fails because a kpack
Image or build Job is missing, find it with
`grep -n "GitSource{" internal/controller/*_test.go`: it builds a git app on
a branch, and gets `pinGit(app, testCommit)` where the App is handed to
`newTestReconciler` the same way.

Then the whole repository's Go: `go build ./... && go vet ./internal/... ./pkg/... && go test ./internal/... ./pkg/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/controller
git commit -m "feat(controller): a branch builds its pinned commit; status.source and SourceReady (RFC-0018)

kpack's revision and the Dockerfile build key are the pinned SHA, so
Dockerfile builds redeploy on new commits. Each existing app on a branch
rebuilds once on the upgrade, from the branch to its SHA.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 6: The API: the webhook, the hook Secret, auto-deploy, the deploy, the project view

**Files:**
- Create: `pkg/api/githooks.go`
- Modify: `pkg/api/server.go` (the `Server` fields next to `deployKeyMints` ~280; their setup next to `s.deployKeyMints = ...` ~382; the `pub` group after `pub.GET("/sources/:name", s.serveSource)` ~577; the project routes after the `git-auth` ones ~799)
- Modify: `pkg/api/apps.go` (`AppDetailStatus` ~183, `detail` ~240, `UpdateAppRequest` 543-553, `updateApp` 558-617, `deployApp` 703-709)
- Test: `pkg/api/githooks_test.go`

**Interfaces:**
- Consumes: Task 1 (`AnnotationGit*`, `App.WatchesGit`, `GitRevision`, `GitSource.AutoDeploy`, `SourceStatus`), Task 3 (`gitauth.HookSecretName`, `HookKeyToken`, `HookKeySecret`, `VerifyDelivery`, `HookOf`, `ParsePush`, `ReadPush`, `ReadSourceRecord`); `s.findApp`, `s.loadApp`, `s.workspace`, `s.audit`, `abort`, `requestScheme`, `mergeStringMaps`, `project.SlugOf`, `newRateLimiter`, `s.mutateApp`.
- Produces (JSON for the CLI and the dashboard):
  ```go
  type GitHooksView struct {
  	URL    string `json:"url"`
  	Secret string `json:"secret"`
  }
  ```
  Routes:
  - `POST /api/hooks/git/:slug/:token` (public, tenant from Host) → 202 / 200 `pong` / 400 / 401 / 413 / 429
  - `GET /api/projects/:slug/git-hooks` → `GitHooksView` (ProjectConfig; makes the Secret on first use)
  - `POST /api/projects/:slug/git-hooks/rotate` → `GitHooksView` (ProjectConfig; audit `git-hooks.rotate`)
  - `PATCH /api/projects/:slug` accepts `{"autoDeploy": bool}`
  - `AppDetail.Status.Source *shpyrdv1.SourceStatus` (`status.source`); `spec.source.git.autoDeploy` comes with `Spec.Source`

- [ ] **Step 1: Write the failing tests**

`pkg/api/githooks_test.go`:

```go
package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
)

const hookCommit = "89abcdef0123456789abcdef0123456789abcdef"

func githubPush(ref string) []byte {
	return []byte(`{"ref":"` + ref + `","before":"0123456789abcdef0123456789abcdef01234567","after":"` + hookCommit +
		`","head_commit":{"id":"` + hookCommit + `","message":"fix login\n\nThe redirect lost the session.","author":{"name":"Alice","username":"alice"}}}`)
}

func hmacHex(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func githubHeaders(secret, event, delivery string, body []byte) map[string]string {
	return map[string]string{"X-GitHub-Event": event, "X-GitHub-Delivery": delivery, "X-Hub-Signature-256": "sha256=" + hmacHex(secret, body)}
}

// deliver posts body to a project's webhook, as a git host does.
func deliver(t *testing.T, s *Server, slug, token string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/hooks/git/"+slug+"/"+token, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// hookOf sets up the project's webhook and returns its token and secret.
func hookOf(t *testing.T, s *Server, slug string) (string, string) {
	t.Helper()
	rec := do(t, s, "GET", "/api/projects/"+slug+"/git-hooks", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("git-hooks: %d %s", rec.Code, rec.Body.String())
	}
	var v GitHooksView
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(v.URL, "http://example.com/api/hooks/git/"+slug+"/") || len(path.Base(v.URL)) != 64 || len(v.Secret) != 64 {
		t.Fatalf("view = %+v", v)
	}
	return path.Base(v.URL), v.Secret
}

func appNamed(t *testing.T, cr client.Client, slug string) *shpyrdv1.App {
	t.Helper()
	app := &shpyrdv1.App{}
	if err := cr.Get(context.Background(), types.NamespacedName{Namespace: "app-" + slug, Name: slug}, app); err != nil {
		t.Fatal(err)
	}
	return app
}

func TestGitHooksKeepTheirURLUntilRotated(t *testing.T) {
	s, _ := newTestServer(t, nil, []client.Object{sampleApp("web1", shpyrdv1.PhaseRunning)})
	token, secret := hookOf(t, s, "web1")
	if again, _ := hookOf(t, s, "web1"); again != token {
		t.Error("the URL must not change by itself")
	}
	rec := do(t, s, "POST", "/api/projects/web1/git-hooks/rotate", "", true)
	var v GitHooksView
	_ = json.Unmarshal(rec.Body.Bytes(), &v)
	if rec.Code != http.StatusOK || path.Base(v.URL) == token || v.Secret == secret {
		t.Fatalf("rotate: %d %+v", rec.Code, v)
	}
	body := githubPush("refs/heads/main")
	if rec := deliver(t, s, "web1", token, body, githubHeaders(v.Secret, "push", "d-0", body)); rec.Code != http.StatusUnauthorized {
		t.Errorf("the old URL after a rotation: %d", rec.Code)
	}
	if rec := do(t, s, "GET", "/api/projects/web1/git-hooks", "", false); rec.Code != http.StatusUnauthorized {
		t.Errorf("the secret needs a session: %d", rec.Code)
	}
}

func TestGitHookAcceptsASignedGitHubPush(t *testing.T) {
	s, cr := newTestServer(t, nil, []client.Object{sampleApp("web1", shpyrdv1.PhaseRunning)})
	token, secret := hookOf(t, s, "web1")
	body := githubPush("refs/heads/main")
	if rec := deliver(t, s, "web1", token, body, githubHeaders(secret, "push", "d-1", body)); rec.Code != http.StatusAccepted {
		t.Fatalf("push: %d %s", rec.Code, rec.Body.String())
	}
	p, err := gitauth.ReadPush(appNamed(t, cr, "web1").Annotations[shpyrdv1.AnnotationGitPush])
	if err != nil || p == nil || p.SHA != hookCommit || p.Ref != "refs/heads/main" || p.Author != "alice" || p.Delivery != "d-1" || p.At.IsZero() {
		t.Fatalf("push = %+v, %v", p, err)
	}
	// The watcher took the push; the host delivers it again: ignored.
	app := appNamed(t, cr, "web1")
	delete(app.Annotations, shpyrdv1.AnnotationGitPush)
	app.Annotations[shpyrdv1.AnnotationGitSource] = `{"ref":"main","delivery":"d-1"}`
	if err := cr.Update(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	if rec := deliver(t, s, "web1", token, body, githubHeaders(secret, "push", "d-1", body)); rec.Code != http.StatusAccepted {
		t.Fatalf("again: %d", rec.Code)
	}
	if appNamed(t, cr, "web1").Annotations[shpyrdv1.AnnotationGitPush] != "" {
		t.Error("a repeated delivery is not applied twice")
	}
}

func TestGitHookRefusesAWrongTokenOrSignature(t *testing.T) {
	s, _ := newTestServer(t, nil, []client.Object{sampleApp("web1", shpyrdv1.PhaseRunning), sampleApp("web2", shpyrdv1.PhaseRunning)})
	token, secret := hookOf(t, s, "web1")
	body := githubPush("refs/heads/main")
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"wrong token":     deliver(t, s, "web1", strings.Repeat("0", 64), body, githubHeaders(secret, "push", "d", body)),
		"wrong signature": deliver(t, s, "web1", token, body, githubHeaders("not-the-secret", "push", "d", body)),
		"unsigned":        deliver(t, s, "web1", token, body, map[string]string{"X-GitHub-Event": "push"}),
		"no webhook yet":  deliver(t, s, "web2", token, body, githubHeaders(secret, "push", "d", body)),
	} {
		if rec.Code != http.StatusUnauthorized || strings.Contains(rec.Body.String(), secret) {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
}

func TestGitHookReadsGitLabAndGitea(t *testing.T) {
	s, cr := newTestServer(t, nil, []client.Object{sampleApp("web1", shpyrdv1.PhaseRunning)})
	token, secret := hookOf(t, s, "web1")
	gitlab := []byte(`{"object_kind":"push","ref":"refs/heads/main","after":"` + hookCommit + `","checkout_sha":"` + hookCommit +
		`","user_username":"bruno","commits":[{"id":"` + hookCommit + `","message":"bump deps","author":{"name":"Bruno"}}]}`)
	if rec := deliver(t, s, "web1", token, gitlab, map[string]string{"X-Gitlab-Event": "Push Hook", "X-Gitlab-Token": secret, "X-Gitlab-Event-UUID": "g-1"}); rec.Code != http.StatusAccepted {
		t.Fatalf("gitlab: %d %s", rec.Code, rec.Body.String())
	}
	if p, _ := gitauth.ReadPush(appNamed(t, cr, "web1").Annotations[shpyrdv1.AnnotationGitPush]); p == nil || p.Author != "Bruno" || p.Message != "bump deps" || p.Delivery != "g-1" {
		t.Errorf("gitlab push = %+v", p)
	}
	gitea := githubPush("refs/heads/main")
	if rec := deliver(t, s, "web1", token, gitea, map[string]string{"X-Gitea-Event": "push", "X-Gitea-Delivery": "t-1", "X-Gitea-Signature": hmacHex(secret, gitea)}); rec.Code != http.StatusAccepted {
		t.Fatalf("gitea: %d %s", rec.Code, rec.Body.String())
	}
	if p, _ := gitauth.ReadPush(appNamed(t, cr, "web1").Annotations[shpyrdv1.AnnotationGitPush]); p == nil || p.Delivery != "t-1" {
		t.Errorf("gitea push = %+v", p)
	}
}

func TestGitHookIgnoresWhatIsNotAPushToTheBranch(t *testing.T) {
	s, cr := newTestServer(t, nil, []client.Object{sampleApp("web1", shpyrdv1.PhaseRunning)})
	token, secret := hookOf(t, s, "web1")
	ping := []byte(`{"zen":"Keep it logically awesome."}`)
	if rec := deliver(t, s, "web1", token, ping, githubHeaders(secret, "ping", "p", ping)); rec.Code != http.StatusOK || rec.Body.String() != "pong" {
		t.Errorf("ping: %d %q", rec.Code, rec.Body.String())
	}
	issue := []byte(`{"action":"opened"}`)
	other := githubPush("refs/heads/dev")
	deleted := []byte(`{"ref":"refs/heads/main","after":"0000000000000000000000000000000000000000"}`)
	tag := githubPush("refs/tags/v1.2")
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"an issue":           deliver(t, s, "web1", token, issue, githubHeaders(secret, "issues", "i", issue)),
		"another branch":     deliver(t, s, "web1", token, other, githubHeaders(secret, "push", "o", other)),
		"a deleted branch":   deliver(t, s, "web1", token, deleted, githubHeaders(secret, "push", "x", deleted)),
		"a tag":              deliver(t, s, "web1", token, tag, githubHeaders(secret, "push", "v", tag)),
		"an unknown project": deliver(t, s, "nope", token, other, githubHeaders(secret, "push", "n", other)),
	} {
		if rec.Code != http.StatusAccepted {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
	if appNamed(t, cr, "web1").Annotations[shpyrdv1.AnnotationGitPush] != "" {
		t.Error("none of these is a push to deploy")
	}
	garbage := []byte(`{"ref":`)
	if rec := deliver(t, s, "web1", token, garbage, githubHeaders(secret, "push", "g", garbage)); rec.Code != http.StatusBadRequest {
		t.Errorf("an unreadable push: %d", rec.Code)
	}
}

func TestGitHookIsRateLimitedAndCapped(t *testing.T) {
	s, _ := newTestServer(t, nil, []client.Object{sampleApp("web1", shpyrdv1.PhaseRunning)})
	token, secret := hookOf(t, s, "web1")
	big := bytes.Repeat([]byte("x"), hookBodyLimit+1)
	if rec := deliver(t, s, "web1", token, big, githubHeaders(secret, "push", "b", big)); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("1 MiB and more: %d", rec.Code)
	}
	s.gitHookDeliveries = newRateLimiter(1)
	body := githubPush("refs/heads/main")
	deliver(t, s, "web1", token, body, githubHeaders(secret, "push", "r-1", body))
	if rec := deliver(t, s, "web1", token, body, githubHeaders(secret, "push", "r-2", body)); rec.Code != http.StatusTooManyRequests {
		t.Errorf("past the limit: %d", rec.Code)
	}
}

func TestAGitDeployKeepsAutoDeployAndReadsTheBranchAgain(t *testing.T) {
	app := sampleApp("web1", shpyrdv1.PhaseRunning)
	off := false
	app.Spec.Source.Git.AutoDeploy = &off
	app.Annotations = map[string]string{shpyrdv1.AnnotationGitCommit: hookCommit, shpyrdv1.AnnotationGitCommitRef: "main"}
	s, cr := newTestServer(t, nil, []client.Object{app})
	if rec := do(t, s, "POST", "/api/projects/web1/deploy", `{"git":{"url":"https://example.test/web1","revision":"main"}}`, true); rec.Code != http.StatusAccepted {
		t.Fatalf("deploy: %d %s", rec.Code, rec.Body.String())
	}
	got := appNamed(t, cr, "web1")
	if got.Annotations[shpyrdv1.AnnotationGitCommit] != "" || got.Annotations[shpyrdv1.AnnotationGitCommitRef] != "" {
		t.Errorf("a deploy reads the branch again: %v", got.Annotations)
	}
	if got.Spec.Source.Git.AutoDeploy == nil || *got.Spec.Source.Git.AutoDeploy {
		t.Error("a deploy that does not say keeps auto-deploy off")
	}
	if rec := do(t, s, "POST", "/api/projects/web1/deploy", `{"git":{"url":"https://example.test/web1","autoDeploy":true}}`, true); rec.Code != http.StatusAccepted {
		t.Fatalf("deploy: %d", rec.Code)
	}
	if got := appNamed(t, cr, "web1"); got.Spec.Source.Git.AutoDeploy == nil || !*got.Spec.Source.Git.AutoDeploy {
		t.Error("--auto-deploy switches it back on")
	}
}

func TestAutoDeployIsAProjectSetting(t *testing.T) {
	app := sampleApp("web1", shpyrdv1.PhaseRunning)
	app.Status.Source = &shpyrdv1.SourceStatus{Commit: hookCommit, Latest: hookCommit, Author: "alice", Via: "push"}
	site := sampleApp("site", shpyrdv1.PhaseRunning)
	site.Spec.Source = &shpyrdv1.Source{Blob: &shpyrdv1.BlobSource{URL: "http://shpyrd-server/api/sources/ab.tgz", SHA256: "ab"}}
	s, cr := newTestServer(t, nil, []client.Object{app, site})
	rec := do(t, s, "PATCH", "/api/projects/web1", `{"autoDeploy":false}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}
	var d AppDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if d.Spec.Source == nil || d.Spec.Source.Git.AutoDeploy == nil || *d.Spec.Source.Git.AutoDeploy {
		t.Errorf("spec.source = %+v", d.Spec.Source)
	}
	if d.Status.Source == nil || d.Status.Source.Author != "alice" || d.Status.Source.Commit != hookCommit {
		t.Errorf("status.source = %+v", d.Status.Source)
	}
	if got := appNamed(t, cr, "web1"); *got.Spec.Source.Git.AutoDeploy {
		t.Error("not stored")
	}
	if rec := do(t, s, "PATCH", "/api/projects/site", `{"autoDeploy":true}`, true); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "git repository") {
		t.Errorf("an archive has no branch: %d %s", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./pkg/api/ -run 'GitHook|AutoDeploy|AGitDeploy'`
Expected: FAIL to compile: `undefined: GitHooksView`, `hookBodyLimit`, `s.gitHookDeliveries`.

- [ ] **Step 3: Write the implementation**

`pkg/api/githooks.go`:

```go
package api

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
	"github.com/shpyrd-io/shpyrd/pkg/project"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// Deploy on push (RFC-0018): a project's webhook, which GitHub, GitLab or
// Gitea call on a push, and its URL and secret. The webhook only leaves
// the push on the App; the source watcher decides what it deploys.

// GitHooksView is a project's webhook: the URL to paste into the git host,
// and the secret it signs deliveries with.
type GitHooksView struct {
	URL    string `json:"url"`
	Secret string `json:"secret"`
}

const (
	// hookBodyLimit caps a delivery: a push of many commits stays far below.
	hookBodyLimit = 1 << 20
	// gitHooksPerMinute is how many deliveries one address may make: a busy
	// organisation's pushes all come from a few of its host's addresses.
	gitHooksPerMinute = 300
	// hookRefusalsPerMinute is how many refused deliveries are logged per
	// project.
	hookRefusalsPerMinute = 6
)

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// projectHookSecret is the project's webhook Secret, made on first use;
// rotate replaces its token and secret.
func (s *Server) projectHookSecret(ctx context.Context, app *shpyrdv1.App, rotate bool) (*corev1.Secret, error) {
	key := types.NamespacedName{Namespace: app.Namespace, Name: gitauth.HookSecretName}
	sec := &corev1.Secret{}
	err := s.apps.Get(ctx, key, sec)
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, err
	}
	exists := err == nil
	if exists && !rotate && len(sec.Data[gitauth.HookKeyToken]) > 0 && len(sec.Data[gitauth.HookKeySecret]) > 0 {
		return sec, nil
	}
	token, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	secret, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	if !exists {
		sec.ObjectMeta = metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace}
	}
	sec.Labels = mergeStringMaps(sec.Labels, map[string]string{shpyrdv1.LabelManagedBy: "shpyrd", shpyrdv1.LabelProject: app.Name})
	sec.Type = corev1.SecretTypeOpaque
	sec.Data = map[string][]byte{gitauth.HookKeyToken: []byte(token), gitauth.HookKeySecret: []byte(secret)}
	if exists {
		if err := s.apps.Update(ctx, sec); err != nil {
			return nil, err
		}
		return sec, nil
	}
	if err := s.apps.Create(ctx, sec); apierrors.IsAlreadyExists(err) {
		// Made by a request at the same moment: that one is the webhook.
		if err := s.apps.Get(ctx, key, sec); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	return sec, nil
}

// hookURL is the webhook's address at the door the request came through,
// HTTPS when the platform is.
func (s *Server) hookURL(c *gin.Context, slug, token string) string {
	scheme := requestScheme(c)
	if scheme == "http" && strings.HasPrefix(s.opts.Public.DashboardURL, "https://") {
		scheme = "https"
	}
	return scheme + "://" + c.Request.Host + "/api/hooks/git/" + url.PathEscape(slug) + "/" + token
}

func (s *Server) showGitHooks(c *gin.Context)   { s.gitHooks(c, false) }
func (s *Server) rotateGitHooks(c *gin.Context) { s.gitHooks(c, true) }

func (s *Server) gitHooks(c *gin.Context, rotate bool) {
	app, ok := s.loadApp(c)
	if !ok {
		return
	}
	sec, err := s.projectHookSecret(c.Request.Context(), app, rotate)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	slug := project.SlugOf(app)
	if rotate {
		s.audit(c, slug, "git-hooks.rotate", "webhook", "a new webhook URL and secret; the old ones are refused")
	}
	c.JSON(http.StatusOK, GitHooksView{
		URL:    s.hookURL(c, slug, string(sec.Data[gitauth.HookKeyToken])),
		Secret: string(sec.Data[gitauth.HookKeySecret]),
	})
}

// gitHook is POST /api/hooks/git/:slug/:token. The tenant comes from the
// Host. Every answer that is not a refusal is 2xx, so hosts do not retry
// what will never apply.
func (s *Server) gitHook(c *gin.Context) {
	if !s.gitHookDeliveries.allow(c.ClientIP()) {
		c.Header("Retry-After", "60")
		abort(c, http.StatusTooManyRequests, errors.New("too many webhook deliveries from this address; shpyrd checks the branch by itself meanwhile"))
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, hookBodyLimit))
	if err != nil {
		abort(c, http.StatusRequestEntityTooLarge, errors.New("the delivery is larger than shpyrd reads (1 MiB)"))
		return
	}
	ctx := c.Request.Context()
	app, err := s.findApp(ctx, s.workspace(c), c.Param("slug"))
	if errors.Is(err, store.ErrNotFound) || apierrors.IsNotFound(err) {
		c.JSON(http.StatusAccepted, gin.H{"ignored": "no such project"})
		return
	}
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	sec := &corev1.Secret{}
	err = s.apps.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: gitauth.HookSecretName}, sec)
	if err != nil && !apierrors.IsNotFound(err) {
		abort(c, http.StatusBadGateway, err)
		return
	}
	if err != nil || subtle.ConstantTimeCompare([]byte(c.Param("token")), sec.Data[gitauth.HookKeyToken]) != 1 ||
		gitauth.VerifyDelivery(c.Request.Header, body, string(sec.Data[gitauth.HookKeySecret])) != nil {
		s.refuseHook(c, project.SlugOf(app))
		return
	}
	hook := gitauth.HookOf(c.Request.Header)
	switch hook.Event {
	case "ping":
		c.String(http.StatusOK, "pong")
		return
	case "push":
	default:
		c.JSON(http.StatusAccepted, gin.H{"ignored": "not a push"})
		return
	}
	push, err := gitauth.ParsePush(body)
	if err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	push.Delivery, push.At = hook.Delivery, time.Now().UTC()
	if why := ignoredPush(app, push); why != "" {
		c.JSON(http.StatusAccepted, gin.H{"ignored": why})
		return
	}
	raw, _ := json.Marshal(push)
	patch, _ := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]string{shpyrdv1.AnnotationGitPush: string(raw)}}})
	if err := s.apps.Patch(ctx, app.DeepCopy(), client.RawPatch(types.MergePatchType, patch)); err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"accepted": push.SHA})
}

// ignoredPush says why a push is not for the project's branch; "" when it
// is. Each write is an App event for two controllers: none for nothing.
func ignoredPush(app *shpyrdv1.App, p gitauth.Push) string {
	switch {
	case p.Deleted:
		return "the branch was deleted; what runs keeps running"
	case !app.WatchesGit():
		return "the project does not follow a git branch"
	case p.Ref != "refs/heads/"+app.GitRevision():
		return "a push to another branch"
	case p.Delivery != "" && seenDelivery(app, p.Delivery):
		return "delivered before"
	}
	return ""
}

// seenDelivery says the host delivers again what was applied: the push
// waiting for the watcher, or the last one it took.
func seenDelivery(app *shpyrdv1.App, id string) bool {
	if last, _ := gitauth.ReadPush(app.Annotations[shpyrdv1.AnnotationGitPush]); last != nil && last.Delivery == id {
		return true
	}
	return gitauth.ReadSourceRecord(app.Annotations[shpyrdv1.AnnotationGitSource]).Delivery == id
}

// refuseHook answers a delivery whose URL or signature is not the
// project's, and says so in the log a few times a minute at most.
func (s *Server) refuseHook(c *gin.Context, slug string) {
	if s.hookRefusals.allow(slug) {
		s.log.Warn("git webhook refused: wrong URL or signature", "project", slug, "from", c.ClientIP())
	}
	abort(c, http.StatusUnauthorized, errors.New("this webhook's URL or signature is not the project's: copy them again from shpyrd git-hooks show"))
}
```

`pkg/api/server.go`, in the `Server` struct after `deployKeyMints *rateLimiter`:

```go
	// gitHookDeliveries throttles webhook deliveries per address, and
	// hookRefusals the log lines of refused ones per project (RFC-0018).
	gitHookDeliveries *rateLimiter
	hookRefusals      *rateLimiter
```

after `s.deployKeyMints = newRateLimiter(deployKeysPerMinute)`:

```go
	s.gitHookDeliveries = newRateLimiter(gitHooksPerMinute)
	s.hookRefusals = newRateLimiter(hookRefusalsPerMinute)
```

in the `pub` group, after `pub.GET("/sources/:name", s.serveSource)`:

```go
	// Git hosts deliver pushes here (RFC-0018): no session; the URL's
	// token and the delivery's signature are the credential.
	pub.POST("/hooks/git/:slug/:token", s.gitHook)
```

after the project `git-auth` routes:

```go
	api.GET("/projects/:slug/git-hooks", s.require(authz.ProjectConfig), s.showGitHooks) // RFC-0018
	api.POST("/projects/:slug/git-hooks/rotate", s.require(authz.ProjectConfig), s.rotateGitHooks)
```

`pkg/api/apps.go`:

In `AppDetailStatus`, after `Release`:

```go
	// Source is what the source watcher knows of the branch and the
	// commit pinned for it (RFC-0018).
	Source *shpyrdv1.SourceStatus `json:"source,omitempty"`
```

and in `detail`, in the `Status: AppDetailStatus{...}` literal, after
`Domains:    a.Status.Domains,`:

```go
			Source:     a.Status.Source,
```

`UpdateAppRequest` gains, after `IconColor`:

```go
	// AutoDeploy switches whether a push to the project's branch deploys
	// (spec.source.git.autoDeploy, RFC-0018).
	AutoDeploy *bool `json:"autoDeploy,omitempty"`
```

In `updateApp`, the "nothing to update" check:

```go
	if req.Name == nil && req.Description == nil && req.Featured == nil && req.Icon == nil && req.IconColor == nil && req.AutoDeploy == nil {
		abort(c, http.StatusBadRequest, errors.New("nothing to update: give name, description, featured, icon, iconColor or autoDeploy"))
		return
	}
```

in its `mutateApp` closure, after the `IconColor` block:

```go
		if req.AutoDeploy != nil {
			if a.Spec.Source == nil || a.Spec.Source.Git == nil {
				return errors.New("the project does not deploy from a git repository: auto-deploy follows a git branch")
			}
			on := *req.AutoDeploy
			a.Spec.Source.Git.AutoDeploy = &on
			changes = append(changes, fmt.Sprintf("auto-deploy %v", on))
		}
```

and the rename check after it:

```go
	if req.Name != nil && req.Description == nil && req.Featured == nil && req.Icon == nil && req.IconColor == nil && req.AutoDeploy == nil {
		action = "project.rename"
	}
```

In `deployApp`, the `case hasGit:` branch becomes:

```go
		case hasGit:
			a.Spec.Image = ""
			if req.Git.Revision == "" {
				req.Git.Revision = "main"
			}
			// Auto-deploy is the project's setting: a deploy that does not
			// say keeps it (RFC-0018).
			if req.Git.AutoDeploy == nil && a.Spec.Source != nil && a.Spec.Source.Git != nil {
				req.Git.AutoDeploy = a.Spec.Source.Git.AutoDeploy
			}
			a.Spec.Source = &shpyrdv1.Source{Git: req.Git, SubPath: req.SubPath}
			// The source watcher reads the branch's commit again at once.
			delete(a.Annotations, shpyrdv1.AnnotationGitCommit)
			delete(a.Annotations, shpyrdv1.AnnotationGitCommitRef)
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./pkg/api/`
Expected: PASS, the existing tests included (the deploy and update tests
send no `autoDeploy`).

- [ ] **Step 5: Commit**

```bash
git add pkg/api/githooks.go pkg/api/githooks_test.go pkg/api/server.go pkg/api/apps.go
git commit -m "feat(api): the generic git webhook, project hook secrets, the auto-deploy setting (RFC-0018)

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 7: The CLI: `shpyrd git-hooks` and the deploy flags

**Files:**
- Create: `internal/cli/githooks.go`
- Modify: `internal/cli/gitauth.go:189-215` (`gitAuthRequest` over a shared `signedInRequest`), `internal/cli/deploy.go` (flags 26-36 and 299-310, the help at ~45, the start of `RunE` ~66, the `--git` case 196-199), `internal/cli/root.go:131`
- Test: `internal/cli/githooks_test.go`

**Interfaces:**
- Consumes: Task 6 (`api.GitHooksView`, the two routes, `api.AppDetail.Spec.Source.Git.AutoDeploy`), Task 1 (`shpyrdv1.IsCommit`, `GitSource.AutoDeploy`); `resolveAppName`, `appFlag`, `newAppClient`, `ac.serverRequest`, `g.print`, `firstNonEmpty`, `truncate`.
- Produces:
  - `func newGitHooksCmd(g *globalFlags) *cobra.Command`
  - `func signedInRequest(g *globalFlags, cmd *cobra.Command, refusal, method, path string, body []byte, out any) error`
  - `func gitDeploySource(url, ref string, on, off bool) (*shpyrdv1.GitSource, error)`
  - flags `shpyrd deploy --auto-deploy`, `--no-auto-deploy`

- [ ] **Step 1: Write the failing tests**

`internal/cli/githooks_test.go`:

```go
package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// runShpyrd runs the CLI against handler, signed in.
func runShpyrd(t *testing.T, handler http.HandlerFunc, args ...string) (string, error) {
	t.Helper()
	srv := httptest.NewServer(handler)
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KUBECONFIG", "")
	t.Setenv("SHPYRD_URL", srv.URL)
	t.Setenv("SHPYRD_TOKEN", "test-token")
	root := New()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestGitHooksShowPrintsTheWebhookAndHowToAddIt(t *testing.T) {
	out, err := runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/projects/shop/git-hooks":
			_, _ = w.Write([]byte(`{"url":"https://acme.shpyrd.app/api/hooks/git/shop/t0k3n","secret":"s3cr3t"}`))
		case "GET /api/projects/shop":
			_, _ = w.Write([]byte(`{"slug":"shop","spec":{"source":{"git":{"url":"https://github.com/acme/shop","revision":"main","autoDeploy":false}}},"status":{"phase":"Running","releases":[]}}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}, "git-hooks", "show", "--project", "shop")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"https://acme.shpyrd.app/api/hooks/git/shop/t0k3n", "s3cr3t",
		"GitHub", "application/json", "Just the push event", "GitLab", "Secret token", "Gitea", "Push events",
		"Auto-deploy is off", "--auto-deploy",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestGitHooksShowSaysAutoDeployIsOn(t *testing.T) {
	out, err := runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/git-hooks") {
			_, _ = w.Write([]byte(`{"url":"https://acme.shpyrd.app/api/hooks/git/shop/t0k3n","secret":"s3cr3t"}`))
			return
		}
		_, _ = w.Write([]byte(`{"slug":"shop","spec":{"source":{"git":{"url":"https://github.com/acme/shop"}}},"status":{"phase":"Running","releases":[]}}`))
	}, "git-hooks", "show", "--project", "shop")
	if err != nil || !strings.Contains(out, "Auto-deploy is on: every push to main deploys") {
		t.Errorf("%v\n%s", err, out)
	}
}

func TestGitHooksRotate(t *testing.T) {
	out, err := runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/projects/shop/git-hooks/rotate" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"url":"https://acme.shpyrd.app/api/hooks/git/shop/n3w","secret":"n3ws3cr3t"}`))
	}, "git-hooks", "rotate", "--project", "shop")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/shop/n3w", "n3ws3cr3t", "refused"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestGitDeploySource(t *testing.T) {
	src, err := gitDeploySource("https://github.com/acme/shop", "", false, false)
	if err != nil || src.Revision != "main" || src.AutoDeploy != nil {
		t.Errorf("no flag leaves the project's setting: %+v %v", src, err)
	}
	if src, _ := gitDeploySource("https://github.com/acme/shop", "dev", true, false); src.Revision != "dev" || src.AutoDeploy == nil || !*src.AutoDeploy {
		t.Errorf("--auto-deploy: %+v", src)
	}
	if src, _ := gitDeploySource("https://github.com/acme/shop", "", false, true); src.AutoDeploy == nil || *src.AutoDeploy {
		t.Errorf("--no-auto-deploy: %+v", src)
	}
	if _, err := gitDeploySource("https://github.com/acme/shop", "", true, true); err == nil {
		t.Error("both flags contradict each other")
	}
}

func TestAutoDeployFlagsGoWithGit(t *testing.T) {
	_, err := runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("nothing is sent: %s %s", r.Method, r.URL.Path)
	}, "deploy", "--project", "shop", "--no-auto-deploy")
	if err == nil || !strings.Contains(err.Error(), "go with --git") {
		t.Errorf("err = %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/ -run 'GitHooks|GitDeploySource|AutoDeployFlags'`
Expected: FAIL to compile: `undefined: gitDeploySource`; once it compiles,
`unknown command "git-hooks"`.

- [ ] **Step 3: Write the implementation**

`internal/cli/gitauth.go`, replace `gitAuthRequest` with:

```go
// gitAuthRequest calls the API signed in; git credentials are not managed
// over a kubeconfig.
func gitAuthRequest(g *globalFlags, cmd *cobra.Command, method, path string, body []byte, out any) error {
	return signedInRequest(g, cmd, "git credentials are managed signed in: run shpyrd login first", method, path, body, out)
}

// signedInRequest calls the API with the signed-in session; over a
// kubeconfig it refuses with refusal.
func signedInRequest(g *globalFlags, cmd *cobra.Command, refusal, method, path string, body []byte, out any) error {
	ctx := signalContext()
	ac, err := newAppClient(g, g.progress(cmd))
	if err != nil {
		return err
	}
	if !ac.session {
		return errors.New(refusal)
	}
	ct := ""
	if body != nil {
		ct = "application/json"
	}
	raw, err := ac.serverRequest(ctx, method, path, body, ct)
	if err != nil {
		return err
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("unexpected response: %s", truncate(string(raw), 200))
	}
	return nil
}
```

`internal/cli/githooks.go`:

```go
package cli

import (
	"fmt"
	"io"
	"net/url"

	"github.com/spf13/cobra"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/api"
)

// Deploy on push (RFC-0018): `shpyrd git-hooks`, the webhook a git host
// calls when the project's branch moves. Signed in only.

func newGitHooksCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "git-hooks",
		Short: "Deploy on push: the webhook a git host calls",
		Long: `A project that deploys from a git branch deploys every push to it. shpyrd
checks the branch every few minutes by itself; a webhook makes it seconds.

  shpyrd git-hooks show --project shop      the webhook's URL and secret, and how to add them
  shpyrd git-hooks rotate --project shop    a new URL and secret; the old ones are refused

A commit whose message says [skip deploy] is recorded, not deployed. To record
pushes without deploying them: shpyrd deploy --git <url> --no-auto-deploy.`,
	}
	var showApp, rotateApp string
	show := &cobra.Command{
		Use:   "show [--project <name>]",
		Short: "Print the webhook's URL and secret, and how to add them",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := resolveAppName(showApp)
			if err != nil {
				return err
			}
			var v api.GitHooksView
			if err := gitHooksRequest(g, cmd, "GET", "api/projects/"+url.PathEscape(name)+"/git-hooks", &v); err != nil {
				return err
			}
			var d api.AppDetail
			if err := gitHooksRequest(g, cmd, "GET", "api/projects/"+url.PathEscape(name), &d); err != nil {
				return err
			}
			return g.print(cmd, v, func(w io.Writer) { printGitHooks(w, name, v, d) })
		},
	}
	appFlag(show, &showApp)
	rotate := &cobra.Command{
		Use:   "rotate [--project <name>]",
		Short: "Make a new webhook URL and secret; the old ones stop working",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := resolveAppName(rotateApp)
			if err != nil {
				return err
			}
			var v api.GitHooksView
			if err := gitHooksRequest(g, cmd, "POST", "api/projects/"+url.PathEscape(name)+"/git-hooks/rotate", &v); err != nil {
				return err
			}
			return g.print(cmd, v, func(w io.Writer) {
				fmt.Fprintf(w, "New webhook for %s. Change it on the git host: the old URL and secret are refused from now on.\n\n  URL     %s\n  Secret  %s\n", name, v.URL, v.Secret)
			})
		},
	}
	appFlag(rotate, &rotateApp)
	cmd.AddCommand(show, rotate)
	return cmd
}

func gitHooksRequest(g *globalFlags, cmd *cobra.Command, method, path string, out any) error {
	return signedInRequest(g, cmd, "webhooks are managed signed in: run shpyrd login first", method, path, nil, out)
}

// printGitHooks says where the webhook goes on each host, and what a push
// does now.
func printGitHooks(w io.Writer, name string, v api.GitHooksView, d api.AppDetail) {
	fmt.Fprintf(w, "Deploy %s on push: add this webhook to its repository.\n\n  URL     %s\n  Secret  %s\n\n", name, v.URL, v.Secret)
	fmt.Fprintln(w, `GitHub: the repository's Settings, Webhooks, Add webhook. Payload URL: the URL; Content type: application/json; Secret: the secret; "Just the push event".`)
	fmt.Fprintln(w, "GitLab: the project's Settings, Webhooks, Add new webhook. URL: the URL; Secret token: the secret; Trigger: Push events.")
	fmt.Fprintln(w, "Gitea: the repository's Settings, Webhooks, Add webhook, Gitea. Target URL: the URL; Content type: application/json; Secret: the secret; Trigger on: Push events.")
	fmt.Fprintln(w)
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

`internal/cli/root.go`, after `root.AddCommand(newGitAuthCmd(g))`:

```go
	root.AddCommand(newGitHooksCmd(g))
```

`internal/cli/deploy.go`:

In the `var (...)` block of `newDeployCmd`, after `save bool`:

```go
		autoDeploy   bool
		noAutoDeploy bool
```

In the long help, the `--git` line becomes:

```
  shpyrd deploy --git https://github.com/o/r  build from a Git URL; every push to the branch deploys (--no-auto-deploy records them only)
```

At the very start of `RunE`, before `ctx := signalContext()`:

```go
			if gitURL == "" && (autoDeploy || noAutoDeploy) {
				return errors.New("--auto-deploy and --no-auto-deploy go with --git")
			}
			if autoDeploy && noAutoDeploy {
				return errors.New("--auto-deploy and --no-auto-deploy contradict each other")
			}
```

The `case gitURL != "":` branch becomes:

```go
			case gitURL != "":
				src, err := gitDeploySource(gitURL, gitRef, autoDeploy, noAutoDeploy)
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "==> Deploying %s @ %s to %s\n", gitURL, src.Revision, name)
				req.Git = src
```

After the `--save` flag:

```go
	cmd.Flags().BoolVar(&autoDeploy, "auto-deploy", false, "with --git: deploy every push to the branch (the default; switches it back on)")
	cmd.Flags().BoolVar(&noAutoDeploy, "no-auto-deploy", false, "with --git: record pushes to the branch without deploying them")
```

And at the end of the file:

```go
// gitDeploySource is the source `deploy --git` sends. Without a flag
// auto-deploy is left out, and the server keeps the project's setting.
func gitDeploySource(url, ref string, on, off bool) (*shpyrdv1.GitSource, error) {
	if on && off {
		return nil, errors.New("--auto-deploy and --no-auto-deploy contradict each other")
	}
	src := &shpyrdv1.GitSource{URL: url, Revision: firstNonEmpty(ref, "main")}
	if on || off {
		v := on
		src.AutoDeploy = &v
	}
	return src, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/cli/`
Expected: PASS, PR 1's `git-auth` tests included.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/githooks.go internal/cli/githooks_test.go internal/cli/gitauth.go internal/cli/deploy.go internal/cli/root.go
git commit -m "feat(cli): shpyrd git-hooks; deploy --auto-deploy and --no-auto-deploy (RFC-0018)

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 8: The dashboard: the source in the Summary card

The maintainer reviews this in screenshots: build it, then look at it in the
Mock (`npm run design` in `apps/workspace`) before committing. It is not a
design session, so the tests come first as everywhere else; the rules of
`apps/AGENTS.md` and `design/DESIGN_FOR_AGENT.md` still hold: library
components and tokens only, every call through `api`, the Mock answers it.

**Files:**
- Modify: `apps/workspace/src/api/types.ts` (`Project` ~204-247; new types after it)
- Modify: `apps/workspace/src/api/api.ts` (`updateProject` ~141-150; the type import list)
- Modify: `apps/workspace/src/api/backend.ts` (after `updateProject` ~279)
- Modify: `apps/workspace/src/api/mock.ts` (`shape` ~84; `updateProject` ~677-685)
- Modify: `apps/workspace/mock/projects.json` (fixtures, by the script below)
- Create: `apps/workspace/src/screens/project/source.tsx`
- Modify: `apps/workspace/src/screens/project/overview.tsx` (props; the Summary card's content ~73-90), `apps/workspace/src/screens/project/index.tsx:109`
- Test: `apps/workspace/src/screens/project/source.test.tsx`, `apps/workspace/src/api/api.test.ts`

**Interfaces:**
- Consumes: Task 6 (`PATCH /api/projects/:slug {autoDeploy}`, `GET /api/projects/:slug/git-hooks` → `{url, secret}`, `status.source`, `spec.source.git.autoDeploy`).
- Produces:
  - `type SourceStatus`, `type GitHooks = { url: string; secret: string }` in `@/api/types`
  - `Api.updateProject(slug, { ..., autoDeploy?: boolean })`, `Api.projectGitHooks(slug): Promise<GitHooks>`
  - `export function GitSource({ project, perms }: { project: Project; perms: Perms })` in `screens/project/source.tsx`
  - `Overview({ project, perms })`

- [ ] **Step 1: Write the failing tests**

`apps/workspace/src/screens/project/source.test.tsx`:

```tsx
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Project, SourceStatus } from "@/api/types";
import type { Perms } from "@/lib/perms";
import seed from "../../../mock/projects.json";
import { GitSource } from "./source";

const fake = vi.hoisted(() => ({ updateProject: vi.fn(), projectGitHooks: vi.fn() }));
vi.mock("@/api/api", () => ({ api: fake }));
let queries: QueryClient;
afterEach(() => {
  cleanup();
  queries?.clear();
  vi.resetAllMocks();
});

const commit = "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678";
const newer = "b2c3d4e5f60718293a4b5c6d7e8f901234567890";
const minutesAgo = (m: number) => new Date(Date.now() - m * 60_000).toISOString();

function projectWith(
  git: { url: string; revision?: string; autoDeploy?: boolean } | undefined,
  source?: SourceStatus,
): Project {
  const p = structuredClone(seed[0]) as unknown as Project;
  p.spec.source = git ? { git } : { blob: { sha256: "e3b0c442", ref: "local checkout" } };
  p.status.source = source;
  return p;
}

function show(project: Project, perms: Partial<Perms> = { config: true }) {
  queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queries}>
      <GitSource project={project} perms={perms as Perms} />
    </QueryClientProvider>,
  );
}

const repo = { url: "https://github.com/acme/hello-world", revision: "main" };

it("shows the commit that runs, its message and author, and when the branch was last checked", () => {
  show(projectWith(repo, { commit, latest: commit, message: "Fix the login redirect\n\nLonger body", author: "alice", via: "push", checkedAt: minutesAgo(3) }));
  expect(screen.getByText("a1b2c3d")).toBeTruthy();
  expect(screen.getByText("Fix the login redirect")).toBeTruthy();
  expect(screen.getByText("by alice")).toBeTruthy();
  expect(screen.getByText("3m ago · via push")).toBeTruthy();
  expect(screen.getByRole("switch", { name: "Auto-deploy" }).getAttribute("aria-checked")).toBe("true");
});

it("says the branch is being read before a commit is pinned", () => {
  show(projectWith(repo, undefined));
  expect(screen.getByText("Reading the branch's latest commit.")).toBeTruthy();
  expect(screen.getByText("not yet")).toBeTruthy();
});

it("says a skipped commit was not deployed", () => {
  show(projectWith(repo, { commit, latest: newer, skipped: newer, message: "[skip deploy] typo", author: "bruno", via: "push", checkedAt: minutesAgo(1) }));
  expect(screen.getByText("Skipped b2c3d4e: [skip deploy]")).toBeTruthy();
  expect(screen.queryByText("[skip deploy] typo")).toBeNull();
});

it("says the latest commit waits when auto-deploy is off", () => {
  show(projectWith({ ...repo, autoDeploy: false }, { commit, latest: newer, via: "poll", checkedAt: minutesAgo(2) }));
  expect(screen.getByText("Latest on main: b2c3d4e, not deployed: auto-deploy is off.")).toBeTruthy();
  expect(screen.getByRole("switch", { name: "Auto-deploy" }).getAttribute("aria-checked")).toBe("false");
});

it("shows why the branch cannot be read", () => {
  show(projectWith(repo, { commit, error: "shpyrd could not read the repository: it is private.", checkedAt: minutesAgo(1) }));
  expect(screen.getByText("shpyrd could not read the repository: it is private.")).toBeTruthy();
});

it("shows a commit revision as pinned, with nothing to switch", () => {
  show(projectWith({ url: repo.url, revision: "a1b2c3d" }, undefined));
  expect(screen.getByText("Pinned to a commit: a push deploys nothing.")).toBeTruthy();
  expect(screen.queryByRole("switch")).toBeNull();
});

it("renders nothing for a project that does not come from git", () => {
  const { container } = show(projectWith(undefined));
  expect(container.innerHTML).toBe("");
});

it("switches auto-deploy off through the api, and only for those who may change the project", async () => {
  const project = projectWith(repo, { commit, latest: commit, checkedAt: minutesAgo(1) });
  fake.updateProject.mockResolvedValue(projectWith({ ...repo, autoDeploy: false }, project.status.source));
  show(project);
  fireEvent.click(screen.getByRole("switch", { name: "Auto-deploy" }));
  await waitFor(() => expect(fake.updateProject).toHaveBeenCalledWith(project.slug, { autoDeploy: false }));
  cleanup();
  show(project, { config: false });
  expect((screen.getByRole("switch", { name: "Auto-deploy" }) as HTMLButtonElement).disabled).toBe(true);
  expect(screen.queryByRole("button", { name: "Deploy on push" })).toBeNull();
});

it("shows the webhook URL and secret to copy in the dialog", async () => {
  const project = projectWith(repo, { commit, latest: commit, checkedAt: minutesAgo(1) });
  fake.projectGitHooks.mockResolvedValue({ url: "https://acme.shpyrd.app/api/hooks/git/hello-world/t0k3n", secret: "s3cr3t" });
  show(project);
  fireEvent.click(screen.getByRole("button", { name: "Deploy on push" }));
  expect(await screen.findByText("https://acme.shpyrd.app/api/hooks/git/hello-world/t0k3n")).toBeTruthy();
  expect(screen.getByText("s3cr3t")).toBeTruthy();
  expect(screen.getByRole("button", { name: "Copy the secret" })).toBeTruthy();
  expect(fake.projectGitHooks).toHaveBeenCalledWith(project.slug);
});
```

In `apps/workspace/src/api/api.test.ts`, inside the `describe`, add:

```ts
  it("switches auto-deploy of a project on a branch, and refuses it for one that is not", async () => {
    const billing = await api.updateProject("billing", { autoDeploy: true });
    expect(billing.spec.source?.git?.autoDeploy).toBe(true);
    await expect(api.updateProject("dash-tv", { autoDeploy: false })).rejects.toThrow(/git repository/);
    expect((await api.projectGitHooks("billing")).url).toContain("/api/hooks/git/billing/");
  });

  it("knows the source of the projects on a branch: pinned, skipped, recorded only, unreadable", async () => {
    expect((await api.project("hello-world")).status.source?.commit).toMatch(/^[0-9a-f]{40}$/);
    expect((await api.project("docs")).status.source?.skipped).toBeTruthy();
    expect((await api.project("billing")).status.source?.latest).not.toBe((await api.project("billing")).status.source?.commit);
    expect((await api.project("reports")).status.source?.error).toContain("shpyrd git-auth");
  });
```

The two Mock tests share the Mock's kept state and do not depend on their
order: `billing`'s `latest` and `commit` differ whatever auto-deploy says.

`toProject` in `backend.ts` spreads `d.status` and `d.spec`, so
`status.source` and `spec.source.git.autoDeploy` reach the screens with no
change there; only the types learn them.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd apps/workspace && npx vitest run src/screens/project/source.test.tsx src/api/api.test.ts`
Expected: FAIL: `Failed to resolve import "./source"`; the Mock tests fail on
`api.projectGitHooks is not a function` and missing fixtures.

- [ ] **Step 3: Write the implementation**

`apps/workspace/src/api/types.ts`: in `Project`, the `git` of `spec.source`
becomes

```ts
      git?: { url: string; revision?: string; autoDeploy?: boolean };
```

and in `status`, after `release?: {...};`:

```ts
    // What the source watcher knows of the branch (RFC-0018).
    source?: SourceStatus;
```

After `Project`:

```ts
// What the source watcher knows of the branch a project follows
// (RFC-0018): the commit that runs, the newest seen, and how.
export type SourceStatus = {
  commit?: string;
  latest?: string;
  latestAt?: string;
  message?: string;
  author?: string;
  via?: "push" | "poll" | "deploy";
  // A commit whose message asked not to deploy it.
  skipped?: string;
  lastWebhookAt?: string;
  checkedAt?: string;
  // Why the branch cannot be read, in words.
  error?: string;
};

// The webhook a git host calls on a push: its address and its secret.
export type GitHooks = { url: string; secret: string };
```

`apps/workspace/src/api/api.ts`: add `GitHooks` to the type import list, and
in `Api`:

```ts
  updateProject: (
    slug: string,
    body: {
      name?: string;
      description?: string;
      featured?: boolean;
      icon?: string;
      iconColor?: string;
      // Whether a push to the project's branch deploys.
      autoDeploy?: boolean;
    },
  ) => Promise<Project>;
  // The project's webhook for deploy on push: its URL and secret.
  projectGitHooks: (slug: string) => Promise<GitHooks>;
```

`apps/workspace/src/api/backend.ts`, after `updateProject`:

```ts
  projectGitHooks: (slug) => request(`${project(slug)}/git-hooks`),
```

`apps/workspace/src/api/mock.ts`: `const shape = "5";` becomes
`const shape = "6";` (the fixtures changed: browsers drop what they kept).
In `updateProject`, before `return projectsOf.set(p);`:

```ts
    if (body.autoDeploy !== undefined) {
      const git = p.spec.source?.git;
      if (!git)
        throw new ApiError(
          400,
          "the project does not deploy from a git repository: auto-deploy follows a git branch",
        );
      git.autoDeploy = body.autoDeploy;
    }
```

and after `updateProject`:

```ts
  projectGitHooks: async (slug) => {
    await projectsOf.find(slug);
    return {
      url: `https://acme.shpyrd.app/api/hooks/git/${slug}/5e0c2a7b9d4f61385e0c2a7b9d4f61385e0c2a7b9d4f61385e0c2a7b9d4f6138`,
      secret: "9a41d0c7e2b85f369a41d0c7e2b85f369a41d0c7e2b85f369a41d0c7e2b85f36",
    };
  },
```

The fixtures, written into `apps/workspace/mock/projects.json` by a script
so the rest of the file stays as it is. Run from the repository root:

```bash
node -e '
const fs = require("fs");
const file = "apps/workspace/mock/projects.json";
const projects = JSON.parse(fs.readFileSync(file, "utf8"));
const by = (slug) => projects.find((p) => p.slug === slug);
const at = "2026-10-16T09:41:00Z";
const access = "shpyrd could not read the repository: it is private, or does not exist under that name. Give shpyrd access: `shpyrd git-auth deploy-key` prints a key to add to the repository as a read-only deploy key, or a workspace admin sets a token for the host with `shpyrd git-auth set --host github.com --token -`. shpyrd tries again by itself.";
by("hello-world").status.source = { commit: "4f9e2c1a7b3d5e6f8091a2b3c4d5e6f708192a3b", latest: "4f9e2c1a7b3d5e6f8091a2b3c4d5e6f708192a3b", latestAt: at, message: "Fix the login redirect", author: "alice", via: "push", lastWebhookAt: at, checkedAt: at };
by("docs").status.source = { commit: "2b7d9e0f1a2c3b4d5e6f708192a3b4c5d6e7f809", latest: "c31f8a2e9d0b4c5a6f7e8d9c0b1a2f3e4d5c6b7a", latestAt: at, message: "[skip deploy] Fix a typo in the README", author: "bruno", via: "push", skipped: "c31f8a2e9d0b4c5a6f7e8d9c0b1a2f3e4d5c6b7a", lastWebhookAt: at, checkedAt: at };
by("billing").spec.source.git.autoDeploy = false;
by("billing").status.source = { commit: "d4e5f60718293a4b5c6d7e8f9012345678a1b2c3", latest: "e5f60718293a4b5c6d7e8f9012345678a1b2c3d4", latestAt: at, via: "poll", checkedAt: at };
by("reports").status.source = { commit: "f60718293a4b5c6d7e8f9012345678a1b2c3d4e5", error: access, checkedAt: at };
fs.writeFileSync(file, JSON.stringify(projects, null, 2) + "\n");
'
git diff --stat apps/workspace/mock/projects.json
```

Expected: only additions in that file. If the diff reformats other lines,
undo with `git checkout apps/workspace/mock/projects.json` and add the same
four `source` objects (and `billing`'s `"autoDeploy": false`) by hand.

`apps/workspace/src/screens/project/source.tsx`:

```tsx
"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Copy } from "lucide-react";
import { toast } from "sonner";
import { Alert, AlertDescription, AlertTitle } from "@shpyrd/ui/components/alert";
import { Button } from "@shpyrd/ui/components/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@shpyrd/ui/components/dialog";
import { InfoTable, InfoTableItem } from "@shpyrd/ui/components/info-table";
import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { Skeleton } from "@shpyrd/ui/components/skeleton";
import { Switch } from "@shpyrd/ui/components/switch";
import { api } from "@/api/api";
import type { Project, SourceStatus } from "@/api/types";
import type { Perms } from "@/lib/perms";
import { ago } from "@/lib/project";

const isCommit = (rev: string) => /^[0-9a-f]{7,40}$/i.test(rev);
const short = (sha?: string) => sha?.slice(0, 7) ?? "";
const subject = (message?: string) => message?.split("\n")[0].trim() ?? "";

// Where the project's code comes from when it is a git repository: the
// branch, the commit that runs, when shpyrd last looked, and whether a
// push deploys (RFC-0018). Nothing for any other source.
export function GitSource({ project, perms }: { project: Project; perms: Perms }) {
  const git = project.spec.source?.git;
  if (!git) return null;
  const branch = git.revision ?? "main";
  const source = project.status.source;
  const autoDeploy = git.autoDeploy !== false;
  if (isCommit(branch)) {
    return (
      <div className="grid gap-3">
        <InfoTable columns={3}>
          <InfoTableItem label="Repository" mono truncate span={2}>
            {git.url}
          </InfoTableItem>
          <InfoTableItem label="Commit" mono>
            {branch}
          </InfoTableItem>
        </InfoTable>
        <p className="text-xs text-muted-foreground">Pinned to a commit: a push deploys nothing.</p>
      </div>
    );
  }
  const checked = source?.checkedAt
    ? `${ago(source.checkedAt)}${source.via === "push" || source.via === "poll" ? ` · via ${source.via}` : ""}`
    : "not yet";
  return (
    <div className="grid gap-3">
      <InfoTable columns={3}>
        <InfoTableItem label="Repository" mono truncate span={2}>
          {git.url}
        </InfoTableItem>
        <InfoTableItem label="Branch" mono>
          {branch}
        </InfoTableItem>
        <InfoTableItem label="Commit" span={2}>
          <CommitLine source={source} />
        </InfoTableItem>
        <InfoTableItem label="Last checked">{checked}</InfoTableItem>
        <InfoTableItem label="Auto-deploy">
          <AutoDeploySwitch slug={project.slug} on={autoDeploy} allowed={perms.config} />
        </InfoTableItem>
      </InfoTable>
      {source?.skipped && (
        <p className="text-xs text-muted-foreground">Skipped {short(source.skipped)}: [skip deploy]</p>
      )}
      {!autoDeploy && source?.latest && source.latest !== source.commit && (
        <p className="text-xs text-muted-foreground">
          Latest on {branch}: {short(source.latest)}, not deployed: auto-deploy is off.
        </p>
      )}
      {source?.error && (
        <Alert variant="destructive">
          <AlertTitle>shpyrd cannot read the branch</AlertTitle>
          <AlertDescription>{source.error}</AlertDescription>
        </Alert>
      )}
      {perms.config && <DeployOnPush slug={project.slug} />}
    </div>
  );
}

// The commit that runs, with its subject and author when a push said them.
function CommitLine({ source }: { source?: SourceStatus }) {
  if (!source?.commit)
    return <span className="text-muted-foreground">Reading the branch's latest commit.</span>;
  const known = source.latest === source.commit;
  return (
    <span className="flex min-w-0 items-baseline gap-2">
      <span className="shrink-0 font-mono text-xs">{short(source.commit)}</span>
      {known && subject(source.message) && <span className="truncate">{subject(source.message)}</span>}
      {known && source.author && <span className="shrink-0 text-muted-foreground">by {source.author}</span>}
    </span>
  );
}

function AutoDeploySwitch({ slug, on, allowed }: { slug: string; on: boolean; allowed: boolean }) {
  const queries = useQueryClient();
  const set = useMutation({
    mutationFn: (autoDeploy: boolean) => api.updateProject(slug, { autoDeploy }),
    onSuccess: (p) => {
      queries.setQueryData(["project", slug], p);
      toast.success(p.spec.source?.git?.autoDeploy === false ? "Pushes are recorded, not deployed" : "Every push deploys again");
    },
    onError: (e: Error) => toast.error(e.message),
  });
  return (
    <Switch
      size="sm"
      checked={on}
      disabled={!allowed || set.isPending}
      loading={set.isPending}
      onCheckedChange={(v) => set.mutate(v)}
      aria-label="Auto-deploy"
    />
  );
}

// The webhook a git host calls, to copy into it. Its secret is shown only
// to those who may change the project.
function DeployOnPush({ slug }: { slug: string }) {
  const [open, setOpen] = useState(false);
  const hooks = useQuery({ queryKey: ["git-hooks", slug], queryFn: () => api.projectGitHooks(slug), enabled: open });
  const copy = (what: string, text: string) => {
    void navigator.clipboard?.writeText(text);
    toast.success(`${what} copied`);
  };
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant="link" size="sm" className="justify-self-start px-0">
          Deploy on push
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Deploy on push</DialogTitle>
          <DialogDescription>
            Add this webhook to the repository and every push to the branch deploys within seconds. Without it, shpyrd checks the
            branch every few minutes.
          </DialogDescription>
        </DialogHeader>
        {hooks.isLoading ? (
          <Skeleton className="h-24 w-full" />
        ) : hooks.error ? (
          <Alert variant="destructive">
            <AlertDescription>{(hooks.error as Error).message}</AlertDescription>
          </Alert>
        ) : hooks.data ? (
          <div className="grid gap-3 text-sm">
            <Value label="Payload URL" value={hooks.data.url} onCopy={() => copy("URL", hooks.data.url)} copyLabel="Copy the URL" />
            <Value label="Secret" value={hooks.data.secret} onCopy={() => copy("Secret", hooks.data.secret)} copyLabel="Copy the secret" />
            <ul className="grid gap-1 text-muted-foreground">
              <li>
                <b className="font-medium text-foreground">GitHub</b>: Settings, Webhooks, Add webhook. Content type{" "}
                <InlineCode>application/json</InlineCode>, the secret above, “Just the push event”.
              </li>
              <li>
                <b className="font-medium text-foreground">GitLab</b>: Settings, Webhooks. The secret as the Secret token, Push events.
              </li>
              <li>
                <b className="font-medium text-foreground">Gitea</b>: Settings, Webhooks, Add webhook, Gitea. Content type{" "}
                <InlineCode>application/json</InlineCode>, the secret above, Push events.
              </li>
            </ul>
            <p className="text-xs text-muted-foreground">
              From a terminal: <InlineCode>shpyrd git-hooks show --project {slug}</InlineCode>
            </p>
          </div>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

function Value({ label, value, onCopy, copyLabel }: { label: string; value: string; onCopy: () => void; copyLabel: string }) {
  return (
    <div className="grid gap-1">
      <span className="text-xs text-muted-foreground">{label}</span>
      <span className="flex items-start gap-1">
        <InlineCode className="break-all">{value}</InlineCode>
        <Button variant="ghost" size="icon-xs" icon={<Copy />} aria-label={copyLabel} onClick={onCopy} />
      </span>
    </div>
  );
}
```

`apps/workspace/src/screens/project/overview.tsx`:
- imports: add `import type { Perms } from "@/lib/perms";` and
  `import { GitSource } from "./source";`
- the signature becomes `export function Overview({ project, perms }: { project: Project; perms: Perms }) {`
- `<CardContent>` of the Summary card becomes
  `<CardContent className="grid gap-4">`, with `<GitSource project={project} perms={perms} />`
  as its first child, before `<InfoTable columns={3}>`;
- the `{spec.source?.git && (<>...Git...Revision...</>)}` block inside that
  `InfoTable` is removed: `GitSource` shows the repository and branch.

`apps/workspace/src/screens/project/index.tsx:109`:

```tsx
            <Route index element={<Overview project={project.data} perms={perms} />} />
```

- [ ] **Step 4: Run the tests and the gates**

Run: `cd apps/workspace && npm run test && npm run lint && npm run typecheck`
Expected: all pass.

Then look at it: `npm run design` in `apps/workspace` (port 4325), and open
`/projects/hello-world`, `/projects/docs`, `/projects/billing` and
`/projects/reports` in the browser tools, light and dark. Check: nothing
overflows at a phone's width; the switch, the skipped line, the "not
deployed" line, the error and the dialog read as the tests say. Fix what
does not, then stop the server.

- [ ] **Step 5: Commit**

```bash
git add apps/workspace/src apps/workspace/mock/projects.json
git commit -m "feat(workspace): the project's source: commit, last check, auto-deploy, deploy on push (RFC-0018)

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 9: Docs and RFC-0018 in progress

**Files:**
- Modify: `content/docs/deploying.mdx` (the `--git` example ~66; a new "Deploying on push" section after "Private repositories", before the "Connect GitHub is coming" callout ~108)
- Modify: `content/docs/cli.mdx` (the `deploy` flags; a "Deploy on push" table after "Git credentials" ~111)
- Modify: `rfcs/0018-repository-monitoring.md` (header, Implementation History), `rfcs/README.md:61`

**Interfaces:**
- Consumes: the behaviour of Tasks 4 to 8, as built. Words, commands and
  messages are copied from the code, not paraphrased.

- [ ] **Step 1: The deploying page**

In the "Other sources" block, the `--git` line's comment becomes
`# every push to the branch deploys`.

After the "Private repositories" section (before the "Connect GitHub is
coming" callout), add:

````mdx
### Deploying on push

A project that deploys from a git branch deploys every push to it. shpyrd
reads the branch's latest commit and builds that commit, never the branch
itself, for buildpack and Dockerfile builds alike. Each release names its
commit: `Push a1b2c3d by alice: fix login`, or `Commit a1b2c3d (polled)`.

**With a webhook: within seconds.** Each project has a webhook URL and a
secret. Add them to the repository:

```shell
shpyrd git-hooks show --project shop      # the URL, the secret, and where they go
shpyrd git-hooks rotate --project shop    # a new URL and secret; the old ones are refused
```

- GitHub: the repository's Settings, Webhooks, Add webhook. Payload URL: the
  URL; Content type: `application/json`; Secret: the secret; "Just the push
  event".
- GitLab: the project's Settings, Webhooks, Add new webhook. URL: the URL;
  Secret token: the secret; Trigger: Push events.
- Gitea: the repository's Settings, Webhooks, Add webhook, Gitea. Target
  URL: the URL; Content type: `application/json`; Secret: the secret;
  Trigger on: Push events.

The dashboard shows the same under the project's Summary, **Deploy on
push**. Only people who may change the project see the secret.

**Without one: within minutes.** shpyrd checks the branch every 2 minutes
(the operator sets it with `SHPYRD_GIT_POLL_INTERVAL`), and every hour once a
webhook has delivered in the last day: the webhook is then the signal, the
check the net under it. A repository shpyrd cannot read says why on the
project, with the `shpyrd git-auth` command that fixes it, and shpyrd tries
again by itself.

**Skipping a commit.** A commit whose message says `[skip deploy]` (or
`[deploy skip]`) is recorded and not deployed, also when shpyrd finds it
later at the head of the branch. The next commit deploys as usual.

**Auto-deploy off.** To record pushes without deploying them:

```shell
shpyrd deploy --git https://github.com/org/repo --no-auto-deploy
```

or switch **Auto-deploy** off under the project's Summary. The project shows
the latest commit it has not deployed; a deploy or switching auto-deploy on
again deploys it. A deploy that names neither `--auto-deploy` nor
`--no-auto-deploy` keeps the project's setting.

**What runs.** A `--ref` that is a branch or a tag is followed; a tag's
commit is the one it points at. A `--ref` that is a commit (7 to 40
hexadecimal characters) is built as given, and nothing is followed. A
deploy reads the branch again at once. If the branch is deleted, what runs
keeps running, and the project says so.

The first time a project on a branch is built this way after an upgrade, it
builds once more, the same code: the build now names the commit instead of
the branch.
````

- [ ] **Step 2: The CLI page**

In `cli.mdx`, the `shpyrd deploy` row (line 173) describes its flags in one
sentence each. After `` `--git <url> --ref <rev> --path <dir>` builds from Git; ``
insert:

```mdx
with `--git`, a branch or tag is followed and every push deploys, `--no-auto-deploy` records pushes without deploying them and `--auto-deploy` switches that back on (with neither, the project's setting stays);
```

After the "Git credentials" table:

```mdx
## Deploy on push

The webhook a git host calls when the project's branch moves. Signed in only. See [Deploying on push](/docs/deploying#deploying-on-push).

| Command | What it does |
| --- | --- |
| `shpyrd git-hooks show [--project <name>]` | The webhook's URL and secret, where they go on GitHub, GitLab and Gitea, and whether a push deploys. |
| `shpyrd git-hooks rotate [--project <name>]` | A new URL and secret. The old ones are refused from then on: change them on the git host. |
```

- [ ] **Step 3: RFC-0018 in progress**

`rfcs/0018-repository-monitoring.md` header:

```markdown
**Status:** in progress

**Owner:** Marcelo Paez Sequeira

**Depends on:** RFC-0017 (implemented)

**Creation date:** 2026-09-22

**Last update:** 2026-10-16
```

Under `## Implementation History`, after `- 2026-09-22: RFC written.`:

```markdown
- 2026-10-16: a source watcher pins each branch's commit (shpyrd.io/git-commit) from a generic webhook (GitHub HMAC, GitLab token, Gitea HMAC) or polling with a built-in ls-remote; kpack and BuildKit build the pinned SHA, so Dockerfile builds redeploy on push; [skip deploy]; auto-deploy per project; shpyrd git-hooks; the dashboard's source in the project Summary. See docs/superpowers/specs/2026-10-16-private-git-watcher-decisions.md.
```

`rfcs/README.md` line 61: the status column `provisional` becomes
`in progress`.

- [ ] **Step 4: Check the MDX**

`content/` has no build or check of its own (its `package.json` only
exports the files; the website renders them). Check by eye: every fence
closed, the new table with the same column count as its neighbours, the
anchor `#deploying-on-push` matching the heading.

- [ ] **Step 5: Commit**

```bash
git add content/docs/deploying.mdx content/docs/cli.mdx rfcs/0018-repository-monitoring.md rfcs/README.md
git commit -m "docs: deploying on push, shpyrd git-hooks; RFC-0018 in progress

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 10: The end-to-end run, screenshots, RFC implemented, the PR

**Done after the final review of the whole branch, not before.** Tasks 1 to
9 are reviewed one by one and then together; this task runs the result on
a real cluster and opens the PR. A fault found here is fixed in the task
that owns the code, with a test, and reviewed again.

**Files:**
- Modify: `rfcs/0018-repository-monitoring.md`, `rfcs/README.md:61` (implemented)

- [ ] **Step 1: Full test run**

Run: `go build ./... && go vet ./... && go test ./... && make lint && npm run test --workspace apps/workspace`
Expected: all pass.

- [ ] **Step 2: The cluster and Gitea**

Follow the local cluster memory: kind on 80/443, load the built server and
controller images (`grep -n "^kind\|^dev\|load" Makefile` names the
targets), never `extensions enable`. PR 1's run left an in-cluster Gitea
(`kubectl get pods -A | grep -i gitea`); reuse it, or install it the same
way. Then:

1. Gitea refuses webhooks to private addresses by default. Set, in its
   `app.ini` (`[webhook]`), `ALLOWED_HOST_LIST = *` and
   `SKIP_TLS_VERIFY = true` (the kind cluster's certificate is the local
   CA's), and restart Gitea.
2. Gitea must reach the server's webhook with the workspace's Host. First
   try: a CoreDNS rewrite of the workspace's host to the ingress
   controller's Service, so the delivery goes through the front door as from
   outside:
   `kubectl -n kube-system edit configmap coredns`, add
   `rewrite name <workspace host> ingress-nginx-controller.ingress-nginx.svc.cluster.local`
   inside the `.:53 {}` block, and
   `kubectl -n kube-system rollout restart deployment coredns`. Check from
   the Gitea pod: `wget -qO- --no-check-certificate https://<workspace host>/api/healthz`.
   If that cannot work, point the webhook at the server Service over HTTP and
   say in the PR how the Host was given.
3. Short polls for the test:
   `kubectl -n shpyrd-system set env deployment/shpyrd-server SHPYRD_GIT_POLL_INTERVAL=20s`.

- [ ] **Step 3: The runs**

With a private Gitea repository holding a tiny Node app and a `Dockerfile`
(PR 1's), and a deploy key or token for it:

1. `shpyrd deploy --git <url> --ref main --project watch` (buildpacks). The
   release says `Commit <sha> (polled)` or is the deploy's; the kpack Image's
   revision is the SHA
   (`kubectl get image.kpack.io -n <ns> -o jsonpath='{.items[0].spec.source.git.revision}'`).
2. `shpyrd git-hooks show --project watch`; add the webhook in Gitea as it
   says. Gitea's "Test delivery" answers 202.
3. Push a commit "change the greeting". Expect a release
   `Push <sha> by <user>: change the greeting` within seconds, and the new
   greeting at the app's URL.
4. Push "tidy [skip deploy]". Expect no release, the Summary's "Skipped
   <sha>: [skip deploy]", a `git.skip` entry on the dashboard's Activity
   page, and still no release after two poll intervals.
5. Remove the webhook in Gitea; push "by polling". Expect a release
   `Commit <sha> (polled)` within about 20 s.
6. Switch auto-deploy off in the dashboard; push "recorded only". Expect no
   release and the Summary's "Latest on main: <sha>, not deployed". Switch it
   on: a release follows.
7. Switch the project to the Dockerfile strategy and push once more. Expect
   a Dockerfile build of the new SHA and a release.
8. Break access (remove the deploy key in Gitea); push. Expect the
   Summary's error naming `shpyrd git-auth`, and what runs keeps running.
   Restore it: within two minutes the error clears and the push deploys.

- [ ] **Step 4: Screenshots**

With the browser tools, from the dashboard: the Summary pinned after a push
(commit, message, author, "via push"); skipped; auto-deploy off with the
"not deployed" line; the error; the **Deploy on push** dialog; the Releases
page with a release named `Push … by …`. Light theme, and one in dark. As
text: the `shpyrd git-hooks show` output and the CLI transcript of step 1.
Save images in the scratchpad, not the repository.

- [ ] **Step 5: RFC-0018 implemented**

`**Status:** implemented`, `**Last update:**` the run's date, and under
Implementation History:

```markdown
- 2026-10-16: verified end to end on a kind cluster against a private Gitea: a webhook push deploys in seconds, [skip deploy] does not, polling picks up a push without the webhook, auto-deploy off records only, buildpacks and Dockerfile alike.
```

`rfcs/README.md` line 61: `implemented`.

```bash
git add rfcs/0018-repository-monitoring.md rfcs/README.md
git commit -m "docs(rfc): RFC-0018 implemented, verified end to end on a kind cluster

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

- [ ] **Step 6: Push and open the PR**

```bash
git push -u origin feat/private-git-watcher
```

`gh pr create --base feat/private-git-repositories` (stacked on PR 1, #223;
after #223 merges, retarget to `main`):
- Title: `Private repositories, part 2: deploy on push (RFC-0018)`.
- Body: what it does; the deviations at the top of this plan; **the upgrade
  cost**: every existing app on a branch, buildpack or Dockerfile, rebuilds
  once when its build starts naming the SHA instead of the branch, and
  redeploys the same code (a saved but undeployed config edit of such an app
  is applied with that release); how it was tested; the screenshots; the CLI
  transcripts in code blocks; that PR 3 and PR 4 (the GitHub App) follow.
- End the body with:
  ```
  🤖 Generated with [Claude Code](https://claude.com/claude-code)

  https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb
  ```
- `gh` cannot attach images: open the PR first, then give the maintainer the
  image paths to drop in, or upload them through the browser tools on the
  PR page.
