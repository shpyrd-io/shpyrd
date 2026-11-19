---
id: shpyrd-les-001
type: lesson
title: Recognise a state by a mark only the system writes, not by inferring it from history
status: active
date: 2026-11-19
related: [shpyrd-spec-001, shpyrd-dec-001]
governs: [pkg/api/catalog.go, internal/controller/status.go, internal/controller/app_controller.go, api/v1alpha1/app_types.go]
---

## What happened

The catalog update work had to tell a project held on an older version by a
rollback apart from a project a person moved to an image by hand. The first
is still offered the catalog's version; the second is not.

Each attempt inferred the rollback from something already in the project:

- first from the image history (an earlier image deployed again);
- then from the release note a rollback writes ("Rollback to vN").

Each review found a case the inference got wrong:

- an image deployed again by hand (A, B, A) read as a rollback;
- the deploy endpoint accepts a free release note, so anyone could write the
  rollback's note;
- a rollback to a release that ran a hand-deployed image read as held by the
  catalog.

The task ran out of review rounds twice. It was merged with a known limit:
the offer after a rollback lasts only while the rollback's release is within
the 20-release history (shpyrd-io/shpyrd#371).

## Why it happened

The state "held by a rollback" was never recorded when the rollback happened.
Every later reader had to reconstruct it from data that other actions also
produce: images, notes and release order. A deploy by hand produces the same
data as a rollback, so any reconstruction from it is ambiguous.

The contract stated the outcome to reach, but not where the fact should
come from. Each fix closed the case the last review found, and the next
review found another case.

## What we changed

The controller now writes `rollbackTo` on the release it records for a
rollback, only when it consumes the rollback annotation and the release runs
the returned-to image. `rollbackTo` is a field of `Release` in
`api/v1alpha1/app_types.go`. The API reads that field. It no longer infers
the rollback from images or notes, and the release returned to must carry a
version of the entry. The limit that remains, the 20-release history, is
documented in shpyrd-spec-001 and in the user documentation.

## How to notice next time

A fix starts reading history to decide which action produced the current
state ("the image was deployed before", "the note starts with ..."). Before
writing it, look for a mark that only the action in question writes. If
there is none, add one where the action happens. Do this also when the
history is truncated: a reconstruction from it has a limit the person cannot
see.

## Harness follow-up

A fixed criterion for sensitive tasks could help. When the code decides
which action led to a state, the deciding fact must be written by that
action, and must not be writable through a public endpoint. A calibration
example for the skeptic would show it how to find the ambiguous cases: by
reaching the same state through another action.
