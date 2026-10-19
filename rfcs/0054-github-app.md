# RFC-0054 GitHub App integration

**Status:** implemented

**Owner:** Marcelo Paez Sequeira

**Depends on:** RFC-0017 (implemented), RFC-0018 (implemented)

**Creation date:** 2026-09-22

**Last update:** 2026-10-17

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
- 2026-10-16: part 3, Connect GitHub: `shpyrd-ctl github app create` registers the cluster's App from a manifest; workspaces connect installations (only an owner of the GitHub account can connect it) from the dashboard's Git settings, and `shpyrd github connect` opens that page and waits; From GitHub in New project; builds and the source watcher clone with repository-scoped installation tokens before deploy keys and tokens; the App's webhook routes pushes; installation suspensions and removals are recorded. See docs/superpowers/specs/2026-10-16-github-app-connect-design.md. Part 4 (checks, Deployments, watch paths, wait for CI) is the entry below.
- 2026-10-16: part 3 (Connect GitHub) verified end to end on a kind cluster against a real GitHub App on a personal account: registration with shpyrd-ctl, connect (fresh install and an installation that already existed), the repository picker, private builds through the App (kpack and Dockerfile), pushes through the App's webhook deploying two projects at once, a repository not granted refused, suspend and unsuspend. Part 4 (checks, Deployments, watch paths, wait for CI) is the entry below.
- 2026-10-17: part 4, checks and Deployments: a `shpyrd / <project>` check run on each commit (building, deploying, running at the URL or failed; waiting for CI; not deployed; skipped; superseded or replaced) and a GitHub Deployment per release that changes the code, written by a reporter loop that keeps what it sent in an annotation and never holds up a deploy; wait for CI (every check run and status except shpyrd's own; no CI after a minute; given up after an hour; a re-run of a failed head commit deploys) through `check_suite` and `status` webhooks with a 30-second fallback; watch paths (gitignore-style globs from the repository root, last match wins, GitHub's compare from the running commit); per-scope installation tokens. RFC-0054 is implemented. See docs/superpowers/specs/2026-10-16-github-checks-design.md.
- 2026-10-17: part 4 verified end to end on a kind cluster against the test GitHub App and paezao/shpyrd-e2e-hello, webhooks through an ngrok tunnel limited to POST /api/github/webhook: the check and the Deployment of a push, wait for CI with a GitHub Actions workflow (pass, fail, green re-run), watch paths (a README push ignored, the next matching push deployed), and a GitHub outage leaving the deploy untouched.
- 2026-10-18: GitHub access by person, and webhooks before polling: each person links their own GitHub account (refresh token sealed with AES-256-GCM, access token in memory only), the picker lists only repositories the person can read, and create, deploy with a changed URL and archive restore check that; repositories the App covers are never polled (a sweep every 5 minutes redelivers failed deliveries), other hosts are polled every 5 minutes (was 2) under a cluster-wide rate cap until their own webhook delivers; the wait-for-CI fallback recheck is 2 minutes (was 30 seconds). See docs/superpowers/specs/2026-10-17-github-user-link-and-polling-design.md.
- 2026-10-19: no polling at all, as on Vercel: a repository on another host deploys on push once its own webhook is set up, and until then when someone deploys (`SHPYRD_GIT_POLL_INTERVAL` and `SHPYRD_GIT_POLL_RATE` are gone). Linking your GitHub account moved out of the person menu into the New project picker.
