# Embedded git: a repository per project the platform keeps

Date: 2026-10-28
Status: plan; researched, not started. The overview is the description of
PR #339.
Issue: #310 (provisional, not started; its record before the RFC retirement is
`git show 50231629:rfcs/0069-embedded-git.md`). Related: #288 (builds in
their own namespace), #289 (supply chain and signing), #305 (notifications),
#306 (durable audit), #312 (AI gateway), #316 (MCP connector)
Scope: `api/v1alpha1`, `internal/controller`, `internal/cli`, `pkg/api`,
`pkg/install`, `pkg/backup`, `pkg/store`, `deploy/components`,
`deploy/profiles`, `Dockerfile`, `apps/workspace`, `content/docs`; the cloud
repository's Terraform for the backup bucket

How to read this document. A claim the verification pass marked disputed,
refuted or unverified carries a short marker where it is used, such as
`(disputed, C1)`; section 13.2 lists them with their corrections, and the
mechanisms of the first draft that the review found wrong. Numbers come with
their arithmetic and their source. Business pricing stays out: the core
repository is public, so the costs in section 6 are infrastructure costs only.

## Summary

A git service in `shpyrd-system`: the git binary behind the platform's front
door, policy in the platform's own process through a hook shim, the live
repositories on one ReadWriteOnce block disk, incremental bundles in object
storage as the durable copy (section 3). A push to `main` is journalled,
answered in one line, exported as a tar.gz archive into the existing sources
store and deployed through `mutateApp` exactly as a CLI upload is today, so
kpack, BuildKit and the release path do not change (section 3.5). The CLI
turns a plain folder into a clone with go-git and pushes; a coding tool's own
git pushes to the same remote (section 3.6). Every build is a commit.
GitHub-sourced projects get a read-only mirror (section 8).

Costs, infrastructure only at list prices (section 6): about 36 USD a month at
1,000 projects of 20 MB on OCI, 86 USD at 10,000 projects of 20 MB, 626 USD at
10,000 projects of 200 MB. Internet egress is the only term that grows with
use.

Disaster recovery (section 5): a push is acknowledged once fsync'd to the disk
and is bundled to the backup bucket within about a minute. RPO is zero for
anything short of losing the volume and seconds to a minute for that; RTO is
minutes for one repository, a 10-minute target for an unplanned node loss and
hours for a region, each to be measured in the drills of section 5.6.
Production needs a separate, retention-locked, cross-region backup bucket from
the cloud repository's Terraform before the rollout (section 5.1).

Effort (section 11): 15 weeks of one engineer to a first version, 18 with the
agent's first slice (fixes for failed builds), 20 with candidate builds. The
decisions in one table: section 12. What is still open: section 13.1.

## 1. Why now

The people this is for build applications with AI coding tools and do not know
git. Their coding tool knows git. Their folder on a laptop is the only copy of
the application, and `shpyrd deploy` today uploads an archive of that folder
(`internal/cli/deploy.go:256`), which the platform keeps as a content-addressed
file in the sources store (`pkg/api/sources.go:106-152`) and never deletes
(nothing in `pkg/api/sources.go` deletes; `deleteApp` leaves archives behind,
`pkg/api/apps.go:1057`). There is no history, no second deploy of the same
project from another computer, no way to hand the code to someone else, and no
place for a program to change the code.

The platform's agent is the second reason. The decision already taken is that
the agent becomes a second writer of the repository: it commits fixes, first for failed builds, then for guard-rail refusals (a secret
file, an oversized asset, a missing Procfile), then for production errors.
Agents change code, not archives. An archive has no parent, no author and no
way to tell a fix from a person's upload. A commit has all three.

The third reason is the partner story. A company that leaves a consulting shop
must leave with its code. Today the only copy a partner holds is on the
partner's laptop; the project export carries the deployed source as one
`source/source.tgz` (`pkg/api/project_archive_metadata.go:234`) and, for a
GitHub-sourced project, a credential-less clone that fails on private
repositories (`pkg/api/project_archive_runtime.go:383-419`).

The decisions already taken, which this document treats as given:

- The repository is a full git remote in Heroku's manner. Everyone, the agent
  included, is a clone. The server accepts fast-forwards; merging happens in
  clones; rare conflicts are handed to the agent.
- Build on push to the default branch. Releases point at commits. Rollback to a
  version builds that commit again. The dashboard shows versions in words,
  never branches or commits.
- The CLI turns a plain folder into a clone on the first deploy; a
  non-developer never types git. Whether the CLI needs a git binary or uses
  go-git was open; section 3.6 decides it.
- Repository monitoring and the GitHub App (both implemented; their records
  are `git show 50231629:rfcs/0018-repository-monitoring.md` and
  `rfcs/0054-github-app.md`) stay for people who already have GitHub. Their
  projects get a read-only mirror.
- Later: candidate builds, the agent's sandbox, fix commits carrying their
  intent in a trailer, MCP tools, fixes to GitHub for GitHub-sourced projects.

This revisits git push deploys, rejected once (`git show
50231629:rfcs/0050-git-push-receiver.md`), with a different motivation. That
proposal asked whether `git push` is a nicer deploy than an upload. This asks
where the source of applications built by people with no repository lives, and
what surface the agent works on. Heroku itself says its repository "is not
intended to be a stable git repository" (devcenter, git article, 2026-08-27).
This design does what Heroku chose not to: durability, backup, export and
access by role, because for these customers the platform's copy is the only
copy.

Done when:

- a push from a laptop without a git binary builds and releases: `shpyrd
  deploy` turns the folder into a clone, pushes, and the push answer names the
  version; a second computer clones and `shpyrd pull` gets the same tree;
- `git push shpyrd main` from a coding tool's own git builds the same way, with
  the one-line answer and the refusals of section 3.3 shown as `remote:` lines;
- "Build version 7 again" rebuilds an older commit and the timeline says so;
- on kind, every repository is restored from bundles alone and passes
  `git fsck --full`; on staging, a node kill is timed and the measured RTO is
  in section 5.3;
- a GitHub-sourced project with a private repository exports with its code;
- the drills of section 5.6 have run once with their durations recorded.

Out of scope: candidate builds, the agent's sandbox and its fix commits, and
the MCP tools (section 9 orders them; they follow the first version); push
mirrors to GitHub or GitLab; pull request previews; Git LFS (refused with a
sentence, section 3.3); repositories that are not a project's.

## 2. What exists today and what changes

Source reaches the platform three ways today: `spec.source.blob` (an archive
the CLI uploaded, identified by SHA-256, served to build pods on port 8082 with
a capability URL), `spec.source.git` (a URL and a revision, a branch pinned to
a commit by the source watcher), or `spec.image` (a pinned digest, set by
`--local-build` and by rollbacks). The build pipeline consumes the first two
through kpack (`internal/controller/desired.go:588-604`) or a BuildKit Job
whose fetch init container runs `wget` for a blob or `git clone` for a git URL
(`internal/controller/dockerfile.go:521-573`). A release records a
12-character source string (`internal/controller/status.go:176-230`), not a
full commit. Rollback pins the old image (`pkg/api/apps.go:1116-1149`); it
never rebuilds.

The table lists every touchpoint, grouped by area. "Change" is what this
design does there; the sections that follow give the reasons.

