# RFC-0080 Two doors: the console and workspaces are separate applications

**Status:** implemented (v0.9.52); gaps

**Owner:** Patrick Negri

**Depends on:** RFC-0033 (workspaces, addresses, per-workspace sign-in methods), RFC-0058
(external identity providers), RFC-0076 (stable identifiers), RFC-0078 (workspace
ownership, the default workspace setting)

**Amends:** RFC-0033 (the implicit workspace is removed; realms are hosts, not slugs),
RFC-0078 (the default workspace is explicit, with an address; the console is not a
workspace), RFC-0058 (three connector scopes: console, platform defaults, workspace),
RFC-0012 (two sign-in pages, one per door), RFC-0007 (per-host relying party; no
console handoff), RFC-0035/0036 (domains and front doors on the cloud), RFC-0065 (the
UI is two applications served by host)

**Creation date:** 2026-09-29

**Last update:** 2026-09-29 (implemented)

---


## Amendment (2026-10-04)

- **Console users.** The console has its own people: a list of emails
  (`console_users`), each an admin of the console, managed on its Console users page or
  with `shpyrd-ctl console-users`. They sign in with email and password or through a
  method of the console's realm; a provider only proves who someone is, and the email
  must be on the list. The admin token always opens it. While the list is empty and the
  workspace's roles are not enforced, the first admin is let in, as at install. Migration
  `000021_console_users` filled the list with the owners and admins of the default
  workspace. The console's Accounts page still lists every local account.
- **The console's Workspaces page** left the core: the open-source console keeps a
  Workspace link to its one workspace. A binary built on the core may ship applications of
  its own beside the two (a folder each in the UI directory), which the console serves
  under `/apps/<folder>/`, and link them from the console's sidebar with an `ext.Link` of
  area `console`.

## Summary

The platform has two kinds of users and today one application for both. The **operator**
administers the cluster: nodes, pools, workspaces, plans, economics, who may sign in to
administer. **People in workspaces** build and run projects. RFC-0078 separated the data
(a workspace has an owner; one is the default) but left the doors joined: the console host
*is* the default workspace, the implicit one; its login page "offers every method" and
cannot be restricted; every sign-in on the platform, customers included, returns through
the console's OIDC callback; the same SPA renders on every host and decides what to show
from a boolean.

This RFC separates the two completely:

- **Two doors.** The console answers at the console host only (`operator.shpyrd.io` on
  the cloud, `shpyrd.<domain>` in OSS) and is **never a workspace**. Every workspace,
  the default one included, is explicit and has an address. In OSS the default
  workspace's address is the platform domain itself, so project URLs do not change;
  its dashboard moves to the apex. On the cloud the operator's own workspace lives under
  the workspaces domain like everyone else's (`platform.shpyrd.app`).
- **Two realms, one identity provider.** One Dex stays. Each door has its own sign-in
  methods: the console's (which the operator can reduce to a single Google Workspace
  domain), the platform's defaults offered to workspaces, and each workspace's own. A
  session records the realm and the connector it was minted through; a realm accepts only
  its own. Sign-in returns to the host it started from — per-host OIDC callbacks, no
  handoff through the console.
- **One identity.** A person is one account keyed by verified email across both doors;
  roles are per realm. `platform-admin` exists in the console realm alone and is
  bootstrapped by the console's first sign-in. Platform admins are owners of
  operator-owned workspaces without memberships. An operator-owned workspace shows a
  platform admin a link to the console; nothing else is shared.
- **Two applications.** The UI becomes `ui/apps/console` and `ui/apps/workspace` on a
  shared `ui/src` (components, tokens, client), one Vite build with two entries; the
  server serves the console bundle at the console host and the workspace bundle
  everywhere else.

Decided with the owner on 2026-09-29: separate doors also in OSS (no combined mode);
one Dex; one identity per verified email with per-realm sessions and roles; no migration
of existing installs' hosts (development and production are rebuilt).

## Motivation

