# RFC-0079 Node.js builds: pnpm, and Next.js 16 on buildpacks

**Status:** provisional

**Owner:** Patrick Negri

**Depends on:** RFC-0065 (implemented: build composition, the builder), RFC-0067
(implemented: build profiles, the `next` profile)

**Amends:** RFC-0065 (the Node.js group of the builder), RFC-0067 (what the `next`
profile knows)

**Creation date:** 2026-09-29

**Last update:** 2026-09-29 (Part B: Paketo-style pnpm buildpacks, built here and
proposed upstream; `heroku/nodejs` kept as the exit if upstream stalls)

---

## Summary

Two things a Node.js developer takes for granted do not work on the builder today, and
both surfaced on the first real project deployed to production
(`rubix-documents`, a Next.js 16 documentation site with a `pnpm-lock.yaml`):

1. **pnpm is not supported.** The builder's Node.js group is Paketo's composite
   `paketobuildpacks/nodejs`, whose order has three groups: yarn, npm, bare node. A
   project with `pnpm-lock.yaml` falls into the npm group; the lockfile is ignored and
   `npm install` resolves fresh versions. Paketo has no pnpm buildpack at all — and has
   had an open request for one since June 2022.
2. **Next.js 16 does not build.** Next 16 bundles `next build` with Turbopack by
   default; Turbopack refuses a `node_modules` that is a symlink pointing outside the
   project root, and that is exactly how Paketo's `npm-install` and `yarn-install` lay
   out dependencies (a layer under `/layers`, symlinked into `/workspace`). The build
   dies with `Symlink [project]/node_modules is invalid, it points out of the filesystem
   root` before compiling anything. Our `nextjs` example is on Next 15, where `next
   build` still used webpack, so the suite never saw it.

This RFC records what was found upstream and proposes two parts:

- **Part A** — the CLI's `next` build profile learns Next 16 and says the right thing
  before upload and after a failed build. Small; ships first.
- **Part B** — shpyrd writes the missing pnpm buildpacks **in Paketo's style** (`pnpm`,
  `pnpm-install`, `pnpm-start`, mirroring `yarn`, `yarn-install`, `yarn-start`), runs
  them in its own builder from day one, and proposes them upstream with the production
  evidence and the test matrix. `pnpm-install` keeps `node_modules` as a real directory
  in the application, so pnpm projects build with Turbopack unchanged; the same layout
  is proposed to `npm-install` and `yarn-install` as an opt-in. If Paketo has not
  adopted the buildpacks within two of its release cycles, the exit is `heroku/nodejs`
  as the builder's Node group, not owning the buildpacks forever.

## Motivation

The docs001 deploy on 2026-09-29, on production, in order:

```
==> Uploading source (1.3 MiB)
    Running 'npm run build'                      ← pnpm-lock.yaml present, npm chosen
      Creating an optimized production build ...
      FATAL: An unexpected Turbopack error occurred.
      Error [TurbopackInternalError]: Symlink [project]/node_modules is invalid,
        it points out of the filesystem root
===> step build failed (exit 51)
```

Changing the build script to `next build --webpack` and deploying again: compiled in
32 s on one OCPU, 25 pages, `Build successful`. The application itself was never the
problem. Next.js is the most deployed framework in the segment this platform is for, pnpm
is its most recommended package manager, and Next 16 has been the current major since
October 2025. A platform that fails both by default fails its main audience on the first
`shpyrd deploy`.

## Research

Facts, with where they come from. Checked 2026-09-29.

### Paketo's Node.js family and its composite

Paketo's Node.js support is a family of small buildpacks that the composite
`paketo-buildpacks/nodejs` orders: `node-engine` installs the runtime; `npm-install`
and `yarn-install` (with `yarn`, which delivers the yarn CLI) install dependencies;
`node-run-script` runs `build`; `node-start`, `npm-start`, `yarn-start` set the start
command; `procfile`, `environment-variables`, `image-labels`, `ca-certificates`,
`watchexec`, `tini`, `cpython` are optional. The composite's `buildpack.toml` (main) has
three `[[order]]` groups: `node-engine` + `yarn` + `yarn-install`; `node-engine` +
`npm-install`; `node-engine` + `node-start`. There is no `pnpm-install`, and the
`paketo-buildpacks` organisation has no repository matching `pnpm`.

