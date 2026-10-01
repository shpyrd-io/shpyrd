# RFC-0065 Build composition: buildpacks, stacks and system packages per project

**Status:** implemented (v0.9.9), gaps — see Implementation status below

**Owner:** Patrick Negri

**Depends on:** RFC-0004 (implemented), RFC-0045 (implemented)

**Creation date:** 2026-09-27

**Last update:** 2026-09-27

## Summary

A project can compose its build the way Heroku's `.buildpacks` did: name the buildpacks in
order, pick the stack, and install system packages — without a Dockerfile. `shpyrd.yaml`:

```yaml
build:
  buildpacks: [deb-packages, ruby]   # in order, from the platform's catalog
  stack: full                        # base (default) or full
```

```toml
# project.toml — read by heroku/deb-packages
[com.heroku.buildpacks.deb-packages]
install = ["libglib2.0-0", "libvips42"]
```

The platform keeps a catalog of buildpacks (Paketo's languages and web servers, Heroku's
`.deb` packages buildpack) and two stacks (Ubuntu jammy base and full). When a project names
buildpacks or a stack, the controller composes a builder for it; otherwise the platform's
builder detects the language as today.

## Motivation

Every project builds with one platform builder: Paketo's groups on the jammy-base stack,
detection picks the language. That covers most apps and nothing else: an app needing a
system library (`libglib2.0-0` for image processing, `libvips`, a database client library)
had one answer — write a Dockerfile — and an app needing a buildpack outside the catalog had
none. Heroku users are used to `heroku-buildpack-apt` and to listing buildpacks; the Cloud
Native Buildpacks world has the same pieces under other names, and kpack, which runs our
builds, can compose builders per namespace.

### Goals

- `build.buildpacks`: an ordered list from the catalog; `build.stack`: base or full.
- System packages through Heroku's `.deb` packages buildpack and `project.toml`.
- No change for projects that name nothing.
- Clear errors: an unknown buildpack name lists the catalog.

### Non-Goals

- Arbitrary buildpack images named by tenants (a buildpack runs at build time with the
  registry credentials of the project; the operator curates the catalog).
- Heroku's `Aptfile` (the CNB buildpack reads `project.toml`; the file is one line longer).
- Runtime-metrics agents (Heroku's `metrics` buildpack): the platform scrapes application
  metrics itself (RFC-0027); a language-runtime exporter would be another catalog entry.

## Proposal

- **Catalog** (`deploy/components/kpack`): one `ClusterBuildpack` per entry — Paketo `java`,
  `nodejs`, `go`, `python`, `ruby`, `php` (new), `dotnet-core`, `web-servers`, `procfile`, and
  `heroku/deb-packages` (new, `docker.io/heroku/buildpack-deb-packages`, Ubuntu 22.04 targets
  on amd64; on arm64 it detects only on 24.04, so it is a no-op there). Two `ClusterStack`s:
  `jammy` (Paketo `build/run-jammy-base`, the default) and `jammy-full`. Short names map to
  catalog entries: `java`, `nodejs`/`node`, `go`, `python`, `ruby`, `php`, `dotnet`,
  `web-servers`/`nginx`/`httpd`, `procfile`, `deb-packages`/`apt`; a `ClusterBuildpack` name
  works too.
- **`App.spec.build.buildpacks []string`** and **`App.spec.build.stack string`**. When either is
  set, the controller renders a kpack `Builder` `<app>-builder` in the project namespace: the
  chosen stack, one group with the named buildpacks in order (all required), the project's
  build service account, tag `<registry>/apps/<app>/builder`. The kpack `Image` references it
  (`kind: Builder`) instead of the platform's `ClusterBuilder`. kpack rebuilds the builder
  when the store or stack changes and rebuilds the app when the builder changes, as with the
  platform's. Clearing both fields removes the Builder and the Image returns to the
  platform's.
- **`shpyrd.yaml`** `build.buildpacks` and `build.stack` travel with the deploy request
  (RFC-0052); the API validates names against the catalog and answers with the list.
- **System packages**: `project.toml` at the source root with
  `[com.heroku.buildpacks.deb-packages] install = [...]` and `deb-packages` first in the
  list; the buildpack downloads the `.deb`s and their dependencies into a layer. Documented
  with the example `example-ruby-apt`.

## Design Details

- One group only: composition means "these, in this order, all of them". Detection across
  groups is the platform builder's job; a project that composes has decided.
- A composed builder is an image of its own in the project's registry repository (a few
  hundred megabytes, built once and on updates); `shpyrd projects destroy` removes it with
  the namespace; registry garbage collection (RFC-0059) treats `apps/<app>/builder` as the
  project's.
- The full stack triples the run image; it is opt-in for that reason.
- `build.builder` (a ClusterBuilder name, RFC-0004) stays for operators who add builders of
  their own; `buildpacks`/`stack` win when set.

## Implementation status

Implemented in v0.9.9: catalog entries `paketo-php` and `heroku-deb-packages`, stack
`jammy-full`, the per-project `Builder`, API validation, `shpyrd.yaml` fields, the examples
`example-php` and `example-ruby-apt`. Known gaps: no `GET /api/buildpacks` for the dashboard
(the CLI error lists the catalog); the dashboard's build settings do not show the
composition yet; `heroku/deb-packages` is amd64 on jammy.

## Open questions

1. Should tenants of a hosted platform be able to add buildpack images of their own?
   Default: no; the operator curates the catalog (RFC-0033's plans may allow it per plan
   later).
2. Sugar for packages in `shpyrd.yaml` (`build.packages: [...]`) writing `project.toml`?
   Default: not now; `project.toml` is the CNB convention and one file.

## Implementation History

- 2026-09-27: RFC written and implemented (v0.9.9).
