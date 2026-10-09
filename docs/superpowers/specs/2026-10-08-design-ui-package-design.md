# @shpyrd/ui as a published package

Date: 2026-10-08
Status: draft, awaiting review
Scope: the "Packages" decision of [the design, content and apps plan](../plans/2026-09-29-design-content-apps.md)

## Intent

`design/ui` is the platform's design system, `@shpyrd/ui`. Inside this
repository the applications use it through the npm workspace. The other
projects of the company cannot, so each keeps a copy of it:

| Project | How it gets the library |
|---|---|
| `shpyrd-signup` | `vendor/shpyrd-ui`, committed (93 files), refreshed by an rsync script from `../shpyrd/design/ui` |
| `legal` | `vendor/shpyrd-ui`, committed (99 files), the same way |
| `shpyrd-billing` | `vendor/shpyrd-ui`, committed (96 files), the same way |
| `shpyrd-cloud` (`ui/workspaces`, `ui/monitor`) | copied at build time from a core checkout at `CORE_REF`, git-ignored |

A copy drifts: today the three committed copies are snapshots from
2026-09-30 to 2026-10-06, each behind the library. The build-time copy in
shpyrd-cloud broke the v0.9.76 cloud release twice, because the apps'
lockfiles did not know the dependencies the copied folder had gained.

When this is done, `@shpyrd/ui` is a package on npm with a version of its own.
No project keeps a copy: each depends on a version and moves by bumping it.
The pieces each project built that belong in the design system live in the
library.

### What the copies hold

None of the three committed copies was edited. Each one is, file for file,
`design/ui/src` at a past commit of this repository (`test.ts` aside, which
some rsync scripts leave out):

| Project | Same as `design/ui/src` at |
|---|---|
| `shpyrd-signup` | `37e2b32` (2026-09-30) |
| `shpyrd-billing` | `24f32b0` (2026-10-03) |
| `legal` | `e60e7c2` (2026-10-06) |

So there is nothing to merge back from the copies. What each project has to
give the library is what it wrote itself, in its own `src/`.

## Decisions

| Subject | Decision |
|---|---|
| Registry | public npm, `@shpyrd/ui`; the library is MPL-2.0 like the rest of the core |
| Version | its own, in `design/ui/package.json`, independent of the core's tags |
| Form | TypeScript source, as today: consumers compile it with `transpilePackages` |
| Where it lives | where it is, `design/ui` in this repository; not a repository of its own |
| Order | one project at a time: move its components into the library, release, migrate it |

The library stays in this repository because a design session depends on the
library, its gallery and the applications being one workspace: a saved
component is on screen in a tenth of a second, and finishing a session builds
every application that uses it (`design/DESIGN_FOR_AGENT.md`).

## The package

`design/ui/package.json`:

- `"private": true` goes. `version` stays `0.0.0` until the first release:
  `0.0.0` means "never released", and the release workflow never publishes
  it. The first release makes it `0.1.0` (cycle 1).
- `files` names what ships: `src/` without the tests (`*.test.ts`,
  `*.test.tsx`) and without `src/test.ts`, plus `README.md`, `CHANGELOG.md`
  and `LICENSE` (MPL-2.0, a copy of the repository's). Nothing of `app/`
  (the gallery), `public/` or the Next configuration.
- `exports` stay as they are: `./components/*`, `./lib/*` and
  `./styles.css`, pointing at source files.
- `react`, `react-dom`, `next` and `tailwindcss` move from `dependencies` to
  `peerDependencies` (`^19`, `^19`, `^16`, `^4`), so an application never
  carries two copies of React. They stay in `devDependencies` for the
  library's own gallery, tests and type-check. The other dependencies
  (Radix, the fonts, xterm, class-variance-authority, and the rest) stay
  dependencies.
- `repository`, `homepage` and `license` are filled in for the npm page.

Two things every consumer already does, and keeps doing:

- `transpilePackages: ["@shpyrd/ui"]` in `next.config.ts`;
- `@import "@shpyrd/ui/styles.css";` in its global stylesheet. The stylesheet
  tells Tailwind where the library's classes are with a relative
  `@source "../"`, which holds from `node_modules` too.

The applications of this repository keep `"@shpyrd/ui": "*"`, linked by the
workspace. Nothing changes for them or for a design session.

## Versions

- The version lives in `design/ui/package.json` and is changed in the pull
  request that changes the library.
- Before 1.0, a change that breaks a component (a prop renamed or removed, a
  component removed, a behaviour a screen relies on changed) is a minor
  version (`0.2.0` to `0.3.0`); anything else, a patch. A consumer depends on
  `^0.3.0`, which takes patches only, so a breaking release never reaches an
  application until someone bumps it. `1.0.0` comes when the components'
  interfaces have settled.
- `design/ui/CHANGELOG.md` gets a section per version: what changed, and for
  a minor version, what a consumer has to change.

Finishing a design session (`design/DESIGN_FOR_AGENT.md`) that changed
`design/ui/src` gains a step: bump the version and write its CHANGELOG
section. A session that only touched the gallery or tests does not release.

## Publishing

A workflow of its own, `.github/workflows/ui-release.yml`, on pushes to
`main` that touch `design/ui/**`:

1. Read the version from `design/ui/package.json`. If it is `0.0.0`, or npm
   already has it, stop: a push that did not change the version publishes
   nothing.
2. Run the library's lint, type-check and tests.
3. `npm publish --access public --provenance` from `design/ui`.
4. Tag the commit `ui-v<version>`. The prefix keeps it apart from the core's
   `v*` tags, which start the core's release.
5. Create a GitHub release for `ui-v<version>` with the version's CHANGELOG
   section.

It publishes with npm's trusted publishing from GitHub Actions (OIDC), so no
npm token is stored. If npm requires a token for a package's very first
publication, that one publication uses a token secret, which is removed once
trusted publishing is configured. A version with a pre-release suffix
(`0.4.0-rc.1`) publishes under the `next` dist-tag, never `latest`.

Before the first publication the owner creates the npm organisation `shpyrd`
and gives this repository's workflow the right to publish `@shpyrd/ui`.

## Checks

In `ci.yml`, for every pull request that touches `design/ui/**`:

- **What ships.** `npm pack --dry-run` in `design/ui`; the job fails if the
  list holds a test, `app/`, `public/` or a Next configuration file.
- **What a consumer sees** is checked by the consumers: each project's CI
  builds a new version when it takes it (its Dependabot pull request).
  `shpyrd-signup` and `legal` have no CI today; each gets a minimal one
  (install, type-check, build) in its cycle, before its migration merges.
- **A forgotten bump.** A change to `design/ui/src/**` without a change of
  version gets a warning, not a failure: not every change has to be released
  at once.

`ui-release.yml` runs the library's own gates again before it publishes.

## One project at a time

Each project goes through one cycle:

1. **Into the library.** A design session in this repository moves the
   project's generic components into `design/ui`, each with a gallery page,
   its line in `app/catalog.ts` and tests, following the library's rules
   (`design/DESIGN_FOR_AGENT.md`). A component may get a better name or
   interface on the way; the project adjusts in step 3. A candidate that turns
   out to be the project's own (it only knows that project's data or states)
   stays in the project, and the session says so.
