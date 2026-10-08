# @shpyrd/ui as a published package: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Publish `design/ui` to npm as `@shpyrd/ui` with a version of its own, and move `shpyrd-signup` from its copy of the library to the package, after giving the library its `CodeInput`.

**Architecture:** The library stays in `design/ui` and keeps shipping TypeScript source. A workflow publishes it when the version in `design/ui/package.json` changes on `main`. Two scripts in `design/ui/scripts` check what a release ships and what an application outside the workspace sees, and a third decides what a push to `main` releases.

**Tech Stack:** npm workspaces, Next 16, React 19, Tailwind 4, vitest, GitHub Actions, npm trusted publishing (OIDC).

**Spec:** `docs/superpowers/specs/2026-10-08-design-ui-package-design.md`

This plan covers cycles 0 (the package) and 1 (signup) of the spec. Cycles 2 (legal), 3 (billing) and 4 (shpyrd-cloud) get a plan each when they start: what moves into the library is decided in each cycle's design session, and cannot be written down before it.

## Global Constraints

- Package name `@shpyrd/ui`, public on npm, licence MPL-2.0.
- The version lives in `design/ui/package.json`. `0.0.0` means "never released": the release workflow never publishes it. The first published version is `0.1.0`.
- Before 1.0: a change that breaks a component is a minor version, anything else a patch. Consumers depend on `^0.x.y`.
- Git tags of the library are `ui-v<version>`; the core's tags stay `v*`.
- `react`, `react-dom`, `next` and `tailwindcss` are `peerDependencies` (`^19.0.0`, `^19.0.0`, `^16.0.0`, `^4.0.0`), and stay in `devDependencies` for the gallery.
- What ships: `src/` without tests and without `src/test.ts`, plus `README.md`, `CHANGELOG.md`, `LICENSE`.
- The library's rules hold (`design/DESIGN_FOR_AGENT.md`, "Rules of the library"): relative imports inside `src/`, `"use client"` on components with state, a gallery page and a catalog line for every component, tests named as sentences.
- Components export with `export { Name }` at the end of the file, as 69 of the library's files do.
- Brand name in text is always lowercase: shpyrd.

## Review Focus

1. **A push to `main` that does not change the version.** Expected: the release workflow publishes nothing and passes. Pinned in Task 4 (`releasePlan` with a published version).
2. **The placeholder version `0.0.0` reaching `main`.** Expected: nothing is published. Pinned in Task 4.
3. **A version with no CHANGELOG section.** Expected: the release fails before publishing, rather than publishing with empty notes. Pinned in Task 4.
4. **npm not answering when the workflow asks what is published.** Expected: the release fails, rather than guessing "not published" and trying to publish blind. Pinned in Task 4 (`publishedVersions` rethrows anything but a 404).
5. **An application outside the workspace where Tailwind does not see the library's classes.** Expected: the consumer check fails, not a build that passes with unstyled components. Pinned in Task 2 (the check asserts a library-only class, `rounded-control`, is in the built CSS).

---

## Cycle 0: the package (repository `shpyrd`)

Work on a branch from `main`, for example `feat/ui-package`. Run every command from the repository root unless a step says otherwise.

### Task 1: The package's shape, and the check of what it ships

**Files:**
- Create: `design/ui/scripts/check-pack.mjs`
- Create: `design/ui/README.md`
- Create: `design/ui/CHANGELOG.md`
- Create: `design/ui/LICENSE` (a copy of the root `LICENSE`)
- Modify: `design/ui/package.json`
- Modify: `package-lock.json` (by `npm install`)

**Interfaces:**
- Produces: `node design/ui/scripts/check-pack.mjs`, run with `design/ui` as working directory; exit 0 when the packed file list is right, exit 1 and a list of problems otherwise. Task 3 runs it in CI.

- [ ] **Step 1: Write the check of what ships**

`design/ui/scripts/check-pack.mjs`:

```js
// What npm would publish of @shpyrd/ui: the components, the styles and the
// papers; nothing of the gallery, the tests, the scripts or the
// configuration. Run in design/ui.
import { execFileSync } from "node:child_process";

const [pack] = JSON.parse(execFileSync("npm", ["pack", "--dry-run", "--json"], { encoding: "utf8" }));
const files = pack.files.map((f) => f.path);

const forbidden = files.filter((f) =>
  /\.test\.tsx?$|^src\/test\.ts$|^app\/|^public\/|^scripts\/|^next\.config|^vitest\.config|^tsconfig|^postcss\.config|^next-env\.d\.ts$|\.tsbuildinfo$/.test(f),
);
const required = ["package.json", "README.md", "CHANGELOG.md", "LICENSE", "src/styles/index.css", "src/components/button.tsx"];
const missing = required.filter((f) => !files.includes(f));

if (forbidden.length || missing.length) {
  for (const f of forbidden) console.error(`ships but must not: ${f}`);
  for (const f of missing) console.error(`must ship but does not: ${f}`);
  process.exit(1);
}
console.log(`${pack.name} would ship ${files.length} files, none of them the gallery's, a test or configuration`);
```

- [ ] **Step 2: Run it to see it fail**

Run: `cd design/ui && node scripts/check-pack.mjs; cd ../..`
Expected: exit 1, with `ships but must not:` lines for `app/…`, `*.test.tsx` and `tsconfig.json`, and `must ship but does not:` lines for `README.md`, `CHANGELOG.md` and `LICENSE`.

- [ ] **Step 3: Give the package its shape**

In `design/ui/package.json`:
- remove `"private": true`, and keep `"version": "0.0.0"`;
- add, after `"type": "module"`:

```json
  "description": "The design system of shpyrd: its components, tokens and styles, as React source for Next applications.",
  "license": "MPL-2.0",
  "repository": { "type": "git", "url": "git+https://github.com/shpyrd-io/shpyrd.git", "directory": "design/ui" },
  "homepage": "https://github.com/shpyrd-io/shpyrd/tree/main/design/ui#readme",
  "files": ["src", "!src/**/*.test.ts", "!src/**/*.test.tsx", "!src/test.ts", "README.md", "CHANGELOG.md", "LICENSE"],
  "publishConfig": { "access": "public" },
