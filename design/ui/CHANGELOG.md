# Changelog of @shpyrd/ui

A section per version, newest first. Changes not released yet wait under
`Unreleased`; a release turns it into the version's section
(`design/DESIGN_FOR_AGENT.md`, "Release the library"). A change that breaks
something an application uses starts with **Breaking:** and says what the
application has to change.

## Unreleased

- A select's trigger shrinks with its grid column and clips its value, instead of growing past the column and out of its dialog (a long instance size did).
- An inline progress bar writes its label before itself: a count over a limit with a thin bar beside it, as a table cell holds it.
- `LauncherCard` takes `size="sm"`: a smaller card (192×144) for a row of others, without its description and its tags.
- `LauncherCard` draws its signs (local network, locked) in the band, before the address; the top corner holds only the gear.
- A web application's `LauncherCard` that is failed or deploying says so with a badge beside the gear, "Not working" or "Updating"; a worker keeps saying how it is in its band.

## 0.1.2

- Add `MetricChart` with 30 visualization kinds extracted from Warehouse, exact
  decimal values, missing-data handling, accessible legends and an icon-only
  table toggle. Chart styles ship with the library stylesheet.
- Group chart gallery pages in an alphabetical Charts section, with separate
  vertical and horizontal funnel examples. Preserve existing component APIs
  and former gallery URLs; use the existing `Stat` and `Sparkline` for KPIs.
- **Breaking:** `Changelog` no longer renders heading, description, or actions props. Compose the page heading and actions outside the timeline.
- Changelog entries use rounded Tile markers; `ChangelogSummary` and `ChangelogArchive` provide linked summaries grouped by year with progressive reveal.
- Mobile `SiteHeader` keeps columns inside expandable parent menus and standalone links independent; expanding a group no longer closes the menu.

## 0.1.1-0

A prerelease, under the `next` tag, with nothing changed in the library:
the first version published through npm's trusted publishing, with no
token. `latest` stays 0.1.0.

## 0.1.0

The first published version: the library as it is in shpyrd, and
`CodeInput` (`@shpyrd/ui/components/code-input`), from shpyrd-signup: a
short code typed or pasted, a digit a box.
