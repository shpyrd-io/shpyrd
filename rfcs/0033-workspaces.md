# RFC-0033 Workspaces

**Status:** in progress (definition being finalised; first pieces shipped)

**Owner:** unassigned

**Depends on:** RFC-0008 (implemented), RFC-0016

**Creation date:** 2026-09-22

**Last update:** 2026-09-27

## Summary

A grouping level above projects with inherited membership, and possibly shared settings
(domains, globals, quotas). The definition is not settled; this RFC records the options and
must be decided before implementation.

## Motivation

Companies group projects by product or team; environments (staging, production) of one
application belong together.

## Proposal

Options:

- **(a) Organisational workspace**: a set of projects owned by a team; roles granted on the
  workspace apply to all its projects; workspace-level globals (RFC-0016), quotas (RFC-0042)
  and domains (RFC-0034); a Workspaces page. Projects can move between workspaces.
- **(b) Environments** grouping was considered and set aside: environments follow Git
  branches deploying to their own projects (RFC-0055, deferred).

Default if nothing else is decided: (a).

## Design Details (option a)

- `Workspace` CRD in `shpyrd-system` (owner team, description); label `shpyrd.io/workspace`
  on project namespaces; `ProjectMember` gains `spec.workspace` as an alternative to
  `spec.project`; authz resolves workspace roles into project roles.

## Open questions

1. Is the organisational grouping (a) wanted at all in the near term? Default: (a), low
   priority.
2. Does a workspace need its own domains, quotas or globals in the first version? Default:
   membership inheritance only; the rest follows in the RFCs that introduce them.

## Implementation status

The definition moved on from "a grouping of projects": the workspace is the tenant every
project, team and person belongs to; the open-source platform has exactly one, implicit,
and behaves as before. The first pieces shipped in v0.4.0:

- A control-plane database (component `control-plane-db`, PostgreSQL in the cluster, or a
  managed one through `SHPYRD_DATABASE_URL`) holding the workspace, the people who signed
  in, teams and project grants — moved out of the `Team`/`ProjectMember` objects, which are
  imported once and marked migrated.
- `GET/PATCH /api/workspace`, `/api/workspace/people`, `/api/workspace/grants`,
  `/api/workspace/export|import` (the platform backup carries the database, RFC-0037).
- A Workspace page in the dashboard (name, people, teams, accounts); the labels
  `shpyrd.io/workspace` on new project namespaces.

Shipped in v0.5.0:

- The `user` role (opens the app; nothing in the builder dashboard) and `spec.access`
  (`public`, `authenticated` — the default for new projects —, `identified`).
- The edge: ingress-nginx `auth_request` to the server for non-public apps; a cookie per
  app host (never the dashboard's session cookie), obtained through a one-time code from
  the dashboard host; `X-Shpyrd-*` headers and an EdDSA JWT (`/.well-known/jwks.json`);
  the "available to team X" page; `Open as` previews; the launcher for `user`-only people;
  `shpyrd access`, `projects create --public`; the admin token as a browser session.

Shipped in v0.6.0:

- Sessions and the edge's one-time codes live in the control-plane database (restarts keep
  people signed in; replicas agree on sign-outs).
- Login methods managed on the Workspace page: Google, Microsoft, GitHub and any OpenID
  Connect provider through the bundled issuer; the join policy (anyone who can sign in /
  only accounts of a claimed domain / only people already in a team); company domain claims
  verified by DNS TXT, routing the domain's accounts to one method.
- The built-in `everyone` team; suspending a person.

Shipped in v0.7.0 and v0.8.0:

- Network allow lists (`allow:` on the project; the Connections card), default closed
  between projects.
- `shpyrd login`, API transport for project commands, the `shpyrd-ctl` operator binary.

Shipped in v0.9.1 — the platform resolves the workspace from the request host:

- `pkg/tenancy`: the `Resolver` interface (host → workspace) with the open-source
  implementation (every host is the one implicit workspace) and a host-based one for
  installs that carry several workspaces, each at an address of its own (`<address>` for
  its dashboard, `<app>.<address>` for its apps). Hosts nobody claims answer "nothing
  here"; suspended workspaces answer so.
- Everything the server does is scoped to the request's workspace: projects (namespace
  `app-<workspace>-<project>` for explicit workspaces, `app-<project>` unchanged for the
  implicit one), teams and grants, sessions (a cookie replayed at another workspace's host
  is anonymous), the edge's JWT (`iss` is the workspace's dashboard URL, `ws` its slug),
  personal tokens, audit, log drains (selected by namespace).
- Reserved names (`www`, `api`, `auth`, `login`, `console`, `shpyrd`, `grafana`, …) are
  refused for new projects; the `capabilities` list in `GET /api/config` tells the
  dashboard and CLI what a server offers beyond the core (empty here).
- Self-hosted installs see no change: one workspace, the same names, the same objects.
  Creating further workspaces is not part of the open-source platform.

Shipped in v0.9.2 — sign-in at a workspace's own host:

