---
title: Roadmap
description: What is done, what is decided and what is still a proposal, with a link to the RFC behind every line.
---

Every line of the roadmap is an RFC in the [shpyrd repository](https://github.com/shpyrd-io/shpyrd/tree/main/rfcs): **done** is merged, **ready to implement** is decided and waiting for someone to pick it up, **proposal** still has open questions (each with a default). Where a done RFC's text still promises something the platform does not do, the line says what is *still missing* (from the implementation audit of 2026-09-25; each RFC has the details in its "Implementation status" section). Priorities move with feedback in [GitHub issues](https://github.com/shpyrd-io/shpyrd/issues). {% .lead %}

## Platform

| Item | RFC | Status |
| --- | --- | --- |
| Local platform: installer, App controller, CLI, dashboard | [RFC-0001](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0001-mvp-local-platform.md) | done |
| Extensions enabled per cluster | [RFC-0002](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0002-extension-model.md) | done (one ServiceAccount per extension; extension health still missing) |
| Published binaries, images and CI (Homebrew, curl installer, ghcr image) | [RFC-0045](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0045-published-binaries-and-ci.md) | done (e2e for `examples/hello-docker` still missing) |
| Object storage extension: Garage in the cluster, a bucket and a scoped key per consumer | [RFC-0046](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0046-object-storage.md) | done |
| Platform backup and restore: nightly encrypted archives to the provider's bucket, `shpyrd cluster restore` | [RFC-0037](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0037-platform-backup-and-restore.md) | done (database and volume contents, release history still missing) |
| Local names and front door: `*.shpyrd.test` via dnsmasq, an existing Caddy on 443 as the front door | [RFC-0057](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0057-local-names-and-front-door.md) | done |
| Project identity: display names, `/projects/<slug>` URLs, no "pod" wording | [RFC-0011](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0011-project-identity-and-product-language.md) | done ("pods" in the logs-agent description still missing) |
| Global config vars for every project | [RFC-0016](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0016-global-config-vars.md) | done |
| Project quotas | [RFC-0042](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0042-project-quotas.md) | ready to implement |
| Cost visibility | [RFC-0048](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0048-cost-visibility.md) | ready to implement |
| Workspaces: the workspace every project, team and person belongs to | [RFC-0033](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0033-workspaces.md) | in progress (control-plane database and the Workspace page in v0.4.0; sign-in at the edge, the `user` role, access modes and the launcher in v0.5.0; login methods, join policy, company domains, the everyone team and suspension in v0.6.0) |

## Deploying

| Item | RFC | Status |
| --- | --- | --- |
| Projects and resources, bindings, attach/detach | [RFC-0003](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0003-projects-and-resources.md) | done (unattached resources in `projects info`; attach confirmation; `Deleting` phase still missing) |
| Dockerfile builds with BuildKit | [RFC-0004](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0004-dockerfile-builds.md) | done (a "build" catalog size; TTL on build Jobs still missing) |
| Shell and one-off commands | [RFC-0005](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0005-shell-and-one-off-commands.md) | done (`shpyrd forward`; `run --process` still missing) |
| Persistent volumes (single-instance) | [RFC-0006](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0006-persistent-volumes.md) | done |
| Shared volumes (`storage-rwx`) | [RFC-0041](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0041-shared-volumes.md) | ready to implement |
| Private repositories (tokens, deploy keys) | [RFC-0017](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0017-git-credentials.md) | proposal |
| Auto-deploy on push (webhooks, polling) | [RFC-0018](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0018-repository-monitoring.md) | proposal |
| GitHub App: connect once, pick repositories, statuses | [RFC-0054](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0054-github-app.md) | ready to implement |
| Health checks and zero-downtime rollouts | [RFC-0019](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0019-health-checks-and-rollouts.md) | done (zero-downtime test; probe message in status still missing) |
| Autoscaling mode (min/max, HPA, KEDA) | [RFC-0047](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0047-autoscaling.md) | ready to implement |
| Maintenance mode | [RFC-0020](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0020-maintenance-mode.md) | proposal |
| Run history and scheduled tasks | [RFC-0024](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0024-runs-and-scheduled-tasks.md) | proposal |
| Web terminal | [RFC-0026](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0026-web-terminal.md) | ready to implement |
| Builds namespace and enforce-mode Pod Security | [RFC-0043](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0043-builds-namespace-and-pod-security.md) | ready to implement |

## Data stores

| Item | RFC | Status |
| --- | --- | --- |
| Postgres (CloudNativePG) | [RFC-0009](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0009-postgres-resource.md) | done (typed-name delete confirmation; storage used still missing) |
| Redis and Valkey | [RFC-0010](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0010-redis-resource.md) | done (PodDisruptionBudget still missing) |
| Postgres backups and point-in-time recovery | [RFC-0038](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0038-postgres-backups-and-pitr.md) | done |
| Postgres pooling, credential rotation, resize | [RFC-0039](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0039-postgres-pooling-rotation-resize.md) | ready to implement |
| Redis high availability and metrics exporter | [RFC-0040](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0040-redis-ha-and-exporter.md) | proposal |
| Resource detail pages with their own metrics | [RFC-0028](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0028-resource-pages-and-metrics.md) | proposal |

## Observability

| Item | RFC | Status |
| --- | --- | --- |
| Structured (JSON) logs in the viewer and CLI | [RFC-0021](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0021-structured-logs.md) | ready to implement |
| Log agent: Vector on every node, project/process/instance labels, bounded node logs | [RFC-0022a](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0022a-log-agent.md) | done (NetworkPolicy for logs-system; console sink off on cloud still missing) |
| Log storage and history (Loki as an add-on, `--since`) | [RFC-0022b](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0022-log-pipeline.md) | proposal |
| Log drains (syslog, HTTPS) to any provider, per project or cluster-wide | [RFC-0023](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0023-log-drains.md) | done (`drain.failing` audit event still missing) |
| Application metrics v2 (per instance, aggregation, totals) | [RFC-0027](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0027-application-metrics-v2.md) | ready to implement |
| OpenTelemetry collector and export | [RFC-0029](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0029-opentelemetry.md) | proposal |
| Tracing backend (Jaeger) and a Traces tab | [RFC-0056](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0056-tracing-backend.md) | ready to implement |
| Notifications: webhook, Slack, email | [RFC-0030](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0030-notifications.md) | proposal |
| Audit trail v2 (durable, cluster-wide, export) | [RFC-0025](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0025-audit-trail-v2.md) | proposal |

## Access

| Item | RFC | Status |
| --- | --- | --- |
| Sign-in with accounts (Dex, local users) | [RFC-0007](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0007-authentication.md) | done (`shpyrd login` for developers; stored OIDC tokens still missing) |
| Teams, roles, RBAC mirror, isolation, audit | [RFC-0008](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0008-teams-roles-and-security.md) | done (admin-only domains; audit export; session rotation; API rate limit still missing) |
| Sign-in experience: shpyrd's own sign-in page, local sign-in, sign-out at the issuer | [RFC-0012](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0012-sign-in-experience.md) | done |
| External identity providers: Okta and any OIDC issuer, GitHub and Google | [RFC-0058](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0058-external-identity-providers.md) | done (`auth connector add` message without auth-local still missing) |
| kubectl through the platform's sign-in (`shpyrd auth kubectl`) | [RFC-0062](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0062-kubectl-through-platform-sign-in.md) | proposal |
| Dashboard access zones: public dashboard with intranet-only areas | [RFC-0063](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0063-dashboard-access-zones.md) | proposal |
| Email delivery | [RFC-0013](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0013-email-delivery.md) | proposal |
| Account lifecycle: invitations, reset, verification, lockout | [RFC-0014](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0014-account-lifecycle.md) | proposal |
| MFA and passkeys | [RFC-0053](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0053-mfa-and-passkeys.md) | ready to implement |
| Grafana behind shpyrd sign-in | [RFC-0015](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0015-grafana-sign-in.md) | proposal |
| Per-user API tokens | [RFC-0031](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0031-api-tokens.md) | done (v0.9.0) |
| API-first CLI and `shpyrd login` | [RFC-0052](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0052-api-first-cli-and-login.md) | done (v0.8.0 `shpyrd login` and `shpyrd-ctl`; v0.9.8 every developer command over the API, `shpyrd use`; `run`, `pg`, `redis`, `domains` and a browser sign-in for the CLI still missing) |
| MCP connector for AI agents | [RFC-0032](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0032-mcp-connector.md) | proposal |
| Supply chain and encryption at rest | [RFC-0044](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0044-supply-chain.md) | ready to implement |

## Cloud

| Item | RFC | Status |
| --- | --- | --- |
| Cloud profiles: Oracle Cloud (OKE) and AWS (EKS) with Terraform for the infrastructure, network policy enforcement, a VPN into the platform on AWS | [RFC-0035](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0035-cloud-profiles.md) | done |
| In-cluster registry with TLS on every profile, garbage collection, Registry card | [RFC-0059](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0059-in-cluster-registry-on-cloud.md) | done (registry NetworkPolicy; `SHPYRD_REGISTRY_KEEP`; `--local-build` still missing) |
| DNS providers: automatic records and one wildcard certificate (OCI DNS; Route 53 and Cloudflare with their profiles) | [RFC-0061](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0061-dns-providers.md) | done (OCI DNS) (DNS card; `--dns none` removal; OCI policy printout still missing) |
| Front doors: internal and external load balancers, exposure per project | [RFC-0036](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0036-load-balancer-exposure.md) | done (`SHPYRD_INTERNAL_LB` semantics; platform-CA certificates for internal projects without a wildcard; `status.exposure` still missing) |
| Custom domains: CNAME or A to the project, per-host certificates | [RFC-0034](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0034-domains-and-certificates.md) | done |
| Volumes on cloud profiles: storage classes, provider minimums, snapshots, shared volumes on File Storage | [RFC-0060](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0060-volumes-on-cloud-profiles.md) | done |

## Decided against, for now

| Item | RFC | Why |
| --- | --- | --- |
| GitOps export as a first-class flow | [RFC-0049](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0049-gitops-export.md) | the platform installs and upgrades itself; `shpyrd cluster export` stays as a plain rendering |
| `git push shpyrd main` | [RFC-0050](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0050-git-push-receiver.md) | `shpyrd deploy` and auto-deploy from the repository cover the workflows in use |
| Agents as a separate kind | [RFC-0051](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0051-agents-and-background-processes.md) | an agent is an app: a worker process, a run or a scheduled task, with everything apps get |
| Environments and promotion | [RFC-0055](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0055-environments-and-promotion.md) | deferred: environments are Git branches deploying to their own projects, pull requests promote |

## Not planned

- Emulating cloud services locally (LocalStack and similar). Environment profiles abstract them instead.
- A hosted control plane. Shpyrd runs inside your cluster.
