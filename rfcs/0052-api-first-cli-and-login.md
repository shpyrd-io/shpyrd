# RFC-0052 API-first CLI and `shpyrd login`

**Status:** implemented (complete; nothing of the text is still missing)

**Owner:** Patrick Negri

**Depends on:** RFC-0031, RFC-0026

**Creation date:** 2026-09-22

**Last update:** 2026-09-30

## Summary

The CLI talks to the shpyrd API for project work instead of the Kubernetes API: `shpyrd
login` (OIDC device flow or an API token) gives it a user identity, so developers without
cluster access use every project command, actions are attributed to them, and roles apply.
Cluster operations keep the kubeconfig.

## Motivation

Today the CLI needs a kubeconfig with broad rights; the audit trail records `user@host`
rather than the person; roles do not apply to CLI users at all.

### Goals

- `shpyrd login` then `shpyrd deploy`, `logs`, `shell`, `run`, `scale`, config vars,
  resources, all through the API, with 403s explained by role.
- `shpyrd --cluster` (or a kubeconfig present) keeps the current direct mode for operators.

### Non-Goals

- Removing the direct mode.

## Proposal

- `shpyrd login [--url https://shpyrd.example.com]`: device flow against the issuer (Dex
  supports it) or `--token shp_...`; stored in `~/.shpyrd/config` per cluster URL.
- Project commands get an API transport: uploads, deploy, logs (NDJSON stream), shell and
  run (WebSocket exec from RFC-0026), scale/resize, config vars, volumes, attach/detach,
  members. Missing endpoints (`run` attach, `pg psql` via exec) are added.
- Mode selection: API when logged in, direct when a kubeconfig context is given
  explicitly; the same command surface either way.
- Audit entries from the API carry the user; `via: cli` stays for direct mode.

## Implementation status

Implemented across v0.8.0 (`shpyrd login`, the `shpyrd-ctl` binary, the first commands
over the API) and v0.9.8 (every developer command). Signed in with `shpyrd login`, the CLI
needs no kubeconfig for: projects (create, list, info, rename, destroy), deploy (the
archive is uploaded to `POST /api/sources`, the deploy request carries what `shpyrd.yaml`
declares, the build output is streamed back), logs (streamed, `--build` too), shell
(through the web terminal's WebSocket bridge, RFC-0026), scale, resize, releases,
rollback, redeploy, open, secrets, access, allow, exposure, volumes, attach, detach,
drains, members, teams, tokens. The same transport serves operators through the
kubeconfig proxy, so both paths run the same code. Sessions keep a **current** workspace:
the one signed in to last, `shpyrd use` to list and switch, `SHPYRD_URL` to override,
`--context` to name a cluster; several sessions with none current is an error rather than
a fall-back to the kubeconfig. Commands that still need the cluster say so in one
sentence instead of failing on a nil pointer.

v0.9.61 closed what was left:

- **The browser sign-in.** `shpyrd login --url <workspace>` without `--token` runs a
  device flow (RFC 8628) against the workspace itself, not the issuer: the CLI asks
  `POST /api/cli/device` for a code pair, shows the person the short one and opens
  `/cli/activate`, where the dashboard session approves it (a server-rendered page, like
  the OAuth consent of RFC-0032); the CLI polls `POST /api/cli/device/token` until the
  approval has become a token. The token is a **session token**: a row of the tokens
  table with kind `session` (RFC-0031) that acts as the person with their roles as they
  are now, can mint tokens as they can, and is listed with their API tokens (`CLI on
  <host>`, 30 days, revocable); approving again from the same host replaces it. Two
  kinds, then: an API token carries roles of its own, for a machine that is not the
  person; a session token is the person on one machine. `--no-browser` prints the
  link for a shell over SSH. Codes live ten minutes in memory, like shell tickets.
- **`run` over the API.** `POST /api/projects/:slug/run` creates the one-off instance
  (the same pod the CLI built with a kubeconfig, now `controller.OneOffPod`) and mints a
  ticket bound to it; the web terminal's socket (RFC-0026) waits for the instance to
  start, attaches to its own process, returns the exit code and removes the instance
  when the session ends. The server's ClusterRole gained `create` on pods and
  `pods/attach` for it (deploy/components/shpyrd/base/rbac.yaml); an existing cluster
  receives it with the upgrade, since `shpyrd-ctl cluster init` applies every component
  again (server-side apply), and the role takes effect without a restart. A detached run is created and left. Piped input reaches the
  command, and an `eof` control frame ends its stdin when the pipe does.
- **`pg psql` and `redis cli` over the API.** An extension that takes a shell into its
  resources implements `ext.Shellable`: `POST /api/projects/:slug/resources/:kind/:name/
  shell/ticket` asks it for the pod, container and command, and the socket runs them
  without the launcher. A ticket that names a pod holds a slot of its own, so a database
  shell and a project shell run side by side.
- **`globals`** (RFC-0016) reads and writes `/api/workspace/globals` when signed in.
- `domains` and `shell -- <command>` had gone over the API in v0.9.9.

The developer-facing extension commands (`pg`, `redis`) ship in `shpyrd`, the operator's
(`users`, `auth`, `object-storage`) in `shpyrd-ctl`; an extension declares the audience
of each command (`ext.ForOperator`). What still needs a kubeconfig is the operator's
(`shpyrd-ctl`), by design.

## Implementation History

- 2026-09-30: implemented in full (v0.9.61): the browser sign-in, `run`, `pg psql`,
  `redis cli` and `globals` over the API; exercised end to end on a kind cluster. The RFC
  is done.

- 2026-09-27: implemented (v0.9.8): every developer command over the API, current
  workspace, extension commands split by audience.

- 2026-09-22: RFC written.
