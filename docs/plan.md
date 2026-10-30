# Embedded git: a repository per project the platform keeps

Date: 2026-10-29
Status: plan overview; researched, not started
Issue: #310. Related: #288 (builds in their own namespace), #289 (supply
chain and signing), #305 (notifications), #306 (durable audit), #312 (AI
gateway), #316 (MCP connector)
Scope: `api/v1alpha1`, `internal/controller`, `internal/cli`, `pkg/api`,
`pkg/install`, `pkg/backup`, `pkg/store`, `deploy/components`,
`deploy/profiles`, `Dockerfile`, `apps/workspace`, `content/docs`; the cloud
repository's Terraform for the backup bucket

This is the overview of the feature. A longer design document backs it with
the arithmetic, the code citations, the alternatives weighed and a
verification pass that marks each disputed, refuted or unverified claim where
it is used; it is not part of this change. Business pricing stays out of this
repository: the costs below are infrastructure costs only.

## Intent

The people this is for build applications with AI coding tools and do not know
git. Their coding tool knows git. Their folder on a laptop is the only copy of
the application: `shpyrd deploy` uploads an archive of the folder, and the
platform keeps it as a content-addressed file in the sources store. There is
no history, no second computer, no way to hand the code to someone else, and
no place for a program to change the code.

The platform's agent is the second reason. The decision taken is that the
agent becomes a second writer of the code: it commits fixes, first for failed
builds, then for guard-rail refusals (a secret file, an oversized asset, a
missing Procfile), then for production errors. An archive has no parent, no
author and no way to tell a fix from a person's upload. A commit has all three.

The third reason is the partner story. A company that leaves a consulting shop
must leave with its code; today the export of a GitHub-sourced project with a
private repository carries the deployed source but not the repository, because
its credential-less clone cannot read the code.

After this work the platform hosts a full git remote per project, in Heroku's
manner. Everyone is a clone: the laptop, the coding tool, another computer,
the platform's agent. The server accepts fast-forwards only; merging happens in
clones. A push to the default branch builds; releases point at commits; a
rollback can build an older commit again. The dashboard shows versions in
words, never branches or commits. People who already have GitHub keep the
watcher and the GitHub App; their projects get a read-only mirror. Unlike
Heroku, whose own documentation says its repository is not meant to be a
stable one, this one is durable, backed up, exported and read by role, because
for these customers the platform's copy is the only copy.

Done when:

- a push from a laptop without a git binary builds and releases: `shpyrd
  deploy` turns the folder into a clone, pushes, and the push answer names the
  version; a second computer clones and `shpyrd pull` gets the same tree;
- `git push shpyrd main` from a coding tool's own git builds the same way,
  with the one-line answer and the refusals shown as `remote:` lines;
- "Build version 7 again" rebuilds an older commit and the timeline says so;
- on kind, every repository is restored from bundles alone and passes
  `git fsck --full`; on staging, a node kill is timed and the measured RTO
  recorded;
- a GitHub-sourced project with a private repository exports with its code;
- the recovery drills have run once with their durations recorded.

Out of scope: candidate builds, the agent's sandbox and its fix commits, and
the MCP tools (they follow the first version, in the order given below); push
mirrors to GitHub or GitLab; pull request previews; Git LFS (refused with a
sentence); repositories that are not a project's.

## What a customer sees

The remote is the workspace's own address plus a path:
`https://acme.shpyrd.cloud/git/shop.git` on the cloud, the custom domain where
a workspace has one, the install's own door elsewhere. The routes sit behind a
companion Ingress on each door, under the door's host and its TLS, over HTTPS
only; there is no SSH route. The workspace is resolved from the host, as it is for every token
today, so the console host refuses every credential on purpose.

Two kinds of people:

- **Someone who never types git** runs `shpyrd deploy` in the folder. The CLI
  turns the folder into a repository with go-git, so no git binary is needed;
  it commits as the signed-in person, adds the remote `shpyrd`, pushes `main`
  with the session token as the Basic password, follows the build and prints
  `Released v8` as today. On another computer, `shpyrd pull` fetches and
  fast-forwards.
