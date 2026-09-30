# Applications

What an application here is made of.

## What an application is

- **A Next application, compiled to static files.** Nothing runs on Node
  at run time; the Go server embeds what the build writes to `out/`.
- **A member of the npm workspace**: its folder is listed in `workspaces`
  of the `package.json` at the repository root, and `npm install` runs
  there. Next does not import from outside its folder otherwise.
- **One document.** The server answers every address with `index.html`
  and the application reads the address. The routes are those of the
  dashboard of today, with React Router.
- **Two applications**, `console/` and `workspace/`, and `shared/` for
  what both use. The dashboard they replaced is gone.

## The files of a new application

`next.config.ts`. A static export answers only the addresses it built and
allows no rewrites, in development too, so development is not one:

```ts
import type { NextConfig } from "next";
import { PHASE_DEVELOPMENT_SERVER } from "next/constants";

export default function config(phase: string): NextConfig {
  if (phase === PHASE_DEVELOPMENT_SERVER) {
    return {
      agentRules: false,
      rewrites: async () => [
        { source: "/api/:path*", destination: "http://localhost:8080/api/:path*" },
      ],
    };
  }
  return { output: "export", agentRules: false };
}
```

`agentRules: false` goes in the development branch too: Next would write
an `AGENTS.md` and a `CLAUDE.md` into the folder on every start. Next 16
differs from earlier versions; its documentation ships with it, in
`node_modules/next/dist/docs`, for when something is in doubt.

`app/[[...slug]]/page.tsx`, the entry, and `client.tsx` beside it:

```tsx
import { ClientOnly } from "./client";

export function generateStaticParams() {
  return [{ slug: [""] }];
}

export default function Page() {
  return <ClientOnly />;
}
```

```tsx
"use client";

import dynamic from "next/dynamic";

const App = dynamic(() => import("@/app/App"), { ssr: false });

export function ClientOnly() {
  return <App />;
}
```

`src/styles/global.css`, imported by `app/layout.tsx`. The identity comes
whole from the library:

```css
@import "@shpyrd/ui/styles.css";
```

`app/layout.tsx` loads `/theme.js` with a `<script src>` in the head. No
script is written inside a page: the server's policy blocks it.

`package.json` has the scripts `dev` (Backend), `design` (Mock), `build`,
`lint`, `typecheck` and `test`, a port of its own, and `"@shpyrd/ui": "*"`
among its dependencies. `design/ui/package.json` is the example.

## What an application must use

- **Components from the library**: `@shpyrd/ui/components/<name>`.
  Nothing is copied. What is missing is added to `design/ui`.
- **Colours, fonts and radius from the tokens.** No colour by hand.
- **What two applications share** lives in `apps/shared`, a member of the
  workspace too (`@shpyrd/shared`): the ApiRouter, sign-in, permissions,
  the shell.

## API calls

Every call goes through `api`, the ApiRouter. No `fetch` in a screen.

```ts
function pick(): Promise<Api> {
  if (process.env.NEXT_PUBLIC_API_MODE === "mock") {
    mock ??= import("./mock").then((m) => m.mock);
    return mock;
  }
  return Promise.resolve(backend);
}
```

- **`NEXT_PUBLIC_API_MODE`** is `mock` or `backend`. The comparison is
  written as above, against the variable itself: the build replaces it and
  drops the other branch, so a Backend build never contains the Mock.
- **The Mock** answers from JSON files, one per kind of thing
  (`mock/projects.json`), with one generic handler: list, find by name,
  change, remove. A new screen needs a new file, not new code.
- **What a screen changes** is kept in `localStorage`, so it survives a
  reload. The Mock answers after a short wait, so loading states show.
- **Errors** are `ApiError`, with the status and the message of the server.

## Development and production

**Development** is `npm run design` (Mock) or `npm run dev` (Backend):
Next's development server, which shows a saved file at once. It is what a
design session uses, from start to end.

**Production** is `npm run build`, which writes static files to `out/`.
The Go server embeds them (`pkg/ui`, a folder per application, filled by
`make ui`) and serves them with no route declared: a file of the build by
its path, then the page built for the address, then `index.html`. Next
writes scripts into every page, so the server reads each page it embeds
when it starts and sends the hashes of those scripts in its
Content-Security-Policy (`pkg/api/ui.go`).

## Tests

Vitest, in files beside the source, named as sentences about what the
thing does. None is written while designing; they are written when the
session is finished (`design/DESIGN_FOR_AGENT.md`).
