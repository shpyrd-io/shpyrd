---
id: shpyrd-les-002
type: lesson
title: Compare repository URLs with sameRepository, never as strings
status: active
date: 2026-11-19
related: [shpyrd-spec-001]
governs: [pkg/api/catalog.go, pkg/api/catalog_update.go, pkg/api/github_access.go, pkg/api/apps.go]
---

## What happened

A project installed from the catalog that a person deploys to another
commit of the entry's repository should be offered the catalog's version
again. That worked only when the URL was spelled exactly as in the catalog
entry. With a `.git` suffix, another letter case or a trailing slash, the
deploy was accepted but the project lost its catalog state. Applying the
update was then refused, wrongly saying that the source was no longer the
entry's repository.

The way out documented for another limit is to deploy a commit of the
entry's repository by hand. That failed too for anyone who pasted GitHub's
clone URL, which ends in `.git`.

This was the last failing criterion of the catalog releases task, which
reached the end of its review rounds before it was fixed.

## Why it happened

The deploy endpoint already decided "same repository" with `sameRepository`
(`pkg/api/github_access.go`). It parses both URLs and compares host and
path. The new catalog code compared the URLs with `!=`, so two parts of the
API disagreed on what the same repository is.

The tests used the fixture's URL in its single spelling, so they could not
tell the two comparisons apart. The difference was found only when the
review deployed the clone URL through the real endpoint.

## What we changed

`catalogLookup.of` and `sourceIsVersion` in `pkg/api/catalog.go` now use
`sameRepository`. Applying an update writes the entry's URL into the
project's source, so the project ends on the catalog's spelling. A test
deploys through `POST /api/projects/<slug>/deploy` with the `.git`, upper
case and trailing-slash spellings. The existing test that a different
repository gets no offer still passes.

## How to notice next time

A new `==` or `!=` between two repository URLs, or between a URL and a
stored one, in `pkg/api` or `pkg/catalog`. Use `sameRepository` instead.
Where the result must be canonical, write the canonical URL rather than
keeping the one the person typed. A test about a repository should use at
least one spelling other than the fixture's.

## Harness follow-up

A calibration example for the skeptic: when a criterion depends on
recognising an identifier given by a person (a URL, a name, an email), try
the other spellings the system accepts elsewhere before judging the
criterion met.
