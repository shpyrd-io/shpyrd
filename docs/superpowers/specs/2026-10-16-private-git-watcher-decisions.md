# Private repositories, PR 2: the source watcher (RFC-0018). Decisions

Date: 2026-10-16
Builds on: `docs/superpowers/specs/2026-10-16-private-git-repositories-design.md`,
section 2, "Noticing commits and building them". PR 1 (#223, git credentials) is
the base. This branch, `feat/private-git-watcher`, is stacked on it.

These decisions refine the spec for PR 2. Where the two differ, this file wins.

## Scope of PR 2

In:
- `GitSource.AutoDeploy`;
- a commit pinned for every git app on a branch;
- a source reconciler that resolves, pins and polls;
- the generic webhook (GitHub HMAC, GitLab token, Gitea HMAC);
- `[skip deploy]`;
- `shpyrd git-hooks`;
- `shpyrd deploy --no-auto-deploy` and `--auto-deploy`;
- the auto-deploy setting on `PATCH /api/projects/:slug`;
- the dashboard Source card;
- docs, RFC-0018 status, an end-to-end run with screenshots.

Out (PR 3 and PR 4, the GitHub App):
- `WatchPaths`, `WaitForChecks`;
- the GitHub App webhook;
- checks and Deployments.

Do not add those CRD fields now.

## Reading a branch's commit: a small built-in ls-remote

The server image is distroless and has no git binary, and the maintainer
chose not to add go-git. So `pkg/gitauth/lsremote.go` reads the ref
advertisement itself:

- **HTTP(S):** `GET <repo>.git/info/refs?service=git-upload-pack`.
  - Basic auth comes from the token credential, or nothing for a public repository.
  - Parse pkt-lines: the `# service=git-upload-pack` line, a flush, then `<sha> <ref>\0<caps>` and then `<sha> <ref>` lines, until a flush.
  - Send no `Git-Protocol: version=2` header, so servers answer in protocol v0/v1.
  - Treat 401 and 403 as an access error. A redirect to a sign-in page (an HTML content type) is an access error too.
- **SSH:**
  - Dial `host:port` with `golang.org/x/crypto/ssh` and the deploy key.
  - Check the host key against the deploy key's `known_hosts`. Parse it with `ssh.ParseKnownHosts`; do not use files.
  - Open a session, run `git-upload-pack '<path>'`, read the advertisement until the flush, write `0000`, and close.
  - The path is `org/repo.git` for the scp form and `/org/repo.git` for `ssh://`.
- **Output:** `LsRemote(ctx, cloneURL string, b gitauth.Build) (map[string]string, error)`, a map from ref name to SHA.
  - `b` is PR 1's resolved build: Kind, Secret.Data with username/password or ssh-privatekey/known_hosts, and CloneURL.
  - Also `ResolveBranch(refs, revision) (sha string, ok bool)`. It takes `refs/heads/<rev>` and falls back to `refs/tags/<rev>` (peeled `^{}` preferred).
- **Timeouts:** 20s per call. The response is capped at 4 MiB.
- **Tests:**
  - An `httptest` server that serves a recorded advertisement;
  - an in-process `x/crypto/ssh` server that answers `exec git-upload-pack` (the pattern is in `pkg/gitauth/keys_test.go`);
  - 401, malformed pkt-lines, a host key mismatch.

## Where the pinned commit lives

- **Spec:** `GitSource.AutoDeploy *bool`. Nil means true for a branch revision.
- **Pin:** two annotations on the App, written by the source reconciler and by the API on a deploy:
  - `shpyrd.io/git-commit`: the 40-hex SHA to build.
  - `shpyrd.io/git-commit-ref`: the revision it was resolved from.

  The pin applies only while `git-commit-ref` equals `spec.source.git.revision` (default "main"). A changed branch makes the pin stale.
- **Already a SHA:** if `spec.source.git.revision` is itself a 7 to 40 hex commit, nothing is watched and it is built as given.
- **Push:** the webhook writes one annotation, `shpyrd.io/git-push`, as JSON `{sha, ref, message, author, delivery, at}`. The source reconciler consumes it and removes it.
- **Status:** the source reconciler records what it saw in the annotation `shpyrd.io/git-source`, as JSON: `{latest, latestAt, message, author, via, skipped, lastWebhookAt, checkedAt, error}`.
- **Single writer of status:** the AppReconciler is the only writer of `status`. It copies `git-source` into a new `status.source` (type `SourceStatus`, same fields, plus the pinned `commit`). Nothing else writes status.

## The source reconciler

A new controller, `SourceReconciler`, also on App objects. It is wired in `pkg/server/server.go` `runControllers` beside the others. It reconciles only Apps with a git source on a branch, and:

1. **Credentials:** looks them up as PR 1 does. Refactor `internal/controller/gitcreds.go` so that both reconcilers share one lookup:
   `lookupGitBuild(ctx, c client.Reader, systemNS string, app) (gitauth.Build, error)`.
2. **A push annotation, if present:**
   - It is used only if its ref is `refs/heads/<branch>`; otherwise it is dropped.
   - A message containing `[skip deploy]` (or `[deploy skip]`, case-insensitive) is recorded as skipped, with `git.skip` audited, and not pinned.
   - Otherwise the SHA becomes `latest`.
   - `lastWebhookAt` is set either way.
3. **No push:** runs LsRemote and takes `latest` from it. There is no message or author in that case.
4. **Pinning:**
   - With `AutoDeploy` (nil or true), when `latest` differs from the pin, or the pin is stale or missing, it writes the pin annotations.
   - It writes the release note `Push a1b2c3d by alice: fix login` (or `Commit a1b2c3d (polled)`) and `deployed-by: git:<author> via push|poll`, through the same annotations a deploy uses.
   - It audits `git.push`, actor `git:<author>`.
5. **AutoDeploy false:** records `latest` only. If no pin exists yet, it still pins once, so a new app builds.
6. **Errors:** recorded in `git-source.error` in plain words. A refused listing says the same `shpyrd git-auth` fix PR 1 uses. The reconciler requeues with backoff.
7. **Polling:**
   - Requeue after `SHPYRD_GIT_POLL_INTERVAL` (default 2m) when no webhook arrived in the last 24h, and after 1h otherwise.
   - Jitter it ±10% so polls spread out.
8. **Patches:** annotations go through a merge patch, never a full update, so the AppReconciler's writes are not clobbered.

## The AppReconciler builds the pin

- A git source on a branch with no valid pin is not built. The phase stays as it is, with the message "Reading the branch's latest commit." The reconciler requeues after a few seconds. The source reconciler pins quickly because it reconciles the same App event.
- With a valid pin:
  - the kpack Image's `source.git.revision` is the SHA;
  - the Dockerfile build key and `fetchScript` use the SHA. The fetch already handles a SHA.
- `status.source` is filled from `git-source` and the pin.
- **Upgrade cost, accepted:**
  - each existing buildpack app on a branch rebuilds once, when kpack's revision changes from the branch to the SHA, and redeploys the same code;
  - Dockerfile apps rebuild once too, because their build key changes.

  Note it in the PR.

## The API

- **Webhook:** `POST /api/hooks/git/:slug/:token` in the public `pub` group (the tenant comes from Host).
  - It is rate-limited by IP with its own limiter and message (`newRateLimiter` with a hook-specific message, or `allow()` directly).
  - It has a 1 MiB body cap.
- **Hook secret:** per project, a Secret `shpyrd-git-hook` in the project namespace. It holds `token` (32 random bytes, hex, in the URL) and `secret` (32 random bytes, hex, for HMAC and GitLab's token).
- **Verifying a delivery:** the URL token is compared in constant time. Then one of these must pass:
  - GitHub: `X-Hub-Signature-256: sha256=<hex HMAC-SHA256(body, secret)>`;
  - Gitea: `X-Gitea-Signature: <hex HMAC>`;
  - GitLab: `X-Gitlab-Token` equal to the secret.
- **Responses:**
  - 401 for a bad token or signature;
  - 202 for an event that is not a push (a GitHub `ping` gets 200 with "pong"), and for an unknown project;
  - 400 for an unreadable push.
- **Payload:** parsed into `{ref, after, head message, head author}`. Use the provider's push shape: GitHub/Gitea `head_commit`, GitLab `commits[last]` / `checkout_sha`.
- **Ignored pushes:** a deleted branch (`after` all zeros) is ignored.
- **Duplicates:** a repeated delivery id (`X-GitHub-Delivery`, `X-Gitea-Delivery`, `X-Gitlab-Event-UUID`) for the same App is ignored, by comparing it with the last one in `git-push` or `git-source`.
- **Applying the push:** writes `shpyrd.io/git-push` with a merge patch and returns 202.
- **Hook routes:**
  - `GET /api/projects/:slug/git-hooks` (ProjectConfig) returns `{url, secret}` and makes the Secret on first use.
  - `POST /api/projects/:slug/git-hooks/rotate` (ProjectConfig) makes a new token and secret.
  - The secret is shown only to people with ProjectConfig, so it is a value people paste into GitHub. Audit `git-hooks.rotate`.
- **The hook URL:** built from the request's own scheme and host, the workspace door: `https://<door>/api/hooks/git/<slug>/<token>`.
- **Deploy:** `DeployRequest.Git` gains `AutoDeploy *bool`. A git deploy removes the pin annotations, so the source reconciler resolves the branch again at once.
- **Updating a project:** `UpdateAppRequest` gains `AutoDeploy *bool`, applied to `spec.source.git.autoDeploy`. It is refused with 400 when the project has no git source.
- **Project view:** the response includes `spec.source.git.autoDeploy` and `status.source`. Check `toProject` in the dashboard types.

## CLI

- `shpyrd git-hooks show [--project]` prints:
  - the URL and the secret;
  - steps for GitHub (Settings, Webhooks, content type application/json, secret, "Just the push event"), GitLab (Settings, Webhooks, Secret token, Push events) and Gitea;
  - the auto-deploy state.
- `shpyrd git-hooks rotate [--project]`.
- `shpyrd deploy --git ... --no-auto-deploy`, and `--auto-deploy` to switch it back on.

## The dashboard Source card

The maintainer chose to have it built and reviewed in screenshots. It goes in `apps/workspace`, in the project overview's Summary card (`src/screens/project/overview.tsx`), using only `@shpyrd/ui` components and tokens. Rules: `apps/AGENTS.md` and `design/DESIGN_FOR_AGENT.md`.

- **Rows for a git source:**
  - Repository;
  - Branch;
  - Commit: short SHA, then message and author when known;
  - "Last checked" (relative time, and "via push" or "via poll");
  - an Auto-deploy switch.
- **The switch:** a `Switch` with `useMutation` calling `api.updateProject(slug, {autoDeploy})`. The pattern is in `screens/workspace/sign-in.tsx`. It is disabled without ProjectConfig. Find how permissions are checked in the screens.
- **When `status.source.skipped` is set:** a quiet line, "Skipped a1b2c3d: [skip deploy]".
- **When `status.source.error` is set:** an inline error with the message.
- **Webhook hint:** a link-style button "Deploy on push" that opens a dialog. The dialog shows the webhook URL and secret from `api.projectGitHooks(slug)`, with copy buttons and the GitHub/GitLab steps, if the library has a Dialog. Otherwise a short text pointing at `shpyrd git-hooks show`.
- **Wiring:**
  - add the types to `api/types.ts`;
  - add the calls to the Api interface, `backend.ts` and the mock;
  - add fixture data to the mock JSON so `npm run design` shows the card.
- **Tests:** Vitest tests next to the source, named as sentences, for the card's rendering of the states (pinned, skipped, error, no git source).
- **Checks:** the app's `lint`, `typecheck` and `test` scripts must pass.

## Docs and RFC

- `content/docs/deploying.mdx` gets a section "Deploying on push". It covers:
  - the webhook, with steps per provider;
  - polling and its interval;
  - `[skip deploy]`;
  - auto-deploy on and off;
  - the commit-pinning behaviour, and that the release note names the commit.
- `cli.mdx` gets `git-hooks` and the new deploy flags.
- RFC-0018 is `in progress` until the end-to-end run, then `implemented`, with a history line.

## The end-to-end run (after the final review)

- A kind cluster and an in-cluster Gitea, as in PR 1.
- **Pushes:** a push through a Gitea webhook deploys within seconds, and a `[skip deploy]` push does not.
- **Polling:** with no webhook, a push is picked up by polling. Use a short `SHPYRD_GIT_POLL_INTERVAL` for the test.
- **Auto-deploy off:** records the commit and does not deploy.
- **The webhook URL:** Gitea in the cluster must reach the server's webhook with the workspace Host. Point the Gitea webhook at the server Service, with a Host override if Gitea allows one, or use a CoreDNS rewrite. The run figures this out.
- **Screenshots:** the Source card (pinned, skipped, auto-deploy off), the hook dialog, `git-hooks show`, and a release named "Push … by …".
