---
title: Dashboard
description: The web UI - the launcher, a project's pages, the workspace's pages, and the console where the operator runs the platform.
---

The dashboard is your workspace's address: on shpyrd cloud, `https://<workspace>.shpyrd.cloud`, such as `https://acme.shpyrd.cloud`; on a cluster you run yourself, `https://<domain>`. Everything the CLI does for projects can be done there, and it is the place to watch what is happening. On a cluster you run yourself, the operator also has the **console**, at `https://shpyrd.<domain>`. {% .lead %}

## Signing in

On shpyrd cloud you sign in at your workspace's address with your account, or with your company's sign-in once the workspace has one ([People, teams, roles and security](/docs/access)). The menu under your name says how you signed in and has **Sign out**.

{% callout title="Self-hosted" %}
`shpyrd cluster dashboard` opens the console signed in through a one-time ticket; the login pages also accept the admin token pasted by hand (`shpyrd cluster token`). With the `auth-local` extension enabled, the login page asks for the email and password of accounts created with `shpyrd-ctl users add` or on the console's **Accounts** page. See [Extensions and sign-in](/docs/extensions).
{% /callout %}

## The launcher

Everyone lands on the **launcher** after signing in: a card for every app they may open (public apps included), sorted by name. Each card has the app's icon, its one-line description, who may open it (*Anyone* for a public app, *Everyone* when the `everyone` team has access, else its teams by name), whether it is internal, and the address it opens: its own domain once that domain answers, else its address under the workspace.

People who build open a project from the settings button on its card. The **+** button (**New project**) creates one: a slug, a name, a line of description, the network (public internet or internal, where the workspace has private networking), and optionally a Git repository to build and release right away. The gear opens the workspace's pages (owners and admins). People whose only roles are `user` or `reader` see the cards and nothing else.

A project's name, description and icon are set from the pencil next to its name (or `shpyrd projects describe`): a symbol in one of 14 colours, or an image of its own, an SVG drawn in the colour or a PNG or WebP shown as it is. The workspace's **Name and look** (Workspace › General) puts your logo and accent colour on the launcher, the header and the login page. See [Sign-in for your app](/docs/app-access).

## A project

A project's header has its name and slug, its phase, the network badge (**public internet** or **local network**; developers and admins click it to switch front doors where private networking is on), a lock badge when visitors sign in, and the actions: **Open**, **Deploy** (from Git, with buildpacks or a Dockerfile; its menu also has **Redeploy the current release**, or **Build the same source again** after a failed build, and **Rebuild from the source**) and the trash icon to destroy the project (you type its slug to confirm).

Under the header, only while something is happening: the build's output as it prints; the release command's output, with **Run it again** when it failed; the rollout per process ("web 1/3 on the new release, 3 serving"); or why the build failed or the release is not healthy.

The pages are in a list at the side:

