# The website on design/ui and content/ — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the site's written texts to `content/` and rebuild shpyrd.io as a Next 16 application drawing with `design/ui`.

**Architecture:** `content/` becomes an npm workspace package holding the documentation as Markdoc and the marketing copy as typed data. `apps/example-next` is renamed to `apps/website`, gains the three marketing pages, and reads its texts from `content/`. The old Next 13 site is deleted last, after a preview deploy proves the new one.

**Tech Stack:** Next 16 (static export, no Node at run time), React 19, `@shpyrd/ui` through the root npm workspace, Markdoc, Tailwind 4, vitest, oxlint.

**Spec:** [`docs/superpowers/specs/2026-09-29-website-on-design-ui.md`](../specs/2026-09-29-website-on-design-ui.md)

## Global Constraints

- **The product name is lowercase everywhere: `shpyrd`, never `Shpyrd`** — including at the start of a sentence and in page titles. The `X-Shpyrd-*` HTTP headers keep their capitals; they are wire identifiers.
- **No colour written by hand.** The identity comes whole from `@import "@shpyrd/ui/styles.css"`. No font, no hex, no `oklch()` in the application.
- **Components come from the library**: `@shpyrd/ui/components/<name>`. Nothing is copied out of it. Anything generic that is missing is added to `design/ui` with a gallery page and a line in `design/ui/app/catalog.ts`.
- **Imports inside `design/ui/src` are relative.** `@/` means something else in each application.
- **A component with state or effects starts with `"use client"`**, and nothing reads `window` or `localStorage` while rendering.
- **These addresses do not move, and carry no trailing slash:** `/docs/<slug>` for all 22 documents, `/install.sh`, `/screenshots/*.png`.
- **Next config keeps `agentRules: false`** or Next writes `AGENTS.md` and `CLAUDE.md` into the folder on every start.
- **Node 20.9 or newer**; `npm install` runs at the repository root, never inside a workspace member.

## Review Focus

1. **A document with no `description` in its front matter** — `generateMetadata` would emit `description: undefined` and the page would ship without a meta description, as 23 pages did earlier in this project's history. Covered in Task 2.
2. **Two headings with the same words on one page** — `slug()` has no counter, so both get the same `id` and the "On this page" link jumps to the first. The site it replaces used `slugifyWithCounter`. Covered in Task 2.
3. **A navigation entry naming a document that does not exist**, or a document no entry names — a dead link in the sidebar, or a page nobody can reach. Covered in Task 3.
4. **`/docs` with no slug** — must redirect to `/docs/getting-started`, not 404. It 404s on the site being replaced. Covered in Task 6.
5. **A marketing page rendered at build time reading the browser** — the copy modules are data, but a component that reads `localStorage` during render breaks `next build`, which is how the dashboard's theme broke. Covered in Task 5.

---

### Task 1: `content/` as a workspace package, with the documents moved

**Files:**
- Create: `content/package.json`
- Create: `content/README.md`
- Move: `apps/website/src/pages/docs/*.md` (22 files) → `content/docs/`
- Move: `apps/website/src/pages/index.md` → `content/docs/getting-started.md`
- Move: `apps/example-next/src/lib/navigation.ts` → `content/navigation.ts`
- Modify: `package.json` (root) — add `content` to `workspaces`
- Modify: `apps/example-next/src/lib/content.ts:9` — the directory it reads

**Interfaces:**
- Consumes: nothing.
- Produces: the package `@shpyrd/content`, exporting `./navigation` (`navigation: Group[]`, `find(path: string)`), and the folder `content/docs/` holding 23 Markdoc files.

- [ ] **Step 1: Move the documents and the navigation**

```bash
cd /home/nkr/Projects/shpyrd
mkdir -p content/docs
git mv apps/website/src/pages/docs/*.md content/docs/
git mv apps/website/src/pages/index.md content/docs/getting-started.md
git mv apps/example-next/src/lib/navigation.ts content/navigation.ts
ls content/docs/*.md | wc -l   # expect 23
```

- [ ] **Step 2: Write `content/package.json`**

```json
{
  "name": "@shpyrd/content",
  "private": true,
  "version": "0.0.0",
  "type": "module",
  "exports": {
    "./navigation": "./navigation.ts",
    "./site/*": "./site/*.ts"
  }
}
```

- [ ] **Step 3: Add it to the workspace**

In the root `package.json`, `workspaces` becomes:

```json
  "workspaces": [
    "design/ui",
    "content",
    "apps/example-next"
  ]
```

- [ ] **Step 4: Point the reader at `content/docs`**

