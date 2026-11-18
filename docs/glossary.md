---
id: shpyrd-glo-001
type: glossary
title: Glossary
status: active
date: 2026-11-18
related: [shpyrd-dec-001, shpyrd-spec-001]
---

Terms specific to this repository, in alphabetical order. Terms shared by
several repositories are defined in the workspace and only referenced here.

**Catalog.** The repository of entry folders, published as an index the
server reads, from which projects are made. See shpyrd-spec-001.

**Entry.** One application of the catalog: a folder with a `shpyrd.yaml`
that says what it is, where its code is, how it runs and what it asks.

**Overlay.** The files beside an entry's `shpyrd.yaml`, laid over the
checkout before a Dockerfile build.

**Version (of an entry).** The entry's folder (its digest) together with the
commit of the code, resolved when the catalog was published. Not to be
confused with a *release*, which is the project's own deploy. See
shpyrd-dec-001.
