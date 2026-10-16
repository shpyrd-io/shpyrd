# Deploying from private git repositories

Date: 2026-10-16
Status: draft, awaiting review
RFCs: RFC-0017 (git credentials), RFC-0018 (repository monitoring and
auto-deploy), RFC-0054 (GitHub App integration)
Scope: `api/v1alpha1`, `internal/controller`, `internal/cli`, `pkg/api`,
`pkg/store`, `apps/workspace`, `content/docs`, `rfcs/`

## Intent

Today `shpyrd deploy --git` works only with public repositories, Dockerfile
builds never redeploy on a new commit, and nothing reacts to a push. Teams
keep their code in private repositories, mostly on GitHub, so this is the
first wall a real team meets.

After this work a workspace connects GitHub once, any project in it deploys
from a private repository, every push to the project's branch deploys, and
GitHub shows what shpyrd did with each commit. Repositories elsewhere (GitLab,
Gitea, Bitbucket, a company's own server) deploy with a deploy key or a token.

It runs the same way on shpyrd cloud and on a customer's own cluster: one code
path, with the GitHub App's credentials supplied by the cloud layer or
registered by the cluster's admin.

Done when:

- a private GitHub repository deploys from the dashboard ("From GitHub", a
  repository and branch picker) and from `shpyrd deploy --git`, with no
  personal token anywhere;
- a push to the branch deploys within seconds, for buildpack and Dockerfile
  builds alike, and GitHub shows a `shpyrd / <project>` check and a Deployment
  with the app's URL;
- a private repository on another host deploys with a generated deploy key or
  a stored token, and redeploys on push through a generic webhook or polling;
- the person who connected GitHub leaving the workspace breaks nothing;
- each of the four PRs carries screenshots of the feature in use.

Out of scope: pull request previews and PR comments (the next spec: they bring
a project per PR, databases, cost and fork approval with them); GitLab or
Bitbucket apps; "build, then promote by hand".

## What other platforms do

Researched 2026-10-16 (Render, Vercel, Supabase, Railway, Netlify, Heroku,
Fly.io, Coolify, Dokku):

- A GitHub App is the default nearly everywhere. The person picks the
  repositories on GitHub's install screen, the App's webhook delivers pushes,
  and the platform mints installation tokens that expire in an hour; nobody
  stores user tokens (Heroku's stolen OAuth tokens in 2022 are why). Results go
  back as Deployments, statuses or checks.
- Render, the closest competitor, ties the credential to a person: a service
  deploys with its creator's credential, and when that person leaves, deploys
  break until someone clicks "Use My credentials". It has nothing for private
  repositories outside GitHub, GitLab and Bitbucket.
- Coolify, the closest self-hosted analogue, registers its App through
  GitHub's manifest flow and offers generated deploy keys plus a hand-added
  webhook for every other host.
- Usual extras: wait for CI before deploying, `[skip ...]` in a commit
  message, watch paths for monorepos.

We copy the common pattern, tie the connection to the workspace rather than a
person, and cover every other host with deploy keys and tokens.

## Design

### 1. What is stored, and where

**Connections belong to the workspace**, in the SQL store (`pkg/store`, both
the postgres and memory backends):

- `github_installations`: installation id, GitHub account login and type
  (organisation or user), workspace, who connected it and when (for the audit
  log only), and a removed or suspended state. A workspace can have several.
- `git_credentials`: for any other host.
  - Token: one per host per workspace (`gitlab.example.com`).
  - Deploy key: one per project, because a deploy key belongs to one
    repository. shpyrd generates an ed25519 pair and shows only the public
    half.

  The store keeps names, hosts, kinds and dates. The values live in Secrets in
  `shpyrd-system` and are write-only, following the pattern of config vars and
  cost drains.

**The GitHub App's own credentials** are a Secret `shpyrd-github-app` in
`shpyrd-system`: app id, slug, private key, webhook secret, client id and
client secret. A self-hosted cluster fills it with `shpyrd github app create`.
On shpyrd cloud the cloud layer supplies the same Secret. The code does not
care which.

**The App CRD**, `GitSource` gains:

```go
type GitSource struct {
    URL           string
    Revision      string   // branch, tag or SHA (unchanged)
    AutoDeploy    *bool    // default true when Revision is a branch
    WatchPaths    []string // optional; needs the GitHub App
    WaitForChecks bool     // GitHub App only
}
```

`status.source` records `Commit` (the SHA being built), `CommitMessage`,
`Author` and `Via` (`push`, `poll` or `deploy`).

The CRD never names a credential. The controller finds one from the workspace
and the URL's host, in this order: a GitHub App installation that covers the
repository, then the project's deploy key, then the workspace's token for the
host. If none gives access, the build does not start and the reason says how
to fix it.

### 2. Noticing commits and building them

A **source watcher**, a new loop beside the controllers (they already hold
the store, see `internal/controller/membership_controller.go`), decides which
commit each git app runs.

Signals:

- GitHub App webhook, `POST /api/github/webhook`, one URL per App. It checks
  `X-Hub-Signature-256` and finds the workspace from the installation id, not
  from the Host header, so the shared cloud App works. Events: `push`,
  `check_suite`, `status`, `installation`, `installation_repositories`.
- Generic webhook, `POST /api/hooks/git/<project>/<token>` (RFC-0018): GitHub's
  HMAC for repositories without the App, GitLab's token header, Gitea's HMAC.
  `shpyrd git-hooks show` prints the URL and secret. The token compare is
  constant-time and the route is rate limited (`newRateLimiter`).
- Polling every `SHPYRD_GIT_POLL_INTERVAL` (default 2m), for apps on a branch
  that have had no webhook in 24 hours: the GitHub API when the App covers the
  repository, `git ls-remote` with the stored credential otherwise.
- A deploy by a person (`shpyrd deploy --git`, the dashboard) resolves the
  commit at once.

Decisions, in order:

1. A push to another branch is ignored; so is any push when `autoDeploy` is
   false. A deploy by a person always goes ahead.
2. `[skip deploy]` in the commit message: recorded, not deployed.
3. Watch paths: the changed files come from GitHub's compare API; no match,
   no deploy. On other hosts `watchPaths` is ignored, and the dashboard and
   CLI say so when it is set.
4. Wait for CI: the commit is held as pending until every check and status on
   it, except shpyrd's own, has passed. It is evaluated again on each
   `check_suite` or `status` event. A failure means no deploy, with the reason.

The watcher then pins the commit: it writes the SHA, a release note ("Push
a1b2c3d by alice: fix login") and `deployed-by: github:alice via push` through
the annotations a deploy uses today (`AnnotationReleaseNote`,
`AnnotationDeployRequested`, `AnnotationDeployedBy`), and records an audit
entry `git.push` (or `git.skip`).

The controller builds the pinned SHA, never the branch:

- kpack's Image gets the SHA as its revision, so kpack stops polling on its
  own.
- The Dockerfile build key (`internal/controller/dockerfile.go`) hashes the
  SHA, so Dockerfile builds redeploy on new commits.

**Credentials for a build.** Before creating the kpack Image or the BuildKit
Job the controller writes the Secret `<app>-git` in the project namespace:

- GitHub App: an installation token limited to `repositories: [that repo]`
  with `contents: read`, valid one hour, as `kubernetes.io/basic-auth`
  (username `x-access-token`).
- Token: `kubernetes.io/basic-auth`.
- Deploy key: `kubernetes.io/ssh-auth`, with the host's key pinned in
  `known_hosts`.

kpack reads it through the `shpyrd-builder` ServiceAccount
(`internal/controller/registry.go`), with the `kpack.io/git` annotation. The
BuildKit fetch container (`fetchScript`) reads it as `.netrc` or through
`GIT_SSH_COMMAND`, and the Job runs as `shpyrd-builder`. The credential is
only in the fetch container, never in the build's `RUN` steps.

kpack rebuilds by itself when its builder image changes, and that rebuild
clones again, so for apps on a GitHub repository the watcher re-mints the
token Secret every 50 minutes. That is one API call per app per 50 minutes,
far below GitHub's 5,000 calls an hour per installation.

### 3. The GitHub App, connecting it, and what goes back

Permissions (published in the docs as a table):

| Permission | Why |
|---|---|
| Contents: read | Clone; compare commits for watch paths |
| Metadata: read | Required by GitHub |
| Checks: write | Report build and deploy on each commit |
| Deployments: write | A Deployment with the app's URL |
| Commit statuses: read | Wait for CI reads other tools' statuses |

Events: `push`, `check_suite`, `status`, `installation`,
`installation_repositories`. Pull request access comes with the previews spec.

**Registering the App** (self-hosted, a cluster admin, once):
`shpyrd github app create [--org acme]` opens GitHub's manifest page filled
with this cluster's URLs. GitHub redirects to `/api/github/app/created`; the
server exchanges the code for the App's credentials and writes
`shpyrd-github-app`. shpyrd cloud skips this step.

**Connecting a workspace** (a workspace admin): Workspace settings → Git →
Connect GitHub, or `shpyrd github connect`, which prints the install URL and
waits for it to finish. The URL carries a signed `state` (workspace, person,
expiry). The App requests user authorization during installation. On the
callback, `/api/github/installed`, the server exchanges the code once, checks
with `GET /user/installations` that this person can see the installation, and
discards the user token. Without that check anyone could claim someone else's
installation id.

When an installation is deleted or suspended, it is marked gone and audited
(`github.disconnect`). Apps that used it fail their next build with the
no-access reason.

**Using it** (any developer in the project):

- Dashboard: New project → From GitHub, with a repository picker across the
  workspace's installations and then a branch picker. The project's Source
  card shows repository, branch, last commit, why a commit was skipped or is
  pending, and the auto-deploy, watch paths and wait-for-CI settings.
- CLI and agents: `shpyrd deploy --git github.com/acme/api --ref main`.
  Without access the error names the fix (`shpyrd github connect`, or
  `shpyrd git-auth deploy-key <project>`).
- Other hosts: `shpyrd git-auth set --host gitlab.example.com --token ...`,
  `shpyrd git-auth deploy-key <project>` (prints the public key),
  `shpyrd git-auth list`, `shpyrd git-auth unset`.

**What goes back to GitHub:**

- A check run per project per commit, `shpyrd / <project>`: building, then
  deploying, then success or failure, with a summary, a link to the dashboard
  and the failure reason.
- A GitHub Deployment per release: environment `<project>`, `environment_url`
  set to the app's URL, statuses in progress, success or failure.
- A skipped or held commit gets a neutral check with the reason ("skipped:
  [skip deploy]", "waiting for CI").

We write checks, not commit statuses: a check is the modern form, shows in
the same place, and carries a summary. Writing both would show shpyrd twice.

Dashboard work follows `apps/AGENTS.md` and `design/DESIGN_FOR_AGENT.md`.

### 4. When things fail

- Webhook with a bad signature: `401` and a rate-limited log line. An unknown
  installation or project: `202` and ignored, so GitHub does not retry.
  A repeated delivery id is ignored.
- Clone or token failure: no build starts. The App's condition
  `SourceReady=False` carries a plain reason: no access ("Connect GitHub or
  add a deploy key"), token mint failed ("GitHub unavailable, retrying"),
  host key mismatch. The dashboard and `shpyrd status` show it.
- GitHub API down or rate limited: the watcher backs off and retries; polling
  catches missed webhooks. A failed write-back is logged and retried and never
  fails a deploy.
- Audit entries: `git.push`, `git.skip`, `github.connect`,
  `github.disconnect`, `git-auth.set`, `git-auth.unset`.

## Testing

- Unit: the credential lookup order; the watcher's decisions (branch,
  auto-deploy, skip, watch paths, CI states); HMAC checks for GitHub, GitLab
  and Gitea; signing and checking `state`; token Secret rendering.
- A fake GitHub API (`httptest`) for the manifest exchange, installation
  tokens, `/user/installations`, compare, checks and Deployments.
- Controller tests: the `<app>-git` Secret and the `shpyrd-builder` wiring for
  both strategies; a new SHA changes the build key.
- End to end on the local kind cluster: a private repository on an in-cluster
  Gitea for deploy keys, tokens and the generic webhook; a real test GitHub
  App on a test organisation for the App flows and the screenshots.

## Delivery

Four PRs, in order. Each updates its RFC's status and implementation history
and carries screenshots of the feature in use (dashboard, CLI output, and on
GitHub the check and Deployment where they exist).

1. Git credentials (RFC-0017): store tables and Secrets, the lookup, kpack
   and BuildKit wiring, `shpyrd git-auth`, the failure reasons.
2. Source watcher (RFC-0018): CRD fields, SHA pinning, Dockerfile
   auto-deploy, the generic webhook, polling, `[skip deploy]`, the Source card.
3. GitHub App, connect (RFC-0054): manifest registration, connecting a
   workspace, token minting, the repository and branch picker.
4. GitHub App, events (RFC-0054): App webhooks into the watcher, watch paths,
   wait for CI, checks and Deployments.
