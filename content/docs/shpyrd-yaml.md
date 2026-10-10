---
title: shpyrd.yaml
description: The project file - which project a repository is, its process types, sizes, build settings, custom domains and exposure.
---

`shpyrd.yaml` lives at the root of the directory you deploy from and plays the role of `fly.toml` or a `Procfile` plus `app.json`. Everything but `project` is optional. {% .lead %}

```yaml
# Which project this repository (or directory) deploys to.
project: hello-world

# Plain environment variables for every process, committed with the code.
# For secrets (API keys, passwords) use `shpyrd secrets set` instead.
env:
  RACK_ENV: production
  RAILS_LOG_TO_STDOUT: "1"

# Process types. Declared types are authoritative: a type removed here is
# removed from the cluster on the next deploy. Instance counts set with
# `shpyrd scale` survive unless pinned with `replicas`.
processes:
  web:
    port: 8080          # exposed through the URL; PORT is injected. Default 8080 for web.
    size: shared-m      # instance size from the cluster catalog (shpyrd sizes list). Default: shared-m for Node.js, Ruby, Python and Java; shared-s for the rest.
    healthCheck:
      path: /up         # HTTP GET on PORT; replaces the default /
    volumes:
      - name: data      # a volume of the project (shpyrd volumes create data --size 5Gi)
        path: /data
  worker:
    size: shared-s
    replicas: 2         # pin the instance count
    command: ["/cnb/process/worker"]   # override the entrypoint (required for non-web types of Dockerfile images)
    args: ["--queue", "default"]
  release:
    command: ["python", "manage.py", "migrate"]   # runs before every rollout; needed for Dockerfile images

# Build settings.
build:
  strategy: buildpacks  # or dockerfile; without it, dockerfile when the directory has a Dockerfile (or build.dockerfile/target is set)
  env:
    BP_GO_TARGETS: ./cmd/web:./cmd/worker   # buildpacks: any BP_* variable of the buildpack in use
    BP_NODE_VERSION: "22.*"                 # Dockerfile: build arguments
  buildpacks: [deb-packages, ruby]  # explicit buildpack group from the platform catalog
  stack: full           # base (default, Ubuntu jammy minimal) or full (more system libraries)
  builder: shpyrd       # kpack ClusterBuilder; the default is fine
  dockerfile: Dockerfile  # dockerfile strategy: path inside the deployed directory
  target: runtime         # dockerfile strategy: multi-stage target

# Custom domains you own, served in addition to the project's address
# (<workspace>-<project>.shpyrd.app on shpyrd cloud, <project>.<domain> on a
# cluster you run): point each at that hostname (CNAME) or at the front door
# (A record at a zone apex).
domains:
  - www.myprod.com

# Which other projects of the workspace (or platform callers) may reach this
# one from inside the cluster. Projects are isolated by default.
allow:
  - project: expenses
  - platform: mcp

# Self-hosted: which front door serves the project on a cluster you run with a
# cloud profile: external (public load balancer, default) or internal (private one).
exposure: external
```

## Fields

