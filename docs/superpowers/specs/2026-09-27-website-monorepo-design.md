# Moving the website into the monorepo

Date: 2026-09-27
Status: approved, ready for an implementation plan
Scope: phase 1 of the repository reorganization proposal

## Intent

The [repository, product, licensing and domain proposal][proposal] asks the
`shpyrd-io` organization to converge on two canonical repositories — a public
`shpyrd` and a private `shpyrd-cloud` — so that one product change becomes one
pull request: implementation, tests, SDK, examples and the documentation that
describes them all move together.

This spec covers only the first step: relocating the public website and
documentation from `shpyrd-io/shpyrd-docs` into `website/` in this repository,
with nothing about the site itself changed. It is deliberately a move and not a
redesign. The proposal's information architecture, its Cloud-first positioning
and the `enterprise/` boundary are later phases, each needing its own design.

[proposal]: https://gist.github.com/paezao/c078584b5946b409dd58b0c13c84bdb6

Success for this phase means: the site's source lives in this repository with
its history intact, every URL `shpyrd.io` serves today resolves to the same
content, CI builds the site without slowing Go work down, and the licensing
boundary between MPL-covered code and the purchased website template is
explicit in the tree.

## Current state

Surveyed on 2026-09-27. This inventory is the reason several decisions below
went the way they did, so it is recorded rather than left implicit.

| Repository | Visibility | Contents |
| --- | --- | --- |
| `shpyrd` | public | Go platform, `ui/` Vite dashboard (embedded in the binary), 70 RFCs, 5 example apps the e2e job deploys. MPL 2.0 |
| `shpyrd-docs` | public | Next.js 13.4.2 + Markdoc site: one marketing homepage and 22 documentation pages. **Tailwind UI licensed**, not MPL |
| `shpyrd-examples` | public | 17 examples, one per language or pattern |
| `homebrew-tap` | public | Homebrew tap for the CLI |
| `shpyrd-cloud` | private | Go; already matches the proposal's target |

Relevant facts:

- `shpyrd.io` is hosted on Vercel and redirects to `www.shpyrd.io`. There is no
  `vercel.json` in either repository, so all build configuration lives in the
  Vercel dashboard and must be reproduced by hand.
- `shpyrd-docs` has no GitHub Actions workflows. It deploys purely through
  Vercel's git integration.
- The root `README.md` links `shpyrd.io/install.sh`, `shpyrd.io/screenshots/*.png`
  and `shpyrd.io/docs/tour`. The install script served at `/install.sh` is the
  documented non-Homebrew installation path, so its URL is load-bearing.
- `shpyrd` has no branch protection and no rulesets, so no status check is
  required to merge. Path-filtered workflows cannot block a merge by reporting
  "skipped".
- Neither repository has an open pull request, and `shpyrd-docs` has one stale
  branch, `docs/mvp`.
- `node_modules` is already ignored repository-wide by the root `.gitignore`.

## Decisions

Four choices shape the work. Each was taken deliberately over a named
alternative.

**A pure lift-and-shift, not a restructure.** `website/` becomes the current
site verbatim. Reshaping the information architecture at the same time would put
every live `/docs/*` URL at risk in the same change that relocates the files,
making a regression impossible to attribute. Restructuring is a later phase that
can add redirects as its own reviewable concern.

**History preserved through a subtree merge.** `git blame` keeps resolving to
the original authors and `git log HEAD^2` reaches the imported commits.
(Path-limited `git log -- website/` does not list them — the pre-move commits
spell their paths at the repository root, so the filter cannot match.) The cost is
a merge commit with two parents, which the repository's squash-merge convention
would discard; this one pull request is therefore merged with a merge commit.
The alternative — copying the tree in a single commit and leaving the history
reachable in `shpyrd-docs` — was rejected because blame on documentation is worth
more than convention uniformity for a single PR.

