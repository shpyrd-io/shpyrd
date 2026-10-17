# Checks, Deployments, watch paths and wait for CI, the GitHub App part 4 (RFC-0054) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** For a repository the GitHub App covers, GitHub shows what shpyrd
does with each commit (a `shpyrd / <project>` check run and a Deployment per
release that changes the code), a project can wait for a commit's other CI
before deploying it, and a project can deploy only the commits that change
its watch paths.

**Architecture:** Two new pure pieces: `pkg/gitauth/watchpaths.go` (the
gitignore-style matcher) and `pkg/github/ci.go` (the CI verdict over check
runs and commit statuses). `pkg/github` grows per-scope installation tokens
(`Permissions`: the clone scope, a watch scope, a report scope, each with its
own cache key) and the calls the two loops need; the fake GitHub
(`githubtest`) answers them and records every write with the token's
permissions. The source watcher's `decideSource` stays pure: it says when it
needs GitHub (`sourceUpdate.ask`), `Reconcile` gathers the answer through the
App (`GitHubApp.cover`) and decides again; steps 4 (watch paths) and 5 (wait
for CI) hold or skip a commit in the record (`Held`, `NotDeployed`). The App's
webhook turns `check_suite` and `status` events into a `shpyrd.io/git-ci`
nudge on the Apps waiting on that commit. A new reconciler,
`GitHubReporter`, beside the watcher, writes check runs and Deployments from
App state, remembers what it sent in `shpyrd.io/github-report`, and writes
nothing to status. The API and CLI set `watchPaths` and `waitForChecks`; the
dashboard's Source block shows the held and not-deployed commits and has the
two settings.

**Tech Stack:** Go, gin, cobra, controller-runtime v0.25.1,
controller-gen (`make generate`); Next 16, React 19, TanStack Query,
`@shpyrd/ui`, Vitest.

**Spec:** `docs/superpowers/specs/2026-10-16-github-checks-design.md`
(binding), building on
`docs/superpowers/specs/2026-10-16-private-git-repositories-design.md` §2-3
and PR 3 (`docs/superpowers/plans/2026-10-16-github-app-connect.md`, #227).
Branch: `feat/github-checks`, stacked on `feat/github-app-connect`.

## Global Constraints

- The brand is written `shpyrd`, lowercase, everywhere, including docs, CLI
  output, check run names (`shpyrd / <project>`) and dashboard copy.
  `X-Shpyrd-*` headers keep their capitals.
- The spec is binding; where the code disagreed, or the spec left a choice
  open, the deviations below say how it was resolved.
- Out of scope, not even as fields or routes: pull request previews, PR
  comments, commit statuses written by shpyrd, the "Re-run" button on
  shpyrd's own check, other hosts' checks.
- Credential values (installation tokens, the App's private key, the webhook
  secret) never appear in a log line, an audit detail, an error, an API
  answer, a check run or a Deployment. A check's summary carries the release
  note and, on failure, the last lines of the App's message: never logs,
  tokens or config.
- Every call to GitHub dials through `gitauth.SafeHTTPClient` (the
  `github.Client` default). Tests reach the fake GitHub through
  `githubtest.Server.Options()` / `Source()`.
- Token scopes, each its own cache key, each scoped to the one repository:
  clone and ls-remote `{"contents":"read"}` (unchanged); the watcher
  `{"contents":"read","checks":"read","statuses":"read"}`; the reporter
  `{"checks":"write","deployments":"write"}`.
- The App's webhook keeps PR 3's rules: its own per-IP limiter, a 5 MiB body
  cap, `X-Hub-Signature-256` only, 404 while no App Secret exists, 401 for a
  bad signature, 202 for everything that is not applied.
- Watch paths: at most 20 patterns of up to 200 characters, checked when set
  (API `422`), never at match time. Compare: GitHub's cap is 300 files.
- Wait for CI: "no CI" after 60 seconds with no run and no status; given up
  one hour after `HeldSince`; a held commit is read again at most every 30
  seconds, and at once on a nudge.
- The reporter: a failed write is logged at most once a minute per App and
  retried through `RequeueAfter`, from 30 seconds doubling up to 10 minutes;
  the annotation keeps only what GitHub accepted.
- Audit actions stay `git.push` and `git.skip`; a commit not deployed is
  `git.skip` with the reason as its detail.
- Messages are plain sentences that name the fix, in the style of
  `describeSourceError` and PR 3's GitHub messages.
- The dashboard uses only `@shpyrd/ui` components and tokens, calls only
  through `api`, and the Mock answers every new call (`apps/AGENTS.md`;
  `design/DESIGN_FOR_AGENT.md` for any design session).
  `npm run lint`, `npm run typecheck` and `npm run test` pass in
  `apps/workspace`. Vitest tests are named as sentences.
- Every `go build`, `go test` and `go vet` runs with
  `GOTMPDIR=/home/nkr/.cache/gotmp` (create it once with `mkdir -p`). After
  changing `api/v1alpha1`, run `GOTMPDIR=/home/nkr/.cache/gotmp make generate`
  and commit the regenerated deepcopy and CRD.
- Every commit ends with these two lines, the first naming the model that
  wrote the commit (for example `Claude Opus 5.5 <noreply@anthropic.com>`):
  ```
  Co-Authored-By: <your model name> <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb
  ```
- The PR carries screenshots from both sides: GitHub's commit page and
  Deployments, shpyrd's Source block and settings (Task 12).

## Deviations, deliberate, and where the code disagreed with the spec

1. **There is no `shpyrd status` for a project.** A project's state is
   `shpyrd projects info <slug>` (`internal/cli/apps.go`, `printAppInfo`);
   the held or not-deployed commit and its reason are shown there.
   `detailToApp` did not copy `status.source` at all; it does now.
2. **"No CI" deploys with the note "(no CI)", not "(CI passed)".** The spec
   puts both under one note; saying CI passed when there is none would
   mislead the release history.
3. **GitHub unreadable while a commit waits for CI keeps it held.** An empty
   answer is never taken for "no CI": the commit stays held, is read again
   at the next requeue (with PR 3's per-installation backoff), and is given
   up an hour after `HeldSince`. "A GitHub outage never fails or delays a
   deploy" is read as the reporter's rule; a person who asked to wait for CI
   gets no unverified deploy.
4. **A commit CI failed and then re-run gets a second check run.** GitHub
   does not take a completed run back to `in_progress`; when the stored
   check is completed and the new state is not, a new run of the same name
   is made on the commit. Otherwise one run per commit.
5. **A Deployment's `ref` is the full SHA when the watcher's record knows
   it, else `Release.Source`'s 12 characters** (a rollback to a commit no
   longer pinned). `Release.Source` stays as it is; GitHub resolves an
   abbreviated SHA.
6. **The deploy request takes `watchPaths` and `waitForChecks` at the top
   level, beside `git`.** `waitForChecks` is a plain `bool` in the CRD, so a
   deploy body cannot say "keep" inside `git`; the server keeps the
   project's two settings unless the top-level fields say otherwise, and
   ignores `git.watchPaths`/`git.waitForChecks` in a deploy body. In
   `PATCH /api/projects/:slug`, `"watchPaths": []` removes them.
7. **The API says when the settings do nothing with `sourceNote`**, a
   sentence in the project's view beside `githubFeatures`; `shpyrd deploy`
   prints it, the dashboard shows "Needs the GitHub App".
8. **A compare that says `behind` is not trusted either**, like `diverged`:
   a branch moved back (a force-push) has no files in a three-dot compare.
9. **Only a CI failure reopens on a nudge.** A commit given up after an hour
   stays given up (the spec names re-runs of failed commits only). A re-run
   counts its hour from the nudge.
10. **Watch paths are anchored at the repository root.** `*.md` is the root's
    Markdown files; `**/*.md` is any depth (the placeholder `!**/*.md`
    agrees).
11. **The reporter covers Apps that follow a branch on github.com**; an App
    pinned to a commit ref gets neither checks nor Deployments. Its first
    reconcile after the upgrade reports the running commit and the newest
    code release once.
12. **The CI reason is stored as `CI failed: <check>`** (the spec's words for
    `NotDeployed`); the dashboard words it "CI failed (<check>)", as the
    spec's Source block does.
13. **A rollback, or a release of config or sizes, does not touch the
    commit's check.** After a rollback the pinned commit still names the
    newest code; its check stays as it was, and the rolled-back release
    gets its Deployment. A config release rolling out does not take a
    finished check (or Deployment) back to in progress.
14. **The check's `details_url` is `<workspace dashboard>/projects/<slug>`**,
    the address `Config.issuer` gives the App's workspace.

## Review Focus

1. **A force-push or rebase on a branch with watch paths.** GitHub's compare
   then says `diverged` or `behind`, or does not know the old commit (404).
   Expected: the commit deploys, and its release note ends "(watch paths not
   checked: …)"; nothing is silently skipped. Test:
   `TestWatchPathsAreNotCheckedWhenGitHubsListCannotBeTrusted` (Task 5).
2. **GitHub down while a commit waits for CI.** Two failed reads look like
   "no runs, no statuses". Expected: the commit stays held, is never deployed
   as "no CI", and deploys once GitHub answers green. Test:
   `TestAHeldCommitStaysHeldWhileGitHubIsDown` (Task 6).
3. **shpyrd's own check counted in its own CI.** The reporter puts a queued
   `shpyrd / <project>` run on the held commit. Expected: the verdict leaves
   it out, so a commit never waits on itself. Tests:
   `TestTheVerdictLeavesOutShpyrdsOwnRuns` (Task 3) and the own run in
   `TestAHeldCommitDeploysOnceItsCIPasses` (Task 6).
4. **A rollback after a successful deploy.** The latest release is an older
   commit while the pin names the newest. Expected: the newest commit's check
   stays "Running at …", and the rollback is a Deployment of the older
   commit. Test: `TestARollbackKeepsTheCheckAndIsADeployment` (Task 8).
5. **A CI event for a commit another project or workspace also has.**
   `status` and `check_suite` name a SHA and a repository. Expected: only
   the Apps of the installation's workspace, on that repository, held on
   that SHA or CI-failed on it, are nudged. Test:
   `TestGitHubWebhookNudgesOnlyTheAppsWaitingOnThatCommit` (Task 6).

Also pinned by tests, outside the five: a person's deploy ignoring both
settings (`TestAPersonsDeployIgnoresWatchPathsAndWaitForCI`, Task 5), the
writes never using the clone token
(`TestEveryReportWriteUsesTheReportToken`, Task 7), one check run per commit
across a restart (`TestOneCheckRunPerCommitAcrossARestart`, Task 7), a
suspended installation reporting nothing
(`TestNothingIsReportedForASuspendedInstallation`, Task 7).

---

## File structure

| File | Responsibility |
|---|---|
| `api/v1alpha1/app_types.go` (modify: annotations ~118-125, `GitSource` 320-329, `SourceStatus` 601-631) | `WatchPaths`, `WaitForChecks`; `Held`, `HeldSince`, `NotDeployed`, `NotDeployedReason`; `AnnotationGitCI`, `AnnotationGitHubReport` |
| `api/v1alpha1/zz_generated.deepcopy.go`, `deploy/components/shpyrd/base/crds/shpyrd.io_apps.yaml` (generated) | `make generate` |
| `pkg/gitauth/webhook.go` (modify: `SourceRecord` and after it), `webhook_test.go` | The record's new fields, the reasons, `WaitsOnCI`, `CINudge` |
| `pkg/gitauth/watchpaths.go`, `watchpaths_test.go` (new) | The matcher: `ValidWatchPaths`, `CleanWatchPaths`, `MatchWatchPaths` |
| `pkg/github/ci.go`, `ci_test.go` (new) | `CheckRun`, `CommitStatus`, `CIVerdict`, `Verdict`, `Unanswered` |
| `pkg/github/client.go` (modify: `tokenKey`, `InstallationToken`, `CachedInstallationToken`, `Repositories`, `Branches`) | `Permissions` and the three scopes |
| `pkg/github/checks.go`, `checks_test.go` (new) | `Compare`, `CheckRuns`, `CommitStatuses`, check run and Deployment writes, `IsNotFound` |
| `pkg/github/client_test.go` (modify) | The new argument |
| `pkg/github/githubtest/fake.go` (modify), `githubtest/checks.go` (new) | Tokens remember their permissions; compare, runs, statuses, writes |
| `internal/controller/github.go` (modify: `build`, new `cover`) | The clone scope; `cover` |
| `internal/controller/source_gates.go`, `source_gates_test.go` (new) | Steps 4 and 5: `sourceGates`, `gateCommit`, `gather` |
| `internal/controller/source_controller.go` (modify: `SourceReconciler`, `sourceEvent`, `sourceUpdate`, `Reconcile`, `waitingCommit`, `forget`, `decideSource`, `auditUpdate`) | The watcher's new steps |
| `internal/controller/source_build.go` (modify: `sourceStatus`), `source_build_test.go` | Status shows them |
| `internal/controller/github_report.go`, `github_report_test.go` (new) | The reporter |
| `pkg/server/server.go` (modify: after the source watcher ~585) | Register the reporter |
| `pkg/api/github_webhook.go` (modify: the event switch, `githubPush`), `github_webhook_test.go` | `check_suite`, `status` nudges |
| `pkg/api/github_features.go` (new), `source_settings_test.go` (new) | `githubCovers`, `projectDetail`, `sourceNote` |
| `pkg/api/apps.go` (modify: `AppDetail` 130-146, `UpdateAppRequest`/`updateApp` 545-626, `DeployRequest`/`deployApp` 642-800, `getApp` 413-419) | The settings |
| `internal/cli/deploy.go` (modify: flags, `RunE`), `appclient.go` (`detailToApp` ~1069), `apps.go` (`printAppInfo` ~444), `source_settings_test.go` (new) | The flags; `projects info` |
| `apps/workspace/src/api/types.ts`, `api.ts`, `mock.ts`, `mock/projects.json`, `api.test.ts` | Types, call, Mock |
| `apps/workspace/src/screens/project/source.tsx`, `source.test.tsx` | The Source block |
| `content/docs/deploying.mdx`, `content/docs/cli.mdx` | Docs |
| `rfcs/0054-github-app.md`, `rfcs/README.md:98` | Implemented |

---

### Task 1: The App's new fields, and the watcher's record

