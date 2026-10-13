# Content

The editable words of the website live here as MDX. Layouts, icons, validation,
calculations and rendering code live in the applications. The one-off Small
Software manifesto is maintained entirely in `apps/website`.

## Find the text

| What you want to edit | File |
| --- | --- |
| Changelog heading and navigation labels | `changelog/index.mdx` |
| Resources menu groups and links | `site/resources.mdx` |
| Product updates | `changelog/entries/<date>-<slug>.mdx` |
| Documentation | `docs/<slug>.mdx` |
| Documentation menu | `docs/index.mdx` |
| Feature pages | `site/features/<slug>.mdx` |
| Solution pages | `site/solutions/<slug>.mdx` |
| Solution menu groups | `site/solutions/groups.mdx` |
| Home sections | `site/home.mdx` |
| Calls to action and community links | `site/offer.mdx` |
| Agent setup instructions and downloads | `site/agents.mdx` |
| Getting Started marketing page | `site/sharing.mdx` |
| Plans, questions and explanations | `site/pricing/plans.mdx` |
| Regional rates and minimums | `site/pricing/international.mdx`, `br.mdx` |
| Contact pages, field labels and messages | `site/contact/*.mdx` |
| Shared positioning and boundaries | `site/shared/*.mdx` |
| Missing-page message | `site/not-found.mdx` |

Run `npm --prefix apps/website run dev` from the repository root and open
`http://localhost:4324`. Save a file and the page updates through Next's Fast
Refresh. There is no content-generation command. Existing URLs remain the same.

## Documentation

The small YAML header between `---` lines contains `title` and `description`.
Write paragraphs, headings, lists, links, bold, italics, tables and fenced code
blocks below it. Add a new document to `docs/index.mdx` so readers can find it.
`docs/index.mdx` organizes the menu and does not create an article route.
The filename of each article determines `/docs/<slug>`; keep published filenames stable.

These blocks are available; leave blank lines around their Markdown bodies:

```mdx
<Lead>

An opening paragraph.

</Lead>

<Callout title="Before you start" type="note">

Read the [installation guide](/docs/installation) first.

</Callout>
```

`<AgentSetup />` draws agent setup instructions. `<AddToAgent />` draws the
installer button; `<AddToAgent manual />` also shows the manual-install link.
`<Chat>` holds `<Message from="person">` and `<Message from="agent">` blocks.
Inside an agent message, `<MessageSteps>` holds a Markdown list of completed
steps. See `docs/getting-started.mdx` for a complete example.

## Features and solutions

Copy an existing page in the same folder to start a new landing. The filename
is its address. `layout: landing` selects the shared landing family. `menu`
contains its label, description and order; solutions also name a menu `group`.
The catalog discovers the MDX files automatically.

| Block | What to write |
| --- | --- |
| `Hero` | A `#` heading, one paragraph, an `Action`, optionally a Markdown link |
| `Steps` | A `##` heading, optional introduction, three `###` headings with one paragraph each |
| `Capabilities` | A `##` heading, optional introduction, then `###` headings and paragraphs |
| `Questions` | A `##` heading, then each question as `###` and its answer as a paragraph |
| `Related` | A list of Markdown links |
| `Closing` | A `##` heading, one paragraph and an `Action` |

```mdx
<Action destination="signup">Deploy a project</Action>
```

Actions accept `signup`, `contact`, a site path or a full web address. Ordinary
Markdown links use actual URLs such as `/contact/sales`. The layout determines
where each block appears. Headings and button labels are plain text; paragraphs
support bold, italics, inline code and links. Icons and visual variants belong
in the website, so editing the text cannot accidentally change its styling.

## Copy used by a page with its own layout

Home, pricing and contact forms retain their own compositions. Their MDX files
use `layout: copy`: headings name fields, and the text below each heading is
its value. Nested headings group related fields. A list holds text items; a
table holds short repeated records. `<Record>` separates longer records:

```mdx
# Hero

## Heading

You built it. We ship it.

## Description

Your introduction goes here.

# Links

| Label | Href |
| --- | --- |
| Read the docs | /docs/getting-started |
```

Edit values; keep field headings, identifiers and column names stable because
the page consumes them. Copy fields are plain text, so Markdown emphasis is
not rendered there. Put URLs with query parameters in inline code (backticks) to preserve them literally, without Markdown escapes. A fenced code block preserves multi-line snippets. Numbers,
`true`, `false` and `null` are typed values; put a numeric-looking string in
inline code when it must remain text. Keep price values numeric.

Some text contains placeholders such as `{{starterMinimum}}` or `{{email}}`,
wrapped in inline code. Keep their spelling: the website fills these in from
regional prices or the form. This is substitution, not JavaScript.

## Keep code out of content

Do not add imports, exports, JavaScript expressions, CSS classes or inline
styles. The editorial compiler reports unsupported blocks with their source
location. Types and adapters are in `apps/website/src/content`; the shared MDX
compiler is in `apps/shared/src/editorial`. Neither belongs in this folder.

## Changelog

Add one MDX file per update in `changelog/entries`, named
`YYYY-MM-DD-short-title.mdx`. The front matter requires `title`, `description`,
a quoted `date` (`"2026-10-10"`), and `type`: `feature`, `improvement` or `fix`.
Use `draft: true` to keep an entry out of all public lists and routes.
The full Markdown body is displayed directly in the timeline; `description` is a short editorial summary.
All public copy is in English. Confirm the actual availability date before
publishing; a PR merge date alone does not establish Cloud availability.
The initial entries use provisional merge dates; their PR sources and rollout
review notes are in `docs/website-changelog-sources.md` at the repository root.

The website discovers entries automatically and sorts newest first, with
slug order breaking ties. `/changelog` shows the three most recent entries in
full. Earlier updates appear as summaries grouped by year, initially ten at a
time. “Load older updates” reveals ten more summaries. Each title links to
`/changelog/<slug>`, where the complete MDX body is rendered. The three recent
entries also link to these permanent detail pages. Archive dates omit the year
because the year labels the group beside the timeline rail.
The summary comes from `description`; the detail comes from the same MDX body.
The archive receives summaries only, without the older entries' rendered bodies.
Keep the slug (filename without its date prefix) stable when correcting a date.
Edit the collection heading and navigation labels in `changelog/index.mdx`.