- **Someone with a coding tool** gives the tool's own git two lines, printed
  by `shpyrd git url` and shown on the dashboard beside the MCP address:

  ```
  git remote add shpyrd https://acme.shpyrd.cloud/git/shop.git
  git config credential.https://acme.shpyrd.cloud.helper '!shpyrd git-credential'
  ```

  `git push shpyrd main` builds the same way. An anonymous clone gets a 401
  with a Basic challenge, so git prompts; the credential helper answers with
  the session token from the CLI's own file, never `~/.git-credentials` and
  never a token in the URL. A personal API token at git's prompt works without
  the CLI. Connected assistants push with their OAuth token once the scope
  question is settled (open question 2).

The push answer is one line after git's own report, such as `Version 13 will
build: https://acme.shpyrd.cloud/projects/shop/releases`. The CLI's push
carries the push option `follow=build` to receive the build log on the push
(whether git tolerates the relay after report-status is unverified, open
question 8; the CLI falls back to following the build through the API as
today). A coding tool's plain push is answered at once and never held for the
build. Refusals are one sentence shown as `remote:` lines: a non-fast-forward,
over the cap, deleting the default branch, a ref outside `refs/heads/` and
`refs/tags/`, Git LFS pointers, a push to a mirror (with the GitHub URL). Other
branches and tags are accepted and build nothing.

Versions, not commits: "Version 13, Fix the sign-in error, by ada through
Claude, 2 hours ago". Two rollbacks, worded apart. "Run version 7 again" pins
the image that exists today: fast, byte-identical, within the 20 kept releases.
"Build version 7 again" builds that commit again and restores the config
snapshot; it reaches releases older than the kept history, as long as the
commit is in the repository or its archive is in the sources store.

Every build is a commit. An upload from an old CLI, the dashboard, a
kubeconfig session or a restore is committed server-side, Author the signed-in
person and Committer shpyrd, before it is deployed. A stale folder cannot
silently revert what the agent or another person pushed since: when the upload
names a commit that is behind `main`, the deploy is refused with the versions
in between ("`shpyrd pull` first, or pass `--replace`"); when it names no
commit, as a plain folder does, the deploy proceeds and the answer says the
upload replaced the whole tree, naming the versions it replaced.

## The shape

A git service in `shpyrd-system`: the git binary behind the platform's own
front door, policy in the platform's process through a hook shim, the live
repositories on one ReadWriteOnce block disk, incremental bundles in object
storage as the durable copy. Four shapes were weighed: a pure go-git service
on a bucket, a git-binary service on a disk, Gitea as a component, and a
bundle-authoritative store with a disk cache. The second was chosen; the
bucket-authoritative shapes reach zero RPO and no pause on a roll at the price
of serving from a bucket, so the layout is kept convertible to one, which is
where GitHub and GitLab are heading and where Netlify already is.

- **The wire is C git, not go-git.** In local benchmarks go-git served clones
  22 to 40 times slower than `git http-backend`; its receive-pack has no thin
  packs and no atomic push; it cannot fsck, gc or bundle; it shipped ten
  security advisories in 2026 in the parsers a push server runs on untrusted
  input; and every Go forge that tried the pure-Go wire went back to the
  binary. The binary costs about 7 MiB in the image. Git is GPL-2.0-only; it
  ships as the distro package in a separate image that the platform execs,
  the ordinary arrangement, which leaves the MPL core and the ee folder
  untouched.
- **The live store is a POSIX disk.** Ref updates are git's
  lockfile-and-rename. Nothing depends on OCI honouring `If-Match`, which the
  object gateway already works around with a single writer. GitLab refuses
  NFS, EFS and Filestore for repositories, and a shared volume adds nothing
  to a single writer. Reading individual git objects from a bucket has failed
  on latency and request storms in every documented attempt.
- **The build pipeline does not change.** The pushed commit becomes a
  content-addressed archive in the existing sources store.

The pieces:

- **Image.** A new Dockerfile stage, `shpyrd-git`: Alpine stable with the
  distro git package (2.47 or newer, asserted at start), no shell, uid 65532,
  read-only root.