**Files:**
- Modify: `api/v1alpha1/app_types.go` (annotations after `AnnotationGitSource` ~124; `GitSource` 320-329; `SourceStatus` 601-631)
- Generated: `api/v1alpha1/zz_generated.deepcopy.go`, `deploy/components/shpyrd/base/crds/shpyrd.io_apps.yaml`
- Modify: `pkg/gitauth/webhook.go` (`SourceRecord`, and after `ReadSourceRecord`)
- Modify: `internal/controller/source_build.go` (`sourceStatus`)
- Test: `pkg/gitauth/webhook_test.go`, `internal/controller/source_build_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces:
  - `shpyrdv1.GitSource.WatchPaths []string` (`json:"watchPaths,omitempty"`), `shpyrdv1.GitSource.WaitForChecks bool` (`json:"waitForChecks,omitempty"`)
  - `shpyrdv1.SourceStatus.Held string`, `HeldSince *metav1.Time`, `NotDeployed string`, `NotDeployedReason string` (json `held`, `heldSince`, `notDeployed`, `notDeployedReason`)
  - `const shpyrdv1.AnnotationGitCI = "shpyrd.io/git-ci"`, `shpyrdv1.AnnotationGitHubReport = "shpyrd.io/github-report"`
  - `gitauth.SourceRecord.Held string`, `HeldSince *time.Time`, `NotDeployed string`, `NotDeployedReason string` (same json names)
  - `const gitauth.ReasonNoWatchedChanges = "No changes in the watch paths"`, `gitauth.ReasonCIFailedPrefix = "CI failed: "`, `gitauth.ReasonCITimeout = "CI did not finish within an hour"`
  - `func (gitauth.SourceRecord) WaitsOnCI(sha string) bool`
  - `type gitauth.CINudge struct { SHA, Delivery string; At time.Time }` (json `sha`, `delivery`, `at`), `func gitauth.ReadCINudge(annotation string) *gitauth.CINudge`

- [ ] **Step 1: Write the failing tests**

Append to `pkg/gitauth/webhook_test.go` (its imports already have `encoding/json`, `strings`, `time`):

```go
func TestSourceRecordKeepsHeldAndNotDeployedCommits(t *testing.T) {
	since := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	in := SourceRecord{Ref: "main", Latest: "89abcdef0123456789abcdef0123456789abcdef",
		Held: "89abcdef0123456789abcdef0123456789abcdef", HeldSince: &since,
		NotDeployed: "0123456789abcdef0123456789abcdef01234567", NotDeployedReason: ReasonNoWatchedChanges}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"held":`, `"heldSince":`, `"notDeployed":`, `"notDeployedReason":"No changes in the watch paths"`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("%s lacks %s", raw, key)
		}
	}
	out := ReadSourceRecord(string(raw))
	if out.Held != in.Held || out.HeldSince == nil || !out.HeldSince.Equal(since) || out.NotDeployed != in.NotDeployed || out.NotDeployedReason != in.NotDeployedReason {
		t.Errorf("read back %+v", out)
	}
}

func TestARecordWaitsOnTheCIOfAHeldOrACIFailedCommit(t *testing.T) {
	const a, b = "89abcdef0123456789abcdef0123456789abcdef", "0123456789abcdef0123456789abcdef01234567"
	for _, c := range []struct {
		name string
		rec  SourceRecord
		sha  string
		want bool
	}{
		{"held", SourceRecord{Latest: a, Held: a}, a, true},
		{"held, named in capitals", SourceRecord{Latest: a, Held: a}, strings.ToUpper(a), true},
		{"another commit", SourceRecord{Latest: a, Held: a}, b, false},
		{"CI failed, still the newest", SourceRecord{Latest: a, NotDeployed: a, NotDeployedReason: ReasonCIFailedPrefix + "test"}, a, true},
		{"CI failed, a newer commit since", SourceRecord{Latest: b, NotDeployed: a, NotDeployedReason: ReasonCIFailedPrefix + "test"}, a, false},
		{"no change in the watch paths", SourceRecord{Latest: a, NotDeployed: a, NotDeployedReason: ReasonNoWatchedChanges}, a, false},
		{"CI did not finish", SourceRecord{Latest: a, NotDeployed: a, NotDeployedReason: ReasonCITimeout}, a, false},
		{"no commit named", SourceRecord{Latest: a, Held: a}, "", false},
	} {
		if got := c.rec.WaitsOnCI(c.sha); got != c.want {
			t.Errorf("%s: %v", c.name, got)
		}
	}
}

func TestACINudgeIsReadBack(t *testing.T) {
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	raw, _ := json.Marshal(CINudge{SHA: "89abcdef0123456789abcdef0123456789abcdef", Delivery: "d-1", At: at})
	n := ReadCINudge(string(raw))
	if n == nil || n.SHA != "89abcdef0123456789abcdef0123456789abcdef" || n.Delivery != "d-1" || !n.At.Equal(at) {
		t.Errorf("read back %+v from %s", n, raw)
	}
	if ReadCINudge("") != nil || ReadCINudge("{not json") != nil {
		t.Error("nothing to read is no nudge")
	}
}
```

Append to `internal/controller/source_build_test.go`:

```go
func TestSourceStatusShowsAHeldAndANotDeployedCommit(t *testing.T) {
	app := pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit)
	withRecord(app, gitauth.SourceRecord{Ref: "main", Latest: otherCommit, Held: otherCommit, HeldSince: at(sourceNow),
		NotDeployed: testCommit, NotDeployedReason: gitauth.ReasonNoWatchedChanges})
	st := sourceStatus(app)
	if st.Held != otherCommit || st.HeldSince == nil || !st.HeldSince.Time.Equal(sourceNow) ||
		st.NotDeployed != testCommit || st.NotDeployedReason != gitauth.ReasonNoWatchedChanges {
		t.Errorf("status.source = %+v", st)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `mkdir -p /home/nkr/.cache/gotmp && GOTMPDIR=/home/nkr/.cache/gotmp go test ./pkg/gitauth/ ./internal/controller/ -run 'SourceRecordKeeps|WaitsOnTheCI|CINudge|SourceStatusShowsAHeld'`
Expected: FAIL to compile: `unknown field Held in struct literal`, `undefined: ReasonNoWatchedChanges`, `undefined: CINudge`.

- [ ] **Step 3: The CRD fields**

In `api/v1alpha1/app_types.go`, `GitSource` becomes:

```go
type GitSource struct {
	URL string `json:"url"`
	// Revision is a branch, tag or commit. Defaults to "main".
	// +optional
	Revision string `json:"revision,omitempty"`
	// AutoDeploy deploys every new commit of the branch; false records
	// them without deploying. Unset means true.
	// +optional
	AutoDeploy *bool `json:"autoDeploy,omitempty"`
	// WatchPaths are gitignore-style globs from the repository root: a
	// commit deploys only when a file it changes matches. The last pattern
	// that matches a file decides it; a leading ! excludes. GitHub App only
	// (RFC-0054).
	// +optional
	// +kubebuilder:validation:MaxItems=20
	// +kubebuilder:validation:items:MaxLength=200
	WatchPaths []string `json:"watchPaths,omitempty"`
	// WaitForChecks holds a commit until its other checks and statuses on
	// GitHub pass. GitHub App only (RFC-0054).
	// +optional
	WaitForChecks bool `json:"waitForChecks,omitempty"`
}
```

In `SourceStatus`, after the `Skipped` field:

```go
	// Held is a commit waiting for its other checks on GitHub, since
	// HeldSince (RFC-0054).
	// +optional
	Held string `json:"held,omitempty"`
	// +optional
	HeldSince *metav1.Time `json:"heldSince,omitempty"`
	// NotDeployed is the last commit the watcher decided not to deploy:
	// nothing changed in the watch paths, CI failed, or CI did not finish.
	// NotDeployedReason says which, in words.
	// +optional
	NotDeployed string `json:"notDeployed,omitempty"`
	// +optional
	NotDeployedReason string `json:"notDeployedReason,omitempty"`
```

After `AnnotationGitSource = "shpyrd.io/git-source"`:

```go
	// AnnotationGitCI is a nudge the GitHub App's webhook leaves when a
	// commit's checks or statuses change (RFC-0054), as JSON: the source
	// watcher evaluates a held or CI-failed commit again, then drops it.
	AnnotationGitCI = "shpyrd.io/git-ci"
	// AnnotationGitHubReport is what the GitHub reporter last sent for the
	// App, as JSON: the check run and the Deployment (RFC-0054).
	AnnotationGitHubReport = "shpyrd.io/github-report"
```

- [ ] **Step 4: Regenerate**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp make generate && grep -c 'watchPaths\|waitForChecks\|heldSince\|notDeployedReason' deploy/components/shpyrd/base/crds/shpyrd.io_apps.yaml`
Expected: a count of at least 4; `git diff --stat` shows `zz_generated.deepcopy.go` (the `WatchPaths` slice and `HeldSince` copied) and the CRD.

- [ ] **Step 5: The record**

In `pkg/gitauth/webhook.go`, `SourceRecord` gains, after `Skipped`:

```go
	// Held is a commit waiting for its CI on GitHub, since HeldSince: the
	// time the commit was first seen, or the nudge of a re-run (RFC-0054).
	Held      string     `json:"held,omitempty"`
	HeldSince *time.Time `json:"heldSince,omitempty"`
	// NotDeployed is the last commit decided not to deploy, and why:
	// ReasonNoWatchedChanges, ReasonCIFailedPrefix and the check's name,
	// or ReasonCITimeout.
	NotDeployed       string `json:"notDeployed,omitempty"`
	NotDeployedReason string `json:"notDeployedReason,omitempty"`
```

After `ReadSourceRecord`:

```go
// Why a commit was not deployed (RFC-0054), as SourceRecord's
// NotDeployedReason says it.
const (
	ReasonNoWatchedChanges = "No changes in the watch paths"
	ReasonCIFailedPrefix   = "CI failed: "
	ReasonCITimeout        = "CI did not finish within an hour"
)

// WaitsOnCI says a change of sha's checks matters to the record: sha is
// held for its CI, or CI failed it and it is still the branch's newest
// commit, so a re-run may pass.
func (r SourceRecord) WaitsOnCI(sha string) bool {
	sha = strings.ToLower(sha)
	if sha == "" {
		return false
	}
	return r.Held == sha || (r.NotDeployed == sha && r.Latest == sha && strings.HasPrefix(r.NotDeployedReason, ReasonCIFailedPrefix))
}

// CINudge is a change of a commit's checks or statuses, left by the GitHub
// App's webhook in the App's shpyrd.io/git-ci annotation for the source
// watcher.
type CINudge struct {
	SHA      string    `json:"sha"`
	Delivery string    `json:"delivery,omitempty"`
	At       time.Time `json:"at"`
}

// ReadCINudge reads the git-ci annotation; nil when there is none or it
// cannot be read.
func ReadCINudge(annotation string) *CINudge {
	if annotation == "" {
		return nil
	}
	var n CINudge
	if err := json.Unmarshal([]byte(annotation), &n); err != nil || n.SHA == "" {
		return nil
	}
	n.SHA = strings.ToLower(n.SHA)
	return &n
}
```

- [ ] **Step 6: Status shows them**

In `internal/controller/source_build.go`, `sourceStatus`, after the line that sets `st.LatestAt, st.LastWebhookAt, st.CheckedAt`:

```go
	st.Held, st.HeldSince = rec.Held, metaTime(rec.HeldSince)
	st.NotDeployed, st.NotDeployedReason = rec.NotDeployed, rec.NotDeployedReason
```

- [ ] **Step 7: Run the tests**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp go build ./... && GOTMPDIR=/home/nkr/.cache/gotmp go test ./pkg/gitauth/ ./internal/controller/ ./api/...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add api/v1alpha1/app_types.go api/v1alpha1/zz_generated.deepcopy.go deploy/components/shpyrd/base/crds/shpyrd.io_apps.yaml pkg/gitauth/webhook.go pkg/gitauth/webhook_test.go internal/controller/source_build.go internal/controller/source_build_test.go
git commit -m "feat(api): watch paths, wait for CI, and the held and not-deployed commits (RFC-0054)

Co-Authored-By: <your model name> <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 2: The watch-path matcher

**Files:**
- Create: `pkg/gitauth/watchpaths.go`
- Test: `pkg/gitauth/watchpaths_test.go`

**Interfaces:**
- Consumes: nothing (the standard library's `path.Match` for one segment).
- Produces:
  - `const gitauth.MaxWatchPaths = 20`, `gitauth.MaxWatchPathLength = 200`
  - `func gitauth.CleanWatchPaths(patterns []string) []string`: trimmed, blank lines dropped, nil when none
  - `func gitauth.ValidWatchPaths(patterns []string) error`: the count, the length, each pattern readable
  - `func gitauth.MatchWatchPaths(patterns, files []string) bool`

- [ ] **Step 1: Write the failing tests**

`pkg/gitauth/watchpaths_test.go`:

```go
package gitauth

import (
	"fmt"
	"strings"
	"testing"
)

func TestWatchPathsMatchFromTheRepositoryRoot(t *testing.T) {
	for _, c := range []struct {
		patterns []string
		file     string
		want     bool
	}{
		{[]string{"services/api/**"}, "services/api/main.go", true},
		{[]string{"services/api/**"}, "services/api/internal/db/db.go", true},
		{[]string{"services/api/**"}, "services/web/main.go", false},
		{[]string{"services/api/**"}, "services/api", false},
		{[]string{"*.md"}, "README.md", true},
		{[]string{"*.md"}, "docs/guide.md", false},
		{[]string{"**/*.md"}, "docs/guide.md", true},
		{[]string{"**/*.md"}, "README.md", true},
		{[]string{"**"}, "anything/at/all.txt", true},
		{[]string{"/web/**"}, "web/app.ts", true},
		{[]string{"docs/"}, "docs/a/b.md", true},
		{[]string{"docs/"}, "docsite/index.md", false},
		{[]string{"a/**/b.txt"}, "a/b.txt", true},
		{[]string{"a/**/b.txt"}, "a/x/y/b.txt", true},
		{[]string{"v[0-9].txt"}, "v7.txt", true},
		{[]string{"v[0-9].txt"}, "vx.txt", false},
		{[]string{"src/?.go"}, "src/a.go", true},
		{[]string{"src/*.go"}, "src/pkg/a.go", false},
	} {
		if got := MatchWatchPaths(c.patterns, []string{c.file}); got != c.want {
			t.Errorf("%v on %s: %v", c.patterns, c.file, got)
		}
	}
}

func TestTheLastWatchPathThatMatchesDecides(t *testing.T) {
	everythingButDocs := []string{"**", "!**/*.md"}
	if MatchWatchPaths(everythingButDocs, []string{"README.md", "docs/guide.md"}) {
		t.Error("only Markdown changed: nothing deploys")
	}
	if !MatchWatchPaths(everythingButDocs, []string{"README.md", "main.go"}) {
		t.Error("a Go file changed with the docs: it deploys")
	}
	if !MatchWatchPaths([]string{"!**/*.md", "**"}, []string{"README.md"}) {
		t.Error("** comes last and matches: it decides")
	}
	if MatchWatchPaths([]string{"!services/**"}, []string{"main.go"}) {
		t.Error("only an exclusion: nothing is included")
	}
	if MatchWatchPaths([]string{"services/api/**", "!services/api/docs/**"}, []string{"services/api/docs/x.md"}) {
		t.Error("an exclusion after an inclusion wins for the files it matches")
	}
}

func TestARenamedFileCountsUnderBothNames(t *testing.T) {
	// The compare lists a rename as its new name and its old one.
	if !MatchWatchPaths([]string{"services/api/**"}, []string{"services/shared/util.go", "services/api/util.go"}) {
		t.Error("moved out of the watch path: its old name matches")
	}
}

func TestWatchPathsThatCannotBeReadAreRefusedWhenSet(t *testing.T) {
	for _, p := range []string{"", "  ", "!", "/", "a//b", "a**", "**b/c", "src/[a-", `x\`} {
		if err := ValidWatchPaths([]string{p}); err == nil {
			t.Errorf("%q is taken", p)
		}
	}
	if err := ValidWatchPaths([]string{"services/api/**", "!**/*.md", "/web/", "docs/"}); err != nil {
		t.Errorf("good patterns: %v", err)
	}
	var many []string
	for i := range MaxWatchPaths + 1 {
		many = append(many, fmt.Sprintf("dir%d/**", i))
	}
	if err := ValidWatchPaths(many); err == nil || !strings.Contains(err.Error(), "at most 20") {
		t.Errorf("21 patterns: %v", err)
	}
	if err := ValidWatchPaths([]string{strings.Repeat("a", MaxWatchPathLength+1)}); err == nil || !strings.Contains(err.Error(), "200 characters") {
		t.Errorf("a long one: %v", err)
	}
	if !MatchWatchPaths([]string{"a**", "**"}, []string{"x"}) {
		t.Error("an unreadable pattern that slipped through matches nothing; the others still count")
	}
}

func TestCleanWatchPathsDropsBlankLines(t *testing.T) {
	got := CleanWatchPaths([]string{" services/api/** ", "", "  ", "!**/*.md"})
	if strings.Join(got, "|") != "services/api/**|!**/*.md" {
		t.Errorf("got %q", got)
	}
	if CleanWatchPaths([]string{"", " "}) != nil {
		t.Error("nothing left is nil, so the field is removed")
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp go test ./pkg/gitauth/ -run 'WatchPath|Renamed'`
Expected: FAIL: `undefined: MatchWatchPaths`.

- [ ] **Step 3: Write the matcher**

`pkg/gitauth/watchpaths.go`:

```go
package gitauth

import (
	"fmt"
	"path"
	"strings"
)

// Watch paths (RFC-0054): gitignore-style globs from the repository root
// that say which changes deploy a project. Patterns run in order and the
// last one that matches a file decides it; a leading ! excludes. `*`
// matches within one part of a path, `**` any number of parts, and a
// pattern ending in / everything below it.

const (
	MaxWatchPaths      = 20
	MaxWatchPathLength = 200
)

// watchPattern is one pattern, read: whether it excludes, and its parts.
type watchPattern struct {
	exclude  bool
	segments []string
}

// parseWatchPath reads a pattern. A leading / is the repository root, as
// is none; a trailing / is everything below.
func parseWatchPath(p string) (watchPattern, error) {
	raw := strings.TrimSpace(p)
	var w watchPattern
	if rest, ok := strings.CutPrefix(raw, "!"); ok {
		w.exclude, raw = true, rest
	}
	raw = strings.TrimPrefix(raw, "/")
	if raw != "" && strings.HasSuffix(raw, "/") {
		raw += "**"
	}
	if raw == "" {
		return watchPattern{}, fmt.Errorf("the watch path %q names no file or folder", p)
	}
	for _, seg := range strings.Split(raw, "/") {
		switch {
		case seg == "":
			return watchPattern{}, fmt.Errorf("the watch path %q has an empty part (//)", p)
		case seg == "**":
		case strings.Contains(seg, "**"):
			return watchPattern{}, fmt.Errorf("in the watch path %q, ** stands alone between slashes, as in services/**/api", p)
		default:
			// path.Match checks the whole pattern even when the name does
			// not match.
			if _, err := path.Match(seg, ""); err != nil {
				return watchPattern{}, fmt.Errorf("the watch path %q is not a pattern shpyrd can read: check its [ ] and \\", p)
			}
		}
		w.segments = append(w.segments, seg)
	}
	return w, nil
}

// CleanWatchPaths trims each pattern and drops the blank ones: what a
// person typed, one per line. Nil when none is left.
func CleanWatchPaths(patterns []string) []string {
	var out []string
	for _, p := range patterns {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ValidWatchPaths checks watch paths as they are set: at most 20 patterns
// of up to 200 characters, each one readable.
func ValidWatchPaths(patterns []string) error {
	if len(patterns) > MaxWatchPaths {
		return fmt.Errorf("a project takes at most %d watch paths; there are %d", MaxWatchPaths, len(patterns))
	}
	for _, p := range patterns {
		if len(p) > MaxWatchPathLength {
			return fmt.Errorf("a watch path is at most %d characters; %.40q… is longer", MaxWatchPathLength, p)
		}
		if _, err := parseWatchPath(p); err != nil {
			return err
		}
	}
	return nil
}

// MatchWatchPaths says a change deploys: one of the files is matched, and
// the last pattern that matches it does not exclude. A pattern that cannot
// be read matches nothing (patterns are checked when they are set).
func MatchWatchPaths(patterns, files []string) bool {
	var ps []watchPattern
	for _, p := range patterns {
		if w, err := parseWatchPath(p); err == nil {
			ps = append(ps, w)
		}
	}
	for _, f := range files {
		name := strings.Split(strings.TrimPrefix(f, "/"), "/")
		in := false
		for _, w := range ps {
			if matchSegments(w.segments, name) {
				in = !w.exclude
			}
		}
		if in {
			return true
		}
	}
	return false
}

// matchSegments matches a pattern's parts against a path's: ** takes any
// number of parts, and at the end at least one (a folder's contents, not
// a file of that name).
func matchSegments(pat, name []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			rest := pat[1:]
			if len(rest) == 0 {
				return len(name) > 0
			}
			for i := 0; i <= len(name); i++ {
				if matchSegments(rest, name[i:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 {
			return false
		}
		if ok, _ := path.Match(pat[0], name[0]); !ok {
			return false
		}
		pat, name = pat[1:], name[1:]
	}
	return len(name) == 0
}
```

- [ ] **Step 4: Run the tests**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp go test ./pkg/gitauth/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/gitauth/watchpaths.go pkg/gitauth/watchpaths_test.go
git commit -m "feat(gitauth): watch paths, gitignore-style globs from the repository root (RFC-0054)

Co-Authored-By: <your model name> <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 3: The CI verdict

**Files:**
- Create: `pkg/github/ci.go`
- Test: `pkg/github/ci_test.go` (package `github_test`, as the package's other tests)

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type github.CheckRun struct { ID int64; Name, Status, Conclusion string; AppID int64 }`
  - `type github.CommitStatus struct { Context, State string }`
  - `const github.CIPending = "pending"`, `CIPassed = "passed"`, `CIFailed = "failed"`, `CINone = "none"`, `CITimedOut = "timed-out"`
  - `const github.NoCIAfter = 60 * time.Second`, `github.GiveUpAfter = time.Hour`
  - `type github.CIVerdict struct { State, Failing string }`
  - `func github.Verdict(runs []CheckRun, statuses []CommitStatus, appID int64, since, now time.Time) CIVerdict`
  - `func github.Unanswered(since, now time.Time) CIVerdict`

- [ ] **Step 1: Write the failing tests**

`pkg/github/ci_test.go`:

```go
package github_test

import (
	"testing"
	"time"

	"github.com/shpyrd-io/shpyrd/pkg/github"
)

const ourApp = 1234

var held = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

func run(name, status, conclusion string) github.CheckRun {
	return github.CheckRun{Name: name, Status: status, Conclusion: conclusion, AppID: 99}
}

func TestEachConclusionOfACheckRun(t *testing.T) {
	now := held.Add(5 * time.Minute)
	for conclusion, want := range map[string]string{
		"success": github.CIPassed, "neutral": github.CIPassed, "skipped": github.CIPassed,
		"failure": github.CIFailed, "cancelled": github.CIFailed, "timed_out": github.CIFailed,
		"action_required": github.CIFailed, "startup_failure": github.CIFailed,
		"stale": github.CIPending,
	} {
		v := github.Verdict([]github.CheckRun{run("test", "completed", conclusion)}, nil, ourApp, held, now)
		if v.State != want {
			t.Errorf("%s: %+v", conclusion, v)
		}
		if want == github.CIFailed && v.Failing != "test" {
			t.Errorf("%s names %q", conclusion, v.Failing)
		}
	}
	for _, status := range []string{"queued", "in_progress", "waiting", "requested", "pending"} {
		if v := github.Verdict([]github.CheckRun{run("test", status, "")}, nil, ourApp, held, now); v.State != github.CIPending {
			t.Errorf("%s: %+v", status, v)
		}
	}
}

func TestAFailureIsFinalWhileOthersStillRun(t *testing.T) {
	runs := []github.CheckRun{run("lint", "in_progress", ""), run("test", "completed", "failure"), run("build", "completed", "failure")}
	if v := github.Verdict(runs, nil, ourApp, held, held.Add(time.Minute)); v.State != github.CIFailed || v.Failing != "test" {
		t.Errorf("the first failing check is named: %+v", v)
	}
}

func TestCommitStatusesCountToo(t *testing.T) {
	now := held.Add(time.Minute)
	ok := []github.CheckRun{run("test", "completed", "success")}
	if v := github.Verdict(ok, []github.CommitStatus{{Context: "ci/circle", State: "success"}}, ourApp, held, now); v.State != github.CIPassed {
		t.Errorf("both green: %+v", v)
	}
	if v := github.Verdict(ok, []github.CommitStatus{{Context: "ci/circle", State: "pending"}}, ourApp, held, now); v.State != github.CIPending {
		t.Errorf("a status pending: %+v", v)
	}
	for _, state := range []string{"failure", "error"} {
		if v := github.Verdict(ok, []github.CommitStatus{{Context: "ci/circle", State: state}}, ourApp, held, now); v.State != github.CIFailed || v.Failing != "ci/circle" {
			t.Errorf("a status %s: %+v", state, v)
		}
	}
	if v := github.Verdict(nil, []github.CommitStatus{{Context: "deploy/preview", State: "success"}}, ourApp, held, now); v.State != github.CIPassed {
		t.Errorf("statuses alone: %+v", v)
	}
}

func TestTheVerdictLeavesOutShpyrdsOwnRuns(t *testing.T) {
	own := github.CheckRun{Name: "shpyrd / shop", Status: "queued", AppID: ourApp}
	runs := []github.CheckRun{own, run("test", "completed", "success")}
	if v := github.Verdict(runs, nil, ourApp, held, held.Add(10*time.Second)); v.State != github.CIPassed {
		t.Errorf("shpyrd's queued run does not hold its own commit: %+v", v)
	}
	if v := github.Verdict([]github.CheckRun{own}, nil, ourApp, held, held.Add(time.Minute)); v.State != github.CINone {
		t.Errorf("only shpyrd's own run is no CI: %+v", v)
	}
}

func TestNoCIIsSaidAfterAMinuteWithNothingOnTheCommit(t *testing.T) {
	if v := github.Verdict(nil, nil, ourApp, held, held.Add(59*time.Second)); v.State != github.CIPending {
		t.Errorf("before a minute: %+v", v)
	}
	if v := github.Verdict(nil, nil, ourApp, held, held.Add(60*time.Second)); v.State != github.CINone {
		t.Errorf("after a minute: %+v", v)
	}
}

func TestCIStillPendingAfterAnHourIsGivenUp(t *testing.T) {
	runs := []github.CheckRun{run("test", "in_progress", "")}
	if v := github.Verdict(runs, nil, ourApp, held, held.Add(59*time.Minute)); v.State != github.CIPending {
		t.Errorf("59 minutes: %+v", v)
	}
	if v := github.Verdict(runs, nil, ourApp, held, held.Add(time.Hour)); v.State != github.CITimedOut {
		t.Errorf("an hour: %+v", v)
	}
	if v := github.Unanswered(held, held.Add(10*time.Minute)); v.State != github.CIPending {
		t.Errorf("GitHub not answering is pending, never no CI: %+v", v)
	}
	if v := github.Unanswered(held, held.Add(time.Hour)); v.State != github.CITimedOut {
		t.Errorf("not answering for an hour: %+v", v)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp go test ./pkg/github/ -run 'Conclusion|Failure|Statuses|OwnRuns|NoCI|AnHour'`
Expected: FAIL: `undefined: github.Verdict`.

- [ ] **Step 3: Write the verdict**

`pkg/github/ci.go`:

```go
package github

import "time"

// The CI verdict (RFC-0054): what a commit's other checks and statuses on
// GitHub say, for wait for CI. Check suites are not read: the empty suite
// every installed App gets on every commit never holds one.

// CheckRun is a check run on a commit, as GitHub lists it.
type CheckRun struct {
	ID   int64
	Name string
	// Status is queued, in_progress, completed, waiting, requested or
	// pending; Conclusion is set once it is completed.
	Status     string
	Conclusion string
	// AppID is the App that made it.
	AppID int64
}

// CommitStatus is a commit status: a context and its state (pending,
// success, failure or error).
type CommitStatus struct {
	Context string
	State   string
}

// The states of a verdict.
const (
	CIPending  = "pending"
	CIPassed   = "passed"
	CIFailed   = "failed"
	CINone     = "none"
	CITimedOut = "timed-out"
)

const (
	// NoCIAfter: a commit with no check run and no status this long after
	// it was held has no CI, and deploys.
	NoCIAfter = 60 * time.Second
	// GiveUpAfter: a commit whose CI is still pending this long after it
	// was held is not deployed.
	GiveUpAfter = time.Hour
)

// CIVerdict is what a commit's CI says. Failing names the first check run
// or status that failed.
type CIVerdict struct {
	State   string
	Failing string
}

// Verdict judges a commit's check runs and statuses, leaving out the runs
// of the App appID, shpyrd's own. since is when the commit was held.
func Verdict(runs []CheckRun, statuses []CommitStatus, appID int64, since, now time.Time) CIVerdict {
	seen, done := 0, true
	for _, r := range runs {
		if appID != 0 && r.AppID == appID {
			continue
		}
		seen++
		if r.Status != "completed" {
			done = false
			continue
		}
		switch r.Conclusion {
		case "failure", "cancelled", "timed_out", "action_required", "startup_failure":
			return CIVerdict{State: CIFailed, Failing: r.Name}
		case "success", "neutral", "skipped":
		default:
			done = false
		}
	}
	for _, s := range statuses {
		seen++
		switch s.State {
		case "failure", "error":
			return CIVerdict{State: CIFailed, Failing: s.Context}
		case "success":
		default:
			done = false
		}
	}
	switch {
	case seen > 0 && done:
		return CIVerdict{State: CIPassed}
	case seen == 0 && now.Sub(since) >= NoCIAfter:
		return CIVerdict{State: CINone}
	}
	return Unanswered(since, now)
}

// Unanswered is the verdict while nothing decides it, GitHub not answering
// included: pending, or given up an hour after since.
func Unanswered(since, now time.Time) CIVerdict {
	if now.Sub(since) >= GiveUpAfter {
		return CIVerdict{State: CITimedOut}
	}
	return CIVerdict{State: CIPending}
}
```

- [ ] **Step 4: Run the tests**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp go test ./pkg/github/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/github/ci.go pkg/github/ci_test.go
git commit -m "feat(github): the CI verdict over a commit's check runs and statuses (RFC-0054)

Co-Authored-By: <your model name> <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 4: Token scopes, GitHub's checks, statuses and Deployments, and the fake

**Files:**
- Modify: `pkg/github/client.go` (`tokenKey` ~61, `InstallationToken` ~153-200, `CachedInstallationToken` ~204-212, `Repositories` ~216, `Branches` ~245)
- Create: `pkg/github/checks.go`
- Modify: `pkg/github/githubtest/fake.go` (the `Server` fields, `NewServer`, `serve`, `accessToken`, `installationOf`)
- Create: `pkg/github/githubtest/checks.go`
- Modify: `internal/controller/github.go` (`build`: the two token calls ~262-270)
- Modify: `pkg/github/client_test.go` (every `InstallationToken`/`CachedInstallationToken` call)
- Test: `pkg/github/checks_test.go` (package `github_test`)

**Interfaces:**
- Consumes: `github.CheckRun`, `github.CommitStatus` (Task 3); the existing `Client.call`, `APIError`, `pageOf`, `reply`, `grants` of the fake.
- Produces:
  - `type github.Permissions map[string]string`; `var github.ContentsRead = Permissions{"contents": "read"}`, `github.WatchRead = Permissions{"contents": "read", "checks": "read", "statuses": "read"}`, `github.ReportWrite = Permissions{"checks": "write", "deployments": "write"}`
  - `func (*Client) InstallationToken(ctx, installation int64, repo string, perms Permissions) (Token, error)`; `func (*Client) CachedInstallationToken(installation int64, repo string, perms Permissions) (Token, bool)`
  - `type github.Comparison struct { Status string; Files []string; Capped bool }`; `func (*Client) Compare(ctx, installation int64, owner, repo, base, head string) (Comparison, error)`
  - `func (*Client) CheckRuns(ctx, installation int64, owner, repo, sha string) ([]CheckRun, error)`; `func (*Client) CommitStatuses(ctx, installation int64, owner, repo, sha string) ([]CommitStatus, error)`
  - `type github.CheckRunWrite struct { Name, HeadSHA, Status, Conclusion, DetailsURL, ExternalID, Title, Summary string; StartedAt, CompletedAt time.Time }`; `func (*Client) CreateCheckRun(ctx, installation int64, owner, repo string, w CheckRunWrite) (int64, error)`; `func (*Client) UpdateCheckRun(ctx, installation int64, owner, repo string, id int64, w CheckRunWrite) error`
  - `type github.DeploymentWrite struct { Ref, Environment, Description string }`; `func (*Client) CreateDeployment(ctx, installation int64, owner, repo string, d DeploymentWrite) (int64, error)`
  - `type github.DeploymentStatusWrite struct { State, EnvironmentURL, LogURL, Description string }`; `func (*Client) CreateDeploymentStatus(ctx, installation int64, owner, repo string, id int64, s DeploymentStatusWrite) error`
  - `func github.IsNotFound(err error) bool`
  - `githubtest`: types `File{Name, Previous string}`, `Compare{Status string; Files []File}`, `CheckRun{ID int64; Name, Status, Conclusion string; AppID int64; HeadSHA, Title, Summary, DetailsURL, ExternalID string}`, `Status{Context, State string}`, `Deployment{ID int64; Ref, Environment, Description string; AutoMerge, Transient bool; RequiredContexts []string; Statuses []DeploymentStatus}`, `DeploymentStatus{State, EnvironmentURL, LogURL string}`, `Write{Method, Path string; Permissions map[string]string}`; methods `SetCompare(fullName, base, head string, c Compare)`, `SetCheckRuns(sha string, runs ...CheckRun)`, `SetStatuses(sha string, statuses ...Status)`, `CheckRunsOn(sha string) []CheckRun`, `DeleteCheckRun(id int64)`, `Deployments() []Deployment`, `DeleteDeployment(id int64)`, `Writes() []Write`. Repository calls answer 404 without a token of an installation that grants the repository, and 403 ("Resource not accessible by integration") without the permission; every POST and PATCH is recorded, refused or not.

- [ ] **Step 1: Write the failing tests**

`pkg/github/checks_test.go`:

```go
package github_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/shpyrd-io/shpyrd/pkg/github"
	"github.com/shpyrd-io/shpyrd/pkg/github/githubtest"
)

func TestEachScopeHasItsOwnToken(t *testing.T) {
	c, fake := newClient(t)
	ctx := context.Background()
	clone, err := c.InstallationToken(ctx, 42, "shop", github.ContentsRead)
	if err != nil {
		t.Fatal(err)
	}
	report, err := c.InstallationToken(ctx, 42, "shop", github.ReportWrite)
	if err != nil || report.Value == clone.Value || fake.MintedFor(42) != 2 {
		t.Fatalf("report token %+v, %v; minted %d", report, err, fake.MintedFor(42))
	}
	if body := fake.LastTokenRequest(); fmt.Sprint(body["permissions"]) != "map[checks:write deployments:write]" || fmt.Sprint(body["repositories"]) != "[shop]" {
		t.Errorf("asked for %v", body)
	}
	if again, _ := c.InstallationToken(ctx, 42, "shop", github.Permissions{"contents": "read"}); again.Value != clone.Value || fake.MintedFor(42) != 2 {
		t.Errorf("the clone scope keeps its own token: %+v", again)
	}
	if _, ok := c.CachedInstallationToken(42, "shop", github.WatchRead); ok {
		t.Error("no watch token was asked for")
	}
}

func TestACompareListsARenamedFileUnderBothNames(t *testing.T) {
	c, fake := newClient(t)
	fake.SetCompare("Acme/shop", "aaa111", "bbb222", githubtest.Compare{Status: "ahead", Files: []githubtest.File{
		{Name: "services/api/main.go"}, {Name: "web/new.ts", Previous: "web/old.ts"},
	}})
	got, err := c.Compare(context.Background(), 42, "acme", "shop", "aaa111", "bbb222")
	if err != nil || got.Status != "ahead" || got.Capped || strings.Join(got.Files, ",") != "services/api/main.go,web/new.ts,web/old.ts" {
		t.Fatalf("%+v %v", got, err)
	}
	if body := fake.LastTokenRequest(); fmt.Sprint(body["permissions"]) != "map[checks:read contents:read statuses:read]" {
		t.Errorf("the watcher's scope: %v", body)
	}
}

func TestACompareOfThreeHundredFilesIsCapped(t *testing.T) {
	c, fake := newClient(t)
	var files []githubtest.File
	for i := range 300 {
		files = append(files, githubtest.File{Name: fmt.Sprintf("f%03d.txt", i)})
	}
	fake.SetCompare("Acme/shop", "aaa111", "bbb222", githubtest.Compare{Status: "ahead", Files: files})
	got, err := c.Compare(context.Background(), 42, "acme", "shop", "aaa111", "bbb222")
	if err != nil || !got.Capped {
		t.Errorf("%d files, capped %v, %v", len(got.Files), got.Capped, err)
	}
	if _, err := c.Compare(context.Background(), 42, "acme", "shop", "unknown", "bbb222"); !github.IsNotFound(err) {
		t.Errorf("a commit GitHub does not know: %v", err)
	}
}

func TestTheChecksAndStatusesOfACommitAreRead(t *testing.T) {
	c, fake := newClient(t)
	ctx := context.Background()
	fake.SetCheckRuns("bbb222", githubtest.CheckRun{Name: "test", Status: "completed", Conclusion: "success", AppID: 99},
		githubtest.CheckRun{Name: "lint", Status: "in_progress", AppID: 98})
	fake.SetStatuses("bbb222", githubtest.Status{Context: "ci/circle", State: "pending"})
	runs, err := c.CheckRuns(ctx, 42, "acme", "shop", "bbb222")
	if err != nil || len(runs) != 2 || runs[0].Name != "test" || runs[0].Conclusion != "success" || runs[0].AppID != 99 ||
		runs[1].Status != "in_progress" || runs[1].Conclusion != "" {
		t.Fatalf("%+v %v", runs, err)
	}
	statuses, err := c.CommitStatuses(ctx, 42, "acme", "shop", "bbb222")
	if err != nil || len(statuses) != 1 || statuses[0].Context != "ci/circle" || statuses[0].State != "pending" {
		t.Fatalf("%+v %v", statuses, err)
	}
	if runs, err := c.CheckRuns(ctx, 42, "acme", "shop", "ccc333"); err != nil || len(runs) != 0 {
		t.Errorf("a commit without runs: %+v %v", runs, err)
	}
}

func TestACheckRunIsWrittenWithTheReportTokenAndUpdated(t *testing.T) {
	c, fake := newClient(t)
	ctx := context.Background()
	if _, err := c.InstallationToken(ctx, 42, "shop", github.ContentsRead); err != nil {
		t.Fatal(err)
	}
	id, err := c.CreateCheckRun(ctx, 42, "acme", "shop", github.CheckRunWrite{Name: "shpyrd / shop", HeadSHA: "bbb222", Status: "in_progress",
		Title: "Building", Summary: "Push bbb222 by alice: fix login", DetailsURL: "https://acme.test/projects/shop", ExternalID: "uid-1", StartedAt: t0})
	if err != nil || id == 0 {
		t.Fatalf("create: %d %v", id, err)
	}
	if err := c.UpdateCheckRun(ctx, 42, "acme", "shop", id, github.CheckRunWrite{Status: "completed", Conclusion: "success",
		Title: "Running at https://shop.test", Summary: "Push bbb222 by alice: fix login", CompletedAt: t0}); err != nil {
		t.Fatal(err)
	}
	runs := fake.CheckRunsOn("bbb222")
	if len(runs) != 1 || runs[0].Name != "shpyrd / shop" || runs[0].Status != "completed" || runs[0].Conclusion != "success" ||
		runs[0].Title != "Running at https://shop.test" || runs[0].DetailsURL != "https://acme.test/projects/shop" ||
		runs[0].ExternalID != "uid-1" || runs[0].AppID != githubtest.AppID {
		t.Fatalf("%+v", runs)
	}
	for _, w := range fake.Writes() {
		if fmt.Sprint(w.Permissions) != "map[checks:write deployments:write]" {
			t.Errorf("%s %s with %v", w.Method, w.Path, w.Permissions)
		}
	}
	fake.DeleteCheckRun(id)
	if err := c.UpdateCheckRun(ctx, 42, "acme", "shop", id, github.CheckRunWrite{Status: "in_progress", Title: "Building"}); !github.IsNotFound(err) {
		t.Errorf("a deleted run: %v", err)
	}
}

func TestADeploymentIsMadeForTheCommitAndShpyrdDecides(t *testing.T) {
	c, fake := newClient(t)
	ctx := context.Background()
	id, err := c.CreateDeployment(ctx, 42, "acme", "shop", github.DeploymentWrite{Ref: "bbb222", Environment: "shop", Description: "Push bbb222 by alice"})
	if err != nil || id == 0 {
		t.Fatalf("%d %v", id, err)
	}
	if err := c.CreateDeploymentStatus(ctx, 42, "acme", "shop", id, github.DeploymentStatusWrite{State: "success",
		EnvironmentURL: "https://shop.test", LogURL: "https://acme.test/projects/shop"}); err != nil {
		t.Fatal(err)
	}
	d := fake.Deployments()
	if len(d) != 1 || d[0].Ref != "bbb222" || d[0].Environment != "shop" || d[0].AutoMerge || d[0].Transient ||
		d[0].RequiredContexts == nil || len(d[0].RequiredContexts) != 0 ||
		len(d[0].Statuses) != 1 || d[0].Statuses[0].State != "success" || d[0].Statuses[0].EnvironmentURL != "https://shop.test" {
		t.Fatalf("%+v", d)
	}
	fake.DeleteDeployment(id)
	if err := c.CreateDeploymentStatus(ctx, 42, "acme", "shop", id, github.DeploymentStatusWrite{State: "failure"}); !github.IsNotFound(err) {
		t.Errorf("a deleted Deployment: %v", err)
	}
}

func TestAWriteWithTheCloneTokenIsRefusedAndRecorded(t *testing.T) {
	// What GitHub does to a token without checks: write; the fake keeps
	// the attempt, so the controller's tests can say none happens.
	c, fake := newClient(t)
	ctx := context.Background()
	tok, err := c.InstallationToken(ctx, 42, "shop", github.ContentsRead)
	if err != nil {
		t.Fatal(err)
	}
	status, _ := rawPost(fake, tok.Value, "/repos/acme/shop/check-runs", `{"name":"x","head_sha":"bbb222","status":"queued"}`)
	writes := fake.Writes()
	if status != 403 || len(writes) != 1 || fmt.Sprint(writes[0].Permissions) != "map[contents:read]" {
		t.Errorf("status %d, writes %+v", status, writes)
	}
}
```

and, at its end, the helper `rawPost`:

```go
// rawPost posts to the fake with a token, as a client without scopes would.
func rawPost(fake *githubtest.Server, token, path, body string) (int, error) {
	req, err := http.NewRequest("POST", fake.URL+path, strings.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := fake.Client().Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}
```

(add `"net/http"` to the imports).

- [ ] **Step 2: Run them to see them fail**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp go test ./pkg/github/ -run 'Scope|Compare|ChecksAndStatuses|CheckRunIsWritten|Deployment|CloneToken'`
Expected: FAIL to compile: `undefined: github.ContentsRead`, `too many arguments in call to c.InstallationToken`.

- [ ] **Step 3: Token scopes in the client**

In `pkg/github/client.go`, add `"sort"` to the imports; after `type tokenKey`, replace the type and add the scopes:

```go
type tokenKey struct {
	installation int64
	repo         string
	perms        string
}

// Permissions are what an installation token may do, as GitHub names
// them. The values below are shared: never change them.
type Permissions map[string]string

// The scopes shpyrd asks for (RFC-0054). Each has its own tokens, so a
// build's clone token is never widened.
var (
	// ContentsRead clones, lists branches and repositories.
	ContentsRead = Permissions{"contents": "read"}
	// WatchRead compares commits and reads their checks and statuses: the
	// source watcher.
	WatchRead = Permissions{"contents": "read", "checks": "read", "statuses": "read"}
	// ReportWrite writes check runs and Deployments: the reporter.
	ReportWrite = Permissions{"checks": "write", "deployments": "write"}
)

// key is the scope as a cache key: "checks=read,contents=read".
func (p Permissions) key() string {
	keys := make([]string, 0, len(p))
	for k, v := range p {
		keys = append(keys, k+"="+v)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}
```

`InstallationToken` takes the scope; its doc comment gains "with the permissions perms (ContentsRead when empty), each scope kept apart", and its first lines and body become:

```go
func (c *Client) InstallationToken(ctx context.Context, installation int64, repo string, perms Permissions) (Token, error) {
	if len(perms) == 0 {
		perms = ContentsRead
	}
	key := tokenKey{installation, strings.ToLower(repo), perms.key()}
```

and, further down, the request body line becomes:

```go
	body := map[string]any{"permissions": map[string]string(perms)}
```

`CachedInstallationToken`:

```go
// CachedInstallationToken is the token InstallationToken would give for
// the scope without calling GitHub: one kept and not yet due for renewal.
func (c *Client) CachedInstallationToken(installation int64, repo string, perms Permissions) (Token, bool) {
	if len(perms) == 0 {
		perms = ContentsRead
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.tokens[tokenKey{installation, strings.ToLower(repo), perms.key()}]
	if !ok || !c.now().Before(t.renewAt()) {
		return Token{}, false
	}
	return t, true
}
```

In `Repositories`: `tok, err := c.InstallationToken(ctx, installation, "", ContentsRead)`. In `Branches`: `tok, err := c.InstallationToken(ctx, installation, repo, ContentsRead)`.

The existing tests call the old signature; give each call the clone scope:

```bash
perl -0pi -e 's/(\.InstallationToken\((?:[^()]|\([^()]*\))*?)\)/$1, github.ContentsRead)/g; s/(\.CachedInstallationToken\([^()]*)\)/$1, github.ContentsRead)/g' pkg/github/client_test.go
```

In `internal/controller/github.go`, `build`, the two calls become:

```go
	tok, cached := cl.CachedInstallationToken(inst.ID, name, github.ContentsRead)
```

```go
		tok, err = cl.InstallationToken(callCtx, inst.ID, name, github.ContentsRead)
```

- [ ] **Step 4: The calls**

`pkg/github/checks.go`:

```go
package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// A repository's checks, statuses and Deployments (RFC-0054): the source
// watcher reads with WatchRead, the reporter writes with ReportWrite, each
// with a token scoped to the one repository.

// maxCompareFiles is how many files GitHub's compare lists at most.
const maxCompareFiles = 300

// IsNotFound says GitHub answered 404: a check run or Deployment deleted
// on GitHub, or a commit it does not know.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound
}

// repoURL is the API's address of a repository's path.
func (c *Client) repoURL(owner, repo, path string) string {
	return fmt.Sprintf("%s/repos/%s/%s/%s", c.api, url.PathEscape(owner), url.PathEscape(repo), path)
}

// Comparison is GitHub's compare of two commits: ahead, behind, identical
// or diverged, and every file changed, a renamed one under both names.
type Comparison struct {
	Status string
	Files  []string
	// Capped: GitHub listed as many files as it lists; there may be more.
	Capped bool
}

// Compare compares base with head (base...head).
func (c *Client) Compare(ctx context.Context, installation int64, owner, repo, base, head string) (Comparison, error) {
	tok, err := c.InstallationToken(ctx, installation, repo, WatchRead)
	if err != nil {
		return Comparison{}, err
	}
	var res struct {
		Status string `json:"status"`
		Files  []struct {
			Filename string `json:"filename"`
			Previous string `json:"previous_filename"`
		} `json:"files"`
	}
	if err := c.call(ctx, "GET", c.repoURL(owner, repo, "compare/"+url.PathEscape(base)+"..."+url.PathEscape(head)), tok.Value, nil, &res); err != nil {
		return Comparison{}, err
	}
	out := Comparison{Status: res.Status, Capped: len(res.Files) >= maxCompareFiles}
	for _, f := range res.Files {
		out.Files = append(out.Files, f.Filename)
		if f.Previous != "" {
			out.Files = append(out.Files, f.Previous)
		}
	}
	return out, nil
}

// CheckRuns are the check runs on a commit, every App's.
func (c *Client) CheckRuns(ctx context.Context, installation int64, owner, repo, sha string) ([]CheckRun, error) {
	tok, err := c.InstallationToken(ctx, installation, repo, WatchRead)
	if err != nil {
		return nil, err
	}
	out := []CheckRun{}
	for page := 1; page <= 10; page++ {
		var res struct {
			Total int `json:"total_count"`
			Runs  []struct {
				ID         int64  `json:"id"`
				Name       string `json:"name"`
				Status     string `json:"status"`
				Conclusion string `json:"conclusion"`
				App        struct {
					ID int64 `json:"id"`
				} `json:"app"`
			} `json:"check_runs"`
		}
		endpoint := c.repoURL(owner, repo, fmt.Sprintf("commits/%s/check-runs?per_page=%d&page=%d", url.PathEscape(sha), perPage, page))
		if err := c.call(ctx, "GET", endpoint, tok.Value, nil, &res); err != nil {
			return nil, err
		}
		for _, r := range res.Runs {
			out = append(out, CheckRun{ID: r.ID, Name: r.Name, Status: r.Status, Conclusion: r.Conclusion, AppID: r.App.ID})
		}
		if len(res.Runs) == 0 || len(out) >= res.Total {
			break
		}
	}
	return out, nil
}

// CommitStatuses are the latest status of each context on a commit.
func (c *Client) CommitStatuses(ctx context.Context, installation int64, owner, repo, sha string) ([]CommitStatus, error) {
	tok, err := c.InstallationToken(ctx, installation, repo, WatchRead)
	if err != nil {
		return nil, err
	}
	out := []CommitStatus{}
	for page := 1; page <= 10; page++ {
		var res struct {
			Total    int `json:"total_count"`
			Statuses []struct {
				Context string `json:"context"`
				State   string `json:"state"`
			} `json:"statuses"`
		}
		endpoint := c.repoURL(owner, repo, fmt.Sprintf("commits/%s/status?per_page=%d&page=%d", url.PathEscape(sha), perPage, page))
		if err := c.call(ctx, "GET", endpoint, tok.Value, nil, &res); err != nil {
			return nil, err
		}
		for _, s := range res.Statuses {
			out = append(out, CommitStatus{Context: s.Context, State: s.State})
		}
		if len(res.Statuses) == 0 || len(out) >= res.Total {
			break
		}
	}
	return out, nil
}

// CheckRunWrite is a check run as shpyrd writes it. Name and HeadSHA are
// read on create only; Conclusion and CompletedAt when Status is completed.
type CheckRunWrite struct {
	Name, HeadSHA          string
	Status, Conclusion     string
	DetailsURL, ExternalID string
	Title, Summary         string
	StartedAt, CompletedAt time.Time
}

func (w CheckRunWrite) body(create bool) map[string]any {
	b := map[string]any{"status": w.Status, "output": map[string]string{"title": w.Title, "summary": w.Summary}}
	if create {
		b["name"], b["head_sha"] = w.Name, w.HeadSHA
	}
	if w.DetailsURL != "" {
		b["details_url"] = w.DetailsURL
	}
	if w.ExternalID != "" {
		b["external_id"] = w.ExternalID
	}
	if !w.StartedAt.IsZero() {
		b["started_at"] = w.StartedAt.UTC().Format(time.RFC3339)
	}
	if w.Status == "completed" {
		b["conclusion"] = w.Conclusion
		if !w.CompletedAt.IsZero() {
			b["completed_at"] = w.CompletedAt.UTC().Format(time.RFC3339)
		}
	}
	return b
}

// CreateCheckRun makes a check run on w.HeadSHA and gives its id.
func (c *Client) CreateCheckRun(ctx context.Context, installation int64, owner, repo string, w CheckRunWrite) (int64, error) {
	tok, err := c.InstallationToken(ctx, installation, repo, ReportWrite)
	if err != nil {
		return 0, err
	}
	var res struct {
		ID int64 `json:"id"`
	}
	if err := c.call(ctx, "POST", c.repoURL(owner, repo, "check-runs"), tok.Value, w.body(true), &res); err != nil {
		return 0, err
	}
	if res.ID == 0 {
		return 0, errors.New("GitHub made the check run but gave no id")
	}
	return res.ID, nil
}

// UpdateCheckRun changes a check run; a deleted one is IsNotFound.
func (c *Client) UpdateCheckRun(ctx context.Context, installation int64, owner, repo string, id int64, w CheckRunWrite) error {
	tok, err := c.InstallationToken(ctx, installation, repo, ReportWrite)
	if err != nil {
		return err
	}
	return c.call(ctx, "PATCH", c.repoURL(owner, repo, fmt.Sprintf("check-runs/%d", id)), tok.Value, w.body(false), nil)
}

// DeploymentWrite is a Deployment as shpyrd makes it: shpyrd decided to
// deploy, so GitHub merges nothing and waits for no context.
type DeploymentWrite struct {
	Ref, Environment, Description string
}

// CreateDeployment makes a Deployment of d.Ref and gives its id.
func (c *Client) CreateDeployment(ctx context.Context, installation int64, owner, repo string, d DeploymentWrite) (int64, error) {
	tok, err := c.InstallationToken(ctx, installation, repo, ReportWrite)
	if err != nil {
		return 0, err
	}
	body := map[string]any{"ref": d.Ref, "environment": d.Environment, "description": d.Description,
		"auto_merge": false, "required_contexts": []string{}, "transient_environment": false}
	var res struct {
		ID      int64  `json:"id"`
		Message string `json:"message"`
	}
	if err := c.call(ctx, "POST", c.repoURL(owner, repo, "deployments"), tok.Value, body, &res); err != nil {
		return 0, err
	}
	if res.ID == 0 {
		return 0, fmt.Errorf("GitHub made no Deployment: %s", firstNonEmpty(res.Message, "it gave no id"))
	}
	return res.ID, nil
}

// DeploymentStatusWrite is a Deployment's new state: in_progress, success
// or failure. GitHub marks the previous Deployment inactive by itself.
type DeploymentStatusWrite struct {
	State, EnvironmentURL, LogURL, Description string
}

// CreateDeploymentStatus adds a status to a Deployment; a deleted one is
// IsNotFound.
func (c *Client) CreateDeploymentStatus(ctx context.Context, installation int64, owner, repo string, id int64, s DeploymentStatusWrite) error {
	tok, err := c.InstallationToken(ctx, installation, repo, ReportWrite)
	if err != nil {
		return err
	}
	body := map[string]any{"state": s.State}
	if s.EnvironmentURL != "" {
		body["environment_url"] = s.EnvironmentURL
	}
	if s.LogURL != "" {
		body["log_url"] = s.LogURL
	}
	if s.Description != "" {
		body["description"] = s.Description
	}
	return c.call(ctx, "POST", c.repoURL(owner, repo, fmt.Sprintf("deployments/%d/statuses", id)), tok.Value, body, nil)
}
```

- [ ] **Step 5: The fake remembers each token's scope**

In `pkg/github/githubtest/fake.go`:

The `Server` struct's `tokens map[string]int64 // installation token -> installation` becomes, and new fields follow `calls`:

```go
	tokens         map[string]tokenGrant // installation token -> what it may do
	lastBody       map[string]any
	calls          []string
	compares       map[string]Compare
	runs           map[string][]CheckRun // by commit, lower case
	statuses       map[string][]Status   // by commit, lower case
	deployments    []Deployment
	nextID         int64
	writes         []Write
```

and, after the struct:

```go
// tokenGrant is an installation token: its installation, the repositories
// it was asked for (none: all of the installation's) and its permissions.
type tokenGrant struct {
	installation int64
	repos        []string
	perms        map[string]string
}
```

`NewServer` initialises them: replace `tokens:   map[string]int64{},` with

```go
		tokens:   map[string]tokenGrant{},
		compares: map[string]Compare{},
		runs:     map[string][]CheckRun{},
		statuses: map[string][]Status{},
```

In `accessToken`, the decoded body gains the permissions:

```go
	var body struct {
		Repositories []string          `json:"repositories"`
		Permissions  map[string]string `json:"permissions"`
	}
```

and `f.tokens[tok] = installation` becomes:

```go
	f.tokens[tok] = tokenGrant{installation: installation, repos: body.Repositories, perms: body.Permissions}
```

`installationOf` becomes:

```go
func (f *Server) installationOf(r *http.Request) (int64, bool) {
	g, ok := f.tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
	return g.installation, ok
}
```

In `serve`, after the `/branches` case, add:

```go
	case strings.HasPrefix(p, "/repos/"):
		f.repoCall(w, r, p)
```

`pkg/github/githubtest/checks.go`:

```go
package githubtest

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// Checks, statuses and Deployments (RFC-0054): what the source watcher
// reads and the reporter writes. Each repository call needs a token of an
// installation that grants the repository (404 otherwise) with the
// permission GitHub asks for (403 otherwise).

// File is a file a compare lists; Previous is its old name when renamed.
type File struct{ Name, Previous string }

// Compare is GitHub's answer to a compare.
type Compare struct {
	Status string
	Files  []File
}

// CheckRun is a check run on a commit: other tools' are set with
// SetCheckRuns, shpyrd's are made through the API (AppID is then AppID).
type CheckRun struct {
	ID                       int64
	Name, Status, Conclusion string
	AppID                    int64
	HeadSHA, Title, Summary  string
	DetailsURL, ExternalID   string
	viaAPI                   bool
}

// Status is a commit status.
type Status struct{ Context, State string }

// Deployment is one made through the API, with its statuses in order.
type Deployment struct {
	ID                            int64
	Ref, Environment, Description string
	AutoMerge, Transient          bool
	RequiredContexts              []string
	Statuses                      []DeploymentStatus
}

// DeploymentStatus is one status of a Deployment.
type DeploymentStatus struct{ State, EnvironmentURL, LogURL string }

// Write is a call that would change something, refused or not, with the
// permissions of the token that made it (nil without one).
type Write struct {
	Method, Path string
	Permissions  map[string]string
}

func compareKey(fullName, base, head string) string {
	return strings.ToLower(fullName) + " " + strings.ToLower(base) + "..." + strings.ToLower(head)
}

// SetCompare is GitHub's answer to base...head on the repository.
func (f *Server) SetCompare(fullName, base, head string, c Compare) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.compares[compareKey(fullName, base, head)] = c
}

// SetCheckRuns sets the check runs other tools made on sha; the ones made
// through the API stay.
func (f *Server) SetCheckRuns(sha string, runs ...CheckRun) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sha = strings.ToLower(sha)
	kept := []CheckRun{}
	for _, r := range f.runs[sha] {
		if r.viaAPI {
			kept = append(kept, r)
		}
	}
	for _, r := range runs {
		f.nextID++
		r.ID, r.HeadSHA, r.viaAPI = f.nextID, sha, false
		kept = append(kept, r)
	}
	f.runs[sha] = kept
}

// SetStatuses sets the statuses on sha.
func (f *Server) SetStatuses(sha string, statuses ...Status) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statuses[strings.ToLower(sha)] = statuses
}

// CheckRunsOn are the check runs on sha, other tools' and shpyrd's.
func (f *Server) CheckRunsOn(sha string) []CheckRun {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]CheckRun(nil), f.runs[strings.ToLower(sha)]...)
}

// DeleteCheckRun forgets a check run, as when it is deleted on GitHub.
func (f *Server) DeleteCheckRun(id int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for sha, runs := range f.runs {
		for i := range runs {
			if runs[i].ID == id {
				f.runs[sha] = append(runs[:i:i], runs[i+1:]...)
				return
			}
		}
	}
}

// Deployments are the Deployments made, oldest first.
func (f *Server) Deployments() []Deployment {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Deployment, len(f.deployments))
	for i, d := range f.deployments {
		d.Statuses = append([]DeploymentStatus(nil), d.Statuses...)
		out[i] = d
	}
	return out
}

// DeleteDeployment forgets a Deployment, as when it is deleted on GitHub.
func (f *Server) DeleteDeployment(id int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.deployments {
		if f.deployments[i].ID == id {
			f.deployments = append(f.deployments[:i:i], f.deployments[i+1:]...)
			return
		}
	}
}

// Writes are the POST and PATCH calls on repositories, in order.
func (f *Server) Writes() []Write {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Write(nil), f.writes...)
}

// may says the token has the permission at the level (write covers read).
func may(g tokenGrant, perm, level string) bool {
	v := g.perms[perm]
	return v == "write" || (level == "read" && v == "read")
}

func names(repos []string, name string) bool {
	for _, r := range repos {
		if strings.EqualFold(r, name) {
			return true
		}
	}
	return false
}

type checkRunBody struct {
	Name       string `json:"name"`
	HeadSHA    string `json:"head_sha"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	DetailsURL string `json:"details_url"`
	ExternalID string `json:"external_id"`
	Output     struct {
		Title   string `json:"title"`
		Summary string `json:"summary"`
	} `json:"output"`
}

func (b checkRunBody) apply(r *CheckRun) {
	for _, set := range []struct {
		to   *string
		from string
	}{{&r.Status, b.Status}, {&r.Conclusion, b.Conclusion}, {&r.DetailsURL, b.DetailsURL}, {&r.ExternalID, b.ExternalID},
		{&r.Title, b.Output.Title}, {&r.Summary, b.Output.Summary}} {
		if set.from != "" {
			*set.to = set.from
		}
	}
}

// repoCall answers /repos/{owner}/{repo}/...: a compare, a commit's check
// runs and status, and the check runs and Deployments written.
func (f *Server) repoCall(w http.ResponseWriter, r *http.Request, p string) {
	parts := strings.SplitN(strings.TrimPrefix(p, "/repos/"), "/", 3)
	if len(parts) < 3 {
		reply(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	fullName, rest := parts[0]+"/"+parts[1], parts[2]
	grant, hasToken := f.tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
	if r.Method != "GET" {
		var perms map[string]string
		if hasToken {
			perms = grant.perms
		}
		f.writes = append(f.writes, Write{Method: r.Method, Path: p, Permissions: perms})
	}
	if !hasToken || !grants(f.repos[grant.installation], parts[1]) || (len(grant.repos) > 0 && !names(grant.repos, parts[1])) {
		reply(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	need := func(perm, level string) bool {
		if may(grant, perm, level) {
			return true
		}
		reply(w, http.StatusForbidden, map[string]string{"message": "Resource not accessible by integration"})
		return false
	}
	seg := strings.Split(rest, "/")
	raw, _ := io.ReadAll(r.Body)
	switch {
	case r.Method == "GET" && len(seg) == 2 && seg[0] == "compare":
		if !need("contents", "read") {
			return
		}
		base, head, _ := strings.Cut(seg[1], "...")
		c, ok := f.compares[compareKey(fullName, base, head)]
		if !ok {
			reply(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
			return
		}
		files := []map[string]string{}
		for _, fl := range c.Files {
			m := map[string]string{"filename": fl.Name, "status": "modified"}
			if fl.Previous != "" {
				m["previous_filename"], m["status"] = fl.Previous, "renamed"
			}
			files = append(files, m)
		}
		reply(w, http.StatusOK, map[string]any{"status": c.Status, "files": files})
	case r.Method == "GET" && len(seg) == 3 && seg[0] == "commits" && seg[2] == "check-runs":
		if !need("checks", "read") {
			return
		}
		runs := f.runs[strings.ToLower(seg[1])]
		out := []map[string]any{}
		for _, run := range pageOf(runs, r) {
			var conclusion any
			if run.Conclusion != "" {
				conclusion = run.Conclusion
			}
			out = append(out, map[string]any{"id": run.ID, "name": run.Name, "status": run.Status, "conclusion": conclusion, "app": map[string]any{"id": run.AppID}})
		}
		reply(w, http.StatusOK, map[string]any{"total_count": len(runs), "check_runs": out})
	case r.Method == "GET" && len(seg) == 3 && seg[0] == "commits" && seg[2] == "status":
		if !need("statuses", "read") {
			return
		}
		all := f.statuses[strings.ToLower(seg[1])]
		state := "success"
		if len(all) == 0 {
			state = "pending"
		}
		out := []map[string]string{}
		for _, s := range all {
			switch {
			case s.State == "failure" || s.State == "error":
				state = "failure"
			case s.State == "pending" && state != "failure":
				state = "pending"
			}
		}
		for _, s := range pageOf(all, r) {
			out = append(out, map[string]string{"context": s.Context, "state": s.State})
		}
		reply(w, http.StatusOK, map[string]any{"state": state, "total_count": len(all), "statuses": out})
	case r.Method == "POST" && rest == "check-runs":
		if !need("checks", "write") {
			return
		}
		var in checkRunBody
		_ = json.Unmarshal(raw, &in)
		f.nextID++
		run := CheckRun{ID: f.nextID, Name: in.Name, HeadSHA: strings.ToLower(in.HeadSHA), AppID: AppID, viaAPI: true}
		in.apply(&run)
		f.runs[run.HeadSHA] = append(f.runs[run.HeadSHA], run)
		reply(w, http.StatusCreated, map[string]any{"id": run.ID})
	case r.Method == "PATCH" && len(seg) == 2 && seg[0] == "check-runs":
		if !need("checks", "write") {
			return
		}
		id, _ := strconv.ParseInt(seg[1], 10, 64)
		var in checkRunBody
		_ = json.Unmarshal(raw, &in)
		for sha := range f.runs {
			for i := range f.runs[sha] {
				if f.runs[sha][i].ID == id {
					in.apply(&f.runs[sha][i])
					reply(w, http.StatusOK, map[string]any{"id": id})
					return
				}
			}
		}
		reply(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
	case r.Method == "POST" && rest == "deployments":
		if !need("deployments", "write") {
			return
		}
		var in struct {
			Ref              string   `json:"ref"`
			Environment      string   `json:"environment"`
			Description      string   `json:"description"`
			AutoMerge        bool     `json:"auto_merge"`
			RequiredContexts []string `json:"required_contexts"`
			Transient        bool     `json:"transient_environment"`
		}
		_ = json.Unmarshal(raw, &in)
		f.nextID++
		f.deployments = append(f.deployments, Deployment{ID: f.nextID, Ref: in.Ref, Environment: in.Environment, Description: in.Description,
			AutoMerge: in.AutoMerge, Transient: in.Transient, RequiredContexts: in.RequiredContexts})
		reply(w, http.StatusCreated, map[string]any{"id": f.nextID})
	case r.Method == "POST" && len(seg) == 3 && seg[0] == "deployments" && seg[2] == "statuses":
		if !need("deployments", "write") {
			return
		}
		id, _ := strconv.ParseInt(seg[1], 10, 64)
		var in struct {
			State          string `json:"state"`
			EnvironmentURL string `json:"environment_url"`
			LogURL         string `json:"log_url"`
		}
		_ = json.Unmarshal(raw, &in)
		for i := range f.deployments {
			if f.deployments[i].ID == id {
				f.deployments[i].Statuses = append(f.deployments[i].Statuses, DeploymentStatus{State: in.State, EnvironmentURL: in.EnvironmentURL, LogURL: in.LogURL})
				f.nextID++
				reply(w, http.StatusCreated, map[string]any{"id": f.nextID})
				return
			}
		}
		reply(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
	default:
		reply(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
	}
}
```

Update the package comment at the top of `fake.go`: after "the manifest conversion" add ", a repository's compare, check runs, statuses and Deployments".

- [ ] **Step 6: Run the tests**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp go build ./... && GOTMPDIR=/home/nkr/.cache/gotmp go vet ./pkg/github/... ./internal/controller/ && GOTMPDIR=/home/nkr/.cache/gotmp go test ./pkg/github/... ./internal/controller/ ./pkg/api/`
Expected: PASS, the PR 3 tests included (`TestAnInstallationTokenIsForOneRepositoryAndKeptUntilItAges` still sees `map[contents:read]`).

- [ ] **Step 7: Commit**

```bash
git add pkg/github/client.go pkg/github/checks.go pkg/github/checks_test.go pkg/github/client_test.go pkg/github/githubtest/fake.go pkg/github/githubtest/checks.go internal/controller/github.go
git commit -m "feat(github): token scopes; compare, checks, statuses and Deployments; the fake answers them (RFC-0054)

Co-Authored-By: <your model name> <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 5: The watcher's watch paths

**Files:**
- Modify: `internal/controller/github.go` (add `githubRepoRef` and `cover` after `build`)
- Create: `internal/controller/source_gates.go`
- Modify: `internal/controller/source_controller.go` (`sourceEvent`, `sourceUpdate`, `Reconcile` around `u := decideSource(...)`, `decideSource`, `auditUpdate`)
- Test: `internal/controller/source_gates_test.go`

**Interfaces:**
- Consumes: `gitauth.MatchWatchPaths`, `gitauth.ReasonNoWatchedChanges` (Tasks 1-2); `github.Client.Compare`, `github.IsNotFound` (Task 4); PR 3's `GitHubApp.backingOff`, `GitHubApp.remember`, `workspaceOf`; PR 2's `pushNote`, `shortCommit`.
- Produces:
  - `type githubRepoRef struct { client *github.Client; installation int64; owner, name string }`
  - `func (gh *GitHubApp) cover(ctx context.Context, app *shpyrdv1.App) (githubRepoRef, bool)`: nil-safe; a github.com URL, the App configured, an installation of the App's workspace on the owner (case-insensitive), not suspended; no call to GitHub
  - `type sourceGates struct { covered bool; changed *changedFiles }`, `type changedFiles struct { files []string; untrusted string }`
  - `type githubAsk struct { sha string; changed bool }`, `func (githubAsk) any() bool`
  - `sourceEvent.gates sourceGates`; `sourceUpdate.ask githubAsk`, `sourceUpdate.target`, `sourceUpdate.reason string`
  - `func hasGates(app *shpyrdv1.App) bool`
  - `func gateCommit(app *shpyrdv1.App, u *sourceUpdate, rec gitauth.SourceRecord, sha string, ev sourceEvent, now time.Time) (string, bool)`
  - `func notDeploy(u *sourceUpdate, rec gitauth.SourceRecord, sha, reason string)`
  - `func (r *SourceReconciler) gather(ctx context.Context, app *shpyrdv1.App, ev *sourceEvent, ask githubAsk, now time.Time)`
  - `const githubGateTimeout = 15 * time.Second`

- [ ] **Step 1: Write the failing tests**

`internal/controller/source_gates_test.go`:

```go
package controller

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
	"github.com/shpyrd-io/shpyrd/pkg/github/githubtest"
)

const thirdCommit = "fedcba9876543210fedcba9876543210fedcba98"

// gatedWatcher is the source watcher on a platform with the fake's App,
// its installation 42 on Acme connected; its clock is moved with advance.
func gatedWatcher(t *testing.T, ls *fakeLsRemote, app *shpyrdv1.App) (*SourceReconciler, *githubtest.Server) {
	t.Helper()
	fake, st := connectedGitHub(t)
	r, c := newSourceReconciler(t, ls, fake.Secret("shpyrd-system"), app)
	r.GitHub = &GitHubApp{Source: fake.Source(c, "shpyrd-system"), Installations: st}
	r.Now = fake.Time
	return r, fake
}

// advance moves the watcher's and the fake's clock.
func advance(fake *githubtest.Server, d time.Duration) { fake.SetNow(fake.Time().Add(d)) }

// shopWith is the shop app on github.com/acme/shop, its branch main
// pinned at testCommit, last seen through a webhook a minute ago.
func shopWith(git shpyrdv1.GitSource) *shpyrdv1.App {
	app := gitApp("shop", "https://github.com/acme/shop")
	git.URL = "https://github.com/acme/shop"
	app.Spec.Source.Git = &git
	pinGit(app, testCommit)
	return withRecord(app, gitauth.SourceRecord{Ref: "main", Latest: testCommit, Via: "push",
		CheckedAt: at(sourceNow.Add(-time.Minute)), LastWebhookAt: at(sourceNow.Add(-time.Minute))})
}

// pushed leaves a push of sha after before on app, as the webhook does.
func pushed(app *shpyrdv1.App, sha, before, message string, when time.Time) *shpyrdv1.App {
	return withPush(app, gitauth.Push{Ref: "refs/heads/main", SHA: sha, Before: before, Message: message, Author: "alice", At: when})
}

func calledGitHub(fake *githubtest.Server, prefix string) int {
	n := 0
	for _, c := range fake.Calls() {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func TestWatchPathsIgnoreACommitAndCountItInTheNextCompare(t *testing.T) {
	app := pushed(shopWith(shpyrdv1.GitSource{WatchPaths: []string{"services/api/**"}}), otherCommit, testCommit, "docs: typo", sourceNow)
	r, fake := gatedWatcher(t, &fakeLsRemote{}, app)
	heard := heardAudits(t)
	fake.SetCompare("acme/shop", testCommit, otherCommit, githubtest.Compare{Status: "ahead", Files: []githubtest.File{{Name: "README.md"}}})
	_, got := reconcileSource(t, r, app)
	rec := sourceRecord(got)
	if got.PinnedCommit() != testCommit || rec.NotDeployed != otherCommit || rec.NotDeployedReason != gitauth.ReasonNoWatchedChanges || rec.Latest != otherCommit {
		t.Fatalf("pin %s, record %+v", got.PinnedCommit(), rec)
	}
	if len(*heard) != 1 || (*heard)[0].Action != "git.skip" || (*heard)[0].Target != "89abcde" || (*heard)[0].Detail != gitauth.ReasonNoWatchedChanges {
		t.Errorf("audited %+v", *heard)
	}

	// The next push changes the API: the compare runs from the commit that
	// runs, so the README change counts with it.
	fake.SetCompare("acme/shop", testCommit, thirdCommit, githubtest.Compare{Status: "ahead", Files: []githubtest.File{{Name: "README.md"}, {Name: "services/api/main.go"}}})
	got = pushed(got, thirdCommit, otherCommit, "fix the api", sourceNow.Add(time.Minute))
	if err := r.Update(context.Background(), got); err != nil {
		t.Fatal(err)
	}
	advance(fake, time.Minute)
	_, got = reconcileSource(t, r, got)
	rec = sourceRecord(got)
	if got.PinnedCommit() != thirdCommit || rec.NotDeployed != "" || got.Annotations[shpyrdv1.AnnotationReleaseNote] != "Push fedcba9 by alice: fix the api" {
		t.Fatalf("pin %s, note %q, record %+v", got.PinnedCommit(), got.Annotations[shpyrdv1.AnnotationReleaseNote], rec)
	}
	if calledGitHub(fake, "GET /repos/acme/shop/compare/"+testCommit+"..."+thirdCommit) != 1 {
		t.Errorf("calls %v", fake.Calls())
	}
}

func TestWatchPathsAreNotCheckedWhenGitHubsListCannotBeTrusted(t *testing.T) {
	var many []githubtest.File
	for i := range 300 {
		many = append(many, githubtest.File{Name: fmt.Sprintf("docs/%03d.md", i)})
	}
	for _, c := range []struct {
		name    string
		compare *githubtest.Compare
		note    string
	}{
		{"a commit GitHub does not know", nil, "GitHub's compare failed"},
		{"300 files", &githubtest.Compare{Status: "ahead", Files: many}, "more than 300 files changed"},
		{"a rewritten history", &githubtest.Compare{Status: "diverged", Files: []githubtest.File{{Name: "README.md"}}}, "the branch's history was rewritten"},
		{"a branch moved back", &githubtest.Compare{Status: "behind"}, "the branch's history was rewritten"},
	} {
		app := pushed(shopWith(shpyrdv1.GitSource{WatchPaths: []string{"services/api/**"}}), otherCommit, testCommit, "fix login", sourceNow)
		r, fake := gatedWatcher(t, &fakeLsRemote{}, app)
		if c.compare != nil {
			fake.SetCompare("acme/shop", testCommit, otherCommit, *c.compare)
		}
		_, got := reconcileSource(t, r, app)
		want := "Push 89abcde by alice: fix login (watch paths not checked: " + c.note + ")"
		if got.PinnedCommit() != otherCommit || got.Annotations[shpyrdv1.AnnotationReleaseNote] != want {
			t.Errorf("%s: pin %s, note %q", c.name, got.PinnedCommit(), got.Annotations[shpyrdv1.AnnotationReleaseNote])
		}
	}
}

func TestAPersonsDeployIgnoresWatchPathsAndWaitForCI(t *testing.T) {
	app := gitApp("shop", "https://github.com/acme/shop")
	app.Spec.Source.Git.WatchPaths, app.Spec.Source.Git.WaitForChecks = []string{"services/api/**"}, true
	app.Annotations = map[string]string{shpyrdv1.AnnotationDeployedBy: "ana@acme.test"}
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": otherCommit}}
	r, fake := gatedWatcher(t, ls, app)
	_, got := reconcileSource(t, r, app)
	if got.PinnedCommit() != otherCommit || sourceRecord(got).Held != "" {
		t.Errorf("pin %s, record %+v", got.PinnedCommit(), sourceRecord(got))
	}
	if n := calledGitHub(fake, "GET /repos/"); n != 0 {
		t.Errorf("a person's deploy asks GitHub nothing about the commit: %v", fake.Calls())
	}
}

func TestTheFirstCommitOfABranchIgnoresWatchPaths(t *testing.T) {
	app := gitApp("shop", "https://github.com/acme/shop")
	app.Spec.Source.Git.WatchPaths = []string{"services/api/**"}
	r, fake := gatedWatcher(t, &fakeLsRemote{refs: map[string]string{"refs/heads/main": otherCommit}}, app)
	if _, got := reconcileSource(t, r, app); got.PinnedCommit() != otherCommit || calledGitHub(fake, "GET /repos/") != 0 {
		t.Errorf("pin %s, calls %v", got.PinnedCommit(), fake.Calls())
	}
}

func TestWatchPathsAreIgnoredWhereTheGitHubAppDoesNotReach(t *testing.T) {
	app := pushed(shopWith(shpyrdv1.GitSource{WatchPaths: []string{"services/api/**"}}), otherCommit, testCommit, "docs", sourceNow)
	r, _ := newSourceReconciler(t, &fakeLsRemote{}, app)
	if _, got := reconcileSource(t, r, app); got.PinnedCommit() != otherCommit || got.Annotations[shpyrdv1.AnnotationReleaseNote] != "Push 89abcde by alice: docs" {
		t.Errorf("without the App every push deploys: pin %s, note %q", got.PinnedCommit(), got.Annotations[shpyrdv1.AnnotationReleaseNote])
	}
}

func TestACommitNotDeployedStaysSoWhenTheBranchIsReadAgain(t *testing.T) {
	app := shopWith(shpyrdv1.GitSource{WatchPaths: []string{"services/api/**"}})
	withRecord(app, gitauth.SourceRecord{Ref: "main", Latest: otherCommit, Via: "push", NotDeployed: otherCommit,
		NotDeployedReason: gitauth.ReasonNoWatchedChanges, CheckedAt: at(sourceNow.Add(-3 * time.Hour))})
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": otherCommit}}
	r, fake := gatedWatcher(t, ls, app)
	_, got := reconcileSource(t, r, app)
	if ls.calls != 1 || got.PinnedCommit() != testCommit || sourceRecord(got).NotDeployed != otherCommit || calledGitHub(fake, "GET /repos/") != 0 {
		t.Errorf("listed %d, pin %s, record %+v, calls %v", ls.calls, got.PinnedCommit(), sourceRecord(got), fake.Calls())
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp go test ./internal/controller/ -run 'WatchPaths|PersonsDeploy|FirstCommitOfABranch|NotDeployedStays'`
Expected: FAIL: the watch paths are ignored (`pin 89abcdef…` where `0123456…` was wanted), and `TestWatchPathsAreNotChecked…` notes lack the suffix.

- [ ] **Step 3: `cover`**

Append to `internal/controller/github.go`:

```go
// githubRepoRef is a repository the GitHub App covers: the App's client,
// the installation, and the repository's owner and name.
type githubRepoRef struct {
	client       *github.Client
	installation int64
	owner, name  string
}

// cover is the GitHub App's hold on the App's repository, by the
// credential lookup's test without its call to GitHub: a github.com URL,
// the App configured, and a live, unsuspended installation of the App's
// workspace on the repository's owner. Watch paths, wait for CI and the
// reporter work only then. false when the App does not cover it.
func (gh *GitHubApp) cover(ctx context.Context, app *shpyrdv1.App) (githubRepoRef, bool) {
	if gh == nil || gh.Source == nil || gh.Installations == nil || app.Spec.Source == nil || app.Spec.Source.Git == nil {
		return githubRepoRef{}, false
	}
	repo, err := gitauth.Parse(app.Spec.Source.Git.URL)
	if err != nil || !strings.EqualFold(repo.Host, "github.com") {
		return githubRepoRef{}, false
	}
	owner, name, ok := strings.Cut(repo.Path, "/")
	if !ok || strings.Contains(name, "/") {
		return githubRepoRef{}, false
	}
	cl, err := gh.Source.Client(ctx)
	if err != nil {
		return githubRepoRef{}, false
	}
	list, err := gh.Installations.GitHubInstallations(ctx, workspaceOf(app))
	if err != nil {
		log.FromContext(ctx).Info("could not read the workspace's GitHub connections", "err", err.Error())
		return githubRepoRef{}, false
	}
	for i := range list {
		if strings.EqualFold(list[i].AccountLogin, owner) && list[i].SuspendedAt == nil {
			return githubRepoRef{client: cl, installation: list[i].ID, owner: owner, name: name}, true
		}
	}
	return githubRepoRef{}, false
}
```

- [ ] **Step 4: The gates**

`internal/controller/source_gates.go`:

```go
package controller

import (
	"context"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
	"github.com/shpyrd-io/shpyrd/pkg/github"
)

// Steps 4 and 5 of the source watcher (RFC-0054): watch paths and wait for
// CI, for a repository the GitHub App covers. decideSource stays pure: it
// says what it needs from GitHub (sourceUpdate.ask), Reconcile gathers it
// and decides again.

// githubGateTimeout bounds the calls one decision makes to GitHub.
const githubGateTimeout = 15 * time.Second

// sourceGates are GitHub's answers for steps 4 and 5.
type sourceGates struct {
	// covered: the GitHub App covers the repository; the gates apply only
	// then.
	covered bool
	// changed is the compare of the pinned commit with the new one; nil
	// until asked.
	changed *changedFiles
}

// changedFiles is what changed since the pinned commit.
type changedFiles struct {
	files []string
	// untrusted says why the list cannot be trusted ("" when it can): the
	// commit then deploys, and its release note says so.
	untrusted string
}

// githubAsk is what decideSource needs from GitHub before it decides sha.
type githubAsk struct {
	sha     string
	changed bool
}

func (a githubAsk) any() bool { return a.changed }

// hasGates says the App sets watch paths or wait for CI.
func hasGates(app *shpyrdv1.App) bool {
	if app.Spec.Source == nil || app.Spec.Source.Git == nil {
		return false
	}
	git := app.Spec.Source.Git
	return len(git.WatchPaths) > 0 || git.WaitForChecks
}

// gateCommit runs steps 4 and 5 on a commit about to deploy. It returns
// what the release note gains, and stop when the commit does not deploy
// now: not deployed (the record says why), or GitHub must be asked first
// (u.ask says what).
func gateCommit(app *shpyrdv1.App, u *sourceUpdate, rec gitauth.SourceRecord, sha string, ev sourceEvent, now time.Time) (string, bool) {
	git := app.Spec.Source.Git
	if !ev.gates.covered {
		return "", false
	}
	note := ""
	if len(git.WatchPaths) > 0 {
		c := ev.gates.changed
		switch {
		case c == nil:
			u.ask = githubAsk{sha: sha, changed: true}
			return "", true
		case c.untrusted != "":
			note = " (watch paths not checked: " + c.untrusted + ")"
		case !gitauth.MatchWatchPaths(git.WatchPaths, c.files):
			notDeploy(u, rec, sha, gitauth.ReasonNoWatchedChanges)
			return "", true
		}
	}
	return note, false
}

// notDeploy records sha as not deployed, for reason; a decision not
// recorded before is audited (git.skip, the reason as its detail).
func notDeploy(u *sourceUpdate, rec gitauth.SourceRecord, sha, reason string) {
	u.record.NotDeployed, u.record.NotDeployedReason = sha, reason
	if rec.NotDeployed != sha || rec.NotDeployedReason != reason {
		u.audit, u.target, u.reason = "git.skip", sha, reason
	}
}

// gather asks GitHub, through the App, what decideSource asked for.
func (r *SourceReconciler) gather(ctx context.Context, app *shpyrdv1.App, ev *sourceEvent, ask githubAsk, now time.Time) {
	ref, ok := r.GitHub.cover(ctx, app)
	if !ok {
		ev.gates.covered = false
		return
	}
	ctx, cancel := context.WithTimeout(ctx, githubGateTimeout)
	defer cancel()
	if ask.changed {
		c := r.changedFiles(ctx, ref, app.PinnedCommit(), ask.sha)
		ev.gates.changed = &c
	}
}

// changedFiles is GitHub's compare of the pinned commit with head: every
// file changed since what runs, so a commit not deployed earlier counts
// once a later one matches. A list that cannot be trusted says why.
func (r *SourceReconciler) changedFiles(ctx context.Context, ref githubRepoRef, base, head string) changedFiles {
	if r.GitHub.backingOff(ref.installation) {
		return changedFiles{untrusted: "GitHub is not answering"}
	}
	cmp, err := ref.client.Compare(ctx, ref.installation, ref.owner, ref.name, base, head)
	switch {
	case err != nil:
		if !github.IsNotFound(err) {
			r.GitHub.remember(ref.installation, err)
		}
		log.FromContext(ctx).Info("could not compare the commits on GitHub; deploying without checking the watch paths", "commit", shortCommit(head), "err", err.Error())
		return changedFiles{untrusted: "GitHub's compare failed"}
	case cmp.Capped:
		return changedFiles{untrusted: "more than 300 files changed"}
	case cmp.Status == "diverged" || cmp.Status == "behind":
		return changedFiles{untrusted: "the branch's history was rewritten"}
	}
	return changedFiles{files: cmp.Files}
}
```

- [ ] **Step 5: The watcher asks, then decides again**

In `internal/controller/source_controller.go`:

`sourceEvent` gains a field:

```go
type sourceEvent struct {
	push   *gitauth.Push
	latest string
	err    error
	// gates are GitHub's answers for watch paths and wait for CI.
	gates sourceGates
}
```

`sourceUpdate` gains, after `audit, actor string`:

```go
	// target and reason are the commit and the words of a git.skip for a
	// commit not deployed.
	target, reason string
	// ask is what decideSource needs from GitHub before it decides.
	ask githubAsk
```

In `Reconcile`, replace

```go
	u := decideSource(app, rec, ev, now)
```

with

```go
	if hasGates(app) && ev.err == nil {
		_, ev.gates.covered = r.GitHub.cover(ctx, app)
	}
	u := decideSource(app, rec, ev, now)
	// Watch paths and wait for CI need GitHub's answers: asked, then the
	// commit is decided again.
	for i := 0; i < 2 && u.ask.any(); i++ {
		r.gather(ctx, app, &ev, u.ask, now)
		u = decideSource(app, rec, ev, now)
	}
```

Replace the whole of `decideSource` with:

```go
// decideSource turns what was learnt into what is written: the record
// (Reconcile writes it when it changed), a pin when the branch moved and
// the App deploys it. The order: the branch, auto-deploy, [skip deploy],
// watch paths, wait for CI, the pin. A person's deploy and the first
// commit of a branch skip watch paths and wait for CI.
func decideSource(app *shpyrdv1.App, rec gitauth.SourceRecord, ev sourceEvent, now time.Time) sourceUpdate {
	u := sourceUpdate{record: rec}
	u.record.Ref = app.GitRevision()
	u.record.CheckedAt = &now
	if ev.err != nil {
		u.record.Error = describeSourceError(ev.err, app)
		u.record.Failures = min(u.record.Failures+1, maxFailures)
		return u
	}
	u.record.Error, u.record.Failures = "", 0
	sha, via, message, author := ev.latest, "poll", "", ""
	// seen is when the commit was seen: the push's own time, else now.
	seen := now
	if p := ev.push; p != nil {
		sha, via, message, author = strings.ToLower(p.SHA), "push", p.Message, p.Author
		if !p.At.IsZero() {
			seen = p.At.UTC()
		}
		u.record.LastWebhookAt = &seen
		u.record.Delivery = p.Delivery
	}
	if via == "push" || sha != rec.Latest {
		u.record.Latest, u.record.LatestAt = sha, &seen
		u.record.Message, u.record.Author, u.record.Via = message, author, via
	}
	u.actor = "git"
	if u.record.Author != "" {
		u.actor = "git:" + u.record.Author
	}
	pin := app.PinnedCommit()
	// A skip of the commit already running is no skip.
	skip := ev.push != nil && (ev.push.Skip || gitauth.SkipsDeploy(message)) && sha != pin
	if skip {
		if rec.Skipped != sha {
			u.audit = "git.skip"
		}
		u.record.Skipped = sha
	}
	suffix := ""
	switch {
	case pin == "":
		// Nothing pinned for this revision yet (a new app, a deploy, a
		// changed branch): its commit, whatever auto-deploy, watch paths
		// and wait for CI say. A person's deploy keeps their note and name.
		if by := app.Annotations[shpyrdv1.AnnotationDeployedBy]; by != "" && by != "git" && !strings.HasPrefix(by, "git:") {
			u.pin, u.record.Via, u.record.Skipped, u.audit = sha, "deploy", "", ""
			u.record.NotDeployed, u.record.NotDeployedReason = "", ""
			return u
		}
	case sha == pin, skip, sha == rec.Skipped, !app.AutoDeploys():
		// Nothing new, a commit that asked not to deploy (now, or when a
		// push said so before this poll found it), or pushes recorded only.
		return u
	case sha == rec.NotDeployed:
		// Decided before: no change in the watch paths, or its CI.
		return u
	default:
		var stop bool
		if suffix, stop = gateCommit(app, &u, rec, sha, ev, now); stop {
			return u
		}
	}
	u.pin, u.record.Skipped, u.audit = sha, "", "git.push"
	u.record.NotDeployed, u.record.NotDeployedReason = "", ""
	u.deployedBy, u.client = u.actor, u.record.Via
	if u.record.Via == "push" {
		u.note = pushNote(sha, u.record.Author, u.record.Message)
	} else {
		u.note = "Commit " + shortCommit(sha) + " (polled)"
	}
	u.note += suffix
	return u
}
```

In `auditUpdate`, the entry's `Target` and `Detail` become:

```go
		Actor: u.actor, Action: u.audit, Target: shortCommit(firstNonEmpty(u.target, u.pin, u.record.Skipped)),
		Detail: firstNonEmpty(u.reason, firstLine(u.record.Message)), Via: "controller", Client: u.record.Via, Realm: "workspace",
```

In `waitingCommit`, a commit decided not to deploy does not wait: its condition gains `|| rec.Latest == rec.NotDeployed`:

```go
	if !app.AutoDeploys() || rec.Ref != app.GitRevision() || rec.Failures != 0 || rec.Latest == "" || pin == "" ||
		rec.Latest == pin || rec.Latest == rec.Skipped || rec.Latest == rec.NotDeployed {
		return ""
	}
```

- [ ] **Step 6: Run the tests**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp go vet ./internal/controller/ && GOTMPDIR=/home/nkr/.cache/gotmp go test ./internal/controller/`
Expected: PASS, PR 2's and PR 3's watcher tests included.

- [ ] **Step 7: Commit**

```bash
git add internal/controller/github.go internal/controller/source_gates.go internal/controller/source_gates_test.go internal/controller/source_controller.go
git commit -m "feat(controller): watch paths: a commit that changes nothing in them is recorded, not deployed (RFC-0054)

Co-Authored-By: <your model name> <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 6: The watcher's wait for CI, and the webhook's nudge

**Files:**
- Modify: `internal/controller/source_gates.go` (`sourceGates`, `githubAsk`, `gateCommit`, `gather`; new `heldSince`, `ciVerdict`)
- Modify: `internal/controller/source_controller.go` (`SourceReconciler`, `sourceEvent`, `Reconcile`, `decideSource`, `forget`; new `dropAnnotation`, `noteCI`, `ciDue`, `requeue`)
- Modify: `pkg/api/github_webhook.go` (the event switch; new `githubCIEvent`, `githubWorkspaceApps`; `githubPush` uses the latter)
- Test: `internal/controller/source_gates_test.go` (append), `pkg/api/github_webhook_test.go` (append)

**Interfaces:**
- Consumes: `github.Verdict`, `github.Unanswered`, `github.CI*` (Task 3); `github.Client.CheckRuns`, `CommitStatuses`, `App()` (Task 4); `gitauth.CINudge`, `ReadCINudge`, `SourceRecord.WaitsOnCI`, the reasons (Task 1); Task 5's gates; PR 3's `deliverApp`, `connectAcme`, `onGitHub`, `appNamed`, `githubServer`, `hookCommit`.
- Produces:
  - `sourceGates.ci *github.CIVerdict`; `githubAsk.ci bool`, `githubAsk.since time.Time`; `sourceEvent.recheck bool`
  - `const heldRecheck = 30 * time.Second`
  - `func heldSince(rec gitauth.SourceRecord, sha string, ev sourceEvent, next gitauth.SourceRecord, now time.Time) time.Time`
  - `func (r *SourceReconciler) dropAnnotation(ctx, app *shpyrdv1.App, key, raw string)`
  - `func (s *Server) githubCIEvent(c *gin.Context, event string, body []byte, delivery string)`, `func (s *Server) githubWorkspaceApps(ctx context.Context, workspace string) ([]shpyrdv1.App, error)`
  - Release notes "… (CI passed)" and "… (no CI)"; reasons "CI failed: <check>", "CI did not finish within an hour"

- [ ] **Step 1: Write the failing controller tests**

Append to `internal/controller/source_gates_test.go` (add `"k8s.io/apimachinery/pkg/types"`, `"sigs.k8s.io/controller-runtime/pkg/client"` and `"encoding/json"` to its imports):

```go
// nudgeCI leaves on app what the webhook leaves when sha's checks change.
func nudgeCI(t *testing.T, c client.Client, app *shpyrdv1.App, sha string, when time.Time) {
	t.Helper()
	raw, _ := json.Marshal(gitauth.CINudge{SHA: sha, Delivery: "d-ci", At: when})
	patch, _ := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]string{shpyrdv1.AnnotationGitCI: string(raw)}}})
	if err := c.Patch(context.Background(), app, client.RawPatch(types.MergePatchType, patch)); err != nil {
		t.Fatal(err)
	}
}

func ciRun(name, status, conclusion string) githubtest.CheckRun {
	return githubtest.CheckRun{Name: name, Status: status, Conclusion: conclusion, AppID: 99}
}

func waitingShop() *shpyrdv1.App {
	return pushed(shopWith(shpyrdv1.GitSource{WaitForChecks: true}), otherCommit, testCommit, "fix login", sourceNow)
}

func TestAHeldCommitDeploysOnceItsCIPasses(t *testing.T) {
	app := waitingShop()
	r, fake := gatedWatcher(t, &fakeLsRemote{}, app)
	// shpyrd's own queued check is on the commit too: it is left out.
	fake.SetCheckRuns(otherCommit, ciRun("test", "in_progress", ""), githubtest.CheckRun{Name: "shpyrd / shop", Status: "queued", AppID: githubtest.AppID})
	res, got := reconcileSource(t, r, app)
	rec := sourceRecord(got)
	if got.PinnedCommit() != testCommit || rec.Held != otherCommit || rec.HeldSince == nil || !rec.HeldSince.Equal(sourceNow) {
		t.Fatalf("pin %s, record %+v", got.PinnedCommit(), rec)
	}
	if res.RequeueAfter <= 0 || res.RequeueAfter > heldRecheck {
		t.Errorf("a held commit is read again within 30 s: %v", res.RequeueAfter)
	}
	// Too soon: GitHub is not asked again.
	reads := calledGitHub(fake, "GET /repos/acme/shop/commits/")
	advance(fake, 5*time.Second)
	if _, again := reconcileSource(t, r, got); again.PinnedCommit() != testCommit || calledGitHub(fake, "GET /repos/acme/shop/commits/") != reads {
		t.Errorf("read again after 5 s: %v", fake.Calls())
	}
	fake.SetCheckRuns(otherCommit, ciRun("test", "completed", "success"), githubtest.CheckRun{Name: "shpyrd / shop", Status: "queued", AppID: githubtest.AppID})
	advance(fake, 25*time.Second)
	_, got = reconcileSource(t, r, got)
	rec = sourceRecord(got)
	if got.PinnedCommit() != otherCommit || rec.Held != "" || got.Annotations[shpyrdv1.AnnotationReleaseNote] != "Push 89abcde by alice: fix login (CI passed)" {
		t.Fatalf("pin %s, note %q, record %+v", got.PinnedCommit(), got.Annotations[shpyrdv1.AnnotationReleaseNote], rec)
	}
	if rec.CheckedAt == nil || !rec.CheckedAt.Equal(sourceNow) {
		t.Errorf("reading the checks is no check of the branch: %v", rec.CheckedAt)
	}
}

func TestACommitCIFailedDeploysWhenARerunPasses(t *testing.T) {
	app := waitingShop()
	r, fake := gatedWatcher(t, &fakeLsRemote{}, app)
	heard := heardAudits(t)
	fake.SetCheckRuns(otherCommit, ciRun("lint", "completed", "success"), ciRun("test", "completed", "failure"))
	_, got := reconcileSource(t, r, app)
	rec := sourceRecord(got)
	if got.PinnedCommit() != testCommit || rec.Held != "" || rec.NotDeployed != otherCommit || rec.NotDeployedReason != "CI failed: test" {
		t.Fatalf("pin %s, record %+v", got.PinnedCommit(), rec)
	}
	if len(*heard) != 1 || (*heard)[0].Action != "git.skip" || (*heard)[0].Detail != "CI failed: test" {
		t.Errorf("audited %+v", *heard)
	}
	// Without a change of its checks, the failed commit is not read again.
	reads := calledGitHub(fake, "GET /repos/acme/shop/commits/")
	advance(fake, time.Minute)
	if _, got = reconcileSource(t, r, got); calledGitHub(fake, "GET /repos/acme/shop/commits/") != reads {
		t.Errorf("read again without a nudge: %v", fake.Calls())
	}
	// Re-run on GitHub: green; the webhook nudges.
	fake.SetCheckRuns(otherCommit, ciRun("lint", "completed", "success"), ciRun("test", "completed", "success"))
	nudgeCI(t, r.Client, got, otherCommit, fake.Time())
	_, got = reconcileSource(t, r, got)
	if got.PinnedCommit() != otherCommit || got.Annotations[shpyrdv1.AnnotationReleaseNote] != "Push 89abcde by alice: fix login (CI passed)" {
		t.Fatalf("pin %s, note %q, record %+v", got.PinnedCommit(), got.Annotations[shpyrdv1.AnnotationReleaseNote], sourceRecord(got))
	}
	if _, ok := got.Annotations[shpyrdv1.AnnotationGitCI]; ok {
		t.Error("the nudge is dropped once read")
	}
}

func TestAHeldCommitIsGivenUpAfterAnHour(t *testing.T) {
	app := waitingShop()
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": otherCommit}}
	r, fake := gatedWatcher(t, ls, app)
	heard := heardAudits(t)
	fake.SetCheckRuns(otherCommit, ciRun("test", "in_progress", ""))
	_, got := reconcileSource(t, r, app)
	advance(fake, 59*time.Minute)
	if _, got = reconcileSource(t, r, got); sourceRecord(got).Held != otherCommit {
		t.Fatalf("59 minutes: %+v", sourceRecord(got))
	}
	advance(fake, time.Minute)
	_, got = reconcileSource(t, r, got)
	rec := sourceRecord(got)
	if got.PinnedCommit() != testCommit || rec.Held != "" || rec.NotDeployed != otherCommit || rec.NotDeployedReason != gitauth.ReasonCITimeout {
		t.Fatalf("pin %s, record %+v", got.PinnedCommit(), rec)
	}
	if n := len(*heard); n != 1 || (*heard)[0].Detail != gitauth.ReasonCITimeout {
		t.Errorf("audited %+v", *heard)
	}
}

func TestANewerPushReplacesAHeldCommitAtOnce(t *testing.T) {
	app := waitingShop()
	r, fake := gatedWatcher(t, &fakeLsRemote{}, app)
	fake.SetCheckRuns(otherCommit, ciRun("test", "in_progress", ""))
	_, got := reconcileSource(t, r, app)
	fake.SetCheckRuns(thirdCommit, ciRun("test", "completed", "success"))
	got = pushed(got, thirdCommit, otherCommit, "fix it again", sourceNow.Add(10*time.Second))
	if err := r.Update(context.Background(), got); err != nil {
		t.Fatal(err)
	}
	advance(fake, 10*time.Second)
	_, got = reconcileSource(t, r, got)
	if rec := sourceRecord(got); got.PinnedCommit() != thirdCommit || rec.Held != "" || rec.Latest != thirdCommit {
		t.Errorf("pin %s, record %+v", got.PinnedCommit(), rec)
	}
}

func TestACommitWithNoCIDeploysAfterAMinute(t *testing.T) {
	app := waitingShop()
	r, fake := gatedWatcher(t, &fakeLsRemote{}, app)
	_, got := reconcileSource(t, r, app)
	if sourceRecord(got).Held != otherCommit {
		t.Fatalf("held first: %+v", sourceRecord(got))
	}
	advance(fake, 30*time.Second)
	if _, got = reconcileSource(t, r, got); got.PinnedCommit() != testCommit {
		t.Fatal("30 s: CI may still report")
	}
	advance(fake, 30*time.Second)
	_, got = reconcileSource(t, r, got)
	if got.PinnedCommit() != otherCommit || got.Annotations[shpyrdv1.AnnotationReleaseNote] != "Push 89abcde by alice: fix login (no CI)" {
		t.Errorf("pin %s, note %q", got.PinnedCommit(), got.Annotations[shpyrdv1.AnnotationReleaseNote])
	}
}

func TestAHeldCommitStaysHeldWhileGitHubIsDown(t *testing.T) {
	app := waitingShop()
	r, fake := gatedWatcher(t, &fakeLsRemote{}, app)
	fake.SetCheckRuns(otherCommit, ciRun("test", "in_progress", ""))
	_, got := reconcileSource(t, r, app)
	fake.SetDown(true)
	for range 4 {
		advance(fake, 30*time.Second)
		_, got = reconcileSource(t, r, got)
	}
	if rec := sourceRecord(got); got.PinnedCommit() != testCommit || rec.Held != otherCommit || rec.NotDeployed != "" {
		t.Fatalf("two minutes of GitHub down is not 'no CI': pin %s, record %+v", got.PinnedCommit(), rec)
	}
	fake.SetDown(false)
	fake.SetCheckRuns(otherCommit, ciRun("test", "completed", "success"))
	advance(fake, 2*time.Minute)
	if _, got = reconcileSource(t, r, got); got.PinnedCommit() != otherCommit {
		t.Errorf("green once GitHub answers: pin %s, record %+v", got.PinnedCommit(), sourceRecord(got))
	}
}

func TestWaitForCIComesAfterTheWatchPaths(t *testing.T) {
	app := pushed(shopWith(shpyrdv1.GitSource{WatchPaths: []string{"services/api/**"}, WaitForChecks: true}), otherCommit, testCommit, "docs", sourceNow)
	r, fake := gatedWatcher(t, &fakeLsRemote{}, app)
	fake.SetCompare("acme/shop", testCommit, otherCommit, githubtest.Compare{Status: "ahead", Files: []githubtest.File{{Name: "README.md"}}})
	_, got := reconcileSource(t, r, app)
	if rec := sourceRecord(got); rec.NotDeployedReason != gitauth.ReasonNoWatchedChanges || calledGitHub(fake, "GET /repos/acme/shop/commits/") != 0 {
		t.Errorf("record %+v, calls %v", rec, fake.Calls())
	}
}
```

- [ ] **Step 2: Write the failing webhook tests**

Append to `pkg/api/github_webhook_test.go` (it imports `gitauth`, `store`, `githubtest`, `shpyrdv1`, `strings`, `fmt`, `net/http` already; add `"encoding/json"` if missing):

```go
func ciEvent(event string, installation int64, fullName, sha, action string) []byte {
	if event == "status" {
		return []byte(fmt.Sprintf(`{"sha":%q,"state":"success","context":"ci/test","installation":{"id":%d},"repository":{"full_name":%q}}`, sha, installation, fullName))
	}
	return []byte(fmt.Sprintf(`{"action":%q,"check_suite":{"head_sha":%q,"status":"completed","conclusion":"success"},"installation":{"id":%d},"repository":{"full_name":%q}}`,
		action, sha, installation, fullName))
}

// recordOn leaves the source watcher's record on app.
func recordOn(app *shpyrdv1.App, rec gitauth.SourceRecord) *shpyrdv1.App {
	raw, _ := json.Marshal(rec)
	if app.Annotations == nil {
		app.Annotations = map[string]string{}
	}
	app.Annotations[shpyrdv1.AnnotationGitSource] = string(raw)
	return app
}

func TestGitHubWebhookNudgesOnlyTheAppsWaitingOnThatCommit(t *testing.T) {
	const older = "0123456789abcdef0123456789abcdef01234567"
	held := gitauth.SourceRecord{Ref: "main", Latest: hookCommit, Held: hookCommit}
	failed := gitauth.SourceRecord{Ref: "main", Latest: hookCommit, NotDeployed: hookCommit, NotDeployedReason: gitauth.ReasonCIFailedPrefix + "test"}
	ignored := gitauth.SourceRecord{Ref: "main", Latest: hookCommit, NotDeployed: hookCommit, NotDeployedReason: gitauth.ReasonNoWatchedChanges}
	another := gitauth.SourceRecord{Ref: "main", Latest: older, Held: older}
	s, cr, _ := githubServer(t,
		recordOn(onGitHub("shop", "https://github.com/acme/shop", "main", ""), held),
		recordOn(onGitHub("shopci", "https://github.com/Acme/Shop", "main", ""), failed),
		recordOn(onGitHub("shopdocs", "https://github.com/acme/shop", "main", ""), ignored),
		recordOn(onGitHub("shopnext", "https://github.com/acme/shop", "main", ""), another),
		recordOn(onGitHub("site", "https://github.com/acme/site", "main", ""), held),
		recordOn(onGitHub("theirs", "https://github.com/acme/shop", "main", "beta"), held),
	)
	connectAcme(t, s, store.DefaultWorkspace)
	rec := deliverApp(t, s, "status", "d-5", ciEvent("status", 42, "Acme/shop", strings.ToUpper(hookCommit), ""), githubtest.WebhookSecret)
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"projects":["shop","shopci"]`) {
		t.Fatalf("status: %d %s", rec.Code, rec.Body.String())
	}
	for _, slug := range []string{"shop", "shopci"} {
		if n := gitauth.ReadCINudge(appNamed(t, cr, slug).Annotations[shpyrdv1.AnnotationGitCI]); n == nil || n.SHA != hookCommit || n.Delivery != "d-5" {
			t.Errorf("%s: %+v", slug, n)
		}
	}
	for _, slug := range []string{"shopdocs", "shopnext", "site", "theirs"} {
		if v, ok := appNamed(t, cr, slug).Annotations[shpyrdv1.AnnotationGitCI]; ok {
			t.Errorf("%s was nudged: %s", slug, v)
		}
	}
}

func TestGitHubWebhookTakesACheckSuiteOnlyWhenItCompletes(t *testing.T) {
	s, cr, _ := githubServer(t, recordOn(onGitHub("shop", "https://github.com/acme/shop", "main", ""), gitauth.SourceRecord{Ref: "main", Latest: hookCommit, Held: hookCommit}))
	connectAcme(t, s, store.DefaultWorkspace)
	if rec := deliverApp(t, s, "check_suite", "d-6", ciEvent("check_suite", 42, "Acme/shop", hookCommit, "requested"), githubtest.WebhookSecret); rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), "ignored") {
		t.Errorf("requested: %d %s", rec.Code, rec.Body.String())
	}
	if _, ok := appNamed(t, cr, "shop").Annotations[shpyrdv1.AnnotationGitCI]; ok {
		t.Fatal("a suite just requested nudges nothing")
	}
	if rec := deliverApp(t, s, "check_suite", "d-7", ciEvent("check_suite", 42, "Acme/shop", hookCommit, "completed"), githubtest.WebhookSecret); rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"projects":["shop"]`) {
		t.Errorf("completed: %d %s", rec.Code, rec.Body.String())
	}
}

func TestGitHubWebhookRefusesACIEventWithABadSignature(t *testing.T) {
	s, cr, _ := githubServer(t, recordOn(onGitHub("shop", "https://github.com/acme/shop", "main", ""), gitauth.SourceRecord{Ref: "main", Latest: hookCommit, Held: hookCommit}))
	connectAcme(t, s, store.DefaultWorkspace)
	if rec := deliverApp(t, s, "status", "d-8", ciEvent("status", 42, "Acme/shop", hookCommit, ""), "not-the-secret"); rec.Code != http.StatusUnauthorized {
		t.Errorf("bad signature: %d", rec.Code)
	}
	if _, ok := appNamed(t, cr, "shop").Annotations[shpyrdv1.AnnotationGitCI]; ok {
		t.Error("nudged on a forged delivery")
	}
}

func TestGitHubWebhookIgnoresTheCIOfASuspendedOrUnknownInstallation(t *testing.T) {
	s, _, _ := githubServer(t, recordOn(onGitHub("shop", "https://github.com/acme/shop", "main", ""), gitauth.SourceRecord{Ref: "main", Latest: hookCommit, Held: hookCommit}))
	if rec := deliverApp(t, s, "status", "d-9", ciEvent("status", 42, "Acme/shop", hookCommit, ""), githubtest.WebhookSecret); rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), "no workspace") {
		t.Errorf("unknown: %d %s", rec.Code, rec.Body.String())
	}
	connectAcme(t, s, store.DefaultWorkspace)
	if err := s.store.SetGitHubInstallationState(context.Background(), 42, true, false); err != nil {
		t.Fatal(err)
	}
	if rec := deliverApp(t, s, "status", "d-10", ciEvent("status", 42, "Acme/shop", hookCommit, ""), githubtest.WebhookSecret); rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), "suspended") {
		t.Errorf("suspended: %d %s", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 3: Run them to see them fail**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp go test ./internal/controller/ -run 'Held|CIFailed|NoCI|WaitForCI' ; GOTMPDIR=/home/nkr/.cache/gotmp go test ./pkg/api/ -run 'Nudges|CheckSuite|CIEvent|CIOfA'`
Expected: FAIL: `undefined: heldRecheck`; the webhook answers `{"ignored":"status"}`.

- [ ] **Step 4: Wait for CI in the gates**

In `internal/controller/source_gates.go`:

`sourceGates` gains, after `changed`:

```go
	// ci is the verdict on the commit's other checks; nil until asked.
	ci *github.CIVerdict
```

`githubAsk` and `any` become:

```go
// githubAsk is what decideSource needs from GitHub before it decides sha:
// the files changed, or the CI verdict counted from since.
type githubAsk struct {
	sha     string
	changed bool
	ci      bool
	since   time.Time
}

func (a githubAsk) any() bool { return a.changed || a.ci }
```

After `githubGateTimeout`:

```go
// heldRecheck is how often a held commit's checks are read when no
// webhook says they changed: the net under missed deliveries, two reads.
const heldRecheck = 30 * time.Second
```

`gateCommit` becomes:

```go
func gateCommit(app *shpyrdv1.App, u *sourceUpdate, rec gitauth.SourceRecord, sha string, ev sourceEvent, now time.Time) (string, bool) {
	git := app.Spec.Source.Git
	if !ev.gates.covered {
		return "", false
	}
	note := ""
	// A commit held, or CI failed and read again, passed its watch paths.
	if len(git.WatchPaths) > 0 && rec.Held != sha && !ev.recheck {
		c := ev.gates.changed
		switch {
		case c == nil:
			u.ask = githubAsk{sha: sha, changed: true}
			return "", true
		case c.untrusted != "":
			note = " (watch paths not checked: " + c.untrusted + ")"
		case !gitauth.MatchWatchPaths(git.WatchPaths, c.files):
			notDeploy(u, rec, sha, gitauth.ReasonNoWatchedChanges)
			return "", true
		}
	}
	if !git.WaitForChecks {
		return note, false
	}
	since := heldSince(rec, sha, ev, u.record, now)
	v := ev.gates.ci
	if v == nil {
		u.ask = githubAsk{sha: sha, ci: true, since: since}
		return "", true
	}
	switch v.State {
	case github.CIPassed:
		return note + " (CI passed)", false
	case github.CINone:
		return note + " (no CI)", false
	case github.CIFailed:
		notDeploy(u, rec, sha, gitauth.ReasonCIFailedPrefix+v.Failing)
		return "", true
	case github.CITimedOut:
		notDeploy(u, rec, sha, gitauth.ReasonCITimeout)
		return "", true
	}
	// Pending: held, nothing pinned; a newer commit replaces it at once.
	u.record.Held, u.record.HeldSince = sha, &since
	if u.record.NotDeployed == sha {
		u.record.NotDeployed, u.record.NotDeployedReason = "", ""
	}
	return "", true
}

// heldSince is when a commit's wait for CI started: the hold already
// running; the nudge for a re-run of a commit CI failed; else when the
// commit was first seen, so a push's own time counts.
func heldSince(rec gitauth.SourceRecord, sha string, ev sourceEvent, next gitauth.SourceRecord, now time.Time) time.Time {
	switch {
	case rec.Held == sha && rec.HeldSince != nil:
		return *rec.HeldSince
	case ev.recheck:
		return now
	case next.Latest == sha && next.LatestAt != nil:
		return *next.LatestAt
	}
	return now
}
```

`gather` gains, after the `ask.changed` block:

```go
	if ask.ci {
		v := r.ciVerdict(ctx, ref, ask.sha, ask.since, now)
		ev.gates.ci = &v
		r.noteCI(app.UID, now)
	}
```

and after `changedFiles`:

```go
// ciVerdict reads the commit's check runs and statuses. GitHub not
// answering keeps the commit held, and the hour still counts: an empty
// answer is never taken for "no CI".
func (r *SourceReconciler) ciVerdict(ctx context.Context, ref githubRepoRef, sha string, since, now time.Time) github.CIVerdict {
	if r.GitHub.backingOff(ref.installation) {
		return github.Unanswered(since, now)
	}
	runs, err := ref.client.CheckRuns(ctx, ref.installation, ref.owner, ref.name, sha)
	if err == nil {
		var statuses []github.CommitStatus
		if statuses, err = ref.client.CommitStatuses(ctx, ref.installation, ref.owner, ref.name, sha); err == nil {
			return github.Verdict(runs, statuses, ref.client.App().ID, since, now)
		}
	}
	r.GitHub.remember(ref.installation, err)
	log.FromContext(ctx).Info("could not read the commit's checks on GitHub; it stays held", "commit", shortCommit(sha), "err", err.Error())
	return github.Unanswered(since, now)
}
```

- [ ] **Step 5: The watcher reads held commits again**

In `internal/controller/source_controller.go`:

`SourceReconciler` gains, after `checked map[types.UID]sourceCheck`:

```go
	// ciChecked is when each App's held commit's checks were last read.
	ciChecked map[types.UID]time.Time
```

`sourceEvent` gains:

```go
	// recheck: only a held or CI-failed commit's checks are read again,
	// not the branch; its watch paths passed already.
	recheck bool
```

In `Reconcile`, after `rawPush := app.Annotations[shpyrdv1.AnnotationGitPush]` and the lines that read the push:

```go
	rawCI := app.Annotations[shpyrdv1.AnnotationGitCI]
	nudge := gitauth.ReadCINudge(rawCI)
```

In the `switch` that sets `ev`, add two cases after the case that calls `r.poll` and before `default:`:

```go
	case rec.Held != "" && rec.Held == rec.Latest && ((nudge != nil && nudge.SHA == rec.Held) || r.ciDue(app.UID, now)):
		// A held commit: its checks are read again.
		ev.latest, ev.recheck = rec.Latest, true
	case nudge != nil && nudge.SHA == rec.Latest && rec.WaitsOnCI(nudge.SHA):
		// A commit CI failed: its checks changed (a re-run), read again.
		ev.latest, ev.recheck = rec.Latest, true
```

In the `default:` branch, each of its two `return ctrl.Result{RequeueAfter: r.untilDue(last, now)}, nil` becomes

```go
			r.dropAnnotation(ctx, app, shpyrdv1.AnnotationGitCI, rawCI)
			return ctrl.Result{RequeueAfter: r.requeue(app.UID, last, now)}, nil
```

(the first one inside the `waitingCommit` block, keeping its indentation; the second at the end of `default:`).

The end of `Reconcile`, from `r.remember(app.UID, rev, now, consumed, u.pin)` to the final `return`, becomes:

```go
	if !ev.recheck {
		r.remember(app.UID, rev, now, consumed, u.pin)
	} else if u.pin != "" {
		r.notePinned(app.UID, rev, u.pin, last.CheckedAt)
	}
	r.dropPush(ctx, app, rawPush)
	r.dropAnnotation(ctx, app, shpyrdv1.AnnotationGitCI, rawCI)
	r.auditUpdate(ctx, app, u)
	return ctrl.Result{RequeueAfter: r.requeue(app.UID, u.record, now)}, nil
```

In `decideSource`, replace

```go
	u.record.Ref = app.GitRevision()
	u.record.CheckedAt = &now
```

with

```go
	u.record.Ref = app.GitRevision()
	if !ev.recheck {
		// Reading a held commit's checks is no check of the branch.
		u.record.CheckedAt = &now
	}
```

replace

```go
	u.record.Error, u.record.Failures = "", 0
```

with

```go
	u.record.Error, u.record.Failures = "", 0
	// A hold lasts while the gate holds the commit again.
	u.record.Held, u.record.HeldSince = "", nil
```

and the case `case sha == rec.NotDeployed:` becomes:

```go
	case sha == rec.NotDeployed && !ev.recheck:
		// Decided before: no change in the watch paths, or its CI. Only a
		// change of its checks reopens it.
```

`waitingCommit`'s condition gains `|| rec.Latest == rec.Held` after `rec.Latest == rec.NotDeployed`.

`forget` becomes:

```go
func (r *SourceReconciler) forget(uid types.UID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.checked, uid)
	delete(r.ciChecked, uid)
}
```

and, after `dropPush`:

```go
// dropAnnotation removes an annotation this reconcile read, unless it
// changed meanwhile: the newer one is read next.
func (r *SourceReconciler) dropAnnotation(ctx context.Context, app *shpyrdv1.App, key, raw string) {
	if raw == "" {
		return
	}
	if _, err := removeAnnotationIfUnchanged(ctx, r.Client, app, key, raw); err != nil {
		logRemoveAnnotation(ctx, err, "kept a newer one for the next pass", "an annotation", "annotation", key)
	}
}

// noteCI keeps when the App's held commit's checks were read.
func (r *SourceReconciler) noteCI(uid types.UID, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ciChecked == nil {
		r.ciChecked = map[types.UID]time.Time{}
	}
	r.ciChecked[uid] = now
}

// ciDue says a held commit's checks are to be read again: never read by
// this watcher, or 90 % of heldRecheck ago (requeues are spread).
func (r *SourceReconciler) ciDue(uid types.UID, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	last, ok := r.ciChecked[uid]
	return !ok || now.Sub(last) >= heldRecheck*9/10
}

// requeue is the wait until the next pass: the next check of the branch,
// or sooner, the next read of a held commit's checks.
func (r *SourceReconciler) requeue(uid types.UID, rec gitauth.SourceRecord, now time.Time) time.Duration {
	d := r.untilDue(rec, now)
	if rec.Held == "" || rec.Held != rec.Latest {
		return d
	}
	r.mu.Lock()
	last, ok := r.ciChecked[uid]
	r.mu.Unlock()
	wait := heldRecheck
	if ok {
		wait = max(heldRecheck-now.Sub(last), time.Second)
	}
	return min(d, wait)
}
```

- [ ] **Step 6: The webhook nudges**

In `pkg/api/github_webhook.go`, the event switch's `default` and the comment above it become:

```go
	case "check_suite", "status":
		s.githubCIEvent(c, event, body, c.GetHeader("X-GitHub-Delivery"))
	default:
		c.JSON(http.StatusAccepted, gin.H{"ignored": event})
```

In `githubPush`, replace the block from `// The installation's workspace's Apps;` to the end of the `s.apps.List` error check with:

```go
	list, err := s.githubWorkspaceApps(ctx, inst.Workspace)
	if err != nil {
		s.githubPushUnavailable(c, "could not list the apps", err)
		return
	}
```

and its loop `for i := range list.Items { app := &list.Items[i]` becomes `for i := range list { app := &list[i]`.

Append:

```go
// githubWorkspaceApps are the Apps of an installation's workspace; the
// default workspace's include the Apps from before workspaces, which carry
// no label. Callers check workspaceOf again.
func (s *Server) githubWorkspaceApps(ctx context.Context, workspace string) ([]shpyrdv1.App, error) {
	var list shpyrdv1.AppList
	var opts []client.ListOption
	if workspace != store.DefaultWorkspace {
		opts = append(opts, client.MatchingLabels{shpyrdv1.LabelWorkspace: workspace})
	}
	if err := s.apps.List(ctx, &list, opts...); err != nil {
		return nil, err
	}
	return list.Items, nil
}

// githubCIEvent is a commit's checks or statuses changing (a check suite
// completed, a status set): every App of the installation's workspace on
// the repository that waits on that commit's CI is nudged, through the
// git-ci annotation, to read it again.
func (s *Server) githubCIEvent(c *gin.Context, event string, body []byte, delivery string) {
	ctx := c.Request.Context()
	var ev struct {
		Action     string `json:"action"`
		SHA        string `json:"sha"`
		CheckSuite struct {
			HeadSHA string `json:"head_sha"`
		} `json:"check_suite"`
		Installation struct {
			ID int64 `json:"id"`
		} `json:"installation"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(body, &ev); err != nil || ev.Installation.ID == 0 || ev.Repository.FullName == "" {
		c.JSON(http.StatusAccepted, gin.H{"ignored": "the event names no installation or no repository"})
		return
	}
	if event == "check_suite" && ev.Action != "completed" {
		c.JSON(http.StatusAccepted, gin.H{"ignored": "check_suite " + ev.Action})
		return
	}
	sha := strings.ToLower(firstNonEmpty(ev.SHA, ev.CheckSuite.HeadSHA))
	if sha == "" {
		c.JSON(http.StatusAccepted, gin.H{"ignored": "the event names no commit"})
		return
	}
	inst, err := s.store.GitHubInstallation(ctx, ev.Installation.ID)
	if errors.Is(err, store.ErrNotFound) {
		c.JSON(http.StatusAccepted, gin.H{"ignored": "no workspace has connected this installation"})
		return
	}
	if err != nil {
		s.githubPushUnavailable(c, "could not look up the installation", err)
		return
	}
	if inst.SuspendedAt != nil {
		c.JSON(http.StatusAccepted, gin.H{"ignored": "the installation is suspended"})
		return
	}
	list, err := s.githubWorkspaceApps(ctx, inst.Workspace)
	if err != nil {
		s.githubPushUnavailable(c, "could not list the apps", err)
		return
	}
	raw, _ := json.Marshal(gitauth.CINudge{SHA: sha, Delivery: delivery, At: time.Now().UTC()})
	patch, _ := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]string{shpyrdv1.AnnotationGitCI: string(raw)}}})
	nudged := []string{}
	for i := range list {
		app := &list[i]
		if workspaceOf(app) != inst.Workspace || !app.DeletionTimestamp.IsZero() || !namesGitHubRepo(app, ev.Repository.FullName) || !gitauth.Watched(app) {
			continue
		}
		rec := gitauth.ReadSourceRecord(app.Annotations[shpyrdv1.AnnotationGitSource])
		if rec.Ref != app.GitRevision() || !rec.WaitsOnCI(sha) {
			continue
		}
		if err := s.apps.Patch(ctx, app.DeepCopy(), client.RawPatch(types.MergePatchType, patch)); err != nil {
			s.githubPushUnavailable(c, "could not nudge "+project.SlugOf(app), err)
			return
		}
		nudged = append(nudged, project.SlugOf(app))
	}
	sort.Strings(nudged)
	c.JSON(http.StatusAccepted, gin.H{"nudged": sha, "projects": nudged})
}
```

Add `"context"` to the file's imports.

- [ ] **Step 7: Run the tests**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp go vet ./internal/controller/ ./pkg/api/ && GOTMPDIR=/home/nkr/.cache/gotmp go test ./internal/controller/ ./pkg/api/`
Expected: PASS, PR 3's webhook tests included.

- [ ] **Step 8: Commit**

```bash
git add internal/controller/source_gates.go internal/controller/source_gates_test.go internal/controller/source_controller.go pkg/api/github_webhook.go pkg/api/github_webhook_test.go
git commit -m "feat(controller,api): wait for CI: held until the commit's other checks pass; check_suite and status nudge (RFC-0054)

Co-Authored-By: <your model name> <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 7: The reporter's check runs

**Files:**
- Create: `internal/controller/github_report.go`
- Modify: `pkg/server/server.go` (after the source watcher's `SetupWithManager`, ~585)
- Test: `internal/controller/github_report_test.go`

**Interfaces:**
- Consumes: `GitHubApp.cover`, `githubRepoRef` (Task 5); `github.Client.CreateCheckRun`, `UpdateCheckRun`, `CheckRunWrite`, `IsNotFound` (Task 4); `shpyrdv1.AnnotationGitHubReport`, the record's `Held`/`NotDeployed` (Task 1); `Config.issuer` (`desired.go`), `buildsFromGitHub` (`app_controller.go`), `watchesGit`, `pushNote`, `shortCommit`, `project.SlugOf`.
- Produces:
  - `type GitHubReporter struct { client.Client; GitHub *GitHubApp; Config Config; Now func() time.Time }`, `func (*GitHubReporter) SetupWithManager(ctrl.Manager) error`, `func (*GitHubReporter) Reconcile(ctx, ctrl.Request) (ctrl.Result, error)`
  - `type githubReport struct { Commit string; CheckRun int64; Check string; Release int; Deployment int64; DeploymentState string }` (json `commit`, `checkRun`, `check`, `release`, `deployment`, `deploymentState`), `func readGitHubReport(annotation string) githubReport`
  - `type checkWant struct { commit, status, conclusion, label, title, summary string }`, `func (checkWant) key() string` ("in_progress/deploying"), `func checkDone(key string) bool`, `func wantCheck(app *shpyrdv1.App) (checkWant, bool)`
  - `func releaseIs(rel *shpyrdv1.Release, sha string) bool`, `func configRelease(app *shpyrdv1.App) bool`
  - `func (r *GitHubReporter) report(ctx, app, ref githubRepoRef, old githubReport) (githubReport, error)`: Task 8 adds the Deployment to it
  - `func (r *GitHubReporter) projectPage(app *shpyrdv1.App) string`
  - `const reportBackoff = 30 * time.Second`, `reportMaxBackoff = 10 * time.Minute`, `reportLogEvery = time.Minute`

- [ ] **Step 1: Write the failing tests**

`internal/controller/github_report_test.go`:

```go
package controller

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
	"github.com/shpyrd-io/shpyrd/pkg/github"
	"github.com/shpyrd-io/shpyrd/pkg/github/githubtest"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// githubReporter is the reporter on a platform with the fake's App, its
// installation 42 on Acme connected, on the fake's clock.
func githubReporter(t *testing.T, objs ...client.Object) (*GitHubReporter, client.Client, *githubtest.Server, store.Store) {
	t.Helper()
	for _, o := range objs {
		if o.GetUID() == "" {
			o.SetUID(types.UID(o.GetNamespace() + "/" + o.GetName()))
		}
	}
	fake, st := connectedGitHub(t)
	_, c := newTestReconciler(t, append([]client.Object{fake.Secret("shpyrd-system")}, objs...)...)
	cfg := Config{Domain: "example.test", DashboardURL: "https://acme.shpyrd.test"}.Defaults()
	r := &GitHubReporter{Client: c, GitHub: &GitHubApp{Source: fake.Source(c, "shpyrd-system"), Installations: st}, Config: cfg, Now: fake.Time}
	return r, c, fake, st
}

func reconcileReport(t *testing.T, r *GitHubReporter, app *shpyrdv1.App) (ctrl.Result, *shpyrdv1.App) {
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

// shopAt is the shop app on github.com/acme/shop, main pinned at sha, as
// a push by alice left it.
func shopAt(sha, phase string, rels ...shpyrdv1.Release) *shpyrdv1.App {
	app := pinGit(gitApp("shop", "https://github.com/acme/shop"), sha)
	app.Status.Phase, app.Status.URL, app.Status.Releases = phase, "https://shop.example.test", rels
	return withRecord(app, gitauth.SourceRecord{Ref: "main", Latest: sha, Message: "fix login", Author: "alice", Via: "push"})
}

func release(n int, sha, description string) shpyrdv1.Release {
	return shpyrdv1.Release{Number: n, Image: fmt.Sprintf("10.96.0.50:5000/apps/shop@sha256:%064d", n), Source: short(sha), Description: description, CreatedAt: metav1.NewTime(sourceNow)}
}

// setStatus changes what the App controller would have written.
func setStatus(t *testing.T, c client.Client, app *shpyrdv1.App, change func(*shpyrdv1.AppStatus)) {
	t.Helper()
	got := &shpyrdv1.App{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(app), got); err != nil {
		t.Fatal(err)
	}
	change(&got.Status)
	if err := c.Status().Update(context.Background(), got); err != nil {
		t.Fatal(err)
	}
}

// setAnnotations changes what the source watcher would have written.
func setAnnotations(t *testing.T, c client.Client, app *shpyrdv1.App, change func(*shpyrdv1.App)) {
	t.Helper()
	got := &shpyrdv1.App{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(app), got); err != nil {
		t.Fatal(err)
	}
	change(got)
	if err := c.Update(context.Background(), got); err != nil {
		t.Fatal(err)
	}
}

func reportOf(app *shpyrdv1.App) githubReport {
	return readGitHubReport(app.Annotations[shpyrdv1.AnnotationGitHubReport])
}

func TestACheckRunFollowsTheCommitFromBuildingToRunning(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseBuilding)
	r, c, fake, _ := githubReporter(t, app)
	_, got := reconcileReport(t, r, app)
	runs := fake.CheckRunsOn(testCommit)
	if len(runs) != 1 || runs[0].Name != "shpyrd / shop" || runs[0].Status != "in_progress" || runs[0].Title != "Building" ||
		runs[0].DetailsURL != "https://acme.shpyrd.test/projects/shop" || runs[0].ExternalID != string(app.UID) ||
		!strings.Contains(runs[0].Summary, "Push 0123456 by alice: fix login") {
		t.Fatalf("%+v", runs)
	}
	if rep := reportOf(got); rep.Commit != testCommit || rep.CheckRun != runs[0].ID || rep.Check != "in_progress/building" {
		t.Errorf("annotation %+v", rep)
	}
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) {
		st.Phase, st.Releases = shpyrdv1.PhaseDeploying, []shpyrdv1.Release{release(1, testCommit, "Push 0123456 by alice: fix login")}
	})
	reconcileReport(t, r, app)
	if runs = fake.CheckRunsOn(testCommit); len(runs) != 1 || runs[0].Title != "Deploying" || runs[0].Status != "in_progress" {
		t.Fatalf("deploying: %+v", runs)
	}
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) { st.Phase = shpyrdv1.PhaseRunning })
	_, got = reconcileReport(t, r, app)
	if runs = fake.CheckRunsOn(testCommit); len(runs) != 1 || runs[0].Status != "completed" || runs[0].Conclusion != "success" || runs[0].Title != "Running at https://shop.example.test" {
		t.Fatalf("running: %+v", runs)
	}
	if rep := reportOf(got); rep.Check != "completed/success" {
		t.Errorf("annotation %+v", rep)
	}
}

