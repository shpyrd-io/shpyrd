# The console application

The console: the operator's application, at the console host. The cluster, the workspaces it hosts, the accounts, the platform's own settings.

It starts as a copy of `apps/example-next` (the site made of `design/ui`),
to be replaced screen by screen by those of `ui/`. What is already its own
is how it runs: against a shpyrd server, in development.

## Run

```bash
npm install                            # at the repository root
npm --prefix apps/console run dev         # http://localhost:4326
SHPYRD_DEV_API=https://shpyrd.shpyrd.test npm --prefix apps/console run dev   # the same: the default
npm --prefix apps/console run build       # static files in out/
```

In development `/api` and `/account` go to `SHPYRD_DEV_API` (by default
`https://shpyrd.shpyrd.test`, the console door of a local kind cluster; `http://localhost:8080`
for a `go run ./cmd/shpyrd-server`). Sessions and the CSRF cookie work
through it. A local cluster's certificates come from the CA of the Caddy that
is its front door, the Homebrew service (`brew services start caddy`); its
root is `/opt/homebrew/var/lib/caddy/pki/authorities/local/root.crt`, and
the `dev` script hands it to Node. Elsewhere, name the file in
`SHPYRD_DEV_CA`. Node warns and goes on when the file is not there, which
is fine against `http://localhost:8080`. The shell and the logs, which are WebSockets, do not: Next's
rewrites carry HTTP only.

## What draws what

| In the page | Component of `design/ui` |
|---|---|
| The areas of the page: side, header, content, pane, foot | `page-layout` |
| The pages of the site, at the side | `nav-list` |
| Where the page is | `breadcrumbs` |
| The title of a text and what explains it | `page-heading` |
| The text itself: headings, lists, tables, links, pictures | `prose` |
| A block of code, with its copy button | `ide` |
| A note or a warning in a text (`{% callout %}`) | `alert` |
| The links of the first page (`{% quick-link %}`) | `card` |
| The parts of a text, beside it | `nav-list`, in a pane |
| The mark, the theme, the menu when narrow | `brand`, `button`, `anchored-overlay` |

## Where things are

- `src/lib/content.ts` reads a Markdoc file and says which tag is drawn by
  what. `CONTENT_DIR` names another folder for the texts.
- `src/components/markdoc.tsx` is the one place that ties a tag of a text
  to a component of the library.
- `src/components/shell.tsx` is what is around every page.
- `src/components/text.tsx` is a text as a page.
- `src/lib/navigation.ts` is the order of the pages, as the site has it.

## What it does not show

An application with data: the ApiRouter, the Mock, the routes read in the
browser. `apps/AGENTS.md` says how those are made.

The pictures of the texts come from `https://shpyrd.io`, where the site
serves them.
