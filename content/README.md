# content

What is written: the documentation, the pages and the texts of the site.

- `docs/` — one Markdoc file per documentation page. The address follows the
  filename, and those addresses do not move: the root `README.md`, the install
  instructions and published release notes link to them.
- `site/` — the marketing copy as typed data, because it is not prose: the
  boundaries are claim-and-limit pairs and the workspace roster is rows.
- `navigation.ts` — the order of the documentation, beside the texts it names.

`apps/website` reads `docs/` from disk and imports the rest as
`@shpyrd/content`.
