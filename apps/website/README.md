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
npm --prefix apps/website run build      # every page rendered; /api/contact runs on request
npm --prefix apps/website run lint
npm --prefix apps/website run typecheck
npm --prefix apps/website run test
```

Or `make website-dev` from the root.

## Layout

```
app/page.tsx                 the marketing homepage
app/getting-started/         the builder-to-colleague journey
app/bring-an-app/            the assisted-beta offer
app/docs/[slug]/             every documentation page
src/components/shell.tsx     the chrome; it branches on document or marketing page
src/components/markdoc.tsx   which component of design/ui draws each tag
src/lib/content.ts           reads content/docs and returns front matter, tree and headings
public/install.sh            served at shpyrd.io/install.sh, the documented install path
public/screenshots/          screenshots used by the docs and the root README
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

## Measuring

The site carries Google Tag Manager when the build is given a container:
`NEXT_PUBLIC_GTM_ID`, a build argument in `shpyrd.yaml` (`GTM-NRQD4CPW`;
public, as it is in every page's source). Without it nothing is loaded. The container's tags
decide what is measured; the page pushes `shpyrd_app: "website"` before the
container loads, so the tags know which part of the funnel they are on (the
sign-up says `signup`, the dashboards `console` or `workspace`). Every
"Get started" goes to the sign-up (`src/lib/signup.ts`), which the container
reads as a link click.

## On shpyrd

The site is the project `website` of the platform workspace
(`shpyrd.yaml`). It is built from the repository's root with `Dockerfile`
(and its own `Dockerfile.dockerignore`), since it needs the workspace's
`design/ui` and `content`, into Next's standalone server. The `website`
workflow deploys it after its checks pass on `main`; by hand, from the
root: `cp apps/website/shpyrd.yaml . && shpyrd deploy`.

The redirects (`src/lib/redirects.ts`) are Next's own: `/discord`, `/docs`,
`/how-sharing-works`, the bare `shpyrd.io` to `www.shpyrd.io`, and
`/pricing` to `/pricing/br` for a browser that prefers Brazilian Portuguese
(or where a country header says Brazil).

## The contact forms

`/contact/sales` and `/contact/enterprise` post to `app/api/contact`, the
one route that runs on request. It checks the answers by the forms' rules
(`src/lib/contact.ts`, from `content/site/contact.ts`) and mails them to the
team through Mailgun's SMTP. Its settings are the `website` project's config
vars on shpyrd (`shpyrd secrets set --project website …`):

| Variable | Example |
|---|---|
| `SMTP_HOST` | `smtp.mailgun.org` |
| `SMTP_PORT` | `587` |
| `SMTP_USER` | `postmaster@mg.shpyrd.io` |
| `SMTP_PASS` | Mailgun's SMTP password |
| `CONTACT_FROM` | `shpyrd website <website@mg.shpyrd.io>` |
| `CONTACT_TO` | `sales@shpyrd.io` |

Without them (the development server, a cluster that lacks them) the route answers `503`
and the page offers Discord: nothing is mailed.

## License

[MPL-2.0](../../LICENSE), like the rest of the repository. Every component here
comes from `design/ui`.