### Paketo installs dependencies into a layer and symlinks them

`npm-install` (and `yarn-install`) create `node_modules` in a layer
(`/layers/paketo-buildpacks_npm-install/build-modules/node_modules`, and a separate
`launch-modules` layer without dev dependencies for runtime) and symlink
`/workspace/node_modules` to it. The buildpack's documented configuration
(`BP_NPM_VERSION`, `BP_KEEP_NODE_BUILD_CACHE`, `BP_NPM_INCLUDE_BUILD_PYTHON`,
`BP_NODE_PROJECT_PATH`) has no switch for this; the layout is the caching strategy and
the build/launch split.

### Turbopack's root boundary is deliberate and stays

Next.js documentation, `turbopack` options, version 16.3.6:

> Turbopack uses the root directory to resolve modules. Files outside of the project root
> are not resolved. The reason files are not resolved outside of the project root is to
> improve cache validation, reduce filesystem watching overhead, and reduce the number of
> resolving steps needed.

The root is detected from the lockfile (`pnpm-lock.yaml`, `package-lock.json`,
`yarn.lock`, `bun.lock`) — in a buildpack build, `/workspace`. `turbopack.root` overrides
it with an absolute path; the documented use is linked dependencies, "the parent directory
of both the project and the linked dependencies".

Upstream history of the exact error: vercel/next.js#88335 (Jan 2026, symlinked
`node_modules`, closed once the reporter changed their own build), #91896 (Mar 2026,
Bazel, closed as duplicate), PR #92886 "Support cross-root symlinks in Turbopack's
filesystem layer" (Apr 2026) **closed unmerged** in June 2026. All three are locked. A
GitHub search for the message in issues and pull requests returns 344 results from the
last months — git worktrees with shared `node_modules`, shadow builds, CI harnesses — all
converging on the same three workarounds:

1. `next build --webpack` (webpack follows symlinks; Next 16 keeps the flag);
2. `turbopack.root` (or `outputFileTracingRoot`) set to a common ancestor of the project
   and the symlink target — in a buildpack that ancestor is `/`;
3. a real `node_modules` directory instead of a symlink.

Nothing the platform controls from outside the project can apply 1 or 2: the build runs
the project's own `build` script and reads the project's own `next.config`. 3 is a
property of the buildpack. No issue in the `paketo-buildpacks` organisation mentions
Turbopack; the Next 16 half is unreported upstream.

### Upstream appetite for pnpm, and the bar

- paketo-buildpacks/nodejs#594 "[feature request] add pnpm supported" — **open since
  2022-06-21**.
- paketo-buildpacks/rfcs#334 (2026-08-04) and #336 (2026-09-03), both by the same
  author, both proposing `pnpm` + `pnpm-install` and a new order group, both **closed
  without review**. The Node.js maintainer's stated reason: the RFC appeared 17 minutes
  after the author's first look at the repository, and the second submission "seems like
  100% AI generated"; he would not proceed with a review. pnpm was not declined; the
  submissions were. The bar is demonstrable work: an implementation, tests, evidence of
  use.
- Contribution mechanics: Linux Foundation EasyCLA on every commit; an RFC in
  `paketo-buildpacks/rfcs` reviewed by the Node.js subteam; new repositories are created
  by maintainers, or incubated in `paketo-community` (their RFC 0008) and promoted later
  (as Rust was, RFC 0014); dependencies such as the pnpm CLI are declared in
  `buildpack.toml` with checksums and kept current by Paketo's dependency pipeline (see
  how `yarn` declares yarn 1.22.19); a change to the composite's order is a separate pull
  request; releases follow their cadence. Months of calendar time when everything goes
  well.
- Size of the analogues we would model on: `yarn-install` is 18 source files (~30 KB of
  Go on the `packit` library) with ~129 KB of tests; `yarn` (the CLI deliverer) 9 source
  files; `npm-install` 28 source files. A `pnpm-install` of the same shape is weeks of
  careful work, most of it in integration tests written the Paketo way.

### Heroku's Cloud Native Buildpack for Node.js (the exit)