func TestOneCheckRunPerCommitAcrossARestart(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseRunning, release(1, testCommit, "Push 0123456 by alice: fix login"))
	r, _, fake, _ := githubReporter(t, app)
	reconcileReport(t, r, app)
	reconcileReport(t, r, app)
	writes := len(fake.Writes())
	// A new process: nothing in memory, the annotation says what was sent.
	again := &GitHubReporter{Client: r.Client, GitHub: &GitHubApp{Source: fake.Source(r.Client, "shpyrd-system"), Installations: r.GitHub.Installations}, Config: r.Config, Now: fake.Time}
	reconcileReport(t, again, app)
	if len(fake.Writes()) != writes || len(fake.CheckRunsOn(testCommit)) != 1 {
		t.Errorf("writes %d then %d, runs %+v", writes, len(fake.Writes()), fake.CheckRunsOn(testCommit))
	}
}

func TestAFailedBuildFailsTheCheckWithTheLastLinesOfItsMessage(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseFailed)
	var lines []string
	for i := 1; i <= 30; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	app.Status.Message = strings.Join(lines, "\n")
	r, _, fake, _ := githubReporter(t, app)
	reconcileReport(t, r, app)
	runs := fake.CheckRunsOn(testCommit)
	if len(runs) != 1 || runs[0].Conclusion != "failure" || runs[0].Title != "Failed" ||
		!strings.Contains(runs[0].Summary, "line 11\n") || !strings.Contains(runs[0].Summary, "line 30") || strings.Contains(runs[0].Summary, "line 10\n") {
		t.Errorf("%+v", runs)
	}
}