- The platform's dashboard is the bundled issuer's one relying party; a workspace at its
  own address sends the browser there to sign in and receives a one-time code back, which
  becomes the workspace's own session (the platform's dashboard keeps none). The
  workspace's join policy and domain claims decide who may enter.
- A reconciler publishes each explicit workspace at its address (Ingress and certificate);
  cluster-level routes and pages (cluster, extensions, global config vars, accounts) belong
  to the operator and do not answer at a workspace's host.
- Explicit workspaces are enforced from birth: no bootstrap mode for them.

Shipped in v0.9.3 — plan limits:

- A workspace may carry ceilings (projects, instances, CPU, memory, storage). The API
  refuses what would exceed them with the number ("this would run 4 instances; the plan
  allows 3"), and the controller backs the check with a `ResourceQuota` per project
  namespace. The Workspace page shows the plan and the usage. Self-hosted installs have no
  ceilings unless one is set.

Shipped in v0.9.4:

- The front doors blank any `X-Shpyrd-*` header a client sends, on every request to every
  app, public ones included; the edge sets the real values where sign-in is on. Every
  process receives `SHPYRD_ISSUER`, `SHPYRD_PROJECT` and `SHPYRD_WORKSPACE` so it can verify
  the JWT against `<iss>/.well-known/jwks.json`; `examples/hello` shows how.

Shipped in v0.9.5 — the seams for a platform hosting many workspaces:

- `pkg/server` wires the server for any binary built on the core; `shpyrd-ctl workspaces
  create|list|plan|suspend|resume` speaks the console routes of a server that reports the
  `workspaces` capability (the open-source platform does not, and says so).

Shipped in v0.9.7 and v0.9.8:

- Apps of an explicit workspace are served with the workspace's own wildcard certificate,
  copied into their namespaces by the controller; the workspace's front door answers "No
  app here" for unknown hosts under its address.
- After `shpyrd login`, every developer command speaks the workspace API: deploy (with the
  build output streamed back), logs, shell (through the web terminal's bridge), scale,
  resize, releases, rollback, redeploy, open, projects, volumes, attach, detach, drains,
  members. The CLI keeps a current workspace (`shpyrd use`). `run`, `pg`, `redis` and
  `domains` still need a kubeconfig. Extension commands split by audience: `pg`/`redis` in
  `shpyrd`, `users`/`auth`/`object-storage` in `shpyrd-ctl`.
- The platform's exposure survives `cluster init` re-runs; the admin token is not offered
  at a workspace's login page.

Shipped in v0.9.9 and v0.9.10 (after an audit of the model against the code):

- `pg`, `redis` and `domains` speak the API; `secrets set` too.
- Allow lists work end to end: an entry opened the callee's ingress but the caller's own
  egress refused the packets on every profile with a pod CIDR; projects may now send to
  the pods of their own workspace's projects and the callee's ingress decides. The peer
  names project and workspace (two workspaces may both have `shop`), and the API refuses
  an entry naming a project outside the caller's workspace.
- A suspended workspace's apps are not served (their Ingresses go, the front door says
  why); before, public apps kept serving.
- The platform backup carries every explicit workspace's people, teams, grants and domain
  claims, and `cluster restore` puts them back, recreating a workspace that is gone.
- A project's custom domain resolves to the app's workspace under host-based tenancy
  (sign-in there was broken).
- The operator's global config vars no longer leave a Secret in tenant namespaces.

Shipped in v0.9.11 — the edge keeps the model's promises:

- Personal API tokens (`shp_…`) open apps at the edge as their owner, within the token's
  roles: scripts, CI and agents can call a closed app. A bearer that is not the platform's
  is anonymous, and an `identified` app receives it untouched for its own API clients.
- Signing out of an app (`/.shpyrd/logout`) ends the whole session.
- The signing key rotates every 30 days; retired keys verify for a week and stay in the
  JWKS. The JWT's `sub` is the person's id in the workspace.
- The "available to the … team" page names only the teams that may open the app; API
  clients get a JSON 401 instead of a sign-in redirect.
- The server has a NetworkPolicy: the API and the edge are for the front doors; build pods
  reach only a sources port; per-IP throttles see the real client.
- Denials are counted per project (`shpyrd_edge_denials_total`); audit entries carry the
  actor's realm.

Shipped in v0.9.11/v0.9.12 as well: the control-plane database migrates with golang-migrate
(versioned up/down files; the previous runner's record is bridged once); every identifier
is a native `uuid`; image repositories are `apps/<workspace id>/<slug>` for every workspace,
the id rendered in base36, so two workspaces with a project of the same name never share a
repository (the move rebuilds each buildpack app once; the previous release keeps serving).

Shipped in v0.9.13 — workspace roles and invitations:

- A person's role in the workspace is a membership by email: `owner`, `admin` or `member`.
  Owners and admins administer the workspace and every project (they are the workspace's
  platform admins); only owners name or demote owners, and the last owner stays. Members
  may create projects and get the admin role on what they create; teams and project grants
  give everything else. A membership decides over a team's `platformRole`; without one the
  team's role still counts (the older way, kept for existing installs).
- Any membership ends bootstrap mode, like the first team did; the first person to define
  who is who (a role, an invitation, a team, a grant) becomes the owner instead of losing
  their own access. The operator's admin token holds the owner's actions.
- Invitations (`shpyrd invite`, the People tab): a link shown once, emailed when the
  platform sends mail (RFC-0013), valid for seven days. Signing in with the invited address
  accepts it, link or no link, and an invited address passes every join policy; inviting
  someone who has signed in before applies the role at once. `/invite/<token>` shows the
  holder what they were invited to.
- The cloud names a new workspace's owner with the role; `GET /api/workspace` lists the
  owners; the People list includes people with a role who have not signed in yet; the
  platform backup carries memberships (dump version 2).

Shipped in v0.9.15 — a workspace's own sign-in, and readers:

- **Per-workspace SSO.** Owners and admins add their company's Google, Microsoft, GitHub or
  OpenID Connect provider on the workspace's Sign-in tab (or `shpyrd sso add`); it appears
  on that workspace's login page only. The platform's methods stay offered until the
  workspace switches them off, which it may do once it has a method of its own. A claimed
  email domain with a method skips the chooser: the login page asks for the work email and
  sends the person straight to their company's sign-in.
- **The `reader` role** opens an app read-only: the edge refuses requests that change
  things and tells the app `reader`, so apps have viewers without permission code.

Shipped in v0.9.16 — the workspace's names:

- **Address change.** Owners move the workspace to another label under the same parent
  (`demo.shpyrd.app` → `acme.shpyrd.app`; Overview page or `shpyrd workspace address`). The
  label may not be another workspace's name, address or domain, or a reserved word. Apps
  move with it; the old address — dashboard and app hosts — redirects permanently for
  thirty days and cannot be taken meanwhile. (The internal identifier, the namespace's
  name, does not change: Kubernetes namespaces cannot be renamed.)
- **Custom domains** in CNAME mode: `intranet.acme.com` and `*.intranet.acme.com` point at
  the address, a TXT record (or the CNAME itself) proves the domain, the platform issues a
  certificate for the domain and one per app host, and the dashboard and every app answer
  there. A verified domain may be made **primary**: the dashboard and app URLs (and the
  JWT's issuer) use it; the address keeps answering. Delegated (NS) mode and per-app
  custom domains on it come later.

Also in v0.9.16 — the launcher and the workspace's look:

- **The launcher is where everyone lands** after signing in (`/`): the workspace's apps as
  tiles — every app the person may open and every public one — with a search, the
  **featured** apps first and larger, and a one-line **description** under each name
  (both set on the project page or with `shpyrd projects describe`). People who build have
  a Projects page (`/projects`) a link away; people who only use apps see nothing else.
- **Branding**: a logo and an accent colour on Workspace › Overview, shown by the launcher,
  the header and the login page instead of the platform's.

Known gaps (the model promises these; the code does not do them yet): an `identified` app
sees a signed-in person only after the browser crossed to it through `/.shpyrd/signin` once
(the launcher and Open do; a typed URL does not) — RFC-0068 fixes it; workspace delete;
delegated (NS) custom domains and PSL submission; SAML methods and step-up on claimed
domains; a way to disable previews; one signing key ring per platform rather than
per workspace; the legacy `Team`/`ProjectMember` CRDs still ship; `run`, `globals`,
`sizes`, `extensions` still need a kubeconfig; `shpyrd login` has no browser flow. OAuth
for agents follows in later releases; the full text is published when it settles.

## Implementation History

- 2026-09-22: RFC written; blocked on the definition.
- 2026-09-26: definition settled internally; phase 1 (control-plane database, implicit
  workspace, Workspace page) shipped in v0.4.0.
- 2026-09-26: phase 2 (the `user` role, access modes, the edge, previews, launcher) shipped
  in v0.5.0.
- 2026-09-26: phase 3 (sessions in the database, login methods, join policy, domain claims,
  the `everyone` team, suspension) shipped in v0.6.0.
- 2026-09-26: phase 5 (allow lists) shipped in v0.7.0; phase 4 (CLIs) in v0.8.0; phase 6's
  first slice (the workspace resolved from the host) in v0.9.1, its second (sign-in at the
  workspace host through the platform's dashboard) in v0.9.2, its third (plan limits) in
  v0.9.3; the CLI over the API for every developer command in v0.9.8.
- 2026-09-27: audit of the model against the code; allow lists, suspension, backups,
  custom-domain tenancy and globals fixed (v0.9.10); gaps listed above.
- 2026-09-27: tokens at the edge, sign-out, key rotation, the denied page and API 401s,
  the server's NetworkPolicy, denial counters and audit realm (v0.9.11).
- 2026-09-27: workspace roles (`owner`/`admin`/`member`), invitations, the mail extension
  (v0.9.13).
- 2026-09-27: per-workspace SSO, the email-first login step for claimed domains, the
  `reader` role (v0.9.15).
- 2026-09-27: address change with redirects, custom workspace domains in CNAME mode with a
  primary; the launcher for everyone with search, featured apps and descriptions; workspace
  branding (v0.9.16).