In `apps/example-next/src/lib/content.ts`, replace the `dir` line and the comment above it:

```ts
// The texts of the site, read where they live: the Markdoc files of
// content/docs. CONTENT_DIR names another folder, relative to this
// application.

const dir = path.resolve(process.cwd(), process.env.CONTENT_DIR ?? "../../content/docs");
```

The reader took `docs/<name>`; it now takes `<name>`, because `content/docs`
*is* the folder. Replace `read` and `documents`:

```ts
/** A text by the name of its file: "getting-started". */
export function read(name: string): Text {
  const ast = Markdoc.parse(fs.readFileSync(path.join(dir, `${name}.md`), "utf8"));
  const m = meta(ast.attributes.frontmatter);
  const content = Markdoc.transform(ast, config);
  return {
    title: m.title ?? name,
    description: m.description,
    content,
    headings: headings(content),
  };
}

/** The names of the documents, as their addresses have them. */
export function documents(): string[] {
  return fs
    .readdirSync(dir)
    .filter((f) => f.endsWith(".md"))
    .map((f) => f.replace(/\.md$/, ""))
    .sort();
}
```

- [ ] **Step 5: Fix every caller of the old two-part name**

`apps/example-next/app/page.tsx` read `index`; the homepage text is now a
document like any other, and the marketing homepage takes `/`. For now, point
it at the moved file so the tree keeps building:

```tsx
import { Text } from "@/components/text";
import { read } from "@/lib/content";

// Replaced by the marketing homepage in Task 5.
export default function Home() {
  return <Text text={read("getting-started")} />;
}
```

`apps/example-next/app/docs/[slug]/page.tsx` — three occurrences of
`` `docs/${...}` `` become the slug alone:

```tsx
import { Text } from "@/components/text";
import { documents, read } from "@/lib/content";

// The documents are known when the application is built: one page each.
export function generateStaticParams() {
  return documents().map((slug) => ({ slug }));
}

export async function generateMetadata({ params }: { params: Promise<{ slug: string }> }) {
  const text = read((await params).slug);
  return { title: text.title, description: text.description };
}

export default async function Document({ params }: { params: Promise<{ slug: string }> }) {
  return <Text text={read((await params).slug)} />;
}
```

`apps/example-next/src/components/shell.tsx` imports `find` and `navigation`
from `@/lib/navigation`; change that one import to the package:

```tsx
import { find, navigation } from "@shpyrd/content/navigation";
```

- [ ] **Step 6: Point "Getting started" at its own address**

In `content/navigation.ts`, the first link is `href: "/"`, which will be the
marketing homepage. It becomes its own page, and the external link the old site
carried comes back:

```ts
      { title: "Getting started", href: "/docs/getting-started" },
```

and at the end of the `Project` group:

```ts
      {
        title: "Support the project",
        href: "https://donate.stripe.com/9B63cxfbwg8H31OgPX2ZO01",
      },
```

- [ ] **Step 7: Write `content/README.md`**

```markdown
# content

What is written: the documentation, the pages and the texts of the site.

- `docs/` — one Markdoc file per documentation page. The address follows the
  filename, and those addresses do not move: the root `README.md`, the install
  instructions and published release notes link to them.
- `site/` — the marketing copy as typed data, because it is not prose: the
  boundaries are claim-and-limit pairs and the workspace roster is rows.
- `navigation.ts` — the order of the documentation, beside the texts it names.

`apps/website` reads `docs/` from disk and imports the rest as
`@shpyrd/content`.
```

- [ ] **Step 8: Install and run the existing tests**

```bash
cd /home/nkr/Projects/shpyrd && npm install
npm --prefix apps/example-next run test
```

Expected: `content.test.ts` FAILS — it calls `read("docs/...")` and `read("index")`, both of which moved.

- [ ] **Step 9: Update the tests to the new names**

In `apps/example-next/src/lib/content.test.ts`:

```ts
  it("finds every document of the site and reads each", () => {
    const all = documents();
    expect(all.length).toBeGreaterThan(20);
    for (const name of all) expect(read(name).title).not.toBe("");
  });

  it("lists the headings of a text, each with a name for its address", () => {
    const { headings } = read("getting-started");
    expect(headings.map((h) => h.id)).toContain("what-you-get");
  });
```

- [ ] **Step 10: Run the tests and the build**

```bash
npm --prefix apps/example-next run test
npm --prefix apps/example-next run typecheck
npm --prefix apps/example-next run build
```

Expected: all pass, and `out/docs/` holds 23 HTML files.

- [ ] **Step 11: Commit**