- **Component.** A new `deploy/components/git`, behind install var `SHPYRD_GIT`
  (default off): a Deployment of one replica, strategy Recreate, on the data
  pool when there is one; the claim `git-data`; a Service whose git and admin
  ports are reachable only from the server pods (its metrics port from
  monitoring); a Lease the API routes by; the backup passphrase and the
  backup bucket credentials mounted.
- **The git service**, a `git-server` subcommand of the server binary: runs
  `git upload-pack` and `git receive-pack` in stateless-RPC mode as a child
  process per request, bounded by deadlines and by the pod's memory. The pod
  never opens Postgres: the catalog, the ref journal and the leases are tables
  in the control-plane database reached through the server's internal routes.
- **The hook shim.** The platform's own pre-receive and post-receive hooks run
  on every push; a repository's own hooks never run. git's index-pack checks
  every object with `receive.fsckObjects` before any hook. Pre-receive:
  fast-forward only, the default branch undeletable, refs only under
  `refs/heads/` and `refs/tags/`, no Git LFS, a warning on `.gitmodules` and
  on secret-file names, and the caps: 500 MB per repository by default,
  settable to 2 GB; 100 MiB per blob; 1,000 commands and 100,000 objects per
  push; 5,000 entries per tree and 60 levels. Post-receive journals the push
  and answers at once.
- **API routes** in `pkg/api` at the engine root, so the open-source platform
  gets the repository too: `GET /git/:repo/info/refs`,
  `POST /git/:repo/git-upload-pack`, `POST /git/:repo/git-receive-pack`. HTTP
  Basic with the token as password, resolved by the identifiers the API has
  (personal and session tokens; OAuth tokens with the license); the admin token
  refused ("the admin token cannot push code: sign in or create a token");
  401 Basic for no credential, 404 for a valid credential without ProjectView;
  ProjectView to clone, ProjectDeploy to push; the maintenance gate refuses
  pushes (423) and leaves clones; a suspended workspace keeps clones for 30
  days and refuses pushes (to confirm, open question 1). The API signs what it
  forwards to the git pod, with a short expiry; the pod trusts only that
  signature, and the API reaches only the pod that holds the Lease.
- **Front door.** One more companion Ingress per door beside the two that
  exist for source uploads and project archives, with the body size and the
  timeouts a push needs.
- **Build integration.** After a push to the default branch a pending-deploy
  worker, not post-receive, asks the service for `git archive` of the commit,
  puts it in the sources store and calls `deployCommit` through `mutateApp`,
  which sets `spec.source.blob` with `Ref` the full commit, exactly as a CLI
  upload does today. kpack, BuildKit and the release path are unchanged.
  `Release` gains `Commit`, `SourceKind`, `BuildNumber` and `RollbackOf`. The
  Aptfile translation the CLI applies today moves server-side into the
  archive step. A crash between the ref update and the journal is repaired by
  a periodic sweep.
- **Archive garbage collector**, a prerequisite: nothing deletes archives
  today and every built commit adds one. A leader-only sweep keeps every
  archive named by a current source or a kept release and deletes the rest
  after 30 days. It is the one new deletion path in the platform.
- **Open mode.** A cluster running without sign-in refuses `/git`: a push
  needs an author, so the route answers "sign in: a push needs an author".

## What changes, by area

| Area | Today | After |
|---|---|---|
| CLI | `exec git` or a tar of the folder; archive upload | go-git: a plain folder becomes a clone and pushes; `shpyrd pull`, `shpyrd git url`, `shpyrd git-credential`, `deploy --commit`, `rollback --rebuild`; releases in words |
| API | `POST /api/sources` and the blob deploy | the `/git` routes; `deployCommit`; the repository in the project detail; commits; `PATCH /api/projects/:slug/repository {cap, defaultBranch}`; `rollback {release, rebuild: true}`; the archive garbage collector |
| Controller | the kpack and BuildKit pipeline | unchanged pipeline; `Release` fields; the mirror fetch at pin time for GitHub projects; one fix so a failed rebuild does not leave the rollback annotation standing |
| Install | | component `git` at runlevel rc4; hook `git-internal`; vars `SHPYRD_GIT` and `SHPYRD_GIT_SIZE`; the Dockerfile stage; CRD regeneration; a store migration for three tables |
| Storage | the sources store grows without bound | the repository disk; the archive garbage collector |
| Backup and export | the platform backup; the project archive with `source/source.tgz` | bundles under `git/` on the backup target; backup `FormatVersion` 2 with `git/index.json`; `repository/repo.bundle` in the project archive; `shpyrd-ctl project restore-repository` |
| Dashboard and MCP | a GitHub-shaped source block; 12-character revisions and digests on the Releases page | a repository block with the clone address and the coding-tool lines; versions in words; the two rollbacks; Activity marks `git.push` and `git.rollback`; a design session per `design/DESIGN_FOR_AGENT.md`; MCP tools later |
| Docs | `content/docs/deploying.mdx` | rewritten for both kinds of people |

