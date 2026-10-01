---
title: Getting started
pageTitle: shpyrd - Getting started
description: Sign up for shpyrd cloud, deploy your first app from your folder and share it with your team - or run shpyrd on a cluster of your own.
---

shpyrd puts the apps you build online and in front of the people you choose: push code, get a URL with TLS and a sign-in in front of it, with logs, metrics, releases and rollbacks - no Dockerfiles, no YAML, and nothing to install or operate on shpyrd cloud. {% .lead %}

{% quick-links %}
{% quick-link title="Deploying" icon="presets" href="/docs/deploying" description="Create a project, deploy from a checkout or a Git URL, set config vars, scale, watch logs, roll back." /%}
{% quick-link title="Sign-in for your app" icon="theming" href="/docs/app-access" description="Who may open an app is decided before a request reaches it; your app is told who they are." /%}
{% quick-link title="AI assistants (MCP)" icon="plugins" href="/docs/mcp" description="Add your workspace to the agent you already use, and ask it about your projects." /%}
{% quick-link title="Running it yourself" icon="installation" href="/docs/installation" description="shpyrd is open source: install it on a laptop, on AWS or on Oracle Cloud." /%}
{% /quick-links %}

## Your first deploy

### 1. Create your workspace

Sign up for shpyrd cloud and name your workspace. It answers at its own address - `acme.shpyrd.app` - and every app you deploy gets an address under it: `shop.acme.shpyrd.app`. Your company's own domain can come later ([Domains](/docs/domains)).

### 2. Install the CLI

```shell
brew install shpyrd-io/tap/shpyrd                 # macOS
curl -fsSL https://shpyrd.io/install.sh | sh      # macOS or Linux
```

### 3. Sign in

Create a personal API token in the dashboard (**Workspace › API tokens**), then sign the CLI in to your workspace with it:

```shell
shpyrd login --url https://acme.shpyrd.app --token shp_…
```

### 4. Deploy

From the folder your app is in:

```shell
shpyrd projects create "Purchase requests"
shpyrd deploy
shpyrd open                     # https://purchase-requests.acme.shpyrd.app
```

shpyrd builds it from source with [Cloud Native Buildpacks](https://buildpacks.io) (Go, Node.js, Java, Python, Ruby, .NET, static sites), or with your `Dockerfile` when there is one, and releases it. New projects ask visitors to sign in: nobody gets in until you say so.

### 5. Share it

Give it to a team, or to a person:

```shell
shpyrd members add purchase-requests --team finance --role user
shpyrd members add purchase-requests --user ana@acme.com --role user
```

They sign in with their account and find it among their apps. [Sign-in for your app](/docs/app-access) and [Teams, roles and security](/docs/access) say who can do what.

### Or ask your agent

The steps above are ordinary commands, so the coding agent you built the app with can run them for you - "put this on shpyrd and share it with Finance". Add your workspace to the agent as well ([AI assistants](/docs/mcp)) and it can read your projects, their logs and their metrics.

## What you get

- **Deploys from source.** `shpyrd deploy` archives your checkout (or points at a Git URL) and builds it with buildpacks, or with your `Dockerfile`. No manifests.
- **Sign-in in front of every app.** People sign in before a request reaches the app; the app is told who they are in headers and a signed JWT, with no sign-in code of its own.
- **Releases you can trust.** Every deploy, config change or rollback is a numbered release that records its build and its config vars. Rolling back restores both.
- **Config vars that stay secret.** Set, replace and remove them from the CLI or the dashboard; values are never shown again.
- **Processes, Heroku style.** A project can run several process types (`web`, `worker`, ...). Only `web` gets a URL; a project without `web` is a background worker or an agent.
- **Databases and caches.** PostgreSQL and Redis-compatible stores, attached to the app as `DATABASE_URL` and `REDIS_URL` ([Databases and caches](/docs/databases)).
- **Apps that sleep when nobody uses them.** An idle app can sleep and wake on the next request.
- **Logs and metrics out of the box.** Instance-named logs (`web.1`, `worker.2`), throughput by status class, response time percentiles, CPU and memory.
- **Teams and roles.** Your company's sign-in, groups as teams, and separate rights to use an app, to change it and to manage it.

## Running it yourself

shpyrd is open source (MPL-2.0). The same platform installs on a Kubernetes cluster you run - a local [kind](https://kind.sigs.k8s.io) cluster from one command, [Oracle Cloud (OKE)](/docs/oracle-cloud) or [AWS (EKS)](/docs/aws):

```shell
brew install shpyrd-io/tap/shpyrd
shpyrd cluster create        # kind cluster + base stack, 10-20 min the first time
shpyrd cluster dashboard     # opens the dashboard, signed in
```

[Installation](/docs/installation) has the requirements and every step.

## Status

shpyrd is in beta and developed in the open. The [roadmap](/docs/roadmap) shows what is done and what comes next; [how to contribute](/docs/how-to-contribute) explains the way in.

## Getting help

- Bugs, ideas and questions: [GitHub issues](https://github.com/shpyrd-io/shpyrd/issues).
- Design changes go through short [RFCs](https://github.com/shpyrd-io/shpyrd/tree/main/rfcs).
- Community chat: [Discord](https://discord.gg/RYAT4wNKfw).
