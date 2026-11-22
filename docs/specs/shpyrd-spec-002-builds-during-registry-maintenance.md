---
id: shpyrd-spec-002
type: spec
title: Builds during the registry's maintenance
status: active
date: 2026-11-22
related: [shpyrd-les-001]
governs: [internal/controller/registry_maintenance.go, internal/controller/registrygc.go, internal/controller/build_failure.go, internal/controller/dockerfile.go, internal/cli/appclient.go]
---

## Why

The garbage collection of the in-cluster registry needs the registry
read-only while the collector runs. A build that tries to save its image in
that window is refused. Such a build used to end as a failed release that a
person had to deploy again. Now it waits, or runs again by itself.

## The mark

"The registry is read-only for a collection" is a fact only the collector
knows, so only the collector writes it (shpyrd-les-001). When it switches
the registry to read-only it sets the annotation
`shpyrd.io/registry-readonly-since` on the registry's Deployment, with the
time of the switch, in the same update that swaps the configuration. The
switch back removes it in the update that swaps the configuration back.

The registry is **in maintenance** when all of these hold:

- the mark is present and reads as a time;
- the mark is younger than the bound, 90 minutes
  (`RegistryMaintenanceBound`: a collection is bounded at one hour, plus the
  rollouts of the two switches);
- the Deployment runs the read-only configuration.

A registry pointed at the read-only configuration by hand has no mark, and
is not in maintenance. A mark older than the bound is a collection that
died, or a registry left read-only, and is not maintenance either. In both
cases builds fail as they do for any other cause, with the same words.

An install whose registry is external is never in maintenance: no collector
runs there. A registry Deployment that is missing or cannot be read counts
as not in maintenance.

## What is held

While the registry is in maintenance the controller starts no build:

- buildpacks: the build's definition is not created, and an existing one is
  not given a new source or a rebuild request;
- Dockerfile: the build Job is not created.

The project's phase is `Building`. Its `Built` condition is `Unknown` with
the reason `RegistryMaintenance`, and its message says that the platform is
in scheduled maintenance and that the build starts by itself. What runs
keeps running. The project is reconciled again on a timer, and the first
reconcile after the switch back starts the build, with no action of a
person.

The reason is the machine-readable sign of this state. A reader that has to
tell it from any other build in progress reads the reason, never the
message.

## What is run again

Holding cannot cover a buildpacks build that was already running when the
switch landed, or one the build system started by itself. When such a build
fails while the registry is in maintenance, in a step that writes to the
registry, the controller marks the failed build with the annotation
`shpyrd.io/build-rerun`:

- the `prepare` step, with the registry's refusal of the write in its
  output;
- the `export` or `completion` step, whatever its output.

A marked build is not a failure of the project: the phase stays `Building`
and the message says the build runs again by itself. Once the registry is
no longer in maintenance, the controller asks for one new build of the same
source and records the time on the mark. It asks once per failed build.

If no new build has started five minutes after it was asked for, the
failure stands, in the words it has for any other cause.

A build that fails in another step, such as the app's own build script, is
not marked and fails as before, also during maintenance.

## The CLI

`shpyrd deploy`, and `shpyrd redeploy` when it builds, wait for a build to
start and then for the release to run, each with a limit. While the project
reports the reason `RegistryMaintenance`, the CLI prints the project's
message once and neither limit counts. The limits apply again when the
reason changes. The exit code keeps its meaning: zero only when the release
runs.

This holds for the waits that go through the workspace's API. The waits of
a CLI that talks to the cluster with a kubeconfig are unchanged.

## What remains

- A Dockerfile build already running when the switch lands, which fails at
  its final push, ends as a failed release. A person deploys again.
- A failure the controller first sees after the maintenance ended stays a
  failure. This needs the controller to restart between the build's failure
  and its next reconcile.
- If the registry is still read-only when the bound passes, a build that
  was waiting to run again is asked for, meets the registry read-only, and
  fails as any build would.
- The window itself is as long as the collector takes. This document does
  not shorten it: builds started in it are delayed until it ends.
