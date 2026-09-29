# Designing with an agent

Interface work here has two moments. While designing, the only thing that
matters is seeing the screen change. When the design is done, the work that
was put off is paid in one go: tests, clean-up, the quality gates. The
person says when each moment starts.

## Where things are

| Folder | What it holds |
|---|---|
| `design/brand` | the logo, the colours, the fonts |
| `design/ui` | the components (`src/`) and the gallery that shows them (`app/`) |
| `content/` | what is written: documentation, pages, texts |
| `apps/` | the applications, which use `design/ui` and `content/` |

## Enter design mode

The person says **"enter design mode"** (or "entrar em modo design") and
names the target: what is being designed. Without a target, ask.

| Target | Server (`.claude/launch.json`) | API | Gates at the end |
|---|---|---|---|
| `design/ui`, the library | `design-ui`, the gallery | none | those of `design/ui` |
| an application, `apps/<name>` | the entry named after it | mock | those of the application |

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

- Change the screen and look at it. Hot reload is the feedback.
- No tests, no lint, no type-check, no build, no commit unless asked.
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

What may be owed: `test`, `gallery` (a section for the component),
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
   changed, also build every application that uses it: a change there can
   break a screen elsewhere.
4. **Report**: what changed, what was paid, what is still owed and why.
   The summary goes into the pull request; the changelog itself is never
   committed.

A session is not finished while a gate fails.

## Rules of the library

- **Nothing of an application**: no router, no API call, no data fetching.
- **Imports inside `src/` are relative.** `@/` means something else in
  each application that uses the library.
- **A component with state or effects starts with `"use client"`.**
- **Nothing reads the browser while rendering.** A page may be rendered
  when the application is built, where there is no `localStorage` and no
  `window`; read them in an effect or an event.
- **Every component has a section in the gallery.**
- **Tests** are vitest files beside the source, named as sentences about
  what the thing does.
