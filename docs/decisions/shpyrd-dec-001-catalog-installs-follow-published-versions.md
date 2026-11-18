---
id: shpyrd-dec-001
type: decision
title: A catalog install stays on the version the catalog published, and updates when a person applies one
status: accepted
date: 2026-11-18
related: [shpyrd-spec-001]
governs: [pkg/catalog, pkg/api/catalog.go, pkg/api/catalog_update.go]
---

## Decision

A project made from the catalog runs a fixed commit of the template, with the
entry's overlay and settings, until the catalog publishes a different
**version** of the entry. Then the project says an update is available, and
a person previews and applies it. No template is read on a timer.

- A **version** of an entry is its folder (the digest, unchanged) together
  with the commit its `source.revision` names when the catalog is published.
  `shpyrd catalog publish` resolves the revision and writes the commit in
  the index. A revision that is a full commit needs no lookup.
- `publish` accepts a branch, and says so in its output: the version is then
  the head of the branch at the moment of publishing. Pinning a tag or a
  commit is the catalog repository's policy, not enforced here.
- In what people read, the catalog's is a **version** of the entry and the
  project's is a **release**.
- The version a project runs is recorded on each release of the project and
  read from the current release, never from an annotation. A release with a
  new build takes the version the App is set to deploy, only when it was
  built from that version's commit; a release that reuses an earlier build
  (rollback, change of config vars) takes that release's version. So a
  rollback shows the older version and offers the update again.
- An update is applied by a person and only adds: it never deletes anything
  and never overwrites a value a person set. It needs the permission to
  deploy and to change config, and to manage resources when it makes one.
- Reapplying an update after a rollback takes back, from the config
  snapshots of the releases still in the history, the value a project had for
  an input the rollback removed, instead of generating or asking again.

## Why

- Reading the template's branch for new code meant a project changed whenever
  upstream did, with nobody deciding. A fixed commit is reproducible: two
  installs of one version build the same code.
- The digest alone misses changes of the template's code, so the commit is
  part of the version.
- The controller already writes each release and handles rollback by both
  paths, so recording the version there needs no new mechanism, and the
  running version is right after a rollback.
- Applying is additive because the entry does not record what an earlier
  version declared, so removing what a later version dropped cannot be done
  safely.

## Alternatives rejected

- Reading the template's branch on a timer: the project would change with
  nobody asking.
- The digest alone, or a tag name with no resolved commit: they miss code
  changes, and tags move.
- Only an annotation on the App for the version: wrong after a rollback.
- Rewriting the annotation from the rollback handlers: there are two
  rollback paths, one of which does not pass through the API.
- An automatic update: it needs a per-project opt-in, a rule for updates
  that need a new input, and a way not to undo a person's rollback. It is
  not available.
