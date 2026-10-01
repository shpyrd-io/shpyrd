# RFC-0052 API-first CLI and `shpyrd login`

**Status:** implemented (v0.8.0 login and shpyrd-ctl; v0.9.8 every developer command over the API), gaps — see Implementation status below

**Owner:** Patrick Negri

**Depends on:** RFC-0031, RFC-0026

**Creation date:** 2026-09-22

**Last update:** 2026-09-27

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

Known gaps:

- `run` (one-off commands), `pg`, `redis` and `domains` still talk to the cluster; the
  shell bridge cannot run a given command (it opens the image's shell).
- No browser device flow: `shpyrd login` takes a token (a personal token from RFC-0031
  or the admin token). People without cluster access create tokens in the dashboard.
- The developer-facing extension commands (`pg`, `redis`) ship in `shpyrd`, the
  operator's (`users`, `auth`, `object-storage`) in `shpyrd-ctl`; an extension declares
  the audience of each command (`ext.ForOperator`).

## Implementation History

- 2026-09-27: implemented (v0.9.8): every developer command over the API, current
  workspace, extension commands split by audience.

- 2026-09-22: RFC written.