`heroku/nodejs` (heroku/buildpacks-nodejs, 5.7.20 at the time of writing) is a single
buildpack, Buildpack API 0.10, published multi-arch at `docker.io/heroku/buildpack-nodejs`
with `[[targets]]` `linux/amd64` and `linux/arm64` and no distro restriction, so it can
run on our jammy stack. npm, yarn and pnpm from lockfiles; `build` and
`heroku-{pre,post}build` run automatically; versions from `engines.*` or
`packageManager`. npm and yarn install into the application directory (a real
`node_modules`; Turbopack works). pnpm (`src/package_managers/pnpm.rs`) puts the store in
a cached layer and the **virtual store in a launch layer under `/layers`**, with a
symlink from the layer's `node_modules` back to the application's: the application's
`node_modules/<package>` entries point into `/layers`, the shape Turbopack rejects. pnpm
+ Next 16 is expected to fail there too (to verify).

### What our own suite covers

`shpyrd-examples/nextjs` pins `next ^15.5.0` with `build: next build`: webpack. The
example that should have caught this is one major behind the framework's default.

## Design

### Part A — the CLI says the right thing (RFC-0067 amendment)

The `next` build profile (`internal/cli/profiles.go`) already recognises a project with
`next` in `dependencies`. It learns:

- **Before upload**, when the resolved Next major is ≥ 16, the `build` script runs
  `next build` without `--webpack`, `next.config.*` sets no `turbopack.root`, and the
  project is not a pnpm project (Part B makes pnpm projects work unchanged): print one
  warning naming the cause and the two fixes, then continue.

  ```
  Next.js 16 builds with Turbopack, which does not follow the node_modules symlink
  buildpacks create. If the build fails with "points out of the filesystem root", use
  `next build --webpack` in your build script, or set turbopack.root to "/" in next.config.
  ```

- **After a failed build**, when the followed log contains `points out of the filesystem
  root`, the deploy's error message ends with the same two fixes. The log follower in
  `shpyrd deploy` already reads every line; this is a pattern and a sentence.

Documentation: the Node.js page gets a "Next.js 16" section with the same two options and
why. The `nextjs` example moves to Next 16 with `next build --webpack` until Part B's
layout reaches npm and yarn projects, so the suite exercises the framework's default.

This part is independent of the builder and costs an afternoon.

### Part B — pnpm buildpacks in Paketo's style, here first, then upstream

Three buildpacks, one repository each under `shpyrd-io`, written on `packit` like the
family they join, published multi-arch (`linux/amd64`, `linux/arm64`) to
`ghcr.io/shpyrd-io/buildpacks/<name>`, Apache-2.0, in the Paketo naming and file layout
so that promotion into `paketo-buildpacks` is a transfer, not a rewrite:

| Buildpack | Mirrors | Does |
| --- | --- | --- |
| `pnpm` | `yarn` | Delivers the pnpm CLI: version from `packageManager` (exact, corepack-compatible), else `engines.pnpm`, else `BP_PNPM_VERSION`, else the latest pinned in `buildpack.toml`. Downloaded from pnpm's GitHub releases with a checksum declared in `buildpack.toml`, the way `yarn` declares yarn; the pipeline entry is Paketo's to wire once upstream. Provides `pnpm`. |
| `pnpm-install` | `yarn-install` | Detects `pnpm-lock.yaml`; requires `node` and `pnpm`; `pnpm install --frozen-lockfile` with the content-addressable **store in a cached build layer**; `node_modules` **in the application directory** as a real directory (see below); after the build phase's scripts have run, prunes dev dependencies for launch (`pnpm prune --prod`) unless `BP_KEEP_DEV_DEPENDENCIES=true`. Provides `node_modules`. `BP_NODE_PROJECT_PATH` honoured like its siblings. |
| `pnpm-start` | `yarn-start` | Sets the `web` process to `pnpm start` when `scripts.start` exists and no Procfile names one. |

The builder's order gains a group ahead of the Paketo Node composite, composed from
individual Paketo buildpacks (all published multi-arch) plus the three above:

```yaml
- group:
    - {name: paketo-ca-certificates, kind: ClusterBuildpack, optional: true}
    - {name: paketo-node-engine,     kind: ClusterBuildpack}
    - {name: shpyrd-pnpm,            kind: ClusterBuildpack}
    - {name: shpyrd-pnpm-install,    kind: ClusterBuildpack}
    - {name: paketo-node-run-script, kind: ClusterBuildpack, optional: true}
    - {name: shpyrd-pnpm-start,      kind: ClusterBuildpack, optional: true}
    - {name: paketo-procfile,        kind: ClusterBuildpack, optional: true}
    - {name: paketo-environment-variables, kind: ClusterBuildpack, optional: true}
    - {name: paketo-image-labels,    kind: ClusterBuildpack, optional: true}
- group:
    - {name: paketo-nodejs, kind: ClusterBuildpack}   # npm, yarn, bare node: unchanged
```