```bash
git add content package.json apps/example-next apps/website
git commit -m "refactor(content): the texts of the site move to content/

The documentation and the navigation leave apps/website for content/, a
member of the workspace, so an application reads them rather than owning
them. apps/website/src/pages/index.md becomes content/docs/getting-started.md:
the homepage address is taken by the marketing page.

Step 3 of the design, content and apps plan."
```

---

### Task 2: The reader keeps its promises about front matter and headings

**Files:**
- Modify: `apps/example-next/src/lib/content.ts` — `slug` gains a counter
- Test: `apps/example-next/src/lib/content.test.ts`

**Interfaces:**
- Consumes: `read(name)`, `documents()`, `slug(text)` from Task 1.
- Produces: `slug(text: string, seen?: Map<string, number>): string` — the same name, made unique within a page.

- [ ] **Step 1: Write the failing tests**

Add to `apps/example-next/src/lib/content.test.ts`:

```ts
  it("gives every document a title and a description", () => {
    for (const name of documents()) {
      const text = read(name);
      expect(text.title, `${name} has no title`).toBeTruthy();
      expect(text.description, `${name} has no description`).toBeTruthy();
    }
  });

  it("gives two headings of the same words two different names", () => {
    const seen = new Map<string, number>();
    expect(slug("Status", seen)).toBe("status");
    expect(slug("Status", seen)).toBe("status-2");
    expect(slug("Status", seen)).toBe("status-3");
  });

  it("never names two headings of one document the same", () => {
    for (const name of documents()) {
      const ids = read(name).headings.map((h) => h.id);
      expect(new Set(ids).size, `${name} repeats a heading name`).toBe(ids.length);
    }
  });
```

- [ ] **Step 2: Run them to verify they fail**

```bash
npm --prefix apps/example-next run test
```

Expected: FAIL — `slug` takes one argument, and a repeated heading gets a repeated id.

- [ ] **Step 3: Give `slug` a counter**

In `apps/example-next/src/lib/content.ts`:

```ts
/**
 * A name for an address, made of the words of a heading. Two headings of the
 * same words in one document get different names: the second is "-2".
 */
export function slug(text: string, seen?: Map<string, number>): string {
  const base = text
    .toLowerCase()
    .normalize("NFD")
    .replace(/[̀-ͯ]/g, "")
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "");
  if (!seen) return base;
  const n = (seen.get(base) ?? 0) + 1;
  seen.set(base, n);
  return n === 1 ? base : `${base}-${n}`;
}
```

- [ ] **Step 4: Give each document its own counter**

`config` is a module constant shared by every call, so the counter cannot live
in it. Make the config a function of the counter, and build it per document.
Replace `const config: Config = {` with:

```ts
function configFor(seen: Map<string, number>): Config {
  return {
```

leave the `tags` and `nodes` as they are, change the heading transform's
`slug(words(children))` to `slug(words(children), seen)`, and close the
function after the object:

```ts
  };
}
```

Then in `read`:

```ts
export function read(name: string): Text {
  const ast = Markdoc.parse(fs.readFileSync(path.join(dir, `${name}.md`), "utf8"));
  const m = meta(ast.attributes.frontmatter);
  const content = Markdoc.transform(ast, configFor(new Map()));
  return {
    title: m.title ?? name,
    description: m.description,
    content,
    headings: headings(content),
  };
}
```

- [ ] **Step 5: Run the tests**

```bash
npm --prefix apps/example-next run test
```

Expected: PASS. If "gives every document a title and a description" fails, a
document really is missing one — add it to that file's front matter rather
than weakening the test.

- [ ] **Step 6: Commit**

```bash
git add apps/example-next/src/lib
git commit -m "fix(content): two headings of the same words get two names

A document with two headings alike gave both the same address, and the
list beside the text sent a reader to the first one twice. The counter is
per document, so the config is built per read rather than shared.

Also pins what every document owes: a title and a description."
```

---

### Task 3: The navigation and the documents agree

**Files:**
- Test: `content/navigation.test.ts` (create)
- Modify: `content/package.json` — a test script

**Interfaces:**
- Consumes: `navigation` from `@shpyrd/content/navigation`, `documents()` from the application's reader.
- Produces: nothing new.

- [ ] **Step 1: Write the failing test**

`content/navigation.test.ts`:

```ts
import fs from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";
import { navigation } from "./navigation";

const docs = fs
  .readdirSync(path.join(import.meta.dirname, "docs"))
  .filter((f) => f.endsWith(".md"))
  .map((f) => `/docs/${f.replace(/\.md$/, "")}`);

const listed = navigation
  .flatMap((group) => group.links.map((link) => link.href))
  .filter((href) => href.startsWith("/docs/"));

describe("the navigation of the site", () => {
  it("names only documents that exist", () => {
    for (const href of listed) expect(docs, `${href} is listed and missing`).toContain(href);
  });

  it("leaves no document unreachable", () => {
    for (const href of docs) expect(listed, `${href} exists and is not listed`).toContain(href);
  });

  it("names each document once", () => {
    expect(new Set(listed).size).toBe(listed.length);
  });
});
```

- [ ] **Step 2: Give `content` a test script**

In `content/package.json`, add:

```json
  "scripts": {
    "test": "vitest run --passWithNoTests",
    "typecheck": "tsc --noEmit"
  },
  "devDependencies": {
    "typescript": "~6.0.2",
    "vitest": "^5.0.2"
  }
```

- [ ] **Step 3: Run it to verify it fails or passes honestly**

```bash
cd /home/nkr/Projects/shpyrd && npm install
npm --prefix content run test
```

Expected: the third test passes; the first two tell you whether the list and
the folder agree. They did not before this plan — `getting-started` is new to
the list.

- [ ] **Step 4: Reconcile whatever it reports**

Add the missing entries to `content/navigation.ts`, or remove entries naming
files that are not there. Do not weaken the tests.

- [ ] **Step 5: Run it again**

```bash
npm --prefix content run test
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add content
git commit -m "test(content): the navigation and the documents agree

A page listed and missing is a dead link in the sidebar; a page present
and unlisted is one nobody can reach. Both are now build failures."
```

---

### Task 4: `apps/example-next` becomes `apps/website`

**Files:**
- Move: `apps/example-next/` → `apps/website/` (the old `apps/website` is deleted in Task 7, so this is done in two moves)
- Modify: `package.json` (root) — `workspaces`
- Modify: `apps/website/package.json` — name and port
- Modify: `.claude/launch.json` — the entry
- Modify: `apps/website/src/components/shell.tsx` — the footer text and the badge
- Move: `apps/website/public/install.sh`, `apps/website/public/screenshots/` from the old site

**Interfaces:**
- Consumes: `@shpyrd/content` from Task 1.
- Produces: the application `@shpyrd/website`, on port 4324.

- [ ] **Step 1: Put the old site aside and move the new one in**

```bash
cd /home/nkr/Projects/shpyrd
git mv apps/website apps/website-old
git mv apps/example-next apps/website
git mv apps/website-old/public/install.sh apps/website/public/install.sh
git mv apps/website-old/public/screenshots apps/website/public/screenshots
```

`apps/website-old` is deleted in Task 7, after a preview deploy.

- [ ] **Step 2: Rename the package**

`apps/website/package.json`:

```json
  "name": "@shpyrd/website",
```

- [ ] **Step 3: Point the workspace at it**

Root `package.json`:

```json
  "workspaces": [
    "design/ui",
    "content",
    "apps/website"
  ]
```

- [ ] **Step 4: Rename the launch entry**

`.claude/launch.json`, replacing the `example-next` configuration:

```json
    {
      "name": "website",
      "runtimeExecutable": "npm",
      "runtimeArgs": ["--prefix", "apps/website", "run", "dev"],
      "port": 4324
    }
```

- [ ] **Step 5: Take the example's words out of the shell**

In `apps/website/src/components/shell.tsx`, remove the `Badge` beside the
wordmark and its import, and replace the footer's text:

```tsx
      <PageLayoutFooter
        divider="line"
        className="px-4 py-4 text-sm text-muted-foreground @3xl/page-layout:px-6"
      >
        shpyrd is open source under MPL-2.0, and in beta.
      </PageLayoutFooter>
```

- [ ] **Step 6: Install and run every gate**

```bash
cd /home/nkr/Projects/shpyrd && npm install
npm --prefix apps/website run lint
npm --prefix apps/website run typecheck
npm --prefix apps/website run test
npm --prefix apps/website run build
```

Expected: all pass. `apps/website/out/` holds the 23 documents.

- [ ] **Step 7: Commit**

```bash
git add -A apps .claude package.json
git commit -m "refactor(website): the example becomes the site

apps/example-next was written as the seed of this step and already held
the reader, the docs route and the shell. It takes the name, the port and
the public files of the site. The old Next 13 site waits as
apps/website-old until a preview deploy proves this one.

Step 7 of the design, content and apps plan."
```

---

### Task 5: The marketing copy moves to `content/site`