| Group | Where | Today | Change |
|---|---|---|---|
| CLI | `internal/cli/deploy.go:210-263`, `archiveSource` 405-446 | The local tree is archived with `git archive HEAD` through `exec.Command("git")`, or the folder is tarred when `git rev-parse` fails (including when no git binary exists, 410-414); `.gitignore` is never read (463-514) | The default source case becomes "make this folder a clone and push": init, `.gitignore`, commit as the signed-in person, remote, push with push options. The exec archive path goes; go-git v5.19.x comes in (section 3.6) |
| CLI | `internal/cli/deploy.go:421-427`, `internal/controller/status.go:222-225` | `--working-tree` tars the folder and suffixes the ref `-dirty` | Replaced by `--commit "message"` (commit everything now) or a refusal naming the uncommitted changes; `short()` keeps accepting `-dirty` for old releases |
| CLI | `internal/cli/deploy.go:239-255`, `internal/cli/aptfile.go:12-17, 59-75` | `withSystemPackages(archive, pkg)` rewrites the uploaded archive (the Aptfile, or `<workspace package>/Aptfile`, into project.toml) and promises the repository is untouched | The translation moves server-side into the archive step, which reads the package path from `spec.build.workspace` (section 3.5); the promise is kept because the archive, not the commit, is rewritten |
| CLI | `internal/cli/deploy.go:126-208`, `jsworkspace.go:143`, `profiles.go:171-250`, `runtime_size.go:17` | Local detections (workspace root, Dockerfile, profiles, runtime) run on the folder | Unchanged; they stay valid on a clone. The JavaScript workspace root is the repository root |
| CLI | `internal/cli/appclient.go:620-675` `followBuildAPI`, 500-560 `waitRunningAPI` | Polls `status.latestBuild`, streams `/builds/:name/logs?follow=true`, waits for the release | Kept as the fallback; with push option `follow=build` the log arrives on the push (section 3.3) |
| CLI | `internal/cli/deploy.go:285-302` `sourceChanged`, 529 `sameSource` | Decides whether a build is expected after `/deploy`: a new blob SHA, a changed `Build` block or no previous build | After a push the CLI calls no `/deploy`; it expects a build when the push answer says a version will build and skips the wait when the answer says nothing was built (section 3.3) |
| CLI | `internal/cli/appclient.go:104-225` `projectConfig`; `projectconfig_save.go:20` `saveInferences` | `shpyrd.yaml` names project, processes, build, env, domains, globals, exposure and allow; nothing about a source, remote or branch | Unchanged: `shpyrd.yaml` gains nothing, the remote lives in `.git/config` (section 3.6) |
| CLI | `internal/cli/appclient.go:677-687` `uploadSourceAPI` | Uploads the archive to `POST /api/sources` | Kept for kubeconfig-only sessions and old CLIs; the server commits such uploads (section 8) |
| CLI | `internal/cli/registry.go:179-237, 251` | Session bearer token on direct calls; kubeconfig proxy with the admin token otherwise | The session token becomes git's Basic password through a credential helper; the kubeconfig path keeps the archive upload (no HTTPS door for git) |
| CLI | `internal/cli/login.go:105-161` `normaliseURL`, `SessionFor`; `clilogin.go:35` | One session token per workspace URL in `sessions.json` (mode 0600), 30-day session kind | `shpyrd git-credential get` answers git's credential protocol from this file; `password_expiry_utc` from the session's expiry |
| CLI | `internal/cli/appcmds.go:436-480` `releases`, 482-607 `rollback`; `apps.go:529-537` | Releases print digest and description; rollback pins the image | Versions in words; the commit only under `--json`; `rollback --rebuild` |
| CLI | `internal/cli/root.go:124-167`; `deploy.go:210-222, 334` (`--git`, `--image`) | `git-auth`, `git-hooks`, `github` are API-only commands; `--git` and `--image` pick the other two source kinds | `shpyrd pull`, `shpyrd git url [--set]`, `shpyrd git-credential` registered beside them; `--git` and `--image` keep their meaning and change the project's source kind as section 8 says |
| API | `pkg/api/server.go:985` `NoRoute` | A `/git/...` path answers the dashboard HTML with 200 | Three routes at the engine root: `GET /git/:repo/info/refs`, `POST /git/:repo/git-upload-pack`, `POST /git/:repo/git-receive-pack` (section 3.7) |
| API | `pkg/api/server.go:1017-1085` `auth()`, `tokens.go:66-74` `bearerOf`, 1062 and 1077 Bearer challenge | Only `X-Shpyrd-Token` and `Authorization: Bearer` are read; the 401 challenge names Bearer, which git cannot satisfy | A `gitAuth` middleware reads Basic, sets the password as a synthetic `X-Shpyrd-Token`, runs `identifyWithToken` and the identifier loop itself, answers `WWW-Authenticate: Basic realm="shpyrd"` (section 3.7, C7) |
| API | `pkg/api/tokens.go:104-205` `resolveAPIToken`; `ee/mcp/oauth.go:614-653`, 659-671 | Hash lookup, workspace binding (174-175), active owner (182-184), role intersection (136-155); OAuth identifier registered while licensed (`ee/mcp/extension.go:72`) | Reused unchanged. OAuth tokens need an audience or `projects:write` for pushes (open question) |
| API | `pkg/api/members.go:108-156` `require`, 757-770 `workspaceOnlyPath`; `hosts.go:179-184` `machinePath`; `pkg/project/project.go:22-25` | `require` reads `c.Param("slug")`; slugs forbid dots; `/git/` is in neither prefix list | A slug middleware strips `.git` into the `slug` param by mutating `c.Params`; `/git/` joins both lists so the console host says "not a workspace" and a moved address is served in place |
| API | `pkg/api/host.go:66-67, 130-132` `Sign`/`Verify` | EdDSA edge keys sign and verify | The API-to-git-service trust header is signed with these keys, 60-second expiry |
| API | `pkg/api/auth.go:872-886` `securityHeaders` | A Content-Security-Policy header on every non-`/api/` answer (880) | `/git/` joins the exemption, so git answers carry no CSP; the other headers stay |
| API | `pkg/api/auth.go:937-999` `rateLimiter`; `shell.go:116` `throttleShellMints`; `shellstate.go:223` `actorKey` | Per-IP and per-actor token buckets, in one replica's memory | Failed Basic attempts and info/refs are limited in the API (per replica); the push limit lives in the git service (section 3.7) |
| API | `pkg/api/apps.go:684-696` `deployDetail` | The audit detail is the image or `<git url>@<rev>`, and empty for a blob deploy | `deployCommit` audits with the branch and the commit, so a push's deploy entry is never blank |
| API | `pkg/api/project_archive_download.go:44-69` | A file that exists on one replica is reached through a route record in the store | The routing pattern the lease table reuses when git pods shard by project (section 4.2) |
| API | `pkg/api/apps.go:857-859, 912-931` `deployApp` blob branch | Sets `spec.image=""`, `spec.source={Blob, SubPath}`, release note and attribution | `deployCommit(project, commit, identity, note)` does the same from an archive the git service exported (section 3.5) |
| API | `pkg/api/apps.go:1116-1149` `rollbackApp`, 1225 `redeployApp` | Image pin; rebuild refused while `spec.image` is set | A second rollback, `{release, rebuild: true}`, deploys the release's commit again (section 3.8) |
| API | `pkg/api/apps.go:208-231` `ReleaseView`, 281 `detail` | `source` is 12 characters | `commit`, `sourceKind`, `buildNumber`, `rollbackOf`; `detail().repository` block |
| API | `pkg/api/apps.go:448` `createApp`, 1057 `deleteApp`, 1416 `renameApp` | No repository exists | Create the repository empty; soft-delete it (its bundles stay 60 days, section 5.2); a renamed slug answers 301 for 30 days |
| API | `pkg/api/sources.go:28, 106-152`, `server.go:654-663, 814` | Archives are content-addressed and never deleted; `maxSourceSize` is 512 MiB; `POST /api/sources` open to any signed-in caller | The archive step writes through `PutContext` with a 2 GiB limit on the internal route; the public upload keeps 512 MiB; an archive garbage collector is a launch prerequisite (section 3.5) |
| API | `pkg/api/logs.go:299-427` `buildLogs`, 444-465 `lineWriter` | Bound to a `gin.ResponseWriter`; 25-second keepalive (386); 30-minute bound (315-319) | Refactored onto an `io.Writer` so the git service can relay it as sideband (section 3.3) |
| API | `pkg/api/audit.go:17-41` `audit` | Events against the App, one-hour TTL | New actions `git.push`, `git.rollback`, `git.refused`, `git.mirror`, `repository.import` (section 3.7) |
| API | `pkg/api/audit.go:81-93` `clientOf` | OAuth callers read as "dashboard" | Client words `git`, `oauth`, `agent` |
| API | `ee/mcp/mcp.go:77-111`, 246, 470-477; `pkg/api/host.go:139` | Four read-only tools; `ServeAPI` is GET-only; no server streams | Later: `read_file`, `list_tree`, `list_versions`, `propose_change`, `deploy_branch`, `build_candidate`, `rollback_version`; a POST seam on `Host` (section 9) |
| Controller | `internal/controller/desired.go:514-528, 599-600`; `dockerfile.go:525-530` | kpack and BuildKit fetch the blob on port 8082 with the capability URL | Unchanged: the pushed commit arrives as a blob |
| Controller | `internal/controller/dockerfile.go:232-251` `buildKey` | Hashes archive SHA (or git URL#commit), subPath, `spec.build`, `AnnotationRebuildAt` | Unchanged. A different archive SHA builds by itself; `AnnotationRebuildAt` is needed only when the target archive equals the current one (C5) |
| Controller | `internal/controller/app_controller.go:794-806` | The same `<sha256>.tgz` name at another URL is not a new source for kpack (#93), only while no rebuild is pending | Unchanged; documented correctly (section 3.8) |
| Controller | `internal/controller/app_controller.go:660-666`, 349-355; `snapshots.go:82-114` | `AnnotationRollbackTo` is consumed when `!HasSource() \|\| Spec.Image != "" \|\| build.Ready == "True"`; `restoreRelease` reruns while it stands | Add consumption on `build.Ready == "False"` so a failed rebuild does not re-apply old config vars every reconcile |
| Controller | `internal/controller/status.go:176-181, 222-230, 236-277` | `sourceID` shortens `Blob.Ref` to 12 characters; `recordRelease` keeps 20 (`status.go:22`) | Release gains `Commit`, `SourceKind`, `BuildNumber`, `RollbackOf`; `Source` keeps its shape |
| Controller | `api/v1alpha1/app_types.go:822-846`; `Makefile:82-83` | Release has no full commit | CRD and deepcopy regenerated |
| Controller | `internal/controller/source_controller.go:311-314, 377-384`; `source_build.go:55-58` | A watched branch re-pins its head within a minute; `status.source` exists only for watched branches | Not used for embedded projects: the commit arrives as a blob, so the watcher is not involved; the detail shows a `repository` block instead |
| Controller | `internal/controller/github.go:252-306`, `gitcreds.go:135-159` | The GitHub App mints `contents:read` tokens into `<app>-git` | The mirror fetch reuses that credential at pin time (section 8) |
| Controller | `internal/controller/workspace_controller.go:289-335, 359`; `deploy/components/shpyrd/base/ingress.yaml:52-78` | Two companion Ingresses per door (`/api/sources` Exact 512m, `/api/project-archives` Prefix 65g); others keep nginx's 1 MiB body | A third companion: `/git` Prefix, 600m body, no request or response buffering, 3600 s timeouts, also on custom-domain and moved-host doors |
| Controller | `internal/controller/quota.go:34-47`; `deploy/components/shpyrd/base/step-priorities.yaml` | `shpyrd-build` and `shpyrd-release` are excluded from the workspace quota | The sandbox pod and candidate builds use `shpyrd-build` (section 9) |
| Install | `deploy/components/<name>/component.yaml`; `pkg/install/types.go:44-106`; `engine.go:320-336` | A component is a Kustomize base plus hooks and waits; a runlevel's components apply in parallel, the server last within its runlevel (C2) | `deploy/components/git` in rc4 on every profile, behind install var `SHPYRD_GIT` |
| Install | `pkg/install/pools.go:44-58`; `deploy/profiles/oci/profile.yaml:124`, `aws:111` | `pool: data` falls back to the platform pool; both cloud profiles ship `SHPYRD_DATA_POOL: ""` | The component says `pool: data`; the pod lands on the platform pool when no data pool exists, and the runbook (section 5.7) says so (C1) |
| Install | `pkg/install/sources.go:14-67`; `engine.go:551-553` | With a sources bucket the server runs 2 replicas, RollingUpdate, nothing on a ReadWriteOnce claim (C2) | The repository disk is its own Deployment, never a claim on the server |
| Install | `pkg/install/hooks.go:66` `RegisterHook`, 571-575 | Secrets with generated data are made by hooks; `shpyrd-backup-key` holds the backup passphrase | Hook `git-internal` makes Secret `git-internal` with a `route` key (the internal routes) and a `capability` key (sandbox capabilities); the pod also mounts `shpyrd-backup-key` and the backup credentials (section 3.2) |
| Install | `deploy/components/shpyrd/base/rbac.yaml:125-127` | The server's ClusterRole covers Leases | Unchanged for the server; the git ServiceAccount gets a Role on Lease `shpyrd-git` alone (section 3.2) |
| Install | `pkg/install/vars.go:233-322` `derivedVars` | `SHPYRD_EXTENSIONS`, bucket settings derived | `SHPYRD_GIT_IMAGE` derived from `SHPYRD_SERVER_IMAGE`; `SHPYRD_GIT_SIZE` (200Gi cloud, 20Gi local) |
| Install | `Dockerfile:44` | Distroless static runtime, no git binary | A fourth stage, `shpyrd-git`, Alpine with the git package (section 3.2); the cloud repository's `deploy.yml` publishes this second image beside the server's |
| Install | `pkg/server/server.go:83-174` subcommand dispatch; 484-504 lease timings | One binary, many subcommands; the server elects on Lease `shpyrd-server` | `git-server` and `git-hook` subcommands; Lease `shpyrd-git` with the same timings |
| Install | `deploy/components/shpyrd/base/networkpolicy.yaml:30-37, 46-61` | 8082 open to project namespaces; 8083 to named internal apps | 8083 gains the git pod by pod selector; later 8090 of the git Service opens to project namespaces for sandbox clones |
| Install | `deploy/components/monitoring/values.yaml:171` | `PlatformBackupFailed`, `PlatformBackupStale` | `GitDiskNearlyFull`, `GitBundleStale`, `GitBundleVerifyFailed`, `GitLeaseLost`, `GitFsckFailed` |
| Storage | `pkg/objectgateway/preflight.go:14-16`, `records.go:84-92`; `deploy/profiles/oci/profile.yaml:74-79` | OCI ignores `If-Match`; the gateway is one pod with `Recreate` and an in-process mutex | Live repositories live on a block disk, not in the bucket, so no ref update depends on `If-Match` (section 4) |
| Storage | `pkg/objectgateway/backend.go:35, 591-592, 622-629`; `records.go:553-600` | A logical bucket is a prefix in one physical bucket; no versioning, no lock; `RetentionDays` is a deletion sweep (C3) | The locked backup copy cannot be a gateway logical bucket; it is a separate physical bucket through the platform backup target (section 5) |
| Storage | `pkg/objectgateway/records.go:351-365` `Allow`, `MaxBytes` | A byte cap per logical bucket, refused in words | Not used: the cap is per project on a disk and enforced in the shim (section 3.3); the gateway's cap is per bucket, not per prefix |
| Storage | `contrib/oci/terraform/backups/main.tf:216-248` | The objects bucket is versioned with previous versions deleted after seven days (commit d3fb0e30) | Stays the fallback undo when no external backup target is set |
| Storage | `pkg/snapshots/snapshots.go:243-248, 259-266, 441`; `pkg/server/snapshots.go:45` | The nightly job snapshots claims in `shpyrd-system` whose `storageClassName` equals `SHPYRD_STORAGE_CLASS`; `sync` inside the pod, crash-consistent | The claim sets `storageClassName` explicitly and conditionally (the `controlplane.go:114` pattern) so it is snapshotted; kind has no snapshots (C1) |
| Storage | `deploy/components/object-storage/base/garage.yaml:33-40, 62-64`; `registry/s3/kustomization.yaml:15-33` | Garage and the filesystem registry are single pods on ReadWriteOnce claims, but only on kind; cloud profiles run both stateless over the gateway (disputed, C1) | The repository disk is stated as a new shape, not as a precedent |
| Storage | `deploy/profiles/local/profile.yaml:35`; `storage-fss`, `storage-efs` classes | No shared class on kind; FSS squashes to root, EFS access points are uid 1000, the server is 65532 | A shared RWX volume is not used for the live repositories (section 4) |
| Backup and export | `pkg/backup/backup.go:37-50, 257, 295-322, 441`; `restore.go:33, 266-297, 343` | One age-encrypted tarball; only each App's current blob SHA travels; `Read` loads everything into memory | Manifest `FormatVersion` 2 with `Repositories`; the archive carries `git/index.json` (newest backup ID and refs checksum per project), not the bundles; a `RestoreRepository` callback |
| Backup and export | `pkg/backup/target.go:18-24, 157`; `pkg/install/vars.go:506-510`; `pkg/install/controlplane.go:204` | An S3 bucket outside the cluster with its own credentials; count-based prune; the default target is the gateway's `platform-backups` logical bucket, and the control-plane Barman backups go to `platform-control-plane`, both in the cluster's region | Bundles go to the target under `git/`, in a separate physical bucket in production; region loss needs the platform backup, the control-plane backups and the passphrase outside the region too (section 5.1) |
| Backup and export | `deploy/components/platform-backup/base/cronjob.yaml:42-47, 67-84` | Fetches archives over 8082; 4Gi scratch; 1Gi memory | Holds, because bundles are not in the nightly tarball |
| Backup and export | `pkg/api/project_archives.go:124-178`; `project_archive_metadata.go:77-90, 261-339`; `project_archive_restore.go:390-433`; `project_archive_runtime.go:383-419` | One `source/source.tgz`; a credential-less helper clone for git sources; a whitelist of entries | `repository/repo.bundle` written before the drain, whitelisted, restored as an asset import; the mirror's bundle replaces the helper clone where a mirror exists, and the helper clone gains the `<app>-git` credential where none does (section 5.4) |
| Backup and export | `pkg/api/workspace.go:878-887`; `pkg/store/store.go:1116-1127` | The workspace export is the store dump; tokens are not in it | `GET /api/workspace/export/repositories` streams a tar of bundles; tokens stay out (open question) |
| Backup and export | `pkg/audit/audit.go:3-5` | Audit entries are Events with a one-hour TTL | The ref journal in Postgres is the durable record of pushes until #306 |
| Backup and export | `content/docs/backups.mdx:16-21, 94, 165`; `apps/shared/src/project-backups.tsx:96-107` | Enumerate what a backup contains | Say the repository is in both |
| Docs | `content/docs/deploying.mdx:37-42`; `shpyrd-yaml.mdx:77-103`; `resources.mdx:49, 147` | The deploy is described as `git archive HEAD` plus an upload; the `shpyrd.yaml` fields are listed; the three copies of a project's bytes are named | Rewritten for people who type git and for people who do not; `shpyrd.yaml` is confirmed unchanged; the repository joins the copies (sections 3.6, 5.4) |
| Dashboard and MCP | `apps/workspace/src/api/types.ts:237-245`; `backend.ts:69-87`; `mock.ts:179-191, 998-1004`; `mock/projects.json:75` | `Release` has no commit; the Mock has releases without commits | `commit` on `Release`; a `repository` address; commits per mock release |
| Dashboard and MCP | `apps/workspace/src/screens/project/releases.tsx:23-180` (83, 149) | Shows `#build` or the 12-character digest and `b.source` revisions | Versions in words; revisions and digests hidden (section 3.8) |
| Dashboard and MCP | `releases.tsx:88-105`, the Rollback button | One rollback, the image pin | Two actions: "Run version 7 again" and "Build version 7 again" |
| Dashboard and MCP | `apps/workspace/src/screens/workspace/platform.tsx:50-213`; `connections.tsx:15` | The MCP page shows the address with Copy and per-assistant setup snippets; Connections lists each client's scope | Coding-tool setup lines (`git remote add shpyrd <url>`, the credential helper line) beside the MCP address; Connections shows when a client holds the code scope |
| Dashboard and MCP | `apps/workspace/src/screens/project/source.tsx:83-162`; `overview.tsx:57-62, 113-122` | The source block is GitHub-shaped | A block for the repository: clone address with Copy (the pattern of `workspace/platform.tsx:103-123`), who pushed last, "connect your coding tool" lines |
| Dashboard and MCP | `apps/workspace/src/screens/project/overview.tsx:176-197`; `access.tsx:459-461` | Marks for deploy, rollback, destroy, access | Marks for `git.push` and `git.rollback` |
| Dashboard and MCP | `ee/mcp/oauth.go:50-54, 333-336, 568, 646-648` | Two scopes; consent text never mentions code; audience `<dashboard>/mcp` | Open question: `projects:write` with extended consent, or a `code:write` scope |

Two things in the table deserve a sentence each. First, the controller does not
change for the first version: a pushed commit reaches the pipeline as an
archive in the sources store, exactly as a CLI upload does today, so kpack, the
BuildKit fetch container, `buildKey`, `sourceID` and `recordRelease` are
untouched (C4). Second, the API grows a Basic-auth path and three routes but
no second authentication system: the Basic password is resolved by the code
that already resolves bearer tokens (C7).

## 3. The design

### 3.1 Architecture, and why this shape

A git service on a block disk, the git binary on the wire behind the
platform's own front door, policy in the platform's process through a hook
shim, incremental bundles in object storage as the durable copy. Four shapes
were weighed: a pure go-git service on a bucket, a git-binary service on a
disk, Gitea as a component, and a bundle-authoritative store with a disk
cache. The second was chosen, for these reasons:

**The wire is C git, not go-git.** go-git v6.0.0-beta.1 served a 211 MB clone
in 230 s where `git http-backend` took 9.2 s (local benchmark, 2026-10-28); a
second run on 2026-10-29 with two repository shapes (223 and 62,040 objects)
found go-git 22 to 40 times slower (162 to 314 s against 4 to 14 s). The
memory half of the first figure did not reproduce: go-git's clone RSS was 0.3
to 1.3 times git's, and go-git was the faster receiver of a push, not slower
(disputed, C9). The decision rests on clone time and on capabilities, not
memory. go-git's receive-pack advertises `no-thin` and does not advertise
`atomic` (`plumbing/transport/serve.go` at v6.0.0-beta.1, both marked TODO;
COMPATIBILITY.md lists `atomic` as supported, but that is the client side), so
every push from a laptop is a full pack and `git push --atomic` fails. go-git
cannot fsck, gc, prune or bundle (COMPATIBILITY.md). It shipped ten security
advisories dated 2026-02-09 to 2026-07-30 in the parsers a push server runs on
untrusted input, and v6 is a beta with a v7 milestone already open. Every Go
forge that tried the pure-Go wire went back to the binary: Gitaly in 2018,
Gitea (native git the default since PR #13673), soft-serve (archived
2026-10-26). The binary costs 6.7 to 7.1 MiB installed (section 3.2).

**The live store is a POSIX disk.** Ref updates are git's lockfile-and-rename.
Nothing depends on OCI honouring `If-Match`, which the object gateway already
works around with a single writer (`pkg/objectgateway/preflight.go:14-16`,
`records.go:84-92`, `deploy/profiles/oci/profile.yaml:74-79`). GitLab refuses
NFS, EFS and Filestore for repositories; the local profile has no shared class
(`deploy/profiles/local/profile.yaml:35`). Every production git host that
documents its practice keeps the hot copy on local disk (section 14). The one
property this gives up, no pause during a roll, is softened at the router
(section 3.3, step 7) and the layout is kept convertible to a bucket-
authoritative store (section 4.2), which is where GitHub and GitLab are
heading and where this platform goes when one disk is no longer enough.

**The build pipeline does not change.** The pushed commit becomes a
content-addressed archive in the sources store (section 3.5).

**It is made of the platform's own patterns**, with one new shape. A hook
registered with `install.RegisterHook` (`pkg/install/hooks.go:66`), the
internal listener on 8083 (`pkg/api/server.go:537`), `mutateApp` as the one
writer of `spec.source` (`pkg/api/apps.go:1309-1369`), the Lease the server
already elects on (`pkg/server/server.go:484-504`), the backup pipeline and
the project archive are all reused. "A single pod on a ReadWriteOnce claim on
the data pool" is not a precedent (disputed, C1): Garage has that shape but is
skipped on every cloud profile (`pkg/install/engine.go:271-273`), the registry
has no `pool:` and goes stateless over the gateway when a registry bucket is
set (`deploy/components/registry/s3/kustomization.yaml:15-33`), and both cloud
profiles ship `SHPYRD_DATA_POOL: ""`. The installer machinery handles the
shape (pool pinning at `engine.go:548`, `poolOf` fallback at `pools.go:44-58`,
snapshot coverage by storage class at `pkg/snapshots/snapshots.go:259-266`);
the repository disk is a new shape on this platform and this document treats
it as one.

**Effort.** Fifteen weeks to a first version (section 11), with the most
deterministic test strategy of the four shapes: a real git client against the
service (section 10).

### 3.2 Components

**The component.** `deploy/components/git/component.yaml`: name `git`,
namespace `shpyrd-system`, `pool: data`, hooks `[git-internal]`, a Kustomize
base, `wait: deployment shpyrd-system/git`, timeout 10m, gated by
`requiredVar: SHPYRD_GIT` so a cluster without the variable installs nothing
(`pkg/install/engine.go:281-283`). It sits in **rc4** beside `platform-backup`
on every profile. A runlevel's components apply in parallel and the platform
server goes last within its runlevel (`pkg/install/engine.go:320-336`, C2), so
a component that needs the control-plane database must not sit in rc3 beside
it. The git pod never opens Postgres itself: the catalog and the journal live
behind the server's internal routes, so there is one database client and no
new label on the control-plane database's NetworkPolicy
(`deploy/components/control-plane-db/base/networkpolicy.yaml:13-19`). On a
cluster without a data pool the pod lands on the platform pool
(`pkg/install/pools.go:44-49`); the runbook (section 5.7) says so.

The Kustomize base, modelled on the gateway's
(`deploy/components/object-gateway/base/gateway.yaml:30-136`):

- ServiceAccount `git`, token mounted (it holds Lease `shpyrd-git`), RBAC
  limited to get, create and update of that Lease.
- Service `git`: `http` 8090 (git over HTTP, from the server pods), `admin`
  8091 (catalog routes, from the server pods), `metrics` 8092 (monitoring).
- Deployment `git`: 1 replica, `strategy: Recreate`,
  `terminationGracePeriodSeconds: 120`, PodDisruptionBudget
  `maxUnavailable: 1` so drains are not blocked. Pod `runAsNonRoot`, uid and
  fsGroup 65532, `RuntimeDefault` seccomp; container read-only root, drop ALL;
  writable mounts `/repos` (the claim), `/run/git` (emptyDir, the hook socket),
  `/tmp` (emptyDir 2Gi, bundle staging). Requests 500m and 1Gi, memory limit
  4Gi, `GOMEMLIMIT` 3GiB.
- PersistentVolumeClaim `git-data` (section 4.1).
- Secrets the pod reads, as environment through `envFrom` or as files:
  `git-internal`, with a `route` key for the server's internal routes and a
  `capability` key for the HMAC capabilities of section 9 (two keys, so a
  capability never doubles as the route credential); `shpyrd-backup-key`, the
  age passphrase the bundler encrypts and decrypts with
  (`pkg/install/hooks.go:571`); `git-backup-write`, the backup target's
  endpoint, bucket and a credential that can only PUT under `git/`;
  `git-backup-read`, a credential that can GET and LIST under `git/`, used by
  the hourly sample restore, the volume-loss replay and `restore-repository`
  (section 5). On the default target, the gateway's `platform-backups` logical
  bucket, the gateway issues one key per bucket
  (`pkg/objectgateway/records.go:253`), so both Secrets hold the same
  credential and the PUT-only split exists only with the separate production
  bucket (section 5.1). The server's edge public key, for the trust header of
  section 3.3, is mounted read-only.
- NetworkPolicy: 8090 and 8091 from pods labelled
  `app.kubernetes.io/name: shpyrd-server` in the system namespace; 8092 from
  the monitoring namespace; later, 8090 from namespaces labelled
  `shpyrd.io/project` for sandbox clones, the shape of the sources rule
  (`deploy/components/shpyrd/base/networkpolicy.yaml:30-37`).
- ServiceMonitor on 8092.

**The image.** A fourth Dockerfile stage, `FROM alpine:<pinned>` with
`apk add --no-cache git`, copying `/out/shpyrd-server`, published as
`shpyrd-git` with the server's tag; the installer derives `SHPYRD_GIT_IMAGE`
from `SHPYRD_SERVER_IMAGE` in `derivedVars` (`pkg/install/vars.go:233-322`),
and the cloud repository's `deploy.yml` publishes it beside the server image.
Alpine's git 2.56.0-r0 (7.1 MiB installed, five shared-library dependencies,
build 2026-10-01) is on Alpine edge, which is not pinnable; the newest stable
branch, v3.24, ships git 2.54.0-r0 at 6.7 MiB (C11). The stage uses the stable
branch. Chainguard's git image was considered and dropped: its free public
tags are `latest` only. The server image stays distroless and git-less
(`Dockerfile:44`). At start the service runs `git version` and refuses
anything below 2.47.0. That floor is Gitaly's (its README), with one scope:
Gitaly's floor is a build-time constraint on the git it embeds, not a runtime
check on an installed package (C11); here it is a runtime check, and it covers
the 2.45.1 fixes for CVE-2024-32002 and its four siblings (github.blog,
2024-05-14). `/hooks/pre-receive` and
`/hooks/post-receive` are symlinks to `/shpyrd-server` baked into the image;
the binary dispatches on `argv[0]` as it already dispatches subcommands
(`pkg/server/server.go:83-174`): `git-server` is the service, `git-hook` the
shim. Git is GPL-2.0-only; shipping the distro package in an image that execs
it is the ordinary arrangement and leaves the MPL core and the ee folder
untouched.

**Process model.** Per request the service spawns git with a fixed argument
list and no shell. For upload-pack:

```
git -c uploadpack.allowFilter=true -c uploadpack.keepAlive=5
    -c pack.threads=2 -c pack.windowMemory=256m
    upload-pack --stateless-rpc <dir>
```

`-c uploadpack.allowAnySHA1InWant=true` is added only for a sandbox capability
request (section 9), which names one commit. A viewer's clone cannot fetch an
unreachable object by its SHA, so a secret removed from history stops being
fetchable as soon as its commit leaves the refs, instead of staying reachable
for the 30-day object grace.

For receive-pack:

```
git -c core.hooksPath=/hooks -c receive.fsckObjects=true
    -c receive.fsck.badTimezone=ignore -c receive.fsck.zeroPaddedFilemode=ignore
    -c receive.fsck.missingSpaceBeforeDate=ignore
    -c receive.fsck.hasDotgit=error -c receive.fsck.gitmodulesSymlink=error
    -c receive.maxInputSize=<min(remaining cap, 600 MiB)>
    -c receive.denyNonFastForwards=true -c receive.advertisePushOptions=true
    -c receive.keepAlive=5 -c receive.unpackLimit=1 -c core.fsync=all
    -c pack.windowMemory=256m -c pack.threads=2
    receive-pack --stateless-rpc <dir>
```

`pack.threads` is set on purpose: git's default is `online_cpus()`, which reads
the node's CPU count and is blind to the pod's CFS quota, so one cold clone can
take more than two cores and multiply delta-window memory by the thread count
(C12: 232 percent of a core measured on 8 cores). `GIT_PROTOCOL` carries the
`Git-Protocol` header so fetches negotiate v2; git's own receive-pack ignores
v2 for push and falls back to v0 (`builtin/receive-pack.c`, the
`case protocol_v2` comment; C8), and so does ours. Gzip request bodies
are inflated under a cap. Stdout is written chunked and flushed per read.
Deadlines: 10 minutes for the git child, 60 s idle on either pipe (Gerrit's
defaults are 4 and 1 minute). A weighted semaphore sized from the memory limit
admits upload-packs and receive-packs; its weights are in section 4.3.

### 3.3 Request path and the write path of a push

1. **Front door.** The workspace controller gains a companion Ingress per
   front door, shared door, custom-domain door and moved-host door
   (`internal/controller/workspace_controller.go:289-335`, `ensureHostFrontDoor`
   at 359; console base `deploy/components/shpyrd/base/ingress.yaml:52-78`):
   path `/git` **Prefix** (the two existing companions are Exact),
   `proxy-body-size 600m`, `proxy-request-buffering off`,
   `proxy-buffering off`, 3600 s read and send timeouts. Three Ingresses per
   door become four. Kubeconfig-only operators have no front door and keep the
   archive upload.
2. **API routes** in `pkg/api` at the engine root, not an ee extension:
   `require()` is not reachable from `api.Host` (`pkg/api/host.go:24-78`), and
   the open-source platform gets the repository. `GET /git/:repo/info/refs`,
   `POST /git/:repo/git-upload-pack`, `POST /git/:repo/git-receive-pack`.
   Middleware in order: tenant (`pkg/api/members.go:341-390`); `gitAuth`
   (section 3.7); a slug middleware that strips `.git` into `c.Params` under
   the name `slug` so `ValidSlug` (`pkg/project/project.go:22-25`) and
   `projectApp` (`pkg/api/apps.go:385`) work; `require(authz.ProjectView)` for
   the upload-pack pair and `require(authz.ProjectDeploy)` for the receive-pack
   pair (`members.go:108-156`, including the 423 maintenance gate); then a
   streaming reverse proxy (`httputil.ReverseProxy`, `FlushInterval -1`) to
   `http://git.<ns>.svc:8090/<project ID>/<service>`. Refusals are
   `text/plain` one-liners so git shows them. `/git/` joins
   `workspaceOnlyPath` (`members.go:757-770`), `machinePath`
   (`pkg/api/hosts.go:179-184`, served in place with
   `X-Shpyrd-Workspace-URL` instead of a redirect that drops credentials) and
   the request logger's prefixes (`pkg/api/server.go:1123`).
3. **Trust between the API and the git service.** The proxy adds
   `X-Shpyrd-Git` = `Host.Sign("git", claims)` (`pkg/api/host.go:66-67,
   130-132`, the EdDSA edge keys; C7) with claims {workspace ID,
   project ID, slug, default branch, cap bytes, remaining bytes, mirrorOf,
   identity {subject, email, name, provider, client, tokenID}, role, request
   ID, expiry 60 s}; it drops inbound `Authorization`, `Cookie` and any inbound
   `X-Shpyrd-Git`. The git service verifies the signature against the server's
   public key (read from the Secret the edge keys live in) and refuses
   anything else; it never reads `Authorization` and never resolves identity.
   The repository directory is derived from the signed project ID, never from
   the URL.
4. **Hook shim.** `/hooks/pre-receive` (the binary) reads the ref lines from
   stdin, connects to `/run/git/hook.sock` and sends the payload git inherited
   in `SHPYRD_HOOK_PAYLOAD` (the verified claims, the request ID,
   `GIT_PUSH_OPTION_n`, `GIT_QUARANTINE_PATH`) plus the commands. The service
   decides in-process (step 3 of the write path below), calls the server's
   `/internal/git/pre-receive` to journal, and the shim prints the decision
   sentence to stderr (band 2, shown as `remote:` lines) and exits non-zero to
   refuse; git then discards the quarantine and nothing lands on disk
   (git-receive-pack, QUARANTINE ENVIRONMENT). `post-receive` follows the same
   path, records the applied push, triggers the build and prints the one-line
   answer. This is the Gitaly and Gitea pattern: the hook binary is a gateway
   to the server process, where the logic runs with the database.
5. **Server-side internal routes** on the internal listener 8083 (its
   NetworkPolicy rule gains the git pod by pod selector,
   `deploy/components/shpyrd/base/networkpolicy.yaml:46-61`), authenticated
   with Secret `git-internal` plus the signed payload:
   `/internal/git/pre-receive` (journal rows with `applied_at NULL`, allow or
   deny), `/internal/git/post-receive` (mark applied, update the catalog, flag
   the row for deploy), `/internal/git/archive` (streams a tar.gz into
   `SourceStore.PutContext` and returns the capability URL, so the signing key
   stays in the server; `PutContext` gains a limit argument, 2 GiB on this
   route so a repository at the maximum cap of section 4.3 still builds, while
   the public upload keeps `maxSourceSize` at 512 MiB, `pkg/api/sources.go:28`,
   which its companion Ingress also bounds; this route is on 8083, behind no
   Ingress), `/internal/git/build-log` (`buildLogs` refactored onto an
   `io.Writer`, `pkg/api/logs.go:360-465`). Admin routes on the git
   service's 8091, called by the server: `PUT /repos/<id>` (init bare, HEAD
   main, marker), `DELETE /repos/<id>` (soft), `GET /repos/<id>/refs`,
   `/tree/<commit>/<path>`, `/blob/<commit>/<path>`, `/log`,
   `POST /repos/<id>/commit` (a set of tree edits as one commit on a branch,
   author the person, committer shpyrd), `POST /repos/<id>/fetch-mirror`,
   `GET /repos/<id>/bundle`, `POST /repos/<id>/restore`,
   `POST /repos/<id>/maintain`.