Detection: `pnpm-install` detects only on `pnpm-lock.yaml`, so npm and yarn projects fall
through to the composite exactly as today. `BP_NODE_RUN_SCRIPTS` keeps its meaning (the
same `node-run-script` runs the scripts).

**Why `node_modules` in the application directory.** Three reasons, in the order they
matter upstream:

1. pnpm's own layout already is a directory of symlinks — `node_modules/<package>` →
   `node_modules/.pnpm/<package>@<version>/node_modules/<package>` — and pnpm's cache is
   the content-addressable store, not `node_modules`. Putting the store in a cached layer
   and `node_modules` in the application is pnpm's design; a second symlink hop into
   `/layers` would be ours.
2. Turbopack follows symlinks that stay under the project root and refuses those that
   leave it. Layout 1 stays under the root: Next 16 builds with pnpm unchanged, no flag,
   no `turbopack.root`.
3. Heroku's buildpack made the same choice for npm and yarn; the precedent exists in the
   CNB ecosystem.

Hard links from the store to `node_modules` do not survive export across layers (each
layer is its own tar); `pnpm-install` therefore installs with `package-import-method=copy`
into the application when the store is a layer, or lets pnpm hard-link when both sit on
the same filesystem during the build and accepts regular files in the image. Measured in
the matrix: image size and second-build time against today's npm path.

**The same layout proposed to `npm-install` and `yarn-install`.** Not a fork: an issue
with the production evidence (Appendix B), then, if welcomed, an RFC for an opt-in
(`BP_NODE_MODULES_IN_APP=true`, name to be agreed) that installs into the application and
prunes dev dependencies after the build instead of splitting build and launch layers.
Until that exists, Part A's guidance is what npm and yarn projects on Next 16 get.

**Ownership and the exit.** shpyrd runs the three buildpacks in its builder from the
first green matrix. In parallel: the CLA, a comment on nodejs#594 with the evidence
(Appendix A), an RFC in `paketo-buildpacks/rfcs` pointing at working code and a passing
matrix, and the willingness to transfer the repositories or incubate them in
`paketo-community`. Decision rule, so this does not become a permanent fork: **if two
Paketo release cycles after the RFC opens (about six months) the buildpacks are neither
adopted nor on a concrete path, the builder's Node group becomes `heroku/nodejs`** (the
table of differences is in the Research section) and the three repositories are archived
with a pointer. Owning a package-manager integration indefinitely is the outcome this
rule prevents.

Not chosen:

- **`heroku/nodejs` now.** It gives pnpm, and Turbopack for npm/yarn, today — at the
  price of changing build conventions for every Node project (`BP_NODE_RUN_SCRIPTS`,
  `.nvmrc`, `BP_NODE_VERSION`, `build.stack`), of a second buildpack ecosystem to
  document and support, and of pnpm projects most likely still failing on Turbopack
  through its virtual-store layer. It stays the exit, not the first move.
- **A shpyrd "materialise node_modules" buildpack** bracketing `node-run-script` for
  npm/yarn. Two custom buildpacks, a copy of `node_modules` per build, solves only the
  Turbopack half. The opt-in proposed to `npm-install`/`yarn-install` is the same idea in
  the right place.
- **Asking Turbopack.** The boundary is documented as a design choice and the pull
  request implementing cross-root symlinks was closed.
- **Documentation alone.** pnpm is table stakes; "commit a package-lock.json" is not an
  answer a Node developer accepts in 2026.

### Test matrix (before Part B's group enters the builder)

On the development cluster:

| Example | Package manager | Expectation |
| --- | --- | --- |
| `node` | npm | unchanged: falls through to the Paketo composite |
| `nextjs` (bumped to 16, `next build --webpack`) | npm | builds on the composite with the flag; Part A's warning shown |
| `nextjs-pnpm` (new, same app, plain `next build`) | pnpm | `pnpm` + `pnpm-install` detect; installs from the lockfile; **Turbopack builds without a flag**; `pnpm start` is the web process |
| `rubix-documents` (the production case) | pnpm | installs from the lockfile; builds with plain `next build`; runs in `shared-m` |
| `react` | npm | unchanged |
| `sinatra-react` | ruby + node | the Ruby group still wins detection; unaffected |
| a pnpm project with `packageManager: pnpm@x.y.z` | pnpm | that exact version is used |
| a pnpm project with a `Procfile` | pnpm | Procfile wins over `pnpm-start` |
| second build of `nextjs-pnpm` | pnpm | store restored from the cache layer; install time and image size recorded next to today's npm path |

Also: `pack build` locally with the three buildpacks against `paketobuildpacks/builder-jammy-base`,
which is the form Paketo's reviewers will run.

## Open questions

1. **`node_modules` in the application directory** (Part B). Default: **yes** — pnpm's
   own layout, Turbopack, Heroku precedent; the tradeoff (no separate launch-modules
   layer) is paid by `pnpm prune --prod` after the build.