```

- move `next`, `react`, `react-dom` and `tailwindcss` from `dependencies` to `devDependencies` (same ranges as today), and move `@tailwindcss/postcss` to `devDependencies` too: an application configures PostCSS itself;
- add:

```json
  "peerDependencies": {
    "next": "^16.0.0",
    "react": "^19.0.0",
    "react-dom": "^19.0.0",
    "tailwindcss": "^4.0.0"
  },
```

Copy the licence: `cp LICENSE design/ui/LICENSE`.

`design/ui/CHANGELOG.md`:

```markdown
# Changelog of @shpyrd/ui

A section per version, newest first. For a version that breaks something
(a minor version before 1.0), the section says what an application has to
change.
```

`design/ui/README.md`:

````markdown
# @shpyrd/ui

The design system of shpyrd: its components, its tokens and its styles, as
React source for Next applications. The gallery that shows every component
is this folder's Next application (`npm run dev --workspace design/ui`).

## In an application of another repository

```sh
npm install @shpyrd/ui
```

The package is TypeScript source, and Tailwind reads its classes from it, so
an application needs two things:

```ts
// next.config.ts
const config = { transpilePackages: ["@shpyrd/ui"] };
export default config;
```

```css
/* the application's global stylesheet */
@import "@shpyrd/ui/styles.css";
```

Then: `import { Button } from "@shpyrd/ui/components/button";`.

It needs React 19, Next 16 and Tailwind 4 in the application
(`peerDependencies`).

## In this repository

The applications of this repository use the library through the npm
workspace (`"@shpyrd/ui": "*"`): they always see it as it is in the
working tree. How the library is worked on is in
`design/DESIGN_FOR_AGENT.md`.

## Versions

The version is the one in this folder's `package.json`. Before 1.0, a
change that breaks a component (a prop renamed or removed, a component
removed, a behaviour a screen relies on changed) is a minor version
(`0.2.0` to `0.3.0`); anything else, a patch. Applications depend on
`^0.x.y`, which takes patches only.

`0.0.0` is the version of a library never released.

## Releasing

1. In the pull request that changes the library, raise the version in
   `package.json` and add its section to `CHANGELOG.md`.
2. Merge it. On `main`, `.github/workflows/ui-release.yml` sees a version
   npm does not have, runs the library's lint, type-check and tests,
   publishes it, tags the commit `ui-v<version>` and makes a GitHub release
   with the version's section.

A version with a suffix (`0.4.0-rc.1`) is published under the `next`
dist-tag, never `latest`.
````

- [ ] **Step 4: Refresh the lockfile**

Run: `npm install --no-audit --no-fund`
Expected: exit 0; `git diff --stat package-lock.json` shows the `design/ui` entry changed (peers and dev dependencies), and nothing removed from the applications.

- [ ] **Step 5: Run the check again**

Run: `cd design/ui && node scripts/check-pack.mjs; cd ../..`
Expected: exit 0, `@shpyrd/ui would ship N files, …`.

- [ ] **Step 6: Run the library's and the applications' gates**

Run: `npm run lint --workspace design/ui --workspace apps/shared --workspace apps/console --workspace apps/workspace && npm run typecheck --workspace design/ui --workspace apps/shared --workspace apps/console --workspace apps/workspace && npm run test --workspace design/ui --workspace apps/shared --workspace apps/console --workspace apps/workspace`
Expected: all pass: moving the peers changed nothing the workspace resolves.

- [ ] **Step 7: Commit**

```bash
git add design/ui/package.json design/ui/scripts/check-pack.mjs design/ui/README.md design/ui/CHANGELOG.md design/ui/LICENSE package-lock.json
git commit -m "feat(ui): @shpyrd/ui is a package: what it ships, its peers, its papers"
```

### Task 2: The check of what an application outside the workspace sees

**Files:**
- Create: `design/ui/scripts/check-consumer.sh`

**Interfaces:**
- Consumes: the package shape of Task 1.
- Produces: `design/ui/scripts/check-consumer.sh` (executable, any working directory); exit 0 when a Next application installing the packed library builds with its styles, non-zero otherwise. Task 3 runs it in CI.

- [ ] **Step 1: Write the check**

`design/ui/scripts/check-consumer.sh`:

```bash
#!/usr/bin/env bash
# What an application of another repository sees of @shpyrd/ui: the packed
# library installed from its tarball into a Next application outside the
# workspace, built. It proves the exports, the peers, the compilation of the
# source and Tailwind's @source from node_modules: a component's class must
# be in the CSS the build writes.
set -euo pipefail

