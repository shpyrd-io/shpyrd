---
title: Tour
description: The dashboard of a shpyrd cloud workspace, feature by feature, with three sample projects deployed.
---

Every workspace on shpyrd cloud has a dashboard at its own address. These screenshots come from **acme.shpyrd.app**, running the three sample projects in `examples/`: **shop** (Go, buildpacks, a `web` and a `worker` process, an attached PostgreSQL database and a Valkey cache), **blog** (Node.js, buildpacks, a persistent volume) and **api** (Python, built from a Dockerfile). Everything below is also available from the CLI, which is how your agent does it. {% .lead %}

## Signing in

![Login page](/screenshots/login.png)

The workspace's login page, at its own address. People sign in with their shpyrd account, or with your company's sign-in once the workspace has one - Google Workspace, Microsoft Entra, GitHub or any OpenID Connect provider - and the page carries the workspace's logo and colour. The CLI signs in through the same page: `shpyrd login` opens it in the browser.

## The launcher

![Launcher](/screenshots/launcher.png)

Where everyone lands after signing in: a tile for each app they may open, the featured ones first, each with its one-line description. People who only use apps see nothing else; people who build reach their projects from the **Projects** link.

## Projects

![Projects list](/screenshots/projects.png)

Every project with its phase, current release, per-process health and URL. A project is what you deploy to: a name, a URL, config vars and a set of resources.

## A project

![Project overview](/screenshots/project-overview.png)

The overview of `shop`: source and build strategy, the current release and its build, and the **Processes** card where instance counts and sizes are edited as a draft and applied at once (one release). Below it the **Resources**, **Access**, **Roles**, **Recent actions** and **Releases** cards.

### Resources and attachments

![Resources card](/screenshots/resources-card.png)

Everything in the project: the app, its volumes, databases and caches, with status, endpoint and what uses them. `db` (PostgreSQL 17 on CloudNativePG) and `cache` (Valkey 8) are attached to the app: their connection details are injected as `DATABASE_*` and `REDIS_*` config vars. **Detach** removes them in a new release; **Add resource** creates a volume, a database or a cache.

![Add resource menu](/screenshots/add-resource.png)

### Releases and rollback

![Releases card](/screenshots/releases-card.png)

A release is a build plus the config in effect. Six releases of `shop`: the initial deploy, two attachments and a config change (all `config` releases reusing build #1), a second deploy (build #2) and a rollback to v4. Rollback re-releases a build together with its config vars, sizes and attachments.

### Access and roles

![Access card](/screenshots/access-card.png)

Who may open the app: sign-in required (the default), public, or public with signed-in visitors identified. **Open as** opens the app in a new tab as one of your teams, or as a stranger, so you see what your users see.

![Roles card](/screenshots/members-card.png)

Roles on the project - `reader`, `user`, `viewer`, `developer`, `admin` - granted to people or teams. Next to it, **Recent actions** lists who did what, from the dashboard, the CLI or an agent.

## Deploying

![Deploy dialog](/screenshots/deploy-dialog.png)

Deploy from a Git repository with buildpacks or a Dockerfile; local checkouts deploy with `shpyrd deploy`.

![Activity panel while building](/screenshots/activity-building.png)

While a build runs, the **Activity** panel streams its output; while a release rolls out it shows per-process progress, and when instances fail it shows the reason with a one-click rollback.

## Metrics

![Metrics tab](/screenshots/project-metrics.png)

Traffic at the edge by status class, response time percentiles, instances per process type, CPU and memory as a percentage of each process' allocation, network. Orange dashed lines mark releases; the red line is 100% of the allocation.

## Logs

![Logs tab](/screenshots/project-logs.png)

Every instance streamed live and named the Heroku way (`web.1`, `web.2`, `worker.1`), with a process filter, a text filter, level highlighting and pause. Here the `web` instances log JSON request lines and queue orders that `worker.1` fulfils.

## Builds

![Builds tab](/screenshots/project-builds.png)

Build history with strategy, source and duration; select one to read its full output, and see which releases use it.

## Config vars

![Config tab](/screenshots/project-config.png)

Config vars are write-only: names and last-updated times are shown, values never. Variables provided by attached resources (`DATABASE_URL`, `REDIS_URL`, ...) appear read-only with their provider and win over a config var of the same name.

## Volumes

![The blog project with a single-instance volume](/screenshots/project-blog.png)

`blog` mounts a persistent volume at `/data` for its visit counter. A single-instance volume pins the process to one instance (the scale control is disabled) and rolls out with Recreate; the data survives deploys.

## The workspace

![Workspace overview](/screenshots/workspace-overview.png)

The **Workspace** page, for its owners and admins. **Overview**: the name, the look (logo and accent colour), the workspace's own domains, and the address for AI assistants.

![People tab](/screenshots/workspace-people.png)

**People**: everyone in the workspace with their role (owner, admin, member) and how they last signed in, an **Invite** link, and suspension when someone has to be switched off at once.

![Teams tab](/screenshots/teams.png)

**Teams** group people - by email or by a group of your company's sign-in - so projects grant roles to many people at once.

![Sign-in tab](/screenshots/workspace-signin.png)

**Sign-in**: your company's login methods, who may join on first sign-in, and your claimed email domains. **API tokens** holds the tokens for CI and scripts. Billing is not a page of the workspace here: on shpyrd cloud it opens from the sidebar's **Cloud** section.

## Try it yourself

Sign in, then deploy the samples from a checkout of the repository:

```shell
shpyrd login
shpyrd projects create shop --public && cd examples/shop && shpyrd deploy   # a public shop; without --public visitors sign in
shpyrd pg create db --project shop && shpyrd redis create cache --project shop
shpyrd attach db && shpyrd attach cache
shpyrd projects create blog && shpyrd volumes create data --size 1Gi --project blog && (cd ../blog && shpyrd deploy)
shpyrd projects create api && (cd ../api && shpyrd deploy)
```

Or ask your agent to do it: "Deploy the three examples in examples/ to shpyrd."
