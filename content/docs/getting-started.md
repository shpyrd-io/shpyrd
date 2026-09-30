---
title: Getting started
pageTitle: Shpyrd - Opensource Cloud PaaS
description: Manage applications and agents stack from one place, from deploy to monitoring.
---

Shpyrd turns a Kubernetes cluster into a platform you can `deploy` to: push code, get a URL with TLS, logs, metrics, releases and rollbacks, without writing Dockerfiles or YAML. {% .lead %}

{% quick-links %}

{% quick-link title="Installation" icon="installation" href="/docs/installation" description="Create a local cluster with the base stack in one command, or install it on an existing cluster." /%}

{% quick-link title="Deploying" icon="presets" href="/docs/deploying" description="Create a project, deploy from a checkout or a Git URL, set config vars, scale, watch logs, roll back." /%}

{% quick-link title="Dashboard" icon="theming" href="/docs/dashboard" description="Projects, releases, builds, streamed logs and Heroku-style metrics in a web UI." /%}

{% quick-link title="Architecture guide" icon="plugins" href="/docs/architecture-guide" description="How the installer, the App controller and the server fit together." /%}

{% /quick-links %}

---

## What you get

[![The shop project in the dashboard](/screenshots/project-overview.png)](/docs/tour)

Take the [tour](/docs/tour) for every feature with a screenshot.

- **Deploys from source.** `shpyrd deploy` archives your checkout (or points at a Git URL); the cluster builds it with [Cloud Native Buildpacks](https://buildpacks.io) (Go, Node.js, Java, Python, Ruby, .NET, static sites), or with your `Dockerfile` when there is one, and rolls it out. No manifests.
- **Processes, Heroku style.** A project can run several process types (`web`, `worker`, ...). Only `web` gets a URL; a project without `web` is a background worker or an agent.
- **Releases you can trust.** Every deploy, config change or rollback is a numbered release that records its build and its config vars. Rolling back restores both.
- **Config vars that stay secret.** Set, replace and remove them from the CLI or the dashboard; values are never shown again.
- **Accounts, teams and roles.** The dashboard starts with an admin token; enable `auth-local` for email/password accounts, grant viewer, developer or admin roles per project, and get the same view in `kubectl` through the RBAC mirror. Projects are isolated from each other and every action is audited.
- **Databases and caches.** `shpyrd pg create db` and `shpyrd redis create cache`, then `shpyrd attach` to get `DATABASE_URL` and `REDIS_URL` in the app, Heroku style.
- **A shell when you need one.** `shpyrd shell` into a running instance, `shpyrd run` for migrations and scripts, `shpyrd volumes` for disks that survive deploys.
- **Logs and metrics out of the box.** Instance-named logs (`web.1`, `worker.2`), throughput by status class, response time percentiles, CPU and memory as a percentage of each process' allocation, cluster capacity.
- **One binary, no cloud account.** The whole base stack (cert-manager, ingress, registry, kpack, Prometheus, Grafana) is installed by the CLI in dependency-ordered runlevels on a local [kind](https://kind.sigs.k8s.io) cluster today; cloud profiles come next.

## Quick start

Requirements: Docker (Docker Desktop with 6-8 GB of memory), macOS or Linux.

```shell
brew install shpyrd-io/tap/shpyrd                 # macOS; or: curl -fsSL https://shpyrd.io/install.sh | sh
shpyrd cluster create        # kind cluster + base stack, 10-20 min the first time
shpyrd cluster trust-ca      # trust the development CA (asks for sudo)
shpyrd cluster dashboard     # opens https://shpyrd.127.0.0.1.nip.io signed in
```

Then deploy the bundled example, a Go module with a `web` and a `worker` process:

```shell
shpyrd projects create hello-world
cd examples/hello && shpyrd deploy
shpyrd open                              # https://hello-world.127.0.0.1.nip.io
shpyrd secrets set GREETING="Olá mundo"  # new release, the page picks it up
shpyrd scale web=3 worker=2
shpyrd logs -f
```

{% callout title="Ports 80 and 443 taken?" %}
`shpyrd cluster create --http-port 8080 --https-port 8443` maps other host ports; URLs then carry the port (`https://hello-world.127.0.0.1.nip.io:8443`).
{% /callout %}

## Status

Shpyrd is in beta: it runs on a local kind cluster and on [Oracle Cloud](/docs/oracle-cloud), and is developed in the open. The [roadmap](/docs/roadmap) shows what is done and what comes next; [how to contribute](/docs/how-to-contribute) explains the way in.

## Getting help

- Bugs, ideas and questions: [GitHub issues](https://github.com/shpyrd-io/shpyrd/issues).
- Design changes go through short [RFCs](https://github.com/shpyrd-io/shpyrd/tree/main/rfcs).
- Community chat: [Discord](https://discord.gg/AxWMXXW7).
