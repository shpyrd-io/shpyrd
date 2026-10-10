---
title: Roadmap
description: What is coming - what is decided and what is still a proposal - with a link to the RFC behind every line.
---

Every line of the roadmap is an RFC in the [shpyrd repository](https://github.com/shpyrd-io/shpyrd/tree/main/rfcs) that is not done yet: **in progress** is partly shipped, **ready to implement** is decided and waiting for someone to pick it up, **proposal** still has open questions (each with a default). What is done is in the rest of these docs. Priorities move with feedback in [GitHub issues](https://github.com/shpyrd-io/shpyrd/issues). {% .lead %}

## Platform

| Item | RFC | Status |
| --- | --- | --- |
| Cost visibility | [RFC-0048](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0048-cost-visibility.md) | ready to implement (the enterprise costs feature already shows what the cluster uses and costs) |
| Project quotas | [RFC-0042](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0042-project-quotas.md) | ready to implement |
| Workspaces: the workspace every project, team and person belongs to | [RFC-0033](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0033-workspaces.md) | in progress (control-plane database and the Workspace page in v0.4.0; sign-in at the edge, the `user` role, access modes and the launcher in v0.5.0; login methods, join policy, company domains, the everyone team and suspension in v0.6.0; allow lists v0.7.0; CLIs v0.8.0; the workspace from the host, sign-in at the workspace host, plan limits, the CLI over the API in v0.9.x; an audit in v0.9.10 fixed allow lists, suspension of public apps and backups of every workspace; v0.9.11 keeps the edge's promises: personal tokens open apps, sign-out from an app, key rotation, JSON 401 for API clients, a NetworkPolicy for the server, denial counters; v0.9.11/v0.9.12: native `uuid` identifiers with golang-migrate, image repositories keyed by the workspace id; v0.9.13: workspace roles (owner, admin, member) and invitations accepted by signing in; v0.9.15: per-workspace SSO, the email-first login step for claimed domains, the `reader` role; v0.9.16: address change with redirects, custom workspace domains (CNAME mode), the launcher for everyone with search and featured apps, workspace branding, the MCP server — the remaining gaps (workspace delete, delegated (NS) custom domains, SAML and step-up sign-in on claimed domains, `run`, `globals`, `sizes` and `extensions` without a kubeconfig) are listed in the RFC) |

## Deploying

| Item | RFC | Status |
| --- | --- | --- |
| Shared volumes (`storage-rwx`) | [RFC-0041](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0041-shared-volumes.md) | ready to implement (shared volumes work on EFS or File Storage where the cloud has them, and on one node's disk on the local profile) |
| Private repositories (tokens, deploy keys) | [RFC-0017](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0017-git-credentials.md) | proposal |
| Auto-deploy on push (webhooks, polling) | [RFC-0018](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0018-repository-monitoring.md) | proposal |
| GitHub App: connect once, pick repositories, statuses | [RFC-0054](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0054-github-app.md) | ready to implement |
| Autoscaling mode (min/max, HPA, KEDA) | [RFC-0047](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0047-autoscaling.md) | ready to implement |
| Maintenance mode | [RFC-0020](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0020-maintenance-mode.md) | proposal |
| Run history and scheduled tasks | [RFC-0024](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0024-runs-and-scheduled-tasks.md) | proposal |
| Web terminal | [RFC-0026](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0026-web-terminal.md) | in progress (the Shell tab shipped; the gaps are listed in the RFC) |
| Node.js builds: pnpm, and Next.js 16 on buildpacks | [RFC-0079](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0079-nodejs-builds-pnpm-and-next-16.md) | proposal |
| Builds namespace and enforce-mode Pod Security | [RFC-0043](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0043-builds-namespace-and-pod-security.md) | ready to implement |

## Data stores

| Item | RFC | Status |
| --- | --- | --- |
| Postgres pooling, credential rotation, resize | [RFC-0039](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0039-postgres-pooling-rotation-resize.md) | ready to implement (resize already shipped: `shpyrd pg resize`) |
| Project portability and local storage: portable project archives, moves between nodes, data on the node's disk (the local profile; cloud profiles use block volumes) | [RFC-0081](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0081-project-portability-and-local-storage.md) | ready to implement (local storage, archives and moves already in the console) |
| Redis high availability and metrics exporter | [RFC-0040](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0040-redis-ha-and-exporter.md) | proposal |
| Resource detail pages with their own metrics | [RFC-0028](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0028-resource-pages-and-metrics.md) | proposal |

## Observability

| Item | RFC | Status |
| --- | --- | --- |
| Log storage and history (Loki as an add-on, `--since`) | [RFC-0022b](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0022-log-pipeline.md) | proposal |
| Application metrics v2 (per instance, aggregation, totals) | [RFC-0027](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0027-application-metrics-v2.md) | in progress (shipped; the gaps are listed in the RFC) |
| OpenTelemetry collector and export | [RFC-0029](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0029-opentelemetry.md) | proposal |
| Tracing backend (Jaeger) and a Traces tab | [RFC-0056](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0056-tracing-backend.md) | ready to implement |
| Notifications: webhook, Slack, email | [RFC-0030](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0030-notifications.md) | proposal |
| Audit trail v2 (durable, cluster-wide, export) | [RFC-0025](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0025-audit-trail-v2.md) | proposal |

## Access

| Item | RFC | Status |
| --- | --- | --- |
| kubectl through the platform's sign-in (`shpyrd auth kubectl`) | [RFC-0062](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0062-kubectl-through-platform-sign-in.md) | proposal |
| Dashboard access zones: public dashboard with intranet-only areas | [RFC-0063](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0063-dashboard-access-zones.md) | proposal |
| Signed-in detection on identified apps: a signed-in person is identified from the first page, however they arrive | [RFC-0068](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0068-signed-in-detection-on-identified-apps.md) | proposal |
| Embedded git: a repository per project the platform keeps — deploys commit, pushes deploy, agents work on it | [RFC-0069](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0069-embedded-git.md) | proposal |
| Internal names: `http://crm.internal` between projects, with a service identity | [RFC-0070](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0070-internal-names.md) | proposal |
| AI gateway: model calls through the platform, metered and budgeted per project | [RFC-0071](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0071-ai-gateway.md) | proposal |
| App actions: endpoints an app declares that the platform runs for a person (assistants, launcher) | [RFC-0072](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0072-app-actions.md) | proposal |
| Data in backups: database archives and volumes in the platform backup; workspace data export | [RFC-0073](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0073-data-in-backups.md) | proposal |
| SCIM provisioning: people and teams from the company directory, immediate deprovisioning | [RFC-0074](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0074-scim-provisioning.md) | proposal |
| MFA and passkeys | [RFC-0053](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0053-mfa-and-passkeys.md) | ready to implement |
| Grafana behind shpyrd sign-in | [RFC-0015](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0015-grafana-sign-in.md) | proposal |
| MCP connector for AI agents: every workspace is a remote MCP server with OAuth 2.1 and read tools | [RFC-0032](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0032-mcp-connector.md) | in progress (v0.9.16: remote server, OAuth 2.1, read tools; writing tools, stdio mode still missing) |
| Supply chain and encryption at rest | [RFC-0044](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0044-supply-chain.md) | ready to implement |
