---
title: Deploying
description: Create a project, deploy it from a checkout or a Git URL, configure it, scale it, read its logs and roll back.
---

The CLI talks to your cluster with your kubeconfig; the only call that reaches the shpyrd server is the upload of your source archive. {% .lead %}

{% callout title="Examples" %}
[shpyrd-io/shpyrd-examples](https://github.com/shpyrd-io/shpyrd-examples) has fifteen small projects deployed to the demo workspace, one per language or pattern: Go, Node.js, Express, Python, Ruby, Sinatra, Rails, Java, .NET, PHP, static nginx, static httpd, React (Vite), Next.js, a multi-stage Dockerfile (Sinatra + React) and a Ruby app with system packages from an Aptfile.
{% /callout %}

## Create a project

```shell
shpyrd projects create "My Service"        # slug my-service: namespace app-my-service + App resource
shpyrd projects create "My Service" --save # also writes shpyrd.yaml (project: my-service) in the current directory
```

Names are lowercase letters, digits and dashes (max 40 characters) and become the hostname: `https://my-service.<domain>`.

A new project asks its visitors to sign in: only people with a role on it can open the app, and the app receives who they are. For a site anyone may open, create it with `--public` or switch later with `shpyrd access set public`. See [Sign-in for your app](/docs/app-access).

## Deploy

From inside your repository:

```shell
shpyrd deploy                 # app from shpyrd.yaml, or --project my-service
```

What happens:

1. **Archive.** The committed tree of the current directory (`git archive HEAD`) is packed; run it from a subdirectory to deploy just that service of a monorepo. Uncommitted changes are not included unless you pass `--working-tree` (also chosen automatically when nothing in the directory is committed yet). Outside a Git repository the directory is tarred.
2. **Upload.** The archive is sent to the cluster through the Kubernetes API server (no ingress or token needed).
3. **Build.** In the cluster, with the Paketo buildpacks (kpack) or, when the directory has a `Dockerfile`, with BuildKit; the CLI streams every step.
4. **Release.** The controller rolls the new image out process by process and prints the release number and URL.

```
==> Archiving HEAD:examples/hello (654f4925638e)
==> Uploading source (2.6 KiB)
==> Building
===> prepare
===> detect
4 of 9 buildpacks participating
===> build
    web (default): /layers/paketo-buildpacks_go-build/targets/bin/web
    worker:        /layers/paketo-buildpacks_go-build/targets/bin/worker
===> export
==> Releasing
    Deploying: Releasing v3: web 1/3 updated · worker 0/1 updated
    Running: web 3/3 · worker 1/1
Released v3: Deploy 654f4925638e
https://hello-world.127.0.0.1.nip.io
```

Other sources:

```shell
shpyrd deploy --git https://github.com/org/repo --ref main --path services/api   # new commits rebuild automatically
shpyrd deploy --image ghcr.io/org/repo:1.4.2                                   # run a prebuilt image, no build
shpyrd deploy --no-wait                                                         # do not follow the build
```

Deploying from Git is also available in the dashboard (**Deploy** button, or when creating the project).

{% callout title="Which languages?" %}
Anything the Paketo buildpacks understand: Go, Node.js, Java, Python, Ruby, .NET Core and static sites served by nginx or httpd. Repositories with a `Dockerfile` are built with BuildKit instead (below), and `--image` runs anything already built.
{% /callout %}

## Buildpacks: languages, stacks and system packages

The Paketo buildpacks detect the language from the repository and do the right thing for the common case. A few patterns need a hint in [`shpyrd.yaml`](/docs/shpyrd-yaml).

### Static sites

A directory with only static files (HTML, CSS, JS) needs `BP_WEB_SERVER` or the web-servers buildpack cannot detect it:

```yaml
# shpyrd.yaml
build:
  env:
    BP_WEB_SERVER: nginx          # or httpd
    BP_WEB_SERVER_ROOT: public    # directory that holds index.html
```

### Single-page apps (React, Vite)

A Vite project with no `start` script in `package.json` is served as a static site after `npm run build`. The Node buildpack would win detection (there is a `package.json`) and produce an image with no process to start. Use `build.buildpacks` to compose explicitly:

```yaml
build:
  buildpacks: [web-servers]       # Paketo web-servers: builds with Node, serves with nginx
  env:
    BP_NODE_RUN_SCRIPTS: build
    BP_WEB_SERVER: nginx
    BP_WEB_SERVER_ROOT: dist
    BP_WEB_SERVER_ENABLE_PUSH_STATE: "true"   # HTML5 routing
```

### Buildpacks and stacks

`build.buildpacks` pins the buildpack group the project uses (names from `shpyrd sizes list`); `build.stack` chooses the base image. The full stack (`jammy-full`) carries more system libraries than the base (`jammy`, default) and is useful when a language extension needs a C library that is present on Ubuntu but not in Paketo's minimal base image:

```yaml
build:
  stack: full    # base (default) or full
```

### System packages (Aptfile)

An `Aptfile` in the repository root lists Ubuntu packages to install into the image, one per line:

```
# Aptfile
libvips42
```

The CLI translates it to the format the `heroku/deb-packages` buildpack reads and composes it in front of the language's buildpack. No `project.toml` or explicit `build.buildpacks` needed.

```shell
# shpyrd.yaml not required for an Aptfile — the CLI detects it
shpyrd deploy
```

{% callout title="Stack for deep dependencies" %}
Some packages (libvips, ImageMagick) pull in glib, libcurl and other libraries that exist on the build image but not on the minimal run image. Use `build.stack: full` when the app crashes at start with a missing shared library that is not in your Aptfile.
{% /callout %}

## Release phase

When the image has a `release` process type — the Procfile line `release: bundle exec rails db:prepare` — the platform runs it before every new release rolls out. The rollout waits; a failure leaves the previous release serving and marks the project Failed with the reason.

```
# Procfile
release: bundle exec rails db:prepare
web:     bundle exec puma -C config/puma.rb
```

```
==> Releasing
    Deploying: release phase: running /cnb/process/release
    Running: web 1/1
Released v2: Deploy abc123def456
```

`shpyrd logs --process release` shows the command's output. `shpyrd projects info` lists the process types, including `release`, and a pending or failed release phase message.

For Dockerfile images without a Procfile, declare the command in `shpyrd.yaml`:

```yaml
processes:
  release:
    command: ["python", "manage.py", "migrate"]
```

## Dockerfile builds

When the deployed directory contains a `Dockerfile`, `shpyrd deploy` builds it instead of using buildpacks:

```
==> Archiving working tree (c26f84642501)
==> Building with Dockerfile (Dockerfile)
==> Uploading source (1.7 KiB)
==> Building
===> fetch
===> build
#8 importing cache manifest from 10.96.0.50:5000/apps/hello-docker:cache
#9 CACHED
...
pushed 10.96.0.50:5000/apps/hello-docker@sha256:2a6749dc...
==> Releasing
Released v2: Deploy c26f84642501
```

The build runs as a rootless [BuildKit](https://github.com/moby/buildkit) Job in the project namespace: a first step fetches your archive (or clones the Git revision), the second builds the context and pushes the image. Multi-stage builds, build arguments, `.dockerignore` and a layer cache between builds all work as with `docker build`.

Pin or tune it in [`shpyrd.yaml`](/docs/shpyrd-yaml):

```yaml
build:
  strategy: dockerfile        # buildpacks | dockerfile; without it, auto-detected from the Dockerfile
  dockerfile: deploy/Dockerfile
  target: runtime             # multi-stage target
  env:
    NODE_ENV: production      # build arguments
processes:
  web: {}                     # runs the image CMD
  worker:
    command: ["node", "worker.js"]   # Dockerfile images have one entrypoint: other process types name their command
```

From Git, detection is not possible; say so explicitly: `shpyrd deploy --git https://github.com/o/r --dockerfile` (optionally `--dockerfile deploy/Dockerfile`). The dashboard's **Deploy** dialog has the same choice. Git sources with a Dockerfile are rebuilt when the revision or the build settings change, not on every new commit as buildpack builds are; pass a commit or redeploy to rebuild a branch.

Failures show BuildKit's error in the CLI, the Activity panel and `shpyrd projects info`; the previous release keeps serving. `examples/hello-docker` in the repository is a complete example.

## Processes and sizes

Declare process types in [`shpyrd.yaml`](/docs/shpyrd-yaml) next to your code; `shpyrd deploy` applies it:

```yaml
project: hello-world
processes:
  web:
    port: 8080
  worker:
    cpu: "500m"
    memory: 256Mi
build:
  env:
    BP_GO_TARGETS: ./cmd/web:./cmd/worker   # Go: one process per command
```

Scale at any time; counts survive deploys:

```shell
shpyrd scale web=3 worker=2
```

## Config vars

```shell
shpyrd secrets set DATABASE_URL=postgres://... LOG_LEVEL=debug
shpyrd secrets unset LOG_LEVEL
shpyrd secrets list            # names and when each was last set; values are never shown
```

Every change is a release (`Set DATABASE_URL config var`) and restarts the processes with the new environment. The dashboard's **Config** tab does the same, including pasting `.env` files.

Plain, non-secret variables can also live in `shpyrd.yaml` under `env:` and travel with the code — useful for things like `RACK_ENV`, `RAILS_ENV` or `NODE_ENV` that belong in the repository rather than in the cluster's secret store:

```yaml
env:
  RACK_ENV: production
  RAILS_LOG_TO_STDOUT: "1"
```

`env:` is authoritative when present: an empty map (`env: {}`) removes every previously declared plain variable. Secret values — API keys, database passwords — always go through `shpyrd secrets set`, never here.

### Global config vars

Settings every project should have (an `OPENAI_API_KEY`, a region) are set once by a platform admin and injected into every process of every project ([RFC-0016](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0016-global-config-vars.md)):

```shell
shpyrd globals set OPENAI_API_KEY=sk-... REGION=eu
shpyrd globals unset REGION
shpyrd globals list            # names and when each was set; values are never shown
```

Globals come first in the environment: a project's own config var of the same name wins, and variables from attached resources win over both. A change is a **Global config change** release in every project that receives them (the Cluster page's card asks first and says how many). `shpyrd secrets list` and the Config tab show them as *provided by cluster* and mark project vars that override one. A project opts out in `shpyrd.yaml`:

```yaml
globals: false                       # none of them
globals: { exclude: [OPENAI_API_KEY] } # all but these
```

## Health checks

shpyrd configures probes automatically ([RFC-0019](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0019-health-checks-and-rollouts.md)). No configuration is needed for the common case:

| Process type | Default probe |
|---|---|
| `web` (or any process with a port) | HTTP `GET /` on `PORT` |
| Any process with `port:` set | TCP on that port |
| Workers and other processes without a port | None — relies on restart-on-crash |

The deploy waits for each new instance to pass its readiness probe before the old one is removed, so traffic is always served. If a new instance never becomes healthy the rollout stalls, the Activity panel shows the reason (exit code, probe error) and a rollback button; the old instances keep serving.

Override the default or disable checking in `shpyrd.yaml`:

```yaml
processes:
  web:
    healthCheck:
      path: /healthz          # HTTP GET on PORT; replaces the default /
      interval: 5s
      gracePeriod: 30s        # startup time before failures count
      shutdownDelay: 5s       # drain time before SIGTERM
  worker:
    healthCheck:
      command: [python, -c, "import app; app.is_healthy()"]   # custom command
  api:
    healthCheck:
      tcp: true               # explicit TCP when port: is set
  batch:
    healthCheck:
      disabled: true          # no probe
```

`shpyrd projects info` shows the health config per process. `shpyrd.yaml` changes take effect on the next deploy.

## Shell and one-off commands

```shell
shpyrd shell                          # bash (or sh) in web.1
shpyrd shell --instance worker.2      # a specific instance
shpyrd shell -- cat /etc/os-release   # run one command and return its exit code
```

`shpyrd shell` attaches to a **running instance**: what you see is the live process's filesystem and environment. Buildpack images get the same environment as the process (through the CNB launcher), so `node`, `bundle` or `python` are on the `PATH`.

```shell
shpyrd run rails db:migrate                 # a new instance of the current release, removed when the command exits
shpyrd run --size shared-l python manage.py import big.csv
shpyrd run --detach ./nightly.sh            # start and return; follow with shpyrd logs -p run
```

`shpyrd run` starts a **temporary instance** (like `heroku run`) with the release's image and config vars, streams its output and exits with the command's exit code; piped input works (`cat dump.sql | shpyrd run psql`). Instances left by `--detach` or a killed terminal are cleaned up after they finish.

## Volumes

Processes that need a disk mount a project volume:

```shell
shpyrd volumes create data --size 5Gi        # a persistent disk of the project
```

```yaml
# shpyrd.yaml
processes:
  web:
    volumes:
      - name: data
        path: /data
```

Volumes are persistent: they outlive deploys, scaling and crashes and are deleted only by `shpyrd volumes delete` (or the project's destruction). A volume is **single-instance** by default (block storage attaches to one node): the process mounting it runs one instance and rolls out with a stop-then-start (a few seconds of downtime per deploy, no data risk), and `shpyrd scale web=3` is refused with that explanation. `--shared` volumes can be mounted by many instances and processes but need a provisioner that offers `ReadWriteMany`; they are unsafe for SQLite. See [Resources](/docs/resources) for the details.

## Logs

```shell
shpyrd logs --project shop -f
```

The **Logs** tab streams every instance live. The [Logs](/docs/logs) page covers the log agent, the on-node limits and drains to your log provider.

## Releases and rollback

```shell
shpyrd releases
```

```
RELEASE       CREATED              DIGEST        DESCRIPTION
v7 (current)  2026-09-21 22:44:30  2edf5661353b  Rollback to v5
v6            2026-09-21 22:44:18  2edf5661353b  Set GREETING config var
v5            2026-09-21 22:44:06  2edf5661353b  Set GREETING config var
v4            2026-09-21 21:53:52  2edf5661353b  Deploy 654f4925638e
```

```shell
shpyrd rollback        # to the release before the current one
shpyrd rollback 4      # to a specific release: its build and its config vars
```

A rollback is refused while another release is still rolling out (`--force` overrides). See [Concepts](/docs/concepts#releases) for what a release contains.

## Redeploy

```shell
shpyrd redeploy                # new instances of the current release
shpyrd redeploy --rebuild      # build the same source again
```

Redeploy tries the current release again without creating a release. With a healthy or unhealthy release it starts new instances of it (a rolling restart: the fix for an instance stuck on a dependency that came back). When the last build failed - a registry hiccup, a flaky download - it builds the same source again instead, and the release that results is a normal deploy. The project page has the same button; in the card of a failed release it reads **Retry build**.

## Open and inspect

```shell
shpyrd open                 # opens https://my-service.<domain>
shpyrd projects info my-service     # status, releases and every resource of the project (app, volumes...)
shpyrd projects list
kubectl -n app-my-service get all,ingress,volumes.shpyrd.io,image.kpack.io,jobs
```

## Destroy

```shell
shpyrd projects destroy my-service      # deletes namespace app-my-service and everything in it
```

The command warns about the data on the project's volumes before asking for confirmation. Images stay in the registry.
