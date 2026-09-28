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

Every command must run with this directory as the working directory. Next, the
Markdoc loader and `src/markdoc/search.mjs` all resolve their config and content
relative to the process working directory, so a build started from the repository
root fails — at lint with `Cannot find module 'next/babel'`, or, with lint
skipped, in the Markdoc loader. That is why `make website` and the CI job both
`cd` in first.

## Layout

```
src/pages/index.md        the homepage
src/pages/docs/*.md       the documentation, one file per page, URL follows the filename
src/components/Layout.jsx the page shell and the documentation navigation tree
src/components/           navigation, search, callouts, code fences
src/markdoc/              Markdoc tags, nodes, and the search index builder
src/styles/               Tailwind entrypoint, fonts, syntax highlighting
public/install.sh         served at shpyrd.io/install.sh, the documented install path
public/screenshots/       screenshots used by the docs and the root README
```

## Adding a documentation page

Add `src/pages/docs/<slug>.md` with `title` and `description` front matter, then
add it to the `navigation` tree in `src/components/Layout.jsx`. The search index
picks it up automatically on the next build.

## URLs that must not move

The root `README.md`, the install instructions and published release notes link
to `/install.sh`, `/screenshots/*.png` and `/docs/*`. Add a redirect in
`next.config.mjs` if a path has to change.