2. **Dev dependencies at launch.** Default: **pruned**, `BP_KEEP_DEV_DEPENDENCIES=true`
   keeps them (name aligned with `BP_KEEP_NODE_BUILD_CACHE`'s style).
3. **pnpm version source order.** Default: **`packageManager` > `engines.pnpm` >
   `BP_PNPM_VERSION` > latest pinned** — `packageManager` first because it is exact and
   what corepack users already have.
4. **Delivering the CLI: download vs corepack.** corepack ships with Node and could
   activate pnpm without a download, but its network access at build time and its
   changing status in Node (no longer enabled by default) make it a moving target.
   Default: **download from GitHub releases with a checksum in `buildpack.toml`**, as
   `yarn` does; the pipeline entry is added upstream.
5. **Which fix to recommend first for npm/yarn + Next 16** (Part A): `next build
   --webpack` or `turbopack.root: "/"`. Default: **`--webpack`** — documented flag, no
   widening of the bundler's root to the whole filesystem; `turbopack.root` second.
6. **Where the repositories start.** `shpyrd-io/paketo-pnpm`, `-pnpm-install`,
   `-pnpm-start`, or a request to incubate in `paketo-community` from day one. Default:
   **`shpyrd-io` first** — we need them in the builder before any upstream decision;
   transfer or incubation is offered in the RFC.
7. **The exit rule's clock.** Two Paketo release cycles after the upstream RFC opens.
   Default: **six months**, reviewed in the backlog at every release.
8. **Runtime memory.** The same deploy was OOM-killed at start on the default instance
   size (`shared-s`, 64 MiB) until resized to `shared-m`. Out of scope here: a separate
   change makes the default size follow the detected runtime (the catalog already names
   `shared-m` for Node.js). Recorded so it is not lost.

## Implementation plan

0. **Upstream threads** — the comment on nodejs#594 (Appendix A) and the issue on
   `npm-install` (Appendix B), posted by the owner after review, plus the CLA.
1. **Part A** — `next` profile warning and failure hint, docs section, `nextjs` example
   to Next 16 with `--webpack`. One release.
2. **`pnpm`, `pnpm-install`, `pnpm-start`** — repositories, unit and integration tests
   in Paketo's style, multi-arch images, `pack build` against `builder-jammy-base`.
3. **Builder group + matrix** on dev; then the group ships in the next release with the
   `nextjs-pnpm` example.
4. **Upstream RFC** in `paketo-buildpacks/rfcs` with the code and matrix as evidence;
   iterate on review; offer transfer/incubation.
5. **Decision point** per open question 7: adopted → switch the ClusterBuildpacks to
   Paketo's images and archive ours; stalled → `heroku/nodejs` per the Research table.

## Relationship to other RFCs

- **RFC-0065** defined the builder as an ordered list of Paketo composites, one
  ClusterBuildpack each, multi-arch images only. Part B keeps the mechanism, composes one
  group from individual buildpacks (all multi-arch) and adds three of ours.
- **RFC-0067** made the CLI infer what buildpacks cannot guess. Part A is exactly that:
  the platform knows which bundler a Next major uses and what buildpacks do to
  `node_modules`; the developer should not have to.
- **RFC-0075** build cache: the registry-backed cache is per builder layer; a new group
  starts with a cold cache once. Expected, measured in the matrix.

## Implementation status

Nothing implemented. Evidence, research and the two upstream drafts below.

## Appendix A — draft comment for paketo-buildpacks/nodejs#594

To be reviewed and posted by the owner; adjust voice freely.

> We run Paketo's Node.js family in production (shpyrd, an open-source PaaS on
> Kubernetes with kpack) and hit the absence of pnpm on the first real customer project:
> a Next.js 16 site with `pnpm-lock.yaml` fell into the npm group, the lockfile was
> ignored and `npm install` resolved fresh versions.
>
> We are writing `pnpm`, `pnpm-install` and `pnpm-start` in the family's style (packit;
> mirroring `yarn`, `yarn-install`, `yarn-start`), with integration tests, and will run
> them in our builder. Design notes we would like feedback on before opening an RFC:
>
> - `pnpm-install` keeps `node_modules` in the application directory with the
>   content-addressable store in a cached layer — pnpm's own layout (its `node_modules`
>   is already symlinks into `.pnpm`; the store is the cache) — and prunes dev
>   dependencies after the build instead of a separate launch-modules layer. A side
>   effect matters to Next.js 16 users: Turbopack refuses `node_modules` symlinks that
>   leave the project root (vercel/next.js#88335, #91896; PR #92886 closed), which is what
>   the family's current layout produces; we are opening a separate issue on
>   `npm-install` with the evidence.
> - pnpm version from `packageManager`, then `engines.pnpm`, then `BP_PNPM_VERSION`;
>   the CLI downloaded from GitHub releases with a checksum in `buildpack.toml` like
>   `yarn`, pipeline entry to follow.
>
> We are happy to transfer the repositories, incubate them in paketo-community, or keep
> maintaining them under review — whichever the subteam prefers. We saw #334/#336 and
> understand the bar; this comes with working code, tests and a production test matrix,
> and we will link both when the RFC opens.

## Appendix B — draft issue for paketo-buildpacks/npm-install

Title: *Next.js 16 (Turbopack) fails on the `node_modules` symlink into the layer*

> **Summary.** Since Next.js 16 (October 2025) `next build` uses Turbopack by default.
> Turbopack does not follow symlinks whose target is outside the project root
> (documented: "Files outside of the project root are not resolved"; the PR adding
> cross-root symlink support, vercel/next.js#92886, was closed unmerged). `npm-install`
> makes `/workspace/node_modules` a symlink to
> `/layers/paketo-buildpacks_npm-install/build-modules/node_modules`, so every Next 16
> project fails at `next build` on the base builder unless its build script passes
> `--webpack` or `next.config` sets `turbopack.root` to `/`. `yarn-install` has the same
> layout and the same failure.
>
> **Reproduce.** Any `create-next-app@16` project, `pack build app --builder
> paketobuildpacks/builder-jammy-base -e BP_NODE_RUN_SCRIPTS=build`:
>
> ```
> Running 'npm run build'
>   Creating an optimized production build ...
>   FATAL: An unexpected Turbopack error occurred.
>   Error [TurbopackInternalError]: Symlink [project]/node_modules is invalid,
>     it points out of the filesystem root
> ```
>
> With `next build --webpack` the same project builds.
>
> **Proposal for discussion.** An opt-in — `BP_NODE_MODULES_IN_APP=true` or a better
> name — under which `npm-install` installs into the application directory (a real
> directory), keeps the npm cache in the layer, and prunes dev dependencies after
> `node-run-script` has run instead of building a separate launch-modules layer. Heroku's
> Node.js CNB uses this layout for npm and yarn; our pnpm-install (see nodejs#594) does
> as well. Happy to write the RFC and the implementation if the subteam is open to it;
> otherwise a note in the docs pointing Next 16 users at `--webpack` would already save
> people an afternoon.

## History

- 2026-09-29: created after the first production deploy of a real Next.js 16 / pnpm
  project failed twice (npm chosen over pnpm; Turbopack refusing the buildpack's
  `node_modules` symlink) and succeeded with `next build --webpack`.
- 2026-09-29: Part B rewritten from "switch the Node group to `heroku/nodejs`" to
  "Paketo-style pnpm buildpacks built here, proposed upstream, `heroku/nodejs` as the
  exit" after finding pnpm wanted upstream since 2022 (nodejs#594) and this year's two
  RFC attempts closed for being low-effort rather than unwanted. Upstream drafts added as
  appendices.