| Field | Meaning |
| --- | --- |
| `project` | The project's slug (`my-shop`, not "My Shop"); used when `--project` is not given. `shpyrd projects create "<name>" --save` writes this file. `app` is accepted as an alias. |
| `env` | Plain environment variables for every process, committed with the code (`RACK_ENV`, `NODE_ENV`, feature flags). The key is authoritative when present: `env: {}` removes all of them. Secrets go through `shpyrd secrets set`, never here. `PORT`, `REVISION` and the `SHPYRD_*` variables are set by the platform and cannot be declared. |
| `globals` | `false` leaves every [global config var](/docs/deploying#global-config-vars) out of the project; `{exclude: [NAME, ...]}` leaves out only those. Default: all globals. |
| `processes.<type>.port` | Port the process listens on. `web` defaults to 8080 and is published through the URL; other types get no port unless set. `PORT` is injected. |
| `processes.<type>.replicas` | Pin the number of instances. Without it, `shpyrd scale` values are kept across deploys (default 1). |
| `processes.<type>.size` | Instance size from the cluster catalog (`shpyrd sizes list`): `shared-*` sizes have a CPU ceiling and a guaranteed share of 1/8 of it (burstable); `dedicated-*` sizes get whole cores with requests equal to limits. Default: `shared-m` (0.5 CPU, 256 MiB) for Node.js, Ruby, Python and Java; `shared-s` (0.5 CPU, 64 MiB) for the rest. Changing it is a release (`shpyrd resize` does the same from the CLI). |
| `processes.<type>.cpu`, `memory` | Override the size's limits (e.g. `cpu: "1"`, `memory: 1Gi`). Prefer a size; use these for one-off needs. |
| `processes.<type>.command`, `args` | Override the command. By default `web` runs the image entrypoint and other types run `/cnb/process/<type>`, the process the buildpacks recorded under that name. Dockerfile images have a single entrypoint, so every type other than `web` must set `command`. |
| `processes.<type>.healthCheck` | Readiness and liveness check. Default: HTTP `GET /` on `PORT` for `web`, TCP for other processes with a port, none for workers. One of `path`, `tcp: true` or `command`; tune with `interval` (10s), `timeout` (5s), `gracePeriod` (30s, also the minimum) and `shutdownDelay` (5s); `disabled: true` turns it off. See [Deploying › Health checks](/docs/deploying#health-checks). |
| `processes.<type>.volumes` | Project volumes to mount: `name` (created with `shpyrd volumes create`) and `path`. A single-instance volume pins the process to one instance; see [Resources](/docs/resources#volumes). |
| `processes.release` | The command that runs before every rollout (`command: ["python", "manage.py", "migrate"]`). Needed for Dockerfile images; for buildpack images it replaces the `Procfile`'s `release:` line. It runs at the `web` size unless it sets its own. |
| `build.strategy` | `buildpacks` (default) or `dockerfile`. Without it, `shpyrd deploy` picks `dockerfile` when the deployed directory has a `Dockerfile`, or when `build.dockerfile` or `build.target` is set. |
| `build.env` | Environment for the build: buildpack configuration such as `BP_GO_TARGETS`, `BP_JVM_VERSION`, `BP_NODE_RUN_SCRIPTS`, or `ARG` values for a Dockerfile. Runtime config vars are set with `shpyrd secrets` or `env:`, not here; a buildpack build sees those too, below `build.env` ([At build time](/docs/deploying#at-build-time)). |
| `build.buildpacks` | Explicit buildpack group from the platform catalog, in order (`[deb-packages, ruby]`). When set, the project gets a builder of its own instead of the platform's detection. Names: `java`, `node`/`nodejs`, `go`, `python`, `ruby`, `php`, `dotnet`/`dotnet-core`, `web-servers`/`nginx`/`httpd`, `procfile`, `deb-packages`/`apt`; a platform buildpack's own name (`paketo-…`, `heroku-…`) works too. |
| `build.stack` | Base image for the build and run: `base` (default, Ubuntu jammy minimal) or `full` (jammy full, more system libraries). Use `full` when a native extension needs a library present on Ubuntu but absent from the minimal image. |
| `build.builder` | kpack `ClusterBuilder` to use (buildpacks). The default is fine. Ignored when `build.buildpacks`, `build.stack` or an `Aptfile` gives the project a builder of its own. |
| `build.dockerfile`, `build.target` | Dockerfile path relative to the deployed directory (default `Dockerfile`) and the multi-stage target to build. |
| `domains` | Custom domains served in addition to the project hostname, each with its own certificate once its DNS record points here. See [Domains and exposure](/docs/domains). |
| `allow` | Which callers may reach the project from inside the cluster: `- project: <slug>` (another project of the workspace) or `- platform: actions` / `- platform: mcp`. Projects are isolated by default. Same as `shpyrd allow`; applies without a release. |
| `exposure` | Self-hosted: `external` (default) or `internal`, which load balancer serves the project on a cluster you run with a cloud profile. Changing it is release-free. |

## Process types and buildpacks

The buildpacks decide which process types an image has:

- **Go**: one per build target; use `BP_GO_TARGETS=./cmd/a:./cmd/b` to build several. The first is the default (`web`).
- **Node.js**: `web` from `npm start`/`package.json`; other types via a `Procfile`.
- **Java, Python, Ruby, .NET**: the framework's entry point becomes `web`; add a `Procfile` for others.
- **Procfile**: `release: bundle exec rails db:prepare` runs before every rollout; `worker: bundle exec sidekiq` adds a `worker` type on any stack.

`processes` in `shpyrd.yaml` tells shpyrd which of those types to run and how many instances; the names must match what the image provides.

With a Dockerfile there is no such catalogue: `web` runs the image's `CMD`, each other type names its `command` (`worker: { command: ["node", "worker.js"] }`), and the release command is declared under `processes.release.command`.

## Platform variables

Every process receives these read-only variables from the platform:

| Variable | Value |
| --- | --- |
| `PORT` | Port the process should listen on: `web`, and any process with `port:` set. |
| `RUNNING_IN_SHPYRD` | `true`: the process runs on shpyrd. |
| `SHPYRD_PROJECT` | The project slug. |
| `SHPYRD_PROJECT_ID` | The project's id, which never changes. |
| `SHPYRD_PROJECT_NAME` | The project's name, as it is shown. |
| `SHPYRD_WORKSPACE` | The workspace slug. |
| `SHPYRD_PROCESS` | The process type the instance runs: `web`, `worker`, `release`. |
| `SHPYRD_RELEASE` / `SHPYRD_RELEASE_VERSION` | The number of the release the instance belongs to: `5`, and `v5`. |
| `SHPYRD_ISSUER` | JWT issuer URL; `<issuer>/.well-known/jwks.json` holds the signing keys for verifying visitor identity (see [App access](/docs/app-access)). |
| `REVISION` / `SHPYRD_REVISION` / `SHPYRD_PROJECT_REVISION` | The git commit the release was built from (short SHA), or the archive digest for a `shpyrd deploy` from a working tree. Follows the image on rollback. Empty for prebuilt images (`--image`). |