func TestHeldNotDeployedAndSkippedCommitsHaveTheirOwnCheck(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseRunning, release(1, testCommit, "Push 0123456 by alice: fix login"))
	r, c, fake, _ := githubReporter(t, app)
	reconcileReport(t, r, app)
	held := gitauth.SourceRecord{Ref: "main", Latest: otherCommit, Message: "fix it", Author: "alice", Via: "push", Held: otherCommit, HeldSince: at(sourceNow)}
	setAnnotations(t, c, app, func(a *shpyrdv1.App) { withRecord(a, held) })
	reconcileReport(t, r, app)
	runs := fake.CheckRunsOn(otherCommit)
	if len(runs) != 1 || runs[0].Status != "queued" || runs[0].Title != "Waiting for CI" {
		t.Fatalf("held: %+v", runs)
	}
	if first := fake.CheckRunsOn(testCommit); len(first) != 1 || first[0].Conclusion != "success" {
		t.Errorf("the running commit's finished check is left as it was: %+v", first)
	}
	failed := held
	failed.Held, failed.HeldSince, failed.NotDeployed, failed.NotDeployedReason = "", nil, otherCommit, "CI failed: test"
	setAnnotations(t, c, app, func(a *shpyrdv1.App) { withRecord(a, failed) })
	reconcileReport(t, r, app)
	if runs = fake.CheckRunsOn(otherCommit); len(runs) != 1 || runs[0].Status != "completed" || runs[0].Conclusion != "neutral" || runs[0].Title != "CI failed: test" {
		t.Fatalf("not deployed: %+v", runs)
	}
	skipped := gitauth.SourceRecord{Ref: "main", Latest: thirdCommit, Message: "[skip deploy] typo", Via: "push", Skipped: thirdCommit}
	setAnnotations(t, c, app, func(a *shpyrdv1.App) { withRecord(a, skipped) })
	reconcileReport(t, r, app)
	if runs = fake.CheckRunsOn(thirdCommit); len(runs) != 1 || runs[0].Conclusion != "neutral" || runs[0].Title != "Skipped: [skip deploy]" {
		t.Fatalf("skipped: %+v", runs)
	}
}