**Files:**
- Create: `content/site/messages.ts`, `boundaries.ts`, `home.ts`, `sharing.ts`, `offer.ts`, `agents.ts`
- Test: `content/site/site.test.ts`

**Interfaces:**
- Consumes: nothing.
- Produces: `@shpyrd/content/site/messages` (`active: Message`), `.../boundaries` (`boundaries: Boundary[]`, `boundariesForStep(step: string): Boundary[]`), `.../home`, `.../sharing`, `.../offer`, `.../agents`.

- [ ] **Step 1: Bring the six modules across**

They were written on the `docs/website-positioning` branch and are plain data.
Copy them verbatim, then add the types below.

```bash
cd /home/nkr/Projects/shpyrd && mkdir -p content/site
for f in messages boundaries home sharing offer agents; do
  git show docs/website-positioning:apps/website/src/copy/$f.js > content/site/$f.ts
done
ls content/site
```

- [ ] **Step 2: Type the two functions TypeScript will refuse**

`content/site/boundaries.ts` — add the type above the array and annotate the
helper at the foot of the file:

```ts
export type Boundary = {
  id: string;
  claim: string;
  limit: string;
  step: string | null;
};

export const boundaries: Boundary[] = [
```

```ts
export function boundariesForStep(step: string): Boundary[] {
  return boundaries.filter((boundary) => boundary.step === step);
}
```

`content/site/messages.ts` — add the type and annotate the three families:

```ts
export type Message = {
  id: string;
  headline: string;
  explanation: string;
  supporting: string | null;
};

export const sharing: Message = {
```

Annotate `access` and `familiarTools` the same way, and leave
`export const active = sharing` as it is.

- [ ] **Step 3: Write the test**

`content/site/site.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { boundaries, boundariesForStep } from "./boundaries";
import { active, access, familiarTools } from "./messages";
import { agents } from "./agents";

describe("the copy of the site", () => {
  it("gives every message family a headline and an explanation", () => {
    for (const m of [active, access, familiarTools]) {
      expect(m.headline).toBeTruthy();
      expect(m.explanation).toBeTruthy();
    }
  });

  it("writes the name in lower case, everywhere it appears", () => {
    const text = JSON.stringify([active, access, familiarTools, boundaries, agents]);
    expect(text).not.toMatch(/Shpyrd/);
  });

  it("gives each boundary a claim and the limit that goes with it", () => {
    for (const b of boundaries) {
      expect(b.claim, b.id).toBeTruthy();
      expect(b.limit, b.id).toBeTruthy();
    }
  });

  it("finds the boundaries of a step of the walkthrough", () => {
    expect(boundariesForStep("access").map((b) => b.id)).toContain("sign-in");
    expect(boundariesForStep("nothing-is-this")).toEqual([]);
  });

  it("gives every agent a name and something to paste", () => {
    for (const a of agents) {
      expect(a.name, a.id).toBeTruthy();
      expect(a.snippet, a.id).toContain("/mcp");
    }
  });
});
```

- [ ] **Step 4: Run it**

```bash
npm --prefix content run test
npm --prefix content run typecheck
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add content/site
git commit -m "feat(content): the marketing copy, as data

The sharing-led copy, the claim boundaries and the add-to-agent snippets,
written against the September research. Data rather than prose, because
the boundaries are claim-and-limit pairs and the roster is rows.

agents.ts carries its own warning: the workspace MCP endpoint is ours and
documented, the per-client formats are the clients' own and unverified,
and the homepage's deploy claim is ahead of the four read-only tools."
```

---

### Task 6: The three marketing pages — a design session

This task is interface work. Follow `design/DESIGN_FOR_AGENT.md`: say "enter
design mode" with `apps/website` as the target, keep one server up for the
whole task, write a changelog entry per accepted change in
`tmp/design-session/session_changelog.md`, and pay the tests and gates at the
end.

**Files:**
- Create: `apps/website/app/how-sharing-works/page.tsx`, `apps/website/app/bring-an-app/page.tsx`
- Modify: `apps/website/app/page.tsx` — the marketing homepage
- Create: `apps/website/src/components/roster.tsx`, `apps/website/src/components/boundaries.tsx`, `apps/website/src/components/add-to-agent.tsx`
- Modify: `apps/website/next.config.ts` — the `/docs` redirect
- Modify: `apps/website/src/components/shell.tsx` — the marketing pages are not documents

**Interfaces:**
- Consumes: `@shpyrd/content/site/*` from Task 5; `@shpyrd/ui/components/*`.
- Produces: the three addresses `/`, `/how-sharing-works`, `/bring-an-app`.