6. **Lease and handover.** The pod serves only while it holds Lease
   `shpyrd-git` (60 s, 40 s, 5 s as the server's,
   `pkg/server/server.go:484-504`). With a ReadWriteOnce claim there is only
   ever one pod; the Lease is the handover protocol: on SIGTERM the pod stops
   admitting receive-packs, finishes in-flight ones within the grace period,
   flushes the bundle queue, releases the Lease and exits; `Recreate` starts
   the successor, which attaches the disk, runs the recovery sweep and takes
   the Lease. The Lease is also the fence during a node partition: a pod that
   loses the Lease stops admitting receive-packs at once and kills its
   children, the successor cannot attach the ReadWriteOnce device until the
   provider has detached it from the old node, so two writers never hold the
   disk, and the API's proxy sends a request only to the endpoint whose pod
   name is the Lease holder (section 5.3, the partition row).
7. **Holding connections through a handover.** During a planned handover the
   API's proxy, on connection refused or no ready endpoint, holds the client's
   connection and polls the Service's endpoints for up to 90 s before
   answering 503 (git ignores `Retry-After`), so a coding tool sees a slow
   push, not a failed one. The CLI retries a 503 for up to 3 minutes.
8. **Observability.** Metrics on 8092 (requests by service and status, child
   duration and RSS, queue waits, semaphore refusals, bundle lag per
   repository, disk used); alerts `GitDiskNearlyFull` (70 percent),
   `GitBundleStale` (newest bundle older than the newest journal row by more
   than 5 minutes for any repository with pushes), `GitBundleVerifyFailed`,
   `GitLeaseLost`, `GitFsckFailed`, beside `PlatformBackupFailed` and
   `PlatformBackupStale` (`deploy/components/monitoring/values.yaml:171`).

**The write path of `git push shpyrd main`:**

