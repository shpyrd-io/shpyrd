# Design, content and apps

One identity for the applications, the site and the landing pages, with as
little repeated code as possible.

## Decided

| Subject | Decision |
|---|---|
| Framework | Next, for everything, compiled to static files; no Node at run time |
| Root folders | `design/` (identity), `content/` (what is written), `apps/` (the applications) |
| Packages | one npm workspace at the root; `design/ui` is `@shpyrd/ui` |
| Code the applications share | `apps/shared` |
| Mock | an `ApiRouter` in the application; an environment variable picks Mock or Backend |
| Builds | one per application |
| Embed | a fixed Go folder, `pkg/ui`, embedding the `dist/` the build writes |
| Serving | no route declared in Go |
| How interface work is done | `design/DESIGN_FOR_AGENT.md` |

## Steps

1. **`design/ui`** (started). Go through `ui/src` and `apps/website` and
   bring in what repeats: one header, one page heading, one key and value
   row, one empty state, one confirmation in place of `window.confirm`,
   names for the status colours. A gallery section for each.
2. **`design/brand`.** Move `apps/design-system/logo`; one drawing of the
   mark, the copies made from it.
3. **`content/`.** Move the Markdoc files of `apps/website/src/pages/docs`.
   Read them with Velite rather than code of our own.
4. **`apps/shared`.** The `ApiRouter`, sign-in, permissions, the shell. The
   Mock answers from JSON files with one generic handler, and keeps what a
   screen changes in `localStorage`.
5. **`apps/console` and `apps/workspace`** in Next: the screens of `ui/`,
   one document each, the routes of today.
6. **Go.** `pkg/ui` with the embed; the serving rule and the script hashes
   below; the folder of files chosen by host. `ui/` goes away.
7. **`apps/website`** on Next 16, `design/ui` and `content/`.
8. **The build chain**: Makefile, Dockerfile, `ci.yml`, and a note in
   RFC-0080, which had put this split off.

## What Go has to do (step 6)

For any address, in this order: a file of the build by its path; the page
built for the address (`<address>.html`); the entry, `index.html`.

Next writes scripts into every page. When the server starts it reads each
page it embeds, takes the SHA-256 of every such script, and sends the page
with today's policy plus `script-src 'self'` and those hashes.

## What was tried

| Tried | Result |
|---|---|
| Importing `design/ui` through a path alias only | Next does not look outside its folder |
| The same, with Next's root widened | the files are found, their own imports are not |
| npm workspace | works: build, types, styles and fonts, in both directions |
| Code that reads `localStorage` while rendering | breaks the build; fixed in `src/lib/theme.ts` |
| A Go server with the rule and the hashes above | Next and Astro both worked, no console error |
| Go tooling with `node_modules` at the root | unaffected: no Go file in it |

Proofs of concept in Astro and in Next, with the same screens, content and
mock, were compared and removed. Next loads more JavaScript (535 kB
against 304 kB for the application) and needs the hashes; it was chosen as
the one framework.

## To check when the step comes

- Vercel and `apps/website`, now that a `package.json` sits at the root.
- How the private cloud layer builds the dashboard, before `ui/` moves.
- `ci.yml` does not know `design/` or `content/` yet.