func TestAMovedPinReplacesAnUnfinishedCheck(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseBuilding)
	r, c, fake, _ := githubReporter(t, app)
	reconcileReport(t, r, app)
	setAnnotations(t, c, app, func(a *shpyrdv1.App) {
		pinGit(a, otherCommit)
		withRecord(a, gitauth.SourceRecord{Ref: "main", Latest: otherCommit, Message: "again", Author: "alice", Via: "push"})
	})
	_, got := reconcileReport(t, r, app)
	old := fake.CheckRunsOn(testCommit)
	if len(old) != 1 || old[0].Status != "completed" || old[0].Conclusion != "neutral" || old[0].Title != "Replaced by 89abcde" {
		t.Errorf("the old check: %+v", old)
	}
	if runs := fake.CheckRunsOn(otherCommit); len(runs) != 1 || runs[0].Title != "Building" || reportOf(got).Commit != otherCommit {
		t.Errorf("the new one: %+v, annotation %+v", runs, reportOf(got))
	}
}

func TestReportingWaitsOutAGitHubOutageAndNeverTouchesTheDeploy(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseBuilding)
	r, _, fake, _ := githubReporter(t, app)
	fake.SetDown(true)
	res, got := reconcileReport(t, r, app)
	if res.RequeueAfter != 30*time.Second || reportOf(got) != (githubReport{}) {
		t.Fatalf("first failure: %v, annotation %+v", res.RequeueAfter, reportOf(got))
	}
	if !reflect.DeepEqual(got.Spec, app.Spec) || got.PinnedCommit() != testCommit {
		t.Error("the reporter changed what deploys")
	}
	calls := len(fake.Calls())
	if res, _ = reconcileReport(t, r, app); res.RequeueAfter <= 0 || res.RequeueAfter > 30*time.Second || len(fake.Calls()) != calls {
		t.Errorf("waiting: %v, %d calls", res.RequeueAfter, len(fake.Calls())-calls)
	}
	advance(fake, 30*time.Second)
	if res, _ = reconcileReport(t, r, app); res.RequeueAfter != time.Minute {
		t.Errorf("second failure: %v", res.RequeueAfter)
	}
	fake.SetDown(false)
	advance(fake, time.Minute)
	res, got = reconcileReport(t, r, app)
	if res.RequeueAfter != 0 || len(fake.CheckRunsOn(testCommit)) != 1 || reportOf(got).Check != "in_progress/building" {
		t.Errorf("sent later: %v, %+v", res.RequeueAfter, fake.CheckRunsOn(testCommit))
	}
}

