---
id: shpyrd-spec-001
type: spec
title: Catalog versions, the record on a project, and updates
status: active
date: 2026-11-18
related: [shpyrd-dec-001]
governs: [pkg/catalog, pkg/api/catalog.go, pkg/api/catalog_update.go, pkg/api/apps.go, internal/cli/catalog.go, internal/controller/status.go, api/v1alpha1/app_types.go]
---

## The index

`index.json` has `version: 1`. Each entry has `name`, `digest` (the hash of
its folder) and an optional `commit`: the full commit (40 or 64 lowercase
hexadecimal digits) its revision named at publish. An index without commits
is read as before: its entries install on their revision and offer no
update. A commit that is not a full commit name makes the index refused, in
words. The commit is not part of the digest.

`publish` resolves each entry's revision with the repository's ref
listing, read without credentials, once per repository. A branch wins over a
tag of the same name, as in the source watcher. If any revision or
repository cannot be resolved, it names every such entry and revision, and
writes nothing. It prints, per entry, the revision and the short commit, and
marks a branch.

## Two records on a project

- **What it is set to deploy** (the target): the annotations of the App for
  the entry, digest, index URL, commit and revision name. An install of an
  entry with a commit and no `--revision` sets the source to that commit and
  records them; otherwise it records no commit.
- **What each release runs**: `status.releases[].catalog` (entry, digest,
  commit, revision). When the controller records a release it fills it:
  - the image equals the image of an earlier release: that release's
    version (the latest such); if the release just before it has a different
    version, the App is not pinned and is set to a newer version of the same
    commit, the target;
  - a new image: the target, only if the image is not pinned, the git
    revision equals the target commit, and the release's source (the short
    commit built) is a prefix of it and not dirty;
  - otherwise none.

The App definition (CRD) must be applied with the controller of the same
version, or the field is dropped.

## What a project says

`catalog` on the project summary and detail: `entry`, `running` (the current
release's version, absent when unknown), `state` and `offered`.

- `up-to-date`: running equals the catalog's current version (commit and
  digest).
- `updating`: not running it, the target is the catalog's current version,
  and the source is that version: the entry's repository at its commit, with
  its sub-path and overlay, and no image pinned. It is rolling out, or its
  build failed. No second offer.
- `update-available`: otherwise, including a project whose source was moved
  by a deploy to another commit or branch of the entry's repository (the
  annotations still name the target, the source does not), before and after
  the controller records a release for it.
- An image pinned on the project ends the offer (empty state) unless a
  rollback pinned it to a release that ran a version of this entry. A deploy
  of an image, a rollback to a release that carries no version of the entry,
  and an image deployed while a rollback was still pending all end it. A
  rollback's pin is told by the rollback annotation (whose release must
  carry a version of the entry and run the pinned image), then by the
  rollback's mark on the App's status, `status.rollbackHold`, which only the
  controller writes (see "The offer after a rollback" below).
  `up-to-date` needs the source to be the version, or the pinned image to be
  the current release's.
- Empty state: the server reads no catalog, or another than the one recorded,
  the entry is gone or has no commit, or the project's source is not the
  entry's git repository. The read never fails and waits at most 2 seconds
  beyond what the fetcher does; the catalog is read once per request.

## Preview and apply

`GET /api/projects/:slug/catalog/update` (view) previews. `POST` (deploy, plus
config, plus resources when it makes any) applies the digest that was
previewed; a stale digest, an unknown input, an input the project already
has, a missing required input, an unavailable resource kind or an overlay
that does not match the index refuse it with nothing changed. A project
already running or rolling out the offered version is left as it is.

Apply does, in order: new volumes and resources; new config vars (an input
that is a config var is skipped; a value held in a snapshot of a release in
the history is taken back; else generated, defaulted, or the value given);
then one update of the App: commit, sub-path, overlay, build, the entry's
processes (command, arguments, port, mounts and health check of existing
ones, whole new ones; replicas, size, sleep and autoscale kept; dropped
ones kept), the entry's plain variables that the project lacks (one it has is kept as it
is, whatever the version declares; the preview lists `envAdded` and, apart,
`envKept`), bindings, the target annotations,
the release note, the deploy request, and `spec.image` cleared. It is
audited as `catalog.update`.