## Storage

The live repositories live on one block claim, `git-data` in `shpyrd-system`,
ReadWriteOnce, with the profile's storage class named explicitly on the cloud
profiles so the nightly snapshot job covers it (omitted on kind, where the
cluster default applies). Proposed defaults (open question 12): 200Gi on the
cloud profiles, 20Gi local.

| Profile | Class | Snapshots | Durable copy |
|---|---|---|---|
| local (kind) | the cluster default, local-path | none | bundles to the local backup target; the only second copy |
| OCI | `oci-bv`, expansion allowed, 50 GiB minimum | nightly, keep 7 | bundles to the backup target; a separate physical bucket in production |
| AWS | `gp3`, encrypted, expansion allowed | nightly, keep 7 | bundles to the backup target |

The disk is provisioned by a rule, not a multiple. At 70 percent used the
service asks the server to grow the claim by half (local-path cannot expand,
so the local profile alerts instead); whether that request needs an operator
gate on OCI's 50 GiB minimum class is open (question 12). At 95 percent pushes
are refused with "the platform's code store is full; the operator has been
told", so one tenant filling the disk cannot corrupt everyone's repositories.
Repositories are repacked with bitmaps and garbage-collected with a grace
period; the reflog is kept 90 days; a deleted repository is soft-deleted for
30 days and its bundles stay until 60 days after its last push.

## Disaster recovery

The copies:

1. The live disk. A push is acknowledged once fsync'd (`core.fsync=all`).
2. Nightly provider snapshots of the claim where the profile has them (OCI and
   AWS, keep 7; none on kind).
