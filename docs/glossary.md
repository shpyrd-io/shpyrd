---
id: shpyrd-glo-001
type: glossary
title: Glossary
status: active
date: 2026-11-24
related: [shpyrd-dec-001, shpyrd-spec-001, shpyrd-spec-002, shpyrd-spec-003, shpyrd-spec-004]
---

Terms specific to this repository, in alphabetical order. Terms shared by
several repositories are defined in the workspace and only referenced here.

**Builder (of a project).** The image a project that composes its build is
built with, made by the build system from the buildpacks the project uses
and saved in the project's own builder repository,
`<app repository>/builder`. See shpyrd-spec-003.

**Builder pruning.** The removal, by the controller, of the old time tags
of a project's builder repository, so that the registry's garbage
collection can remove the builders no longer in use. See shpyrd-spec-003.

**Catalog.** The repository of entry folders, published as an index the
server reads, from which projects are made. See shpyrd-spec-001.

**Entry.** One application of the catalog: a folder with a `shpyrd.yaml`
that says what it is, where its code is, how it runs and what it asks.

**Moved address.** An address a workspace had before it changed its
address. Requests to it are redirected for a time, and until that redirect
expires the name still belongs to the workspace. See shpyrd-spec-004.

**Overlay.** The files beside an entry's `shpyrd.yaml`, laid over the
checkout before a Dockerfile build.

**Refused host.** A host in a project's `spec.domains` that the project may
not serve: it is owned by another workspace, is a name of the platform, or
is not a valid hostname. See shpyrd-spec-004.

**Registry maintenance.** The time the in-cluster registry is read-only
for a garbage collection, told by a mark the collector writes. Builds wait,
or run again, instead of failing. See shpyrd-spec-002.

**Version (of an entry).** The entry's folder (its digest) together with the
commit of the code, resolved when the catalog was published. Not to be
confused with a *release*, which is the project's own deploy. See
shpyrd-dec-001.

**Verified domain.** A custom domain a workspace has proven it controls.
The domain and every name under it belong to that workspace, unless a name
under it was verified by another. See shpyrd-spec-004.