- [ ] **Step 1: Start the server and open it**

```bash
cd /home/nkr/Projects/shpyrd && npm install
npm --prefix apps/website run dev
```

It serves on `http://localhost:4324`. Leave it up for the whole task.

- [ ] **Step 2: Read the gallery before writing markup**

```bash
npm --prefix design/ui run dev
```

`http://localhost:4323` — look at `prose`, `card`, `alert`, `page-layout`,
`page-heading`, `stack`, `button`, `dropdown-button`, `nav-list`, `brand`,
`badge`, `blankslate`. The rule of the library is that anything generic that
is missing is added there, with a gallery page and a line in
`design/ui/app/catalog.ts` — not copied into the application.

- [ ] **Step 3: Add the `/docs` redirect**

`apps/website/next.config.ts`:

```ts
import type { NextConfig } from "next";

// Everything is compiled to static files (out/): no Node at run time.
const config: NextConfig = {
  output: "export",
  // Our own guides say what an agent needs (CLAUDE.md at the root).
  agentRules: false,
  // There is no /docs index; the getting started page is the entry point.
  redirects: async () => [
    { source: "/docs", destination: "/docs/getting-started", permanent: true },
  ],
};

export default config;
```

`output: "export"` does not emit redirects, so the same rule goes in
`apps/website/public/_redirects` for the host to serve:

```
/docs /docs/getting-started 308
```

- [ ] **Step 4: Build the three pages**

Draw them from `@shpyrd/content/site/*`, with the library's components. The
homepage is the hardest, so it is written out; the other two follow the same
shape, reading `sharing` and `offer` instead.

`apps/website/app/page.tsx`:

```tsx
import { PageLayoutContent } from "@shpyrd/ui/components/page-layout";
import { Stack } from "@shpyrd/ui/components/stack";
import { Button } from "@shpyrd/ui/components/button";
import { active } from "@shpyrd/content/site/messages";
import { boundaries } from "@shpyrd/content/site/boundaries";
import { pillars, situation, hosting, useCases } from "@shpyrd/content/site/home";
import { offer, secondaryCta } from "@shpyrd/content/site/offer";
import { AddToAgent } from "@/components/add-to-agent";
import { Boundaries } from "@/components/boundaries";
import { Roster } from "@/components/roster";

// The last row is the hook: the reader's own app, the one that works and
// that nobody else can open yet.
const workspace = [
  { name: "Purchase requests", audience: "Finance, Operations", shared: true },
  { name: "Onboarding checklist", audience: "People", shared: true },
  { name: "Quote tool", audience: "Sales", shared: true },
  { name: "Field reports", audience: "Not shared yet", shared: false },
];

export const metadata = {
  title: "shpyrd - One place to share apps with your team",
  description: active.explanation,
};

export default function Home() {
  return (
    <PageLayoutContent width="large" padding="normal" className="grid content-start gap-16">
      <Stack gap="normal">
        <h1 className="text-4xl font-semibold sm:text-5xl">{active.headline}</h1>
        <p className="max-w-prose text-lg text-muted-foreground">{active.explanation}</p>
        <Stack direction="horizontal" gap="cozy" align="center">
          <AddToAgent />
          <Button variant="outline" asChild>
            <a href={secondaryCta.href}>{secondaryCta.label}</a>
          </Button>
        </Stack>
        {active.supporting && (
          <p className="max-w-prose text-sm text-muted-foreground">{active.supporting}</p>
        )}
      </Stack>

      <Stack gap="normal">
        <h2 className="text-2xl font-semibold">{situation.title}</h2>
        {situation.body.map((p) => (
          <p key={p} className="max-w-prose">{p}</p>
        ))}
      </Stack>

      <Stack gap="normal">
        <h2 className="text-2xl font-semibold">{pillars.title}</h2>
        <div className="grid gap-8 lg:grid-cols-[1fr_20rem]">
          <dl className="grid gap-8 sm:grid-cols-2">
            {pillars.items.map((item) => (
              <div key={item.id}>
                <dt className="font-semibold">{item.title}</dt>
                <dd className="mt-2 text-muted-foreground">{item.body}</dd>
              </div>
            ))}
          </dl>
          <Roster caption="Your workspace" entries={workspace} />
        </div>
      </Stack>

      <Stack gap="normal">
        <h2 className="text-2xl font-semibold">{hosting.title}</h2>
        {hosting.body.map((p) => (
          <p key={p} className="max-w-prose text-muted-foreground">{p}</p>
        ))}
      </Stack>

      <Stack gap="normal">
        <h2 className="text-2xl font-semibold">{useCases.title}</h2>
        <ul className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          {useCases.examples.map((e) => (
            <li key={e} className="font-medium">{e}</li>
          ))}
        </ul>
        <p className="max-w-prose text-muted-foreground">{useCases.disclaimer}</p>
      </Stack>

      <Stack gap="normal">
        <h2 className="text-2xl font-semibold">{offer.title}</h2>
        <p className="max-w-prose text-muted-foreground">{offer.intro}</p>
        <Boundaries items={boundaries} />
      </Stack>
    </PageLayoutContent>
  );
}
```

