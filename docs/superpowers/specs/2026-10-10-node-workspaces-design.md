# Buildpack builds for JavaScript workspaces

Date: 2026-10-10
Status: draft, awaiting review
Issues: #145 (workspaces), #143 (a project's first builder)
Scope: `buildpacks/`, `api/v1alpha1`, `internal/controller`, `internal/cli`,
`pkg/api`, `deploy/components/kpack`, `examples/`, `content/docs`, CI

## Intent

An app that lives in a JavaScript workspace (a monorepo of npm, pnpm or
Yarn packages) builds and runs with buildpacks, with no Dockerfile.
Today it can't: Paketo's npm-install and yarn-install copy every workspace
package into a layer of their own and leave symlinks behind, so a build
runs in the copy, cannot read the rest of the repository, and writes its
output where the image never sees it; Paketo has no pnpm at all (#145).

The first user is shpyrd's own website (`apps/website`, which uses the
workspace's `design/ui` and `content`). Any customer with a Turborepo, Nx
or plain workspace monorepo meets the same wall.

Done when:

- an app in an npm, pnpm or Yarn workspace deploys with `cd <package> &&
  shpyrd deploy` and nothing else;
- `examples/monorepo` (pnpm, two packages, one depending on the other)
  deploys in CI's end-to-end job;
- a project's first deploy with a builder of its own no longer fails while
  that builder is made (#143);
- the docs say how (`deploying.md`, `shpyrd-yaml.md`, `cli.md`);
- after a release, the website deploys this way and its Dockerfile is
  deleted (a follow-up pull request).

## Decisions

| Subject | Decision |
|---|---|
| Package managers | npm, pnpm and Yarn (1 and 2+), now |
| Shape | one shpyrd buildpack, `node-workspace`, after Paketo's `node-engine` |
| Where it builds | in place, in `/workspace`: `node_modules` and the build output stay where the app expects them |
| How a project asks | `build.workspace: <path>` in `shpyrd.yaml`, or the CLI detects it from the package's folder |
| #143 | fixed in the same work: every workspace project gets a builder of its own on its first deploy |

Rejected: a buildpack per manager (the same logic three times, three
things to release); Paketo's own buildpacks with hints (the layer copy is
the problem, and there is no pnpm); a change upstream at Paketo (slow,
out of our hands).

## 1. How it is used

A package of a workspace is deployed from its own folder, with its own
`shpyrd.yaml` there:

```
cd apps/website && shpyrd deploy
```

- The CLI walks up from the current folder, at most to the Git
  repository's root, to the nearest folder that declares a workspace
  containing the current one:
  - npm and Yarn: a `package.json` whose `workspaces` (an array, or
    Yarn 1's `{ "packages": [...] }`) matches the folder's path;
  - pnpm: a `pnpm-workspace.yaml` whose `packages` matches it.

  Matching follows the managers' globs (`apps/*`, `packages/**`) and
  their negations (`!**/test`). The workspace's root need not be the Git
  root (`examples/monorepo` inside this repository).
- It says what it worked out, as build profiles do:

  ```
  ==> Detected a pnpm workspace at examples/monorepo; building apps/web in it
      build.workspace=apps/web (shpyrd.yaml values win; --save writes it there)
  ```

- It archives the workspace's root, not the folder (`git archive` of that
  subtree at HEAD, or the working tree with `--working-tree`), and sends
  `build.workspace` (the package's path from the workspace's root) with the
  deploy, as it sends `build.env`.
- `shpyrd.yaml` is read from the folder you deploy from, as always.
  `build.workspace` may also be written by hand; whatever is written wins
  over detection, as everywhere. `--save` writes the detected value.
- A workspace build is a buildpacks build: `strategy` defaults to
  `buildpacks` even when the workspace's root has a Dockerfile (shpyrd's
  does). `strategy: dockerfile`, written, still wins, and then
  `build.workspace` is refused (below).
- Nothing changes elsewhere: detection fires only inside a workspace and
  only for a folder the workspace lists.

## 2. The buildpack: `buildpacks/node-workspace`

Shell, as `buildpacks/build-env` is, with small Node scripts where JSON or
globs must be read: Node is there at build time, from `node-engine`.
`api = "0.9"`, `id = "shpyrd/node-workspace"`, stacks `*`.

### Detect

Passes when the build names a workspace (`BP_NODE_WORKSPACE`, set by the
controller from `build.workspace`) and the workspace's root has a lockfile.
It requires `node` at build and at launch, so `node-engine` provides it.
Otherwise it fails detection and ordinary Node apps keep Paketo.

### Build

1. **The manager**, by lockfile at the root: `pnpm-lock.yaml` → pnpm,
   `yarn.lock` → Yarn (1 when the lockfile is Yarn 1's format, else 2+),
   `package-lock.json` → npm. With none: fail, "no lockfile at the
   workspace's root; commit the one your package manager writes".
2. **The manager's version.** npm is Node's own. pnpm and Yarn come through
   corepack at the version the root's `packageManager` names (corepack's
   own default without it). Where Node has no corepack (Node 25 and
   later), the buildpack installs it with npm into a layer. corepack's
   home is a cached layer.
