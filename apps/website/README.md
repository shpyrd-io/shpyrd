# shpyrd.io

The site and the product documentation. A Next application compiled to static
files, drawing with [`design/ui`](../../design/ui) and reading
[`content/`](../../content).

## Running it

Every command runs from the repository root: the applications are members of
one npm workspace, and Next does not import from outside its own folder
without it.

```sh
npm install
npm --prefix apps/website run dev        # http://localhost:4324
npm --prefix apps/website run build      # static files in out/
npm --prefix apps/website run lint
npm --prefix apps/website run typecheck
npm --prefix apps/website run test
```

Or `make website-dev` from the root.

## Layout

```
app/page.tsx                 the marketing homepage
app/how-sharing-works/       the builder-to-colleague journey
app/bring-an-app/            the assisted-beta offer
app/docs/[slug]/             every documentation page
src/components/shell.tsx     the chrome; it branches on document or marketing page
src/components/markdoc.tsx   which component of design/ui draws each tag
src/lib/content.ts           reads content/docs and returns front matter, tree and headings
public/install.sh            served at shpyrd.io/install.sh, the documented install path
public/screenshots/          screenshots used by the docs and the root README
public/_redirects            /docs -> /docs/getting-started; a static export emits none
```

The texts are not here. Documentation is `content/docs/*.md` and the marketing
copy is `content/site/*.ts`; see [`content/README.md`](../../content/README.md).

## Adding a documentation page

Add `content/docs/<slug>.md` with `title` and `description` front matter, then
add it to the navigation in `content/navigation.ts`. A test fails if the two
disagree, and the route and the metadata follow automatically.

## What this must not break

The root `README.md`, the install instructions and published release notes link
to `/install.sh`, `/screenshots/*.png` and `/docs/*`. Those addresses keep their
exact shape.

## License

[MPL-2.0](../../LICENSE), like the rest of the repository. Every component here
comes from `design/ui`.

The commercial Tailwind UI *Syntax* template, and the carve-out that covered it,
live with the previous site in `apps/website-old/` until a deploy proves this
one and it is removed.
