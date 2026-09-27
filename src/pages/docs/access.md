---
title: People, teams, roles and security
description: Who is in the workspace and what they may do, how roles map to Kubernetes RBAC, and the isolation and audit that come with every project.
---

Identity says who you are; roles say what you may do. A person holds a **workspace role** (owner, admin or member), projects grant **project roles** to people and teams, the API enforces them, the dashboard hides what a role cannot do, and a controller mirrors the grants into Kubernetes RBAC. {% .lead %}

## Workspace roles

| Role | May |
| --- | --- |
| `owner` | everything an admin may, and name or demote owners |
| `admin` | administer the workspace — people, invitations, teams, sign-in settings, every project — and the cluster pages on a self-hosted install |
| `member` | create projects, and administer the ones they create; on other projects, what grants give them |
| *(none)* | what grants give them |

Roles are set on the **People** tab of the Workspace page or with `shpyrd people role <email> <role>`; the person need not have signed in yet, the role waits for them. The last owner cannot step down: name another owner first. Owners and admins are the workspace's *platform admins*; the older way of making someone a platform admin — a team with a `platformRole` — still works for people without a workspace role, and the People tab says so ("platform-admin through a team"). A workspace role, once set, decides.

## Inviting people

```shell
shpyrd invite ada@example.com                        # a member
shpyrd invite bob@example.com --role admin --team web
shpyrd invitations                                   # pending
shpyrd invitations revoke bob@example.com
```