ui=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

tarball=$(cd "$ui" && npm pack --pack-destination "$work" --silent | tail -n 1)

# The versions the library develops against: the same an application uses.
dep() {
  node -e "const p = require('$ui/package.json'); console.log((p.devDependencies || {})['$1'] || (p.dependencies || {})['$1'])"
}

app="$work/app"
mkdir -p "$app/app"
cat > "$app/package.json" <<EOF
{
  "name": "consumer-check",
  "private": true,
  "type": "module",
  "dependencies": {
    "@shpyrd/ui": "file:$work/$tarball",
    "next": "$(dep next)",
    "react": "$(dep react)",
    "react-dom": "$(dep react-dom)",
    "tailwindcss": "$(dep tailwindcss)",
    "@tailwindcss/postcss": "$(dep @tailwindcss/postcss)"
  },
  "devDependencies": {
    "typescript": "$(dep typescript)",
    "@types/node": "$(dep @types/node)",
    "@types/react": "$(dep @types/react)",
    "@types/react-dom": "$(dep @types/react-dom)"
  }
}
EOF
cat > "$app/next.config.ts" <<'EOF'
const config = { transpilePackages: ["@shpyrd/ui"] };
export default config;
EOF
cat > "$app/postcss.config.mjs" <<'EOF'
export default { plugins: { "@tailwindcss/postcss": {} } };
EOF
cat > "$app/app/globals.css" <<'EOF'
@import "@shpyrd/ui/styles.css";
EOF
cat > "$app/app/layout.tsx" <<'EOF'
import "./globals.css";

export default function Layout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
EOF
cat > "$app/app/page.tsx" <<'EOF'
import { Button } from "@shpyrd/ui/components/button";

export default function Page() {
  return <Button>Deploy</Button>;
}
EOF

cd "$app"
npm install --no-audit --no-fund --loglevel=error
NEXT_TELEMETRY_DISABLED=1 npx next build

# rounded-control is the library's own radius, used by Button: present only
# when Tailwind read the library's source in node_modules.
if ! grep -rqs "rounded-control" .next/static; then
  echo "the build has no rounded-control: Tailwind did not read @shpyrd/ui's classes" >&2
  exit 1
fi
echo "an application outside the workspace builds @shpyrd/ui with its styles"
```

Run: `chmod +x design/ui/scripts/check-consumer.sh`

- [ ] **Step 2: Run it**

Run: `design/ui/scripts/check-consumer.sh`
Expected: exit 0, ending with `an application outside the workspace builds @shpyrd/ui with its styles`. (It downloads Next and React: a minute or two.)

- [ ] **Step 3: See it catch a library whose classes Tailwind cannot find**

Temporarily remove the line `@source "../";` from `design/ui/src/styles/index.css`, run `design/ui/scripts/check-consumer.sh`, and expect exit 1 with `the build has no rounded-control: …`. Then restore it: `git checkout design/ui/src/styles/index.css`, and run the check once more to see exit 0.

- [ ] **Step 4: Commit**

```bash
git add design/ui/scripts/check-consumer.sh
git commit -m "test(ui): an application outside the workspace builds the packed library with its styles"
```

### Task 3: The checks in CI, and a warning for a forgotten version

**Files:**
- Modify: `.github/workflows/ci.yml` (the `changes` job's `diff` step, and the `ui` job)

**Interfaces:**
- Consumes: `design/ui/scripts/check-pack.mjs` (Task 1), `design/ui/scripts/check-consumer.sh` (Task 2).

- [ ] **Step 1: Run both checks in the Applications job**

In `.github/workflows/ci.yml`, job `ui` ("Applications"), after the `npm run test …` step and before `- run: make ui`, add:

```yaml
      - name: What @shpyrd/ui would ship
        working-directory: design/ui
        run: node scripts/check-pack.mjs
      - name: What an application of another repository sees of @shpyrd/ui
        run: design/ui/scripts/check-consumer.sh
```

- [ ] **Step 2: Warn when the library changes without a new version**

In the `changes` job, step `diff`, after the two `echo "…" >> "$GITHUB_OUTPUT"` lines at its end, add:

```bash
          # The library reaches the other projects only through a version
          # (design/ui/README.md): a change to its source without one is
          # worth a word, not a failure, since not every change is released
          # at once.
          if git diff --no-renames --name-only "$BASE" HEAD -- design/ui/src \
              | grep -vE '\.test\.tsx?$|^design/ui/src/test\.ts$' | grep -q . \
            && ! git diff "$BASE" HEAD -- design/ui/package.json | grep -qE '^[-+] *"version":'; then
            echo "::warning file=design/ui/package.json::design/ui/src changed and the version did not: the other projects get the change only once a version is released (design/ui/README.md)"
          fi