1. **Security boundary.** The operator wants the cluster behind a single Google Workspace
   domain while customers sign in however they like. Today that is impossible: the
   console's methods are the platform's methods, shown to every workspace by default, and
   the console cannot hide any (`workspace.go:196`, "the platform's own workspace offers
   every method").
2. **Blast radius of the callback.** A customer signing in at `console.acme.com` is
   redirected through `operator.shpyrd.io/api/auth/callback` (`auth.go:604-611`,
   `realms.go:189-227`). The locked door is the doormat for every other door.
3. **Product clarity.** `atConsole()` means "the request resolved to the slug `default`"
   (`realms.go:162-165`), not "the request arrived at the console". The SPA decides what
   it is from `workspace.implicit` (`layout.tsx:44`). Nobody can point at the console and
   at a workspace and say what each one is.
4. **A dead end already declared.** RFC-0078 wrote "the implicit-workspace code branch is
   deleted" and listed 40 places where it is not (see the audit in the Design section).

## Design

### Realms

A request belongs to exactly one realm, decided from its host by the tenancy resolver:

| Host | Realm | Tenant |
| --- | --- | --- |
| the console host (`SHPYRD_CONSOLE_HOST`) | `console` | none |
| a workspace address, one label under it, a verified custom host, a moved host | `workspace` | that workspace |
| an internal host (`localhost`, an IP, `*.svc`: the kubeconfig proxy) | `console` | the default workspace for project APIs |
| anything else | — | 404 unknown host |

The internal row keeps `shpyrd` and `shpyrd-ctl` over a kubeconfig working unchanged:
kubeconfig and admin-token identities are the operator's, so console routes are allowed,
and project APIs act on the default workspace as they do today.

`atConsole(c)` becomes "the request's realm is console". `requireConsole` keeps its
name. `Realms.Methods` receives the realm and the workspace (nil at the console).

### Sign-in methods: three scopes

Connectors (Dex `Connector` CRs, RFC-0058) carry a scope:

| Scope | Dex connector id | Label | Shown on |
| --- | --- | --- | --- |
| `console` | `console-<id>` | `shpyrd.io/realm=console` | the console's login page only |
| `platform` | `<id>` | `shpyrd.io/realm=platform` | every workspace's login page unless it sets `ownMethodsOnly` |
| `workspace` | `ws-<workspace id>-<id>` | `shpyrd.io/realm=workspace`, `shpyrd.io/workspace-id=<id>` | that workspace's login page |

The workspace scope moves from slug to the base36 workspace id (RFC-0076 gap: connector
ids keyed by slug). Existing `ws-<slug>-…` connectors are re-created under the id form
by the server at start, once.

The password method (`auth-local`) is offered on the console when the cluster setting
`console.password_signin` is true (default true, so a fresh install is usable before
any IdP exists) and on workspace login pages as a platform default unless
`ownMethodsOnly`. The operator turns it off at the console after adding Google
(`shpyrd-ctl auth console --password=false`); the console then has exactly the methods
in scope `console`. Google's `hostedDomains` restriction is already supported
(`connectors.go:127-132`).

The admin token (`shpyrd-ctl cluster token`) is accepted at the console realm and at
internal hosts only; it is never offered on a login page, as today.

### Sessions

A session records `Realm` (`console` | `workspace`), `WorkspaceID` (empty for the
console) and the connector it came from (`Identity.Provider`, already there). Validation
(`sessionAuth` → `getIn`) requires the realm to match the request's and, for
workspaces, the workspace to match — a console cookie presented at a workspace host, or
a workspace cookie at the console, is not a session. Cookies stay host-only (no `Domain`
attribute), so browsers already keep them apart; the realm check makes the rule
explicit. The `secure` flag follows the request's scheme, not the console URL.

`platform-admin` bootstrap ("everyone is admin until the first role exists",
`authz.go:173-183`) applies to the console realm only. Workspaces, the default one
included, are enforced from their first day: their owners are their memberships — and,
for operator-owned workspaces, every platform admin (`authz.RolesIn`: `Owner == operator`
and the account holds `platform-admin` → workspace owner). No membership rows for the
operator in his own workspaces; deleting a platform admin removes his access everywhere
at once.

### Per-host callbacks

The relying party builds the redirect URI from the request's host:
`https://<host>/api/auth/callback`. Dex must know every such URI. The static client in
`dex/base/config.yaml` goes; the server owns an `OAuth2Client` resource (Dex's
`oauth2clients.dex.coreos.com`, read live from its Kubernetes storage) named `shpyrd`,
with the client secret from Secret `shpyrd-oidc-client` and `redirectURIs` = the console
callback + one per workspace address + one per verified custom host. The server
reconciles it at start and whenever workspaces or hosts change (the same
`workspacesChanged` signal the front doors use). Until the resource exists after a cold
start, sign-in is refused by Dex for a few seconds; nothing else.

`GET /api/auth/login` starts the flow at the host it was called on, for the methods that
host's realm offers; `GET /api/auth/callback` completes it at the same host and mints
that realm's session. `/.shpyrd/session` (the handoff redeem), `consoleLoginURL`,
`handoffTarget`, `handoff` and `sessionHandoff` are removed; the `edge` one-time codes
stay for the edge's own use.

### The default workspace

`store.Migrate` creates the default workspace as today (slug from
`SHPYRD_DEFAULT_WORKSPACE`, default `default`; `owner=operator`; recorded in
`settings.default_workspace_id`) and now gives it an **address**:
`SHPYRD_DEFAULT_WORKSPACE_ADDRESS`, derived by the installer as the platform domain when
the console is `shpyrd.<domain>` (OSS layout: apps stay at `<project>.<domain>`), and as
`<slug>.<workspaces domain>` when the console is at the apex (cloud layout:
`platform.shpyrd.app`). An existing install whose default workspace has no address
receives one on upgrade. `Workspace.Implicit()` is deleted; `store.DefaultWorkspace`
survives only in `pkg/project` for the legacy `app-<slug>` namespace form.

Tenancy resolves the default workspace from `settings.default_workspace_id`, not from the
constant. `PATCH /api/cluster/settings` can move the default to another operator-owned
workspace.

### What "owned" shows

`GET /api/workspace` gains `ownedByOperator` (from `Owner`). `GET /api/config` gains
`door` (`console` | `workspace`) and `consoleUrl`. The workspace application shows a
"Console" link when `ownedByOperator` and the signed-in person's account holds
`platform-admin` — the one place where the doors acknowledge each other.

### Two applications

```
ui/
  apps/console/    index.html, main.tsx, App.tsx   routes: /, /workspaces, /accounts, /signin, /settings
  apps/workspace/  index.html, main.tsx, App.tsx   routes: /, /projects, /projects/:slug, /workspace/:tab
  src/             shared: components/ui, layout shell (nav as props), lib (api, auth, me, theme, branding), pages used by both (login, invite), page modules
```

One Vite build with two entries (`rollupOptions.input`), one `dist/` with
`console/index.html` and `workspace/index.html` and shared hashed chunks under
`assets/`. `serveUI` picks the entry by realm. `ui/src` is the shared package in fact;
splitting it into `packages/ui` and `packages/client` as npm workspaces is a follow-up
once two consumers exist (the design-system site would be the second).

*As built later (2026-09-30):* the two applications are `apps/console` and
`apps/workspace`, Next applications compiled to static files, on the library
`design/ui` (`@shpyrd/ui`, an npm workspace at the root) and `apps/shared`. Each
build is a folder of its own in the embed, `pkg/ui/dist/console` and
`pkg/ui/dist/workspace`, with no shared chunks; the server picks the folder by
host and serves it with no route declared (the plan of 2026-09-29,
`docs/superpowers/plans/2026-09-29-design-content-apps.md`).

The console application gets what the API had and the UI never showed: the workspaces
list (with owner and address; creation on the cloud), the default workspace setting,
accounts, and the two sign-in cards ("this console" and "defaults for workspaces"). The
workspace application loses the Cluster page and the Accounts tab.

### Audit: every implicit-workspace branch and what replaces it

| Where | Today | After |
| --- | --- | --- |
| `realms.go:158-177` `consoleWorkspace`, `atConsole`, `requireConsole` | console = slug `default` | console = realm from host |
| `realms.go:37-54` `DefaultRealms.Methods` | implicit → all methods | console → console scope (+ password by setting); workspace → own + platform unless `ownMethodsOnly` |
| `realms.go:69-71` token hidden for explicit | | token at console realm only |
| `auth.go:604-611, 650-659`, `realms.go:181-249` | bounce to console, handoff | per-host login and callback; handoff removed |
| `auth.go:128` `redirectURI()` | console URL | `https://<host>/api/auth/callback` |
| `auth.go:711-713` secure cookies | from console URL | from request scheme |
| `sessions.go:40-53, 129-169` | bound by workspace | + realm |
| `authz.go:173-183, 335, 380` | bootstrap when slug is `default` | bootstrap at console realm; platform admins own operator workspaces |
| `workspace.go:196` `ownMethodsOnly` refused for implicit | | refused for none; the console has its own setting |
| `workspace.go:487` `admitSignIn` `""`→default | | realm-aware: console admission checks the console's methods |
| `connectors_api.go:37` scope | default → platform | console host → `console` (and `?scope=platform`); workspace host → `workspace` by id |
| `connectors.go:59-64` ids by slug | `ws-acme-google` | `ws-<id>-google`, rekeyed once |
| `edge.go:64-111` dashboard/apps hosts | implicit → console URL/platform domain | every workspace has an address; console URL is the console's |
| `edge.go:149, 803` `appByHost`, `workspaceOf` | default fallback | default workspace by setting |
| `hosts.go:48-127, 178, 301` | implicit guards | removed: every workspace has an address |
| `domains.go:152` CNAME target | | `appsDomainOf(ws)` for all |
| `tenancy.go:64-89` `Single` | every host → default | console host → console realm; others → default workspace |
| `tenancy.go:105-208` `ByAddress` | platform names → default | console host → console; reserved platform hosts never a workspace; then addresses/hosts |
| `tenancy.go:252-446` `Addresses` | `""`/default → no address | default has an address like any |
| `project.go:105-142` | `default` reserved, legacy namespace | unchanged (legacy naming only) |
| `workspace_controller.go:89` skip implicit | | front door for every workspace (OSS default: apex + `*.<domain>` on the platform wildcard) |
| `desired.go:295, 328-349, 377-393, 838` | default special cases | labels for all (RFC-0076 ids), apps domain and issuer from the address |
| `domains.go:73`, `workspace_tls.go:35,62`, `globals.go:134` | default special cases | by owner (`operator`) where the intent was "the platform's own apps", otherwise uniform |
| `backup.go:183,200` | default → `store.json` | keyed by the default's slug (compat) |
| `cloud/extension.go:74-90, 237-284` | implicit guards, no owner | `Owner` set (`customer` default, `operator` with `--operator`), url from address |
| `internal/cli/workspaces.go:142` | "(platform)" | the address |
| UI: `layout.tsx:44`, `workspace.tsx:50,202`, `login.tsx:76`, `launcher.tsx:62`, `workspace-names.tsx:160` | `implicit` boolean | two applications; `door` from config |

### Install and Terraform

New variables: `SHPYRD_DEFAULT_WORKSPACE` (slug, default `default`),
`SHPYRD_DEFAULT_WORKSPACE_ADDRESS` (derived as above; settable), `SHPYRD_CONSOLE_PASSWORD_SIGNIN`
(default `true`). The Dex component drops `staticClients`; the server's RBAC gains
`oauth2clients` on `dex.coreos.com`. Production sets `SHPYRD_DEFAULT_WORKSPACE=platform`
in `extra_vars`.

## Open questions

1. **Identity linking across connectors.** Same verified email through Google at the
   console and through Okta at a workspace: one person? Default: **yes when the IdP
   asserts `email_verified`**; password accounts link only after RFC-0014's email
   verification.
2. **Password at the console on the cloud.** Default: **on until the operator turns it
   off** (`console.password_signin`), so a cold install is usable; the runbook says to
   add Google and turn it off as the first act.
3. **Internal hosts.** The kubeconfig proxy resolves to the console realm with the default
   workspace as tenant for project APIs. Default: **keep**; `shpyrd` over kubeconfig
   for another workspace's projects already names the workspace in the App lookup.
4. **Console at the apex in OSS.** `SHPYRD_CONSOLE_NAME=apex` in OSS puts the console at
   `<domain>`; the default workspace's address then cannot be the platform domain.
   Default: **derive `<slug>.<domain>`** and warn that project URLs move one label down.
5. **`packages/ui` as an npm workspace.** Default: **not yet**; `ui/src` is the shared
   package until a second consumer exists. *Done on 2026-09-30, as `design/ui`
   (`@shpyrd/ui`): the second consumer was the site and the state pages the server
   serves by itself; `ui/` was removed.*

## Implementation status

Implemented in v0.9.52 (2026-09-29):

- Tenancy resolves a door: the console host (no workspace), workspace hosts, internal
  hosts as the operator's door with the default workspace as tenant; workspace routes
  answer 404 at the console (`pkg/tenancy`, `pkg/api/members.go`).
- Every workspace has an address; `Migrate` gives the default one the platform domain or
  `<slug>.<workspaces domain>` (`SHPYRD_DEFAULT_WORKSPACE`,
  `SHPYRD_DEFAULT_WORKSPACE_ADDRESS`, derived by the installer). `Workspace.Implicit()`
  is gone; `settings.default_workspace_id` is read everywhere the constant was.
- Three connector scopes with realm labels, workspace connectors keyed by short id and
  rekeyed once at start; the console's password form behind
  `console.password_signin` (`PATCH /api/cluster/settings`, refused without another
  console method); `shpyrd auth connector add/remove --realm console|platform`.
- Sessions carry their realm; a realm accepts only its own; per-host callbacks with the
  server-managed Dex `OAuth2Client`; the console handoff removed; cookies Secure by the
  request's scheme.
- Bootstrap at the console realm only; workspaces enforced from birth; platform admins
  own operator workspaces (`authz.ConsoleAdmin`); `/api/me` `console`; `/api/config`
  `door`, `consoleUrl`, `workspace.ownedByOperator`.
- Two applications (`ui/apps/console`, `ui/apps/workspace`) on the shared `ui/src`,
  served by door; the console's Workspaces, Accounts, Sign-in (three scopes) and Settings
  pages; the workspace application's link to the console; the open-source server's
  `GET /api/workspaces`.
- `shpyrd-ctl workspaces create --operator`; the cloud layer sets the owner and no longer
  knows an implicit workspace.
- `shpyrd cluster destroy --keep-cluster` (not this RFC's, but what the rebuild of
  production for it required).

Known gaps (each stays here until done or dropped):

| Gap | Where |
| --- | --- |
| Identity linking across connectors: a person is their email as each provider asserts it; `email_verified` is not checked and password accounts are not linked to provider accounts | open question 1 |
| The CI end-to-end run (kind) exercises the one-door fallback, not two hosts | test matrix |
| Google `hostedDomains` is set from one `--hosted-domain`; Dex's `groups` for Google are not mirrored | RFC-0058 |
| The docs site does not describe the two doors yet | docs |

## History

- 2026-09-29: created from the owner's decision to separate console and workspaces
  completely (separate also in OSS; one Dex; one identity; rebuild dev and prod rather
  than migrate).
- 2026-09-29: implemented in v0.9.52; the console realm's roles are those of the
  operator's default workspace (its owners and admins are the platform admins), which is
  RFC-0078's operator workspace read from the console's door.
- 2026-09-30: the library split that open question 5 had put off is done: `design/ui`
  is the npm workspace `@shpyrd/ui`, the two applications are `apps/console` and
  `apps/workspace` on Next, each built to a folder of its own in `pkg/ui/dist`,
  served by host with the hashes of the scripts Next writes in the policy; `ui/`
  (Vite, one build with two entries) was removed.
