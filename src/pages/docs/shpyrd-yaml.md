---
title: shpyrd.yaml
description: The project file - which project a repository is, its process types, sizes, build settings, custom domains and exposure.
---

`shpyrd.yaml` lives at the root of the directory you deploy from and plays the role of `fly.toml` or a `Procfile` plus `app.json`. Everything but `app` is optional. {% .lead %}

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
    size: shared-m      # instance size from the cluster catalog (shpyrd sizes list). Default: shared-s.
    healthCheck:
      path: /up         # HTTP GET on PORT; replaces the default /
    volumes:
      - name: data      # a volume of the project (shpyrd volumes create data --size 5Gi)
        path: /data
  worker:
    size: shared-xs
    replicas: 2         # pin the instance count
    command: ["/cnb/process/worker"]   # override the entrypoint (required for non-web types of Dockerfile images)
    args: ["--queue", "default"]
  release:
    command: ["python", "manage.py", "migrate"]   # runs before every rollout; Dockerfile images only

# Build settings.
build:
  strategy: buildpacks  # or dockerfile; without it, dockerfile when the directory has a Dockerfile
  env:
    BP_GO_TARGETS: ./cmd/web:./cmd/worker   # buildpacks: any BP_* variable of the buildpack in use
    BP_NODE_VERSION: "22.*"                 # Dockerfile: build arguments
  buildpacks: [deb-packages, ruby]  # explicit buildpack group from the platform catalog
  stack: full           # base (default, Ubuntu jammy minimal) or full (more system libraries)
  builder: shpyrd       # kpack ClusterBuilder; the default is fine
  dockerfile: Dockerfile  # dockerfile strategy: path inside the deployed directory
  target: runtime         # dockerfile strategy: multi-stage target

# Custom domains you own, served in addition to <project>.<domain>: point
# each at that hostname (CNAME) or at the front door (A record at a zone apex).
domains:
  - www.myprod.com

# Which front door serves the project on cloud profiles: external (public
# load balancer, default) or internal (private one).
exposure: external
```

## Fields

| Field | Meaning |
| --- | --- |
| `project` | The project's slug (`my-shop`, not "My Shop"); used when `--project` is not given. `shpyrd projects create "<name>" --save` writes this file. `app` is accepted as an alias. |
| `env` | Plain environment variables for every process, committed with the code (`RACK_ENV`, `NODE_ENV`, feature flags). The key is authoritative when present: `env: {}` removes all of them. Secrets go through `shpyrd secrets set`, never here. `PORT`, `REVISION`, `SHPYRD_PROJECT`, `SHPYRD_WORKSPACE` and `SHPYRD_ISSUER` are set by the platform and cannot be declared. |
| `globals` | `false` leaves every [global config var](/docs/deploying#global-config-vars) out of the project; `{exclude: [NAME, ...]}` leaves out only those. Default: all globals. |
| `processes.<type>.port` | Port the process listens on. `web` defaults to 8080 and is published through the URL; other types get no port unless set. `PORT` is injected. |
| `processes.<type>.replicas` | Pin the number of instances. Without it, `shpyrd scale` values are kept across deploys (default 1). |
| `processes.<type>.size` | Instance size from the cluster catalog (`shpyrd sizes list`): `shared-*` sizes have a CPU ceiling and a guaranteed share of 1/8 of it (burstable); `dedicated-*` sizes get whole cores with requests equal to limits. Default: the catalog default (`shared-s`, up to 0.5 CPU / 64 MiB). Changing it is a release. |
| `processes.<type>.cpu`, `memory` | Override the size's limits (e.g. `cpu: "1"`, `memory: 1Gi`). Prefer a size; use these for one-off needs. |
| `processes.<type>.command`, `args` | Override the command. By default `web` runs the image entrypoint and other types run `/cnb/process/<type>`, the process the buildpacks recorded under that name. Dockerfile images have a single entrypoint, so every type other than `web` must set `command`. |
| `processes.<type>.healthCheck` | Readiness probe. Default: HTTP `GET /` on `PORT` for `web`, TCP for processes with a port, none for workers. See [Deploying › Health checks](/docs/deploying#health-checks). |
| `processes.<type>.volumes` | Project volumes to mount: `name` (created with `shpyrd volumes create`) and `path`. A single-instance volume pins the process to one instance; see [Resources](/docs/resources#volumes). |
| `processes.release` | For Dockerfile images: the command that runs before every rollout (`command: ["python", "manage.py", "migrate"]`). Buildpack images use a `Procfile` `release:` line instead. |
| `build.strategy` | `buildpacks` (default) or `dockerfile`. Without it, `shpyrd deploy` picks `dockerfile` when the deployed directory has a `Dockerfile`. |
| `build.env` | Environment for the build: buildpack configuration such as `BP_GO_TARGETS`, `BP_JVM_VERSION`, `BP_NODE_RUN_SCRIPTS`, or `ARG` values for a Dockerfile. Runtime config vars are set with `shpyrd secrets` or `env:`, not here. |
| `build.buildpacks` | Explicit buildpack group from the platform catalog, in order (`[deb-packages, ruby]`). When set, the project gets a builder of its own instead of the platform's detection. Names: `java`, `node`/`nodejs`, `go`, `python`, `ruby`, `php`, `dotnet`, `web-servers`/`nginx`/`httpd`, `procfile`, `deb-packages`/`apt`. |
| `build.stack` | Base image for the build and run: `base` (default, Ubuntu jammy minimal) or `full` (jammy full, more system libraries). Use `full` when a native extension needs a library present on Ubuntu but absent from the minimal image. |
| `build.builder` | kpack `ClusterBuilder` to use (buildpacks). The default is fine. |
| `build.dockerfile`, `build.target` | Dockerfile path relative to the deployed directory (default `Dockerfile`) and the multi-stage target to build. |
| `domains` | Custom domains served in addition to the project hostname, each with its own certificate once its DNS record points here. See [Domains and exposure](/docs/domains). |
| `exposure` | `external` (default) or `internal`: which load balancer serves the project on cloud profiles. Changing it is release-free. |

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
| `PORT` | Port the process should listen on (web processes only). |
| `SHPYRD_PROJECT` | The project slug. |
| `SHPYRD_WORKSPACE` | The workspace slug. |
| `SHPYRD_ISSUER` | JWT issuer URL; `<issuer>/.well-known/jwks.json` holds the signing keys for verifying visitor identity (see [App access](/docs/app-access)). |
| `REVISION` / `SHPYRD_REVISION` | The git commit the release was built from (short SHA), or the archive digest for a `shpyrd deploy` from a working tree. Follows the image on rollback. Empty for prebuilt images (`--image`). |
