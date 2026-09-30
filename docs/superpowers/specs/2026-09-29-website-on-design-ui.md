# The website on design/ui and content/

Date: 2026-09-29
Status: draft, awaiting review
Scope: steps 3 and 7 of [the design, content and apps plan](../plans/2026-09-29-design-content-apps.md)

## Intent

The plan gives the applications, the site and the landing pages one identity,
and lists eight steps to get there. This spec covers two of them: moving the
written texts to `content/` (step 3), and rebuilding the site on Next 16,
`design/ui` and `content/` (step 7).

The site today is Next 13 on the commercial Tailwind *Syntax* template, outside
the npm workspace, with fonts and an orange of its own. When this is done it is
a member of the workspace, draws with `design/ui`, and reads its texts from
`content/`.

## These two steps do not have to wait for the others

The plan orders its steps because "the applications depend on the library and
on `apps/shared`, and Go only changes once they exist." The website is the
exception to both halves:

- It makes no API call, has no sign-in and no permissions, so it needs nothing
  from `apps/shared` (step 4).
- It is served by Vercel, not embedded in the Go binary, so none of the serving
  rule, the embed or the script hashes apply to it (step 6).

Its only dependency is `design/ui`, which exists, and the mark, which
`design/ui/src/components/brand.tsx` already carries — so `design/brand`
(step 2) is not a blocker either.

That makes the website the cheapest way to prove the library against a real
application while steps 4 to 6 are still ahead.

## Decisions

Taken with the maintainer on 2026-09-29.

### The identity comes whole from the library

`@import "@shpyrd/ui/styles.css"` and nothing else, exactly as
`apps/example-next` does today. Geist, the dashboard's tokens, no colour
written by hand.

This settles the "identidade do site" question the gist leaves open, in the
direction the plan assumed. Two consequences worth stating plainly: the site's
Lexend and Inter go, and the site's primary becomes
`oklch(0.66 0.22 38)` rather than the mark's `#ff4f00`. The mark keeps its own
colour inside `brand.tsx`, so the logo and the accent are near neighbours
rather than the same value. If that reads badly on a real page, the fix belongs
in the token, where the dashboard inherits it too — not in the site.

### The texts are read by the reader that already exists

`apps/example-next/src/lib/content.ts` parses the Markdoc, returns the front
matter, the content tree and the headings, and has tests beside it. It moves
across and points at `content/`.

Velite was the plan's candidate and stays a candidate. It earns its place when
there is enough content for schema drift to be a real problem — a missing
`description`, a renamed field — and not before. Adopting it now would add a
dependency and a build step to replace working code.

### The site carries the new positioning

The sharing-led copy, the claim boundaries and the add-to-agent action were
written on the `docs/website-positioning` branch against the research of
27 September. They are plain data, independent of the framework, so they
survive that branch being set aside.

What comes across is the copy and the page structure. What does not is the
Astro implementation, its components or its typography — the plan chose Next
and this spec chose the library's identity.

## What moves, and where

```
content/
  docs/*.md        22 pages, from apps/website/src/pages/docs
  site/*.ts        the marketing copy as typed data
```

The documentation stays portable Markdoc. The marketing copy stays structured
data, because it is not prose: the boundaries are claim-and-limit pairs, the
workspace roster is rows, and the agent snippets are per-client configuration.
Flattening them into Markdoc would lose the shape the pages need.

Both belong in `content/`. The plan renamed the folder from `docs` precisely
because "a pasta vai guardar mais do que documentação".

`apps/website/src/pages/index.md` — today's getting-started homepage — becomes
`content/docs/getting-started.md`. The homepage address is taken by the
marketing page.

## The application

`apps/example-next` becomes `apps/website`. It was written as the seed of this
step and already holds most of it: Next 16, static export, `@shpyrd/ui` through
the workspace, the Markdoc reader, a docs route and a shell.

The old `apps/website` — Next 13, the *Syntax* template, its own lock file — is
deleted in the same change.

| Address | What it is |
|---|---|
| `/` | the marketing homepage |
| `/how-sharing-works` | the builder-to-colleague journey |
| `/bring-an-app` | the assisted-beta offer |
| `/docs/<slug>` | the 22 documentation pages |
| `/docs` | redirects to `/docs/getting-started` |

It joins the `workspaces` of the root `package.json` and gains an entry in
`.claude/launch.json` on a port of its own.

### What draws it

The library covers more of this than expected. Only three Markdoc tags are used
across all 23 texts — `callout` (12 times), `quick-link` (4) and `quick-links`
(1), plus the `{% .lead %}` class on 23 first paragraphs — and
`apps/example-next/src/components/markdoc.tsx` already draws all three with
`Alert` and `Card`. `figure` and `loom` are declared in the old site's config
and used nowhere; they are not carried over.

| What the page needs | What draws it |
|---|---|
| the documentation shell | `page-layout` with `nav-list` |
| body text | `prose` |
| quick links | `card` |
| callouts | `alert` |
| the mark and wordmark | `brand` |
| headings, spacing, buttons | `page-heading`, `stack`, `button` |

Two pieces stay in the application because they are not generic: the workspace
roster, and the boundaries list. The add-to-agent action is close to
`dropdown-button`; the gallery is read before any markup is written for it, as
`design/DESIGN_FOR_AGENT.md` requires. Anything that turns out to be generic is
added to `design/ui` with a gallery page and a line in `app/catalog.ts`, never
copied into the application.

## What must not break

The root `README.md`, the install instructions and published release notes link
to `/install.sh`, `/screenshots/*.png` and `/docs/*`. Those addresses keep their
exact shape, without a trailing slash.

`public/install.sh` and `public/screenshots/` move across unchanged.

## Order of work

The content move and the wiring are not interface work and are done normally.
The pages are interface work and are done in a design session, by
`design/DESIGN_FOR_AGENT.md`: one server up for the whole session, a changelog
in `tmp/design-session/`, tests and gates paid when the session is finished.

1. `content/docs` and `content/site`, with the reader pointed at them.
2. `apps/example-next` renamed, wired into the workspace and `launch.json`,
   still drawing what it draws today.
3. **Design session**: the marketing pages, then the documentation shell.
4. The old site deleted; `ci.yml`, the `Makefile` and the Vercel settings
   updated.

Steps 1, 2 and 4 each leave the tree building. Step 3 is where the session
changelog applies.

## Risks

**Vercel.** The root directory, the build command and the output folder all
change, and the cutover to `apps/website` is already done — so a wrong setting
takes shpyrd.io down rather than spoiling a preview. The gist already lists
this as open now that a `package.json` sits above the site. The deploy settings
are changed by the maintainer, not from here, and a preview deploy confirms the
build before the old site is deleted.

**CI.** `ci.yml` knows neither `design/` nor `content/`, and the gist records
that a change under `design/` triggers the end-to-end job, about 45 minutes.
The path filters want fixing while this is open, or every content edit pays for
a cluster run.

**The end-to-end job depends on `examples/`**, not on this, but the same
workflow file is being edited — so the filters are changed with care.

## Out of scope

`design/brand` (step 2), `apps/shared` (4), `apps/console` and `apps/workspace`
(5), the Go embed and serving rule (6), and the build chain (8). Velite. The
blog and changelog. Analytics. The two screenshots the walkthrough page still
lacks — the employee launcher and a denied-access view — which are carried over
as placeholders and remain owed.