The utility classes above are placement and type scale, which the library does
not own. No colour is written: `text-muted-foreground` is a token.

The other two pages:

- `/` — hero (`active.headline`, `active.explanation`, the add-to-agent action, a link to `/how-sharing-works`, `active.supporting`), the situation, the three pillars beside the workspace roster, where it runs, what people put in it, the offer, the boundaries, run it yourself.
- `/how-sharing-works` — the four steps of `sharing.steps`, each with its screenshot and the boundaries of its step, then `runs`, then the offer.
- `/bring-an-app` — `offer.intro`, what to bring, what happens, what this is not, when this is a bad fit, `contact`.

Two screenshots named by `sharing.steps` do not exist —
`/screenshots/launcher.png` and `/screenshots/denied.png`. Render the
placeholder the copy carries (`exists: false`, `note`) rather than a broken
image, and leave both owed in the changelog.

- [ ] **Step 5: Prove no page reads the browser while rendering**

Every page above is rendered when the application is built, where there is no
`window` and no `localStorage`. A component that reads either during render
breaks `next build` outright — it is how the dashboard's theme broke, per the
plan's "what was tried". `AddToAgent` keeps state, so it starts with
`"use client"`, and anything it reads from the browser is read in an effect or
an event, never in the body.

```bash
npm --prefix apps/website run build
```

Expected: PASS, and `out/index.html`, `out/how-sharing-works.html` and
`out/bring-an-app.html` all exist.

- [ ] **Step 6: Keep the marketing pages out of the documentation shell**

`find(path)` returns nothing for `/how-sharing-works`, so the breadcrumbs are
already empty there. Check the sidebar reads sensibly on a marketing page; if
it does not, give the shell a branch on whether `find(path)` matched.

- [ ] **Step 7: One changelog entry per accepted change**

`tmp/design-session/session_changelog.md`, six lines at most each, as
`design/DESIGN_FOR_AGENT.md` shows.

- [ ] **Step 8: Finish the session**

Reconcile the changelog against `git diff --name-only`, pay what is owed, then:

```bash
npm --prefix apps/website run lint
npm --prefix apps/website run typecheck
npm --prefix apps/website run test
npm --prefix apps/website run build
npm --prefix design/ui run lint && npm --prefix design/ui run typecheck \
  && npm --prefix design/ui run test && npm --prefix design/ui run build
```

`design/ui` is built too: a change there can break a screen elsewhere.

- [ ] **Step 9: Commit**

```bash
git add -A apps/website design/ui
git commit -m "feat(website): the marketing pages, made of design/ui

The homepage, the walkthrough and the offer, drawn with the library and
reading content/site. The identity comes whole from @shpyrd/ui: Geist and
the tokens, no colour by hand.

Two screenshots the walkthrough names do not exist yet - the employee
launcher and a denied-access view - and render as labelled placeholders."
```

---

### Task 7: The old site goes, and the build chain learns the new folders

**Files:**
- Delete: `apps/website-old/`
- Modify: `.github/workflows/website.yml`
- Modify: `.github/workflows/ci.yml` — the path filters
- Modify: `Makefile` — the `website` and `website-dev` targets
- Modify: `README.md` — the tree and the website paragraph
- Modify: `apps/AGENTS.md` — `website/` is no longer apart

**Interfaces:**
- Consumes: everything above.
- Produces: nothing new.

- [ ] **Step 1: Confirm the new site on a preview deploy first**

This is the sharp one. Vercel's root directory, build command and output
folder all change, and the cutover to `apps/website` is already live, so a
wrong setting takes shpyrd.io down rather than spoiling a preview.

Ask the maintainer to set, and to confirm a preview deploy is green:

| Setting | Was | Becomes |
|---|---|---|
| Root Directory | `apps/website` | the repository root |
| Install Command | `npm ci` | `npm ci` (the workspace installs from the root) |
| Build Command | `npm run build` | `npm run build --prefix apps/website` |
| Output Directory | `.next` | `apps/website/out` |