**The website is carved out of MPL 2.0 rather than rebuilt.** The site is the
purchased Tailwind UI *Syntax* template. Its licence forbids redistributing the
template or derivatives separately from an End Product, while this repository's
root licence would purport to grant exactly that. The licence does permit the
template inside a public open-source project whose primary purpose is not
redistributing components, which a product website satisfies, so the template
stays and the boundary is made explicit. Rebuilding on an openly licensed docs
framework (Starlight, Nextra, Fumadocs) would give a uniform MPL tree but
discards paid work and rewrites layout, navigation and search for no phase-1
benefit.

**The Vercel cutover runs as a new project, verified before the domain moves.**
Repointing the existing project in place is fewer steps but verifies on the live
domain. A parallel project is checked on its `*.vercel.app` URL first.

## Phase 1 design

### Target layout

```text
shpyrd/
├── website/
│   ├── LICENSE              LICENSE.md renamed; Tailwind UI text unchanged
│   ├── README.md            rewritten; today it is Tailwind "Syntax" boilerplate
│   ├── package.json         name: tailwindui-syntax → shpyrd-website
│   ├── .gitignore           trimmed to what the root does not cover
│   ├── next.config.mjs, tailwind.config.js, postcss.config.js,
│   │   prettier.config.js, .eslintrc.json, jsconfig.json, package-lock.json
│   ├── public/              favicon.ico, install.sh, fonts/, screenshots/
│   └── src/                 components/, images/, markdoc/, pages/, styles/
├── LICENSE                  unchanged MPL 2.0 text
└── README.md                + website/ in the layout, + a License section
```

Two paths are dropped in the move: `.devcontainer/`, because this repository has
its own, and `tmp/Hero.jsx`, a stray scratch file.

Nothing under `src/` or `public/` is modified. `/`, the 22 `/docs/*` pages,
`/install.sh` and `/screenshots/*.png` therefore serve byte-identical content
after the move.

`website/.gitignore` is trimmed to the entries that matter here — `/.next`,
`/out`, `.vercel` and `.env*.local` — because the root already ignores
`node_modules`, `coverage`, `*.pem` and `.DS_Store` unanchored, so those cover
`website/` too. The trim also drops `/.pnp`, `.pnp.js`, `/build` and the
`npm-debug.log*`/`yarn-*.log*` patterns, which the root does *not* cover: npm 7
and later write debug logs under `~/.npm/_logs`, and Next produces neither pnp
files nor `/build`. Keeping the file local to the directory rather than folding it
into the root keeps the paths relative and the website self-contained.

### Licensing boundary

`website/LICENSE` carries the Tailwind UI licence text verbatim. The root
`LICENSE` is not edited — modifying the MPL text would make the repository's
licence non-standard and defeat automated licence detection.

The boundary is declared in prose instead, everywhere a reader meets a licence
claim:

- a **License** section in the root `README.md`: the repository is MPL 2.0
  except `website/`, which is governed by `website/LICENSE`;
- a paragraph in `CONTRIBUTING.md`, whose opening line otherwise calls the whole
  project MPL 2.0;
- a matching note at the top of `website/README.md`.

This is the slot `enterprise/LICENSE` will occupy in a later phase, so the
pattern is established once here.

The proposal recommends the final licensing boundaries be reviewed by counsel.
This spec does not substitute for that review; it records the reading the work
proceeds on.

### History

```bash
git checkout -b chore/website-monorepo
git remote add website-src git@github.com:shpyrd-io/shpyrd-docs.git
git fetch website-src main
git subtree add --prefix=website website-src main    # merge commit, two parents
# a second commit applies the renames, rewrites and deletions under Target layout
```

No `--squash`. The resulting pull request is merged with a merge commit; every
later pull request keeps the repository's squash convention.

### CI

Per-job path filters inside one workflow would need a `paths-filter` action and
a gate job. With no required status checks to protect, splitting the workflow is
simpler and has the same effect.

- `ci.yml` gains a workflow-level
  `paths-ignore: ['website/**', 'rfcs/**', 'docs/**', '*.md']`, so a
  documentation-only pull request no longer runs the 45-minute kind e2e. GitHub
  skips a workflow only when *every* changed path matches an ignore pattern, so
  a Go change that also touches a README still runs the full suite.
