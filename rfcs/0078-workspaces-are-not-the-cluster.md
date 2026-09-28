# RFC-0078 Workspaces are not the cluster

**Status:** implemented (v0.9.46)

**Owner:** Patrick Negri

**Depends on:** RFC-0033 (implemented: workspaces, addresses), RFC-0076 (implemented:
stable identifiers)

**Amends:** RFC-0033 (implicit workspace, console host), RFC-0035 (cloud profiles:
cluster init creates the first workspace)

**Creation date:** 2026-09-28

**Last update:** 2026-09-28 (implemented)

---

## Summary

The cluster and the default workspace are two different things. Today the open-source
platform conflates them: the implicit workspace has no row of its own until something
touches it, its slug is a constant (`default`), and the `shpyrd` console host answers for
both cluster administration and the workspace's projects. This RFC separates the two
concerns cleanly so that:

- **OSS**: `cluster init` always creates one explicit workspace (slug `default`, display
  name from `--name`), the platform domain is its base, nothing else changes for existing
  users.
- **Cloud**: the cluster owns the console; workspaces are created separately, are typed
  as operator-owned or customer-owned, and one is marked the default.

## Motivation

Three things break at scale:

1. **The implicit workspace has no owner.** On the first cloud the `demo` workspace is
   effectively an operator workspace — it holds the platform's own example apps, its costs
   are operator COGS, and it should never be invoiced. But the store has no way to say so;
   the economics card just shows it as another workspace.
2. **"Default" means too many things.** Default workspace, default project access, default
   plan — the word is overloaded and the concept is implicit. The default workspace should
   mean exactly one thing: *where a sign-in at the console host lands the CLI and the
   dashboard*.
3. **The console host and the workspace host are different hosts, but the code conflates
   them.** On the production cloud the console is at `operator.shpyrd.io` and workspaces
   are at `*.shpyrd.app`. A customer at `acme.shpyrd.app` must not reach cluster
   administration pages; the code currently routes by host but the *concept* is muddled.

## Design

### Workspace ownership

A workspace carries an `owner` field:

| Value | Meaning | Economics | Invoice |
| --- | --- | --- | --- |
| `customer` | a paying tenant | revenue | yes |
| `operator` | the platform operator's own use | COGS | never |

`owner` defaults to `customer` (the cloud layer sets it; the OSS server never creates
explicit workspaces). The `default` workspace created at `cluster init` is
`operator`-owned. `demo` on the first cloud becomes `operator`-owned retroactively.

### The default workspace

`default_workspace_id` is a cluster setting (stored in the install record, surfaced in
`/api/config`). It names the workspace where a sign-in at the **console host** lands the
CLI and the dashboard. It is:

- set to the workspace created at `cluster init` (OSS and cloud alike);
- changeable through the console; never changes silently;
- the only thing "default" means for workspaces; nothing else.

The `shpyrd` CLI `login` command uses the host to determine the workspace; `shpyrd use`
switches between workspaces for a person who belongs to more than one. No `--workspace`
flag anywhere; the host is the workspace.

### OSS: one workspace, no change to URLs

`cluster init --name "Acme Corp"` creates a workspace with slug `default`, display name
"Acme Corp", `owner: operator`, and marks it default. Its base domain is the platform
domain, so `crm.acme.co` stays `crm.acme.co` — no new DNS label, no second wildcard,
no cross-host sign-in. The console at `shpyrd.acme.co` and the workspace are on the same
host; routing is by path, not host. The "implicit workspace" code branch is deleted.

### Cloud: two zones, two hosts

The console is at `operator.shpyrd.io` (a delegated zone; `grafana.operator.shpyrd.io`
and any future platform host live under it). Workspaces live under `*.shpyrd.app`. They
are different registrable domains; sign-in at one does not carry over to the other —
which is the right boundary: a customer at `acme.shpyrd.app` has no path to cluster
administration.

`cluster init` on the cloud creates the first workspace operator-owned and default. The
cloud layer creates customer workspaces. The workspace slug and its base domain are
independent: the operator workspace `shpyrd` lives at `shpyrd.shpyrd.app` (its docs, its
status page); a CNAME from `docs.shpyrd.io` makes it reachable there too.

### `auth.shpyrd.io`

Dex (the OIDC issuer) lives at `auth.shpyrd.io`, a plain A record at the registrar
outside the delegated zone. It is customer-facing: workspace SSO connectors (Okta, Google)
use its callback URL; customers paste `auth.shpyrd.io` into their identity provider. It
is not at `auth.operator.shpyrd.io` for the same reason `auth.acme.com` would not read as
the vendor's infrastructure. The address is chosen once and never moves.

Password sign-in never shows this host (the server calls the Dex token endpoint server-
to-server, RFC-0012 password grant). SSO sign-in hops through it briefly.

### Console name

The console host is configurable: a variable `SHPYRD_CONSOLE_NAME` (default `shpyrd`)
prefixes the platform domain, so `shpyrd.<domain>` is the console address. Setting it
empty means the console answers at the platform domain's apex (`operator.shpyrd.io`
itself) — the production layout. The apex must appear in the wildcard certificate's
`dnsNames` (a wildcard does not cover its own apex).

Existing installs keep `shpyrd.<domain>` without any change.

### Deleting the implicit-workspace branch

`project.Namespace(slug)` returns `app-<slug>` for the implicit workspace today (no
workspace prefix). RFC-0076 (`p-<id>` namespaces) makes this distinction irrelevant for
new projects: every project has one namespace regardless of workspace. Legacy projects
keep their names. The implicit-workspace branch in `project.NamespaceIn` and the
`DefaultWorkspace` constant remain for backward compatibility with legacy namespaces but
are no longer the live code path for anything created since v0.9.43.