3. Age-encrypted bundles in object storage under `git/` on the platform backup
   target, written within about a minute of a push: incremental, with a full
   when the chain passes 20 increments or doubles the size of the full, at
   most weekly and at least every 60 days; 60-day retention. Every bundle
   passes `git bundle verify` (format and prerequisites; object integrity is
   the sample restore's fsck). In production this copy lives in a separate
   physical bucket from the cloud repository's Terraform: written by a
   credential that can only PUT, retention-locked, replicated cross-region to
   a read-only destination, to be in place before the rollout (open
   question 3). Until it exists bundles land in the gateway's
   `platform-backups` logical bucket with 7-day versions as the only undo, and
   a region loss is not recoverable.
4. The release archives in the sources store: every built commit's tree, kept
   as long as a release names it, so a release stays rebuildable and runnable
   even if the repository lost the commit.
5. The project export, which gains `repository/repo.bundle`, and the
   control-plane dump, which carries the catalog.

Objectives, targets until a drill measures them:

| Scenario | RPO | RTO |
|---|---|---|
| Pod crash | 0 | 20 to 40 s; an in-flight push fails and the client retries |
| Planned roll or drain | 0 | 1 to 3 min, seen as a slow push |
| Unplanned node loss | 0 | 10 min target, to measure on staging |
| Volume lost or corrupted | seconds to 1 min (bundle lag) | 5 to 15 min from the newest snapshot plus bundle replay, target 1 h; from bundles alone about 30 min for 200 GB at 100 MB/s plus fsck, target 2 h |
| One repository deleted by mistake | 0 to 1 min | minutes, one admin route |
| Backup job failing silently | bounded by the alert `GitBundleStale` | none: serving never stops |
| Control-plane database loss | the WAL archive interval for the catalog and the journal; 0 for the repositories, which carry their own IDs in a `shpyrd.json` marker written into each repository | the database restore plus one reconciliation pass |
| Region loss | about 1 min plus replication lag | hours; target 4 h after a drill |

Verification runs on its own: an hourly sample restore that clones a random
1 percent of repositories from their bundles, all of them weekly; a nightly
reconciliation of catalog, disk and bucket; a probe repository; a monthly and
a quarterly fsck of every live repository; and the alerts `GitBundleStale`,
`GitBundleVerifyFailed`, `GitLeaseLost`, `GitFsckFailed` and
`GitDiskNearlyFull`. A bad but valid push is not a disaster: fast-forward
only, no default-branch delete, the journal with old and new oids, and the two
rollbacks. Drills on staging before production, including a node kill, with
the durations recorded and the measured node-loss RTO entered in the design
document.

## Costs

Infrastructure only, at list prices read on 2026-10-28 and 2026-10-29 (Oracle's
price list dated 2026-10-08; AWS's S3 list dated 2026-10-15 and its
data-transfer list 2026-09-16). 1 GB = 1,024 MB. Two fleet sizes and two mean
repository sizes: 20 MB is above the medians of every sampled AI-built fleet,
200 MB near the worst sampled p90. The sample is the GitHub API sizes of 60
Lovable, 60 v0, 30 Bolt and 60 Replit public repositories found by code search
on 2026-10-28, not a random one; the right next measurement is the size
distribution of the platform's own uploaded archives. The storage lines assume
20 pushes a month per project with 2.5 percent of the tree changed per push,
a stated assumption, not a measurement. Compute counts the pod, not a node
dedicated to the data pool, and the 200 MB totals use the same 1 OCPU, 4 GB
pod as the small fleet; a pod sized for 500 MB repositories adds to the
compute line. The totals carry no internet egress; the egress ceiling below is
given at both the North America and Europe rate and OCI's South America rate.
The AWS column is `us-east-1` list; an AWS South America region would price
egress higher and is not computed, so the columns are not like for like.

| Fleet | Mean repository | OCI, USD a month | AWS, USD a month |
|---|---|---|---|
| 1,000 projects | 20 MB | 35.96 | 52.42 |
| 1,000 projects | 200 MB | 80.96 | 108.13 |
| 10,000 projects | 20 MB | 86.19 | 118.11 |
| 10,000 projects | 200 MB | 626.11 | 844.69 |

The lines behind each total: one always-on pod (22.63 USD a month on OCI;
about 30 on AWS, an estimate with no SKU read, open question 11), the
provisioned disk and its snapshots, the bundles and their replica, the
requests, and the release archives. What the numbers say:

- Storage is a rounding error on both clouds at both sizes; compute dominates
  the small fleet. The largest fleet stores about 21 TB across disk,
  snapshots, bundles, replica and archives.
- The disk against a bucket is an operational choice, not a financial one:
  the disk headroom and the snapshot line cost 18.73 USD a month on OCI over a
  bucket-authoritative design at 10,000 projects of 20 MB, 205.04 at 200 MB.
- Egress is the only term that grows with use. The ceiling, every repository
  cloned over the internet 30 times a month plus the 2,930 GB a month of bundle
  replication, assumed to count against the same 10,240 GB allowance (to
  confirm with Oracle, open question 11), is 435.91 USD at 10,000 projects of
  200 MB with the North America rate and 1,282.09 with OCI's South America
  rate, twice everything else together. Realistic coding-tool traffic is about
  a tenth of that; builds, sandboxes and backup writes stay in the region and
  are free on both clouds.
- A shared file system for the live copy would cost 58.59 and 585.94 USD a
  month for the used bytes alone at 10,000 projects, twelve times the object
  store.

## Security

- The token is never in a URL or a log: the credential helper keeps it in the
  CLI's own file, the `Authorization` header is logged only as the token ID,
  failed attempts are throttled per IP (20 a minute) and audited, and
  repositories the caller may not see answer 404. OAuth tokens need the
  license, an audience check and a write scope for pushes (open question 2).
- The repository path is derived from the signed project ID, never from the
  URL; the slug is validated before any lookup. The git pod trusts only the
  signed header, is reachable only from the server pods, and never dials the
  internet except GitHub for mirrors, through the platform's safe HTTP client.
- On the receive side, the work C git does once configured: fsck of every
  pushed object, the caps above, a quarantine that leaves nothing on refusal,
  the fixed hooks path, git executed with an argument list and no shell in an
  image with no shell. Each request is one bounded child process; the
  ServiceAccount can only touch its Lease.
- Builds never clone: they fetch archives with capability URLs as today. The
  sandbox, later, clones with a capability credential in its own namespace. A
  developer-role user can shape anything in a repository, so it is untrusted
  input even though the platform hosts it.
- The volume is encrypted by the provider class; bundles are age-encrypted
  before upload; the bundler's backup credential can only PUT (a split that
  exists only with the separate production bucket). Config vars never enter
  the repository.