- **Overview**: the **Summary** (source, build strategy, current release and build, each process' size and count) and the latest **Activity**.
- Run
  - **Releases**: the releases (what changed, build number, **Rollback**) and the **Builds** (status, strategy, source, duration; open one to read its output, live while building).
  - **Backups** (admins): back the project up to a file and restore it.
  - **Metrics**: see below.
  - **Logs**: every instance streamed live, named `web.1`, `worker.2`. Pick a process and a minimum level, filter by text, switch **Live** off to pause, **Raw** to see lines as written. See [Logs](/docs/logs).
  - **Shell** (developers and admins): a shell in a running instance.
- Configure
  - **Config**: **Config vars**, by name; set, replace, remove, or **Set from a .env**. Values are never shown. Variables of attached resources are listed read-only. **Plain environment** shows what every process sees besides them.
  - **Resources**: **Processes** (size and number of instances of each, applied together in one release) and **Beside the application** (databases, caches and volumes: create, resize, snapshots).
  - **Domains**: the project's own domains, with the exact DNS record to create and each one's DNS and certificate state. See [Domains and exposure](/docs/domains).
  - **Drains**: the project's log drains and their delivery status.
- Access
  - **Access**: **Who may open it** and **Open as**. See below.
  - **Roles** (admins): who holds `reader`, `user`, `viewer`, `developer` or `admin`, people and teams.
  - **Connections**: which other projects, and which platform callers, may reach this one inside the cluster.
  - **Activity**: the audit trail.

Actions that would start another release are disabled while one is building or rolling out, and everything a role cannot do is hidden or disabled ([People, teams, roles and security](/docs/access)).

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

Self-hosted: Grafana, linked from the console's menu under your name, has the same data with the pre-provisioned "shpyrd / Web apps" dashboard and everything kube-prometheus-stack ships.

## Access

The project's **Access** page says who may open the app in the **Who may open it** card: **Anyone** (public), **Who signs in** (sign-in required) or **Named people** (public, signed-in visitors identified). Project admins change it; making an app public asks for confirmation, and a badge says "Anyone on the internet may open it" while it is. **Open as** (developers and admins) opens the app in a new tab as the teams of your choice, or as nobody, so builders see what their users see; **Open as myself** ends the preview. See [Sign-in for your app](/docs/app-access).

## Workspace

Every project, team and person belongs to the workspace. Its pages are in a list at the side:

- **General**: the name, the **Name and look** (name, accent colour, logo) and, where the workspace has one, its **Plan**.
- Access
  - **People** (owners and admins): the people with their workspace role (owner, admin, member; only owners name owners), the method they signed in with, when they were last seen, **Suspend** and **Let back in**, and the **Invite** dialog, whose link is shown once and emailed when the platform sends mail, with the pending **Invitations** below.
  - **Teams** (owners and admins): the teams projects grant roles to.
  - **Sign-in** (owners and admins): the sign-in methods (Google, Microsoft, GitHub, any OpenID Connect provider), **Who may join** on first sign-in, and the **Company domains**.
  - **API tokens**: your tokens, and the session tokens of your CLI logins.
  - **Connections** (with the MCP server): the assistants and pipelines connected to the workspace.
- Platform
  - **Config vars** (owners and admins): config vars every project receives.
  - **Log drains** (owners and admins): drains for every project's lines.
  - **Domains** (owners): the workspace's address and its custom domains. See [Domains and exposure](/docs/domains#the-workspace-s-own-names).
  - **MCP** (with the MCP server): the name and address assistants use.

See [People, teams, roles and security](/docs/access).

## The console

Self-hosted: on a cluster you run yourself, the operator has the **console** at `https://shpyrd.<domain>`; on shpyrd cloud, we run it. Its users are its own list (**Console users**), apart from every workspace's people. Its pages:

- **Overview**: the cluster as installed (environment, version, domain, the public and private front doors) and its **Capacity**: CPU and memory **used** (what the machines are doing) versus **reserved** (what running processes asked for, which limits scheduling), over the last hour, 6 hours, 24 hours or 7 days.
- Platform: **Workspace** (a link to the workspace), **Console users**, **Accounts** (with `auth-local`), **Sign-in** (the platform's methods), **Costs** and **Cost drains** (with the `costs` extension), and **Settings** (the workspace's limits and sleep defaults, and the license).
- Cluster: **Instances** (the size catalog, in three lists with the same names: processes, Postgres and Redis; add, change, delete sizes and pick each list's default), **Placement** (where each project's processes and data run), **Registry** (mode, health, storage, images, certificate, garbage collection with **Collect now**), **Storage** (with `object-storage`), **Backups** (the platform backups and their archives), **Email** (with `mail`; the sender's status and a test message) and **Components** (the extensions with their state, the installed components with versions, and the Helm releases).

Cluster-wide log drains have no page: the operator adds them with `shpyrd drains add --cluster` ([Logs](/docs/logs)).

## Security notes

- Every `/api` route needs a signed-in session or a token, except the health check, the public configuration and the content-addressed source archives fetched by build pods.
- Config var values are write-only through the API and the UI.
- Image references and internal addresses are not exposed on project pages; builds and releases are identified by number and digest. The registry's address appears in the console only.
- On the local profile the dashboard is only reachable from your machine.