1. `GET /git/shop.git/info/refs?service=git-receive-pack` with
   `Authorization: Basic shpyrd:shp_…` arrives through the companion Ingress
   unbuffered. The API resolves the tenant from the Host
   (`members.go:261-294`), the Basic password through the identifier loop
   (section 3.7), strips `.git`, finds the App by slug, runs
   `require(ProjectDeploy)` (423 during maintenance, plain text), proxies with
   the signed header. The git service verifies the signature, takes the
   repository's mutex for the receive, spawns `git receive-pack
   --stateless-rpc`, which advertises refs (v0) with `report-status
   side-band-64k delete-refs ofs-delta atomic push-options agent`. The client
   must request `report-status` and `side-band-64k` for the rest of this path
   to apply (C8); the acceptance tests assert that the clients the platform
   supports do, and no version floor is claimed here (the first draft's "every
   git since 1.7" had no source and is dropped).
2. `POST /git/shop.git/git-receive-pack` (chunked, maybe gzipped). git writes
   the pack into its quarantine directory, runs index-pack with
   `receive.fsckObjects` and `receive.maxInputSize = min(remaining cap,
   600 MiB)` (an oversize push is cut while bytes still arrive), then runs
   pre-receive. **Nothing has moved yet.**
3. The pre-receive shim hands the commands to the service, which runs git
   with the quarantine as `GIT_OBJECT_DIRECTORY` (the hook inherits it, and
   its alternate is the main store) and: (a) refuses any ref outside
   `refs/heads/*` and `refs/tags/*` or failing check-ref-format (go-git
   CVE-2026-71557 was a ref name used as a path; git's rules plus this
   restriction remove the class); (b) checks fast-forward itself on every
   updated branch and refuses deletion of the default branch, because git
   applies `deny_non_fast_forwards` and `deny_deletes` in `update()` **after**
   the pre-receive hook (functions `execute_commands` and `update` in
   `builtin/receive-pack.c`, cited by function because the line numbers move
   between releases; C8) and the journal must never record a push git later
   refuses. Other branches and tags may be created and deleted, because coding
   tools do that; releases name commits, not tags (`Release.Commit`, section
   3.5), so no tag protects a release, the archive and the 30-day object grace
   do (the first draft protected "tags a kept release names", which nothing
   creates). (c) Caps 1,000 commands and 100,000 objects per push. (d) Reads
   the quarantine pack's index for every new object's type and size (`git
   show-index` on `<quarantine>/pack/*.idx`, or `git verify-pack -v` on that
   pack alone) and refuses any blob over 100 MiB (GitHub's block); then walks
   the trees reachable from the new commits (`git rev-list <new> --not
   <old refs>`, bounded by the object cap, then `git ls-tree` per tree,
   stopping at any tree already in the main store) and refuses any tree with
   more than 5,000 entries or deeper than 60 levels (the 12-object git bomb
   expands to about 10^10 paths, CVE-2017-15298). The first draft used `git
   cat-file --batch-check --batch-all-objects`; that is wrong: it enumerates
   every object in the repository and its alternates (git-cat-file manual), so
   with the quarantine as the object directory it would walk the whole
   repository on every push, and it reports no tree depth. (e) Checks
   `size_bytes + quarantine bytes <= cap` (GitLab's method, Gitaly MR 1263) and
   names the remaining room. (f) Refuses new tree entries on GitLab's
   secret-file list (`id_rsa` and siblings, `*.pem`, `*.key`,
   `aws/credentials`, shell histories), warn-only in the first release. (g)
   Refuses a push that adds a `.gitattributes` line with `filter=lfs` or a blob
   that is a Git LFS pointer (first line `version
   https://git-lfs.github.com/spec/v1`), with "this platform does not host Git
   LFS objects; a pointer file would build a broken application" (Heroku
   refuses LFS too); (h) warns when a push adds `.gitmodules`: `git archive`
   leaves submodule contents out, so the build would silently lack them. (i)
   Calls `/internal/git/pre-receive`, which inserts `ref_journal` rows with
   `applied_at NULL` in one transaction and answers allow or deny. Deny prints
   one sentence as the last line (the lesson from Heroku's and Azure's generic
   rejections) and exits non-zero; git discards the quarantine.
4. git migrates objects out of quarantine, updates refs (all-or-nothing with
   the client's `--atomic`, which the binary advertises and go-git's server
   does not), writes report-status on band 1, then runs post-receive, then
   the final flush (`cmd_receive_pack`: `report`, then
   `run_receive_hook("post-receive")`, then `packet_flush(1)` when sideband is
   in use; C8). A command can still fail after pre-receive, in `update()`, in
   proc-receive or in the ref transaction, so the journal row written in step
   3 is provisional and is completed or failed by step 5 or by the recovery
   sweep (section 3.4).
5. post-receive calls `/internal/git/post-receive` with the commands git
   applied. In one transaction the server marks the rows applied, updates
   `repositories` (head, `size_bytes` from the pack size, `refs_checksum` as
   the XOR of SHA-256(refname, oid), GitHub's incremental ref checksum),
   audits `git.push` with target `<branch> <sha12>` and the commit subject as
   detail (`pkg/api/audit.go:17-41`; `deployDetail` is empty for blob deploys,
   `apps.go:684-696`, so the push entry names the commit itself), and for the
   default branch sets `build_requested` on the row. It does not export the
   archive or run the deploy. The first draft did both inside post-receive and
   said the child "exits within seconds"; it cannot: `git archive` of a 500 MB
   tree (2 GB at the maximum cap) streamed through a hash and a PutObject on
   the single-writer gateway is not seconds, and a gateway restart during a
   platform release would fail the push's post-receive. The pending-deploy
   worker (section 3.4) exports and deploys from the row within seconds of it.
   The service enqueues the bundle (section 5.2). post-receive prints `Version
   13 will build: https://<ws>/projects/shop/releases` (or `nothing was built:
   the project builds main; git push shpyrd <branch>:main deploys it`) and
   exits within a second. git runs post-receive with `skip_broken=1`: it is fed
   only the commands that succeeded and is not run at all when every command
   failed (C8), so the one-line answer for a fully rejected push comes from
   pre-receive's stderr, not from post-receive.
6. If the push carried `-o follow=build` (the CLI sends it), the service has
   held back the child's trailing flush packet while forwarding everything
   else; it now waits for the worker's build to appear (the detail's
   `latestBuild`, as `followBuildAPI` does) and relays the build log from
   `/internal/git/build-log` as band-2 packets with the existing 25-second
   keepalive lines (`logs.go:386`), for at most 30 minutes (`logs.go:315-319`)
   under the 3600 s Ingress, then writes the flush and closes. The git child
   is long gone, which resolves the deadline conflict between a 10-minute
   child and a 30-minute relay. Whether native git 2.4x to 2.5x tolerates
   band-2 lines after report-status while the final flush is held back is
   **unverified** (open question 1); acceptance tests decide, and the CLI
   falls back to `followBuildAPI` (`internal/cli/appclient.go:620-675`), which
   it already has. Git 2.56 adds a `receive-report` hook that runs inside
   `report()` and can write band 2 before post-receive (C8); it is an
   alternative delivery path once that release is in the image.

Deletion of the default branch, a non-fast-forward, an oversize blob, an
over-cap push and a secret file each produce a sentence the client sees as the
last `remote:` line, for example `someone pushed version 14 since your last
pull: run shpyrd deploy again (it pulls first) or git pull`.

### 3.4 Concurrency, atomicity and recovery

One pod mounts one ReadWriteOnce disk; the Lease is the handover protocol.
Inside the pod a per-repository mutex serialises receive-packs and
maintenance; upload-packs run concurrently with each other and with a receive
on the same repository, which is safe because git's object store is
append-only and ref updates are atomic renames. Repack and gc run only from
the maintenance loop (leader-only, one repository at a time, under the mutex;
section 4.4), so a push racing a repack never loses objects; git's own
two-week `gc.pruneExpire` exists for this, and this design uses 30 days.

The journal is written in two steps around git's quarantine: pending rows
before, applied after. git documents that post-receive's exit status is
ignored, so nothing durable depends on post-receive alone. On start, and every
5 minutes on the Lease holder (the shape of the GitHub sweep,
`internal/controller/github_sweep.go:57`), the service asks for rows with
`applied_at NULL` older than 2 minutes and reads the repository's refs
(`git for-each-ref`). A row is applied when the ref equals `new_oid` or has
moved past it, which `git merge-base --is-ancestor <new_oid> <ref>` answers
(a later push landed first and superseded this one; the next journal row's
`old_oid` says the same); the post-receive work runs for it. A row is failed
when the ref still equals `old_oid` (the quarantine died with a crash, or
`update()` refused after pre-receive). Anything else (a branch deleted
meanwhile) is marked failed and flagged for the nightly reconciliation. The
first draft had only "equals `new_oid`" and "unchanged", which misfiles a
superseded push as failed.

The pending-deploy worker runs on the Lease holder every 5 s over rows with
`build_requested` and `deploy_done_at NULL`: it asks the service for the
archive of the commit (section 3.5), streams it to `/internal/git/archive`,
calls `deployCommit` through `mutateApp`, and sets `deploy_done_at`. A failure
(the gateway restarting, `mutateApp` refused, the API down, conflicts
exhausted) leaves the row and retries with backoff up to 30 s; the nightly
reconciliation flags rows pending for over an hour (section 4.6). A durable
push is therefore never silently unbuilt, and a push never waits for the
archive.

Two writers of the App are avoided: the hook never writes `spec.source`; the
server's post-receive handler is the only writer, through `mutateApp`
(`pkg/api/apps.go:1309-1369`) with admission, maintenance and conflict
retries, exactly like `deployApp`. The source watcher is not involved
(`gitauth.Watched` is false for blob sources, `pkg/gitauth/watched.go:8-14`).

Two clones pushing the same branch: the second is a non-fast-forward, refused
in the shim with the sentence above; merging happens in clones (section 3.6).
The agent is a second writer by decision; its push capability is bound to the
expected old oid (section 9), so a human push in between makes the agent's
push "fetch first", never a force.

**Bytes.** `core.fsync=all` makes git fsync packs, indexes and refs before
answering. The block volume is provider-replicated inside the availability
domain or zone. The second copy is the incremental bundle, enqueued in
post-receive and uploaded within seconds (pushes within one minute coalesce
into one increment; retried with backoff; never acknowledged to the pusher).
The third copy is the nightly disk snapshot on cloud profiles. The honest
statement: a push is acknowledged when it is on the disk and before it is in
the bucket, so the RPO on volume loss is seconds to about a minute, not zero.
The two bucket-authoritative designs reach zero at the price of serving from a
bucket. The gateway runs the same server image as one `Recreate` pod on OCI
and restarts on every platform release; in this design that only delays a
bundle upload, while in the bucket-authoritative designs it fails pushes.

### 3.5 Build integration

Two ways to feed a pushed commit to the pipeline were weighed.

**(a) A git source with credentials** would reuse `desiredKpackImage`'s git
branch (`internal/controller/desired.go:590-598`) and the BuildKit clone script
(`dockerfile.go:560-573`), but needs a new credential kind in
`lookupGitBuild` (`internal/controller/gitcreds.go:135-159`; a workspace-wide
token Secret would let any project's build clone any repository of the
workspace, `pkg/gitauth/resolve.go:62`), upload-pack served to project
namespaces (`networkpolicy.yaml:30-37`), kpack's go-git fetcher (v5.16.4 plus a
v6 pseudo-version, `Depth 1`) exposed to a repository a developer-role user can
shape, and "roll back to a version builds that commit again" fights
`waitingCommit` and the resume case
(`internal/controller/source_controller.go:311-314, 377-384`), which re-pin
the branch head within a minute. A fourth argument, that kpack re-fetches
sources on every stack or buildpack update (the comment at
`deploy/components/shpyrd/base/storage.yaml:1-3`), is refuted for the stack
half (C6): kpack v0.18.0 rebases without fetching when only the run image
moves under an unchanged stack id (`pkg/apis/build/v1alpha2/build.go`,
`rebasable`; `build_pod.go`, `rebasePod`), and re-fetches on buildpack,
lifecycle, config, commit and trigger rebuilds. The fleet-wide re-fetch load a
single git pod would carry comes from buildpack and lifecycle rollouts, not
from run-image patches. The comment in `storage.yaml` should be corrected. The
other three arguments against (a) stand.

**(b) The commit exported as an archive**, chosen. After a default-branch
push, the pending-deploy worker (section 3.4), not post-receive, asks the
service for `git archive --format=tar.gz <sha>` (with the Aptfile translation
of addition 3 applied) and streams it to `/internal/git/archive`, which calls
`SourceStore.PutContext` with a 2 GiB limit (`pkg/api/sources.go:106-152`:
content-addressed, bucket or `/data`, capability URL at 64-77; the public
upload keeps `maxSourceSize` at 512 MiB, line 28) and then
`deployCommit(project, commit, identity, note)`, which sets `spec.image = ""`
and `spec.source = {Blob{URL, SHA256, Ref: <full 40-hex commit>}, SubPath
kept}` and stamps release-note, deploy-requested and deployed-by, subject and
client exactly as `deployApp`'s blob branch does (`pkg/api/apps.go:857-859,
912-931`; C4), through `mutateApp`. Downstream nothing changes: kpack gets
`source.blob` with
the port rewritten to 8082 (`desired.go:514-528, 599-600`); the BuildKit fetch
container wgets and untars it and writes `Blob.Ref` as the revision
(`dockerfile.go:525-530`); `buildKey` hashes the archive SHA
(`dockerfile.go:232-251`); `sourceID` shortens `Ref` to the 12-character
commit for `Release.Source` and `REVISION` (`internal/controller/status.go:
176-181, 214-230`); `recordRelease` uses the note (`status.go:236-277`);
`auditReleaseOutcome` attributes `release.succeeded` to the pusher
(`internal/controller/app_controller.go:1277-1330`). `git archive` keeps
today's CLI semantics (export-ignore honoured, submodule contents absent,
`internal/cli/deploy.go:437`). The archive is the release's own authority for
what was built, with the commit as the pointer; a release stays rebuildable
and runnable even if the repository lost or rewrote the commit, which is what
the disaster-recovery research recommends (section 5).

Three additions make (b) complete:

1. **Release fields.** `Release` gains `Commit` (full SHA), `SourceKind`
   (`commit`, `archive` or `image`), `BuildNumber` and `RollbackOf` beside the
   12-character `Source` (`api/v1alpha1/app_types.go:822-846`; CRD and
   deepcopy regenerated, `Makefile:82-83`). `Source` keeps its shape so
   `releaseIs` and `commitSource` (`internal/controller/github_report.go:
   552-555`) and `releaseKind` (`pkg/api/apps.go:220-231`) are untouched;
   `ReleaseView` exposes `commit` (`apps.go:208-231`); the dashboard's
   `Release` type gains it (`apps/workspace/src/api/types.ts:237-245`).
   `BuildNumber` is recorded because today it is derived from kpack Builds by
   digest and lost once kpack's history of 10 or the Job pruning drops them
   (`apps.go:296`, `logs.go:470-482`).
2. **Rollback by rebuild.** Section 3.8.
3. **Aptfile server-side.** `withSystemPackages(archive, pkg)`
   (`internal/cli/aptfile.go:59-75`, `deploy.go:239-255`) moves to a shared
   package the archive step applies, setting `Build.SystemPackages`, so a
   committed Aptfile keeps working for kpack builds with no project.toml in
   the repository. The server-side step reads the workspace package path from
   `spec.build.workspace`, so `<package>/Aptfile` is found as today (the
   package's Aptfile first, the root's after). The promise at `aptfile.go:12-17`
   is kept: the repository is untouched, the archive is rewritten.

**Archive garbage collector (prerequisite).** Nothing deletes archives today
and (b) creates one per built commit. A leader-only sweep keeps every archive
named by any App's current `spec.source.blob` or by any kept release
(`status.releases[].Commit`, or a `Release.ArchiveSHA` field; `maxReleases` is
20, `status.go:22`) and deletes the rest after a 30-day grace. It is the one
new deletion path in the platform; it is keyed to kept releases so "build
version 7 again" works even if the repository were lost. Whether it should also
apply to blob deploys made before this feature is an open question (section
12).

Other branches and tags build nothing (decision) and are journalled; the push
answer says so. Server-side commits (dashboard or API upload of a tree,
restore of an archive, kubeconfig deploys): `deployApp`, for a
repository-backed project, hands the uploaded archive to the git service,
which builds tree and commit objects (`git hash-object -w`, `mktree`,
`commit-tree`, `update-ref` under a temporary index; Author the signed-in
person from `ext.IdentityFrom`, `pkg/ext/ext.go:383-414`, fallback
`shpyrd <noreply@<domain>>` for the admin token; Committer `shpyrd`), commits
it on top of `main` and proceeds with that archive; so every build is a commit
(open question 2: always). That commit replaces the whole tree with the
upload, so a stale folder would silently revert what the agent or another
person pushed since. The check: when the upload carries `Blob.Ref` (the
laptop's HEAD when the folder is a clone, `internal/cli/deploy.go:416`) and
that commit is in the repository but behind `main`, the deploy is refused with
the versions in between ("versions 14 and 15 were pushed after the commit this
folder is at: `shpyrd pull` first, or pass `--replace`"); when `Blob.Ref` is
empty (a plain folder, or a commit the repository does not have) the deploy
proceeds and the answer says the upload replaced the whole tree, naming the
versions it replaced. `status.source` and `SourceReady` exist only for watched
branches (`internal/controller/source_build.go:55-58`); an embedded project
shows a `repository` block from the catalog instead.

### 3.6 The CLI on a plain folder and on an existing repository

**Library decision:** go-git v5.19.x (the stable line) on the laptop, pinned
and bumped monthly; the git binary is used only when present and only where
go-git cannot do the job. Today the CLI's git knowledge is `exec.Command("git")`
with no presence check, and a missing binary silently degrades to tarring the
folder (`internal/cli/deploy.go:410-414, 451-460`); the people this is for may
have no git binary, so the CLI must not depend on one. go-git on the laptop is
the right side of the measured gap: it is a client there and never serves
packs. The one-commit push measured at about 0.1 s was native git pushing into
a go-git server (C9), which says nothing about go-git as a client; no client
benchmark was run (unverified). The exec archive path is removed from
`deploy.go`. Binary sizes measured so far:

| Binary | Stripped size |
|---|---|
| hello world, go1.27.1 | 1.6 MB |
| smart-HTTP server on go-git v6.0.0-beta.1 | 7.9 MB |
| CLI with the go-git v5.19.x client side | not measured; the server-side 6 MB above is the ceiling expected |

**Plain folder, first deploy** (`shpyrd deploy`, optionally
`--commit "first version"`): resolve the project (`deploy.go:87`,
`appclient.go:382`), read the App (`deploy.go:104`), run the local detections
as today (`workspaceFor`, `detectDockerfile`, `applyProfiles`,
`sourceRuntime`, `deploy.go:126-208`; they stay valid on a clone). Then:

- Write a `.gitignore` if none exists (`node_modules/`, `.env`, `.env.*`,
  `*.log`, `.DS_Store`, plus the build output `applyProfiles` recognises,
  `internal/cli/profiles.go:171-250`) and print what was excluded. Today
  `tarDirectory` skips only `.git` and `node_modules` and never reads
  `.gitignore` (`deploy.go:463-514`), so the first commit must not carry
  `.env` files.
- Refuse regular files over 100 MiB with the file name and size; warn on the
  secret-file names.
- `PlainInit` with default branch `main`; add everything; commit with Author
  the signed-in person (name and email from `GET /api/me`,
  `pkg/api/auth.go:844`; the admin token has no email and is refused for this
  flow) and Committer the same person, message from `--commit` or
  `Deploy from <folder name>`.
- Add remote `shpyrd` = `repository.url` from the App detail (computed from
  the door the request came through, as `hookURL` does,
  `pkg/api/githooks.go:109`).
- Write `credential.https://<host>.helper = !shpyrd git-credential` into the
  clone's `.git/config` (local, not global) so a coding tool's own git can
  push later.
- Push `main` with Basic auth (username `shpyrd`, password the session token,
  `internal/cli/registry.go:251`) and push option `follow=build`; then
  `waitRunningAPI` for the release (`appclient.go:500-560`) and print
  `Released vN` as today (`deploy.go:321-328`).

JavaScript workspaces: the repository root is the workspace root, as
`workspaceFor` already decides (`internal/cli/jsworkspace.go:143`). Before
pushing, the CLI checks the two documented causes of Heroku's "Everything
up-to-date" confusion, uncommitted changes and the wrong branch, and names
them.

**Existing clone** (the folder has `.git`): add the remote `shpyrd` if missing.
A dirty tree is refused with "there are uncommitted changes: commit them, or
pass `--commit "message"` to commit everything now" (this replaces
`--working-tree` and the `-dirty` ref, `deploy.go:421-427`;
`status.go:222-225` keeps accepting the suffix for old releases). Then fetch
`shpyrd/main`. go-git merges fast-forward only and has no rebase
(COMPATIBILITY.md at v5.19.3 and on main: `merge` partial, "Fast-forward
only"; `pull` "Only supports merges where the merge can be resolved as a
fast-forward"; `rebase` unsupported; C10), and even its fast-forward
`Merge` only moves the ref without touching the index or working tree, so the
CLI runs a checkout afterwards. Therefore: if the local branch is behind and
can fast-forward, do it; if the histories diverged and a git binary is on PATH,
run `git merge --no-edit shpyrd/main` and report conflicts by file; without a
binary, refuse with the files changed on both sides named and the sentence
"your coding tool has to merge the platform's changes first (`git pull shpyrd
main`)"; later the agent is offered the merge (section 9). go-git PR #2542
(opened 2026-10-26, open) adds in-core three-way tree merges for v6; when it
lands it supplies the tree-merge primitive, and the CLI would still write the
merge commit and check out the result itself. Push HEAD to `main`; the server's
fast-forward check is the backstop.

**Other commands**, registered in `internal/cli/root.go:124-167` beside
`git-auth`, `git-hooks` and `github`:

- `shpyrd pull`: go-git fetch and fast-forward of `main`; refuses otherwise in
  words. This is how the person who never types git gets the agent's fixes
  before editing again.
- `shpyrd git url [--project] [--set]`: prints the clone URL and the two lines
  for a coding tool's git (`git remote add shpyrd <url>`, the credential
  helper line), or writes them.
- `shpyrd git-credential get|store|erase`: git's credential helper protocol on
  stdin and stdout. `protocol=https host=<workspace host>` answers
  `username=shpyrd`, `password=SessionFor(https://host)`
  (`internal/cli/login.go:105-161`), `password_expiry_utc` from the session's
  `ExpiresAt`; `store` and `erase` are no-ops because `sessions.json`, mode
  0600, is the store (`login.go:101`); on a missing session, "run
  `shpyrd login`" to stderr and `quit=true`. Never `git-credential-store`,
  which git's own manual discourages.
- `shpyrd deploy --commit "message"`: commit then push; the message becomes
  the release note through push option `message=…`.
- `shpyrd releases` in version words, the commit only under `--json`
  (`internal/cli/appcmds.go:436-480`); `shpyrd rollback --rebuild`
  (`appcmds.go:482-607`); `shpyrd projects info` likewise
  (`internal/cli/apps.go:529-537`).
- For other computers and CI: `shpyrd tokens create <name> --project X --role
  developer --expires 90d` (`login.go:663`), kept through the OS keychain
  helpers.

Kubeconfig-only operators (`--context`, `registry.go:199-237`) keep the
archive upload (`appclient.go:677-687`). With the exec `git archive` path
gone, the archive is produced by go-git: the tree of HEAD exported as tar.gz
with export-ignore honoured, `Blob.Ref` the full commit; for a plain folder,
`tarDirectory` with `.gitignore` honoured (today it reads none,
`deploy.go:463-514`) and an empty `Blob.Ref`. The server commits the upload
with the `Blob.Ref` check of section 3.5, so the same code path receives a
kubeconfig deploy, an old CLI's upload and a dashboard upload. **Build
following:** with `follow=build` the log arrives on the push and
`followBuildAPI` is skipped; after a 503 handover or `--no-wait` it resumes
from the detail's `latestBuild` as today (`appclient.go:620-675`).
`shpyrd.yaml` gains nothing: the remote lives in `.git/config` and `project:`
stays the pointer (`appclient.go:104-225`); a repository URL in `shpyrd.yaml`
would duplicate the remote and break when a workspace address moves. Docs:
`content/docs/deploying.mdx:37-42` rewritten; a page for people who type git.

### 3.7 API and auth

**Public surface** at every workspace host, custom domain and shared door:
`GET /git/<slug>.git/info/refs?service=…`,
`POST /git/<slug>.git/git-upload-pack`,
`POST /git/<slug>.git/git-receive-pack`. Answers follow gitprotocol-http: the
advertisement content types, no-cache; refusals plain text; never 200 for a
repository that does not exist.

**Credentials as the Basic password**, any non-empty username. `auth()`
itself cannot be the middleware (C7): its `abort()` is `c.AbortWithStatusJSON`
(`pkg/api/server.go:1144-1146`), which writes the Bearer challenge at once,
and git never sends Basic to a Bearer-only challenge (tested locally with git
2.50.1). So `gitAuth` reads `c.Request.BasicAuth()`,
sets the password as `X-Shpyrd-Token` on the request (the header `bearerOf`
prefers, `pkg/api/tokens.go:66-74`, which leaves the client's `Authorization:
Basic` intact), calls `s.identifyWithToken(c)` and ranges over
`s.identifiers` itself (`server.go:296, 1025-1049`), compares the admin token
after the loop and refuses it (below), and when nothing identifies the caller
answers `401` with `WWW-Authenticate: Basic realm="shpyrd"`. Through that
path `resolveAPIToken` (`tokens.go:104-205`: hash lookup, bound to the
workspace it was created in at 174-175, owner active at 182-184, roles
intersected at 136-155; kind `session` tokens from `shpyrd login`, 30 days,
act as the owner, `pkg/api/clilogin.go:35`) and the licensed OAuth identifier
(`ee/mcp/oauth.go:614-653`, registered through `AddIdentifier` while licensed,
`ee/mcp/extension.go:72`) work unchanged. The workspace binding resolves from
the door (`members.go:261-320`), so the git URL must be at the workspace's own
host; at the console host every token is refused at `tokens.go:174`, which is
the intended answer. In open mode (`opts.Token == ""` and not
`TokenDisabled`, `server.go:1019`) an unidentified caller becomes admin
through `rolesOf` (`members.go:38-42`) and has no author identity, and the
admin token is refused on `/git` for the same reason (below); so `/git`
refuses in open mode with "sign in: a push needs an author", and the CLI's
first deploy says the same. The development cluster runs with sign-in (the
`auth-local` extension), as the acceptance tests do.

Two changes: OAuth tokens must name a git audience or carry `projects:write`
for receive-pack (today only the `ws` claim is checked, `oauth.go:568, 629`;
`readOnlyRoles` narrows `projects:read` to clone, `oauth.go:659`), and the
admin token is refused on `/git` ("the admin token cannot push code: sign in
or create a token"; it is a platform admin everywhere,
`pkg/authz/authz.go:250-253`, and has no author identity). Failures spend
`s.tokenFailures` per IP (`server.go:426, 1053-1058`; the counter is in the
API's memory, per replica), including when the admin token is disabled
(closing the gap at `server.go:1045-1049`), are audited `auth.token_failed`,
and answer `401` with the Basic challenge.

**401 and 404.** No credential: 401 Basic for every slug, so viewers' clones
prompt. A valid credential without `ProjectView`: 404 plain text, so project
existence never leaks across tenants (the webhook already answers unknown and
unauthorised alike, `pkg/api/githooks.go`, `refuseHook`).

**Authorization.** `require(ProjectView)` for the upload-pack pair,
`require(ProjectDeploy)` for the receive-pack pair (`authz.go:45-58`: viewer
clones, developer and admin push, user and reader nothing; workspace owners
and platform admins push everywhere, `authz.go:99-111`). Token project scoping
applies unchanged because `require` reads `shpyrd.roles`
(`members.go:31-49`). The slug must sit under the param name exactly `slug`,
so the middleware mutates `c.Params[i].Value` from `shop.git` to `shop`
before `require` runs; gin's `AddParam` appends and `ByName` returns the first
match, so it cannot override an existing segment (C7). Maintenance:
receive-pack 423, upload-pack allowed. Suspended workspace
(`members.go:368-386`): upload-pack allowed for 30 days after suspension so a
company that leaves takes its code, receive-pack 403 (a product decision to
confirm, section 13). Mirrors: receive-pack 403 "this project's code lives at
<github url>; pushes go there".

**Renames and moves.** The URL uses the slug; the disk uses the ID
(`pkg/project/project.go:169, 186`; `renameApp`, `pkg/api/apps.go:1416`). A
renamed slug answers `301` to the new URL for 30 days while the old slug is
free, then 404 with a sentence. git follows the redirect for that one session
and never rewrites the remote, so every push and fetch redirects again until
the remote changes (the first draft said git "rewrites its base URL", which is
true only for the session). The CLI rewrites the remote when it sees the 301;
a coding tool's clone is fixed by `shpyrd git url --set`, and the 301's
sentence says so. Moved workspace addresses are served in place through
`machinePath`; `shpyrd git url --set` rewrites the remote there too.

**Project API additions** (`pkg/api/apps.go`): `detail().repository {url,
defaultBranch, head, headSubject, lastPushAt, lastPushBy, sizeBytes, capBytes,
mirrorOf}`; `GET /api/projects/:slug/commits` (ProjectView, words for the
dashboard); `POST /api/projects/:slug/deploy` accepts `{commit}`
(ProjectDeploy) = `deployCommit`; `POST /api/projects/:slug/rollback {release,
rebuild: true}`; `GET /api/projects/:slug/repository/files?path=&ref=` and
`/tree?ref=` (ProjectView, bounded); `POST /api/projects/:slug/repository/
changes` (ProjectDeploy: a set of file edits committed as one commit on a
branch, author the person, committer shpyrd, trailers) for `propose_change`
and import; `POST /api/projects/:slug/repository/start-from-running`
(ProjectDeploy, admin-gated, only on an empty repository);
`GET /api/projects/:slug/repository/bundle` (ProjectDestroy, like exports);
`PATCH /api/projects/:slug/repository {cap, defaultBranch}`
(ProjectResource). `DELETE /api/projects/:slug` soft-deletes the repository
with the project; its bundles stay in the backup bucket until retention ends
(section 5.2), and the answer says so. `DeployRequest.Note` already carries
the release description (`apps.go:724-725`).

**Rate limits and quotas.** Two limits live in the API, where the credential
is seen: failed Basic attempts spend `s.tokenFailures` (20 a minute per IP,
`pkg/api/server.go:426`; `rateLimiter` at `auth.go:937-999`), and info/refs
is capped at 60 a minute per IP per repository. Both are per replica in
memory, so on the cloud's two replicas the effective budget is double; that
is accepted for the first version (the alternative is a counter in the
store). The push limit lives in the git service, one process: 10
receive-packs a minute per actor per project, the actor being the subject and
provider carried in the signed header (the `actorKey` pattern of
`pkg/api/shellstate.go:223`, `pkg/api/shell.go:116`). The first draft placed
all three in the git service, which never sees `Authorization`. Concurrency:
1 receive-pack and 4 upload-packs per repository, the weighted global
semaphore (section 4.3). Ingress body 600m is the hard pack cap and
`receive.maxInputSize` the quota cap. Whether 10 pushes a minute is right for
coding tools that commit after every action is an open question; GitHub
recommends 6 pushes a minute per repository and reports agents committing
after nearly every action.

**Audit.** `git.push` (journal row plus Event through `s.audit` with target
`<branch> <sha12>`), `git.rollback`, `git.refused` (quota, non-fast-forward,
policy), `git.mirror`, `repository.import`; clones and fetches as log lines
with actor and bytes (GitHub and GitLab both keep per-operation git events out
of the primary store); `clientOf` gains `git`, `oauth` and `agent` so
assistants stop reading as "dashboard" (`pkg/api/audit.go:81-93`); never the
token, only its ID. Events keep the one-hour TTL; the journal is the durable
history until #306.

### 3.8 The versions timeline and rollback

The dashboard shows versions, never branches or commits. A release is
"Version 13, Fix the sign-in error, by ada through Claude, 2 hours ago": the
number from `Release.Number`, the words from the release note (the commit
subject for a push, `--commit` for the CLI, "Back to version 7" for a
rollback), the person from `AnnotationDeployedBy` carried into the release's
audit, the client from the new `clientOf` words. The 12-character revisions
and digests the Releases page shows today (`apps/workspace/src/screens/
project/releases.tsx:83, 149`) are hidden; the commit lives behind `--json`
and in the API's `commit` field. Whether they are hidden for every project or
only for embedded ones is for the design session (section 13).

Two rollbacks, worded apart:

- **"Run version 7 again"**: the image pin that exists today (`rollbackApp`,
  `pkg/api/apps.go:1116-1149`): fast, byte-identical, limited to the 20 kept
  releases whose images are kept (`internal/controller/registry.go:69-94`).
- **"Build version 7 again"**: `POST /api/projects/:slug/rollback {release,
  rebuild: true}` = `deployCommit(release.Commit, note "Back to version 7")`
  plus `AnnotationRollbackTo` to restore the config snapshot
  (`internal/controller/snapshots.go`), with `RollbackOf` on the new release.
  It reaches releases older than the kept history, as long as the commit is
  in the repository or its archive is in the sources store.

Rollback by rebuild does not need `AnnotationRebuildAt` in the common case;
the claim that "the BuildKit build key changes only through
`AnnotationRebuildAt`" is refuted (C5). `buildKey`
(`internal/controller/dockerfile.go:232-251`) hashes the archive
SHA (or the git URL plus the pinned commit), `subPath`, the whole `spec.build`
and `AnnotationRebuildAt`; a new archive SHA is a new Job
(`dockerfile.go:59-65`). On the kpack side, the "same `<sha256>.tgz` name at
another URL is not a new source" rule (`app_controller.go:794-806`, #93)
applies only while no rebuild is pending, and a changed blob URL is a spec
change that builds by itself. So a rollback to a version whose commit differs
from the current source re-exports a different archive and builds on its own.
`AnnotationRebuildAt` is needed only when the target commit's archive equals
the current `spec.source.blob` SHA (a config-only version, or a rollback to
the version that is already the source); `deployCommit` sets it in that case,
and setting it in every case is harmless.

One controller fix is needed: `AnnotationRollbackTo` is consumed only when
`!app.HasSource() || app.Spec.Image != "" || build.Ready == "True"`
(`app_controller.go:664-666`). Every rollback today sets `spec.image`
(`apps.go:1134`), so the annotation is always consumed on the next reconcile.
A rollback by rebuild leaves `spec.image` empty, so a failed rebuild would
leave `AnnotationRollbackTo` standing and `restoreRelease`
(`internal/controller/snapshots.go:82-114`) would rewrite the live env Secret
from the snapshot on every reconcile (`app_controller.go:349-355`) while
`pendingConfig` parks drafts (`pending_config.go:26-37`). The controller
therefore also consumes the annotation when `build.Ready == "False"`, and
keeps `RollbackOf` on the release so the timeline can say what was attempted.

## 4. How to store the code properly

### 4.1 The store on each profile

The live repositories live on one block claim, `git-data` in `shpyrd-system`:
`ReadWriteOnce`, size `${SHPYRD_GIT_SIZE}` (default 200Gi on the cloud
profiles, 20Gi local), `storageClassName` set to `${SHPYRD_STORAGE_CLASS}`
**conditionally**, the way the control-plane database does it
(`pkg/install/controlplane.go:114`): on kind `SHPYRD_STORAGE_CLASS` is empty
and `storageClassName: ""` would mean static binding, so the field is omitted
there and the cluster default class applies. The condition matters because the
nightly snapshot job takes a claim only when its namespace is `shpyrd-system`
or carries the project label and its `storageClassName` equals
`SHPYRD_STORAGE_CLASS` (`pkg/snapshots/snapshots.go:243-248, 259-266`); the
Garage and registry claims name no class and ride on the cluster default
equalling the profile's class (C1). The repository claim names it explicitly.

| Profile | Class | Minimum | Snapshots | Data pool | Durable copy |
|---|---|---|---|---|---|
| local (kind) | cluster default, local-path (`deploy/components/storage-local`, WaitForFirstConsumer) | none | none (`deploy/profiles/local/profile.yaml:60`; `platform-snapshots` absent from the runlevels, 94-102) | none; the pod has no node selector | bundles to the local backup target (Garage); the only copy besides the disk |
| OCI | `oci-bv`, expansion allowed (`deploy/profiles/oci/profile.yaml:59`) | 50 GiB | `oci-bv-backup`, nightly, keep 7 (`profile.yaml:95`) | optional; `SHPYRD_DATA_POOL: ""` shipped (`profile.yaml:124`), set by Terraform only when `data_max_count > 0` (`contrib/oci/terraform/cluster.tf:199-223`) | bundles to the backup target; separate physical bucket in production (section 5) |
| AWS | `gp3`, encrypted, expansion allowed (`deploy/profiles/aws/profile.yaml:55`) | 1 GiB | `ebs-snapshot`, nightly, keep 7 (`profile.yaml:87`) | optional; `SHPYRD_DATA_POOL: ""` shipped (`profile.yaml:111`); the AWS Terraform labels a platform pool only (`contrib/aws/terraform/cluster.tf:168`) | bundles to the backup target |

Block storage was chosen over the two alternatives for these reasons:

- **Object storage as the live store** (the issue's default for open question
  1). Every documented attempt to read individual git objects from a bucket
  failed on latency and request storms: JGit's per-object DHT layer was
  abandoned in 2011-2012 (a Linux kernel clone needed about 2 million row
  lookups); objgit (2026) measured a 100,000-object push turning into about
  200,000 storage calls and a 318-object clone into over 8,500 GETs until packs
  and listings were cached; S3's own guidance is 100 to 200 ms first-byte
  latency per object. The designs that make a bucket authoritative (Netlify
  2026, GitHub's rebuild 2026, GitLab's 2026 proposal) all serve from a local
  disk cache with one coordinated writer; that is the next step for this
  platform (section 4.2), not the first version. On OCI, the conditional
  update a bucket-authoritative ref store needs is the operation the gateway
  already cannot rely on; the OCI native API documents `if-match` on
  PutObject, which conflicts with the platform's observation that the S3
  compatibility endpoint ignores it, and that discrepancy is **unverified**
  until a direct test (section 13).
- **A shared RWX volume** (shared volumes, `git show
  50231629:rfcs/0041-shared-volumes.md`). GitLab refuses NFS, EFS and Filestore for
  repositories and documents pushes that are not seen uniformly across NFS
  clients; the local profile has no shared class (`profile.yaml:35`); FSS
  squashes every client to root and EFS access points are uid 1000 while the
  service runs as 65532 (`deploy/components/storage-fss`,
  `storage-efs`); at 0.30 USD per GB-month on both clouds it costs twelve
  times the object store and seven times balanced block; and with one writer
  per repository it adds nothing to a single-writer service. Bitbucket Data
  Center is the one product that runs git on NFSv3, and it requires a
  dedicated SAN-class machine in the same data centre.

The service watches `statfs`: at 70 percent it asks the server to patch the
claim up by 50 percent (local-path cannot expand, so the local profile alerts
instead); at 95 percent it refuses pushes with "the platform's code store is
full; the operator has been told", so one tenant filling the disk cannot
corrupt everyone's repositories. This rule, not a fixed multiple, is what the
cost tables of section 6.3 provision the disk by. Whether the 70 percent
expansion needs an operator gate on OCI's 50 GiB minimum class is an open
question.

### 4.2 Layout, and why it stays convertible

Under `/repos`, owned by 65532:

- `/repos/v1/<2 hex of sha256(project ID)>/<project ID>.git`: a bare
  repository (`git init --bare --initial-branch=main`), 256 shards, about 40
  entries each at 10,000 projects. The project ID is `spec.ID`, stable through
  renames (`pkg/project/project.go:169, 186`); the slug lives only in the URL
  and the catalog.
- `<repo>/shpyrd.json`: {workspaceID, projectID, slugAtCreation, createdAt,
  capBytes, kind: `own` or `mirror-of <url>`}, so a disk or bundle restored
  without the database is self-describing.
- `<repo>/config`, written at init and asserted on every open:
  `core.logAllRefUpdates=always` (a bare repository has no reflog by default,
  `Documentation/config/core.adoc`), `core.fsync=all`, `gc.auto=0`,
  `gc.pruneExpire=30.days.ago`, `gc.reflogExpire=90.days`,
  `receive.denyNonFastForwards=true`, `repack.writeBitmaps=true`,
  `core.hooksPath=/hooks` (also passed per invocation, so a repository's own
  hooks directory never runs). `uploadpack.allowAnySHA1InWant` is not in the
  config; it is passed per invocation for sandbox capability requests only
  (section 3.2).
- `/repos/.deleted/<project ID>.git.<unix ts>`: soft-deleted repositories,
  purged 30 days after the move.
- `/repos/.state/`: last maintenance and last bundle per repository, the
  recovery marker. Nothing here is authoritative.

`receive.unpackLimit=1` makes every push one pack plus one index, never loose
objects (git's default `transfer.unpackLimit` of 100 would explode small
pushes into loose objects), so size accounting is the sum of pack files,
bundles have a clean basis, and the layout stays convertible to a
bucket-authoritative store: immutable content-addressed packs, refs journaled
in Postgres (section 4.5), a lease table keyed by project ID. When one disk is
no longer enough, the next step is sharding git pods by project through the
lease table, with the API routing to the holder the way project-export
downloads are routed through the store
(`pkg/api/project_archive_download.go:44-69`), and after that the bucket as
authority with disks as caches, without touching the API, the CLI, the build
path or the backups.

### 4.3 Caps and the serving budget

| Limit | Value | Why |
|---|---|---|
| Repository | 500 MB by default, per project; a project admin may raise it to a platform maximum of 2 GB; the internal archive route accepts 2 GiB so a repository at the maximum still builds (section 3.5), while the public upload keeps `maxSourceSize` at 512 MiB (`pkg/api/sources.go:28`) | The issue's figure. It covers 58 to 60 of every 60 sampled AI-built repositories (section 14); the cost of the cap is serving memory, bounded below, not storage |
| Per push | `receive.maxInputSize = min(remaining cap, 600 MiB)`; Ingress body 600m; 1,000 ref commands; 100,000 objects; 100 MiB per blob; 5,000 entries per tree; 60 levels; refs only under `refs/heads/` and `refs/tags/`, at most 1,000 refs; no Git LFS pointers and no `filter=lfs` attribute; hook output 1 MiB; pkt-line command list 3 MiB (JGit's default) | GitHub blocks files over 100 MiB and recommends 3,000 entries per directory and 50 levels; the git bomb is 12 objects; Heroku refuses LFS |
| Concurrency | 1 receive-pack and 4 upload-packs per repository; for a repository over 100 MB, or any repository without a bitmap, one exclusive fetch slot replaces the 4; a weighted semaphore over the 4 GiB limit; 10 s queue wait then 503 | Gitaly's per-repository limits; Gerrit's thread pools |
| Rates | 10 pushes a minute per actor per project (git service); 60 info/refs a minute per IP per repository and 20 failed authentications a minute per IP (API, per replica); mirror fetches once per pin and at most 6 an hour per project | Heroku: 75 git requests an hour per user per app; GitHub: 6 pushes a minute recommended |
| Time | 10 minutes per git child; 60 s idle per pipe; build relay up to 30 minutes; connections held 90 s through a handover | Gerrit: 4 and 1 minute; `logs.go:315-319` |
| Disk | expansion at 70 percent; refusal at 95 percent | one tenant cannot fill the disk for everyone |

The semaphore weights come from measurement (disputed, C12). Atlassian's rule
of 1.5 times the repository size per concurrent clone and one CPU per two
clones (Bitbucket Data Center scaling page, last modified 2024-05-22) is real,
but the measurement behind it is from 2013 (the same text is on the Stash 2.4
page of 2013-04-22), from a git without reachability bitmaps, and the rule as
written is `1.5 x concurrent clones x min(repository size, 700 MB)` scoped to
repositories up to 400 MB; the quoted 270 MB for a 220 MB repository is 1.23
times, not 1.5. Local measurements on 2026-10-29 with git
2.50.1 found: a clone that reuses a bitmapped pack costs about 1.07 to 1.10
times the pack in RSS and well under a second of CPU per 100 MB; a clone that
cannot (no bitmap yet, several packs, a shallow or partial request) costs
memory proportional to object count, 1.1 times the pack at 1,200 objects,
1.81 times at 103,000 objects, 6.05 times at 1,000,000 objects. "About 750 MB
per cold clone at 500 MB" is therefore not a bound; an object-dense 500 MB
repository served without a bitmap can pass 3 GB. The design does three things
about it: the semaphore weighs an upload-pack by bytes and by object count
(the sum of `.idx` entries), not bytes alone; a repository without a bitmap
takes the exclusive slot regardless of size until `repack -adb` has run; and
`pack.threads` is pinned (section 3.2). The pod's 4 GiB limit is sized from the
largest repository hosted and revised from the metrics.

### 4.4 Repacking and maintenance

The maintenance loop runs on the Lease holder, one repository at a time, under
the repository's mutex, so it never races a push:

- `git repack -adl --write-bitmap-index` after 20 packs or 7 days since the
  last repack, whichever comes first. The flag is explicit because verbatim
  pack reuse, the cheap serving path, needs a single bitmapped pack (or a
  multi-pack index bitmap with `pack.allowPackReuse=multi` on git 2.45 and
  later); `repack.writeBitmaps` defaults to true only on bare repositories,
  and the loop does not rely on the default (C12). GitHub reports bitmaps cut
  "Counting objects" for the Linux kernel from up to 8 minutes of CPU to under
  3 ms and the index is 5 to 10 percent of the repository size.
- `git gc --prune=30.days.ago` monthly. The 30-day grace is longer than git's
  default two weeks and matches Codeberg's practice; it is the window in which
  a lost or wrong ref update is a ref write from the journal, not a restore.
- Soft-deleted repositories purged 30 days after the move.
- Disk expansion as in section 4.1.

Build archives and bundles are not on this disk beyond `/tmp` staging.

### 4.5 Catalog and journal

In the control-plane Postgres, written only through the server:

- `repositories(project_id PK, workspace_id, default_branch, head_oid,
  size_bytes, pack_count, last_push_at, last_bundle_id, last_bundle_at,
  refs_checksum, mirror_of, cap_bytes, created_at, deleted_at)`.
- `ref_journal(id, project_id, request_id, ref, old_oid, new_oid,
  actor_subject, actor_email, token_id, client, pushed_at, applied_at,
  build_requested, deploy_done_at, note, agent_intent)`: the durable record of
  pushes that outlives Kubernetes Events (`pkg/audit/audit.go:3-5`, #306).
  Tens of thousands of rows a month at this scale.
- `repository_leases(project_id, holder, expires_at)`: one holder today;
  tomorrow N git pods shard by project ID.

### 4.6 Verification

Integrity is checked continuously, not at restore time, because the hosts that
lost data lost it to backups nobody tested.

| Check | Cadence | Scope |
|---|---|---|
| `receive.fsckObjects` with the severities of section 7, plus the pack-index and tree checks of the shim (section 3.3) | every push | the pushed objects |
| `git bundle verify` | every bundle upload | the bundle format and its prerequisites only (git-bundle manual); not object integrity |
| Sample restore by a leader-only job: unbundle the chain into a scratch repository, `git clone` from it (a bundle from a shallow repository verifies and still cannot be cloned, section 8), `git fsck --full --strict`, compare the refs checksum with the manifest and the live repository, check every kept release's `Commit` is present | hourly, a random 1 percent of repositories (100 at 10,000); all of them weekly | the bundles in the backup bucket, read with `git-backup-read` |
| A deliberately failing probe repository | hourly, with the sample | the alert path (GitLab's 2017 loss was five silent backup failures; Sourcehut keeps a failing cron entry for this) |
| `git fsck --connectivity-only` | monthly | every live repository |
| `git fsck --full` | quarterly | every live repository (GitLab runs fsck per project at most monthly; Gitea nightly with a 60-second timeout) |
| Reconciliation that flags and never deletes: project without repository, repository without project, release commit unreachable, default head without a build record, journal rows pending over an hour, size drift over 5 percent, `git/index.json` against the bucket | nightly | catalog, journal, disk and bucket |

The "never deletes" rule is Praefect's cautionary case: its verifier deletes
metadata whose replica is missing, and a misconfigured path once made every
repository look missing.

## 5. Disaster recovery

### 5.1 The copies

1. The live disk: a provider-replicated block volume with fsync'd packs and
   refs.
2. Per-repository bundles, written within seconds of a push by the service
   with the `git-backup-write` credential (section 3.2), age-encrypted with
   the platform backup passphrase (Secret `shpyrd-backup-key`,
   `pkg/install/hooks.go:571`; `pkg/backup/backup.go:414`), to the platform
   backup target (`pkg/backup/target.go:18-24`: an S3 bucket outside the
   cluster with its own credentials, Secret `platform-backup-target`) under
   the prefix `git/`.
3. The nightly provider snapshot of the claim (`platform-snapshots`, keep 7,
   cloud profiles only; crash-consistent: `Flush` runs `sync` inside the pod,
   `pkg/snapshots/snapshots.go:441`).
4. The release archives in the sources store, which make every kept release
   rebuildable even if the repository lost the commit.
5. The project export's bundle (any git clones it) and the control-plane dump
   with the catalog and journal (Barman to the `platform-control-plane`
   logical bucket, `pkg/install/controlplane.go:204`, in the cluster's region
   by default).

**Where the locked copy goes.** A gateway logical bucket cannot be a
retention-locked, append-only copy (C3): it is the prefix `buckets/<name>/` (or
`buckets/<alias>/` when the record carries an alias, `records.go:66-71`) in
one physical bucket (`pkg/objectgateway/backend.go:35`); the gateway answers
`ObjectLockConfigurationNotFoundError` to `GetObjectLockConfiguration` and
`NoSuchObjectLockConfiguration` to retention and legal-hold reads
(`backend.go:622-629`), and the writes that would lock anything
(`PutObjectLockConfiguration`, `PutObjectRetention`, `PutObjectLegalHold`,
`PutBucketVersioning`) fall to versitygw's `BackendUnsupported` and answer
`NotImplemented`; its `RetentionDays` is a deletion sweep (`records.go:553-
600`); and OCI forbids retention rules on a bucket with versioning enabled
while the production objects bucket is versioned with previous versions
deleted after seven days (`contrib/oci/terraform/backups/main.tf:216-248`,
commit d3fb0e30). So a locked copy needs a separate physical bucket, made in
the cloud repository's Terraform: unversioned, written by a credential that
can only PUT (`git-backup-write`), read by a second credential that can GET
and LIST (`git-backup-read`) for the sample restore, the volume-loss replay and
`restore-repository`, a time-bound retention rule locked after OCI's mandatory
14-day delay (after which nobody, not even Oracle Support, can shorten it), and
a cross-region replication policy to a read-only destination. The passphrase
that decrypts every bundle reaches the pod as Secret `shpyrd-backup-key`
(section 3.2). Three OCI facts shape the replication (C13): it copies only
objects uploaded after the policy, so it must exist before the first bundle;
retention rules can sit on a replication source but a destination can neither
carry rules nor be versioned, so the locked bucket is written directly by the
bundler and the destination is protected by being read-only, holding latest
versions only with deletes propagated; and although the console shows no
replication metrics, `GetReplicationPolicy.timeLastSync` ("changes made to the
source bucket before this time have been replicated") is readable through the
API, so the lag is a watermark the service reads, and the application probe
(write a marker, read it in the other region) verifies end-to-end readability
rather than being the only signal. When no external target is set, the default
target is the gateway's `platform-backups` logical bucket
(`pkg/install/vars.go:506-510`) and the protection is the physical bucket's
7-day previous versions; the runbook (section 5.7) requires production to set
the separate bucket. Whether the cloud repository gains that bucket before the
production rollout, and who owns the probe, is open (section 13).

**What region loss needs.** Three things region loss destroys default to the
same region: the platform backup target is the gateway's `platform-backups`
logical bucket (`pkg/install/vars.go:506-510`), the control-plane database's
Barman backups go to the `platform-control-plane` logical bucket
(`pkg/install/controlplane.go:204`), both prefixes of the one physical bucket
in the cluster's region, and the age passphrase that decrypts every bundle
lives in the cluster (Secret `shpyrd-backup-key`) and inside the encrypted
platform backup. Region loss is recoverable only when the platform backup
target and the control-plane backups also point at the cross-region bucket, or
are replicated with it, and the passphrase is held outside the region:
`shpyrd-ctl cluster backup key` (`internal/cli/cluster_backup.go:103`) prints
it for that purpose. The runbook (section 5.7) carries all three as production
requirements. When the catalog and `git/index.json` are gone, the restore order
is: list `git/v1/` in the replica; read each project's newest manifest for its
`projectID`, `workspaceID` and `slug`; rebuild the `repositories` rows; restore
the App objects and the store from the platform backup; restore every
repository (section 5.5); run the reconciliation (section 4.6). The manifests
and `shpyrd.json` carry the IDs so the bucket alone can rebuild the catalog.

### 5.2 Bundle layout

Keys `git/v1/<project ID>/<backup ID>/manifest.json`, `001.bundle.age`,
`001.refs`, `002.bundle.age`, … where the backup ID is zero-padded
`(2^63 - unixnano)` so one listing with limit 1 returns the newest (GitLab's
layout and its generation trick). The manifest: {projectID, workspaceID, slug,
defaultBranch, head, refs, refsChecksum, chain, sizeBytes, createdAt,
platformVersion, format}. A full bundle (`git bundle create - --all`) when
the chain passes 20 increments or the increments together exceed the full's
size, at least every 60 days, and never more than once a week; otherwise an
increment with the previous backup's refs as prerequisites. At 20 pushes a
month per project (section 6.3) that is about one full a month, so the 60-day
retention holds about two fulls and two months of increments per repository.
The first draft wrote a full weekly regardless, which with every project
pushing would have kept about nine fulls per repository in the bucket. The
service never modifies or deletes an uploaded object (retention does), which
is what makes the layout fit a locked bucket; Gitaly's server-side backups
follow the same rule and the same per-repository manifest-plus-bundle shape.
Empty repositories get a manifest with HEAD and no bundle (git refuses an
empty bundle). Pushes within one minute coalesce into one increment; retention
60 days on the locked bucket. Because the service never deletes, a
soft-deleted project's bundles outlive the 30-day purge of its repository by a
month: the code of a deleted project is recoverable from the bucket for 60
days after its last push, and the project's DELETE answer says so.

### 5.3 Scenarios

RPO is the data that can be lost, RTO the time until pushes and clones work
again. Fleet figures are targets until a drill measures them.

| Scenario | Detection | Mitigation | RPO | RTO |
|---|---|---|---|---|
| Pod crash | readiness probe, `GitLeaseLost` | `Recreate` reschedules on the same disk; the recovery sweep completes or fails pending journal rows | 0 (fsync) | 20 to 40 s; an in-flight push fails and the client retries |
| Planned roll or drain | the Lease handover | the API holds connections 90 s; the CLI retries 3 min | 0 | 1 to 3 min, seen as a slow push |
| Unplanned node loss | pod unreachable, Lease expires | the successor needs a node in the volume's availability domain or zone; the old attachment is force-detached by the attach-detach controller after its unmount wait (about 6 min, `maxWaitForUnmountDuration` in kube-controller-manager; **unverified**: neither that constant nor the OCI block-volume documentation on availability-domain scope was re-read for this document) | 0 | target 10 min, **unverified** until the staging drill kills a node (the first draft's 1 to 5 min had no source) |
| Lease and fencing during a node partition | `GitLeaseLost`; two pods for one Deployment | the old pod stops admitting on Lease loss and kills its children; the successor cannot attach the ReadWriteOnce device until the provider detaches it from the old node, which is the fence; the API proxies only to the endpoint whose pod name holds the Lease (section 3.3, step 6) | 0 | the node-loss figure |
| Volume lost or corrupted | fsck failure, mount failure | new claim from the newest snapshot, then replay every bundle newer than the snapshot (read with `git-backup-read`); without a snapshot (kind) rebuild from bundles | seconds to 1 min (bundle lag) | 5 to 15 min snapshot plus replay (8 workers, about 0.5 s each: 10,000 repositories about 10 min), target 1 h; from bundles alone 200 GB at 100 MB/s about 30 min plus fsck, target 2 h |
| Silent object corruption on the disk | monthly connectivity fsck, quarterly full fsck, a failing fetch or clone | restore the repository from its bundles (section 5.5): bundle objects were fsck'd when pushed; `core.fsync=all` and the provider-replicated volume make this rare | 0 when the newest bundle postdates the corruption; otherwise the pushes since the newest good bundle | minutes, `restore-repository` |
| Accidental deletion of one repository | reconciliation, the person | soft delete 30 days; bundles in the locked bucket for 60 | 0 to 1 min | minutes, one admin route |
| Bad but valid push (human or agent) | the person, the build | not DR: fast-forward only, no default-branch delete, reflog 90 days, journal with old and new oids, objects kept 30 days; "Run version 7 again" or "Build version 7 again" | 0 | minutes |
| Crash between ref update and post-receive | pending journal rows older than 2 min | 5-minute sweep, 30-second deploy retry | 0 | at most 5 min (the sweep) plus 30 s (the deploy retry), automatic |
| Backup job failing silently | `GitBundleStale` per repository within 5 min, the probe repository, verified alert delivery | the hourly sample restore would also catch it; the live disk serves throughout | the exposure grows from the moment bundles stop and is bounded by the time to fix, so by the alert | none: serving never stops |
| Broken increment chain or a deleted increment | the hourly sample restore fails on the chain; `GitBundleVerifyFailed` | the service never deletes increments; a full bounds every chain to about a month (section 5.2); on a broken chain the bundler writes a new full at once | the pushes since the last good full, at most a month, and only if the live disk and snapshots are also lost | one full bundle, seconds |
| Backup bucket deleted or credential leaked | reconciliation of `git/index.json` against the bucket | PUT-only credential, locked retention, read-only replica; the live disk is intact | 0 live | 0 for serving; hours to rebuild the backup tier by a full bundle of every repository |
| Control-plane database loss | CNPG failover fails; the server's readiness | Barman restore from `platform-control-plane` (cross-region in production, section 5.1); repositories are self-describing through `shpyrd.json`, so the reconciliation rebuilds `repositories` rows from the disk; `ref_journal` rows since the last archived WAL are lost, and pending deploys with them, so the reconciliation compares each default head with the newest release and requests a build where none exists | the WAL archive interval (continuous archiving: minutes) for the catalog and journal; 0 for the repositories | the database restore plus one reconciliation pass: tens of minutes |
| Sources-store loss (release archives) | a build fails to fetch its archive | "Build version N again" re-archives the commit from the repository; "Run version N again" is unaffected (the image is in the registry); the archive garbage collector keeps what kept releases need | 0: every archive is derivable from a commit in the repository or its bundles | minutes per project, one rebuild |
| Region loss | provider status, every probe | new cluster from the platform backup and the control-plane backups, both of which must be cross-region, with the passphrase held outside the region (section 5.1); every repository restored from the replicated bucket in the order section 5.1 gives | about 1 min plus replication lag for repositories; the WAL archive interval for the catalog | hours: cluster init plus the fleet restore; target 4 h after a drill (GitLab Dedicated commits to RTO 8 h and RPO 4 h with 4-hourly snapshots and no per-repository bundles, a different shape) |
| Repository and records disagree after a restore | nightly reconciliation | `shpyrd.json` and the manifest carry the IDs so either side rebuilds the other; the release archive stays the authority for what ran | 0: nothing is lost, the sides disagree | minutes per project through the reconciliation report and `restore-repository` |
| Disk full | `GitDiskNearlyFull` | per-project cap, 70 percent expansion, 95 percent refusal in words | 0 | minutes on OCI and EBS, which expand online; on kind until the operator acts, since local-path cannot expand |
| Restore to a newer platform version | a planned operation | bundles are version-independent git; the manifest format is versioned; catalog rows are in the control-plane dump | 0 | that of a normal restore (section 5.5, or the volume row) |

**Adopted objectives:** repository RPO seconds to one minute (bundle lag) and
zero for anything short of volume loss; RTO minutes for one repository, 1 to 3
minutes planned and 10 minutes unplanned for the pod, hours for a volume or
region loss, each claimed only once measured. For comparison, GitLab.com
snapshots Gitaly hourly with "minutes to 6 hours" of expected loss, GitLab
Dedicated commits to RPO 4 h and RTO 8 h on 4-hourly snapshots, and Sourcehut
restored from backups up to 12 hours old after its 2024 outage.

### 5.4 Backup and export changes

- `pkg/backup`: the `Manifest` gains `Repositories` and `FormatVersion` 2
  (`backup.go:37-50`), with `Read` and `Restore` accepting 1 (`backup.go:441`).
  The archive carries `git/index.json` (newest backup ID and refs checksum per
  project; a few hundred KB at 10,000 projects) rather than the bundles, so
  the nightly tarball stays small and the 4Gi scratch and 1Gi memory hold
  (`deploy/components/platform-backup/base/cronjob.yaml:67-84`). The
  `Restorer` gains a `RestoreRepository` callback (like `UploadSourceURL`,
  `pkg/backup/restore.go:33`); `apply` rewrites nothing for blob sources
  (`restore.go:343` already covers them). `Archive.Read` keeps loading
  everything into memory because nothing large is in it.
- The project archive gains `repository/repo.bundle` (plain, inside the
  encrypted-at-rest download), whitelisted in `validateProjectArchive`
  (`pkg/api/project_archive_metadata.go:261-339`), written before the drain
  like buckets (`pkg/api/project_archives.go:124-178`), restored as an asset
  import before `applyArchiveConfiguration`
  (`pkg/api/project_archive_restore.go:390-433`). For GitHub-sourced projects
  with a mirror, the mirror's bundle replaces the credential-less helper clone
  that fails on private repositories
  (`pkg/api/project_archive_runtime.go:383-419`). A mirror is never shallow
  (section 8): a bundle made from a shallow repository passes `git bundle
  verify` and cannot be cloned (tested 2026-10-29 with git 2.50.1: "remote did
  not send all necessary objects"), so the export and the hourly check would
  both pass on an unusable artefact. A GitHub-sourced project over the cap has
  no mirror and exports `git archive` of the pinned commit through the helper
  pod, which gains the `<app>-git` credential it lacks today.
- The workspace export gains `GET /api/workspace/export/repositories`
  streaming a tar of `<slug>.bundle` per project (ClusterAdmin); a leaving
  company gets files any git can clone from.
- Docs (`content/docs/backups.mdx:16-21, 94, 165`;
  `apps/shared/src/project-backups.tsx:96-107`) say the repository is in both.

### 5.5 Restoring one project

`shpyrd-ctl project restore-repository <slug> --from <backup id>`: the server
creates the repository through the lifecycle route if absent; the service
downloads the manifest and chain with `git-backup-read`, decrypts with the
passphrase, unbundles in order into an empty repository, sets HEAD, runs fsck,
recomputes the refs checksum, re-registers the catalog row from `shpyrd.json`
when missing; it refuses to replace a repository that is ahead of the backup
unless `--replace` (never a silent force; commits made after the backup stay
reachable 30 days). Minutes, by a non-expert following the runbook (section
5.7). GitLab.com's snapshot-only equivalent is a 4-to-6-day escalation with no
guarantee; a per-repository bundle layout is what avoids that.

### 5.6 Drills

The hourly sample restore is the continuous drill. A timed fleet restore into
a scratch namespace of the staging cluster runs quarterly and after every
change to the backup path, including an unplanned node kill, and records
duration and date the way GitLab's `recovery-measurements.md` does (its Gitaly
zonal recovery measured 1h15m to 4h15m across seven staging runs against a
2-hour target, which is why this document claims nothing it has not measured).

### 5.7 Runbook outline

The runbook is a page in `content/docs` beside the backups page
(`content/docs/backups.mdx`), written in slice 9 (section 11). Its entries:

- Data pool absent: the git pod lands on the platform pool
  (`pkg/install/pools.go:44-49`); adding a data pool and moving the claim.
- The separate backup bucket in production: the Terraform in the cloud
  repository; the `git-backup-write` and `git-backup-read` credentials; the
  retention rule and its 14-day lock window; the replication policy, created
  before the first bundle; the probe and `timeLastSync`.
- Region loss prerequisites: the platform backup target and the control-plane
  backups cross-region; the passphrase printed by `shpyrd-ctl cluster backup
  key` and kept outside the region (section 5.1); the restore order when the
  catalog is gone.
- `shpyrd-ctl project restore-repository <slug> --from <backup id>`, and when
  `--replace` is right (section 5.5).
- Disk expansion on OCI: the 70 percent request against the 50 GiB class
  minimum, the operator gate if open question 8 settles on one, the 95 percent
  refusal.
- Reading the reconciliation report (section 4.6) and the alerts of section
  3.3, step 8.

## 6. Costs

Infrastructure costs only, at list prices. 1 GB = 1,024 MB. Two fleet sizes
(1,000 and 10,000 projects) and two mean repository sizes (20 MB, which is
above the medians of every sampled AI-built fleet, and 200 MB, which is near
the worst sampled p90; section 14). Every line gives its arithmetic; the
sources are listed once at the end.

### 6.1 Prices

| Item | OCI (one price list; egress differs by region, the production region is `sa-saopaulo-1`) | AWS (`us-east-1` list; the production region would be `sa-east-1`, so the two columns are not like for like) |
|---|---|---|
| Object storage, standard, per GB-month | 0.0255, first 10 GB free per tenancy per SKU (B91628) | 0.023, first 50 TB (S3 Standard) |
| Object storage, cold, per GB-month | 0.0026 Archive (B91633) | 0.0036 Glacier Flexible Retrieval |
| Requests | 0.0034 per 10,000, first 50,000 a month free (B91627) | 0.005 per 1,000 PUT-class; 0.0004 per 1,000 GET-class |
| Egress to the internet | 10,240 GB a month free, then 0.0085 per GB from North America, Europe and the UK (B88327); 0.025 from APAC, Japan and South America (B93455); 0.05 from the Middle East and Africa (B93456) | 100 GB free, then 0.09 first 10 TB, 0.085 next 40 TB, 0.07 next 100 TB |
| Block volume, per GB-month, provisioned | 0.0255 capacity plus 0.0017 per VPU (B91961, B91962): lower-cost 0.0255, balanced (10 VPU) 0.0425 | gp3 0.08 |
| Block snapshots, per GB-month | block volume backups are stored in Object Storage, no separate SKU: 0.0255 | EBS snapshot 0.05 |
| Shared file system, per GB-month | File Storage 0.30 (B89057) | EFS Standard 0.30 |
| Compute | E4 Flex 0.025 per OCPU-hour, 0.0015 per GB-hour (B93113, B93114) | about 30 USD a month for the same pod; **unverified**, no SKU read |
| Inter-region transfer | under the egress allowance (inferred from Oracle's block-volume replication and VCN pricing statements, not an Object Storage page; **medium confidence**) | 0.02 per GB |

The 0.0085 egress rate is the North America, Europe and UK SKU; the production
tenancy is in `sa-saopaulo-1` (`contrib/oci/terraform/terraform.tfvars.example:4`;
`pkg/install/vars_test.go:76-99`), where the rate over the allowance is 0.025
(disputed, C14). The tables below give both. The AWS column is `us-east-1`
list; a like-for-like `sa-east-1` column would be higher (egress 0.15, 0.138
and 0.126 by tier) and is not computed here. A third-party claim that OCI
abolished egress charges in February 2026 is contradicted by Oracle's price
list dated 2026-10-08 and is treated as unconfirmed.

### 6.2 Used bytes

| Fleet | Mean | Used (GB) |
|---|---|---|
| 1,000 | 20 MB | 19.53 |
| 1,000 | 200 MB | 195.31 |
| 10,000 | 20 MB | 195.31 |
| 10,000 | 200 MB | 1,953.13 |

### 6.3 Monthly cost by line, USD

Assumptions. The live disk is provisioned by the rule of section 4.1: 200 GiB
by default, grown by 50 percent whenever use passes 70 percent, so used bytes
sit between 47 and 70 percent of the disk (200 GiB at 19.53 GB used, 300 GiB
at 195.31 GB, 3,418 GiB at 1,953.13 GB, which is 200 × 1.5^7). The first draft
stated 2.5 times used bytes and then provisioned the 200 MB fleet at 1.3 times
(2,500 GiB); both are replaced by the rule. Snapshots keep 7 nightly copies and
hold about 1.2 times used bytes (the first full, the rest incremental).
Bundles follow the policy of section 5.2 with every project pushing 20 times a
month and each push changing about 2.5 percent of the tree (a stated churn
assumption, not a measurement): one full a month, two fulls in the 60-day
retention, plus two months of increments at half the used bytes a month, about
3 times used bytes in all; the cross-region replica holds the same bytes and
receives 1.5 times used bytes a month (a full and a month of increments), which
on AWS is inter-region transfer and on OCI counts against the 10 TB egress
allowance. The first draft's 2.5 times with a weekly full would have been about
9 times. Release archives are 20 kept releases per project at a compressed tree
of 3 MB (20 MB fleet) or 20 MB (200 MB fleet), deduplicated by content.
Requests are 20 pushes a project a month at 3 PUTs each (manifest, bundle,
refs) plus the hourly sample restore's GETs: 1 percent of repositories an hour
× 730 hours × 3 GETs, 219,000 at 10,000 projects and 21,900 at 1,000. OCI's
10 GB free allowance is one per tenancy per SKU and is applied once, on the
bundles line (the first draft applied it on three lines). Compute is one pod of
1 OCPU and 4 GB. Excluded: the GitHub mirrors' disk use (bounded by the same
cap, an unknown share of projects); a node dedicated to the data pool when one
is (compute counts the pod, not the node); the ref journal's growth in the
control-plane database (tens of thousands of rows a month, negligible); the
one-time work of publishing the second image from the cloud repository's
`deploy.yml`.

**10,000 projects, 20 MB mean (195.31 GB used):**

| Line | OCI | AWS |
|---|---|---|
| Live disk, 300 GiB | 300 × 0.0425 = 12.75 | 300 × 0.08 = 24.00 |
| Snapshots, 234.38 GB | 234.38 × 0.0255 = 5.98 | 234.38 × 0.05 = 11.72 |
| Bundles, 585.94 GB | (585.94 − 10) × 0.0255 = 14.69 | 585.94 × 0.023 = 13.48 |
| Requests, 819,000 | (819,000 − 50,000) / 10,000 × 0.0034 = 0.26 | 600 × 0.005 + 219 × 0.0004 = 3.09 |
| Replica, 585.94 GB | 585.94 × 0.0255 = 14.94 | 13.48 storage + 292.97 GB × 0.02 = 5.86 transfer + 600,000 replication PUTs × 0.005 / 1,000 = 3.00 (seed once 3.91) |
| Release archives, 585.94 GB | 585.94 × 0.0255 = 14.94 | 585.94 × 0.023 = 13.48 |
| Compute | (0.025 + 4 × 0.0015) × 730 = 22.63 | about 30 (**unverified**) |
| **Total** | **86.19** (0.0086 per project-month) | **118.11** |
| Egress ceiling (30 internet clones a month per repository, 5,859 GB; on OCI plus 293 GB of replication) | 0 (6,152 GB, under the allowance) | (5,859 − 100) × 0.09 = 518.34 |

**10,000 projects, 200 MB mean (1,953.13 GB used):**

| Line | OCI | AWS |
|---|---|---|
| Live disk, 3,418 GiB | 3,418 × 0.0425 = 145.27 | 3,418 × 0.08 = 273.44 |
| Snapshots, 2,343.75 GB | 59.77 | 117.19 |
| Bundles, 5,859.38 GB | (5,859.38 − 10) × 0.0255 = 149.16 | 5,859.38 × 0.023 = 134.77 |
| Requests, 819,000 | 0.26 | 3.09 |
| Replica, 5,859.38 GB | 5,859.38 × 0.0255 = 149.41 | 134.77 + 2,929.69 GB × 0.02 = 58.59 transfer + 3.00 replication PUTs (seed once 39.06) |
| Release archives, 3,906.25 GB | 3,906.25 × 0.0255 = 99.61 | 3,906.25 × 0.023 = 89.84 |
| Compute | 22.63 | about 30 (**unverified**) |
| **Total** | **626.11** (0.063 per project-month) | **844.69** |
| Egress ceiling, 58,594 GB of clones; on OCI plus 2,930 GB of replication, 61,523 GB | NA/EU: (61,523 − 10,240) × 0.0085 = 435.91; South America: × 0.025 = **1,282.09** | 10,240 × 0.09 + 40,960 × 0.085 + 7,294 × 0.07 = 4,913.76 |

Two earlier figures for this fleet on OCI, 544 and 536.79, were both wrong
(disputed, C14): the first was not derivable from its inputs, and the second
provisioned the disk at 2,500 GiB against its own 2.5-times rule and bundles
at 2.5 times used bytes against a weekly-full policy. The 626.11 above follows
the rules of sections 4.1 and 5.2 with the same pod as the small fleet; a pod
sized for 500 MB repositories (section 4.3) adds to the compute line.

**1,000 projects, 20 MB mean (19.53 GB used):**

| Line | OCI | AWS |
|---|---|---|
| Live disk, 200 GiB default | 200 × 0.0425 = 8.50 | 200 × 0.08 = 16.00 |
| Snapshots, 23.44 GB | 0.60 | 1.17 |
| Bundles, 58.59 GB | (58.59 − 10) × 0.0255 = 1.24 | 1.35 |
| Requests, 81,900 | (81,900 − 50,000) / 10,000 × 0.0034 = 0.01 | 60 × 0.005 + 21.9 × 0.0004 = 0.31 |
| Replica, 58.59 GB | 1.49 | 1.35 + 29.30 GB × 0.02 = 0.59 transfer + 0.30 replication PUTs (seed once 0.39) |
| Release archives, 58.59 GB | 1.49 | 1.35 |
| Compute | 22.63 | about 30 (**unverified**) |
| **Total** | **35.96** (0.036 per project-month) | **52.42** |
| Egress ceiling, 586 GB; on OCI plus 29 GB of replication | 0 | (586 − 100) × 0.09 = 43.73 |

**1,000 projects, 200 MB mean (195.31 GB used):**

| Line | OCI | AWS |
|---|---|---|
| Live disk, 300 GiB | 12.75 | 24.00 |
| Snapshots, 234.38 GB | 5.98 | 11.72 |
| Bundles, 585.94 GB | (585.94 − 10) × 0.0255 = 14.69 | 13.48 |
| Requests, 81,900 | 0.01 | 0.31 |
| Replica, 585.94 GB | 14.94 | 13.48 + 5.86 transfer + 0.30 replication PUTs (seed once 3.91) |
| Release archives, 390.63 GB | 390.63 × 0.0255 = 9.96 | 8.98 |
| Compute | 22.63 | about 30 (**unverified**) |
| **Total** | **80.96** (0.081 per project-month) | **108.13** |
| Egress ceiling, 5,859 GB; on OCI plus 293 GB of replication | 0 | 518.34 |

### 6.4 What the numbers say

- Storage is a rounding error on both clouds at both sizes. The 10,000-project
  fleet at 200 MB stores about 21 TB across disk, snapshots, bundles, replica
  and archives (3,418 + 2,344 + 5,859 + 5,859 + 3,906 GB; the first draft's
  "about 11 TB" did not sum its own lines) for about 626 USD a month on OCI.
- The choice between a disk and a bucket is operational, not financial: what
  this design pays over a bucket-authoritative one is the provisioned disk
  headroom and the snapshot line (18.73 USD a month at 10,000 projects and
  20 MB, 205.04 at 200 MB); what it saves is the per-object request storm
  (objgit measured about 200,000 calls for one push) and a cache tier.
- The alternative live store, a shared file system at 0.30 per GB-month, would
  cost 58.59 (195.31 GB) and 585.94 (1,953.13 GB) a month for the used bytes
  alone, twelve times the object store (585.94, not 585.93; C14).
- Egress is the only term that grows with use, on both clouds; the statement
  that it grows "on AWS only" is wrong for the production region (C14): with
  the South America SKU the OCI ceiling at 10,000 projects and 200 MB is 1,282
  USD a month, twice everything else together, and the replicated bundles
  (about 2.9 TB a month at that size) consume part of the 10 TB allowance
  before any clone. The ceiling assumes every repository is cloned over the
  internet 30 times a month; realistic coding-tool traffic (incremental
  fetches) is about a tenth, and builds, sandboxes and the backup writes are
  in-region and free on both clouds. The design keeps them there on purpose.
- Compute dominates the small fleet: one always-on pod is 22.63 USD a month
  whether it serves 1,000 or 10,000 repositories.
- Ageing fulls older than the newest into Archive or Glacier roughly halves
  the bundle and replica lines; a restore then pays retrieval and, on OCI
  Archive, waits up to an hour, so the newest full and its increments stay in
  the standard tier.

Sources: Oracle Cloud price list API
(`apexapps.oracle.com/pls/apex/cetools/api/v1/products`, `lastUpdated`
2026-10-08, part numbers as given, read 2026-10-28 and 2026-10-29); AWS Price
List (`pricing.us-east-1.amazonaws.com`, AmazonS3 publicationDate 2026-10-15,
AWSDataTransfer 2026-09-16) and `b0.p.awsstatic.com/pricing/2.0/
meteredUnitMaps` for S3, EFS and EBS (read 2026-10-28 and 2026-10-29); OCI
block volume backup docs (`docs.oracle.com/.../blockvolumebackups.htm`); the
AWS blog of 2020-04-16 for `sa-east-1` egress tiers.

## 7. Security

### 7.1 First-version controls

**Transport and identity.** TLS at nginx only. Basic with the token as
password, resolved by the server: hash lookup, workspace binding, active
owner, offered login method (`pkg/api/tokens.go:113-197`). The admin token is
refused. OAuth tokens only with the license, with an audience check and
`projects:write` for pushes. The `Authorization` header is never logged, only
the token ID. Failed attempts are throttled per IP (20 a minute) and audited.
404 for repositories the caller may not see. The CLI's credential helper keeps
the token in `sessions.json` at mode 0600 and never in `~/.git-credentials`,
which git's own manual calls unencrypted and discouraged; a token never goes
into a remote URL (shell history).

**Authorization and tenancy.** Project roles from the store, the maintenance
gate, the suspended-workspace rule. The repository path is derived from the
signed project ID, never from the URL. The slug is validated by the project
regex before any lookup. The git service trusts only EdDSA-signed headers with
a 60-second expiry and is reachable only from the server pods by
NetworkPolicy. It never dials the internet except GitHub for mirrors through
`gitauth.SafeHTTPClient` (`pkg/gitauth/dial.go:36-112`: refuses loopback,
link-local and metadata addresses).

**Receive side**, the work C git does once configured and go-git's v5 server
does not do at all (its `server` package has no fast-forward check, no ref-name
validation, no size limit, no hooks, no atomic push; `plumbing/transport/
server/server.go`, v5.19.3): `receive.fsckObjects=true` with `hasDotgit`,
`hasDotdot`, `hasDot`, `fullPathname`, `gitmodulesSymlink`, `gitmodulesBlob`,
`gitmodulesMissing`, `gitmodulesName`, `gitmodulesPath`, `gitmodulesUrl`,
`nulInHeader`, `badTree`, `duplicateEntries`, `treeNotSorted`, `largePathname`
as errors, and the three cosmetic ids GitLab ignores (`badTimezone`,
`missingSpaceBeforeDate`, `zeroPaddedFilemode`) ignored;
`receive.maxInputSize` = remaining quota; fast-forward and default-branch
rules in the shim before anything moves; ref names limited to `refs/heads/*`
and `refs/tags/*` and checked against check-ref-format; 1,000 commands and
100,000 objects per push; 100 MiB per blob, read from the quarantine pack's
index; 5,000 entries per tree and 60 levels, from a bounded walk of the new
trees (never `--batch-all-objects`, which walks the whole repository); no Git
LFS pointers or `filter=lfs` attributes; a warning on `.gitmodules`; the
quarantine leaves nothing on refusal; `core.hooksPath` fixed per
invocation so a repository's own hooks never run; git executed with an
argument list and no shell in an image with no shell; the version asserted at
start; the base image rebuilt on git security releases (five CVEs in one May
2024 release).

**Process isolation.** One process per request with a deadline and idle
timeout; `pack.windowMemory` and `pack.threads` bounded; the weighted
semaphore from the memory limit; `GOMEMLIMIT` for the Go part; read-only root,
uid 65532, drop ALL; the ServiceAccount can only touch its Lease; the pod sits
on the data pool when one exists, not beside the API. Gitaly does the same
with cgroups; a Go service cannot cgroup a goroutine, which is why the limits
are per request and the git work is a child process.

**Clients the platform runs.** Hooks never travel with a push, so the real
exposure is on the client side: builds fetch archives on 8082 with capability
URLs and never clone (kpack's go-git fetcher, v5.16.4 plus a v6
pseudo-version with `Depth 1`, never touches the repository); the sandbox
(section 9) clones with git 2.45.1 or later, no recursive submodules,
`core.symlinks=false` where a tree is only read, in its own namespace (#288).
A developer-role user can shape anything in the repository, so it is
untrusted input even though the platform hosts it.

**Data at rest.** The block volume encrypted by the provider class; bundles
age-encrypted before upload; the backup bucket's credential can only PUT,
retention locked, replica read-only; archives as today with capability URLs
(`pkg/api/sources.go:75-81`). Config vars never enter the repository
(`REVISION` and `SHPYRD_*` stay the platform's, `pkg/api/apps.go:1050`).

**Secrets in code.** The CLI writes `.gitignore` excluding `.env` before the
first commit; the pre-receive secret-file list (GitLab's push rule: `id_rsa`,
`id_dsa`, `id_ed25519`, `id_ecdsa` and `_sk` variants, `aws/credentials`,
`*.pem`, `*.key`, `*.history`, `*_history`), warn-only in the first release,
then refuse.

**Audit and limits.** Per push the journal row and Event with actor, token
ID, IP, each ref old to new, pack bytes, outcome, request ID (the same ID
printed in the `remote:` lines); refusals audited; per-actor push rate and
per-project clone rate.

**Supply chain** (#289). git from a distro package on a stable Alpine branch
with its checksum in the image build; go-git in the CLI pinned and bumped
monthly (ten advisories in 2026, several in parsers of untrusted objects); the
hook shim is the platform's own binary.

### 7.2 Later

- A keyword-prefiltered pass over new text blobs under 1 MiB with gitleaks'
  high-confidence provider-prefix rules (MIT; 222 rules; Aho-Corasick
  prefilter, RE2 engine), bounded by GitLab's thresholds (skip above 3,150
  changed paths or 350,000 changed lines, accept on timeout with an audit
  event), with a bypass that records a reason (GitHub's three). gitleaks is
  security-fixes only since v8.30.1, so the rules are pinned as a fork.
- GitLab's "unique repositories cloned per period" exfiltration limit.
- Protocol v2 for upload-pack is already there through the binary; `filter`
  and partial clone for the sandbox when it needs them.
- Signed pushes and commit-signature verification, once there is a reader for
  them; go-git CVE-2026-45022 (parsed-versus-raw signing payloads) is the
  cautionary case.
- Bearer tokens through git's credential `authtype` capability and short-lived
  tokens minted by the helper, so the 30-day session token stops being the
  password.
- Moving pack parsing into a separate worker process with its own memory cap,
  the cgroup-style isolation Gitaly has, if the per-request limits prove too
  coarse.

## 8. Migration

**Blob projects**, every project deployed from a folder today. Nothing moves
on upgrade. The repository is created empty on project creation
(`pkg/api/apps.go:448`) and, for existing projects, lazily on the first
`shpyrd deploy` from the new CLI or on demand from the dashboard. It is
**never pre-seeded** from the current archive: a seeded root would make the
person's first push a non-fast-forward of unrelated histories, which is the
defect the operator lens found in the design that imported the archive as
generation 1. For a project whose folder is gone, the project page offers
"Start the repository from the running version" (admin-gated, only while the
repository is empty): the server reads the current `spec.source.blob` archive
(`pkg/api/project_archive_metadata.go:234`) and the service commits it as the
first commit (Author the recorded deployed-by when known, else shpyrd; message
"Imported from the deploy of <date>, version N"); the person then clones. Old
CLIs keep working: `POST /api/sources` and the blob branch of `deployApp` stay
(`pkg/api/server.go:814`, `apps.go:857-859`); such an upload is committed
server-side (every build is a commit) with the `Blob.Ref` check of section 3.5
against a stale folder, and the dashboard nudges towards the clone.
Kubeconfig-only operators keep the archive path for good. Image-only projects
(`--local-build`) get no repository until a source deploy.

**GitHub-sourced projects** (`spec.source.git` on github.com). They keep the
watcher, the gates and the reporter unchanged (`internal/controller/
github.go`, `github_report.go`). The git service becomes a read-only mirror
fed on each pin: when the App controller sees the pin change
(`AnnotationGitCommit`, `api/v1alpha1/app_types.go:118-119`) it asks the
service to `git fetch` the pinned commit and the followed branch from GitHub
with the credential already in `<app>-git` (the GitHub App's `contents:read`
token, `github.go:252-306`, written as netrc as `dockerfile.go:538-542` does).
A mirror is full history or nothing: a bundle made from a shallow repository
passes `git bundle verify` but cannot be cloned (tested 2026-10-29 with git
2.50.1: "remote did not send all necessary objects"), so a shallow mirror
would let the export and the hourly check pass on an unusable artefact (the
first draft kept "the last N commits" over the cap). A repository over the cap
gets no mirror; the project page says so, the export falls back to `git
archive` of the pinned commit through the helper pod with the `<app>-git`
credential (section 5.4), and the sandbox clones GitHub directly later. Pushes
to a mirror are refused in pre-receive with the GitHub URL, Upsun's rule: never
a silent overwrite. Mirrors are created lazily at the first pin after the
upgrade. The mirror also fixes the project export for private repositories,
which the credential-less helper clone cannot read today. Fixes for these
projects go to GitHub later and need `contents:write` (the App asks
`contents:read` today, `internal/cli/ctl_github.go:84`, and every installation
would re-approve), so the agent's first slice is for embedded projects only
(section 9).

**Changing the source kind.** `shpyrd deploy --git <url>` on an embedded
project makes it GitHub-sourced: the repository becomes a mirror of that URL at
the next pin, its own history stays reachable under
`refs/shpyrd/embedded/<date>` for 30 days and in the bundles for 60, pushes
are refused with the GitHub URL from then on, and the deploy answer says all
of this. Removing the GitHub connection of a GitHub-sourced project (the
installation gone, `github.disconnect`) leaves the mirror read-only until a
person runs `shpyrd deploy` from a clone of it or chooses "Make this the
project's repository" on the project page, which clears `mirror_of` and makes
`main` writable from the last mirrored commit. `shpyrd deploy --image` on an
embedded project pins the image as today (`spec.image`) and leaves the
repository alone; the next source deploy clears the pin. The `--git` and
`--image` flags keep their meaning (the CLI rows of section 2). The default
branch is `main` at creation (`git init --bare --initial-branch=main`); `PATCH
/api/projects/:slug/repository {defaultBranch}` (ProjectResource) changes it
and the catalog's `default_branch` with it; the first version offers no other
way, and a push to the new default branch builds from the next push on.

**Projects created before, and the platform rollout.**

1. The cloud repository's `deploy.yml` publishes the `shpyrd-git` image beside
   the server image on every release. `cluster init` adds the git component
   behind install var `SHPYRD_GIT` (default off): new PVC, Deployment, Secret
   `git-internal` and the backup Secrets of section 3.2, the API routes
   answering 404 while off; the companion Ingress per front door appears on
   the workspace controller's next pass; the CRD change regenerates; the
   control-plane store gains the three tables by migration; the backup
   Manifest moves to `FormatVersion` 2 while `Read` and `Restore` accept 1
   (`pkg/backup/backup.go:37, 441`).
2. Enable on the dev cluster, run the drills (section 5.6), including a node
   kill.
3. Enable in production with the separate backup bucket in place.
4. Release the CLI with go-git and the new default.
5. Docs.

**Downgrade.** `cluster init --skip git` removes the component; the
repositories stay on the retained claim ("existing claims are deliberately not
deleted by an upgrade", `pkg/install/sources.go:12`); the blob path serves
builds throughout; archives of every built commit already exist, so nothing
that ran stops being rebuildable.

**Kind.** The PVC lands on local-path (no snapshots); bundles go to the local
backup target (Garage); developers test the whole path including a restore
from bundles. This is the one profile where the bundles are the only second
copy.

**Dashboard and docs.** A design session is required for interface work
(`design/DESIGN_FOR_AGENT.md`). The Overview's GitHub-shaped source block
(`apps/workspace/src/screens/project/source.tsx:83-162`) is joined by a block
for the repository on embedded projects: the clone address with Copy, as the
MCP address card does (`apps/workspace/src/screens/workspace/platform.tsx:
103-123`), who pushed last, and "connect your coding tool" lines; the Releases
page becomes versions in words with "Run version 7 again" and "Build version 7
again" and hides the revisions and digests it shows today (`releases.tsx:83,
149`); the Activity marks learn `git.push` and `git.rollback`
(`overview.tsx:176-197`, `access.tsx:459-461`); the Mock gains commits per
release and a repository address (`mock.ts:179-191`, `mock/projects.json:75`).

## 9. The path to the agent

Everything the agent needs is a function of a commit and a repository the
platform already serves; each step adds, nothing is reworked.

1. **Candidate builds** (build a commit without releasing). `deployCommit`
   splits into `buildCommit(project, commit)` and `releaseBuild(project,
   buildRef)`. `buildCommit` archives the commit into the sources store and
   creates a build that does not touch `spec.source`: for Dockerfile projects a
   BuildKit Job with `buildKey` over the archive and the tag
   `apps/<ws>/<app>/candidates:<sha12>` (`internal/controller/dockerfile.go:
   59-251` parameterised by the source); for buildpack projects a second kpack
   Image `<app>-candidate` whose `spec.source.blob` is the archive and whose
   tag is the candidates repository (`desired.go:578-666` parameterised;
   kpack's tag is immutable, `desired.go:540`); both under PriorityClass
   `shpyrd-build` so they stay out of the workspace quota
   (`internal/controller/quota.go:34-47`). `GET /api/projects/:slug/builds`
   lists candidates with their commit (`pkg/api/logs.go:242-268`). A green
   candidate becomes a release with `deployCommit` of the same commit (same
   archive SHA, no second build). A push to a non-default branch may start one
   and the outcome is reported on the branch's journal row and in the push
   answer. About two weeks after the first version; waits for #288 for
   isolation of untrusted builds.
2. **The sandbox.** A Pod in the project namespace from the current release's
   image (the `internal/controller/release_phase.go` pattern), PriorityClass
   `shpyrd-build`, no service account token, an init container from the
   `shpyrd-git` image running `git clone --depth 1 --branch <ref>
   http://git.shpyrd-system.svc:8090/<project ID>.git /workspace` with a
   capability credential: Basic password = HMAC-SHA256 with the `capability`
   key of Secret `git-internal` (not the `route` key the internal routes use,
   section 3.2) over `shpyrd-git-v1/<project ID>/<commit>/<expiry>/fetch`, the
   parameters in the username (the `SourceCapability` shape,
   `pkg/objectstore/sourcecap.go:11`), verified by the git service itself
   without a server round trip; the advertisement for a capability request
   shows only that commit's ref (an advertise-refs filter in the service before
   the child runs, or `uploadpack.hideRefs` with a per-request `-c`), and the
   child runs with `-c uploadpack.allowAnySHA1InWant=true` for this request
   alone; fetch-only, 2 hours; 8090 opens to namespaces labelled
   `shpyrd.io/project` for this. The agent runs commands in the sandbox through the existing exec
   plumbing (`ProjectExec`) under the AI gateway (#312) and never sees a
   person's token. A depth-1 clone can push a fix: the server has the parent.
3. **Fix commits.** Author `shpyrd agent <agent@<platform domain>>`, Committer
   `shpyrd`, trailers `Shpyrd-Intent: fix failed build 13` (or `guard-rail:
   <rule>`, or `production error: <signature>`), `Shpyrd-Trigger:
   build/<name>` or `release/v13`, `Shpyrd-Requested-By: <person or policy>`;
   pushed to the default branch with a push capability bound to (project, ref,
   expected old oid, expiry), so the credential itself is a compare-and-swap:
   a human push in between makes the agent's push "fetch first"; the agent
   fetches, rebases in its sandbox, re-runs the candidate build, pushes again;
   a conflict it cannot rebase cleanly is left on a branch `shpyrd/fix-<n>`
   with the message on the project page. The journal records actor `agent`
   with the trigger; the release note is the commit subject, so the dashboard
   reads "Version 14, the platform fixed the failed build of version 13". The
   order is the decided one: failed builds first (the `Built` condition False
   after a push by a person; the agent reads the build log through
   `pkg/api/logs.go:299`), then guard-rail fixes (the pre-receive refusals and
   warnings are the input), then production errors (logs, metrics and the
   `release.failed` audit, `app_controller.go:1277-1330`). One agent run at a
   time per project, from a queue in the control-plane database.
4. **Identity and roles.** A per-project machine principal `agent` with the
   developer role, shown in the members list. This is new in `pkg/authz`:
   today the only non-person credential is the admin token
   (`authz.go:250-253`) and tokens need an active person as owner
   (`tokens.go:182-184`). It has no HTTP token, only capabilities minted by the
   server for a sandbox; audit `clientOf` `agent`; a workspace can switch the
   agent off per project. Its shape is an open question to settle before the
   agent slice, not before the first version.
5. **MCP tools** (`ee/mcp/mcp.go:77-111`; `Host` gains a POST-with-body method
   beside `ServeAPI`, `pkg/api/host.go:139`): `read_file` and `list_tree` at
   a commit or the default branch (bounded: 1 MiB per blob, 5,000 entries,
   depth 60, through the catalog routes, never resolving symlinks across the
   tree), `list_versions` (releases with commits in words), `propose_change`
   (a set of edits committed on `shpyrd/proposal-<n>` through
   `/repository/changes`, author the person, committer shpyrd, trailer
   `Shpyrd-Via: <client name>` from `ctxOAuthClient`, with a candidate build;
   answers with the diff in words), `deploy_branch` (fast-forward main to that
   commit through the same receive path, ProjectDeploy), `build_candidate`,
   `rollback_version`. Scopes: reads on `projects:read`; writes on
   `projects:write` with the consent text extended to "change its code", or a
   `code:write` scope later (five places in `oauth.go`; a new scope needs
   re-consent of every connected assistant). Deploy results are polled through
   `/builds` (`logs.go:270`) since MCP has no server streams
   (`mcp.go:470-477`).
6. **Conflicts between a laptop and the agent.** The CLI's fetch-and-merge
   handles the common case; the rare true conflict is a file list the CLI
   prints; handing it to the agent is a sandbox task (clone both heads, merge,
   commit "Merge the laptop's version 15 with the platform's version 14",
   push), offered when the CLI reports the conflict and the person says yes.
7. **GitHub-sourced projects.** The sandbox clones the mirror (fetch scope
   only); fixes must go to GitHub and need `contents:write` on the App (every
   installation re-approves; branch protection on main means a branch, and a
   pull request is out of scope), so the agent's write path for these waits
   for that decision. A second, write-capable App beside the clone App is the
   alternative to widening the one that exists.

## 10. Testing

- Unit: the shim protocol and every pre-receive decision (ref names,
  fast-forward, the default branch, caps, the pack-index and tree walk, the
  secret-file list, LFS pointers, `.gitmodules`); the Basic identifier and its
  challenge; the slug middleware; the signed trust header and its expiry; the
  bundle manifest and chain; the recovery sweep's three outcomes (applied,
  superseded, failed); `deployCommit` and the `Blob.Ref` check; the
  `AnnotationRollbackTo` consumption on a failed build; the archive garbage
  collector's keep set.
- Integration, real git against the service in a container: clone (v0 and v2,
  shallow), push, non-fast-forward, over-cap, atomic, default-branch delete,
  an LFS pointer, 401, 404, 403, 423, mirror refusal, a gzipped body, the
  sideband one-liner, the opt-in build relay against git 2.4x and 2.5x (open
  question 1), 500 concurrent clones under the semaphore.
- Controller tests beside `desired_test.go:41` for the same-archive rule and
  the rebuild rule of section 3.8; `pkg/backup` round trips at `FormatVersion`
  1 and 2.
- End to end on kind: a push from a laptop without a git binary (the CLI's
  go-git path, with git hidden from PATH) builds and releases; a second clone
  pulls; "Build version 7 again"; a restore of every repository from bundles
  alone with `git fsck --full`; a GitHub-sourced export of a private repository
  against the fake GitHub.
- On staging: the timed fleet restore and the node kill of section 5.6, with
  durations recorded.
- Chaos, in slice 9 (section 11): kill the pod mid-push, fill the disk, delete
  a bundle, restore one project, kill the node.

## 11. Slices in order, with effort

One engineer who knows the platform. Weeks are calendar weeks of one person.
The first draft put half the system in a three-week first slice; it is split
here into three and sized by what each contains.

| Slice | Weeks | What lands | Tests |
|---|---|---|---|
| 1. The image and the component | 1 to 2 | Dockerfile stage and `shpyrd-git` image with the version assertion; `deploy/components/git` (PVC, Deployment, Service, NetworkPolicy, PDB, ServiceMonitor, the mounted Secrets); the `git-internal` hook; `SHPYRD_GIT_SIZE` and `SHPYRD_GIT` vars; rc4 placement and pool pinning; the `git-server` subcommand (stateless-rpc plumbing, gzip bodies, deadlines, weighted semaphore, Lease, SIGTERM handling) | A real git client against the service on kind: clone, shallow, push |
| 2. Policy and journal | 3 to 4 | The hook shim and socket with the pre-receive policy (ref rules, fast-forward, caps, the pack-index and tree walk, secret-file list, LFS, `.gitmodules`); the server's internal routes (pre-receive, post-receive, archive with the 2 GiB limit) and the catalog, journal and lease tables; the 5-minute recovery sweep; repository create and soft delete from `createApp` and `deleteApp` | Unit tests for the shim protocol and policy; integration: non-fast-forward, over-cap, atomic, default-branch delete, LFS |
| 3. The API and the front door | 5 | The API routes with the Basic identifier and challenge, the signed proxy with the Lease-holder check, `workspaceOnlyPath`, `machinePath` and the CSP exemption, the companion Ingress per door, the handover hold, the rate limits in the API and in the service | Integration: 401, 404, 403, 423, mirror refusal; a load test with 500 concurrent clones |
| 4. Build integration | 6 to 7 | `deployCommit` through `mutateApp` and the `Blob.Ref` check; the pending-deploy worker exporting the archive with the server-side Aptfile translation; `Release.Commit`, `SourceKind`, `BuildNumber`, `RollbackOf` with CRD regeneration; rollback by rebuild with the `AnnotationRebuildAt` rule of section 3.8 and the `RollbackTo`-on-failure fix; the archive garbage collector; the opt-in build relay (`buildLogs` onto an `io.Writer`, the held-back flush) with its acceptance test against native git; audit words | Controller tests beside `desired_test.go:41` for the same-archive rule and the rebuild rule. **First push-to-deploy works at the end of week 7** |
| 5. The CLI | 8 to 9 | go-git replaces exec git in `deploy.go`; the plain-folder flow (`.gitignore`, size and secret-file checks, commit as the person, remote, credential helper line, push with push options); the existing-clone flow (fetch, fast-forward, `git merge` when a binary exists, refusal with files otherwise); the kubeconfig archive from go-git; `shpyrd pull`, `git url`, `git-credential`, `--commit`, `rollback --rebuild`, releases in words; docs for both kinds of people | Against a kind cluster and the credential protocol |
| 6. Durability and recovery | 10 to 11 | Bundles to the backup target (incremental and full by the policy of section 5.2, manifests, age encryption, retry); the separate-bucket Terraform in the cloud repository with the PUT-only and read credentials, locked retention and replication plus the probe; `git bundle verify`; the hourly sample restore that clones, and the alerts; the nightly reconciliation; the maintenance loop (repack with bitmaps, gc with grace, purge, disk expansion); platform backup `FormatVersion` 2 with `git/index.json` and the restore callback; the project archive entry and restore participant; the workspace repositories export; `shpyrd-ctl project restore-repository`; a timed fleet-restore drill script and its first measured run on staging, including an unplanned node loss | The drill itself |
| 7. GitHub mirrors | 12 | Fetch-on-pin with the `<app>-git` credential, no mirror over the cap, mirror refusal, export using the mirror's bundle or the credentialed helper clone, `mirror_of` in the catalog and the source-kind transitions of section 8, dashboard wording | Watcher tests with the fake GitHub |
| 8. The dashboard | 13 to 14 | A design session per `design/DESIGN_FOR_AGENT.md`: the repository block, versions in words, the two rollbacks, Activity marks, Mock data with commits, docs pages; API additions (repository in detail, commits, start-from-running, `repository/changes`) | The design session's own |
| 9. Hardening and release | 15 | Fsck severities and the pack-index walk under load, secret-file warning, rate limits, the suspended-workspace rule, failure messages reviewed from a non-developer's seat, chaos tests (kill the pod mid-push, fill the disk, delete a bundle, restore one project, kill the node), the runbook of section 5.7, roll on the dev cluster then production | Chaos tests |
| 10. The agent's first slice | 16 to 18 | Machine principal and capability credentials, sandbox pod with the clone init container and the NetworkPolicy opening on 8090, fix commits with trailers for failed builds, the per-project queue, MCP `read_file`, `list_tree`, `propose_change`, `deploy` and `rollback` with the POST seam in `Host` | End to end on kind |
| 11. Candidate builds | 19 to 20 | `buildCommit` and `releaseBuild`, the candidates tag, the second kpack Image, `/builds` listing candidates | Controller tests |

Total: 15 weeks to a first version, 18 with the first agent slice, 20 with
candidate builds. The largest uncertainties are the hook shim (weeks 3 and 4),
the opt-in relay (week 7), the separate backup bucket depending on the cloud
repository (week 10), and the design session's scope (weeks 13 and 14).

## 12. Decisions against the issue's open questions

**Open question 1: object storage or a shared volume for the live
repositories?** Neither as written. A ReadWriteOnce block claim behind one git
service pod holds the live repositories (git's lockfile-and-rename for refs, no
dependence on OCI `If-Match`, snapshotted nightly by the existing job once the
claim names its storage class); object storage holds per-repository bundles as
the durable copy, written within seconds of a push to the platform backup
target, in production a separate physical bucket that can be retention-locked
and replicated. Object storage as the authority with a disk cache is kept as
the next step, since the layout stays convertible; a shared RWX volume is
refused by GitLab for repositories, is absent on kind, has ownership that does
not match the service's uid, and adds nothing to a single writer. Section 4.

**Open question 2: does `shpyrd deploy` always commit, or only when the
project opted in?** Always. Every build is a commit: the CLI turns a plain
folder into a clone and pushes; an uploaded archive (old CLI, dashboard,
kubeconfig session, restore) is committed server-side with Author the
signed-in person and Committer shpyrd before it is deployed. Opt-in would leave
two kinds of history and an agent with nothing to work on for the projects
that matter most. Section 3.5.

**Open question 3: push mirror to GitHub or GitLab in the first version?** No.
The first version has only a read-only mirror in the other direction:
GitHub-sourced projects get their pinned commits fetched into the platform
repository at pin time with the GitHub App's `contents:read` token, so exports
cover private repositories and the sandbox can clone; pushes to a mirror are
refused with the GitHub URL. A push mirror would need `contents:write` on the
App manifest now and break the promise that installations never approve
again. Section 8.

The other decisions this document takes, each with its section:

| Decision | Chosen | Rejected, and why | Section |
|---|---|---|---|
| Who speaks git on the server | the git binary as `--stateless-rpc` children of a Go service, policy in a hook shim | go-git as the wire (22 to 40 times slower clones, no thin packs, no atomic push, no fsck or bundle, beta API, the advisory cadence); Gitea as a component (a second product, a trusted header as the whole boundary, no quota, lossy identity); `git http-backend` through `net/http/cgi` (chunked push bodies fail) | 3.1, 3.2 |
| How a commit reaches the build | an archive in the sources store, `spec.source.blob` through `mutateApp` | a git source with credentials (new credential kind, upload-pack to project namespaces, kpack's go-git fetcher on crafted repositories, the watcher re-pinning on rollback) | 3.5 |
| Rollback | both: pin the image ("Run version 7 again") and build the commit again ("Build version 7 again") | only one of them | 3.8 |
| What a plain `git push` answers | one line after report-status; the build log opt-in through the CLI's push option | streaming the build into every push (holds a coding tool's push for minutes; Heroku's shape) | 3.3 |
| Where the routes live, who authenticates | `pkg/api` at the engine root; Basic with personal or session tokens; OAuth with the license; the admin token refused; 401 Basic for anonymous, 404 for a valid credential without view | an ee root extension (`require()` unreachable); accepting the admin token; 404 for anonymous (viewers' clones would never prompt) | 3.7 |
| Pre-seeding existing projects | never automatic; an explicit admin import on an empty repository | automatic import as the first generation (a non-fast-forward of unrelated histories on the laptop's first push) | 8 |
| Which ref operations are refused | non-fast-forward updates on every branch, deletion of the default branch, names outside `refs/heads/` and `refs/tags/`; checked in the shim before journalling | `receive.denyDeletes` for every ref (breaks coding tools' branch cleanup); protecting "tags a kept release names" (releases name commits, nothing creates such tags) | 3.3 |
| Archive and deploy on a push | asynchronous: post-receive journals and answers at once; the pending-deploy worker exports and deploys from the row | exporting and deploying inside post-receive (holds the push for the archive and a gateway write; a gateway restart fails the push) | 3.3, 3.4 |
| Bundle cadence | a full when the chain passes 20 increments or doubles the full, at most weekly, at least every 60 days | a weekly full regardless (about nine fulls per repository in the bucket at 60-day retention) | 5.2 |
| Mirrors of large GitHub repositories | none over the cap; the export uses `git archive` of the pinned commit through the credentialed helper | a shallow mirror of the last N commits (its bundle verifies but cannot be cloned) | 8 |
| Git LFS | refused in pre-receive with a sentence | accepted (a pointer file builds a broken application) | 3.3 |
| Default cap | 500 MB, settable to 2 GB | 200 MB (only needed when serving from a cold cache; the sampled p90s are 12 to 224 MB) | 4.3 |
| The locked backup copy | the platform backup target, a separate physical bucket | a gateway logical bucket (a prefix; no lock; the production bucket is versioned, which OCI forbids with retention rules) | 5.1 |
| Component placement and database access | rc4; the git pod never opens Postgres; EdDSA-signed trust header | rc3 with a hook creating a database role in the same parallel runlevel (races); a shared Secret alone for the proxy trust | 3.2 |
| CLI on divergence | fetch; fast-forward with go-git; `git merge` when a binary exists; otherwise refuse with the files named; a dirty tree refused unless `--commit` | a three-way merge with go-git (it merges fast-forward only) | 3.6 |
| Renamed project, suspended workspace | 301 for 30 days, then 404; clones for 30 days after suspension, pushes refused (to confirm) | 404 at once; refusing clones on suspension | 3.7 |
| Branch and tag deletes | allowed except the default branch | refusing every delete | 3.3 |
| Node-loss RTO | stated as a 10-minute target to measure | a 1 to 5 minute figure with no source | 5.3 |
| Image base | Alpine stable branch, pinned package | Chainguard git (free tags are `latest` only); Alpine edge (not pinnable) | 3.2 |

## 13. Open questions, and the claims that were verified, disputed or refuted

### 13.1 Open questions

1. Does native git 2.4x to 2.5x tolerate band-2 lines arriving after
   report-status while the final flush is held back (the opt-in build relay)?
   Acceptance tests decide; the fallback is the CLI following the build through
   the existing `/builds` log stream; git 2.56's `receive-report` hook is a
   later alternative.
2. What is the measured RTO of an unplanned node loss on OCI and AWS, given the
   attach-detach controller's force-detach wait (about 6 minutes, unverified)
   and the availability-domain scope of OCI block volumes (unverified)? The
   first drill on staging must include a node kill; the data pool may need a
   second node in the volume's availability domain.
3. Does the cloud repository's Terraform gain the separate physical backup
   bucket (PUT-only and read credentials, locked retention after the 14-day
   window, cross-region replication to a read-only destination) before the
   production rollout, do the platform backup target and the control-plane
   Barman backups move to a cross-region bucket with it, and who owns the
   replication probe and the passphrase kept outside the region (section 5.1)?
   Until then bundles land in the gateway's `platform-backups` logical bucket
   with only the 7-day versions as undo, and region loss is not recoverable.
4. Does OCI's S3 compatibility endpoint honour `If-Match` and `If-None-Match:
   *`? Oracle's native API documents both; the platform's experience says
   `If-Match` is ignored. A direct test (PUT with a wrong ETag, expect 412)
   on both endpoints decides whether the bucket-authoritative next step can
   use a create-only PUT as its compare-and-swap.
5. OCI prices to confirm with Oracle: that Object Storage cross-region
   replication traffic falls under the outbound-transfer allowance; and the
   AWS compute figure, which is an estimate.
6. Should the suspended-workspace rule allow clones for 30 days after
   suspension while refusing pushes? `requireTenant` answers 403 for
   everything today (`pkg/api/members.go:368-386`); a product decision.
7. Do OAuth access tokens minted for `/mcp` (audience `<dashboard>/mcp`) get a
   git audience, or does `projects:write` with extended consent text cover
   pushes until a `code:write` scope exists? A new scope needs re-consent of
   every connected assistant.
8. How many data-pool nodes and how much headroom should `SHPYRD_GIT_SIZE`
   default to per profile (200Gi proposed on cloud, 20Gi local), and does the
   70 percent auto-expansion request need an operator gate on OCI's 50 GiB
   minimum class?
9. Should personal API tokens travel in the store dump so git remotes survive
   a platform restore? `Dump` has no tokens today (`pkg/store/store.go:1116-
   1127`); the alternative is documenting that remotes authenticate again
   after a restore.
10. How does the per-project machine principal `agent` look in `pkg/authz` and
    in the members list? Needed before the agent slice.
11. What is the scope of the dashboard design session, and does it hide
    12-character revisions and digests for every project or only for embedded
    ones?
12. Should the sources store's archive garbage collector also apply to blob
    deploys made before this feature (the existing unbounded growth), and is a
    30-day grace after a release leaves the 20-kept history enough for
    operators who roll back further?
13. Is 10 pushes a minute per actor right for coding tools that commit after
    every action? Measure on the dev cluster and adjust before production.

### 13.2 Claims and their status

| Id | Claim as first stated | Status | What this document does |
|---|---|---|---|
| C1 | A single-replica Deployment with `Recreate` on a ReadWriteOnce claim on the data pool is an accepted platform shape (Garage, the registry), and the snapshot job covers it | **Disputed.** The citations are accurate as text, but Garage is skipped on every cloud profile (`pkg/install/engine.go:271-273`), the registry has no `pool:` and goes stateless over the gateway when a registry bucket is set, both cloud profiles ship `SHPYRD_DATA_POOL: ""`, and the snapshot job matches claims by exact storage class (`pkg/snapshots/snapshots.go:259-266`) while Garage's and the registry's claims name none | States the repository disk as a new shape; sets `storageClassName` explicitly and conditionally; says the pod lands on the platform pool without a data pool; says kind has no snapshot (sections 3.1, 4.1) |
| C2 | The installer applies a runlevel's components in parallel and the server last; with a sources bucket the server runs two replicas and nothing on a ReadWriteOnce claim | **Confirmed** (`engine.go:320-336`; `sources.go:14-71`; `sources_test.go:17`). Precision: the server is last within its runlevel, which is rc4 on every profile | Places the component in rc4 as its own Deployment (section 3.2) |
| C3 | A gateway logical bucket is a prefix; the gateway answers `ObjectLockConfigurationNotFound` to lock, retention and legal hold; `RetentionDays` is a deletion sweep; the production bucket is versioned; OCI forbids retention rules with versioning | **Confirmed in substance, disputed in wording.** Only the lock-configuration read answers that code; retention and legal-hold reads answer `NoSuchObjectLockConfiguration`; the writes answer `NotImplemented`; an aliased bucket serves `buckets/<alias>/` | Section 5.1 carries the corrected statement; the conclusion (a separate physical bucket) is unchanged |
| C4 | A blob deploy sets `spec.image` empty and `spec.source` to the blob; kpack and BuildKit fetch on 8082 with a capability URL; `sourceID` shortens `Ref`; nothing deletes an archive | **Confirmed.** Precisions: the blob case is at `apps.go:857-859`; the token is appended only when the signing key is set; `sourceOf` reuses the first release's string for a re-released image | Build integration (section 3.5) |
| C5 | The same archive name at another URL is not a new source for kpack; the BuildKit build key changes only through `AnnotationRebuildAt`; the rollback annotation is consumed only when `spec.image` is set or `Built` is True | **Refuted** in the middle part. `buildKey` hashes the archive SHA, the git URL plus pinned commit, `subPath`, `spec.build` and `AnnotationRebuildAt`; a new archive SHA builds by itself. The kpack rule applies only while no rebuild is pending. The consumption condition also includes `!HasSource()`; a failed rebuild with `spec.image` empty leaves `AnnotationRollbackTo` and re-applies the snapshot each reconcile | Section 3.8: `AnnotationRebuildAt` only when the target archive equals the current one; consume `AnnotationRollbackTo` on `Ready == "False"` |
| C6 | Project namespaces reach the server only on 8082; kpack re-fetches sources on every stack or buildpack update; kpack's fetcher is go-git asking for depth 1 | **Refuted** in the middle part. kpack v0.18.0 rebases without fetching on a stack-only update with an unchanged stack id; it re-fetches on buildpack, lifecycle, config, commit and trigger rebuilds. The other two parts hold | Section 3.5 narrows the argument; the `storage.yaml:1-3` comment is to be corrected |
| C7 | The Basic password can be resolved by the existing identifier loop through `resolveAPIToken` and the OAuth identifier; today's challenge is Bearer; slugs forbid dots; `Host.Sign` and `Verify` use the EdDSA keys | **Confirmed**, with precisions: `auth()` itself cannot be the middleware (its abort writes the Bearer challenge at once, and git never sends Basic to a Bearer-only challenge, tested); the synthetic header is `X-Shpyrd-Token`; the slug must be mutated under the param name `slug`; the admin token is compared after the loop; open mode and the workspace-host binding apply | Section 3.7 |
| C8 | git runs pre-receive before `update()` applies `deny_deletes` and `deny_non_fast_forwards`, writes report-status before post-receive, flushes after post-receive, and ignores v2 for push | **Confirmed**, with conditions: the cited line numbers are current master only (cite functions); report-status only when the client asked, and "every git since 1.7 asks" is unsourced; post-receive runs with `skip_broken=1` and not at all when every command failed; a command can still fail after pre-receive; git 2.56 adds a `receive-report` hook | Sections 3.3 (write path) and 3.4 |
| C9 | go-git v6 beta served a 211 MB clone in 230 s and 984 MB RSS against 9.2 s and 262 MB for `http-backend`, and received a push in 2.59 GB against 621 MB; it advertises `no-thin` and no `atomic` per COMPATIBILITY.md; ten advisories in 2026 | **Disputed.** The clone-time gap reproduced (22 to 40 times); the memory figures did not (go-git 0.3 to 1.3 times git's RSS, and the faster receiver) and are marked unverified; "no atomic" comes from `serve.go`, not COMPATIBILITY.md, which lists `atomic` as supported on the client side; the advisory count is ten dated 2026-02-09 to 2026-07-30 on the repository's security page (GitHub's global database dates the last two 2026-08-07); the "0.1 s" one-commit push was native git pushing into a go-git server, not a go-git client, and the "6 MB" binary figure is a v6 beta server, not the v5.19.x client | Section 3.1 rests the wire decision on clone time and capabilities, not memory; section 3.6 states the attributions |
| C10 | go-git's merge is fast-forward only and rebase is unsupported | **Confirmed** at v5.19.3, v6.0.0-beta.1 and main; even the fast-forward `Merge` only moves the ref; PR #2542 (open) adds three-way tree merges for v6 | Section 3.6 |
| C11 | Alpine's git 2.56.0-r0 is 7.1 MiB with five dependencies; Chainguard's free images expose only `latest`; Gitaly requires git 2.47.0; git 2.45.1 fixed CVE-2024-32002 and four siblings | **Confirmed**, with scope: 2.56.0-r0 is Alpine edge; stable v3.24 ships 2.54.0-r0 at 6.7 MiB; Chainguard's Catalog Starter is a gated exception; Gitaly's floor is a build-time constraint on its embedded git, not a runtime check | Section 3.2 uses the stable branch and keeps 2.47.0 as a runtime floor |
| C12 | Atlassian measured a 220 MB clone at about 270 MB and 100 to 120 percent of one CPU, with 1.5 times the repository per clone and one CPU per two clones; a local 81 MB repository packed in 84 MB RSS and 0.3 s, or 239 MB and 24 s without delta reuse | **Disputed.** The Atlassian page is real but the measurement is from 2013 without bitmaps, the rule is `1.5 x min(size, 700 MB)` scoped to repositories up to 400 MB, and 270/220 is 1.23; the local figures came from a repository not in the tree; reproduced: with a bitmap RSS is 1.07 to 1.10 times the pack, without one it tracks object count (up to 6.05 times at a million objects); `pack.threads` defaults to the node's CPUs; `--no-reuse-delta` is ignored when a bitmap permits reuse | Section 4.3 weighs the semaphore by bytes and objects, gives repositories without a bitmap the exclusive slot, pins `pack.threads`, and writes bitmaps explicitly |
| C13 | OCI replication copies only objects uploaded after the policy, propagates deletes, one policy per bucket, read-only destination, no console metrics; OCI cannot combine versioning and retention; S3 Object Lock requires versioning and replication does not replicate deletes by version ID | **Confirmed**, with additions: the locked bucket cannot be a replication destination (no rules on a destination, no replication onto a bucket with rules, destination unversioned, previous versions not replicated), so it is written directly; `GetReplicationPolicy.timeLastSync` exposes the lag without a probe | Section 5.1 |
| C14 | At list prices, 10,000 projects at 20 MB cost about 89 USD a month on OCI and 121 on AWS; at 200 MB about 544 and 671; File Storage or EFS 58.59 and 585.93 for the live copy | **Disputed.** The prices hold; the OCI egress rate used (0.0085) is the North America and Europe SKU while production is in `sa-saopaulo-1` at 0.025; the 200 MB totals were not derivable; 585.93 is 585.94; the AWS tiers should be stated; the AWS column is not region-matched. The first draft's own 200 MB table (536.79) was wrong too: its live-disk line was 2,500 GiB against its stated 2.5-times rule, its bundle line assumed 2.5 times used bytes against a weekly-full policy, its replica-transfer line ignored the fulls, its "about 11 TB" did not sum its lines, and OCI's free allowance was applied three times | Section 6 provisions the disk by the rule of section 4.1 and bundles by the policy of section 5.2, derives the request count, applies the allowance once, adds replication to the OCI egress ceiling, and gives the 1,000-project fleets |

Unverified beyond these: the memory figures of C9 (kept out of the argument),
the AWS compute estimate, the OCI replication-traffic SKU, the node-loss RTO
with the force-detach wait and the availability-domain scope of OCI block
volumes, the band-2 relay behaviour, and the size of the go-git client in the
CLI binary, each marked where it is used.

**Mechanisms of the first draft the review found wrong**, and where this
version fixes them:

| First draft | Why it was wrong | Fixed in |
|---|---|---|
| The shim walked the quarantine with `git cat-file --batch-check --batch-all-objects` | that enumerates the whole repository and its alternates on every push and reports no tree depth | 3.3 step 3(d): the quarantine pack's index plus a bounded tree walk |
| post-receive exported the archive and ran the deploy, "within seconds" | a 500 MB to 2 GB archive through the single-writer gateway is not seconds; a gateway restart failed the push | 3.3 step 5 and 3.4: the pending-deploy worker |
| The recovery sweep marked "ref not equal to `new_oid`" as failed | a push superseded by a later push was misfiled as failed | 3.4: `merge-base --is-ancestor` |
| A mirror over the cap kept the last N commits | a bundle from a shallow repository verifies but cannot be cloned | 8, 5.4, 4.6 |
| "Tags a kept release names" were protected from deletion | releases name commits; nothing creates such tags | 3.3 step 3(b), 12 |
| All rate limits "live in the git service" | the service never sees `Authorization`; the failure counter is the API's, per replica | 3.7 |
| Region loss restored from "the platform backup" | the backup target, the control-plane backups and the passphrase default to the same region | 5.1, 5.3, 13.1 |
| The git pod's credentials were unspecified; "a credential that can only PUT" also read | the sample restore, the replay and `restore-repository` read; the bundler decrypts | 3.2, 5.1 |
| A 2 GB cap with a 512 MiB archive route | a repository above about 500 MB could not build | 3.3 step 5, 3.5, 4.3 |
| Uploaded archives were committed on top of `main` without a check | a stale folder silently reverted later pushes | 3.5, 8 |
| git "rewrites its base URL" after a 301 | only for that session; every push redirects again | 3.7 |
| GitLab Dedicated's 8 h / 4 h cited "for the same shape" | Dedicated is 4-hourly snapshots without per-repository bundles | 5.3 |
| "Force-detach after about 6 min" and "AD-scoped" stated as fact | no source read for this document | 5.3, marked unverified |
| Live disk at 2,500 GiB, bundles at 2.5 times used, "about 11 TB", allowance subtracted three times | arithmetic against the document's own rules | 6.3, 6.4 |

## 14. Prior art consulted

Read on 2026-10-28 and 2026-10-29 unless dated otherwise.

**Platforms with a git remote per app.**

- Heroku: the git article (devcenter, updated 2026-08-27) and limits (updated
  2026-10-14): HTTPS only since 2022-03-14, the API key as the Basic password,
  only `main` or `master` builds, 1 GB checkout, 600 MB slowness warning, 75
  git requests an hour per user per app, no LFS, the build in a pre-receive
  hook with the log as `remote:` lines, Ctrl+C detaches without cancelling;
  "Heroku Git is not intended to be a stable git repository". The April 2022
  incident review (heroku.com/blog, 2022-06-14) and GitHub's security alert
  (github.blog, 2022-04-15): stolen GitHub OAuth tokens, every token revoked,
  the GitHub App model still not adopted as of the 2026-08-07 integration doc.
- Dokku (dokku.com/docs; `plugins/git` on GitHub): a bare repository that is
  the app directory, a pre-receive hook over SSH, `repo:gc` and
  `repo:purge-cache` copied from Heroku's plugin. Piku (`piku.py`, last
  commit 2026-09-04): about 1,500 lines, a forced SSH command and a
  post-receive hook. Deis Workflow's builder (archived; maintenance ended
  2018-03-01): an SSH git server that `git archive`d to object storage.
- Aptible (docs, deploying with git), Upsun (philosophy and clean-repository
  pages; the GitHub integration page: once GitHub is connected the hosted
  repository is a mirror and direct pushes are overwritten), Azure App Service
  Local Git (learn.microsoft.com, 2025-06-26, whose error table lists the
  accepted-then-failed wrong-branch push).
- Why Render, Fly.io, Railway and Vercel build only from a provider or a CLI
  upload (their deploy and CLI docs, 2026): no published rationale; Railway's
  `up` streams the build after a quick accept and allows detach, which this
  design copies. OpenShift v3 dropped v2's integrated git for webhooks
  (redhat.com). Google Cloud Source Repositories closed to new customers
  2024-06-17 and stayed closed; AWS CodeCommit closed 2024-07-25 and
  **reopened to new customers on 2025-11-24** with unchanged pricing (AWS
  DevOps blog; docs history), which corrects the issue's premise.
- Netlify, "How we built Git storage for millions of Netlify projects"
  (2026-09-22): git binary on bare repositories, S3 as the durable truth with
  refs as JSON records updated with `If-Match` and `If-None-Match`, a local
  disk cache per pod, a per-repository mutex, pods rebuilt from S3; 93 percent
  of new projects arrive without a connected git host.

**Hosts and their storage.**

- GitHub: "Introducing DGit" (2016-04-05) and "Building resilience in Spokes"
  (2016-09-07): three replicas on local disks, "storing the repository in a
  distributed file system is not viable"; "Stretching Spokes" (2017-10-13): the
  XOR-of-hash ref checksum this design uses; "Building Git infrastructure for
  agent-scale development" (2026-10-06) and "The bridge to agent-scale Git
  infrastructure" (2026-10-21): authoritative data moving to Azure Blob Storage
  with cache workers, per-reference quorums, pushes up 4.9 times year over
  year, agents committing after nearly every action. GitHub repository limits
  and large-files pages: 100 MiB per file, 2 GB per push, 6 pushes a minute and
  15 reads a second recommended per repository, 3,000 entries per directory,
  50 levels. "Counting objects" (2015-09-22, updated 2024-07-23): bitmaps.
- GitLab: Gitaly administration ("only local storage is supported"), the NFS
  page, Praefect (RPO under 1 minute, RTO under 10 seconds for one node),
  Gitaly cgroups and concurrency limiting, consistency checks (fsck on push
  with three cosmetic ids ignored), repository checks (monthly fsck), Geo
  background verification (the refs checksum), the `gitaly-backup` manifest
  layout (never modifies or deletes an uploaded file), server-side incremental
  backups, bundle URIs, the Dedicated DR page (RPO 4 h, RTO 8 h), the
  GitLab.com runbooks (`gcp-snapshots.md`, `recovery.md`,
  `recovery-measurements.md`, `deleted-project-restore.md`, `gameday.md`), the
  2017 postmortem (2017-02-10), the "Scaling Git" design document (created
  2026-06-16, a proposal) and the rejected transaction-management document.
  Secret push protection page (1 MiB blobs; skip above 3,150 paths or 350,000
  lines) and push rules (the secret-file list).
- Sourcehut: "Backups and redundancy at sr.ht" (2019) and the 2024-01-19
  outage post-mortem (about 170 hours; backups up to 12 hours old).
- Codeberg: "Letter from Codeberg" (2023-02-08) and infrastructure planning
  issue 4 (borg too slow on many small repositories); the FAQ (removed
  objects kept about 30 days).
- Gitea: config cheat sheet (repositories on local disk; object storage for
  attachments and LFS only; nightly fsck), backup-and-restore (the instance
  must be stopped), authentication (reverse-proxy auth covers git over HTTP),
  the HA guide and Helm chart (RWX, cron on every replica); pull request
  #21820 (size limit, draft since 2022). Forgejo: GPL-3.0-or-later since v9.0
  (2024-10-16), soft quota, chart README ("not HA ready"). Gogs: v0.14.3,
  experimental API, docs repository archived 2026-02-07. soft-serve: sunset
  and archived 2026-10-26.
- Bitbucket Data Center: "Scaling Bitbucket Data Center" (last modified
  2024-05-22) and its Stash 2.4 (2013-04-22) and 3.2 (2014-06-01) ancestors;
  "Clustering with Bitbucket" (NFSv3, a dedicated shared-storage machine).
- Gerrit `config-gerrit.html`: receive timeout 4 minutes, transfer timeout 1
  minute, thread pools, `maxBatchCommits`. JGit `ReceivePack.java`: the
  setter list (object and pack size limits, `maxCommandBytes` 3 MiB, hooks,
  ref filters) and the DFS commit of 2011 ("DFS: A storage layer for JGit").

**git itself.** `gitprotocol-http`, `git-http-backend`, `git-receive-pack`
(the quarantine environment), `githooks`, `git-bundle`, `bundle-uri`,
`git-fsck` and `fsck-msgids.adoc`, `git-check-ref-format`, `reftable`,
`gitcredentials`, `git-credential`, `git-credential-store`, the `osxkeychain`
and `libsecret` helpers in `contrib/`, `Documentation/config/{core, receive,
transfer, gc, protocol}.adoc`, `builtin/receive-pack.c` (master and tags
v2.43.0 to v2.56.0), `send-pack.c`, `builtin/pack-objects.c` and
`thread-utils.c` (the `online_cpus()` default); "Securing Git: addressing 5
new vulnerabilities" (github.blog, 2024-05-14); the git bomb (kate.io, 2017);
a local test on 2026-10-29 with git 2.50.1 that a bundle made from a shallow
clone passes `git bundle verify` and fails to clone.

**go-git and alternatives.** go-git releases (v5.19.3 and v6.0.0-beta.1,
2026-10-04), COMPATIBILITY.md, `plumbing/transport/serve.go` and
`receive_pack.go` at v6.0.0-beta.1, `plumbing/transport/server/server.go` at
v5.19.3, the security advisories page (fourteen, ten in 2026), issues #1673
and #1774 (memory), PR #2542 (three-way merges), the Entire blog post
(2026-04-06). kpack v0.18.0 `pkg/git/fetch.go`, `go.mod`,
`pkg/apis/build/v1alpha2/build.go` and `build_pod.go`,
`pkg/reconciler/image/build_required.go`. objgit (tigrisdata.com/blog,
2026-06-23). libgit2-backends and git2go (no server side, bindings last
pushed 2024). awslabs/git-remote-s3. gitkit. The local benchmarks of
2026-10-28 and 2026-10-29, whose results are in sections 3.1 and 4.3.

**Clouds.** Oracle docs: object storage replication, retention rules,
versioning, storage tiers, the object storage overview, block volume backups,
the `ReplicationPolicy` model (`timeLastSync`); the OCI Go SDK's
`put_object_request_response.go` (`if-match` documented); the Oracle price
list API. AWS docs: S3 Object Lock, replication (what is and is not
replicated), Replication Time Control, conditional writes (2024-11-25), S3
performance guidance, `ListObjectsV2`, S3 Express One Zone; the AWS Price
List; the 2020-04-16 blog on `sa-east-1` egress. Alpine package pages (edge
and v3.24), Chainguard's container categories and lifecycle pages, Docker Hub
and code.forgejo.org image manifests.

**Repository sizes.** GitHub API `size` for 60 Lovable, 60 v0, 30 Bolt and 60
Replit repositories found by code search on 2026-10-28 (medians 0.6, 2.3, 2.4
and 2.5 MB; p90 12.5, 36.5, 107 and 224 MB; maxima 422 MB, 873 MB, 331 MB and
2.94 GB; a 500 MB cap covers 58 to 60 of every 60), baseline template-sized
apps (83 KB to 403 KB) and products with history (7 MB to 1.16 GB); Koesten et
al., Patterns 1(8), 2020-11-04, Table 2 (median 7.7 MB over 65,537 public
repositories carrying data files). The sample is public repositories found by
code search, not a random one; the right next measurement is the size
distribution of the platform's own uploaded archives.

**Security references.** OWASP Logging Cheat Sheet; GitHub push protection and
audit-log event pages; gitleaks `config/gitleaks.toml` and `go.mod` (222
rules; Aho-Corasick; RE2); trufflehog (AGPL, live verification, not used).