func TestEveryReportWriteUsesTheReportToken(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseDeploying, release(1, testCommit, "Push 0123456 by alice: fix login"))
	r, c, fake, _ := githubReporter(t, app)
	cl, err := r.GitHub.Source.Client(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cl.InstallationToken(context.Background(), 42, "shop", github.ContentsRead); err != nil {
		t.Fatal(err)
	}
	reconcileReport(t, r, app)
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) { st.Phase = shpyrdv1.PhaseRunning })
	reconcileReport(t, r, app)
	writes := fake.Writes()
	if len(writes) == 0 {
		t.Fatal("nothing written")
	}
	for _, w := range writes {
		if fmt.Sprint(w.Permissions) != "map[checks:write deployments:write]" {
			t.Errorf("%s %s with %v", w.Method, w.Path, w.Permissions)
		}
	}
}

func TestADeletedCheckRunStartsANewOne(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseBuilding)
	r, c, fake, _ := githubReporter(t, app)
	_, got := reconcileReport(t, r, app)
	fake.DeleteCheckRun(reportOf(got).CheckRun)
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) {
		st.Phase, st.Releases = shpyrdv1.PhaseDeploying, []shpyrdv1.Release{release(1, testCommit, "Push 0123456 by alice: fix login")}
	})
	_, got = reconcileReport(t, r, app)
	runs := fake.CheckRunsOn(testCommit)
	if len(runs) != 1 || runs[0].Title != "Deploying" || reportOf(got).CheckRun != runs[0].ID {
		t.Errorf("%+v, annotation %+v", runs, reportOf(got))
	}
}

func TestNothingIsReportedForASuspendedInstallation(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseBuilding)
	r, _, fake, st := githubReporter(t, app)
	if err := st.SetGitHubInstallationState(context.Background(), 42, true, false); err != nil {
		t.Fatal(err)
	}
	res, got := reconcileReport(t, r, app)
	if res.RequeueAfter != 0 || len(fake.Calls()) != 0 || reportOf(got) != (githubReport{}) {
		t.Errorf("requeue %v, calls %v, annotation %+v", res.RequeueAfter, fake.Calls(), reportOf(got))
	}
}

func TestOnlyAppsFollowingABranchOnGitHubAreReported(t *testing.T) {
	for _, app := range []*shpyrdv1.App{
		pinGit(gitApp("lab", "https://gitlab.example.test/acme/lab"), testCommit),
		func() *shpyrdv1.App { a := gitApp("fixed", "https://github.com/acme/shop"); a.Spec.Source.Git.Revision = testCommit; return a }(),
	} {
		if reportsToGitHub(app) {
			t.Errorf("%s is reported", app.Name)
		}
	}
	if !reportsToGitHub(shopAt(testCommit, shpyrdv1.PhaseRunning)) {
		t.Error("a branch on github.com is reported")
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp go test ./internal/controller/ -run 'CheckRun|ACheck|AFailedBuild|HeldNotDeployed|MovedPin|Outage|ReportWrite|Suspended|OnlyAppsFollowing'`
Expected: FAIL: `undefined: GitHubReporter`.

- [ ] **Step 3: The reporter**

`internal/controller/github_report.go`:

```go
package controller

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
	"github.com/shpyrd-io/shpyrd/pkg/github"
	"github.com/shpyrd-io/shpyrd/pkg/project"
)

// The GitHub reporter (RFC-0054). For an App on a branch of a repository
// the GitHub App covers, it shows on GitHub what shpyrd does with each
// commit: a check run `shpyrd / <project>`, and a Deployment for each
// release that changes the code. It writes nothing to the App's status:
// what it sent is kept in the App's shpyrd.io/github-report annotation,
// and GitHub is written to only when what it would send differs, so a
// restart or a repeated reconcile never posts twice and a lost write is
// sent again. A failed write never touches what deploys.

const (
	// reportBackoff is the wait after a failed write; it doubles up to
	// reportMaxBackoff.
	reportBackoff    = 30 * time.Second
	reportMaxBackoff = 10 * time.Minute
	// reportLogEvery is how often one App's failures are logged.
	reportLogEvery = time.Minute
	// checkSummaryLines and checkSummaryMax cap the App's message in a
	// failed check's summary.
	checkSummaryLines = 20
	checkSummaryMax   = 4000
)

// GitHubReporter writes check runs and Deployments for the git Apps the
// GitHub App covers.
type GitHubReporter struct {
	client.Client
	GitHub *GitHubApp
	// Config gives the workspace dashboard's address, for the check's link.
	Config Config
	// Now is the clock; nil is time.Now.
	Now func() time.Time

	mu      sync.Mutex
	failing map[types.UID]reportFailure
}

// reportFailure is an App whose last writes failed: how many in a row,
// until when it waits, and when it was last logged.
type reportFailure struct {
	count  int
	until  time.Time
	logged time.Time
}

// githubReport is the github-report annotation: the check run sent for a
// commit and its state ("in_progress/deploying"), and the Deployment of a
// release and its state.
type githubReport struct {
	Commit          string `json:"commit,omitempty"`
	CheckRun        int64  `json:"checkRun,omitempty"`
	Check           string `json:"check,omitempty"`
	Release         int    `json:"release,omitempty"`
	Deployment      int64  `json:"deployment,omitempty"`
	DeploymentState string `json:"deploymentState,omitempty"`
}

// readGitHubReport reads the annotation; an unreadable one is empty.
func readGitHubReport(annotation string) githubReport {
	var r githubReport
	if annotation != "" {
		if err := json.Unmarshal([]byte(annotation), &r); err != nil {
			return githubReport{}
		}
	}
	return r
}

// SetupWithManager registers the reporter on the Apps on a GitHub branch.
func (r *GitHubReporter) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("github-report").
		For(&shpyrdv1.App{}, builder.WithPredicates(predicate.NewPredicateFuncs(func(o client.Object) bool {
			app, ok := o.(*shpyrdv1.App)
			return ok && reportsToGitHub(app)
		}))).
		WithOptions(controller.Options{MaxConcurrentReconciles: 2}).
		Complete(r)
}

// reportsToGitHub says the App follows a branch of a github.com
// repository: the reporter's Apps, before the App is asked.
func reportsToGitHub(app *shpyrdv1.App) bool { return buildsFromGitHub(app) && watchesGit(app) }

func (r *GitHubReporter) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	app := &shpyrdv1.App{}
	if err := r.Get(ctx, req.NamespacedName, app); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !app.DeletionTimestamp.IsZero() || !reportsToGitHub(app) {
		r.forget(app.UID)
		return ctrl.Result{}, nil
	}
	if wait := r.waiting(app.UID); wait > 0 {
		return ctrl.Result{RequeueAfter: wait}, nil
	}
	ref, ok := r.GitHub.cover(ctx, app)
	if !ok {
		// Not covered, suspended or removed: nothing is reported, and
		// nothing is queued.
		r.forget(app.UID)
		return ctrl.Result{}, nil
	}
	old := readGitHubReport(app.Annotations[shpyrdv1.AnnotationGitHubReport])
	next, err := r.report(ctx, app, ref, old)
	if next != old {
		// What GitHub accepted is kept, also when a later write failed.
		if perr := r.saveReport(ctx, app, next); perr != nil {
			return ctrl.Result{}, perr
		}
	}
	if err != nil {
		return ctrl.Result{RequeueAfter: r.failed(ctx, app, err)}, nil
	}
	r.forget(app.UID)
	return ctrl.Result{}, nil
}

// report sends what changed since old, and says what GitHub now has.
func (r *GitHubReporter) report(ctx context.Context, app *shpyrdv1.App, ref githubRepoRef, old githubReport) (githubReport, error) {
	next := old
	if err := r.reportCheck(ctx, app, ref, &next); err != nil {
		return next, err
	}
	return next, nil
}

// checkWant is the check run shpyrd shows for a commit.
type checkWant struct {
	commit             string
	status, conclusion string
	label              string
	title, summary     string
}

// key is what the annotation keeps of it: "in_progress/deploying".
func (w checkWant) key() string { return w.status + "/" + w.label }

// checkDone says a kept check is completed.
func checkDone(key string) bool { return strings.HasPrefix(key, "completed/") }

// wantCheck is the check the App's state calls for, and false when it
// calls for none (nothing pinned yet, or a rollback: the newest commit's
// check stays as it was).
func wantCheck(app *shpyrdv1.App) (checkWant, bool) {
	rec := gitauth.ReadSourceRecord(app.Annotations[shpyrdv1.AnnotationGitSource])
	if rec.Ref != app.GitRevision() {
		rec = gitauth.SourceRecord{}
	}
	pin := app.PinnedCommit()
	if latest := rec.Latest; latest != "" && latest != pin {
		note := commitNote(latest, rec)
		switch latest {
		case rec.Held:
			return checkWant{commit: latest, status: "queued", label: "waiting", title: "Waiting for CI",
				summary: note + "\n\nshpyrd deploys this commit once its other checks pass, and gives up an hour after it was pushed."}, true
		case rec.NotDeployed:
			return checkWant{commit: latest, status: "completed", conclusion: "neutral", label: "not-deployed", title: rec.NotDeployedReason,
				summary: note + "\n\nNot deployed: " + rec.NotDeployedReason + "."}, true
		case rec.Skipped:
			return checkWant{commit: latest, status: "completed", conclusion: "neutral", label: "skipped", title: "Skipped: [skip deploy]",
				summary: note + "\n\nThe commit message asked not to deploy it."}, true
		}
	}
	if pin == "" {
		return checkWant{}, false
	}
	running := releaseIs(app.CurrentRelease(), pin)
	w := checkWant{commit: pin, summary: commitNote(pin, rec)}
	if cur := app.CurrentRelease(); running && cur.Description != "" {
		w.summary = cur.Description
	}
	switch phase := app.Status.Phase; {
	case phase == shpyrdv1.PhaseFailed:
		w.status, w.conclusion, w.label, w.title = "completed", "failure", "failure", "Failed"
		if msg := lastLines(app.Status.Message, checkSummaryLines, checkSummaryMax); msg != "" {
			w.summary += "\n\n```\n" + msg + "\n```"
		}
	case phase == shpyrdv1.PhaseRunning && running:
		w.status, w.conclusion, w.label, w.title = "completed", "success", "success", "Running"
		if app.Status.URL != "" {
			w.title = "Running at " + app.Status.URL
		}
	case phase == shpyrdv1.PhaseRunning && deployedBefore(app, pin):
		// A rollback runs an older release: the commit's check stays.
		return checkWant{}, false
	case phase == shpyrdv1.PhaseDeploying && running && configRelease(app):
		// A release of config or sizes rolls out: the commit's check stays.
		return checkWant{}, false
	case phase == shpyrdv1.PhaseDeploying && running:
		w.status, w.label, w.title = "in_progress", "deploying", "Deploying"
	default:
		w.status, w.label, w.title = "in_progress", "building", "Building"
	}
	return w, true
}

// commitNote names a commit as its release would: "Push a1b2c3d by
// alice: fix login" when the record knows it.
func commitNote(sha string, rec gitauth.SourceRecord) string {
	if rec.Latest == sha {
		return pushNote(sha, rec.Author, rec.Message)
	}
	return "Commit " + shortCommit(sha)
}

// releaseIs says the release was built from sha (Release.Source keeps 12
// characters of it).
func releaseIs(rel *shpyrdv1.Release, sha string) bool {
	return rel != nil && len(rel.Source) >= 7 && strings.HasPrefix(sha, rel.Source)
}

// deployedBefore says a release of the history ran sha.
func deployedBefore(app *shpyrdv1.App, sha string) bool {
	for i := range app.Status.Releases {
		if releaseIs(&app.Status.Releases[i], sha) {
			return true
		}
	}
	return false
}

// configRelease says the current release changed no code: its source is
// the one before it.
func configRelease(app *shpyrdv1.App) bool {
	rels := app.Status.Releases
	n := len(rels)
	return n >= 2 && rels[n-1].Source == rels[n-2].Source
}

// lastLines is the end of a message: at most n lines and max bytes.
func lastLines(msg string, n, maxBytes int) string {
	lines := strings.Split(strings.TrimSpace(msg), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	out := strings.Join(lines, "\n")
	if r := []rune(out); len(out) > maxBytes {
		for len(string(r)) > maxBytes {
			r = r[1:]
		}
		out = string(r)
	}
	return out
}

// reportCheck brings the commit's check run to what the App's state calls
// for: a new run for a new commit (an unfinished one for the commit before
// is marked replaced), or the same run updated. A completed run is never
// taken back: a new state that is not completed is a new run. A run
// deleted on GitHub (404) is made again.
func (r *GitHubReporter) reportCheck(ctx context.Context, app *shpyrdv1.App, ref githubRepoRef, rep *githubReport) error {
	want, ok := wantCheck(app)
	if !ok || (want.commit == rep.Commit && want.key() == rep.Check) {
		return nil
	}
	now := r.now()
	cl := ref.client
	if rep.Commit != "" && rep.Commit != want.commit && rep.CheckRun != 0 && !checkDone(rep.Check) {
		replaced := checkWant{commit: rep.Commit, status: "completed", conclusion: "neutral", label: "replaced",
			title: "Replaced by " + shortCommit(want.commit), summary: "shpyrd moved on to " + shortCommit(want.commit) + " before this commit was deployed."}
		if err := cl.UpdateCheckRun(ctx, ref.installation, ref.owner, ref.name, rep.CheckRun, r.checkRun(app, replaced, now)); err != nil && !github.IsNotFound(err) {
			return err
		}
		rep.Check = replaced.key()
	}
	w := r.checkRun(app, want, now)
	if rep.Commit == want.commit && rep.CheckRun != 0 && (!checkDone(rep.Check) || checkDone(want.key())) {
		err := cl.UpdateCheckRun(ctx, ref.installation, ref.owner, ref.name, rep.CheckRun, w)
		if err == nil {
			rep.Check = want.key()
			return nil
		}
		if !github.IsNotFound(err) {
			return err
		}
	}
	id, err := cl.CreateCheckRun(ctx, ref.installation, ref.owner, ref.name, w)
	if err != nil {
		return err
	}
	rep.Commit, rep.CheckRun, rep.Check = want.commit, id, want.key()
	return nil
}

// checkRun is what GitHub is sent for a check.
func (r *GitHubReporter) checkRun(app *shpyrdv1.App, w checkWant, now time.Time) github.CheckRunWrite {
	out := github.CheckRunWrite{Name: "shpyrd / " + project.SlugOf(app), HeadSHA: w.commit, Status: w.status, Conclusion: w.conclusion,
		DetailsURL: r.projectPage(app), ExternalID: string(app.UID), Title: w.title, Summary: w.summary}
	switch w.status {
	case "in_progress":
		out.StartedAt = now
	case "completed":
		out.CompletedAt = now
	}
	return out
}

// projectPage is the project's page on its workspace's dashboard.
func (r *GitHubReporter) projectPage(app *shpyrdv1.App) string {
	return strings.TrimSuffix(r.Config.issuer(app), "/") + "/projects/" + project.SlugOf(app)
}

// saveReport writes the annotation alone, in a merge patch.
func (r *GitHubReporter) saveReport(ctx context.Context, app *shpyrdv1.App, rep githubReport) error {
	raw, err := json.Marshal(rep)
	if err != nil {
		return err
	}
	patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]string{shpyrdv1.AnnotationGitHubReport: string(raw)}}})
	if err != nil {
		return err
	}
	return r.Patch(ctx, app, client.RawPatch(types.MergePatchType, patch))
}

// failed counts a failed write and says how long to wait: 30 s, doubling
// up to 10 minutes. It logs at most once a minute per App.
func (r *GitHubReporter) failed(ctx context.Context, app *shpyrdv1.App, err error) time.Duration {
	now := r.now()
	r.mu.Lock()
	if r.failing == nil {
		r.failing = map[types.UID]reportFailure{}
	}
	f := r.failing[app.UID]
	f.count++
	wait := min(reportBackoff<<min(f.count-1, 10), reportMaxBackoff)
	f.until = now.Add(wait)
	logIt := f.logged.IsZero() || now.Sub(f.logged) >= reportLogEvery
	if logIt {
		f.logged = now
	}
	r.failing[app.UID] = f
	r.mu.Unlock()
	if logIt {
		log.FromContext(ctx).Info("could not report to GitHub; the deploy goes on, the report is sent later",
			"app", app.Namespace+"/"+app.Name, "retry", wait.String(), "err", err.Error())
	}
	return wait
}

// waiting is how long a failing App still waits; 0 when it does not.
func (r *GitHubReporter) waiting(uid types.UID) time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, ok := r.failing[uid]
	if !ok {
		return 0
	}
	return max(f.until.Sub(r.now()), 0)
}

func (r *GitHubReporter) forget(uid types.UID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.failing, uid)
}

func (r *GitHubReporter) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}
```

Note `waiting` calls `r.now()` under `r.mu`; `now` takes no lock, so this is safe.

- [ ] **Step 4: Register it**

In `pkg/server/server.go`, after the block that ends `return nil, fmt.Errorf("source watcher: %w", err)` and its closing `}`:

```go
	// The GitHub reporter (RFC-0054): check runs and Deployments on GitHub
	// for the Apps its App covers; the dashboard's address from the App
	// controller's Config.
	reporter := &controller.GitHubReporter{Client: mgr.GetClient(), GitHub: githubApp, Config: rec.Config}
	if err := reporter.SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("github reporter: %w", err)
	}
```

- [ ] **Step 5: Run the tests**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp go build ./... && GOTMPDIR=/home/nkr/.cache/gotmp go vet ./internal/controller/ ./pkg/server/ && GOTMPDIR=/home/nkr/.cache/gotmp go test ./internal/controller/ ./pkg/server/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/controller/github_report.go internal/controller/github_report_test.go pkg/server/server.go
git commit -m "feat(controller): the GitHub reporter: a shpyrd / <project> check run on each commit (RFC-0054)

Co-Authored-By: <your model name> <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 8: The reporter's Deployments

**Files:**
- Modify: `internal/controller/github_report.go` (`report`; new `deployWant`, `wantDeployment`, `fullCommitOf`, `reportDeployment`, `newDeployment`, `deploymentStatus`, `deploymentDone`)
- Test: `internal/controller/github_report_test.go` (append)

**Interfaces:**
- Consumes: Task 7's reporter and helpers (`githubReporter`, `shopAt`, `release`, `setStatus`, `reportOf`); `github.Client.CreateDeployment`, `CreateDeploymentStatus`, `DeploymentWrite`, `DeploymentStatusWrite` (Task 4); `shpyrdv1.ConditionBuilt`.
- Produces:
  - `type deployWant struct { release int; ref, description, state string }`, `func wantDeployment(app *shpyrdv1.App) (deployWant, bool)`
  - `func fullCommitOf(app *shpyrdv1.App, src string) string`
  - Deployments: `environment` = the project's slug, `ref` = the commit, statuses `in_progress`, `success` (with `environment_url`), `failure`; `log_url` = the project's page

- [ ] **Step 1: Write the failing tests**

Append to `internal/controller/github_report_test.go` (add `"k8s.io/apimachinery/pkg/api/meta"` to the imports):

```go
func TestAReleaseOfANewCommitIsAGitHubDeployment(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseDeploying, release(1, testCommit, "Push 0123456 by alice: fix login"))
	r, c, fake, _ := githubReporter(t, app)
	_, got := reconcileReport(t, r, app)
	d := fake.Deployments()
	if len(d) != 1 || d[0].Ref != testCommit || d[0].Environment != "shop" || d[0].AutoMerge || d[0].Transient ||
		d[0].RequiredContexts == nil || len(d[0].RequiredContexts) != 0 || len(d[0].Statuses) != 1 || d[0].Statuses[0].State != "in_progress" {
		t.Fatalf("%+v", d)
	}
	if rep := reportOf(got); rep.Release != 1 || rep.Deployment != d[0].ID || rep.DeploymentState != "in_progress" {
		t.Errorf("annotation %+v", rep)
	}
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) { st.Phase = shpyrdv1.PhaseRunning })
	reconcileReport(t, r, app)
	d = fake.Deployments()
	if len(d) != 1 || len(d[0].Statuses) != 2 || d[0].Statuses[1].State != "success" || d[0].Statuses[1].EnvironmentURL != "https://shop.example.test" ||
		d[0].Statuses[1].LogURL != "https://acme.shpyrd.test/projects/shop" {
		t.Errorf("%+v", d)
	}
}

func TestAReleaseThatOnlyChangesConfigMakesNoDeployment(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseRunning, release(1, testCommit, "Push 0123456 by alice: fix login"))
	r, c, fake, _ := githubReporter(t, app)
	reconcileReport(t, r, app)
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) {
		st.Phase = shpyrdv1.PhaseDeploying
		st.Releases = append(st.Releases, release(2, testCommit, "Set LOG_LEVEL"))
	})
	reconcileReport(t, r, app)
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) { st.Phase = shpyrdv1.PhaseRunning })
	reconcileReport(t, r, app)
	d := fake.Deployments()
	if len(d) != 1 || len(d[0].Statuses) != 1 || d[0].Statuses[0].State != "success" {
		t.Errorf("a config release neither makes a Deployment nor takes one back to in progress: %+v", d)
	}
	if runs := fake.CheckRunsOn(testCommit); len(runs) != 1 || runs[0].Conclusion != "success" {
		t.Errorf("a config release leaves the commit's check as it was: %+v", runs)
	}
}

func TestARollbackKeepsTheCheckAndIsADeployment(t *testing.T) {
	app := shopAt(otherCommit, shpyrdv1.PhaseRunning, release(1, testCommit, "Push 0123456"), release(2, otherCommit, "Push 89abcde by alice: fix login"))
	r, c, fake, _ := githubReporter(t, app)
	reconcileReport(t, r, app)
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) {
		st.Releases = append(st.Releases, release(3, testCommit, "Rollback to v1"))
	})
	_, got := reconcileReport(t, r, app)
	if runs := fake.CheckRunsOn(otherCommit); len(runs) != 1 || runs[0].Conclusion != "success" {
		t.Errorf("the newest commit's check stays: %+v", runs)
	}
	if runs := fake.CheckRunsOn(testCommit); len(runs) != 0 {
		t.Errorf("no check for the rolled-back commit: %+v", runs)
	}
	d := fake.Deployments()
	if len(d) != 2 || d[1].Ref != short(testCommit) || d[1].Description != "Rollback to v1" || d[1].Statuses[0].State != "success" || reportOf(got).Release != 3 {
		t.Errorf("%+v, annotation %+v", d, reportOf(got))
	}
}

func TestAFailedRolloutFailsTheDeploymentAndAFailedBuildLeavesIt(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseRunning, release(1, testCommit, "Push 0123456 by alice: fix login"))
	r, c, fake, _ := githubReporter(t, app)
	reconcileReport(t, r, app)
	// The next commit's build fails: the running release's Deployment stays.
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) {
		st.Phase, st.Message = shpyrdv1.PhaseFailed, "The build failed."
		meta.SetStatusCondition(&st.Conditions, metav1.Condition{Type: shpyrdv1.ConditionBuilt, Status: metav1.ConditionFalse, Reason: "BuildFailed", Message: "The build failed."})
	})
	reconcileReport(t, r, app)
	if d := fake.Deployments(); len(d[0].Statuses) != 1 {
		t.Errorf("a failed build is not a failed rollout: %+v", d)
	}
	// The rollout of the release fails.
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) {
		st.Message = "web: CrashLoopBackOff"
		meta.SetStatusCondition(&st.Conditions, metav1.Condition{Type: shpyrdv1.ConditionBuilt, Status: metav1.ConditionTrue, Reason: "Built", Message: "built"})
	})
	reconcileReport(t, r, app)
	if d := fake.Deployments(); len(d[0].Statuses) != 2 || d[0].Statuses[1].State != "failure" {
		t.Errorf("%+v", d)
	}
}

