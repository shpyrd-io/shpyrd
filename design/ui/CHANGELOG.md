# Changelog of @shpyrd/ui

A section per version, newest first. Changes not released yet wait under
`Unreleased`; a release turns it into the version's section
(`design/DESIGN_FOR_AGENT.md`, "Release the library"). A change that breaks
something an application uses starts with **Breaking:** and says what the
application has to change.

## Unreleased

- Add `MetricChart` with 30 visualization kinds extracted from Warehouse, exact
  decimal values, missing-data handling, accessible legends and an icon-only
  table toggle. Chart styles ship with the library stylesheet.
- Group chart gallery pages in an alphabetical Charts section, with separate
  vertical and horizontal funnel examples. Preserve existing component APIs
  and former gallery URLs; use the existing `Stat` and `Sparkline` for KPIs.

## 0.1.1-0

A prerelease, under the `next` tag, with nothing changed in the library:
the first version published through npm's trusted publishing, with no
token. `latest` stays 0.1.0.

## 0.1.0

The first published version: the library as it is in shpyrd, and
`CodeInput` (`@shpyrd/ui/components/code-input`), from shpyrd-signup: a
short code typed or pasted, a digit a box.
