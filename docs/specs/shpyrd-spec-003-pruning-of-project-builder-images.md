---
id: shpyrd-spec-003
type: spec
title: Pruning of project builder images
status: active
date: 2026-11-23
related: [shpyrd-spec-002, shpyrd-les-001]
governs: [internal/controller/builder_prune.go, internal/controller/builder.go, pkg/registry/client.go]
---

## Why

A project that composes its build has a builder of its own, saved in the
repository `<app repository>/builder` of the in-cluster registry. The build
system saves that builder again and again, each time under one more tag,
and nothing removed those tags. Every image a tag names stays in the
registry, and the registry's garbage collection must look up every tag of a
repository for every image in it. With thousands of tags in each builder
repository the collection does not finish within its bound.

The controller therefore removes the old tags of a project's builder
repository. The weekly collection, unchanged, then finds the old builder
images untagged, removes them, and has few tags to look up.

## What is in a builder repository

The build system writes two kinds of tag there:

- two **named tags**: `builder-<namespace>-<builder name>` is the current
  builder, and `builder-<namespace>-<builder name>-run-image` is the
  current run image. They name the same digests as the Builder's
  `status.latestImage` and `status.stack.runImage`;
- **time tags**: 14 digits, `YYYYMMDDhhmmss`, one each time it saves a
  builder whose digest differs from the one under the named tag. Nothing
  reads them back.

The current digest can move between a few values within seconds and return
to an earlier one, so a project can gain tens of time tags a day while its
Builder's definition never changes.

## What is kept

What is in use is read from fields only the build system writes
(shpyrd-les-001), never inferred from the names or the order of tags. The
**keep set** is the digests in:

- the Builder's `status.latestImage`;
- the Builder's `status.stack.runImage`;
- `spec.builder.image` and `spec.runImage.image` of every Build of the
  project that has not finished (its `Succeeded` condition is neither
  `True` nor `False`).

## What is removed

A tag is removed only when all of these hold, checked in this order:

1. its name is exactly 14 digits that read as a date and time;
2. that time, read as UTC, is more than 24 hours in the past. A tag with a
   time in the future stays;
3. the registry resolves it to a digest;
4. that digest is not in the keep set.

Every other tag stays: the two named tags, and a tag of any other shape.
The name narrows what the pass may touch to the one kind of tag known to
be history; whether the image is in use is still decided by the keep set.
Both must hold, so a mistake in one of them alone removes nothing in use.

The pass removes tags, never images. One request removes one tag (a DELETE
on the tag's manifest path) and leaves the image and its other tags. An
image left without a tag can still be pulled by digest until the next
collection. No request goes to a repository other than the project's
builder repository, so the images of the project's releases, which a
rollback needs, are not touched.

The margin of 24 hours keeps a tag on any builder that was the current one
during the last day. It covers a build started on a builder just before
the current one changed.

## When the pass runs

The pass runs in the project's reconcile, after the project's status is
saved. One pass:

- removes at most 50 tags and takes at most 10 seconds;
- asks to run again after 1 minute while tags to look at remain, or after
  a request failed;
- once it has looked at every old time tag, leaves the repository alone
  for 1 hour. The next pass lists it again.

What it remembers between passes is kept in memory. After a restart of the
controller every builder repository is listed once more.

## When nothing is removed

The pass sends no request to remove a tag when:

- the install's registry is external (`Config.RegistryDeletes` is false).
  Then the pass sends no request of any kind;
- the project is being deleted;
- the registry is in maintenance (shpyrd-spec-002);
- the Builder is not ready, its status is of an older definition, or it
  reports no current image or no run image with a digest;
- the current image or the run image is in another host or repository than
  the project's builder repository;
- either named tag does not resolve in the registry to the digest the
  Builder's status gives for it. This happens for a moment while the build
  system saves a new builder; the pass runs again a minute later;
- a Build that has not finished names an image without a digest;
- the Builder or the Builds cannot be read.

A request that fails is logged and tried again by a later pass. It never
changes the project's phase, conditions or message, and never fails the
reconcile.

## A project that stopped composing its build

The controller removes the Builder of a project that no longer composes
its build. Once the Builder is confirmed absent there is no status to
read, and the keep set is the Builds that have not finished. The old time
tags are removed under the same rule. The two named tags stay.

While the Builder still exists, or when it cannot be read for another
reason than being absent, nothing is removed.

## What remains

- The saves that produce the time tags are not stopped. The pass removes
  what they leave, a day later.
- A builder that stopped being the current one shortly before a collection
  keeps its tags for a day, so its image is removed by the collection of
  the week after.
- The two named tags of a project that stopped composing its build, and
  the builder repository of a deleted project, stay in the registry.
- The repositories of build caches and the uploads that never finished
  are not pruned.
- The pass reads the list of tags in one request. A registry that answers
  that list in pages would show it only the first page.
- The first collection after the pruning has more to remove than a later
  one, and may still meet its bound.