- The CLI writes a `.gitignore` excluding `.env` before the first commit; the
  pre-receive secret-file list (private keys, `aws/credentials`, `*.pem`,
  `*.key`, shell histories) warns in the first release, then refuses.
- Per push, a journal row and an Event with actor, token ID, IP, each ref old
  to new, pack bytes, outcome and request ID, the same ID printed in the
  `remote:` lines. A per-actor push rate and a per-project clone rate.
- Supply chain (#289): git from a distro package with its checksum in the
  image build; go-git in the CLI pinned and bumped monthly; the hook shim is
  the platform's own binary.

## Migration

- **Projects deployed from a folder.** Nothing moves on upgrade. The
  repository is created empty on project creation and, for existing projects,
  lazily on the first `shpyrd deploy` from the new CLI or on demand from the
  dashboard. It is never pre-seeded from the current archive: a seeded root
  would make the person's first push a non-fast-forward of unrelated
  histories. For a project whose folder is gone, the project page offers
  "Start the repository from the running version" (admin-gated, only while the
  repository is empty), which commits the running archive as the first commit.
  Old CLIs keep working: their uploads are committed server-side.
  Kubeconfig-only operators keep the archive path for good. Image-only
  projects get no repository until a source deploy.
- **GitHub-sourced projects.** They keep the watcher, the gates and the
  reporter unchanged. At each pin the service fetches the pinned commit and
  the followed branch from GitHub with the GitHub App's `contents:read`
  credential the project already has. The mirror is full history or nothing:
  a bundle of a shallow repository verifies but cannot be cloned. A repository
  over the cap gets no mirror; the export then falls back to `git archive` of
  the pinned commit. Pushes to a mirror are refused with the GitHub URL: never
  a silent overwrite. The mirror also fixes the project export for private
  repositories.
- **Changing the source kind.** `shpyrd deploy --git <url>` on an embedded
  project makes it GitHub-sourced; its own history stays reachable under
  `refs/shpyrd/embedded/<date>` for 30 days and in the bundles for 60.
  Disconnecting GitHub leaves the mirror read-only until a person deploys from
  a clone of it or chooses "Make this the project's repository".
  `shpyrd deploy --image` pins the image as today and leaves the repository
  alone. The default branch is `main` at creation and changes through
  `PATCH /api/projects/:slug/repository`.
- **Rollout.** The cloud repository's deploy pipeline publishes the `shpyrd-git`
  image beside the server image. `cluster init` adds the component behind
  `SHPYRD_GIT`, off by default, with the routes answering 404 while off.
  Enable on the dev cluster, run the drills including a node kill, enable in
  production with the separate backup bucket in place, release the CLI, then
  the docs.
- **Downgrade.** `cluster init --skip git` removes the component; the
  repositories stay on the retained claim; the blob path serves builds
  throughout; every built commit already has its archive, so nothing that ran
  stops being rebuildable.

## The path to the agent

Everything the agent needs is a function of a commit and a repository the
platform already serves; each step adds, nothing is reworked.

1. **Candidate builds**: build a commit without releasing. `deployCommit`
   splits into `buildCommit` and `releaseBuild`; a candidate is a BuildKit Job
   or a second kpack Image tagged apart from the releases; `GET
   /api/projects/:slug/builds` lists candidates with their commit; a green
   candidate becomes a release without a second build. About two weeks of
   work after the first version (weeks 19 to 20 below); waits for #288.
2. **The sandbox**: a Pod in the project namespace from the current release's
   image, with no service account token, that clones one commit from the git
   service with a capability credential the service verifies itself. The
   agent runs commands in it through the existing exec plumbing under the AI
   gateway (#312) and never sees a person's token.
3. **Fix commits**: Author `shpyrd agent`, Committer `shpyrd`, with trailers
   naming the intent (`Shpyrd-Intent: fix failed build 13`), the trigger and
   who asked. The push capability is bound to the project, the ref and the
   expected old commit, so a human push in between makes the agent's push
   "fetch first" and it rebases in its sandbox; a conflict it cannot rebase is
   left on a branch with a message on the project page. The timeline reads
   "Version 14, the platform fixed the failed build of version 13". One agent
   run at a time per project. Failed builds first, then guard rails, then
   production errors.
4. **Identity**: a per-project machine principal `agent` with the developer
   role, shown in the members list, with no HTTP token of its own, only
   capabilities the server mints for a sandbox; a workspace can switch it off
   per project. Its shape in `pkg/authz` is to settle before the agent slice.
5. **MCP tools**: read a file or a tree at a commit, list versions, propose a
   change (edits committed on a proposal branch, author the person, committer
   shpyrd, with a candidate build), deploy a branch, build a candidate, roll
   back a version. Reads on `projects:read`; writes on `projects:write` with
   the consent text extended, or a `code:write` scope later, which means
   re-consent of every connected assistant.
6. **Conflicts between a laptop and the agent**: the CLI's fetch-and-merge
   handles the common case; a true conflict is a file list the CLI prints and
   a sandbox task offered to the agent when the person says yes.
7. **GitHub-sourced projects**: the sandbox clones the mirror; fixes must go
   to GitHub and need `contents:write` on the App, which every installation
   would re-approve, so the agent's write path for these waits for that
   decision.

## Order of work and effort

One engineer who knows the platform; weeks are calendar weeks of one person.

| Slice | Weeks | What lands |
|---|---|---|
| 1. The image and the component | 1 to 2 | The image, the component and the `git-server` subcommand; a real git client against it on kind |
| 2. Policy and journal | 3 to 4 | The hook shim and its policy; the catalog, journal and lease tables; the recovery sweep; repository create and soft delete |
| 3. The API and the front door | 5 | The `/git` routes, the signed proxy, the companion Ingress per door, rate limits; a load test with 500 concurrent clones |
| 4. Build integration | 6 to 7 | `deployCommit`, the pending-deploy worker, the `Release` fields, rollback by rebuild, the archive garbage collector, the opt-in build relay. **First push-to-deploy works at the end of week 7** |
| 5. The CLI | 8 to 9 | go-git replaces exec git; the plain-folder and existing-clone flows; the new commands; docs for both kinds of people |
| 6. Durability and recovery | 10 to 11 | Bundles, the separate-bucket Terraform in the cloud repository, the sample restore and alerts, reconciliation, maintenance, the backup and export changes, the first timed drill on staging including a node loss |
| 7. GitHub mirrors | 12 | Fetch at pin time, the cap rule, export through the mirror, the source-kind transitions |
| 8. The dashboard | 13 to 14 | A design session: the repository block, versions in words, the two rollbacks, Activity marks, mock data, docs pages |
| 9. Hardening and release | 15 | Policy under load, failure messages reviewed from a non-developer's seat, chaos tests, the runbook, the roll on the dev cluster then production |
| 10. The agent's first slice | 16 to 18 | The machine principal and capabilities, the sandbox, fix commits for failed builds, the first MCP tools |
| 11. Candidate builds | 19 to 20 | `buildCommit` and `releaseBuild`, the candidates tag, the second kpack Image, the `/builds` listing |

Total: 15 weeks to a first version, 18 with the agent's first slice, 20 with
candidate builds. The largest uncertainties are the hook shim (weeks 3 and 4),
the opt-in build relay (week 7), the separate backup bucket depending on the
cloud repository (week 10) and the design session's scope (weeks 13 and 14).

## Decisions

The issue's three open questions:

1. **Object storage or a shared volume for the live repositories?** Neither.
   A ReadWriteOnce block claim behind one pod holds the live repositories;
   object storage holds the bundles as the durable copy. A bucket as the
   authority with a disk cache is the next step, and the layout allows it.
2. **Does `shpyrd deploy` always commit?** Always. Every build is a commit;
   uploads are committed server-side. Opting in would leave two kinds of
   history and an agent with nothing to work on for the projects that matter
   most.
3. **A push mirror to GitHub or GitLab in the first version?** No. Only the
   read-only mirror in the other direction, for GitHub-sourced projects. A
   push mirror needs `contents:write` on the App manifest now and breaks the
   promise that installations never approve again.

The other decisions a reader might otherwise reopen, each against what it
replaced:

| Decision | Chosen | Rejected |
|---|---|---|
| Who speaks git on the server | the git binary as children of a Go service, policy in a hook shim | go-git as the wire; Gitea as a component; `git http-backend` through CGI (chunked push bodies fail) |
| How a commit reaches the build | an archive in the sources store through `mutateApp` | a git source with credentials for kpack and BuildKit |
| What a plain `git push` answers | one line after report-status; the build log opt-in through the CLI's push option | streaming the build into every push, Heroku's shape |
| Where the routes live | `pkg/api` at the engine root | an ee extension |
| Pre-seeding existing projects | never automatic; an explicit admin import on an empty repository | importing the archive as the first generation |
| Archive and deploy on a push | asynchronous, from the journal row | inside post-receive, which holds the push for a gateway write |
| Mirrors of large GitHub repositories | none over the cap | a shallow mirror, whose bundle cannot be cloned |
| Default cap | 500 MB, settable to 2 GB | 200 MB |
| The locked backup copy | a separate physical bucket | a gateway logical bucket, which cannot be locked |
| Placement and database access | runlevel rc4; the git pod never opens Postgres; a signed trust header | rc3 with a database role; a shared Secret as the whole trust |
| CLI on divergence | fetch; fast-forward with go-git; `git merge` when a binary exists; otherwise refuse with the files named | a three-way merge with go-git, which merges fast-forward only |

## Open questions

Product decisions:

1. Should a suspended workspace still allow clones for 30 days while refusing
   pushes? Today the tenant check answers 403 for everything.
2. Do OAuth tokens minted for connected assistants get a git audience, or
   does `projects:write` with extended consent text cover pushes until a
   `code:write` scope exists? A new scope means re-consent of every connected
   assistant.
3. Does the cloud repository's Terraform gain the separate backup bucket
   before the production rollout, and do the platform backup target and the
   control-plane backups move cross-region with it?
4. Is 10 pushes a minute per actor right for coding tools that commit after
   every action? Measure on the dev cluster first.
5. Does the dashboard hide revisions and digests for every project or only for
   embedded ones? For the design session.
6. Should personal API tokens travel in the store dump so git remotes survive
   a platform restore, or do remotes authenticate again after one?
7. Should the archive garbage collector also apply to blob deploys made before
   this feature, and is a 30-day grace enough for operators who roll back
   further than the kept history?

To verify, or to settle before the slice that needs it:

8. Does native git tolerate the opt-in build relay's lines after
   report-status? The fallback is the CLI following the build through the
   existing build log stream.
9. The measured RTO of an unplanned node loss on OCI and AWS, given the
   attach-detach controller's force-detach wait (about 6 minutes, unverified)
   and the availability-domain scope of OCI block volumes (unverified); the
   data pool may need a second node in the volume's zone.
10. Whether OCI's S3 compatibility endpoint honours `If-Match` and
    `If-None-Match: *`, which decides whether the bucket-authoritative next
    step can use a create-only PUT as its compare-and-swap.
11. OCI prices to confirm: that cross-region replication traffic falls under
    the egress allowance; and the AWS compute figure, which is an estimate.
12. How many data-pool nodes and how much headroom `SHPYRD_GIT_SIZE` should
    default to per profile, and whether the 70 percent expansion needs an
    operator gate on OCI's 50 GiB minimum.
13. The shape of the machine principal `agent` in `pkg/authz` and in the
    members list, before the agent slice.