func TestADeletedDeploymentStartsANewOne(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseDeploying, release(1, testCommit, "Push 0123456 by alice: fix login"))
	r, c, fake, _ := githubReporter(t, app)
	_, got := reconcileReport(t, r, app)
	fake.DeleteDeployment(reportOf(got).Deployment)
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) { st.Phase = shpyrdv1.PhaseRunning })
	_, got = reconcileReport(t, r, app)
	d := fake.Deployments()
	if len(d) != 1 || d[0].Ref != testCommit || len(d[0].Statuses) != 1 || d[0].Statuses[0].State != "success" || reportOf(got).Deployment != d[0].ID {
		t.Errorf("%+v, annotation %+v", d, reportOf(got))
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp go test ./internal/controller/ -run 'Deployment|Rollback|ConfigMakesNo'`
Expected: FAIL: no Deployment is made (`len(d) == 0`).

- [ ] **Step 3: Deployments**

In `internal/controller/github_report.go`, add `"regexp"` and `"k8s.io/apimachinery/pkg/api/meta"` to the imports; `report` becomes:

```go
func (r *GitHubReporter) report(ctx context.Context, app *shpyrdv1.App, ref githubRepoRef, old githubReport) (githubReport, error) {
	next := old
	if err := r.reportCheck(ctx, app, ref, &next); err != nil {
		return next, err
	}
	if err := r.reportDeployment(ctx, app, ref, &next); err != nil {
		return next, err
	}
	return next, nil
}
```

Append:

```go
// commitSource is a release's source when it is a commit (Release.Source
// keeps 12 characters of it).
var commitSource = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// deployWant is the Deployment of the newest release that changed the
// code: the release, the commit, and the state of its rollout ("" says
// nothing new: a build of the next commit, or a build that failed).
type deployWant struct {
	release     int
	ref         string
	description string
	state       string
}

// wantDeployment is the Deployment the App's history calls for; false
// when its newest code is not a commit.
func wantDeployment(app *shpyrdv1.App) (deployWant, bool) {
	rels := app.Status.Releases
	for i := len(rels) - 1; i >= 0; i-- {
		if i > 0 && rels[i].Source == rels[i-1].Source {
			continue // the same code: config vars, sizes, a restart
		}
		if !commitSource.MatchString(rels[i].Source) {
			return deployWant{}, false
		}
		w := deployWant{release: rels[i].Number, ref: fullCommitOf(app, rels[i].Source), description: firstLine(rels[i].Description)}
		switch app.Status.Phase {
		case shpyrdv1.PhaseDeploying:
			w.state = "in_progress"
		case shpyrdv1.PhaseRunning:
			w.state = "success"
		case shpyrdv1.PhaseFailed:
			if !meta.IsStatusConditionFalse(app.Status.Conditions, shpyrdv1.ConditionBuilt) {
				w.state = "failure"
			}
		}
		return w, true
	}
	return deployWant{}, false
}

// fullCommitOf is the full SHA a release's 12 characters name, when the
// pin or the watcher's record knows it; else the 12 characters, which
// GitHub resolves.
func fullCommitOf(app *shpyrdv1.App, src string) string {
	rec := gitauth.ReadSourceRecord(app.Annotations[shpyrdv1.AnnotationGitSource])
	for _, sha := range []string{app.PinnedCommit(), rec.Latest, rec.Held, rec.NotDeployed, rec.Skipped} {
		if sha != "" && strings.HasPrefix(sha, src) {
			return sha
		}
	}
	return src
}

// deploymentDone says a Deployment state is final; a config release
// rolling out after it does not take it back to in progress.
func deploymentDone(state string) bool { return state == "success" || state == "failure" }

// reportDeployment makes the newest code release's Deployment, once, and
// moves its state on. One deleted on GitHub (404) is made again.
func (r *GitHubReporter) reportDeployment(ctx context.Context, app *shpyrdv1.App, ref githubRepoRef, rep *githubReport) error {
	want, ok := wantDeployment(app)
	if !ok {
		return nil
	}
	if want.release != rep.Release || rep.Deployment == 0 {
		if err := r.newDeployment(ctx, app, ref, want, rep); err != nil {
			return err
		}
	}
	if want.state == "" || want.state == rep.DeploymentState || (want.state == "in_progress" && deploymentDone(rep.DeploymentState)) {
		return nil
	}
	cl := ref.client
	err := cl.CreateDeploymentStatus(ctx, ref.installation, ref.owner, ref.name, rep.Deployment, r.deploymentStatus(app, want.state))
	if github.IsNotFound(err) {
		if err = r.newDeployment(ctx, app, ref, want, rep); err == nil {
			err = cl.CreateDeploymentStatus(ctx, ref.installation, ref.owner, ref.name, rep.Deployment, r.deploymentStatus(app, want.state))
		}
	}
	if err != nil {
		return err
	}
	rep.DeploymentState = want.state
	return nil
}

// newDeployment makes the release's Deployment: environment the project,
// shpyrd having decided to deploy (no merge, no required contexts).
func (r *GitHubReporter) newDeployment(ctx context.Context, app *shpyrdv1.App, ref githubRepoRef, want deployWant, rep *githubReport) error {
	id, err := ref.client.CreateDeployment(ctx, ref.installation, ref.owner, ref.name,
		github.DeploymentWrite{Ref: want.ref, Environment: project.SlugOf(app), Description: want.description})
	if err != nil {
		return err
	}
	rep.Release, rep.Deployment, rep.DeploymentState = want.release, id, ""
	return nil
}

// deploymentStatus is a Deployment's state, with the app's URL once it
// runs and the project's page for its log.
func (r *GitHubReporter) deploymentStatus(app *shpyrdv1.App, state string) github.DeploymentStatusWrite {
	s := github.DeploymentStatusWrite{State: state, LogURL: r.projectPage(app)}
	if state == "success" {
		s.EnvironmentURL = app.Status.URL
	}
	return s
}
```

- [ ] **Step 4: Run the tests**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp go vet ./internal/controller/ && GOTMPDIR=/home/nkr/.cache/gotmp go test ./internal/controller/`
Expected: PASS, Task 7's tests included (`TestEveryReportWriteUsesTheReportToken` now sees the Deployment's writes too).

- [ ] **Step 5: Commit**

```bash
git add internal/controller/github_report.go internal/controller/github_report_test.go
git commit -m "feat(controller): a GitHub Deployment for each release that changes the code (RFC-0054)

Co-Authored-By: <your model name> <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 9: The API and the CLI

**Files:**
- Create: `pkg/api/github_features.go`
- Modify: `pkg/api/apps.go` (`AppDetail` 130-146; `getApp` 413-419; `UpdateAppRequest` 546-560 and `updateApp` 565-626; `DeployRequest` 642-675 and `deployApp` 677-800)
- Modify: `internal/cli/deploy.go` (the flag variables 26-38, the start of `RunE` ~67, the git case ~204-211, after the deploy answer is read ~259, the flags ~322)
- Modify: `internal/cli/appclient.go` (`detailToApp` ~1069), `internal/cli/apps.go` (`printAppInfo`, after the `Source:` line ~447)
- Test: `pkg/api/source_settings_test.go` (new), `internal/cli/source_settings_test.go` (new)

**Interfaces:**
- Consumes: `gitauth.CleanWatchPaths`, `gitauth.ValidWatchPaths` (Task 2); the CRD fields (Task 1); PR 3's `s.githubApp`, `s.store.GitHubInstallations`, `workspaceOf`, `githubServer`, `connectAcme`, `onGitHub`; `runShpyrd` (`internal/cli/githooks_test.go`).
- Produces:
  - `AppDetail.GitHubFeatures bool` (`json:"githubFeatures"`), `AppDetail.SourceNote string` (`json:"sourceNote,omitempty"`)
  - `UpdateAppRequest.WatchPaths *[]string` (`json:"watchPaths,omitempty"`), `UpdateAppRequest.WaitForChecks *bool` (`json:"waitForChecks,omitempty"`); the same two fields on `DeployRequest`
  - `func (s *Server) githubCovers(ctx context.Context, app *shpyrdv1.App) bool`, `func (s *Server) projectDetail(ctx context.Context, app *shpyrdv1.App) AppDetail`, `const githubSourceNote`
  - `func gitSourceSettings(paths []string, noPaths, wait, noWait bool) (*[]string, *bool, error)` (CLI)
  - `shpyrd deploy --watch-path GLOB (repeatable) --no-watch-paths --wait-for-ci --no-wait-for-ci`; `shpyrd projects info` lines `Commit:     89abcde waits for CI (since 2m)` and `Commit:     89abcde not deployed: CI failed: test`

- [ ] **Step 1: Write the failing API tests**

`pkg/api/source_settings_test.go`:

```go
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

func detailOf(t *testing.T, body []byte) AppDetail {
	t.Helper()
	var d AppDetail
	if err := json.Unmarshal(body, &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestWatchPathsAndWaitForCIAreProjectSettings(t *testing.T) {
	s, cr, _ := githubServer(t, onGitHub("shop", "https://github.com/acme/shop", "main", ""))
	connectAcme(t, s, store.DefaultWorkspace)
	rec := do(t, s, "PATCH", "/api/projects/shop", `{"watchPaths":[" services/api/** ","","!**/*.md"],"waitForChecks":true}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}
	d := detailOf(t, rec.Body.Bytes())
	git := d.Spec.Source.Git
	if strings.Join(git.WatchPaths, "|") != "services/api/**|!**/*.md" || !git.WaitForChecks || !d.GitHubFeatures || d.SourceNote != "" {
		t.Errorf("view: %+v, features %v, note %q", git, d.GitHubFeatures, d.SourceNote)
	}
	if got := appNamed(t, cr, "shop").Spec.Source.Git; len(got.WatchPaths) != 2 || !got.WaitForChecks {
		t.Errorf("stored %+v", got)
	}
	if rec := do(t, s, "PATCH", "/api/projects/shop", `{"watchPaths":[]}`, true); rec.Code != http.StatusOK {
		t.Fatalf("clear: %d %s", rec.Code, rec.Body.String())
	}
	if got := appNamed(t, cr, "shop").Spec.Source.Git; got.WatchPaths != nil || !got.WaitForChecks {
		t.Errorf("[] removes the watch paths and leaves wait for CI: %+v", got)
	}
	if rec := do(t, s, "GET", "/api/projects/shop", "", true); !detailOf(t, rec.Body.Bytes()).GitHubFeatures {
		t.Errorf("the project's view says the App covers it: %s", rec.Body.String())
	}
}

func TestAWatchPathThatCannotBeReadIsRefused(t *testing.T) {
	s, cr, _ := githubServer(t, onGitHub("shop", "https://github.com/acme/shop", "main", ""))
	many := make([]string, 21)
	for i := range many {
		many[i] = fmt.Sprintf("%q", fmt.Sprintf("dir%d/**", i))
	}
	for body, words := range map[string]string{
		`{"watchPaths":["services/a**"]}`:                         "stands alone",
		`{"watchPaths":[` + strings.Join(many, ",") + `]}`:        "at most 20",
		`{"watchPaths":["` + strings.Repeat("a", 201) + `"]}`:     "200 characters",
	} {
		if rec := do(t, s, "PATCH", "/api/projects/shop", body, true); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), words) {
			t.Errorf("%.40s: %d %s", body, rec.Code, rec.Body.String())
		}
	}
	if got := appNamed(t, cr, "shop").Spec.Source.Git; got.WatchPaths != nil {
		t.Errorf("nothing stored: %+v", got)
	}
}

func TestTheSettingsSaySoWhereTheGitHubAppDoesNotReach(t *testing.T) {
	site := sampleApp("site", shpyrdv1.PhaseRunning)
	site.Spec.Source = &shpyrdv1.Source{Blob: &shpyrdv1.BlobSource{URL: "http://shpyrd-server/api/sources/ab.tgz", SHA256: "ab"}}
	s, _ := newTestServer(t, nil, []client.Object{sampleApp("web1", shpyrdv1.PhaseRunning), site})
	rec := do(t, s, "PATCH", "/api/projects/web1", `{"waitForChecks":true}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}
	if d := detailOf(t, rec.Body.Bytes()); d.GitHubFeatures || d.SourceNote != githubSourceNote {
		t.Errorf("features %v, note %q", d.GitHubFeatures, d.SourceNote)
	}
	if rec := do(t, s, "PATCH", "/api/projects/site", `{"waitForChecks":true}`, true); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "git repository") {
		t.Errorf("an archive has no branch: %d %s", rec.Code, rec.Body.String())
	}
}

func TestADeployKeepsTheProjectsWatchPathsUnlessItSays(t *testing.T) {
	app := onGitHub("shop", "https://github.com/acme/shop", "main", "")
	app.Spec.Source.Git.WatchPaths, app.Spec.Source.Git.WaitForChecks = []string{"services/api/**"}, true
	s, cr, _ := githubServer(t, app)
	// The git source's own fields are not read in a deploy: a bool cannot
	// say "keep".
	if rec := do(t, s, "POST", "/api/projects/shop/deploy", `{"git":{"url":"https://github.com/acme/shop","revision":"main","watchPaths":["x/**"]}}`, true); rec.Code != http.StatusAccepted {
		t.Fatalf("deploy: %d %s", rec.Code, rec.Body.String())
	}
	if got := appNamed(t, cr, "shop").Spec.Source.Git; strings.Join(got.WatchPaths, "|") != "services/api/**" || !got.WaitForChecks {
		t.Errorf("kept: %+v", got)
	}
	if rec := do(t, s, "POST", "/api/projects/shop/deploy", `{"git":{"url":"https://github.com/acme/shop","revision":"main"},"watchPaths":[],"waitForChecks":false}`, true); rec.Code != http.StatusAccepted {
		t.Fatalf("deploy: %d %s", rec.Code, rec.Body.String())
	}
	if got := appNamed(t, cr, "shop").Spec.Source.Git; got.WatchPaths != nil || got.WaitForChecks {
		t.Errorf("cleared: %+v", got)
	}
	if rec := do(t, s, "POST", "/api/projects/shop/deploy", `{"git":{"url":"https://github.com/acme/shop"},"watchPaths":["a**"]}`, true); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("a bad pattern: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, s, "POST", "/api/projects/shop/deploy", `{"image":"ghcr.io/acme/shop:1","waitForChecks":true}`, true); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "go with git") {
		t.Errorf("with an image: %d %s", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 2: Write the failing CLI tests**

`internal/cli/source_settings_test.go`:

```go
package cli

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestGitSourceSettings(t *testing.T) {
	paths, wait, err := gitSourceSettings(nil, false, false, false)
	if err != nil || paths != nil || wait != nil {
		t.Errorf("no flag leaves the project's settings: %v %v %v", paths, wait, err)
	}
	paths, wait, err = gitSourceSettings([]string{"services/api/**", "!**/*.md"}, false, true, false)
	if err != nil || paths == nil || strings.Join(*paths, "|") != "services/api/**|!**/*.md" || wait == nil || !*wait {
		t.Errorf("set: %v %v %v", paths, wait, err)
	}
	paths, wait, err = gitSourceSettings(nil, true, false, true)
	if err != nil || paths == nil || len(*paths) != 0 || wait == nil || *wait {
		t.Errorf("cleared: %v %v %v", paths, wait, err)
	}
	if _, _, err := gitSourceSettings([]string{"a/**"}, true, false, false); err == nil {
		t.Error("--watch-path and --no-watch-paths contradict each other")
	}
	if _, _, err := gitSourceSettings(nil, false, true, true); err == nil {
		t.Error("--wait-for-ci and --no-wait-for-ci contradict each other")
	}
}

func TestDeploySendsWatchPathsAndWaitForCIAndSaysWhenTheyDoNotApply(t *testing.T) {
	var sent string
	out, err := runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/projects/shop":
			_, _ = w.Write([]byte(`{"slug":"shop","spec":{"source":{"git":{"url":"https://gitlab.example.test/acme/shop","revision":"main"}}},"status":{"phase":"Running","generation":1,"releases":[]}}`))
		case "POST /api/projects/shop/deploy":
			raw, _ := io.ReadAll(r.Body)
			sent = string(raw)
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"slug":"shop","sourceNote":"Watch paths and wait for CI work through the GitHub App, which does not cover this repository: every push deploys.","spec":{},"status":{"phase":"Building","generation":2,"releases":[]}}`))
		default:
			http.NotFound(w, r)
		}
	}, "deploy", "--project", "shop", "--git", "https://gitlab.example.test/acme/shop", "--watch-path", "services/api/**", "--watch-path", "!**/*.md", "--wait-for-ci", "--no-wait")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(sent, `"watchPaths":["services/api/**","!**/*.md"]`) || !strings.Contains(sent, `"waitForChecks":true`) {
		t.Errorf("sent %s", sent)
	}
	if !strings.Contains(out, "which does not cover this repository") {
		t.Errorf("the note is said: %s", out)
	}
}

func TestWatchPathFlagsGoWithGit(t *testing.T) {
	_, err := runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("nothing is sent: %s %s", r.Method, r.URL.Path)
	}, "deploy", "--project", "shop", "--wait-for-ci")
	if err == nil || !strings.Contains(err.Error(), "go with --git") {
		t.Errorf("err = %v", err)
	}
}

func TestProjectsInfoShowsAHeldAndANotDeployedCommit(t *testing.T) {
	since := time.Now().Add(-2 * time.Minute).UTC().Format(time.RFC3339)
	for source, want := range map[string]string{
		`{"commit":"0123456789abcdef0123456789abcdef01234567","latest":"89abcdef0123456789abcdef0123456789abcdef","held":"89abcdef0123456789abcdef0123456789abcdef","heldSince":"` + since + `"}`: "Commit:     89abcde waits for CI (since 2m)",
		`{"commit":"0123456789abcdef0123456789abcdef01234567","latest":"89abcdef0123456789abcdef0123456789abcdef","notDeployed":"89abcdef0123456789abcdef0123456789abcdef","notDeployedReason":"CI failed: test"}`: "Commit:     89abcde not deployed: CI failed: test",
	} {
		out, err := runShpyrd(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method+" "+r.URL.Path != "GET /api/projects/shop" {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(`{"slug":"shop","spec":{"source":{"git":{"url":"https://github.com/acme/shop","revision":"main","waitForChecks":true}}},"status":{"phase":"Running","releases":[],"source":` + source + `}}`))
		}, "projects", "info", "shop")
		if err != nil || !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s (%v)", want, out, err)
		}
	}
}
```

- [ ] **Step 3: Run them to see them fail**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp go test ./pkg/api/ -run 'WatchPath|SettingsSaySo|DeployKeepsTheProjects' ; GOTMPDIR=/home/nkr/.cache/gotmp go test ./internal/cli/ -run 'GitSourceSettings|WatchPathsAndWaitForCI|WatchPathFlags|HeldAndANotDeployed'`
Expected: FAIL to compile: `d.GitHubFeatures undefined`, `undefined: gitSourceSettings`.

- [ ] **Step 4: The API**

`pkg/api/github_features.go`:

```go
package api

import (
	"context"
	"strings"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
)

// githubSourceNote says watch paths or wait for CI are set where they do
// nothing (RFC-0054).
const githubSourceNote = "Watch paths and wait for CI work through the GitHub App, which does not cover this repository: every push deploys. A workspace admin connects GitHub for the repository's owner in Workspace settings → Git."

// githubCovers says the GitHub App covers the App's repository, by the
// controller's test without a call to GitHub: a github.com URL, the App
// configured, a live, unsuspended installation of the App's workspace on
// the repository's owner.
func (s *Server) githubCovers(ctx context.Context, app *shpyrdv1.App) bool {
	if app.Spec.Source == nil || app.Spec.Source.Git == nil {
		return false
	}
	repo, err := gitauth.Parse(app.Spec.Source.Git.URL)
	if err != nil || !strings.EqualFold(repo.Host, "github.com") {
		return false
	}
	owner, _, ok := strings.Cut(repo.Path, "/")
	if !ok {
		return false
	}
	if _, err := s.githubApp.Client(ctx); err != nil {
		return false
	}
	list, err := s.store.GitHubInstallations(ctx, workspaceOf(app))
	if err != nil {
		return false
	}
	for _, inst := range list {
		if strings.EqualFold(inst.AccountLogin, owner) && inst.SuspendedAt == nil {
			return true
		}
	}
	return false
}

// projectDetail is a project's view with what the GitHub App adds: whether
// it covers the repository, and a note when the settings it needs are set
// without it.
func (s *Server) projectDetail(ctx context.Context, app *shpyrdv1.App) AppDetail {
	d := detail(app, s.buildsByDigest(ctx, app))
	d.GitHubFeatures = s.githubCovers(ctx, app)
	if src := app.Spec.Source; src != nil && src.Git != nil && !d.GitHubFeatures && (len(src.Git.WatchPaths) > 0 || src.Git.WaitForChecks) {
		d.SourceNote = githubSourceNote
	}
	return d
}
```

In `pkg/api/apps.go`:

`AppDetail` gains, after `Processes`:

```go
	// GitHubFeatures: the GitHub App covers the repository, so watch paths
	// and wait for CI apply (RFC-0054). SourceNote says when they are set
	// and do not.
	GitHubFeatures bool   `json:"githubFeatures"`
	SourceNote     string `json:"sourceNote,omitempty"`
```

`getApp`'s answer becomes `c.JSON(http.StatusOK, s.projectDetail(c.Request.Context(), app))`; so do `updateApp`'s final `c.JSON(http.StatusOK, …)` and `deployApp`'s final `c.JSON(http.StatusAccepted, …)` (with their status codes unchanged).

`UpdateAppRequest` gains, after `AutoDeploy`:

```go
	// WatchPaths replaces the git source's watch paths; [] removes them.
	// WaitForChecks switches wait for CI. Both need the GitHub App
	// (RFC-0054); set without it, the view's sourceNote says so.
	WatchPaths    *[]string `json:"watchPaths,omitempty"`
	WaitForChecks *bool     `json:"waitForChecks,omitempty"`
```

In `updateApp`, the "nothing to update" check becomes:

```go
	if req.Name == nil && req.Description == nil && req.Featured == nil && req.Icon == nil && req.IconColor == nil && req.AutoDeploy == nil &&
		req.WatchPaths == nil && req.WaitForChecks == nil {
		abort(c, http.StatusBadRequest, errors.New("nothing to update: give name, description, featured, icon, iconColor, autoDeploy, watchPaths or waitForChecks"))
		return
	}
	if req.WatchPaths != nil {
		cleaned := gitauth.CleanWatchPaths(*req.WatchPaths)
		if err := gitauth.ValidWatchPaths(cleaned); err != nil {
			abort(c, http.StatusUnprocessableEntity, err)
			return
		}
		req.WatchPaths = &cleaned
	}
```

Inside the `mutateApp` closure, after the `req.AutoDeploy` block:

```go
		if req.WatchPaths != nil || req.WaitForChecks != nil {
			if a.Spec.Source == nil || a.Spec.Source.Git == nil {
				return errors.New("the project does not deploy from a git repository: watch paths and wait for CI follow a git branch")
			}
			if req.WatchPaths != nil {
				a.Spec.Source.Git.WatchPaths = *req.WatchPaths
				changes = append(changes, fmt.Sprintf("watch paths %q", *req.WatchPaths))
			}
			if req.WaitForChecks != nil {
				a.Spec.Source.Git.WaitForChecks = *req.WaitForChecks
				changes = append(changes, fmt.Sprintf("wait for CI %v", *req.WaitForChecks))
			}
		}
```

and the `project.rename` check gains `&& req.WatchPaths == nil && req.WaitForChecks == nil`.

`DeployRequest` gains, after `Note`:

```go
	// WatchPaths and WaitForChecks set the git source's (RFC-0054); left
	// out, the project's stay. The git source's own fields of those names
	// are not read: a bool cannot say "keep".
	WatchPaths    *[]string `json:"watchPaths,omitempty"`
	WaitForChecks *bool     `json:"waitForChecks,omitempty"`
```

In `deployApp`, after the `validateDeployRequest` block:

```go
	if (req.WatchPaths != nil || req.WaitForChecks != nil) && !hasGit {
		abort(c, http.StatusBadRequest, errors.New("watchPaths and waitForChecks go with git"))
		return
	}
	if req.WatchPaths != nil {
		cleaned := gitauth.CleanWatchPaths(*req.WatchPaths)
		if err := gitauth.ValidWatchPaths(cleaned); err != nil {
			abort(c, http.StatusUnprocessableEntity, err)
			return
		}
		req.WatchPaths = &cleaned
	}
```

and in the `case hasGit:` branch of the closure, after the auto-deploy lines and before `a.Spec.Source = &shpyrdv1.Source{Git: req.Git, SubPath: req.SubPath}`:

```go
			// Watch paths and wait for CI are the project's settings too.
			req.Git.WatchPaths, req.Git.WaitForChecks = nil, false
			if a.Spec.Source != nil && a.Spec.Source.Git != nil {
				req.Git.WatchPaths, req.Git.WaitForChecks = a.Spec.Source.Git.WatchPaths, a.Spec.Source.Git.WaitForChecks
			}
			if req.WatchPaths != nil {
				req.Git.WatchPaths = *req.WatchPaths
			}
			if req.WaitForChecks != nil {
				req.Git.WaitForChecks = *req.WaitForChecks
			}
```

`apps.go` already imports `gitauth`? If not, add `"github.com/shpyrd-io/shpyrd/pkg/gitauth"`.

- [ ] **Step 5: The CLI**

In `internal/cli/deploy.go`, the flag variables gain:

```go
		watchPaths   []string
		noWatchPaths bool
		waitForCI    bool
		noWaitForCI  bool
```

At the start of `RunE`, after the two auto-deploy checks:

```go
			if gitURL == "" && (len(watchPaths) > 0 || noWatchPaths || waitForCI || noWaitForCI) {
				return errors.New("--watch-path, --no-watch-paths, --wait-for-ci and --no-wait-for-ci go with --git")
			}
			if _, _, err := gitSourceSettings(watchPaths, noWatchPaths, waitForCI, noWaitForCI); err != nil {
				return err
			}
```

In the `case gitURL != "":` branch, after `req.Git = src`:

```go
				req.WatchPaths, req.WaitForChecks, _ = gitSourceSettings(watchPaths, noWatchPaths, waitForCI, noWaitForCI)
```

After `var after api.AppDetail` is read (its `json.Unmarshal` check):

```go
			if after.SourceNote != "" {
				fmt.Fprintf(out, "    %s\n", after.SourceNote)
			}
```

After the `--no-auto-deploy` flag:

```go
	cmd.Flags().StringArrayVar(&watchPaths, "watch-path", nil, "with --git: deploy only commits that change a file matching this glob, from the repository root (repeatable; ! excludes, the last match wins; GitHub App only)")
	cmd.Flags().BoolVar(&noWatchPaths, "no-watch-paths", false, "with --git: remove the watch paths, so every commit deploys")
	cmd.Flags().BoolVar(&waitForCI, "wait-for-ci", false, "with --git: deploy a commit only once its other checks on GitHub pass (GitHub App only)")
	cmd.Flags().BoolVar(&noWaitForCI, "no-wait-for-ci", false, "with --git: deploy commits without waiting for CI")
```

and, after `gitDeploySource`:

```go
// gitSourceSettings is what --watch-path, --no-watch-paths, --wait-for-ci
// and --no-wait-for-ci ask of the server; nil leaves the project's setting.
func gitSourceSettings(paths []string, noPaths, wait, noWait bool) (*[]string, *bool, error) {
	if len(paths) > 0 && noPaths {
		return nil, nil, errors.New("--watch-path and --no-watch-paths contradict each other")
	}
	if wait && noWait {
		return nil, nil, errors.New("--wait-for-ci and --no-wait-for-ci contradict each other")
	}
	var watch *[]string
	switch {
	case noPaths:
		none := []string{}
		watch = &none
	case len(paths) > 0:
		given := append([]string(nil), paths...)
		watch = &given
	}
	var waitFor *bool
	if wait || noWait {
		v := wait
		waitFor = &v
	}
	return watch, waitFor, nil
}
```

The long help's line `shpyrd deploy --git https://github.com/o/r  build from a Git URL; …` is followed by:

```
  shpyrd deploy --git ... --watch-path 'services/api/**' --wait-for-ci   deploy only API changes, once CI passes (GitHub App)
```

In `internal/cli/appclient.go`, `detailToApp`'s `app.Status = shpyrdv1.AppStatus{…}` gains `Source: d.Status.Source,`.

In `internal/cli/apps.go`, `printAppInfo`, after the `if app.Spec.Source.SubPath != ""` block (still inside `if app.Spec.Source != nil`):

```go
		if st := app.Status.Source; st != nil {
			switch {
			case st.Held != "" && st.Held == st.Latest:
				since := ""
				if st.HeldSince != nil {
					since = " (since " + age(*st.HeldSince) + ")"
				}
				fmt.Fprintf(out, "Commit:     %s waits for CI%s\n", st.Held[:min(7, len(st.Held))], since)
			case st.NotDeployed != "" && st.NotDeployed == st.Latest && st.NotDeployed != st.Commit:
				fmt.Fprintf(out, "Commit:     %s not deployed: %s\n", st.NotDeployed[:min(7, len(st.NotDeployed))], st.NotDeployedReason)
			}
		}
```

- [ ] **Step 6: Run the tests**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp go build ./... && GOTMPDIR=/home/nkr/.cache/gotmp go vet ./pkg/api/ ./internal/cli/ && GOTMPDIR=/home/nkr/.cache/gotmp go test ./pkg/api/ ./internal/cli/`
Expected: PASS, `TestAutoDeployIsAProjectSetting` and `TestGitDeploySource` included.

- [ ] **Step 7: Commit**

```bash
git add pkg/api/github_features.go pkg/api/apps.go pkg/api/source_settings_test.go internal/cli/deploy.go internal/cli/appclient.go internal/cli/apps.go internal/cli/source_settings_test.go
git commit -m "feat(api,cli): watch paths and wait for CI as project settings; projects info shows a held commit (RFC-0054)

Co-Authored-By: <your model name> <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 10: The dashboard: the Source block's held and not-deployed commits, wait for CI and watch paths

