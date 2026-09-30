---
title: Dashboard
description: The web UI - projects, releases, builds, logs, metrics and cluster capacity.
---

The dashboard is served by the shpyrd server inside the cluster at `https://shpyrd.<domain>`. Everything the CLI does for projects can be done there, and it is the place to watch what is happening. {% .lead %}

## Signing in

`shpyrd cluster dashboard` opens the dashboard signed in with the admin token; the login page also accepts the token pasted by hand (`shpyrd cluster token`). With the `auth-local` extension enabled, the login page asks for the email and password of accounts created with `shpyrd users add` or on the **Workspace** page (Accounts tab); the header shows who is signed in and has a sign-out entry. See [Extensions and sign-in](/docs/extensions).

## Projects

The **Projects** page lists every project with its phase, current release, per-process health (`web 3/3`) and URL, plus counters for the cluster. **New project** creates one, optionally pointing it at a public Git repository to build and deploy immediately.

A project page has:

- **Header**: phase, the exposure badge (external or internal; project admins click it to switch front doors), process chips (green when all instances are on the current release and ready, amber while rolling out, red with a count when instances are failing), the URL, and the **Open**, **Redeploy** (new instances of the current release, or **Retry build** after a failed build), **Deploy** (from Git, with buildpacks or a Dockerfile) and **Destroy** actions (the dialog lists every resource that goes, data-holding ones first).
- **Activity panel**, only when something is happening: live build output while building; per-process rollout progress ("1/3 on new release · 3 serving") while deploying; the container's reason, a redeploy and a one-click rollback when a release is not healthy.
- **Overview**: source and build strategy, current release and build, per-process instances with scale buttons and an instance size selector (pinned to one for processes mounting a single-instance volume), the **Resources** card (the app, its volumes and databases, with status and what uses them; create, resize and delete volumes there), the **Domains** card (the project hostname, custom domains with the exact DNS record to create and each one's DNS and certificate state; add and remove), the log drains, and the **Releases** table (kind badge, what changed, build number, rollback button).
- **Metrics**: see below.
- **Logs**: every instance streamed live, named `web.1`, `worker.2`; filter box, pause/live, error and warning highlighting.
- **Builds**: build history with status, strategy, reason, source and duration; select one to read its full output (live while building), and which releases use it.
- **Config**: config var names and last-updated times; add, replace (blind), remove, or paste a `.env`. Variables provided by attached resources are listed read-only with their provider. Values are never shown.

Actions that would start another release (Deploy, Rollback) are disabled while one is building or rolling out, and everything a role cannot do is hidden or disabled ([teams and roles](/docs/access)). Project admins get a **Members** card; every project shows its **Recent actions** (the audit trail).

## Metrics

Modelled on what Heroku, Fly and Render show for an application:

| Chart | What it shows |
| --- | --- |
| Throughput | requests per second at the ingress, stacked by response class (2xx, 3xx, 4xx, 5xx) |
| Response time | p50, p95 and p99 latency at the ingress |
| Instances | running instances per process type (step chart) |
| CPU | usage as a percentage of each process' allocation, averaged over its instances; 100% means every instance saturating its CPU |
| Memory | working set as a percentage of each process' allocation |
| Network | instance traffic in and out |

Orange dashed lines mark releases; a red line marks 100%. Ranges: last hour, 6 hours, 24 hours, 7 days. 100% is the process' instance size (its allocation); shared sizes can read above 100% while bursting.

Grafana, linked from the header, has the same data with the pre-provisioned "shpyrd / Web apps" dashboard and everything kube-prometheus-stack ships.

## Cluster

The **Cluster** page shows the environment profile, the running server version, the domain and, on cloud profiles, the **front doors** (the external and internal load balancer addresses); **capacity**: CPU and memory **used** (what the machines are doing) versus **reserved** (what running processes have requested, which is what limits scheduling), in total and per node (with each node's machine shape and zone), with utilisation over time; the **Registry** card for platform admins (in-cluster or external, health, storage used, images held, the weekly garbage collection with a **Collect now** button, certificate expiry); the **instance size catalog** (add, change, delete sizes and pick the default); global config vars and cluster-wide log drains; the **extensions** with their state; the installed components with versions; and the Helm releases in the cluster.

## Security notes

- Every `/api` route requires the admin token except the health check, the public configuration and content-addressed source archives fetched by build pods.
- Config var values are write-only through the API and the UI.
- Image references and internal addresses are not exposed on project pages; builds and releases are identified by digest. The registry's address appears on the cluster page only, for platform admins.
- On the local profile the dashboard is only reachable from your machine.

## Workspace

Every project, team and person belongs to the workspace — the open-source platform has one. The **Workspace** page (owners and admins) shows its name (editable) and owners, the **People** of the workspace with their workspace role (owner, admin, member — set from the list; only owners name owners), the login method they used last and an **Invite** dialog whose link is shown once and emailed when the platform sends mail, with the pending invitations below; the **Teams** projects grant roles to; and, with `auth-local`, the **Accounts** it manages. The **Sign-in** tab manages the login methods (Google, Microsoft, GitHub, any OpenID Connect provider), who may join on first sign-in, and the company's claimed email domains; the People tab can suspend someone. The old `/teams` and `/users` links land on the matching tab. See [People, teams, roles and security](/docs/access).

## Access and the launcher

The project page's **Access** card says who may open the app — sign-in required, public, or public with signed-in visitors identified — and lets project admins change it (making an app public asks for confirmation). **Open as** opens the app in a new tab as a team of your choice, or anonymous, so builders see what their users see. The **Roles** card grants `user`, `viewer`, `developer` and `admin` to people and teams.

Everyone lands on the **launcher** after signing in: tiles for the apps they may open (public apps included), the featured ones first and larger, each with its one-line description, and a search once there are more than a few. People who build reach their projects from the **Projects** link; people whose only roles are `user` or `reader` see nothing else. A project's name, description and featured flag are set from the pencil on its page (or `shpyrd projects describe`). The workspace's **Look** (Overview) puts your logo and accent colour on the launcher, the header and the login page. See [Sign-in for your app](/docs/app-access).