### No switcher (for now)

A workspace switcher in the UI/CLI is a follow-up. The host is the workspace; `shpyrd
use` covers the rare operator in two workspaces.

## API changes

- `/api/config` gains `defaultWorkspaceId` and `consoleHost`.
- `GET /api/workspaces` (console-only) gains `owner` on each row.
- `POST /api/workspaces` (cloud layer) accepts `owner`.
- `PATCH /api/cluster/settings` (console-only) sets `default_workspace_id`.

## Store changes

- `workspaces.owner TEXT NOT NULL DEFAULT 'customer'` (migration 000014).
- `settings.default_workspace_id` (a single row in a `settings` table, or a dedicated
  column on the install record — see open questions).
- `cluster init` (both OSS and cloud) writes the first workspace with `owner = operator`
  and records it as the default.

## Install / Terraform changes

- `cluster init` creates the first workspace explicitly and marks it default.
- OCI profile: `SHPYRD_CONSOLE_NAME` variable (empty = apex); the wildcard
  `Certificate`'s `dnsNames` gains the apex when empty.
- `auth.<domain>` becomes `SHPYRD_AUTH_URL` set explicitly; the variable is no longer
  derived from the domain (derivation remains as a fallback for OSS installs that do not
  separate auth from the platform domain).

## Migration on the first cloud

- `demo` (now displayed as "Acme") becomes `owner = operator`.
- `default_workspace_id` is set to the `default` workspace (the OSS workspace created at
  init, which is what the CLI has always pointed at).
- No namespace moves; no App changes; no ledger changes.

## Open questions

1. **`settings` table vs install record for `default_workspace_id`**: a dedicated
   `settings` table is the right home (other settings will follow: SMTP config, sign-in
   policy, plan defaults); the install record is infrastructure metadata. Default:
   **`settings` table** (migration 000014 adds both `workspaces.owner` and `settings`).
2. **OSS workspace slug**: `default` is a reserved word today (ValidateWorkspaceSlug
   rejects it). The workspace at `cluster init` gets slug `default` as a special case —
   the one workspace every install has. Explicit workspaces still cannot use it.
3. **Operator workspace on the cloud**: the first workspace's slug should be meaningful
   (`shpyrd`, `acme`, whatever the operator names it) rather than `default`. `cluster
   init --name <display>` derives the slug from the name; the `default` slug is only for
   OSS where there is one workspace and people never type its slug.

## Relationship to other RFCs

- **RFC-0033**: the address change mechanism and the workspace host are unchanged; this
  RFC only adds the ownership type and the default-workspace setting.
- **RFC-0076**: `p-<id>` namespaces remove the need for the implicit-workspace branch in
  new code; the branch stays for legacy project compatibility.
- **RFC-0035**: the cloud profile's `cluster init` gains the first-workspace creation
  step.
- **RFC-0014**: password reset and invite flows live at the console host
  (`/account/set-password`, `/account/reset`) and are workspace-scoped — they need the
  workspace-not-cluster separation to be clean before they are added.

## Implementation status

Not started.

## History

- 2026-09-28: written from discussions: the two-story OSS and cloud user stories, the
  `operator.shpyrd.io` + `*.shpyrd.app` production domain layout (operator zone
  delegated, auth.shpyrd.io a manual record), and the decision that the host is the
  workspace (no --workspace flag, no switcher yet).

## Implementation status

v0.9.46:

| Part | Status |
| --- | --- |
| `workspaces.owner` column (migration 000014, default 'customer'); `Workspace.Owner` field; `SetWorkspaceOwner`; `WorkspaceOwnerOperator`/`WorkspaceOwnerCustomer` constants | done |
| `settings` table (migration 000014); `Settings` interface (`GetSetting`/`SetSetting`); `SettingDefaultWorkspaceID`; Postgres + Memory implementations | done |
| `Migrate` seeds `owner=operator` for the implicit workspace and sets `default_workspace_id` on first run | done |
| `VarConsoleName` (`SHPYRD_CONSOLE_NAME`, default `shpyrd`; `apex` for the domain-apex production layout); `VarAuthHost` derived from `SHPYRD_AUTH_URL`; `derivedVars` uses consoleName for `SHPYRD_DASHBOARD_URL`; `SHPYRD_AUTH_URL` explicit when set, else derived | done |
| Dex ingress uses `${SHPYRD_AUTH_HOST}` instead of `auth.${SHPYRD_DOMAIN}` | done |
| `/api/config` gains `defaultWorkspaceId` and `consoleHost`; workspace views gain `owner`; `PATCH /api/cluster/settings` sets `default_workspace_id` | done |
| OCI profile: `SHPYRD_CONSOLE_NAME` and `SHPYRD_AUTH_URL` documented | done |
| Applied on OKE (v0.9.46): `demo` and `default` workspaces set to `owner=operator`; `default_workspace_id=default` set in settings | applied |

Known gaps:

- **The implicit-workspace branch** in `project.NamespaceIn` and `DefaultWorkspace` constant stay for legacy project compatibility; they are not the live code path for new projects.
- **`cluster init` does not yet create the first workspace explicitly** from the CLI flags — it relies on `Migrate`'s implicit insert. A dedicated `EnsureWorkspace(ctx, Workspace)` call from `openStore` with the display name and slug from `--name`/`--domain` is the follow-up, needed before production where the workspace should have a meaningful slug.
- **No UI** for `PATCH /api/cluster/settings` yet; the setting is currently changed via the API or directly in the database.
- **`shpyrd use` multi-workspace switching** deferred (RFC says so).

## History

- 2026-09-28: RFC written from the two-story OSS/cloud discussion and the production domain decisions.
- 2026-09-28 (v0.9.46): implemented.