Follow `apps/AGENTS.md` (components from `@shpyrd/ui` only, every call
through `api`, the Mock answers it). If the maintainer asks to look at it
first, run it as a design session (`design/DESIGN_FOR_AGENT.md`: "enter
design mode", target `apps/workspace`, Mock) before the tests below.

**Files:**
- Modify: `apps/workspace/src/api/types.ts` (`Project.spec.source.git` ~207, `Project` 204, `SourceStatus` 254-267)
- Modify: `apps/workspace/src/api/api.ts` (`updateProject`'s body ~146-155)
- Modify: `apps/workspace/src/api/mock.ts` (`updateProject` ~682-700)
- Modify: `apps/workspace/mock/projects.json` (`hello-world`)
- Modify: `apps/workspace/src/screens/project/source.tsx`
- Test: `apps/workspace/src/screens/project/source.test.tsx`, `apps/workspace/src/api/api.test.ts`

**Interfaces:**
- Consumes: Task 9's API: `PATCH /api/projects/:slug` with `watchPaths` and `waitForChecks`, the view's `githubFeatures` and `sourceNote`, `status.source.held`, `heldSince`, `notDeployed`, `notDeployedReason`.
- Produces: `api.updateProject(slug, { watchPaths?: string[]; waitForChecks?: boolean })`; the Source block's lines "Waiting for CI on a1b2c3d (since 2 min)", "a1b2c3d not deployed: CI failed (test)", "a1b2c3d not deployed: no changes in the watch paths"; a "Wait for CI" switch; a "Watch paths" field with the placeholder `services/api/**` / `!**/*.md`; both disabled with "Needs the GitHub App".

- [ ] **Step 1: Write the failing tests**

In `apps/workspace/src/screens/project/source.test.tsx`, `projectWith`'s `git` parameter type becomes
`{ url: string; revision?: string; autoDeploy?: boolean; watchPaths?: string[]; waitForChecks?: boolean } | undefined`, and these tests are appended:

```tsx
it("says a commit waits for CI, and since when", () => {
  show(projectWith({ ...repo, waitForChecks: true }, { commit, latest: newer, held: newer, heldSince: minutesAgo(2), via: "push", checkedAt: minutesAgo(2) }));
  expect(screen.getByText("Waiting for CI on b2c3d4e (since 2 min)")).toBeTruthy();
});

it("says why a commit was not deployed, in the Source block's words", () => {
  show(projectWith({ ...repo, waitForChecks: true }, { commit, latest: newer, notDeployed: newer, notDeployedReason: "CI failed: test", via: "push", checkedAt: minutesAgo(1) }));
  expect(screen.getByText("b2c3d4e not deployed: CI failed (test)")).toBeTruthy();
  cleanup();
  show(projectWith({ ...repo, watchPaths: ["services/api/**"] }, { commit, latest: newer, notDeployed: newer, notDeployedReason: "No changes in the watch paths", via: "push", checkedAt: minutesAgo(1) }));
  expect(screen.getByText("b2c3d4e not deployed: no changes in the watch paths")).toBeTruthy();
  cleanup();
  show(projectWith({ ...repo, waitForChecks: true }, { commit, latest: newer, notDeployed: newer, notDeployedReason: "CI did not finish within an hour", via: "push", checkedAt: minutesAgo(1) }));
  expect(screen.getByText("b2c3d4e not deployed: CI did not finish within an hour")).toBeTruthy();
});

it("switches wait for CI through the api", async () => {
  const project = projectWith(repo, { commit, latest: commit, checkedAt: minutesAgo(1) });
  fake.updateProject.mockResolvedValue(projectWith({ ...repo, waitForChecks: true }, project.status.source));
  show(project);
  fireEvent.click(screen.getByRole("switch", { name: "Wait for CI" }));
  await waitFor(() => expect(fake.updateProject).toHaveBeenCalledWith(project.slug, { waitForChecks: true }));
  await waitFor(() => expect(toasts.success).toHaveBeenCalledWith("Each commit waits for its other checks"));
});

it("saves the watch paths, one pattern per line", async () => {
  const project = projectWith(repo, { commit, latest: commit, checkedAt: minutesAgo(1) });
  fake.updateProject.mockResolvedValue(projectWith({ ...repo, watchPaths: ["services/api/**", "!**/*.md"] }, project.status.source));
  show(project);
  const field = screen.getByLabelText("Watch paths") as HTMLTextAreaElement;
  expect(field.placeholder).toBe("services/api/**\n!**/*.md");
  fireEvent.change(field, { target: { value: " services/api/**\n\n!**/*.md " } });
  fireEvent.click(screen.getByRole("button", { name: "Save watch paths" }));
  await waitFor(() => expect(fake.updateProject).toHaveBeenCalledWith(project.slug, { watchPaths: ["services/api/**", "!**/*.md"] }));
});

it("shows a pattern the server refused under the field", async () => {
  const project = projectWith(repo, { commit, latest: commit, checkedAt: minutesAgo(1) });
  fake.updateProject.mockRejectedValue(new Error('in the watch path "a**", ** stands alone between slashes, as in services/**/api'));
  show(project);
  fireEvent.change(screen.getByLabelText("Watch paths"), { target: { value: "a**" } });
  fireEvent.click(screen.getByRole("button", { name: "Save watch paths" }));
  expect(await screen.findByText(/stands alone between slashes/)).toBeTruthy();
});

it("disables wait for CI and watch paths where the GitHub App does not reach", () => {
  const project = projectWith({ url: "https://gitlab.example.test/acme/shop", revision: "main" }, { commit, latest: commit, checkedAt: minutesAgo(1) });
  project.githubFeatures = false;
  show(project);
  expect((screen.getByRole("switch", { name: "Wait for CI" }) as HTMLButtonElement).disabled).toBe(true);
  expect((screen.getByLabelText("Watch paths") as HTMLTextAreaElement).disabled).toBe(true);
  expect(screen.getAllByText("Needs the GitHub App").length).toBe(2);
});

it("lets only those who may change the project change the two settings", () => {
  show(projectWith(repo, { commit, latest: commit, checkedAt: minutesAgo(1) }), { config: false });
  expect((screen.getByRole("switch", { name: "Wait for CI" }) as HTMLButtonElement).disabled).toBe(true);
  expect((screen.getByLabelText("Watch paths") as HTMLTextAreaElement).disabled).toBe(true);
  expect(screen.queryByRole("button", { name: "Save watch paths" })).toBeNull();
});
```

In `apps/workspace/src/api/api.test.ts`, after the auto-deploy test:

```ts
  it("keeps watch paths and wait for CI on a project on a branch, and refuses a pattern it cannot read", async () => {
    const hello = await api.updateProject("hello-world", { watchPaths: ["services/api/**", "!**/*.md"], waitForChecks: true });
    expect(hello.spec.source?.git?.watchPaths).toEqual(["services/api/**", "!**/*.md"]);
    expect(hello.spec.source?.git?.waitForChecks).toBe(true);
    expect(hello.githubFeatures).toBe(true);
    await expect(api.updateProject("hello-world", { watchPaths: ["a**"] })).rejects.toThrow(/stands alone/);
    await expect(api.updateProject("dash-tv", { waitForChecks: true })).rejects.toThrow(/git repository/);
  });
```

- [ ] **Step 2: Run them to see them fail**

Run: `npm run test --workspace apps/workspace -- source api.test`
Expected: FAIL: no "Wait for CI" switch, no "Watch paths" field; the Mock ignores `watchPaths`.

- [ ] **Step 3: Types, call and Mock**

`apps/workspace/src/api/types.ts`: the git source becomes

```ts
      git?: { url: string; revision?: string; autoDeploy?: boolean; watchPaths?: string[]; waitForChecks?: boolean };
```

`Project` gains, after `spec` and `status` (inside the same object type):

```ts
  // The GitHub App covers the repository: watch paths and wait for CI
  // apply (RFC-0054). sourceNote says when they are set and do not.
  githubFeatures?: boolean;
  sourceNote?: string;
```

`SourceStatus` gains, after `skipped`:

```ts
  // A commit waiting for its other checks on GitHub, since heldSince.
  held?: string;
  heldSince?: string;
  // The last commit not deployed, and why ("No changes in the watch
  // paths", "CI failed: test", "CI did not finish within an hour").
  notDeployed?: string;
  notDeployedReason?: string;
```

`apps/workspace/src/api/api.ts`, `updateProject`'s body gains:

```ts
      // The commits a push deploys: only those that change these globs,
      // and only once their other checks pass (the GitHub App's).
      watchPaths?: string[];
      waitForChecks?: boolean;
```

`apps/workspace/src/api/mock.ts`, in `updateProject`, after the `autoDeploy` block:

```ts
    if (body.watchPaths !== undefined || body.waitForChecks !== undefined) {
      const git = p.spec.source?.git;
      if (!git)
        throw new ApiError(
          400,
          "the project does not deploy from a git repository: watch paths and wait for CI follow a git branch",
        );
      if (body.watchPaths !== undefined) {
        const paths = body.watchPaths.map((l) => l.trim()).filter(Boolean);
        if (paths.length > 20) throw new ApiError(422, `a project takes at most 20 watch paths; there are ${paths.length}`);
        const bad = paths.find(
          (l) => l.length > 200 || l.replace(/^!/, "").split("/").some((part) => part.includes("**") && part !== "**"),
        );
        if (bad) throw new ApiError(422, `in the watch path "${bad}", ** stands alone between slashes, as in services/**/api`);
        git.watchPaths = paths.length ? paths : undefined;
      }
      if (body.waitForChecks !== undefined) git.waitForChecks = body.waitForChecks || undefined;
    }
```

`apps/workspace/mock/projects.json`, the `hello-world` entry gains `"githubFeatures": true,` beside `"access"`.

- [ ] **Step 4: The Source block**

In `apps/workspace/src/screens/project/source.tsx`:

Imports gain:

```tsx
import { Field } from "@shpyrd/ui/components/field";
import { Textarea } from "@shpyrd/ui/components/textarea";
```

After `const subject = …`:

```tsx
const needsApp = "Needs the GitHub App";

// "2 min", or "under a minute", since an ISO time.
const minutesSince = (iso: string) => {
  const m = Math.floor((Date.now() - Date.parse(iso)) / 60_000);
  return m < 1 ? "under a minute" : `${m} min`;
};

// Why a commit was not deployed, as the Source block says it.
function notDeployedWords(reason?: string) {
  if (!reason) return "no reason given";
  const failed = /^CI failed: (.+)$/.exec(reason);
  if (failed) return `CI failed (${failed[1]})`;
  return /^[A-Z][a-z]/.test(reason) ? reason.charAt(0).toLowerCase() + reason.slice(1) : reason;
}
```

In `GitSource`, after `const autoDeploy = …`:

```tsx
  const github = project.githubFeatures === true;
```

The Auto-deploy `InfoTableItem` is followed by:

```tsx
        <InfoTableItem label="Wait for CI">
          <WaitForCISwitch slug={project.slug} on={git.waitForChecks === true} allowed={perms.config} available={github} />
        </InfoTableItem>
```

After the "auto-deploy is off" paragraph:

```tsx
      {source?.held && source.held === source.latest && (
        <p className="text-xs text-muted-foreground">
          Waiting for CI on {short(source.held)}
          {source.heldSince ? ` (since ${minutesSince(source.heldSince)})` : ""}
        </p>
      )}
      {source?.notDeployed && source.notDeployed === source.latest && source.notDeployed !== source.commit && (
        <p className="text-xs text-muted-foreground">
          {short(source.notDeployed)} not deployed: {notDeployedWords(source.notDeployedReason)}
        </p>
      )}
```

Before `<SourceAccess …/>`:

```tsx
      <WatchPaths slug={project.slug} paths={git.watchPaths ?? []} allowed={perms.config} available={github} />
```

After `AutoDeploySwitch`:

```tsx
// Wait for CI (RFC-0054): a commit deploys once its other checks on
// GitHub pass. Only through the GitHub App.
function WaitForCISwitch({ slug, on, allowed, available }: { slug: string; on: boolean; allowed: boolean; available: boolean }) {
  const queries = useQueryClient();
  const set = useMutation({
    mutationFn: (waitForChecks: boolean) => api.updateProject(slug, { waitForChecks }),
    onSuccess: (p) => {
      queries.setQueryData(["project", slug], p);
      toast.success(p.spec.source?.git?.waitForChecks ? "Each commit waits for its other checks" : "Commits deploy without waiting for CI");
    },
    onError: (e: Error) => toast.error(e.message),
  });
  return (
    <span className="flex items-center gap-2">
      <Switch
        size="sm"
        checked={on}
        disabled={!allowed || !available || set.isPending}
        loading={set.isPending}
        onCheckedChange={(v) => set.mutate(v)}
        aria-label="Wait for CI"
      />
      {!available && <span className="text-xs text-muted-foreground">{needsApp}</span>}
    </span>
  );
}

// Watch paths (RFC-0054): globs from the repository root, one per line;
// a commit that changes none of them is not deployed.
function WatchPaths({ slug, paths, allowed, available }: { slug: string; paths: string[]; allowed: boolean; available: boolean }) {
  const saved = paths.join("\n");
  const [text, setText] = useState(saved);
  const [error, setError] = useState<string>();
  const queries = useQueryClient();
  const lines = text
    .split("\n")
    .map((l) => l.trim())
    .filter(Boolean);
  const save = useMutation({
    mutationFn: (watchPaths: string[]) => api.updateProject(slug, { watchPaths }),
    onSuccess: (p) => {
      setError(undefined);
      setText((p.spec.source?.git?.watchPaths ?? []).join("\n"));
      queries.setQueryData(["project", slug], p);
      toast.success(p.spec.source?.git?.watchPaths?.length ? "Watch paths saved" : "Every change deploys again");
    },
    onError: (e: Error) => setError(e.message),
  });
  const editable = allowed && available;
  return (
    <form
      className="grid gap-2"
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate(lines);
      }}
    >
      <Field
        label="Watch paths"
        hint={
          available
            ? "One pattern per line, from the repository root. A commit deploys only when it changes a matching file; ! excludes, and the last match wins."
            : needsApp
        }
        error={error}
      >
        <Textarea
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder={"services/api/**\n!**/*.md"}
          disabled={!editable || save.isPending}
          rows={3}
          className="font-mono text-xs"
        />
      </Field>
      {editable && lines.join("\n") !== saved && (
        <Button type="submit" size="sm" className="justify-self-start" disabled={save.isPending}>
          Save watch paths
        </Button>
      )}
    </form>
  );
}
```

`source.test.tsx`'s seed project is `hello-world`, which now has `githubFeatures: true`: the earlier tests keep their meaning; the one asserting `screen.queryByRole("switch")` is null for a commit revision still passes (that branch returns before the switches).

- [ ] **Step 5: Run the checks**

Run: `npm run test --workspace apps/workspace && npm run lint --workspace apps/workspace && npm run typecheck --workspace apps/workspace`
Expected: PASS, all three.

- [ ] **Step 6: Look at it**

Run the workspace app on the Mock (`npm run design --workspace apps/workspace`), open Hello World's overview, and check in light and dark: the Wait for CI switch beside Auto-deploy, the Watch paths field with its placeholder and hint, the Save button appearing only after a change; and, for a project without `githubFeatures` (any other fixture on a git source), both disabled with "Needs the GitHub App". Fix spacing with the library's classes only.

- [ ] **Step 7: Commit**

```bash
git add apps/workspace/src/api/types.ts apps/workspace/src/api/api.ts apps/workspace/src/api/mock.ts apps/workspace/mock/projects.json apps/workspace/src/api/api.test.ts apps/workspace/src/screens/project/source.tsx apps/workspace/src/screens/project/source.test.tsx
git commit -m "feat(workspace): wait for CI and watch paths in the Source block; held and not-deployed commits (RFC-0054)

Co-Authored-By: <your model name> <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 11: Docs, and RFC-0054 implemented

**Files:**
- Modify: `content/docs/deploying.mdx` (after the last paragraph of "Connect GitHub", "On shpyrd cloud the App is already set up…", ~210, before the "Which languages?" callout)
- Modify: `content/docs/installation.mdx:199` (the permissions sentence)
- Modify: `content/docs/cli.mdx` (the `shpyrd deploy` row ~194, the `shpyrd projects info` row ~187)
- Modify: `rfcs/0054-github-app.md` (header, Implementation History), `rfcs/README.md:98`

**Interfaces:**
- Consumes: the behaviour of Tasks 1 to 10 as built. Commands, messages, notes and labels are copied from the code, not paraphrased.

- [ ] **Step 1: The deploying page**

After the paragraph "On shpyrd cloud the App is already set up; on a self-hosted platform an operator registers it once: see [Installation](/docs/installation#connect-github)." add:

````mdx
### Checks and Deployments on GitHub

For a repository the GitHub App covers, GitHub shows what shpyrd does with each commit. A check named `shpyrd / <project>` appears on the commit: **Building**, then **Deploying**, then **Running at** the app's address, or **Failed** with the last lines of the reason. Its **Details** link opens the project in the dashboard. A commit that waits for CI shows **Waiting for CI**; one shpyrd decided not to deploy, or that asked to be skipped, shows a neutral check that says why. When a newer commit is pinned before the previous one finished, the previous check says **Replaced by** the newer commit.

Each release that changes the code is also a GitHub **Deployment**, in an environment named after the project, with a link to the app once it runs. A rollback to an older commit is a Deployment too; a release that only changes config vars or sizes is not. Two projects on one repository each get their own check and their own environment.

The check and the Deployment never hold a deploy back: when GitHub is down or refuses a write, shpyrd deploys as usual and sends what it owes later. A suspended or removed installation reports nothing.

### Wait for CI

With **Wait for CI** on, a pushed commit deploys only once the other checks and commit statuses on it pass, every one except shpyrd's own. Switch it on under the project's overview, in the Source block, or:

```shell
shpyrd deploy --git https://github.com/acme/shop --wait-for-ci      # --no-wait-for-ci switches it off
```

The Source block says "Waiting for CI on a1b2c3d (since 2 min)" meanwhile. Then:

- **They pass:** the commit deploys; its release says `Push a1b2c3d by alice: fix login (CI passed)`.
- **One fails:** the commit is not deployed, and the project says which check failed ("a1b2c3d not deployed: CI failed (test)"). Re-run it on GitHub: if it passes and the commit is still the branch's newest, it deploys.
- **None reports within a minute:** the repository has no CI on that commit, and it deploys (`… (no CI)`). A CI that reports later than a minute after the push may lose that race.
- **Still pending an hour later:** the commit is not deployed ("CI did not finish within an hour").
- **A newer commit arrives:** it replaces the waiting one at once.

shpyrd learns of results through the App's webhook, and checks a waiting commit every 30 seconds in case a delivery is missed. While GitHub does not answer, the commit keeps waiting. A deploy by a person (`shpyrd deploy`, Redeploy, a rollback) and the first commit of a new project do not wait.

### Watch paths

**Watch paths** deploy only the commits that change certain files: in a repository that holds several services, each project deploys its own. They are globs from the repository root, one per line:

```text
services/api/**
!**/*.md
```

`*` matches within one folder, `**` across any number of folders, and a pattern ending in `/` matches everything below it. A leading `!` excludes. Patterns run in order and the last one that matches a file decides it: the two above deploy any change under `services/api` except Markdown. A project takes up to 20 patterns of up to 200 characters; a pattern shpyrd cannot read is refused when it is set.

Set them under the project's overview, in the Source block, or:

```shell
shpyrd deploy --git https://github.com/acme/monorepo --watch-path 'services/api/**' --watch-path '!**/*.md'
shpyrd deploy --git https://github.com/acme/monorepo --no-watch-paths     # every commit deploys again
```

A commit that changes nothing in them is recorded, not deployed ("a1b2c3d not deployed: no changes in the watch paths"). The files counted are everything changed since the commit that runs, so a commit that was not deployed still counts once a later one matches, and a renamed file counts under its old name and its new one. When GitHub's list of changes cannot be trusted (GitHub's compare failed, more than 300 files changed, or the branch's history was rewritten), the commit deploys, and its release ends with "(watch paths not checked: …)".

Watch paths and wait for CI work through the GitHub App. On a repository it does not cover they are kept but do nothing: the dashboard says "Needs the GitHub App", and `shpyrd deploy` says so when it sets them.
````

- [ ] **Step 2: The installation page**

In `content/docs/installation.mdx:199`, replace "Checks, deployments and statuses are for what comes next, asked now so installations do not have to approve again." with:

```mdx
Checks and deployments let shpyrd show each commit's build and release on GitHub, and statuses let it wait for a commit's CI ([Checks and Deployments on GitHub](/docs/deploying#checks-and-deployments-on-github)).
```

- [ ] **Step 3: The CLI page**

In `content/docs/cli.mdx`, the `shpyrd deploy` row: after "(with neither, the project's setting stays);" insert:

```mdx
`--watch-path <glob>` (repeatable) deploys only commits that change a matching file and `--no-watch-paths` removes them; `--wait-for-ci` deploys a commit once its other checks on GitHub pass and `--no-wait-for-ci` stops waiting (both GitHub App only: [Wait for CI](/docs/deploying#wait-for-ci), [Watch paths](/docs/deploying#watch-paths); with neither, the project's settings stay);
```

The `shpyrd projects info <slug>` row: after "source," insert "the commit waiting for CI or not deployed and why,".

- [ ] **Step 4: RFC-0054 implemented**

`rfcs/0054-github-app.md` header: `**Status:** implemented` and `**Last update:** 2026-10-16` (unchanged date if already so). Under `## Implementation History`, after the part 3 lines:

```markdown
- 2026-10-16: part 4, checks and Deployments: a `shpyrd / <project>` check run on each commit (building, deploying, running at the URL or failed; waiting for CI; not deployed; skipped; replaced) and a GitHub Deployment per release that changes the code, written by a reporter loop that keeps what it sent in an annotation and never touches a deploy; wait for CI (every check run and status except shpyrd's own; no CI after a minute; given up after an hour; re-runs deploy) through `check_suite` and `status` webhooks with a 30-second fallback; watch paths (gitignore-style globs, last match wins, GitHub's compare from the running commit); per-scope installation tokens. See docs/superpowers/specs/2026-10-16-github-checks-design.md.
```

`rfcs/README.md:98`: the status column `in progress` becomes `implemented`.

- [ ] **Step 5: Check the MDX**

`content/` has no build of its own; the website renders it. Check by eye: every fence closed (the new sections use a four-backtick outer fence), the anchors `#checks-and-deployments-on-github`, `#wait-for-ci` and `#watch-paths` match the new headings, the edited table rows keep their column count (a `|` inside a cell is `\|`), and the words match the code: `Waiting for CI on`, `not deployed: CI failed (`, `no changes in the watch paths`, `(CI passed)`, `(no CI)`, `(watch paths not checked: `, `Needs the GitHub App`.

- [ ] **Step 6: Commit**

```bash
git add content/docs/deploying.mdx content/docs/installation.mdx content/docs/cli.mdx rfcs/0054-github-app.md rfcs/README.md
git commit -m "docs: checks and Deployments on GitHub, wait for CI, watch paths; RFC-0054 implemented

Co-Authored-By: <your model name> <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

---

### Task 12: The real run on kind, screenshots, the PR

**Done after the final review of the whole branch, not before.** Tasks 1 to
11 are reviewed one by one and then together; this task runs the result
against the real test GitHub App and opens the PR. A fault found here is
fixed in the task that owns the code, with a test, and reviewed again.

**It needs the maintainer, who clicks everything on GitHub** (creating,
installing, re-running, editing a workflow from the web is theirs; the
agent may push commits to the test repository from the terminal only if the
maintainer says so). What exists from PR 3's run: the test App registered
under the maintainer's personal account, installed on
`paezao/shpyrd-e2e-hello`, connected to the default workspace of the kind
cluster; ngrok configured in `~/.config/ngrok/ngrok.yml`. Ask the maintainer
to confirm these before starting, and wait.

**Files:**
- Modify: `rfcs/0054-github-app.md` (Implementation History)

- [ ] **Step 1: Full test run**

Run: `GOTMPDIR=/home/nkr/.cache/gotmp go build ./... && GOTMPDIR=/home/nkr/.cache/gotmp go vet ./... && GOTMPDIR=/home/nkr/.cache/gotmp go test ./... && npm run test --workspace apps/workspace && npm run lint --workspace apps/workspace && npm run typecheck --workspace apps/workspace`
Expected: all pass.

- [ ] **Step 2: The cluster and the build**

Kind on 80/443 (the dashboard `https://127.0.0.1.nip.io`, the console `https://shpyrd.127.0.0.1.nip.io`; 8443 breaks the local sign-in). `make ui`, then `GOTMPDIR=/home/nkr/.cache/gotmp make dev-deploy` to build and load the server; never `shpyrd extensions enable` after it (it reverts to the published image). Check the image with `kubectl -n shpyrd-system get deploy shpyrd-server -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="SHPYRD_VERSION")].value}'`. Apply the new CRD: `kubectl apply -f deploy/components/shpyrd/base/crds/shpyrd.io_apps.yaml`.

- [ ] **Step 3: The webhook tunnel, the webhook path only**

GitHub must reach `POST /api/github/webhook` and nothing else. In the scratchpad, write an ngrok traffic policy:

```yaml
on_http_request:
  - expressions:
      - "!(req.method == 'POST' && req.url.path == '/api/github/webhook')"
    actions:
      - type: custom-response
        config:
          status_code: 404
          body: not found
```

and start the tunnel to the ingress, keeping the workspace's Host (if ngrok refuses the cluster's self-signed certificate, point it at `http://127.0.0.1:80` instead):

```shell
ngrok http https://127.0.0.1:443 --host-header=127.0.0.1.nip.io --traffic-policy-file <scratchpad>/webhook-only.yml
```

Check from outside: `curl -s -o /dev/null -w '%{http_code}\n' https://<tunnel>/api/projects` prints `404`, and a `POST` to `https://<tunnel>/api/github/webhook` without a signature prints `401`. Point the App's webhook at the tunnel: GitHub's App settings, "Webhook URL", `https://<tunnel>/api/github/webhook` (the maintainer saves it), or `PATCH /app/hook/config` with the App's JWT (`{"url": "https://<tunnel>/api/github/webhook"}`). The App's events must include `check_suite` and `status` besides `push`: in the App's settings, "Permissions & events", "Subscribe to events" (the maintainer ticks them if PR 3's registration lacked them; the permissions `checks: write`, `deployments: write`, `statuses: read` were asked at registration).

- [ ] **Step 4: Run 1, a push shows the check and the Deployment**

The project `hello` deploys `https://github.com/paezao/shpyrd-e2e-hello` from `main` through the App (PR 3's run). Ask the maintainer to push a commit "change the greeting" (or, with their leave, push it from a clone in the scratchpad). Expect:

1. On GitHub, the commit's checks: `shpyrd / hello` going **Building**, **Deploying**, then **Running at https://hello.127.0.0.1.nip.io** (GitHub cannot open that address; the text is what counts). Its Details link is `https://127.0.0.1.nip.io/projects/hello`.
2. The repository's Environments: `hello`, with a Deployment of the commit, active.
3. `kubectl -n app-hello get app hello -o jsonpath='{.metadata.annotations.shpyrd\.io/github-report}'` shows `commit`, `checkRun`, `"check":"completed/success"`, `release`, `deployment`, `"deploymentState":"success"`.
4. The server's log has no token (`kubectl -n shpyrd-system logs deploy/shpyrd-server | grep -c 'ghs_'` prints `0`).

- [ ] **Step 5: Run 2, wait for CI with a GitHub Actions workflow**

The maintainer sets a repository variable `CI_PASS` to `yes` (Settings, Secrets and variables, Actions, Variables) and adds `.github/workflows/ci.yml` to the test repository:

```yaml
name: ci
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: sleep 45 && test "${{ vars.CI_PASS }}" = yes
```

Switch wait for CI on in the dashboard (Source block, **Wait for CI**), or `shpyrd deploy --git https://github.com/paezao/shpyrd-e2e-hello --ref main --wait-for-ci --project hello --no-wait`. Then:

1. **A passing push.** The Source block says "Waiting for CI on <sha> (since …)", and `shpyrd projects info hello` shows `Commit:     <sha> waits for CI (since …)`. On GitHub the commit shows `shpyrd / hello` **Waiting for CI** beside `test` running. When `test` passes, within seconds (the `check_suite` webhook) a release `Push <sha> by <user>: … (CI passed)` goes out and the check moves on to **Running at …**.
2. **A failing push.** The maintainer sets `CI_PASS` to `no` and pushes a commit. Expect "<sha> not deployed: CI failed (test)" in the Source block, a neutral `shpyrd / hello` check titled "CI failed: test", and an Activity entry `git.skip` with that reason; what runs keeps running.
3. **A green re-run.** The maintainer sets `CI_PASS` back to `yes` and clicks "Re-run all jobs" on the failed run. Expect the same commit to deploy once the re-run passes, with `(CI passed)`, and a new `shpyrd / hello` run on it going to **Running at …** (deviation 4).

- [ ] **Step 6: Run 3, watch paths**

Switch wait for CI off, and set the watch paths to `src/**` in the Source block (or `shpyrd deploy --git https://github.com/paezao/shpyrd-e2e-hello --ref main --watch-path 'src/**' --project hello --no-wait`). The maintainer pushes a README-only change: expect "<sha> not deployed: no changes in the watch paths" in the Source block and a neutral check with the title "No changes in the watch paths". Then a change under `src/`: it deploys, with the release note of that commit, and the server's log shows one compare from the running commit (`kubectl -n shpyrd-system logs deploy/shpyrd-server | grep -c 'could not compare'` prints `0`). Remove the watch paths afterwards (`--no-watch-paths`).

- [ ] **Step 7: A GitHub outage during a deploy**

The maintainer pushes a commit (wait for CI off, no watch paths). As soon as its check shows **Building**, make GitHub unreachable for the server: `kubectl -n shpyrd-system set env deploy/shpyrd-server SHPYRD_GITHUB_API_URL=https://127.0.0.2:9` (the server restarts; the build runs on in the cluster). Expect the release to go out and the app to answer with the new greeting, and the server's log to say "could not report to GitHub; the deploy goes on, the report is sent later" at most once a minute for `hello`. Then `kubectl -n shpyrd-system set env deploy/shpyrd-server SHPYRD_GITHUB_API_URL-`: within a minute of the restart the check goes to **Running at …** and the Deployment to success, sent from the annotation's difference.

- [ ] **Step 8: Screenshots**

With the browser tools, into the scratchpad (never the repository):
- GitHub: the commit page with `shpyrd / hello` **Running at …**; the check's page with its summary; the commit with **Waiting for CI** beside the running `test`; the neutral "CI failed: test" check; the README commit's "No changes in the watch paths"; the repository's `hello` environment with its Deployments.
- shpyrd: the Source block "Waiting for CI on …"; "… not deployed: CI failed (test)"; "… not deployed: no changes in the watch paths"; the Wait for CI switch and Watch paths field filled; the same for a project not on GitHub, disabled with "Needs the GitHub App" (from the Mock if easier). Light theme, and one in dark.
- As text: `shpyrd projects info hello` while held; `shpyrd deploy … --watch-path … --wait-for-ci` on a GitLab URL printing the note.

- [ ] **Step 9: RFC-0054's history**

Under `## Implementation History`:

```markdown
- 2026-10-16: part 4 verified end to end on a kind cluster against the test GitHub App and paezao/shpyrd-e2e-hello, webhooks through an ngrok tunnel limited to POST /api/github/webhook: the check and the Deployment of a push, wait for CI with a GitHub Actions workflow (pass, fail, green re-run), watch paths (a README push ignored, the next matching push deployed), and a GitHub outage leaving the deploy untouched.
```

```bash
git add rfcs/0054-github-app.md
git commit -m "docs(rfc): RFC-0054 part 4 verified end to end on a kind cluster

Co-Authored-By: <your model name> <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb"
```

- [ ] **Step 10: Push and open the PR**

```bash
git push -u origin feat/github-checks
```

`gh pr create --base feat/github-app-connect` (stacked on PR 3, #227; after #227 merges, retarget to `main`):
- Title: `Private repositories, part 4: checks, Deployments, watch paths, wait for CI (RFC-0054)`.
- Body: what it does; the deviations at the top of this plan; the new App events an existing App must subscribe to (`check_suite`, `status`); the CRD change (`watchPaths`, `waitForChecks`, the new `status.source` fields) applied by the upgrade; how it was tested and how GitHub reached the cluster (the tunnel's policy); the screenshots; the CLI transcripts in code blocks.
- End the body with:
  ```
  🤖 Generated with [Claude Code](https://claude.com/claude-code)

  https://claude.ai/code/session_01YUVpLWStftw7mUF6R2mseb
  ```
- `gh` cannot attach images: open the PR first, then give the maintainer the image paths to drop in, or upload them through the browser tools on the PR page.