2. **Release.** The version and the CHANGELOG are bumped; the merge publishes.
3. **Migrate the project.** One pull request in the project:
   - removes `vendor/shpyrd-ui` and the rsync script (and legal's equivalent
     in its build);
   - depends on `"@shpyrd/ui": "^<the version>"`, with `package-lock.json`
     refreshed from npm;
   - replaces its own versions of the moved components with imports from the
     library;
   - fixes what moving from its old snapshot to the current library changed:
     types, a renamed prop, a look. This is an upgrade, not only a swap: the
     snapshots predate, among others, #112's design session (the brand
     palette, the new shipyard);
   - passes the project's gates (`npm ci`, lint, type-check, tests, build),
     with screenshots of its main screens in the pull request;
   - rewrites the README's passage on `vendor/shpyrd-ui`: the dependency, and
     how to bump it.
4. **Dependabot.** The project gets a `dependabot.yml` entry for npm limited
   to `@shpyrd/ui`, so each later release arrives as a pull request its CI
   checks.

### The cycles

| # | Project | Into the library | Release |
|---|---|---|---|
| 0 | this repository | the package, publishing and checks above | none |
| 1 | `shpyrd-signup` | `code-input` (`src/screens/code-input.tsx`) | `0.1.0`, the first publication |
| 2 | `legal` | `document-icon`, `table-of-contents`, what its `document-actions` adds to the library's, what is generic in its 26 lines of `globals.css` | `0.2.0` |
| 3 | `shpyrd-billing` | `choice-cards`, `list-toolbar`, `number-input`, `states`, `status`; daily and monthly ranges for `TimeChart` (owed in its README) | `0.3.0` |
| 4 | `shpyrd-cloud` (`ui/workspaces`, `ui/monitor`) | nothing; it has no components of its own to give | none |

The versions are the expected ones: a cycle whose additions break nothing may
be a patch instead. Signup is first because it is the smallest; billing last
because it has the most to give.

Cycle 4 needs only a published version, so it may run any time after cycle 1.
In shpyrd-cloud it also:

- stops the Dockerfiles copying `design/ui` from the core checkout into
  `vendor/`, and removes the `ui` scripts, CI's `npm run ui` step and the
  `vendor` lines of the apps' `.gitignore`;
- leaves `make pin` pinning the core only. The UI library has its own version
  now and is bumped on its own, by Dependabot.

## Documents that change

| Document | Change |
|---|---|
| `design/ui/README.md` (new) | what the package is; installing it; `transpilePackages` and the CSS import; the version rules; how a release happens |
| `design/ui/CHANGELOG.md` (new) | a section per version, from `0.1.0` |
| `design/DESIGN_FOR_AGENT.md` | finishing a session that changed `design/ui/src` bumps the version and the CHANGELOG; "every application that uses it" means this repository's, the other projects get it through their bump |
| `docs/superpowers/plans/2026-09-29-design-content-apps.md` | the "Packages" row: `@shpyrd/ui` is also published to npm, with a version of its own |
| each project's README | in its cycle |

`apps/AGENTS.md` does not change: the applications of this repository keep
the workspace.

## Risks

- **The npm organisation and the first publication** need the owner, before
  cycle 1 can release.
- **Peer ranges.** An application on another major of React or Next than the
  library's ranges gets a warning or a refused install. All four projects run
  React 19 and Next 16 today.
- **A public package.** Whatever is committed under `design/ui/src` is
  published. Nothing secret or licensed lives there today; `files` and the
  pack check keep everything else out. The commercial Tailwind template the
  old site used is under `apps/website`, outside the package.

## Not in this work

- Compiling the library to JavaScript, or publishing type declarations of its
  own: consumers compile the source, as they do today.
- Publishing `apps/shared` or any other member of the workspace.
- Changes to the components beyond what moving a project's pieces needs.
