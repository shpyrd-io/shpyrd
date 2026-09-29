# RFC-0079 Node.js builds: pnpm, and Next.js 16 on buildpacks

**Status:** provisional

**Owner:** Patrick Negri

**Depends on:** RFC-0065 (implemented: build composition, the builder), RFC-0067
(implemented: build profiles, the `next` profile)

**Amends:** RFC-0065 (the Node.js group of the builder), RFC-0067 (what the `next`
profile knows)

**Creation date:** 2026-09-29

**Last update:** 2026-09-29

---

## Summary

Two things a Node.js developer takes for granted do not work on the builder today, and
both surfaced on the first real project deployed to production
(`rubix-documents`, a Next.js 16 documentation site with a `pnpm-lock.yaml`):

1. **pnpm is not supported.** The builder's Node.js group is Paketo's composite
   `paketobuildpacks/nodejs`, whose order has three groups: yarn, npm, bare node. A
   project with `pnpm-lock.yaml` falls into the npm group; the lockfile is ignored and
   `npm install` resolves fresh versions. Paketo has no pnpm buildpack at all.
2. **Next.js 16 does not build.** Next 16 bundles `next build` with Turbopack by
   default; Turbopack refuses a `node_modules` that is a symlink pointing outside the
   project root, and that is exactly how Paketo's `npm-install` and `yarn-install` lay
   out dependencies (a layer under `/layers`, symlinked into `/workspace`). The build
   dies with `Symlink [project]/node_modules is invalid, it points out of the filesystem
   root` before compiling anything. Our `nextjs` example is on Next 15, where `next
   build` still used webpack, so the suite never saw it.

This RFC records what was found upstream, and proposes: the CLI's `next` build profile
learns Next 16 and says the right thing before and after a failed build (small, ships
first); the builder's Node.js group moves to Heroku's `heroku/nodejs` buildpack, which
supports npm, yarn **and pnpm**, installs npm/yarn dependencies as a real directory
(Turbopack works unchanged), and is published multi-arch for `linux/amd64` and
`linux/arm64` without a distro pin — behind a test matrix on the development cluster
first.

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

### Paketo's Node.js composite has no pnpm

`paketo-buildpacks/nodejs` `buildpack.toml` (main) lists three `[[order]]` groups:
`node-engine` + `yarn` + `yarn-install`; `node-engine` + `npm-install`; `node-engine` +
`node-start`. Each carries the optional `node-run-script`, `procfile`,
`environment-variables`, `image-labels`. There is no `pnpm-install`, and the
`paketo-buildpacks` organisation has no repository matching `pnpm`. Adding pnpm to the
current builder is therefore not "add a buildpack to the order"; the buildpack would have
to be written and maintained here.

### Paketo installs dependencies into a layer and symlinks them

`npm-install` (and `yarn-install`) create `node_modules` in a layer
(`/layers/paketo-buildpacks_npm-install/build-modules/node_modules`, and a separate
`launch-modules` layer for runtime) and symlink `/workspace/node_modules` to it. The
buildpack's documented configuration (`BP_NPM_VERSION`, `BP_KEEP_NODE_BUILD_CACHE`,
`BP_NPM_INCLUDE_BUILD_PYTHON`, `BP_NODE_PROJECT_PATH`) has no switch for this; the layout
is the caching strategy.

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
property of the buildpack.

### Heroku's Cloud Native Buildpack for Node.js

`heroku/nodejs` (heroku/buildpacks-nodejs, version 5.7.20 at the time of writing) is a
single buildpack, Buildpack API 0.10, published multi-arch at
`docker.io/heroku/buildpack-nodejs` and on the CNB registry. Its `buildpack.toml`
declares `[[targets]]` `linux/amd64` and `linux/arm64` with no distro restriction, so a
platform may use it on a stack other than Heroku's own; our stack is Paketo's jammy
(Ubuntu 22.04), which Heroku's Node binaries are built for.

- Package managers: npm, yarn, pnpm; a lockfile is required. The version comes from
  `engines.{node,npm,yarn,pnpm}` or `packageManager` in `package.json`.