```

- [ ] **Step 3: Check the workflow parses**

Run: `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml')); print('valid')"`
Expected: `valid`.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: check what @shpyrd/ui ships and what an application sees; warn on a change without a version"
```

The CI run of this branch's pull request is the end-to-end check of this task: the two new steps must pass there, and since this branch changes no file under `design/ui/src`, no warning shows.

### Task 4: The release decision, and the release workflow

**Files:**
- Create: `design/ui/scripts/release-plan.mjs`
- Create: `design/ui/scripts/release-plan.test.mjs`
- Modify: `design/ui/vitest.config.ts` (include the scripts' tests)
- Create: `.github/workflows/ui-release.yml`

**Interfaces:**
- Produces, in `design/ui/scripts/release-plan.mjs`:
  - `changelogSection(changelog: string, version: string): string`: the text under `## <version>` up to the next `## `, trimmed; `""` when there is none.
  - `releasePlan({ version: string, published: string[], changelog: string }): { publish: false, reason: string } | { publish: true, version, distTag: "latest" | "next", gitTag: string, notes: string }`; throws when a version to publish has no CHANGELOG section.
  - `publishedVersions(name: string): string[]`: the versions npm has; `[]` for a package npm has never seen (E404); throws on any other failure.
  - Run as a script in `design/ui`: writes `publish`, `version`, `dist_tag`, `git_tag`, `notes_file` to `$GITHUB_OUTPUT`.

- [ ] **Step 1: Write the failing tests**

`design/ui/scripts/release-plan.test.mjs`:

```js
import { describe, expect, it } from "vitest";
import { changelogSection, releasePlan } from "./release-plan.mjs";

const changelog = `# Changelog of @shpyrd/ui

## 0.2.0

CodeInput takes a length.

## 0.1.0

The first published version.
`;

describe("releasePlan", () => {
  it("releases nothing while the version is the unreleased 0.0.0", () => {
    expect(releasePlan({ version: "0.0.0", published: [], changelog }).publish).toBe(false);
  });

  it("releases nothing when npm has the version already", () => {
    const plan = releasePlan({ version: "0.2.0", published: ["0.1.0", "0.2.0"], changelog });
    expect(plan.publish).toBe(false);
    expect(plan.reason).toContain("0.2.0");
  });

  it("releases a new version under latest, tagged ui-v, with its section as notes", () => {
    expect(releasePlan({ version: "0.2.0", published: ["0.1.0"], changelog })).toEqual({
      publish: true,
      version: "0.2.0",
      distTag: "latest",
      gitTag: "ui-v0.2.0",
      notes: "CodeInput takes a length.",
    });
  });

  it("releases a version with a suffix under next, never latest", () => {
    const withRc = `${changelog}\n## 0.3.0-rc.1\n\nA try.\n`;
    expect(releasePlan({ version: "0.3.0-rc.1", published: [], changelog: withRc }).distTag).toBe("next");
  });

  it("refuses to release a version the CHANGELOG says nothing about", () => {
    expect(() => releasePlan({ version: "0.4.0", published: [], changelog })).toThrow(/CHANGELOG\.md has no section for 0\.4\.0/);
  });
});

describe("changelogSection", () => {
  it("is the text of the version's section, up to the next one", () => {
    expect(changelogSection(changelog, "0.1.0")).toBe("The first published version.");
  });

  it("is empty for a version with no section", () => {
    expect(changelogSection(changelog, "9.9.9")).toBe("");
  });
});
```

In `design/ui/vitest.config.ts`, change the `include` line to:

```ts
    include: ["src/**/*.test.{ts,tsx}", "scripts/**/*.test.mjs"],
