# RFC-0066 Release phase

**Status:** implemented

**Owner:** Patrick Negri

**Depends on:** RFC-0004 (implemented), RFC-0005 (implemented)

**Creation date:** 2026-09-27

**Last update:** 2026-09-27

## Summary

A Procfile line `release: bundle exec rails db:migrate` runs before every new release rolls
out, with the release's image and config vars; the rollout waits for it, and when it fails
the previous release keeps serving and the project says why. Heroku's release phase, on the
process type buildpacks already put in the image.

## Motivation

Rails, Django, Phoenix and most frameworks with a database run migrations at deploy time.
Doing it at boot races between instances and couples a failed migration to a crash loop;
doing it by hand (`shpyrd run rails db:migrate` after each deploy) is what people forget.
Heroku settled this with the release phase. Buildpacks give us the definition for free: the
Procfile buildpack turns `release: ...` into a process type of the image
(`/cnb/process/release`). Nothing runs it: that is the platform's job.

### Goals

- Zero declaration: an image with a `release` process type gets the phase.
- Every new release — a build, a config change, a rollback — runs it first.
- A failure is visible where people look (the project's status, the deploy's output, the
  logs) and never takes the running release down.

### Non-Goals

- Pre-boot hooks per instance (that is the app's entrypoint).
- Running the release command on a schedule (RFC-0024 territory).

## Proposal

- After a build, the controller reads the image's process types from the platform
  registry (the `io.buildpacks.build.metadata` label of the image config) and records them
  in `status.processTypes`. Images built otherwise (Dockerfile) declare a release command
  in `shpyrd.yaml`: `processes.release.command`.
- When the (image, configuration) pair about to roll out is not the current release and
  the image has a `release` type (or a declared command), the controller runs it as a Job
  `<app>-release-<target>` in the project namespace — the release's image, config vars,
  bound vars, the platform's variables plus `SHPYRD_RELEASE_PHASE=1`, the `release`
  process's size (else the catalog default), no volumes, one attempt, a 30-minute
  deadline, kept a day for its logs — and waits: phase `Deploying`, message "release
  phase: running /cnb/process/release". The workloads are not touched meanwhile.
- Success: the rollout proceeds and the release is recorded as before. Failure: phase
  `Failed`, message "release command failed (...): fix it and deploy again; its output is in
  `shpyrd logs -p release`"; nothing rolls out; the next deploy gets a fresh Job.
  `status.release` carries the target, state, message and Job name.
- `shpyrd deploy` shows the phase in its output like any status change; `shpyrd projects
  info` lists the process types and a pending or failed release phase; `shpyrd logs
  --process release` shows the command's output (the Job's pod carries the process label).

## Design Details

- Process types are cached per image digest in the controller; a registry the platform
  cannot read (a pinned image elsewhere) yields no types and no phase.
- A declared `processes.release` never becomes a workload; its `size` sizes the Job.
- Superseded release Jobs are pruned when a new target's Job starts.

## Implementation status

Implemented in v0.9.9; v0.9.10 closed the first gaps: the project page shows the phase
while it runs (its output tailed live) and when it fails (the reason, the output, a "Run it
again" button), the Release card lists the image's process types, `shpyrd deploy` streams
the command's output (`release | ...`) while the phase runs, and a redeploy after a failure
runs the command again (the failed Job is replaced by one carrying the request, so one
request retries once). Known gap: images from registries other than the platform's get no
phase unless `processes.release.command` is declared.

## Open questions

1. Run the phase on configuration-only changes? Default: yes, as Heroku does — migrations
   are idempotent and the platform owes the same semantics for every release.

## Implementation History

- 2026-09-27: RFC written and implemented (v0.9.9).
- 2026-09-27: dashboard card, output in the deploy, retry by redeploy (v0.9.10).