3. **The package.** `BP_NODE_WORKSPACE` must name a folder that is one of
   the workspace's packages; else fail, listing the packages there are.
   Its `name` is read from its `package.json` (Yarn's commands want it).
4. **Install**, with dev dependencies (the build needs them), only what
   the package needs where the manager can:

   | Manager | Command |
   |---|---|
   | npm | `npm ci` (the whole workspace: `--workspace` leaves out the root's own tools, such as turbo) |
   | pnpm | `pnpm install --frozen-lockfile --filter ./<path>...` |
   | Yarn 2+ | `yarn install --immutable` (`workspaces focus` leaves out the root's tools too) |
   | Yarn 1 | `yarn install --frozen-lockfile` (it cannot install one package alone) |

   The managers' download caches (npm's cache, pnpm's store, Yarn's cache)
   are a cached layer, so a second build downloads only what changed.
   `node_modules` is rebuilt every time. A lockfile out of date fails with
   the manager's own message.
5. **Build**: the package's `build` script, if it has one, through its
   manager: `npm run build --workspace <path>`, `pnpm --filter ./<path> run
   build`, `yarn workspace <name> run build`. The package's dependencies
   are not built on their own: a monorepo that needs that says so in the
   package's `build` script (turbo, nx), as it would run it locally.
6. **Launch**: the process `web` (default) is the package's `start` script
   through its manager, run from the workspace's root, with
   `NODE_ENV=production` in the launch environment. Without a `start`
   script and without a `Procfile`: fail, "add a start script to
   <path>/package.json, or a Procfile". A `Procfile` (Paketo's procfile
   buildpack runs after) replaces `web` and may add other types.

7. **Dev dependencies** leave the image once the build is done (the root's
   tools included), from the managers' caches, without the network: `npm
   prune --omit=dev`; pnpm reinstalls with `--prod --offline`; Yarn 4
   `workspaces focus --all --production`; Yarn 1 reinstalls with
   `--production`. Yarn 2 and 3, without `focus`, keep them and say so.
   `BP_KEEP_DEV_DEPENDENCIES=true` keeps them, the name RFC-0079 gives it.

## 3. Platform plumbing

- **API**: `App.spec.build.workspace` (string, optional): a relative path,
  no `..`, no leading `/`. The API refuses it with `strategy: dockerfile`
  and with `build.buildpacks` ("a workspace chooses its own buildpacks").
  `build.stack: full` stays allowed. The CLI's `projectBuild` gains
  `workspace` too.
- **Builder** (`internal/controller/builder.go`): a workspace makes
  `composesBuild` true; the project's Builder has one group:
  `shpyrd-build-env` (when the project has build variables, as today),
  `paketo-node-engine`, `shpyrd-node-workspace`, `paketo-procfile`
  (optional). The stack follows `build.stack`.
- **Build environment** (`desired.go`): `BP_NODE_WORKSPACE=<path>` in the
  kpack Image's build env, before `build.env`, so a value written there
  wins.
- **ClusterBuildpacks** (`deploy/components/kpack/base/builder.yaml`):
  `paketo-node-engine` (`index.docker.io/paketobuildpacks/node-engine`)
  and `shpyrd-node-workspace` (`ghcr.io/shpyrd-io/buildpack-node-workspace:0.1.0`),
  published by `.github/workflows/buildpacks.yml` from the tag
  `buildpack-node-workspace/v0.1.0`.
- **#143**: while a project's own Builder is not ready, the project reports
  Building, "preparing the project's builder", and the controller waits
  for it; the build is marked failed only when the Builder itself fails
  (its Ready condition False with a reason other than not ready yet) or a
  Build does. This replaces "The build could not start: a fault of the
  platform" for that case.

## 4. Example, tests, website, docs

- **Example**: `examples/monorepo`, a pnpm workspace with `apps/web` (a
  small Node HTTP server) depending on `packages/greeting` (whose `build`
  script writes its output). No registry dependencies, so the lockfile is
  small. CI's end-to-end job deploys it from `examples/monorepo/apps/web`
  with no `shpyrd.yaml` workspace setting and checks the page answers with
  the greeting.
- **Buildpack tests** (`buildpacks/node-workspace/*_test.go`, as
  `build_env_test.go`): `detect` and `build` against small fixture
  workspaces for npm, pnpm, Yarn 4 and Yarn 1: what is installed, what is
  built, the `web` process recorded; and the failures: an unknown path, no
  lockfile, no start script. They need Node and the network, and skip
  without Node.
- **Controller tests**: the Builder's group, `BP_NODE_WORKSPACE` and its
  precedence, the API's refusals, #143 (a Builder not ready makes the
  project wait, a Builder failed fails it).
- **CLI tests**: detection for each manager's layout, globs and negations,
  a workspace below the Git root, a folder the workspace does not list
  (no detection), the archive taken at the workspace's root, `--save`, and
  a written `build.workspace` winning.
- **Website** (its own pull request, after the platform release with the
  buildpack): `apps/website/shpyrd.yaml` drops the Dockerfile settings;
  `apps/website/Dockerfile`, `Dockerfile.dockerignore` and
  `output: "standalone"` go; the workflow deploys with `cd apps/website &&
  shpyrd deploy`.
- **Docs**: `deploying.md` gains "Monorepos and workspaces"; `shpyrd-yaml.md`
  the `build.workspace` field; `cli.md` what detection prints.

## Order

1. One pull request: the buildpack, the plumbing, #143, the example, the
   tests and the docs. Before its end-to-end job can pass, the tag
   `buildpack-node-workspace/v0.1.0` is pushed from the branch, so the
   image exists.
2. Merge, and a shpyrd release.
3. The website's pull request. It also needs the object gateway's fix for
   source uploads (in progress elsewhere): no website deploy succeeds today.

## Risks

- **corepack** downloads pnpm and Yarn at build time: a build needs the
  network to reach their registry (as installs already do).
- **Yarn 1** installs the whole workspace: slower for large monorepos.
- **Builds of dependencies** are the package's own `build` script's job; a
  monorepo that relies on its tool to build them in order must call it
  there.
