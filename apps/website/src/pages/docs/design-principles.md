---
title: Design principles
description: The choices behind shpyrd and why they were made.
---

These principles come from building the first version of the platform and are recorded in more detail in [RFC-0001](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0001-mvp-local-platform.md). {% .lead %}

## Zero config for applications

A repository with a `go.mod`, a `package.json` or a `pom.xml` is a deployable project. Buildpacks detect the stack, build the image and record the process types; the platform injects `PORT`, publishes `web`, sizes processes with sane defaults. `shpyrd.yaml` exists for the cases that need a word (several process types, sizes, build settings), never as a prerequisite.

## Heroku's vocabulary on Kubernetes objects

Projects, processes, instances (`web.1`), builds, releases, config vars, rollbacks: the model people already know. Underneath, each concept is a plain Kubernetes object in the project's namespace (an `App`, Deployments, Secrets, kpack `Image`s) that `kubectl` shows and GitOps tools can manage. Nothing is hidden in a database.

## Releases are the unit of change

Every deploy, config change and rollback is a numbered release that records its build and its config vars and says what changed (`Set GREETING config var`, `Rollback to v7`). Rolling back restores the whole release, not just the image. Only one release rolls out at a time.

## One binary, no external tooling

The CLI embeds the manifests and drives kind, Helm and Kustomize as libraries. Users need Docker and nothing else; there is no Flux, Terraform or shell script between them and a working cluster. The same manifests can be exported for teams that want GitOps.

## Ordered, observable installation

Platform components are installed in runlevels with explicit readiness conditions rather than "apply and hope". Every step is reported, every component's health can be re-checked, and re-running the installer is an upgrade.

## Abstract the cloud, do not emulate it

Load balancing, DNS, TLS and the registry are provided by an **environment profile**. The local profile uses host ports, a wildcard `nip.io` domain, a development CA and an in-cluster registry; cloud profiles will use the provider's services. No LocalStack-style emulation.

## Secrets are write-only

Config var values can be set, replaced and removed from the CLI and the dashboard, never read back. Names and change times are enough to operate; anyone who truly needs a value has cluster access.

## Show what is happening, in plain words

Build output streams as it happens, rollouts show instances on the new release versus instances still serving, failures show the container's reason (`exec "/cnb/process/worker": no such file`), and the metrics people see first are the ones Heroku, Fly and Render show: throughput by status class, response time percentiles, instances, CPU and memory as a percentage of the allocation.

## Small, reviewable design changes

Architecture changes go through short RFCs in the repository, with decisions, alternatives and an implementation history, so the "why" survives the code.
