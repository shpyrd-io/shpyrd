# RFC-0054 GitHub App integration

**Status:** in progress

**Owner:** Marcelo Paez Sequeira

**Depends on:** RFC-0017 (implemented), RFC-0018 (implemented)

**Creation date:** 2026-09-22

**Last update:** 2026-10-16

## Summary

"Connect GitHub" in the dashboard: a GitHub App installed on an organisation or account
gives repository access without personal tokens, a repository picker when creating a
project, automatic webhooks, and commit statuses/deployment records back on GitHub.

## Motivation

Tokens and deploy keys work but are per person and per repository; a GitHub App is what
teams expect.

### Goals

- Install once, pick repositories from a list, private repositories build, pushes deploy,
  GitHub shows the deployment status.

### Non-Goals

- GitLab/Bitbucket apps (tokens and webhooks from RFC-0017/0018 cover them).

## Proposal

- Cluster-level app registration (`shpyrd github app create` prints the manifest flow URL)
  storing app id and private key in a Secret; installation through GitHub's flow returning
  to `/api/github/installed`.
- Installation tokens minted on demand for builds (short-lived, injected as RFC-0017
  credentials), webhooks registered automatically for RFC-0018, commit statuses
  (`shpyrd/build`, `shpyrd/deploy`) and GitHub Deployments per release.
- Dashboard: New project → "From GitHub" with a repository and branch picker.

## Why one App per cluster

GitHub delivers webhooks and installation callbacks to a single URL, so a shared app would
need a hosted relay. Each cluster registers its own App through GitHub's manifest flow: the
admin clicks once, GitHub creates the App with the cluster's URLs and returns the
credentials; developers then only "Connect GitHub".

## Implementation History

- 2026-09-22: RFC written.
- 2026-10-16: part 3, Connect GitHub: `shpyrd-ctl github app create` registers the cluster's App from a manifest; workspaces connect installations (only an owner of the GitHub account can connect it) from the dashboard's Git settings, and `shpyrd github connect` opens that page and waits; From GitHub in New project; builds and the source watcher clone with repository-scoped installation tokens before deploy keys and tokens; the App's webhook routes pushes; installation suspensions and removals are recorded. See docs/superpowers/specs/2026-10-16-github-app-connect-design.md. Part 4 (checks, Deployments, watch paths, wait-for-CI) follows.
- 2026-10-16: part 3 (Connect GitHub) verified end to end on a kind cluster against a real GitHub App on a personal account: registration with shpyrd-ctl, connect (fresh install and an installation that already existed), the repository picker, private builds through the App (kpack and Dockerfile), pushes through the App's webhook deploying two projects at once, a repository not granted refused, suspend and unsuspend. Part 4 (checks, Deployments, watch paths, wait for CI) is next.