An invitation is a link, shown once, that works for seven days — and, when the platform [sends email](#email), it is emailed too. Signing in with the invited address **accepts it, link or no link**: the person gets the role (and the team) the moment they sign in, through whatever sign-in method gives that address, whatever the workspace's join policy. Inviting someone who has signed in before applies the role at once; inviting the same address again makes a new link. The People tab has the same dialog and a list of pending invitations; `/invite/<token>` shows the holder what they were invited to.

## Project roles

| Role | Sees | Does |
| --- | --- | --- |
| `user` | the app itself (through the edge), the launcher | opens the app; nothing in the builder dashboard — see [Sign-in for your app](/docs/app-access) |
| `viewer` | overview, releases, builds, logs, metrics, config var **names**; opens the app | nothing |
| `developer` | everything a viewer sees | deploy, roll back, scale, resize, set and unset config vars, shell and one-off commands |
| `admin` | + members, resources | attach and detach resources, create volumes, manage members, destroy the project |
| `platform-admin` | everything, cluster page, extensions, teams, users | everything (what a workspace owner or admin holds) |
| `platform-viewer` | everything, read-only | nothing |

The first four are granted **per project**, to a user (by email) or to a **team**; every operating role opens the app too. A refusal is a plain sentence: *your role on project shop is developer: it cannot destroy the project*.

## Teams and members

```shell
shpyrd teams create platform --platform-role platform-admin --member you@example.com
shpyrd teams create web --member ada@example.com --group engineering
shpyrd teams add web bob@example.com
shpyrd teams list

shpyrd members add shop --team web --role developer
shpyrd members add shop --user guest@example.com --role viewer
shpyrd members list shop
shpyrd members remove shop --user guest@example.com
```

Team `groups` are names from your identity provider's groups claim: a company directory group maps to a team without listing people twice. The built-in team **everyone** holds every person who has signed in — grant it the `user` role and the whole company can open an app (`shpyrd members add intranet --team everyone --role user`); it cannot be edited or deleted, and kubectl bindings skip it. The dashboard has the same operations: the **Workspace** page (Teams tab) for platform admins and a **Members** card on every project for its admins.

Teams and grants live in the platform's **control-plane database** (a small PostgreSQL the base stack runs as `control-plane-db`, or the managed database named by `SHPYRD_DATABASE_URL`), together with the workspace's record of who has signed in (the Workspace page, People tab). Installs made before v0.4 kept them as Kubernetes objects (`Team`, `ProjectMember`); the first server start after the upgrade copies them into the database and marks the objects migrated — nothing to do by hand. The platform backup carries the database's content (`shpyrd cluster backups`).

{% callout title="Before the first role" %}
A fresh cluster has no roles, teams or members, and every signed-in user is a platform admin so nothing is locked. The first role, invitation, team or grant switches enforcement on — and the person who writes it becomes the workspace's **owner** at the same moment, so defining who is who never locks you out. The admin token is always a platform admin and holds the owner's actions.
{% /callout %}

## Suspending someone

The People tab of the Workspace page (or `shpyrd people suspend <email>`) switches a person off at once: no role anywhere, no app opens, sign-in refused — until reactivated. Removing them from the identity provider does the same at the session's end; suspension is for right now. `shpyrd people forget` removes the sign-in record; the role and grants stay.

## Email

Invitations carry their link by email once the platform has a sender. Enable the `mail` extension and point it at your SMTP server (STARTTLS by default; `--tls` for port 465):

```shell
shpyrd-ctl extensions enable mail
shpyrd-ctl mail set --host smtp.example.com --user postmaster@example.com \
    --password @/path/to/password --from "shpyrd <noreply@example.com>"
shpyrd-ctl mail test you@example.com
```

The password stays in the cluster (Secret `shpyrd-mail`) and is never printed; the test message is sent by the server, from inside the cluster, so it proves the settings, the network path and the sender address at once. The Cluster page shows an **Email** card with the status and the same test. Without a sender, invitations show their link to whoever invites, to pass along.

## Kubernetes RBAC mirror

For every project namespace the controller keeps `RoleBinding`s (`shpyrd-viewer`, `shpyrd-developer`, `shpyrd-admin`) bound to the fixed ClusterRoles `shpyrd-project-*`, with the users (by email) and groups holding each role; platform roles become `ClusterRoleBinding`s. The Kubernetes roles grant the same verbs the dashboard allows (developers can update the App and open shells in its instances; config var Secrets stay write-only), never more than shpyrd itself has.

Configure your API server with the same OIDC issuer (`--oidc-issuer-url`, `--oidc-username-claim=email`, `--oidc-groups-claim=groups`) and `kubectl` users get exactly the dashboard's view. The local kind cluster is not configured this way out of the box; the bindings are still created and visible with `kubectl get rolebindings -n app-<project>`.

## Isolation and hardening

Every project namespace gets:

- **A network policy.** Ingress only from the project's own instances, the ingress controller (your URL) and the monitoring namespace (metrics). Egress to the project, to platform namespaces (DNS, the registry, bound services) and to the internet, never to other projects. Projects talk to each other only through what a binding exposes.
- **Pod security.** The `restricted` Pod Security Standard is applied in warn and audit mode, and shpyrd runs every process (and every `shpyrd run` instance) as a non-root user with all capabilities dropped and the runtime's default seccomp profile. Buildpack images comply already; a Dockerfile needs a numeric `USER` (`USER 1000`): a named user cannot be verified by Kubernetes and is refused with an explanation, as is an image running as root.
- **Hardened dashboard.** HttpOnly session cookies with CSRF tokens, security headers with a strict Content Security Policy, and a rate limit on sign-in.

## The admin token

The token created at install time is a shared credential with full platform-admin rights, meant for bootstrap and automation. Obtaining it requires reading Secrets in `shpyrd-system` (`shpyrd cluster token`), which is cluster-admin access; the risk is in the copies you hand out. Keep it in check:

- `shpyrd cluster dashboard` does **not** put the token in the browser: it mints a one-time login ticket (a hashed Secret valid for 60 seconds) that the browser redeems for a normal session, attributed to you as `user@host` in the audit trail.
- `shpyrd cluster token --rotate` replaces it and restarts the server; update automation that used the old value.
- `shpyrd cluster token --disable` switches it off once accounts exist and a `platform-admin` team has members: the API then refuses the token and everyone signs in with an account. `--enable` turns it back on.
- Wrong tokens are audited and throttled per client (20 attempts a minute, after which even the right token waits).

## API tokens

For CI, scripts and other machines, people create their own **API tokens** instead of sharing the admin token: Workspace → **API tokens** in the dashboard, or `shpyrd tokens create ci --project shop --role developer --expires 90d`. A token is `shp_<id>_<random>`, shown once and stored hashed. It carries a platform role or a role on one project, never above what its owner holds: the check runs when the token is *used*, so a demoted owner's token is demoted with them and a suspended owner's tokens stop working at once. Expiry is optional and recommended; revocation (`shpyrd tokens revoke <id>`, or the trash icon) is immediate. A token cannot create tokens.

Use it with `shpyrd login --url https://shpyrd.example.com --token shp_...` on a laptop, or `SHPYRD_URL` and `SHPYRD_TOKEN` in CI. The audit trail names both the person and the token (`ana@example.com (token ci)`).

## Audit trail

Every mutation is recorded as `{who, what, target, detail, when, from, via}`: deploys, rollbacks, scaling and resizing, config var changes (names, never values), shells and one-off commands, volume and membership changes, project creation and destruction, from the dashboard and API (`via: api`, with the signed-in user) and from the CLI (`via: cli`, with the local user and host). The project page shows the recent actions; `GET /api/projects/{slug}/audit` returns them. Entries are Kubernetes Events, kept for the API server's event TTL (an hour by default) until durable storage arrives.

## Not yet

Resource quotas per project, image signing and CVE reporting, per-user API tokens and an enforcing Pod Security mode are on the roadmap ([RFC-0008](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0008-teams-roles-and-security.md)).
