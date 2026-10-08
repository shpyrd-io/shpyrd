# Designing with an agent

Interface work here has two moments. While designing, the only thing that
matters is seeing the screen change. When the design is done, the work that
was put off is paid in one go: tests, clean-up, the quality gates. The
person says when each moment starts.

## Where things are

| Folder | What it holds |
|---|---|
| `design/brand` | the logo, the colours, the fonts |
| `design/ui` | the components (`src/`) and the gallery that shows them (`app/`), a page for each |
| `design/pages` | the pages the server serves by itself, packed into `pkg/pages` (its README) |
| `design/emails` | the emails the platform sends, packed into `pkg/emails` (its README) |
| `content/` | what is written: documentation, pages, texts |
| `apps/` | the applications, which use `design/ui` and `content/` |

## Enter design mode

The person says **"enter design mode"** (or "entrar em modo design") and
names the target: what is being designed. Without a target, ask.

| Target | Server (`.claude/launch.json`) | API | Gates at the end |
|---|---|---|---|
| `design/ui`, the library | `design-ui`, the gallery | none | those of `design/ui` |
| an application, `apps/<name>` | the entry named after it | mock | those of the application |
| `design/pages`, the server's pages | `design-pages`, its gallery | none | those of `design/pages`, then `make pages` and `go test ./pkg/pages` |
| `design/emails`, the emails | `design-emails`, its gallery | none | those of `design/emails`, then `make emails` and `go test ./pkg/emails` |

1. Install what is missing: `npm install` at the repository root.
2. Start the target's server. An application that does not exist yet is
   made first, as `apps/AGENTS.md` says, with its entry in
   `.claude/launch.json`.
3. Open it in the internal browser.
4. Start `tmp/design-session/session_changelog.md` with the header below.
   If one is there with entries not yet paid, ask whether to go on with it.

A session on an application may change the library: a screen often needs
a component that is not there yet. It is one session and one changelog.

## While designing

- **The server is started once and stays up for the whole session.** A
  saved file is on the screen in about a tenth of a second, in the same
  page: measured, 86 ms from saving a component of `design/ui` to seeing
  it in the gallery.
- **To show a change: save the file and look at the tab that is open.** No
  build, no restart, no reload, no second server.
- The server is restarted only when what it reads at start changed (a
  dependency installed, `next.config.ts`, `package.json`, a new member of
  the workspace), or when a new page looks half drawn (The gallery, below).
- The build, the static export and the Go server are how the application
  reaches production (`apps/AGENTS.md`). They have no part in looking at
  a change.
- No tests, no lint, no type-check, no commit unless asked.
- Use what `design/ui` has; look at the gallery before writing markup.
- Something generic that is missing is written where it is quickest and
  noted as owed.
- After each change the person accepts, add one entry to the changelog.
  This is the only duty of this moment.

## The session changelog

`tmp/design-session/session_changelog.md`. The folder is ignored by git.
Entries are only added, one per accepted change, six lines at most.

```markdown
# Design session 2026-09-29 15:30
target: design/ui
branch: claude/ui-design-b91e69
base: f76b423

## 004 · 15:42 · design/ui · the card takes an action on its header
files: design/ui/src/components/card.tsx:40-58
what: a slot on the right of the title, for one button
seen: http://localhost:4323/, light and dark
owed: test card · gallery card with action
```

What may be owed: `test`, `gallery` (a page for the component),
`extract` (markup that belongs in `design/ui`), `token` (a colour written
by hand), `mock` (data a screen needs), `copy` (wording).

## Finish the design session

The person says **"finish design session"** (or "finalizar sessão de
design").

1. **Reconcile.** Compare the changelog with `git diff --name-only <base>`.
   A changed file no entry names gets an entry now.
2. **Pay what is owed**, entry by entry, and mark each as paid.
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
5. **Report**: what changed, what was paid, what is still owed and why.
   The summary goes into the pull request; the changelog itself is never
   committed.

A session is not finished while a gate fails.

## The gallery

The gallery is made of the library itself: a `PageLayout` with a `NavList`
at its side. Each component has a page of its own, in one of four
categories.

| Category | Folder in `app/` | What it holds |
|---|---|---|
| Foundations | `foundations/` | the smallest building blocks: colours, typography, icons, a button |
| Structures | `structures/` | reusable combinations, like forms and cards |
| Blueprints | `blueprints/` | complex interactive components, like a navigation |
| Layouts & Pages | `layouts/` | the placement of a page, and whole views |

A page is made in two steps: the file `app/<category>/<name>/page.tsx`, and
its line in `app/catalog.ts`, which is what the navigation and the overview
are drawn from. Moving a component to another category is moving its
folder and its line.

What a page shows is made up, and made the same way every time
(`app/samples.ts`): the gallery is drawn when it is built and again in the
browser, and both have to draw the same thing.

**When a new page looks half drawn**, it is the build cache. Classes used
only in files made while the server was running may never be written.
Stop the server, remove `design/ui/.next`, start it again.

## Rules of the library

- **Nothing of an application**: no router, no API call, no data fetching.
- **Imports inside `src/` are relative.** `@/` means something else in
  each application that uses the library.
- **A component with state or effects starts with `"use client"`.**
- **Nothing reads the browser while rendering.** A page may be rendered
  when the application is built, where there is no `localStorage` and no
  `window`; read them in an effect or an event.
- **Every component has a page in the gallery**, and a line in
  `app/catalog.ts`.
- **A part of a component is a prop, not a child that is looked for.** A
  component that finds its heading or its action among its children stops
  working when the page is rendered on the server, and while the code is
  reloaded. `heading`, `icon`, `action` are given by name.
- **A colour has a name.** None from the palette is written in a
  component: `success`, `info`, `warning` and `destructive` say how
  something is, each with its `-foreground`; `chart-1` to `chart-5` and
  `chart-success`, `chart-info`, `chart-warning`, `chart-error` are the
  inks of charts. The inks were checked as a set, in their order; a change
  to one has to be checked again.
- **What changes with the room looks at its own room**, not at the
  window: a layout, a table, a chart may be in a narrow column of a wide
  window.
- **Names that repeat.** An icon before the text is `icon`, after it
  `iconEnd`. The other look of a component is `variant="secondary"`.
  What makes a component another element is `asChild`.
- **Tabs go along a line; a list down the side is a `NavList`.** They
  look alike and do different things: tabs change a panel, a list goes to
  another page.
- **A link in a text is written as a link.** There is no component for
  it: a text that comes from a document brings its own links, and `Prose`
  gives them their look.
- **Tests** are vitest files beside the source, named as sentences about
  what the thing does.
