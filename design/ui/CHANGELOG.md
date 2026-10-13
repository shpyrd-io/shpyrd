# Changelog of @shpyrd/ui

A section per version, newest first. Changes not released yet wait under
`Unreleased`; a release turns it into the version's section
(`design/DESIGN_FOR_AGENT.md`, "Release the library"). A change that breaks
something an application uses starts with **Breaking:** and says what the
application has to change.

## Unreleased

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