- Scripts: `heroku-prebuild`, `build` (or `heroku-build`), `heroku-postbuild` run
  automatically. No `BP_NODE_RUN_SCRIPTS`.
- npm and yarn install into the application directory: `/workspace/node_modules` is a
  real directory. Turbopack's rule is not triggered.
- pnpm (`src/package_managers/pnpm.rs`): `store-dir` in a cached layer,
  `virtual-store-dir` in a launch layer under `/layers`, and a symlink from the layer's
  `node_modules` back to the application's so packages resolve. The application's
  `node_modules/<package>` entries therefore point into `/layers`. This is the shape
  Turbopack rejects; **pnpm + Next 16 is expected to fail on this buildpack as well** and
  must be verified (open question 3). No issue mentioning Turbopack exists in that
  repository.

### What our own suite covers

`shpyrd-examples/nextjs` pins `next ^15.5.0` with `build: next build`: webpack. The
example that should have caught this is one major behind the framework's default.

## Design

### Part A — the CLI says the right thing (RFC-0067 amendment)

The `next` build profile (`internal/cli/profiles.go`) already recognises a project with
`next` in `dependencies`. It learns:

- **Before upload**, when the resolved Next major is ≥ 16, the `build` script runs
  `next build` without `--webpack`, and `next.config.*` sets no `turbopack.root`: print
  one warning naming the cause and the two fixes, then continue (the build may be on a
  builder where it works, see Part B). Wording, short:

  ```
  Next.js 16 builds with Turbopack, which does not follow the node_modules symlink
  buildpacks create. If the build fails with "points out of the filesystem root", use
  `next build --webpack` in your build script, or set turbopack.root to "/" in next.config.
  ```

- **After a failed build**, when the followed log contains `points out of the filesystem
  root`, the deploy's error message ends with the same two fixes. The log follower in
  `shpyrd deploy` already reads every line; this is a pattern and a sentence.

Documentation: the Node.js page of the docs gets a "Next.js 16" section with the same two
options and why. The `nextjs` example moves to Next 16 with `next build --webpack` until
Part B lands, so the suite exercises the framework's current default.

This part is independent of the builder decision and costs an afternoon.

### Part B — the builder's Node.js group becomes `heroku/nodejs`

In `deploy/components/kpack/base/builder.yaml`:

```yaml
- group:
    - name: heroku-nodejs          # ClusterBuildpack: docker.io/heroku/buildpack-nodejs
      kind: ClusterBuildpack
    - name: paketo-procfile        # Procfile keeps working; optional so its absence is fine
      kind: ClusterBuildpack
      optional: true
- group:                           # fallback: a package.json without any lockfile
    - name: paketo-nodejs
      kind: ClusterBuildpack
```

What changes for a project:

| | Paketo composite (today) | `heroku/nodejs` |
| --- | --- | --- |
| npm, yarn | yes | yes, lockfile required |
| pnpm | **no** (npm, lockfile ignored) | yes, `--frozen-lockfile` |
| `build` script | only with `BP_NODE_RUN_SCRIPTS=build` (the `next` profile sets it) | runs automatically; `heroku-prebuild`/`heroku-postbuild` too |
| Node version | `BP_NODE_VERSION`, `.nvmrc`, `engines.node` | `engines.node` (latest LTS without) |
| `node_modules` | symlink into `/layers` | npm/yarn: real directory; pnpm: entries symlinked into the virtual store layer |
| Next 16 + Turbopack, npm/yarn | fails | works |
| Next 16 + Turbopack, pnpm | fails | expected to fail (verify) — Part A's guidance applies |
| Start command | `npm start` / Procfile | `npm start` / `pnpm start` / Procfile via the optional procfile buildpack |
| Native modules | full stack (`build.stack: full`) for python/make | same |

`BP_NODE_RUN_SCRIPTS` set by the `next` profile becomes inert on the new group (Heroku
ignores `BP_*`) and stays harmless; the profile keeps setting it for the fallback group.

Not chosen:

- **A shpyrd pnpm buildpack on Paketo.** Paketo has none; writing one means owning a
  package-manager integration (version resolution, store caching, lockfile validation,
  multi-arch publishing) for the life of the platform. Heroku already maintains that.