## What a rollback does not restore

A rollback restores the image, the config vars (the live Secret becomes the
snapshot of the release), sizes and bindings of the release. It does not
restore process definitions, build settings or plain variables, and removes
no volume or resource. The version shown follows the release.

## The offer after a rollback

A rollback is recognised by what only the rollback paths write. Both (the
API, and the CLI writing the App with a kubeconfig) set the rollback
annotation, which the offer treats as a rollback until the controller
consumes it. When the controller consumes it, and the image it reconciles is
the image of the release returned to, it writes the rollback's mark on the
App's status: `status.rollbackHold`, with `image` (the image the rollback
pinned) and `release` (the number of the release returned to). It writes the
mark in the same status write as the release, and also when the rollback
records no release (a rollback to a release with the image and the config
of the current one). It still records `rollbackTo` on the release a rollback
asked for; the offer no longer reads it.

The image pinned on the project is held by a rollback when the mark's image
is `spec.image` and the current release carries a version of this entry. No
request can fill the mark, and a release note, which a deploy may set
freely, does not stand in for it. An image pinned any other way (a deploy of
an image) is not held, whichever images and note it had before, and ends the
offer.

The controller removes the mark at the start of a reconcile in which the
mark's image is not `spec.image`: after a deploy of another image, and after
a deploy from source, which clears the pin. A mark that was removed does not
come back when the same image is deployed by hand afterwards.

So the offer after a rollback does not depend on the project's history of 20
releases: the project is offered the update however many changes of config
vars or sizes follow the rollback.

### Projects held before the mark existed

A project that a controller older than the mark left held has `rollbackTo`
on a release and no mark. The controller gives it the mark on its first
reconcile of the project, by the rule the offer used before: the first
release of the run of releases that run the pinned image carries
`rollbackTo`. It applies that rule once per project: `status.rollbackHoldSettled`
records that the project was evaluated, and from then on the release list
never gives a mark.

### What remains

- The mark is settled when the controller reconciles. If an image is
  deployed by hand and then the held image is deployed by hand again with no
  reconcile in between, the controller never sees the change and the project
  still reads as held, on the image the rollback pinned.
- Between the start of a server that reads the mark and the controller's
  first reconcile of a project held before the mark existed, that project
  reads an empty state.
- A project that had already lost the offer, because the release recorded
  for its rollback had left the history before the mark existed, is not
  given it back: it cannot be told from an image deployed by hand.
- A project restored from an archive is pinned to its image with no mark,
  and is offered no update.

What a person sees on a project whose image is pinned and not held: the
entry and the version it runs, with an empty state. No update is offered in
the dashboard or by `shpyrd catalog status`. The preview's `reason` and the
409 of `shpyrd catalog update` and of the apply call begin "the catalog
offers this project no update", and say that an image is pinned on the
project and that deploying a commit of the entry's repository brings the
offer back.

That way out: deploy a commit of the entry's repository to the project by
hand, or roll back again to a release that ran a version of the entry. The
deploy clears the pinned image and keeps the entry's records, so the update
is offered again. It starts a build of that commit without the files the
entry lays over the code, which for an entry with an overlay may fail, so
the update should be applied straight after: it sets the catalog's commit
and the overlay again.

The App definition gains the two status fields, so it is applied with the
controller of the same version: an older definition drops them without an
error.

## The CLI and the dashboard

`catalog list` and `show` show the entry's version; `catalog status
<project>` prints the state and the preview; `catalog update <project>`
previews, asks (`--yes` skips) and applies, with `--input NAME=value`. The
project's Summary shows a Catalog card with the same, and the release list
names the version of each release that has one.
