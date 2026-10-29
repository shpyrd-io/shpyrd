# Private repositories, PR 4: checks, Deployments, watch paths, wait for CI (RFC-0054 part 4)

Date: 2026-10-16
Status: draft, awaiting review
Builds on: `docs/superpowers/specs/2026-10-16-private-git-repositories-design.md`
(the master spec), `docs/superpowers/specs/2026-10-16-github-app-connect-design.md`
(PR 3, #227). This branch, `feat/github-checks`, is stacked on
`feat/github-app-connect`.

Where this file and the master spec differ, this file wins.

## Done when

- A push to a GitHub repository that the App covers shows a
  `shpyrd / <project>` check on the commit. The check goes building, then
  deploying, then success with the app's URL or failure with the reason. The
  release shows as a GitHub Deployment, with environment `<project>` and a link
  to the app.
- With **wait for CI** on, a commit deploys only after the commit's other
  checks and statuses pass. A red commit is not deployed and says why. A
  re-run that turns green deploys it. A commit still pending after an hour is
  given up.
- With **watch paths** set, a commit that changes nothing in them is recorded
  and not deployed, and says why. The next commit that does match deploys
  everything changed since the running commit.
- A GitHub outage or a refused write never fails or delays a deploy.
- The PR carries screenshots from both sides: GitHub's commit page and
  Deployments, and shpyrd's Source block and settings.

Out of scope: PR previews and PR comments (the next spec), commit statuses
written by shpyrd, the "Re-run" button on shpyrd's own check, other hosts.

## Decisions the maintainer made

- **Wait for CI waits for every check run and commit status on the commit
  except shpyrd's own.** This is what Render's "After CI checks pass" does.
- **A held commit whose CI never finishes is given up after one hour**, not
  deployed, with the reason. A newer commit replaces a held one at once.
- **Watch paths are gitignore-style globs in one list**: `**` for any depth,
  `!` to exclude, the last match wins. This is what Railway does.
- **The reporter is its own loop** (approach A), not calls inside the
  AppReconciler and not an outbox table.

## 1. What changes on the App

`GitSource` gains two fields:

```go
type GitSource struct {
    // ... URL, Revision, AutoDeploy (unchanged)
    // WatchPaths are globs, from the repository root; a commit deploys
    // only when a changed file matches. GitHub App only.
    WatchPaths []string `json:"watchPaths,omitempty"`
    // WaitForChecks holds a commit until its other checks pass. GitHub
    // App only.
    WaitForChecks bool `json:"waitForChecks,omitempty"`
}
```

Both are ignored unless the GitHub App covers the repository, which is the
same test the credential lookup makes. The API, CLI and dashboard say so when
either is set on another host.

`gitauth.SourceRecord`, the `shpyrd.io/git-source` annotation, and
`status.source` gain:

- `Held` and `HeldSince`: a commit waiting for CI.
- `NotDeployed` and `NotDeployedReason`: the last commit the watcher decided
  not to deploy, and why. `Skipped` from PR 2 stays for `[skip deploy]`.
  `NotDeployed` covers watch paths, a CI failure and a CI timeout.

## 2. The watcher's decisions

The order in `decideSource` becomes:
1. branch;
2. auto-deploy;
3. `[skip deploy]`;
4. **watch paths**;
5. **wait for CI**;
6. pin.

A person's deploy skips steps 4 and 5 and always deploys, and so does the
first commit of an app with nothing pinned. Both steps need GitHub calls.
`decideSource` stays pure: the reconciler gathers the answers first (the
changed files, the CI verdict) and passes them in `sourceEvent`.

### Watch paths

- **Which files count:** GitHub's compare of the pinned commit against the new
  one (`GET /repos/{owner}/{repo}/compare/{pinned}...{sha}`). That is
  everything changed since what runs, not only the last push, so a commit
  that was ignored earlier still counts once a later one matches. A renamed
  file counts under its old name and its new one.
- **Matching** (`pkg/gitauth/watchpaths.go`, no new dependency):
  - Patterns run in order, and the last one that matches decides.
  - A leading `!` excludes. A leading `/` is optional, and paths are from
    the repository root.
  - `*` matches within one path segment, `**` matches any number of
    segments, and a pattern ending in `/` matches everything below it.
  - An invalid pattern is refused when it is set (API `422`), never at
    match time.
- **The list of changes can't be trusted, so the commit deploys** when the
  compare fails, when it returns 300 files (GitHub's cap), or when it says
  `diverged`. The release note then ends "(watch paths not checked: <reason>)".
- **No match:** not deployed. `NotDeployed` is set with the reason "No changes
  in the watch paths", the audit entry is `git.skip` with that reason, and the
  check is neutral.

### Wait for CI

- **Holding:** `Held` and `HeldSince` are set and nothing is pinned. The
  `HeldSince` time is when the commit was first seen, so a push's own time
  counts.
- **The verdict** (`pkg/github/ci.go`, a pure function over what GitHub
  returns):
  - It reads `GET /repos/{o}/{r}/commits/{sha}/check-runs`, leaving out runs
    whose `app.id` is our App's, and `GET /repos/{o}/{r}/commits/{sha}/status`.
  - **Failed:** any run concluded `failure`, `cancelled`, `timed_out`,
    `action_required` or `startup_failure`, or any status is `failure` or
    `error`. The reason names the first failing check.
  - **Passed:** at least one run or status exists, every run is `completed`
    with `success`, `neutral` or `skipped`, and every status is `success`.
  - **No CI:** no runs and no statuses 60 seconds or more after `HeldSince`.
    It deploys as if no CI is set up.
  - **Pending:** anything else. Check suites are not read at all, so the
    empty suite every installed App gets never holds a commit.
- **Evaluated again:**
  - on a `check_suite` (action `completed`) or `status` webhook for the
    commit. The webhook nudges, through the `shpyrd.io/git-ci` annotation,
    the apps in the installation's workspace whose repository matches and
    whose held or CI-failed commit is that SHA;
  - while a commit is held, at every requeue of at most 30 seconds. That is
    the fallback for missed webhooks, and it costs two GitHub reads.
- **Endings:**
  - **Passed or no CI:** the commit is pinned and deploys, with the note
    "Push a1b2c3d by alice: fix login (CI passed)".
  - **Failed:** not deployed. `NotDeployed` is set with "CI failed: <check>",
    and the audit entry is `git.skip` with the reason.
  - **Pending for an hour after `HeldSince`:** not deployed, with "CI did not
    finish within an hour".
  - **A newer commit on the branch** replaces the held one at once.
- **Re-run:** a commit that failed CI and is still the branch's head is
  evaluated again when its checks change (`check_suite` or `status`), and
  deploys if it now passes.

### Tokens

`InstallationToken` takes the permissions it asks for. Each scope has its own
cache key, so a build's clone token is never widened:

| Use | Permissions |
|---|---|
| Clone, ls-remote (PR 3, unchanged) | `contents: read` |
| Watcher: compare, check runs, statuses | `contents: read`, `checks: read`, `statuses: read` |
| Reporter: check runs, Deployments | `checks: write`, `deployments: write` |

Each token is still scoped to the one repository.

## 3. The reporter

A new reconciler, `internal/controller/github_report.go`, runs in the
controller manager beside the source reconciler. It watches git Apps that the
App covers. It writes nothing to App status, so the AppReconciler stays the
only writer of status. It keeps what it sent in one annotation,
`shpyrd.io/github-report`:

```json
{"commit":"<sha>","checkRun":123,"check":"in_progress/deploying",
 "release":7,"deployment":456,"deploymentState":"in_progress"}
```

It writes to GitHub only when the state it computes differs from the
annotation. So restarts and repeated reconciles never post twice, and a lost
write is sent again on the next reconcile.

### Check runs

There is one check run named `shpyrd / <project>` per commit. Its
`details_url` is the project's page on the workspace's dashboard, and its
`external_id` is the App's UID.

| What shpyrd knows | Check run |
|---|---|
| The pinned commit, phase Pending or Building | `in_progress`, "Building" |
| Phase Deploying | `in_progress`, "Deploying" |
| Running, and the latest release's source is the commit | `success`, "Running at <URL>" |
| Phase Failed for the commit | `failure`, with the App's message |
| `Held` | `queued`, "Waiting for CI" |
| `NotDeployed` | `neutral`, with the reason |
| `Skipped` | `neutral`, "Skipped: [skip deploy]" |
| Pinned moves on while the old check isn't finished | the old one is `neutral`, "Replaced by <short sha>" |

The summary carries the commit's release note and, on failure, the last lines
of the App's message. It never carries logs, tokens or config.

### Deployments

- There is one Deployment per release whose source is a commit different from
  the previous release's.
- It is created with `ref` set to the commit, `environment` set to
  `<project>`, `auto_merge` false, `required_contexts` empty (shpyrd decides
  when to deploy, GitHub doesn't), and `transient_environment` false.
- Its statuses are `in_progress` while deploying, then `success` with
  `environment_url` set to the app's URL, or `failure`. GitHub marks the
  previous one inactive by itself.
- A rollback to an older commit is a release with a different source, so it
  gets a Deployment.
- A release that only changes config or sizes gets none.

### Failures

- A refused or failed write is logged, at most once a minute per App. It is
  retried with backoff through `RequeueAfter`, from 30 seconds up to 10
  minutes, and the annotation is left unchanged.
- A 404 on a stored check run or Deployment, for example when it was deleted
  on GitHub, starts a new one.
- A suspended or removed installation stops the reporting. Nothing is
  queued.

## 4. API, CLI, dashboard

- **API:** `PATCH /api/projects/:slug` accepts `watchPaths` (an array, at most
  20 patterns of up to 200 characters each, each checked) and
  `waitForChecks`. The project's view returns them, plus `githubFeatures`
  (true when the App covers the repository), so the dashboard can say when
  they don't apply.
- **CLI:**
  - `shpyrd deploy` gains `--watch-path <glob>` (repeatable),
    `--no-watch-paths`, `--wait-for-ci` and `--no-wait-for-ci`. They sit
    beside `--auto-deploy` and `--no-auto-deploy`, and set the source on
    an existing project.
  - `shpyrd status` shows the held or not-deployed commit and its reason.
- **Dashboard** (following `apps/AGENTS.md` and `design/DESIGN_FOR_AGENT.md`):
  - **The Source block:** "Waiting for CI on a1b2c3d (since 2 min)",
    "a1b2c3d not deployed: CI failed (test)" and "a1b2c3d not deployed: no
    changes in the watch paths".
  - **Settings:** next to auto-deploy are a "Wait for CI" switch and a
    "Watch paths" field, one pattern per line, with `services/api/**` and
    `!**/*.md` as placeholders. Both are disabled, with "Needs the GitHub
    App", when the App doesn't cover the repository.
- **Docs:** deploying.mdx gains "Checks and Deployments on GitHub", "Wait for
  CI" and "Watch paths", and cli.mdx gains the flags.
- **RFC-0054** is marked implemented.

## 5. Testing

- **Unit:**
  - the watch-path matcher (`**`, `*`, `!`, order, a leading `/`, a trailing
    `/`, renames, invalid patterns);
  - the CI verdict (each conclusion, statuses, our own run left out, empty
    suites, the 60-second "no CI" rule, the hour);
  - the reporter's mapping from App state to check and Deployment, including
    replaced commits and releases with no code change.
- **The fake GitHub** (`pkg/github/githubtest`) gains:
  - compare;
  - listing check runs and statuses;
  - creating and updating check runs;
  - Deployments and their statuses.

  It records each write with the token's permissions, so tests assert one
  check run per commit across a restart, and that no write uses the clone
  token.
- **Controller:**
  - held, then passed, then pinned;
  - failed, then re-run green, then pinned;
  - the timeout;
  - a newer push replacing a held commit;
  - watch paths ignoring a commit and then counting it in the next compare;
  - a person's deploy ignoring both settings;
  - the reporter during a GitHub outage, with the deploy unaffected and the
    write sent later.
- **Webhook:** `check_suite` and `status` nudge only matching apps in the
  installation's own workspace; a bad signature still gets 401.
- **The real run**, on kind with the test App and `paezao/shpyrd-e2e-hello`,
  with webhooks through the ngrok tunnel limited to the webhook path:
  1. A push shows the check and the Deployment on GitHub.
  2. A small GitHub Actions workflow in the test repository: with wait for CI
     on, a passing run deploys; a failing run doesn't, and says why; a green
     re-run deploys.
  3. A watch path set: a README-only push is ignored, and the next matching
     push deploys.

  Screenshots of each, from GitHub and from shpyrd.

## Risks

- **GitHub's rate limit.** A held commit costs two reads every 30 seconds for
  up to an hour. That is about 240 calls an hour per held app, well inside an
  installation's 5,000 an hour. The backoff from PR 3 applies.
- **A slow first check.** Some CI providers report later than 60 seconds
  after the push, so a commit could deploy as "no CI". Any status or check
  run that appears in that window holds it. A repository whose CI reports
  late can still lose the race.
- **Two projects on one repository** each get their own check and their own
  Deployment environment. That is intended.
