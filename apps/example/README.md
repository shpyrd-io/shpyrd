# An example of a site

A site made of `design/ui`, in Next, compiled to static files. It shows
what the library has for content, with texts that exist: the first page
and the 23 documents of shpyrd.io, loaded from `content/docs/*.mdx`. Nothing is copied.

It is an example to read and to run, not the site. Delete it when
`apps/website` stands on the same base (step 7 of the plan).

## Run

```bash
npm install                                # at the repository root
npm --prefix apps/example run dev     # http://localhost:4327
npm --prefix apps/example run build   # static files in out/
```

## What draws what

| In the page | Component of `design/ui` |
|---|---|
| The areas of the page: side, header, content, pane, foot | `page-layout` |
| The pages of the site, at the side | `nav-list` |
| Where the page is | `breadcrumbs` |
| The title of a text and what explains it | `page-heading` |
| The text itself: headings, lists, tables, links, pictures | `prose` |
| A block of code, with its copy button | `ide` |
| A note or a warning in a text (`<Callout>`) | `alert` |
| The links of the first page (`<QuickLink />`) | `card` |
| The parts of a text, beside it | `nav-list`, in a pane |
| The mark, the theme, the menu when narrow | `brand`, `button`, `anchored-overlay` |

## Where things are

- `src/lib/content.ts` discovers and imports MDX documents.
  `apps/shared/src/editorial` compiles their editorial blocks.
- `src/components/mdx.tsx` is the one place that ties a tag of a text
  to a component of the library.
- `src/components/shell.tsx` is what is around every page.
- `src/components/text.tsx` is a text as a page.
- `src/lib/navigation.ts` is the order of the pages, as the site has it.

## What it does not show

An application with data: the ApiRouter, the Mock, the routes read in the
browser. `apps/AGENTS.md` says how those are made.

The pictures of the texts come from `https://shpyrd.io`, where the site
serves them.