- a new `website.yml` mirrors the existing `ui` job — `actions/setup-node@v4`
  with node 24, `cache-dependency-path: website/package-lock.json`,
  `npm ci --no-audit --no-fund`, `npm run lint`, `npm run build`, and
  `working-directory: website` — filtered to
  `paths: ['website/**', '.github/workflows/website.yml']`. The project defines
  no test script, so there is no test step. `working-directory` is load-bearing:
  a build launched from the repository root fails outright, because Next, the
  Markdoc loader and the search-index builder all resolve config and content
  relative to the process working directory.
- the `Makefile` gains `website:` and `website-dev:` alongside the existing
  `ui:` target.

### Vercel cutover

Executed by the maintainer after the pull request lands, because it needs
dashboard access this work cannot reach.

1. Create a Vercel project against `shpyrd-io/shpyrd` with **Root Directory
   `website`**, framework preset Next.js, node 24.
2. Set the Ignored Build Step to `git diff --quiet HEAD^ HEAD ./`. With the root
   directory set, this skips builds for commits that do not touch `website/` —
   which matters now that the site shares a repository with a public Go project
   taking pull requests.
3. Verify the production-branch deploy on its `*.vercel.app` URL: the homepage,
   a `/docs/*` page, search, and `/install.sh` returning the script.
4. Move `shpyrd.io` and `www.shpyrd.io` to the new project, then disconnect git
   integration on the old one.

### `shpyrd-docs` afterlife

Nothing is deleted. The repository stays public with a README banner pointing at
`shpyrd-io/shpyrd/website`, its Vercel git integration disconnected, and the
stale `docs/mvp` branch untouched. Whether to archive it is a separate, later
decision.

## Verification

- `cd website && npm ci && npm run lint && npm run build` succeeds locally.
- the merge commit has two parents, `git log HEAD^2` reaches the imported
  history, and `git blame website/src/pages/docs/cli.md` attributes lines to
  their original authors. Path-limited `git log -- website/` does *not* list
  them: the pre-move commits spell their paths at the repository root, so the
  filter cannot match them.
- The built route list contains `/`, all 22 `/docs/*` paths and the 404 page;
  `website/public/install.sh` and the 18 screenshots are present and unmodified
  (compare against `shpyrd-docs` at the merged commit).
- A Go-only pull request runs `ci.yml` and not `website.yml`; a
  `website/`-only pull request runs the reverse.
- The Vercel preview renders the homepage, a docs page, search and
  `/install.sh` before any domain is moved.

## Out of scope

Each of these needs its own design before it is touched:

- the proposal's information architecture — `/product`, `/cloud`, `/pricing`,
  `/enterprise`, `/open-source` — and redirects for any `/docs/*` path it moves;
- Cloud-first homepage positioning and copy;
- `blog/` and `changelog/`;
- `[Cloud]` and `[Enterprise]` feature-availability badges in the docs;
- upgrading the site off Next.js 13.4.2 (its `engines` field already asks for
  node 24);
- consolidating `examples/` with `shpyrd-examples`, which must keep the five
  apps the e2e job deploys working;
- the naming clash between `docs/` (currently superpowers plans; the proposal
  wants architecture, development and contributing guides) and `rfcs/`, which is
  already the design record the proposal's `docs/architecture/` would duplicate;
- the `enterprise/` directory and its commercial licence;
- anything in `shpyrd-cloud`.

## Known deviations from the proposal

**The end state cannot be exactly two repositories.** Homebrew requires a tap to
live in its own repository named `homebrew-*`, so `homebrew-tap` survives
consolidation. The realistic end state is `shpyrd` + `shpyrd-cloud` +
`homebrew-tap`.

**`website/` will not subdivide into `marketing/`, `docs/`, `blog/` and
`changelog/` as drawn in the proposal.** Markdoc resolves pages from
`src/pages`, so the site's own routing dictates the layout inside `website/`.
The proposal already defers to "Shpyrd's existing technology and build system
rather than forcing a particular monorepo convention".
