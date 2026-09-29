# Website Monorepo Move Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Relocate the `shpyrd-io/shpyrd-docs` website into `apps/website/` in this repository with its git history intact, its licence carved out of MPL 2.0, and CI building it without slowing Go work down.

**Architecture:** A `git subtree add` brings the site in as a two-parent merge commit, so `git blame` keeps resolving to the original authors and the pre-move commits stay reachable through the merge's second parent. A second commit applies the only four deliberate changes (licence filename, README, package name, `.gitignore`) and prunes two paths. The licensing boundary is declared in prose because the root MPL text must stay pristine. CI splits into two path-filtered workflows rather than adding per-job filters, since no status check is required to merge.

**Tech Stack:** Next.js 13.4.2 + Markdoc + Tailwind CSS 3 (the purchased Tailwind UI *Syntax* template), node 24 in CI, GitHub Actions, GNU Make, Vercel.

**Spec:** [`docs/superpowers/specs/2026-09-27-website-monorepo-design.md`](../specs/2026-09-27-website-monorepo-design.md)

## Global Constraints

- This is a **lift-and-shift**. Nothing under `apps/website/src/` or `apps/website/public/` may be modified — those paths are what `shpyrd.io` serves today.
- The root `LICENSE` file is **never edited**. Its MPL 2.0 text must stay byte-identical so automated licence detection keeps working.
- `apps/website/LICENSE` carries the Tailwind UI licence text **verbatim** from `shpyrd-docs/LICENSE.md`. Only the filename changes.
- Every command that builds the site runs with the working directory set to `apps/website/`. `src/markdoc/search.mjs:53` calls `path.resolve('./src/pages')`, and Next and the Markdoc loader resolve their config the same way, so a build launched from the repository root **fails outright** (verified: lint reports `Cannot find module 'next/babel'`; with `--no-lint`, the Markdoc loader errors).
- Node version in CI is **24**, matching the existing `ui` job and `apps/website/package.json`'s `engines` field.
- No `--squash` on the subtree add, and the resulting pull request is merged with a **merge commit**, not squashed. Squashing discards the second parent and the history with it.
- Commits follow [Conventional Commits](https://www.conventionalcommits.org) and need a DCO sign-off: use `git commit -s`.
- Work happens on the existing branch `chore/website-monorepo`. Nothing is pushed to `main`, and no repository is deleted or archived.

## Review Focus

Five failure modes the spec implies that no obvious task step would catch. Each has a test pinned to the task that owns the code.

1. **A build launched from the repository root fails.** Next, the Markdoc loader and `search.mjs` all resolve config and content relative to the process CWD — lint dies with `Cannot find module 'next/babel'`, and with lint skipped the Markdoc loader errors. Verified 2026-09-27; the failure is loud, not silent. Pinned in Task 2, Step 6 and Task 5, Step 4.
2. **`public/install.sh` must stay byte-identical.** It is the documented non-Homebrew install path (`curl -fsSL https://shpyrd.io/install.sh | sh` in `README.md:18`); a mangled line ending breaks installs for everyone. Pinned in Task 2, Step 5.
3. **A docs-only pull request reports no status checks at all** under `paths-ignore`. Harmless today with no branch protection; it becomes a merge blocker the moment a required check is added. Pinned in Task 4, Step 6.
4. **The 18 screenshots and the 3 `woff2` fonts must survive the move intact** (`public/fonts/` holds four files — three binaries and `lexend.txt`). `README.md:8` hot-links a screenshot, and the fonts are `woff2` binaries that a text-mode copy would corrupt. Pinned in Task 2, Step 5.
5. **Deleting `tmp/Hero.jsx` must not break the build** — `src/components/Hero.jsx` also exists, and the two are easy to confuse. Verified unreferenced on 2026-09-27; re-checked in Task 2, Step 2.

---

## Task 1: Bring the site in with its history

**Files:**
- Create: `apps/website/` (93 files, imported wholesale from `shpyrd-io/shpyrd-docs@main`; 91 after Task 2's prune)
- Modify: nothing outside `apps/website/`

**Interfaces:**
- Consumes: nothing; this is the first task.
- Produces: `apps/website/` populated with the `shpyrd-docs` tree at its `main` commit, and a merge commit whose second parent is that commit. Later tasks edit files inside `apps/website/`.

- [ ] **Step 1: Confirm the starting state**

```bash
cd /home/nkr/Projects/shpyrd
git branch --show-current
git status --short
```

Expected: branch `chore/website-monorepo`, clean tree. If the branch does not exist, `git checkout -b chore/website-monorepo` from `main` first. If the tree is dirty, stop and ask — the subtree add refuses to run against uncommitted changes.

- [ ] **Step 2: Add the source repository as a remote and fetch it**

```bash
git remote add website-src git@github.com:shpyrd-io/shpyrd-docs.git
git fetch website-src main
git log --oneline -1 website-src/main
```

Expected: the fetch succeeds. The tip was `9503858 docs(roadmap): RFC-0075 in progress (v0.9.17 foundation) (#53)` on 2026-09-27; a later SHA just means someone pushed to `shpyrd-docs` since — note it and carry on. Do not import from a local clone of `shpyrd-docs`: the one on this machine was 12 commits behind origin, which is how `mcp.md` nearly went missing.

- [ ] **Step 3: Import the tree with its history**

```bash
git subtree add --prefix=apps/website website-src main
```

Expected: `git subtree` prints `Added dir 'website'` and creates a merge commit. No `--squash` — that flag is what this whole task exists to avoid.

- [ ] **Step 4: Verify the history came with it**

```bash
git log --oneline HEAD^2 | wc -l
git log --oneline HEAD^2 | head -3
git log --oneline HEAD^2 -- src/pages/docs/cli.md | wc -l
```

Expected: ~100 commits reachable through the second parent, the newest being the `shpyrd-docs` tip, and ~27 of them touching `src/pages/docs/cli.md`. `HEAD^2` failing means the import was squashed — reset with `git reset --hard HEAD~1` and redo Step 3 without `--squash`.

Do **not** assert on `git log --oneline -- apps/website/`: it prints 1, and that is correct. Path-limiting matches paths as each commit spells them, and the pre-move commits spell theirs at the repository root (`src/pages/...`), so the filter cannot reach them. `--follow` returns 0 for the same reason.

- [ ] **Step 5: Verify blame resolves to the original authors**

```bash
git blame -L 1,3 --porcelain apps/website/src/pages/docs/cli.md | grep -m2 '^author '
```

Expected: a real author name, not the name of whoever is running this plan.

- [ ] **Step 6: Verify the merge commit has two parents**

```bash
git cat-file -p HEAD | grep -c '^parent '
```

Expected: `2`. This is the property the pull request must preserve at merge time.

- [ ] **Step 7: Verify the imported tree is identical to the source**

```bash
git diff --stat website-src/main HEAD:website
```

Expected: no output. Any difference here means the import was not clean.

- [ ] **Step 8: Nothing to commit**

`git subtree add` already committed. Confirm with:

```bash
git status --short
```

Expected: empty. Do not amend the subtree commit — its parents are the point.

---

## Task 2: Rename, rewrite and prune inside `apps/website/`

**Files:**
- Rename: `apps/website/LICENSE.md` → `apps/website/LICENSE`
- Modify: `apps/website/README.md` (full rewrite), `apps/website/package.json:2` (the `name` field), `apps/website/.gitignore`
- Delete: `apps/website/.devcontainer/` (this repository has its own), `apps/website/tmp/Hero.jsx` (stray scratch file)

**Interfaces:**
- Consumes: `apps/website/` from Task 1.
- Produces: `apps/website/LICENSE` at the path Task 3's prose points at, and a `apps/website/` that builds. Task 5's `Makefile` targets depend on `apps/website/package.json` keeping its `dev` and `build` scripts — only the `name` field changes.

- [ ] **Step 1: Rename the licence so the boundary is a conventional filename**

```bash
cd /home/nkr/Projects/shpyrd
git mv apps/website/LICENSE.md apps/website/LICENSE
```

- [ ] **Step 2: Verify the stray file is unreferenced, then prune both paths**

```bash
grep -rn "tmp/Hero\|tmp'" apps/website/src apps/website/next.config.mjs apps/website/jsconfig.json
```

Expected: no output. `apps/website/src/components/Hero.jsx` is the real component and stays; `apps/website/tmp/Hero.jsx` is scratch. If this grep *does* print a match, stop and ask rather than deleting.

```bash
git rm -r --quiet apps/website/.devcontainer apps/website/tmp
```

- [ ] **Step 3: Rename the npm package**

The package is still named after the template it was bought as. In `apps/website/package.json`, change line 2:

```json
  "name": "shpyrd-website",
```

from:

```json
  "name": "tailwindui-syntax",
```

Leave `version`, `private`, every script, and all dependencies exactly as they are.

- [ ] **Step 4: Trim `apps/website/.gitignore` to what the root does not already cover**

The root `.gitignore` already ignores `node_modules`, `coverage`, `*.pem` and `.DS_Store` repository-wide, so the imported file is mostly duplication. Replace the whole contents of `apps/website/.gitignore` with:

```gitignore
# Next.js build output
/.next/
/out/

# Vercel
.vercel

# local env files
.env*.local
```

- [ ] **Step 5: Verify the served assets survived the move byte-for-byte**

This is Review Focus items 2 and 4. Compare against the source commit rather than trusting the import:

```bash
git diff --stat website-src/main:public HEAD:apps/website/public
sha256sum apps/website/public/install.sh
git show website-src/main:public/install.sh | sha256sum
ls apps/website/public/screenshots | wc -l
ls apps/website/public/fonts | wc -l
file apps/website/public/fonts/Inter-roman.var.woff2
```

Expected: the `git diff --stat` prints nothing; the two checksums match; `18` screenshots; `4` font files; and `file` reports `Web Open Font Format (Version 2)`, not ASCII text.

- [ ] **Step 6: Verify the site still installs, lints and builds — from inside `apps/website/`**

This is Review Focus item 1: the `cd` is load-bearing, not incidental.

```bash
cd apps/website
npm ci --no-audit --no-fund
npm run lint
npm run build
```

Expected: `lint` exits 0 (it prints a `caniuse-lite is outdated` notice, a `scrollRestoration` experimental warning, and one `import/no-anonymous-default-export` warning in `src/markdoc/search.mjs` — all pre-existing and all warnings). `build` exits 0 and its route table lists `/` (as `┌ ● /`), `/404` and 22 `/docs/*` routes — 24 route lines in total.

- [ ] **Step 7: Verify the search index is populated, not silently empty**

The build succeeds either way, so assert on the artifact:

```bash
cd /home/nkr/Projects/shpyrd/website
grep -ro 'docs/installation' .next/static/chunks | head -1
```

Expected: a match. No match means `search.mjs` globbed an empty directory — check that Step 6 ran with `apps/website/` as the working directory.

- [ ] **Step 8: Rewrite `apps/website/README.md`**

It is currently the Tailwind *Syntax* template's own README ("# Syntax", "You can start editing this template..."). Replace the whole file with:

```markdown
# shpyrd.io

The source of [shpyrd.io](https://shpyrd.io): the marketing homepage and the
product documentation. Built with [Next.js](https://nextjs.org) and
[Markdoc](https://markdoc.io), deployed on Vercel from this directory.

## Licence

**This directory is not covered by the repository's MPL 2.0 licence.** The site
is built on the commercial Tailwind UI *Syntax* template and is governed by
[`LICENSE`](LICENSE) in this directory. The rest of the repository is
[MPL-2.0](../LICENSE).

In practice: you may read it, and contribute documentation and content changes
to it, but you may not redistribute the template or derivatives of it separately
from this site.

## Running it

```sh
npm install
npm run dev      # http://localhost:3000
```

Or from the repository root: `make website-dev`.

Every command must run with this directory as the working directory.
`src/markdoc/search.mjs` resolves `./src/pages` from the process working
directory, so a build started from the repository root produces an empty search
index without failing.

## Layout

```
src/pages/index.md        the homepage
src/pages/docs/*.md       the documentation, one file per page, URL follows the filename
src/pages/_app.jsx        the page shell and the documentation navigation tree
src/components/           layout, navigation, search, callouts, code fences
src/markdoc/              Markdoc tags, nodes, and the search index builder
src/styles/               Tailwind entrypoint, fonts, syntax highlighting
public/install.sh         served at shpyrd.io/install.sh, the documented install path
public/screenshots/       screenshots used by the docs and the root README
```

## Adding a documentation page

Add `src/pages/docs/<slug>.md` with `title` and `description` front matter, then
add it to the navigation tree in `src/pages/_app.jsx`. The search index picks it
up automatically on the next build.

## URLs that must not move

The root `README.md`, the install instructions and published release notes link
to `/install.sh`, `/screenshots/*.png` and `/docs/*`. Add a redirect in
`next.config.mjs` if a path has to change.
```

- [ ] **Step 9: Verify the build is unaffected by the README rewrite and commit**

```bash
cd /home/nkr/Projects/shpyrd/website && npm run build 2>&1 | tail -3
cd /home/nkr/Projects/shpyrd
git add -A website
git status --short
```

Expected: the build exits 0. `git status` shows the rename, the two deletions, and the three modified files — and **nothing under `apps/website/src/` or `apps/website/public/`**. If it shows a change under either, revert that file: this task may not touch served content.

```bash
git commit -s -m "chore(website): name the package, prune template scratch, rewrite the README

The site arrives from shpyrd-docs as the Tailwind UI Syntax template it was
bought as: the package is named tailwindui-syntax, the README documents the
template rather than shpyrd.io, and .devcontainer/ and tmp/Hero.jsx are
leftovers this repository has no use for.

LICENSE.md becomes LICENSE so the licensing boundary sits at a conventional
filename; its text is unchanged. .gitignore keeps only what the root does not
already cover.

Nothing under src/ or public/ changes, so every URL shpyrd.io serves is
byte-identical."
```

---

## Task 3: Declare the licensing boundary

**Files:**
- Modify: `README.md:354-356` (the `## License` section), `CONTRIBUTING.md:3-4`
- Never modify: `LICENSE`

**Interfaces:**
- Consumes: `apps/website/LICENSE` from Task 2.
- Produces: the prose boundary. Task 5 edits different sections of the same `README.md`, so if these tasks are done out of order, expect to re-read the file rather than apply a stale line range.

- [ ] **Step 1: Verify the root licence is untouched**

```bash
cd /home/nkr/Projects/shpyrd
git diff main -- LICENSE
head -2 LICENSE
```

Expected: no diff, and the file still opens with `Mozilla Public License Version 2.0`. The boundary is declared in prose precisely so this file never changes.

- [ ] **Step 2: Replace the README's License section**

`README.md` currently ends with:

```markdown
## License

[MPL-2.0](LICENSE)
```

Replace that section with:

```markdown
## License

[MPL-2.0](LICENSE), with one exception: `apps/website/` is built on the commercial
Tailwind UI *Syntax* template and is governed by
[`apps/website/LICENSE`](apps/website/LICENSE) instead. Everything else — the platform,
the CLI, the dashboard, the examples and the RFCs — is MPL-2.0.
```

- [ ] **Step 3: Qualify the blanket claim in CONTRIBUTING.md**

`CONTRIBUTING.md` opens by calling the whole project MPL 2.0, which is no longer accurate. Replace:

```markdown
shpyrd is [MPL 2.0 licensed](LICENSE) and accepts contributions via GitHub
pull requests. This document outlines some of the conventions to make it
easier to get your contribution accepted.
```

with:

```markdown
shpyrd is [MPL 2.0 licensed](LICENSE) and accepts contributions via GitHub
pull requests. This document outlines some of the conventions to make it
easier to get your contribution accepted.

One directory is licensed differently: the website in [`apps/website/`](website) is
built on the commercial Tailwind UI *Syntax* template and is governed by
[`apps/website/LICENSE`](apps/website/LICENSE). Documentation and content contributions
to the site are welcome on those terms.
```

- [ ] **Step 4: Verify every licence claim in the tree agrees**

```bash
grep -rn "MPL" README.md CONTRIBUTING.md | grep -v "^README.md:11"
ls apps/website/LICENSE
head -1 apps/website/LICENSE
```

Expected: both MPL mentions now carry the `apps/website/` exception, `apps/website/LICENSE` exists, and its first line is `# Tailwind UI License`.

- [ ] **Step 5: Commit**

```bash
git add README.md CONTRIBUTING.md
git commit -s -m "docs: scope MPL 2.0 to everything but apps/website/

The site is built on the commercial Tailwind UI Syntax template, whose licence
forbids redistributing the template separately from the product it is part of.
The repository's MPL 2.0 grant would purport to allow exactly that, so the
boundary is stated where a reader meets the licence claim: the README's License
section and CONTRIBUTING's opening paragraph.

The LICENSE file itself is deliberately untouched, so its MPL text stays
byte-identical and licence detection keeps working. apps/website/LICENSE holds the
Tailwind terms, the same slot enterprise/LICENSE will occupy later."
```

---

## Task 4: Split CI so each project builds on its own changes

**Files:**
- Create: `.github/workflows/website.yml`
- Modify: `.github/workflows/ci.yml:6-9` (the `on:` block)

**Interfaces:**
- Consumes: `apps/website/package-lock.json` and the `lint`/`build` scripts from Task 2.
- Produces: two independent workflows. No job in either depends on the other.

- [ ] **Step 1: Confirm no status check is required to merge**

The whole design rests on this, so check rather than assume:

```bash
gh api repos/shpyrd-io/shpyrd/branches/main/protection 2>&1 | head -2
gh api repos/shpyrd-io/shpyrd/rulesets
```

Expected: `Branch not protected` and `[]`. If either has changed since 2026-09-27, **stop** — `paths-ignore` would start blocking merges on documentation-only pull requests, and the design needs a `dorny/paths-filter` gate job instead.

- [ ] **Step 2: Stop the Go suite from running on prose-only changes**

In `.github/workflows/ci.yml`, replace the `on:` block:

```yaml
on:
  push:
    branches: [main]
  pull_request:
```

with:

```yaml
on:
  push:
    branches: [main]
    # Repeated under pull_request below: GitHub Actions does not support YAML
    # anchors, so the two lists have to be kept in sync by hand.
    paths-ignore:
      - 'apps/website/**'
      - 'rfcs/**'
      - 'docs/**'
      - '*.md'
  pull_request:
    paths-ignore:
      - 'apps/website/**'
      - 'rfcs/**'
      - 'docs/**'
      - '*.md'
```

GitHub skips the workflow only when *every* changed path matches an ignore pattern, so a Go change that also touches a README still runs the full suite.

Do not try to deduplicate these with a YAML anchor (`&prose` / `*prose`). Anchors are valid YAML and `yaml.safe_load` expands them happily, but GitHub's workflow parser rejects them — so the mistake passes every local check and only surfaces as a broken workflow after the push.

- [ ] **Step 3: Verify the workflow parses and both filters match**

```bash
cd /home/nkr/Projects/shpyrd
python3 -c "
import yaml
d = yaml.safe_load(open('.github/workflows/ci.yml'))
on = d[True] if True in d else d['on']
push, pr = on['push']['paths-ignore'], on['pull_request']['paths-ignore']
print('push :', push)
print('pr   :', pr)
print('match:', push == pr)
print('jobs :', list(d['jobs']))
"
grep -c '&prose\|\*prose' .github/workflows/ci.yml
```

Expected: both lists print as the same four patterns, `match: True`, jobs `['go', 'ui', 'e2e']`, and the `grep -c` prints `0` — no anchors.

- [ ] **Step 4: Add the website workflow**

Create `.github/workflows/website.yml`, mirroring the existing `ui` job. There is no test script in this project, so there is no test step.

```yaml
# The shpyrd.io website (Next.js + Markdoc) lives in apps/website/ and deploys from
# there on Vercel. This job is the pre-merge check; the Vercel preview is the
# visual one.
name: website

on:
  push:
    branches: [main]
    # Repeated under pull_request below: GitHub Actions does not support YAML
    # anchors, so the two lists have to be kept in sync by hand.
    paths:
      - 'apps/website/**'
      - '.github/workflows/website.yml'
  pull_request:
    paths:
      - 'apps/website/**'
      - '.github/workflows/website.yml'

permissions:
  contents: read

concurrency:
  group: website-${{ github.ref }}
  cancel-in-progress: true

jobs:
  build:
    name: Build
    runs-on: ubuntu-latest
    defaults:
      run:
        # Load-bearing: src/markdoc/search.mjs resolves ./src/pages from the
        # process working directory, so a build from the repository root
        # produces an empty search index without failing.
        working-directory: apps/website
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: 24
          cache: npm
          cache-dependency-path: apps/website/package-lock.json
      - run: npm ci --no-audit --no-fund
      - run: npm run lint
      - run: npm run build
```

- [ ] **Step 5: Verify the new workflow parses and its filters are right**

```bash
python3 -c "
import yaml
d = yaml.safe_load(open('.github/workflows/website.yml'))
on = d[True] if True in d else d['on']
print('push :', on['push']['paths'])
print('pr   :', on['pull_request']['paths'])
j = d['jobs']['build']
print('node :', j['steps'][1]['with']['node-version'])
print('wd   :', j['defaults']['run']['working-directory'])
print('steps:', [s.get('run', s.get('uses')) for s in j['steps']])
"
grep -c '&website\|\*website' .github/workflows/website.yml
```

Expected: both path lists are the two patterns; node is `24`; working directory is `website`; the steps are checkout, setup-node, `npm ci --no-audit --no-fund`, `npm run lint`, `npm run build`; and `grep -c` prints `0`.

- [ ] **Step 6: Record which changes now run which checks**

This is Review Focus item 3 — the consequence is invisible until someone adds branch protection. Verify the filters classify each case as intended:

| A pull request touching | Runs `ci.yml` | Runs `website.yml` |
| --- | --- | --- |
| `pkg/**` only | yes | no |
| `apps/website/**` only | no | yes |
| `rfcs/**` or `*.md` only | no | no |
| `pkg/**` and `README.md` | yes | no |
| `pkg/**` and `apps/website/**` | yes | yes |

Row 3 means a documentation-only pull request reports **no checks at all**. That is intended while `main` is unprotected. Add this note to `.github/workflows/ci.yml` directly under the existing header comment, so whoever enables branch protection finds it:

```yaml
# paths-ignore: prose-only changes (apps/website/, rfcs/, docs/, *.md) run neither
# this workflow nor website.yml, so such a pull request reports no checks at
# all. That is fine while main is unprotected. If a required status check is
# ever added, replace these filters with a dorny/paths-filter gate job that
# always reports, or required checks will block every documentation PR.
```

- [ ] **Step 7: Commit**

```bash
git add .github/workflows/ci.yml .github/workflows/website.yml
git commit -s -m "ci: build the website on its own changes, skip Go CI on prose

Now that the site shares the repository, an unfiltered ci.yml would run the
45-minute kind e2e for a documentation typo, and every Go pull request would
build Next.js. Two path-filtered workflows are simpler than per-job filters,
which would need a paths-filter action and a gate job.

website.yml mirrors the ui job: node 24, npm ci, lint, build, with
working-directory set to website because search.mjs resolves ./src/pages from
the process working directory.

main has no branch protection and no rulesets today, so a prose-only pull
request reporting no checks is harmless; ci.yml carries a comment saying what
to do if that ever changes."
```

---

## Task 5: Wire the site into the repository's own workflow

**Files:**
- Modify: `Makefile:6-7` (the `.PHONY` line) and after `Makefile:28` (after the `ui:` target), `README.md` (the `## Layout` and `## Developing` sections)

**Interfaces:**
- Consumes: `apps/website/package.json`'s `dev` and `build` scripts from Task 2.
- Produces: `make website` and `make website-dev`, referenced by `apps/website/README.md` from Task 2.

- [ ] **Step 1: Add the Make targets next to the existing `ui:` target**

In `Makefile`, extend the `.PHONY` list — replace:

```make
.PHONY: all build cli server ui image dev-image generate test vet lint clean \
        dev-cluster dev-load dev-deploy dev-destroy installclint commitlint
```

with:

```make
.PHONY: all build cli server ui website website-dev image dev-image generate \
        test vet lint clean dev-cluster dev-load dev-deploy dev-destroy \
        installclint commitlint
```

Then insert immediately after the existing `ui:` target (after the line `cd ui && npm ci --no-audit --no-fund && npm run build`):

```make
## Build the website (shpyrd.io); deployed from apps/website/ on Vercel
website:
	cd apps/website && npm ci --no-audit --no-fund && npm run build

## Serve the website locally on http://localhost:3000
website-dev:
	cd apps/website && npm install && npm run dev
```

The `cd` is what makes the search index build correctly — see the constraint at the top of this plan.

- [ ] **Step 2: Add `apps/website/` to the README's Layout section**

In the `## Layout` code block, insert a row after the `ui/` row so the two front ends sit together:

```
ui/                   dashboard (Vite + React 19 + Tailwind 4 + shadcn/ui)
apps/website/              shpyrd.io: marketing homepage and docs (Next.js + Markdoc); licensed separately, see apps/website/LICENSE
```

- [ ] **Step 3: Tell contributors how to run the site, in the Developing section**

In `## Developing`, after the paragraph beginning "The UI can be developed against a local server", add:

```markdown
The website is a separate Next.js project: `make website-dev` serves
[shpyrd.io](https://shpyrd.io) on http://localhost:3000. Documentation pages are
Markdown under `apps/website/src/pages/docs/`; see
[apps/website/README.md](apps/website/README.md).
```

Then update the CI sentence in the same section. Replace:

```markdown
CI runs `go vet`, `go test`, the dashboard lint, tests and build, and an
end-to-end job on a kind cluster (`.github/workflows/ci.yml`). A tag `vX.Y.Z` releases:
```

with:

```markdown
CI runs `go vet`, `go test`, the dashboard lint, tests and build, and an
end-to-end job on a kind cluster (`.github/workflows/ci.yml`); the website
builds separately (`.github/workflows/website.yml`). Each workflow is filtered
to its own paths, so a documentation change does not run the end-to-end job.
A tag `vX.Y.Z` releases:
```

- [ ] **Step 4: Verify both Make targets work**

`make website` is the one that has to be right, since it is the reproducible build. This also re-checks Review Focus item 1 through a second entry point:

```bash
cd /home/nkr/Projects/shpyrd
make website 2>&1 | tail -5
grep -ro 'docs/installation' apps/website/.next/static/chunks | head -1
make -n website-dev
```

Expected: `make website` exits 0 with the Next route table; the `grep` finds a match, proving the search index is populated; `make -n website-dev` prints the `cd apps/website && npm install && npm run dev` command without running it.

- [ ] **Step 5: Verify the README's own links resolve**

```bash
for p in apps/website/LICENSE apps/website/README.md LICENSE CONTRIBUTING.md; do
  test -e "$p" && echo "ok   $p" || echo "MISSING $p"
done
```

Expected: four `ok` lines.

- [ ] **Step 6: Commit**

```bash
git add Makefile README.md
git commit -s -m "chore: make targets and README for the website in-tree

make website builds the site the way CI does and make website-dev serves it,
both cd-ing into apps/website/ because search.mjs resolves ./src/pages from the
process working directory.

The README's Layout section gains a apps/website/ row next to ui/ noting the
separate licence, and Developing says how to run the site and that the two
workflows are path-filtered."
```

---

## Task 6: Verify the whole move, then open the pull request

**Files:**
- Modify: nothing. This task only verifies and publishes.

**Interfaces:**
- Consumes: every preceding task.
- Produces: a pull request that must be merged with a merge commit.

- [ ] **Step 1: Review the full diff against `main` for anything unintended**

```bash
cd /home/nkr/Projects/shpyrd
git diff --stat main...HEAD -- . ':!website'
```

Expected: exactly `.github/workflows/ci.yml`, `.github/workflows/website.yml`, `CONTRIBUTING.md`, `Makefile`, `README.md`, and the two `docs/superpowers/` documents. Anything else is out of scope for this move.

- [ ] **Step 2: Verify no served content changed**

```bash
git diff --stat website-src/main:src HEAD:apps/website/src
git diff --stat website-src/main:public HEAD:apps/website/public
```

Expected: no output from either. This is the lift-and-shift guarantee, and it is the single most important check in the plan.

- [ ] **Step 3: Verify the route list is complete**

```bash
cd apps/website && npm run build 2>&1 > /tmp/rb.log
grep -cE '^[├└] ● /docs/' /tmp/rb.log      # docs pages
grep -cE '^[┌├└] [●○]' /tmp/rb.log         # all routes
grep -E '^┌ ● /  |^├ ○ /404' /tmp/rb.log
```

Expected: `22` docs routes and `24` route lines in total (22 docs + `/` + `/404`). Next prints the homepage as `┌ ● /` (SSG) and the 404 as `├ ○ /404`, both with column padding — an assertion anchored with `$` straight after the path matches neither.

- [ ] **Step 4: Verify the history survived every subsequent commit**

```bash
cd /home/nkr/Projects/shpyrd
git cat-file -p $(git rev-list --merges -1 HEAD) | grep -c '^parent '
git log --oneline HEAD^2 2>/dev/null | wc -l || git log --oneline $(git rev-list --merges -1 HEAD)^2 | wc -l
git blame --porcelain apps/website/src/pages/docs/cli.md | grep -c '^author Patrick Negri'
```

Expected: `2` parents, ~100 commits reachable through the merge's second parent, and blame still crediting the original author. Do **not** assert on `git log --oneline -- apps/website/`: it prints 1, because path-limiting matches paths as each commit spells them and the pre-move commits used `src/pages/...` at the repository root.

- [ ] **Step 5: Confirm before pushing**

Pushing publishes this to a public repository. Report to the human partner:
- the six commits on the branch and what each does,
- that no served content changed (Step 2),
- that `main` has not been touched.

Then ask whether to push and open the pull request. **Wait for an explicit yes.**

- [ ] **Step 6: Push the branch and open the pull request**

```bash
git push -u origin chore/website-monorepo
gh pr create --repo shpyrd-io/shpyrd --base main --head chore/website-monorepo \
  --title "chore(website): move shpyrd.io into the monorepo" --body "$(cat <<'BODY'
Moves the site from `shpyrd-io/shpyrd-docs` into `apps/website/`, the first step of
the [repository reorganization proposal](https://gist.github.com/paezao/c078584b5946b409dd58b0c13c84bdb6).
Design: `docs/superpowers/specs/2026-09-27-website-monorepo-design.md`.

## ⚠️ Merge this with a merge commit, not a squash

The site arrives through `git subtree add`, so this pull request has a merge
commit with two parents. Squashing discards the second parent, which is the only
thing making the site's ~100 pre-move commits reachable, and `git blame` would
then credit the move instead of the authors. Every later pull request keeps the
repository's squash convention.

## What this does

- brings `apps/website/` in with its 102 commits of history intact
- renames `LICENSE.md` to `LICENSE`, text unchanged, and declares the boundary
  in the root `README.md` and `CONTRIBUTING.md`: MPL 2.0 everywhere except
  `apps/website/`, which is governed by the commercial Tailwind UI licence
- renames the npm package from `tailwindui-syntax` to `shpyrd-website`, rewrites
  the template's README, drops `.devcontainer/` and a stray `tmp/Hero.jsx`
- splits CI: `website.yml` builds the site on `apps/website/**`; `ci.yml` now ignores
  prose paths, so a docs typo no longer runs the 45-minute kind e2e
- adds `make website` and `make website-dev`

**Nothing under `apps/website/src/` or `apps/website/public/` changed**, so `/`, the 22
`/docs/*` pages, `/install.sh` and `/screenshots/*.png` serve byte-identical
content.

## Still to do after this merges

The Vercel cutover, which needs dashboard access: a project against this
repository with Root Directory `apps/website`, Ignored Build Step
`git diff --quiet HEAD^ HEAD ./`, verified on its `*.vercel.app` URL before
`shpyrd.io` and `www.shpyrd.io` move over. `shpyrd-docs` then gets a pointer in
its README and its git integration disconnected. It is not deleted or archived.

🤖 Generated with [Claude Code](https://claude.com/claude-code)

https://claude.ai/code/session_01CFQobmze26mnGVmyUF8oVp
BODY
)"
```

- [ ] **Step 7: Verify CI classified the pull request correctly**

```bash
sleep 45 && gh pr checks --repo shpyrd-io/shpyrd chore/website-monorepo
```

Expected: **both** workflows run. `website.yml` is triggered by `apps/website/**`; `ci.yml` is triggered by `.github/workflows/*.yml` and `Makefile`, neither of which matches a `paths-ignore` pattern — correct, since both affect the Go build. The filters themselves are proven by simulating GitHub's matching rules in Task 4 Step 6, not by this one pull request.

- [ ] **Step 8: Hand the Vercel cutover over**

Report the pull request URL and restate what only the maintainer can do, in order:

1. merge this pull request **with a merge commit**, not a squash;
2. the four Vercel steps from the spec's "Vercel cutover" section — new project, Root Directory `apps/website`, Ignored Build Step `git diff --quiet HEAD^ HEAD ./`, verify on `*.vercel.app`, then move `shpyrd.io` and `www.shpyrd.io`.

Do not attempt either. Then name the follow-up pull request this move makes necessary, which is deliberately **not** part of this one because the lift-and-shift constraint forbids touching `apps/website/src/`:

- `apps/website/src/pages/docs/how-to-contribute.md` line 6 tells readers the project is "MPL-2.0" with no exception, and line 48 says "This site lives in shpyrd-io/shpyrd-docs". Line 48 becomes false the moment this merges, and line 6 is the published page a prospective contributor or redistributor is most likely to read — it must name `shpyrd-io/shpyrd` + `apps/website/` and state the `apps/website/` exception. This should be the **next** pull request, not held for the domain cutover.

Then note the follow-up that is deliberately deferred until after the domain moves: `shpyrd-docs` gets a README banner pointing at `shpyrd-io/shpyrd/website` and its Vercel git integration disconnected. Adding that banner while the old project still serves the live domain would advertise a move that has not happened yet, so it waits. The repository is not deleted and not archived; whether to archive it is a separate, later decision.

---

## Out of scope

Per the spec, each of these needs its own design: the proposal's information architecture and Cloud-first positioning, `blog/` and `changelog/`, `[Cloud]`/`[Enterprise]` doc badges, the Next.js 13.4.2 upgrade, consolidating `examples/` with `shpyrd-examples`, the `docs/` versus `rfcs/` naming clash, `enterprise/`, and anything in `shpyrd-cloud`.