- **A shpyrd "materialise node_modules" buildpack** that replaces the symlink with a copy
  before the build script and puts it back after. Two custom buildpacks bracketing
  `node-run-script`, the Paketo composite decomposed into individual buildpacks to make
  room for them, and a copy of `node_modules` per build. Solves only the Turbopack half.
- **Asking upstream.** The boundary is documented as a design choice and the pull
  request implementing cross-root symlinks was closed.
- **Leaving both as documentation.** pnpm is table stakes; "commit a package-lock.json"
  is not an answer a Node developer accepts in 2026.

### Test matrix (before Part B merges)

On the development cluster, with the new group and the fallback:

| Example | Package manager | Expectation |
| --- | --- | --- |
| `node` | npm | builds, starts, `Procfile` honoured if present |
| `nextjs` (bumped to 16, plain `next build`) | npm | builds with Turbopack, no flag |
| `nextjs-pnpm` (new, same app) | pnpm | installs from `pnpm-lock.yaml`; Turbopack outcome recorded (open question 3) |
| `react` | npm | static build served as today |
| `sinatra-react` | ruby + node | the Ruby group still wins detection; unaffected |
| a `package.json` with no lockfile | — | falls to the Paketo group and builds as today |
| `rubix-documents` (the production case) | pnpm | installs from the lockfile; builds with `--webpack` or `turbopack.root`; runs |

Build times and image sizes are recorded next to today's, and the build cache
(RFC-0075, registry-backed) is checked to still hit on the second build.

## Open questions

1. **Replace or add?** `heroku/nodejs` as the first Node group with the Paketo composite
   kept as a fallback for projects without a lockfile — or a clean replacement. Default:
   **keep the fallback** until the matrix shows Heroku's detection fails cleanly (rather
   than builds and errors) without a lockfile; if it errors, drop the fallback and let
   the CLI tell lockfile-less projects to commit one.
2. **Procfile on the new group.** Heroku's own `heroku/procfile` or Paketo's, optional in
   the same group. Default: **Paketo's**, already a ClusterBuildpack here; verify it
   detects after `heroku/nodejs` sets its default process.
3. **pnpm + Next 16 on `heroku/nodejs`.** Expected to hit the same Turbopack rule through
   the virtual store layer. Default: **verify in the matrix; if it fails, Part A's
   guidance is the answer for pnpm projects** and the docs say so plainly; no custom
   buildpack.
4. **Which fix to recommend first**, `next build --webpack` or `turbopack.root: "/"`.
   Default: **`--webpack`** — documented flag, no widening of the bundler's root to the
   whole filesystem; mention `turbopack.root` second for people who want Turbopack's
   speed.
5. **Node version pinning.** Heroku uses `engines.node` and defaults to the latest LTS,
   Paketo honours `.nvmrc` and `BP_NODE_VERSION`. Default: **document `engines.node`**;
   the `next` profile warns when a project has `.nvmrc` but no `engines.node`.
6. **Runtime memory.** The same deploy was OOM-killed at start on the default instance
   size (`shared-s`, 64 MiB) until resized to `shared-m`. Out of scope here: a separate
   change makes the default size follow the detected runtime (the catalog already names
   `shared-m` for Node.js). Recorded so it is not lost.

## Relationship to other RFCs

- **RFC-0065** defined the builder as an ordered list of Paketo composites, one
  ClusterBuildpack each, multi-arch images only. Part B keeps the mechanism and changes
  the Node group; `heroku/nodejs` satisfies the multi-arch rule.
- **RFC-0067** made the CLI infer what buildpacks cannot guess. Part A is exactly that:
  the platform knows which bundler a Next major uses and what buildpacks do to
  `node_modules`; the developer should not have to.
- **RFC-0075** build cache: the registry-backed cache is per builder layer; switching the
  Node group invalidates cached Node layers once. Expected, noted in the matrix.

## Implementation status

Nothing implemented. Evidence and research only.

## History

- 2026-09-29: created after the first production deploy of a real Next.js 16 / pnpm
  project failed twice (npm chosen over pnpm; Turbopack refusing the buildpack's
  `node_modules` symlink) and succeeded with `next build --webpack`.
