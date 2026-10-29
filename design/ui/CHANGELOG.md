# Changelog of @shpyrd/ui

A section per version, newest first. Changes not released yet wait under
`Unreleased`; a release turns it into the version's section
(`design/DESIGN_FOR_AGENT.md`, "Release the library"). A change that breaks
something an application uses starts with **Breaking:** and says what the
application has to change.

## Unreleased

- `LauncherCard` has a `maintenance` phase: a wrench in the warning colour beside the icon, "Under maintenance" in words, for an application behind its maintenance page (RFC-0020).
- `Slider`: a number, or the two bounds of a range, chosen by dragging along a line or with the arrow keys; one thumb per value, each named with `thumbLabels`.
- A `TimeChart` marker takes a `tone` and a `title`: an out-of-memory kill is drawn in its own ink, apart from the releases, and says what happened to who points at it. Markers outside the drawn time are no longer drawn.
- `StatePage` takes any node as its `title`, not only text: the prop no longer meets the HTML `title` attribute's type.
- A select's trigger shrinks with its grid column and clips its value, instead of growing past the column and out of its dialog (a long instance size did).
- An inline progress bar writes its label before itself: a count over a limit with a thin bar beside it, as a table cell holds it.
- `LauncherCard` takes `access="mixed"` for an application that asks for a sign-in on some paths only: a half shield beside the network sign, "Public, sign-in on some paths", instead of the lock.

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