```

- [ ] **Step 2: Run them to see them fail**

Run: `npm run test --workspace design/ui -- scripts/`
Expected: FAIL, `Failed to load url ./release-plan.mjs` (the module does not exist yet).

- [ ] **Step 3: Write the release decision**

`design/ui/scripts/release-plan.mjs`:

```js
// What a push to main releases of @shpyrd/ui: nothing, or a version with
// its dist-tag, its git tag and its notes (design/ui/README.md,
// "Releasing"). Run as a script in design/ui by ui-release.yml.
import { execFileSync } from "node:child_process";
import { appendFileSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { pathToFileURL } from "node:url";

export function changelogSection(changelog, version) {
  const lines = changelog.split("\n");
  const start = lines.findIndex((l) => l.trim() === `## ${version}`);
  if (start < 0) return "";
  let end = lines.findIndex((l, i) => i > start && l.startsWith("## "));
  if (end < 0) end = lines.length;
  return lines.slice(start + 1, end).join("\n").trim();
}

export function releasePlan({ version, published, changelog }) {
  if (version === "0.0.0") {
    return { publish: false, reason: "0.0.0 is the version of a library never released" };
  }
  if (published.includes(version)) {
    return { publish: false, reason: `${version} is on npm already` };
  }
  const notes = changelogSection(changelog, version);
  if (!notes) {
    throw new Error(`CHANGELOG.md has no section for ${version}: write it before releasing`);
  }
  return { publish: true, version, distTag: version.includes("-") ? "next" : "latest", gitTag: `ui-v${version}`, notes };
}

// The versions npm has of the package: none for a package it has never
// seen. Any other failure stops the release rather than publish blind.
export function publishedVersions(name) {
  try {
    const out = execFileSync("npm", ["view", name, "versions", "--json"], { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] });
    const versions = JSON.parse(out);
    return Array.isArray(versions) ? versions : [versions];
  } catch (err) {
    if (String(err.stderr ?? "").includes("E404")) return [];
    throw err;
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const pkg = JSON.parse(readFileSync("package.json", "utf8"));
  const plan = releasePlan({
    version: pkg.version,
    published: pkg.version === "0.0.0" ? [] : publishedVersions(pkg.name),
    changelog: readFileSync("CHANGELOG.md", "utf8"),
  });
  const out = process.env.GITHUB_OUTPUT;
  if (!plan.publish) {
    console.log(`nothing to release: ${plan.reason}`);
    if (out) appendFileSync(out, "publish=false\n");
  } else {
    const notesFile = join(process.env.RUNNER_TEMP ?? ".", "ui-release-notes.md");
    writeFileSync(notesFile, `${plan.notes}\n`);
    console.log(`releasing ${pkg.name}@${plan.version} under ${plan.distTag}, tagged ${plan.gitTag}`);
    if (out) {
      appendFileSync(out, `publish=true\nversion=${plan.version}\ndist_tag=${plan.distTag}\ngit_tag=${plan.gitTag}\nnotes_file=${notesFile}\n`);
    }
  }
}
```

- [ ] **Step 4: Run the tests to see them pass**

Run: `npm run test --workspace design/ui -- scripts/`
Expected: PASS, 7 tests.

- [ ] **Step 5: Run it as the workflow will, on today's version**

Run: `cd design/ui && node scripts/release-plan.mjs; cd ../..`
Expected: `nothing to release: 0.0.0 is the version of a library never released`, exit 0, and no call to npm.

- [ ] **Step 6: Write the workflow**

> Superseded while executing: a supply-chain review of PR #130 found that this single job ran the dependencies' install scripts (`npm ci`) where an OIDC token and a writing token were available, and installed an unpinned npm. The committed `.github/workflows/ui-release.yml` splits it into a read-only `gates` job and a `publish` job that installs nothing but `npm@11.21.0` and keeps no credentials in its checkout; `design/ui/scripts/release-workflow.test.mjs` holds the workflow to that. The version below is the original, kept as the plan's record.

`.github/workflows/ui-release.yml`:

```yaml
# Publishes @shpyrd/ui when the version in design/ui/package.json is one npm
# does not have (design/ui/README.md, "Releasing"): its gates, npm with
# provenance, the ui-v<version> tag and a GitHub release with the version's
# CHANGELOG section. A push that does not change the version publishes
# nothing.
#
# npm trusts this workflow (trusted publishing, OIDC): no token is stored.
# NPM_TOKEN is only for a first publication, before npm can be told to
# trust the workflow; remove the secret once it is.
name: ui-release

on:
  push:
    branches: [main]
    paths: ["design/ui/**"]

permissions:
  contents: write # the ui-v tag and its GitHub release
  id-token: write # npm trusted publishing and provenance

# One release at a time: a push delivered twice must not publish twice.
concurrency:
  group: ui-release
  cancel-in-progress: false

jobs:
  release:
    name: "@shpyrd/ui to npm"
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version-file: .nvmrc
          cache: npm
          cache-dependency-path: package-lock.json
          registry-url: https://registry.npmjs.org
      - name: What to release
        id: plan
        working-directory: design/ui
        run: node scripts/release-plan.mjs
      - name: The library's gates
        if: steps.plan.outputs.publish == 'true'
        run: |
          npm ci --no-audit --no-fund
          npm run lint --workspace design/ui
          npm run typecheck --workspace design/ui
          npm run test --workspace design/ui
          (cd design/ui && node scripts/check-pack.mjs)
      - name: Publish
        if: steps.plan.outputs.publish == 'true'
        working-directory: design/ui
        env:
          NODE_AUTH_TOKEN: ${{ secrets.NPM_TOKEN }}
        run: |
          npm install --global npm@^11.5.1 # trusted publishing needs it
          npm publish --access public --provenance --tag "${{ steps.plan.outputs.dist_tag }}"
      - name: Tag and GitHub release
        if: steps.plan.outputs.publish == 'true'
        env:
          GH_TOKEN: ${{ github.token }}
          TAG: ${{ steps.plan.outputs.git_tag }}
          VERSION: ${{ steps.plan.outputs.version }}
          NOTES: ${{ steps.plan.outputs.notes_file }}
          PRERELEASE: ${{ steps.plan.outputs.dist_tag == 'next' && '--prerelease' || '' }}
        run: gh release create "$TAG" --target "$GITHUB_SHA" --title "@shpyrd/ui $VERSION" --notes-file "$NOTES" $PRERELEASE
```

- [ ] **Step 7: Check the workflow parses**

Run: `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/ui-release.yml')); print('valid')"`
Expected: `valid`.

- [ ] **Step 8: Commit**

```bash
git add design/ui/scripts/release-plan.mjs design/ui/scripts/release-plan.test.mjs design/ui/vitest.config.ts .github/workflows/ui-release.yml
git commit -m "ci(ui): publish @shpyrd/ui when its version changes on main"
```

### Task 5: The design workflow and the plan learn about versions; the pull request

**Files:**
- Modify: `design/DESIGN_FOR_AGENT.md` ("Finish the design session")
- Modify: `docs/superpowers/plans/2026-09-29-design-content-apps.md` (the "Packages" row)

- [ ] **Step 1: Finishing a session that changed the library releases it**

In `design/DESIGN_FOR_AGENT.md`, "Finish the design session", replace step 3 with:

```markdown
3. **Run the gates** of everything the session touched: `npm run lint`,
   `npm run typecheck`, `npm run test`, `npm run build`. When `design/ui`
   changed, also build every application of this repository that uses it:
   a change there can break a screen elsewhere.
4. **Release the library** when the session changed `design/ui/src`: raise
   the version in `design/ui/package.json` and write its section in
   `design/ui/CHANGELOG.md` (`design/ui/README.md`, "Versions"). The other
   projects that use the library get the change from npm, through the pull
   request their Dependabot opens. A session that changed only the gallery
   or tests releases nothing.
```

and renumber the next step, **Report**, to 5.

- [ ] **Step 2: The plan's "Packages" row**

In `docs/superpowers/plans/2026-09-29-design-content-apps.md`, replace the row

```markdown
| Packages | one npm workspace at the root; `design/ui` is `@shpyrd/ui` |
```

with

```markdown
| Packages | one npm workspace at the root; `design/ui` is `@shpyrd/ui`, also published to npm with a version of its own for the other projects ([its spec](../specs/2026-10-08-design-ui-package-design.md)) |
```

- [ ] **Step 3: Commit, push, open the pull request**

```bash
git add design/DESIGN_FOR_AGENT.md docs/superpowers/plans/2026-09-29-design-content-apps.md
git commit -m "docs(ui): a design session that changes the library releases it"
git push -u origin feat/ui-package
gh pr create --base main --title "feat(ui): @shpyrd/ui as a published package" --body "Cycle 0 of docs/superpowers/specs/2026-10-08-design-ui-package-design.md: the package's shape, the checks of what it ships and what an application sees, the release workflow, and the design workflow's new step. The version stays 0.0.0, so merging publishes nothing; cycle 1 releases 0.1.0."
```

Expected: the pull request's CI passes, including `What @shpyrd/ui would ship` and `What an application of another repository sees of @shpyrd/ui`.

### Owner: before the first publication

These need the owner's npm account; the plan cannot do them:

1. Create the npm organisation **`shpyrd`** (npmjs.com, Add Organization; the free plan allows public packages).
2. Create a granular access token that may publish to the `@shpyrd` scope, and store it as the repository secret **`NPM_TOKEN`** in `shpyrd-io/shpyrd`. npm can only be told to trust a workflow for a package that exists, so the first publication (cycle 1) uses the token.
3. After `0.1.0` is on npm: on npmjs.com, `@shpyrd/ui` → Settings → Trusted publishing, add GitHub Actions with organisation `shpyrd-io`, repository `shpyrd`, workflow `ui-release.yml`. Then delete the `NPM_TOKEN` secret and the token.

---

## Cycle 1: signup

### Task 6: CodeInput in the library, released as 0.1.0 (repository `shpyrd`)

**Files:**
- Create: `design/ui/src/components/code-input.tsx`
- Create: `design/ui/src/components/code-input.test.tsx`
- Create: `design/ui/app/structures/code-input/page.tsx`
- Modify: `design/ui/app/catalog.ts` (the `structures` pages)
- Modify: `design/ui/package.json` (`version`)
- Modify: `design/ui/CHANGELOG.md`

**Interfaces:**
- Produces: `CodeInput` from `@shpyrd/ui/components/code-input`, with signup's props unchanged: `value: string`, `onChange(value: string)`, `onComplete?(value: string)`, `label?(i: number, n: number): string` (default `` `Digit ${i} of ${n}` ``), `name?: string` (default `"code"`), `length?: number` (default 6), `autoFocus?`, `disabled?`, `invalid?`, plus the props of a `div` but `onChange`. Task 7 imports it.

Work on a branch from `main` once cycle 0 is merged, for example `feat/ui-code-input`.

- [ ] **Step 1: Write the failing tests**

`design/ui/src/components/code-input.test.tsx`:

```tsx
import * as React from "react";
import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { CodeInput } from "./code-input";

// The field as a screen uses it: it keeps the value it is given.
function Controlled(props: Partial<React.ComponentProps<typeof CodeInput>>) {
  const [value, setValue] = React.useState(props.value ?? "");
  return (
    <CodeInput
      {...props}
      value={value}
      onChange={(v) => {
        setValue(v);
        props.onChange?.(v);
      }}
    />
  );
}

const boxes = () => screen.getAllByRole<HTMLInputElement>("textbox");

describe("CodeInput", () => {
  it("draws a box per digit, each named for who cannot see", () => {
    render(<Controlled length={4} />);
    expect(boxes()).toHaveLength(4);
    expect(boxes()[0].getAttribute("aria-label")).toBe("Digit 1 of 4");
  });

  it("goes to the next box as a digit is typed", () => {
    render(<Controlled />);
    fireEvent.change(boxes()[0], { target: { value: "4" } });
    expect(boxes()[0].value).toBe("4");
    expect(document.activeElement).toBe(boxes()[1]);
  });

  it("ignores what is not a digit", () => {
    const onChange = vi.fn();
    render(<Controlled onChange={onChange} />);
    fireEvent.change(boxes()[0], { target: { value: "a" } });
    expect(onChange).not.toHaveBeenCalled();
  });

  it("fills every box from a code pasted into any of them, and says it is complete", () => {
    const onComplete = vi.fn();
    render(<Controlled onComplete={onComplete} />);
    fireEvent.paste(boxes()[3], { clipboardData: { getData: () => "12 34-56" } });
    expect(boxes().map((b) => b.value).join("")).toBe("123456");
    expect(onComplete).toHaveBeenCalledTimes(1);
    expect(onComplete).toHaveBeenCalledWith("123456");
  });

  it("goes back a box when an empty one is deleted", () => {
    const onChange = vi.fn();
    render(<Controlled value="12" onChange={onChange} />);
    fireEvent.keyDown(boxes()[2], { key: "Backspace" });
    expect(onChange).toHaveBeenLastCalledWith("1");
    expect(document.activeElement).toBe(boxes()[1]);
  });

  it("names each box after the field, for the form and analytics", () => {
    render(<Controlled name="otp" />);
    expect(boxes()[0].id).toBe("otp-1");
    expect(boxes()[5].getAttribute("name")).toBe("otp-6");
  });

  it("marks every box when the code is wrong", () => {
    render(<Controlled invalid />);
    for (const box of boxes()) expect(box.getAttribute("aria-invalid")).toBe("true");
  });
});
```

- [ ] **Step 2: Run them to see them fail**

Run: `npm run test --workspace design/ui -- code-input`
Expected: FAIL, `Failed to resolve import "./code-input"`.

- [ ] **Step 3: Move the component into the library**

Copy signup's component, changing only the export to the library's style (`export { CodeInput }` at the end):

Run: `cp ../shpyrd-signup/src/screens/code-input.tsx design/ui/src/components/code-input.tsx` (with `shpyrd-signup` cloned next to `shpyrd`).

Then in `design/ui/src/components/code-input.tsx`: change `export function CodeInput({` to `function CodeInput({`, and add at the end of the file:

```tsx
export { CodeInput };
```

The file keeps `"use client"`, imports only `react` and `cn`, and uses only named colours (`border-input`, `foreground`, `destructive`): the library's rules hold as it is.

- [ ] **Step 4: Run the tests to see them pass**

Run: `npm run test --workspace design/ui -- code-input`
Expected: PASS, 7 tests.

- [ ] **Step 5: Give it a page in the gallery**

`design/ui/app/structures/code-input/page.tsx`:

```tsx
"use client";

import * as React from "react";
import { CodeInput } from "@shpyrd/ui/components/code-input";
import { Section } from "../../section";

function Example(props: Partial<React.ComponentProps<typeof CodeInput>>) {
  const [value, setValue] = React.useState(props.value ?? "");
  return <CodeInput {...props} value={value} onChange={setValue} />;
}

export default function Page() {
  return (
    <>
      <Section title="Default">
        <Example />
      </Section>
      <Section title="Four digits">
        <Example length={4} />
      </Section>
      <Section title="A code that was wrong">
        <Example value="123456" invalid />
      </Section>
      <Section title="While it is checked">
        <Example value="123456" disabled />
      </Section>
    </>
  );
}
```

In `design/ui/app/catalog.ts`, in the `structures` pages, after the line of `input`, add:

```ts
      { slug: "code-input", title: "Code input", description: "A short code that was sent, typed or pasted, a digit a box." },
```

Run: `npm run dev --workspace design/ui` and open `http://localhost:4323/structures/code-input`: the four sections draw, typing moves between boxes, light and dark. Stop the server.

- [ ] **Step 6: Release it as 0.1.0**

In `design/ui/package.json`, `"version": "0.0.0"` becomes `"version": "0.1.0"`.

In `design/ui/CHANGELOG.md`, under the introduction, add:

```markdown
## 0.1.0

The first published version: the library as it is in shpyrd, and
`CodeInput` (`@shpyrd/ui/components/code-input`), from shpyrd-signup: a
short code typed or pasted, a digit a box.
```

Run: `cd design/ui && node scripts/release-plan.mjs; cd ../..`
Expected: `releasing @shpyrd/ui@0.1.0 under latest, tagged ui-v0.1.0` (npm answers 404 for a package it has never seen).

- [ ] **Step 7: The gates**

Run: `npm run lint --workspace design/ui && npm run typecheck --workspace design/ui && npm run test --workspace design/ui && (cd design/ui && node scripts/check-pack.mjs) && design/ui/scripts/check-consumer.sh`
Expected: all pass; `check-pack.mjs` lists no test.

- [ ] **Step 8: Commit, push, open the pull request**

```bash
git add design/ui/src/components/code-input.tsx design/ui/src/components/code-input.test.tsx design/ui/app/structures/code-input/page.tsx design/ui/app/catalog.ts design/ui/package.json design/ui/CHANGELOG.md
git commit -m "feat(ui): CodeInput, from shpyrd-signup; @shpyrd/ui 0.1.0"
git push -u origin feat/ui-code-input
gh pr create --base main --title "feat(ui): CodeInput, and @shpyrd/ui 0.1.0" --body "Cycle 1 of docs/superpowers/specs/2026-10-08-design-ui-package-design.md: signup's CodeInput moves into the library, with its tests and gallery page, and the version becomes 0.1.0. Merging publishes it (ui-release.yml); the owner's npm organisation and NPM_TOKEN must exist first."
```

- [ ] **Step 9: After the merge, see it published**

Run: `gh run list --workflow ui-release.yml --limit 1` until it is `completed success`, then `npm view @shpyrd/ui@0.1.0 version` and `gh release view ui-v0.1.0`.
Expected: `0.1.0`, and a release `@shpyrd/ui 0.1.0` with the CHANGELOG section. Then the owner does step 3 of "Owner: before the first publication" (trusted publishing, delete the token).

### Task 7: signup on the package (repository `shpyrd-signup`)

**Files:**
- Delete: `vendor/shpyrd-ui/` (93 files)
- Delete: `src/screens/code-input.tsx`
- Modify: `src/screens/signup.tsx:13` (the import of `CodeInput`)
- Modify: `package.json` (the `@shpyrd/ui` dependency, the `ui` script)
- Modify: `package-lock.json` (by `npm install`)
- Modify: `README.md` (lines 5, 12 and 15 speak of the copy)
- Create: `.github/dependabot.yml`

**Interfaces:**
- Consumes: `@shpyrd/ui@^0.1.0` from npm, and `CodeInput` from `@shpyrd/ui/components/code-input` (Task 6).

What to expect from the upgrade: signup's copy is the library at `37e2b32` (2026-09-30). Of what signup imports, `field` has not changed since; `button`, `input` and `state-page` changed their looks only (the radius token `rounded-control`, the primary button's glow, `StatePage`'s title in semibold) and `StatePage` gained an optional `picture`. No prop signup passes changed, so the type-check should pass as it is; the screens will look a little different, in the library's current style.

Work from `shpyrd-signup` on `main`, on a branch such as `feat/ui-package`.

- [ ] **Step 1: Screenshots before**

Run: `npm run design` (port 4328, the Mock), open `http://localhost:4328` in the browser, and capture the signup form and the code step (enter an email and submit to reach it). Save them as `before-form.png` and `before-code.png` outside the repository. Stop the server.

- [ ] **Step 2: Depend on the package**

In `package.json`, `"@shpyrd/ui": "file:vendor/shpyrd-ui"` becomes `"@shpyrd/ui": "^0.1.0"`, and the `"ui": "rsync …"` script line is removed.

Run:

```bash
git rm -r -q vendor/shpyrd-ui
rm -rf node_modules/@shpyrd/ui
npm install --no-audit --no-fund
```

Run: `node -p "require('./package-lock.json').packages['node_modules/@shpyrd/ui'].resolved"`
Expected: `https://registry.npmjs.org/@shpyrd/ui/-/ui-0.1.0.tgz`.

- [ ] **Step 3: Use the library's CodeInput**

In `src/screens/signup.tsx`, line 13, `import { CodeInput } from "./code-input";` becomes:

```tsx
import { CodeInput } from "@shpyrd/ui/components/code-input";
```

Run: `git rm -q src/screens/code-input.tsx`

- [ ] **Step 4: The gates**

Run: `npm run lint && npm run typecheck && npm run test && npm run build`
Expected: all pass. If the type-check reports a prop that changed, fix the call to the library's current interface (the library's gallery, `http://localhost:4323`, shows each component's use).

- [ ] **Step 5: Screenshots after, and a look**

Run: `npm run design`, capture the same two screens as `after-form.png` and `after-code.png`, and check by eye against the ones from before: the same screens in the library's current style, the code step's six boxes behaving as before (type, delete back, paste a code). Stop the server.

- [ ] **Step 6: The README speaks of the package**

In `README.md`:
- the passage at line 5, "(`@shpyrd/ui`: a copy of `../shpyrd/design/ui` kept in `vendor/shpyrd-ui`, so …)", becomes "(`@shpyrd/ui`, the platform's design system, from npm)", keeping the rest of the sentence;
- line 12, `npm install            # copies @shpyrd/ui from vendor/shpyrd-ui`, becomes `npm install            # @shpyrd/ui comes from npm`;
- line 15, `npm run ui ...`, is removed, and after the code block add:

```markdown
A new version of `@shpyrd/ui` arrives as a pull request from Dependabot.
To take one by hand: `npm install @shpyrd/ui@^<version>`. Its changes are in
the library's changelog (`design/ui/CHANGELOG.md` in shpyrd-io/shpyrd).
```

- [ ] **Step 7: Dependabot proposes each release**

`.github/dependabot.yml`:

```yaml
version: 2
updates:
  # A release of the design system arrives as a pull request.
  - package-ecosystem: npm
    directory: /
    schedule:
      interval: daily
    allow:
      - dependency-name: "@shpyrd/ui"
    commit-message:
      prefix: "deps(ui)"
```

- [ ] **Step 8: Commit, push, open the pull request**

```bash
git add package.json package-lock.json src/screens/signup.tsx README.md .github/dependabot.yml
git commit -m "feat: the design system from npm, @shpyrd/ui 0.1.0, in place of the copy in vendor/"
git push -u origin feat/ui-package
gh pr create --base main --title "feat: @shpyrd/ui from npm in place of vendor/shpyrd-ui" --body "Cycle 1 of shpyrd's docs/superpowers/specs/2026-10-08-design-ui-package-design.md: signup depends on @shpyrd/ui ^0.1.0 from npm, the copy in vendor/ is gone, and CodeInput comes from the library. The screens take the library's current style (the copy was from 2026-09-30); screenshots before and after attached."
```

Attach the four screenshots to the pull request. Once it is merged and deployed, signup is done; cycle 2 (legal) gets its plan.