Do not continue past this step until the preview is green.

- [ ] **Step 2: Delete the old site**

```bash
cd /home/nkr/Projects/shpyrd
git rm -r --quiet apps/website-old
```

- [ ] **Step 3: Teach the website workflow the new folders**

`.github/workflows/website.yml` — the paths in both `push` and `pull_request`
(they are repeated because Actions has no YAML anchors), and the job:

```yaml
    paths:
      - 'apps/website/**'
      - 'design/ui/**'
      - 'content/**'
      - 'package.json'
      - 'package-lock.json'
      - '.github/workflows/website.yml'
```

```yaml
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: 24
          cache: npm
          cache-dependency-path: package-lock.json
      - run: npm ci --no-audit --no-fund
      - run: npm --prefix content run test
      - run: npm --prefix design/ui run lint
      - run: npm --prefix design/ui run typecheck
      - run: npm --prefix design/ui run test
      - run: npm --prefix design/ui run build
      - run: npm --prefix apps/website run lint
      - run: npm --prefix apps/website run typecheck
      - run: npm --prefix apps/website run test
      - run: npm --prefix apps/website run build
```

Remove the `defaults.run.working-directory` block: the install is at the root
now, and each step names its own prefix.

- [ ] **Step 4: Keep web changes out of the end-to-end job**

The gist records that a change under `design/` triggers the end-to-end job,
about 45 minutes. In `.github/workflows/ci.yml`, add to the `paths-ignore` of
both `push` and `pull_request`:

```yaml
      - 'design/**'
      - 'content/**'
      - 'apps/**'
```

Check `examples/` is not caught by that last line — the end-to-end job deploys
the five local examples and must still run when they change.

- [ ] **Step 5: Update the Makefile**

```make
## Build the website (shpyrd.io); deployed from apps/website/ on Vercel
website:
	npm ci --no-audit --no-fund && npm --prefix apps/website run build

## Serve the website locally on http://localhost:4324
website-dev:
	npm install && npm --prefix apps/website run dev
```

- [ ] **Step 6: Update the README and the apps guide**

Root `README.md`, the tree line:

```
apps/website/         shpyrd.io: the site and the docs (Next 16 + design/ui)
content/              the documentation and the texts of the site
design/ui             the component library and its gallery
```

and the paragraph:

```markdown
The website is a Next application built from the repository root:
`make website-dev` serves [shpyrd.io](https://shpyrd.io) on
http://localhost:4324. The documentation is Markdown under `content/docs/`;
see [apps/website/README.md](apps/website/README.md).
```

`apps/AGENTS.md`, the line under the title — `website/` is no longer apart:

```markdown
What an application here is made of.
```

- [ ] **Step 7: Run everything**

```bash
cd /home/nkr/Projects/shpyrd && npm install
npm --prefix content run test
npm --prefix design/ui run lint && npm --prefix design/ui run typecheck \
  && npm --prefix design/ui run test && npm --prefix design/ui run build
npm --prefix apps/website run lint && npm --prefix apps/website run typecheck \
  && npm --prefix apps/website run test && npm --prefix apps/website run build
go vet ./...
```

Expected: all pass.

- [ ] **Step 8: Check the addresses that must not move**

```bash
cd apps/website && npx serve out --listen 3000 &
sleep 3
for u in /docs/getting-started /docs/tour /docs/cli /install.sh \
         /screenshots/project-overview.png / /how-sharing-works /bring-an-app; do
  printf "%-36s %s\n" "$u" "$(curl -s -o /dev/null -w '%{http_code}' http://localhost:3000$u)"
done
```

Expected: 200 for every one.

- [ ] **Step 9: Commit**

```bash
git add -A
git commit -m "chore(website): the Next 13 site goes, and the build chain follows

The site is built from the repository root now, so CI installs the
workspace once and runs the gates of content, design/ui and the
application. A change under design/, content/ or apps/ no longer starts
the 45-minute end-to-end job.

Deleting apps/website-old also ends the Tailwind UI licence carve-out:
every component of the site now comes from design/ui, under MPL-2.0."
```

- [ ] **Step 10: Retire the licence carve-out**

`apps/website/LICENSE` is the Tailwind UI licence, and nothing of that template
survives. Deleting it makes the repository uniformly MPL-2.0, which touches
`README.md`, `CONTRIBUTING.md` and `apps/website/README.md`.

This is a legal statement about the repository, not a code change. **Ask the
maintainer before deleting the file.** If they agree, remove it and the
paragraphs that describe the boundary; if they would rather wait, leave every
one of them exactly as it is and note it as still open.
