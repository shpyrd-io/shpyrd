---
title: Concepts
description: Projects, processes, builds, releases and config vars - the vocabulary shpyrd shares with Heroku and Fly.
---

Shpyrd borrows the vocabulary of Heroku and Fly and maps it onto Kubernetes objects you can always inspect with `kubectl`. {% .lead %}

## Project and resources

A project is what you deploy to: a name, a domain, config vars and a set of **resources**. It has a display name ("My Shop") and a **slug** derived from it (`my-shop`) that identifies it everywhere precise: `--project`, `shpyrd.yaml`, the dashboard URL `/projects/my-shop` and the hostname `my-shop.<domain>`. The name can change; the slug cannot. Resource types today are the **app** (your code with its process types) and **volumes** (persistent disks); databases and caches follow (see [Resources](/docs/resources) and the [roadmap](/docs/roadmap)). A project without a web process is a worker or an agent; no separate type is needed. Under the hood the project is a namespace, `app-<slug>` (label `shpyrd.io/project`), holding an `App` custom resource (`shpyrd.io/v1alpha1`), the `Volume` resources and everything the controller creates for them. `kubectl get apps,volumes.shpyrd.io -A` lists them all; `shpyrd projects info` and the dashboard's Resources card show the same list with status and what uses each resource.

```yaml
apiVersion: shpyrd.io/v1alpha1
kind: App
metadata:
  name: hello-world
  namespace: app-hello-world
spec:
  source:
    git: { url: https://github.com/shpyrd-io/shpyrd, revision: main }
    subPath: examples/hello
  processes:
    web: { port: 8080, replicas: 3 }
    worker: { replicas: 2 }
  build:
    env: [{ name: BP_GO_TARGETS, value: ./cmd/web:./cmd/worker }]
status:
  phase: Running
  url: https://hello-world.127.0.0.1.nip.io
  releases: [...]
```

Projects without a `web` process (workers, agents, schedulers) work the same way; they just get no URL.

## Processes

A **process type** is a way of running the build: `web` serves HTTP and receives `PORT`; anything else (`worker`, `scheduler`, `agent`) runs the command of the same name that the buildpacks recorded in the image. Each process type becomes a Deployment with its own **instance** count (`shpyrd scale web=3 worker=1`) and **instance size** (`shpyrd resize web=shared-m`): a named cpu/memory allocation from the cluster catalog.

Two size kinds exist:

- **shared**: the `cpu` value is a ceiling the process may use; it is guaranteed a 1/8 share of it and borrows the rest from idle neighbours (Kubernetes burstable QoS). Many small shared instances fit on one node.
- **dedicated**: requests equal limits — whole cores, Guaranteed QoS.

Memory is never overcommitted: requests equal limits for both kinds. The default size is `shared-s` (up to 0.5 CPU, 64 MiB). `shpyrd sizes list` shows the full catalog.

Instances are named the way Heroku names dynos: `web.1`, `web.2`, `worker.1`, in creation order. Logs and the dashboard use these names.

## Builds

A **build** compiles source into an OCI image inside the cluster, with one of two strategies: [Cloud Native Buildpacks](https://buildpacks.io) (Paketo, run by [kpack](https://github.com/buildpacks-community/kpack)), or the repository's `Dockerfile` built by a rootless [BuildKit](https://github.com/moby/buildkit) Job. A build happens for every new source: an uploaded archive, or a new commit on a Git branch (kpack polls; Dockerfile builds from Git rebuild when the revision changes). Builds are numbered (`#1`, `#2`, ...) and identified by the image digest; the dashboard shows every step live (prepare, analyze, detect, restore, build, export for buildpacks; fetch and build for Dockerfiles).

Buildpacks detect the language from the repository (`go.mod`, `package.json`, `pom.xml`, `requirements.txt`, `Gemfile`, `*.csproj`, or static files) and produce one process type per entry point. A `Procfile` or buildpack-specific settings (`BP_*` variables in `build.env`) refine that, for example `BP_GO_TARGETS` to build several Go commands.

## Releases

A **release** is a build plus the config vars in effect, numbered `v1`, `v2`, ... Anything that changes what runs creates one:

| Kind | Example description | Creates a build? |
| --- | --- | --- |
| deploy | `Deploy 654f4925638e` | yes |
| config | `Set GREETING config var`, `Resize web to shared-m` | no, reuses the current build |
| rollback | `Rollback to v7` | no, reuses v7's build |

Each release records its build (identified by `REVISION`, the git commit or archive digest), its config snapshot (a Secret `<app>-release-vN`), its process types and their sizes, and the resources attached to the app. **Rollback** re-releases an earlier release exactly: its build is pinned and its config vars, sizes and attachments are restored. `REVISION` follows the image that runs, so a rollback reports the older commit, not the newest one. Rolling back to a release whose build predates a process type (say, before `worker` existed) cannot start that process; the dashboard warns before and the failure is reported plainly after.

The next `shpyrd deploy` unpins the build and continues from the new source.

### Release phase

When an image has a `release` process type (a `Procfile` line `release: bundle exec rails db:prepare`, or `processes.release.command` in `shpyrd.yaml` for Dockerfile images), the platform runs it as a one-off Job before the new release rolls out. The rollout waits; a failure leaves the previous release serving and marks the project Failed. Every kind of release — a new build, a config change, a rollback — triggers the phase. See [Deploying › Release phase](/docs/deploying#release-phase).

## Config vars

Config vars are environment variables for every process. Three sources, in increasing priority:

1. **Global vars** — set once by a platform admin with `shpyrd globals set`; every project receives them.
2. **`env:` in `shpyrd.yaml`** — plain, non-secret vars committed with the code (`RACK_ENV`, `NODE_ENV`). Travels with the deploy; wins over globals.
3. **Secrets** — written with `shpyrd secrets set`, stored in Secret `<app>-env`. Write-only: names and timestamps are shown, values never. Wins over `env:`.
4. **Bound vars** — injected by attached resources (`DATABASE_URL`, `REDIS_URL`); win over secrets of the same name.

Changing any of these creates a release and rolls the processes. The platform also injects read-only variables (`PORT`, `REVISION`, `SHPYRD_PROJECT`, `SHPYRD_WORKSPACE`, `SHPYRD_ISSUER`) that cannot be overridden from `shpyrd.yaml`.

## Domains and TLS

A project's `web` process is published at `https://<name>.<cluster domain>` (additional `domains` can be declared). Certificates come from cert-manager: the development CA on the local profile, a public or private CA on cloud profiles.

## Extensions

Optional capabilities are **extensions**: compiled into shpyrd, switched on per cluster with `shpyrd extensions enable`, each bringing its installer component, resource types and commands. `auth-local` (accounts for the dashboard) is the first; databases and caches follow. See [Extensions and sign-in](/docs/extensions).

## Environment profile

The **profile** chosen at install time (`local` today) says how load balancing, DNS, TLS and the registry are provided; see [Installation](/docs/installation#environment-profiles).

## Phases

A project's phase summarises its state:

| Phase | Meaning |
| --- | --- |
| Pending | no source or image yet |
| Building | a build is running (a previous release may still be serving) |
| Deploying | new instances are rolling out; previous ones keep serving until they are ready |
| Running | every process has its desired instances ready |
| Failed | the build failed, or new instances cannot start (the reason is shown, previous instances keep serving) |
