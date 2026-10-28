# RFC-0048 Cost visibility

**Status:** implemented (complete, as the enterprise Costs, v0.9.73–v0.9.95: `ee/costs`, Console › Costs, `shpyrd-ctl costs`; the customer's figure is the Billing app's) — see Current decision below

**Owner:** Patrick Negri (shpyrd-io/shpyrd main)

**Depends on:** RFC-0042

**Creation date:** 2026-09-22

**Last update:** 2026-10-28

## Current decision (2026-10-28)

Cost visibility shipped, in another shape than this RFC drew. RFC-0075 absorbed the
proposal on 2026-09-27 (the same measurement, specified through to gross margin); when
billing left the platform on 2026-10-04, the cost half became the enterprise **Costs**
extension (`ee/costs`, on with a license) and the customer half the Billing app's. What
this RFC promised, and where it is:

| This RFC | What exists |
| --- | --- |
| A cost figure per project and per process, from reserved resources over time and volume sizes | Cost lines of three kinds, written every quarter of an hour: `usage` (what the metering loop measured: CPU used, memory reserved, storage, egress, object storage, shared databases), `estimated` (OpenCost's allocation of each pod and volume, at list prices) and `real` (the provider's bill, read from OCI once a day, resource by resource); summed by workspace, project, workload, process, resource type, resource or provider service |
| A cluster-wide price sheet (`SHPYRD_PRICES`: CPU-hour, GiB-hour, GiB-month, currency), edited on the Cluster page; cloud profiles preload a provider's list prices | Not built, by decision: the platform prices nothing since 2026-10-04 (RFC-0075, amendment). The estimate is at OpenCost's list prices, in USD; the real figure is the bill itself, with an OCI API key given under Console › Costs › Cost settings or `shpyrd-ctl costs oci set`. What a customer pays is the Billing app's plan prices; what the platform pays per unit is its cost model (Billing › Settings) |
| `GET /api/projects/{slug}/cost?range=month` | `GET /api/cluster/costs?from=&to=&kind=&group=` (operator, licensed), with filters, search, paging and CSV exports (`POST /api/cluster/costs/exports`). A project's own usage, unpriced, is `GET /api/projects/:slug/usage` (RFC-0075) |
| `shpyrd projects cost shop` | `shpyrd-ctl costs --by project\|process\|resource…` for the operator. No developer-side command: a developer's prices are their workspace's plan, which the platform does not know |
| The Cluster page lists projects by cost | Console › Costs: by project, highest cost first, each project expandable into its workloads, processes and resources; System services, Idle capacity and Unattributed costs told apart; documented in the dashboard docs ("Cluster costs") |
| A Cost card on the project page: this month so far, projected, by process and resource | For the customer, the Billing app's Usage page: this period so far and projected to its end at this pace, by resource and by project, at the plan's prices, from the cost lines the cluster's cost drain posts to Billing (`POST /api/v1/costs`). The workspace app's project page shows storage used against its ceiling, no cost |
| Non-goal: billing, invoices, chargeback exports beyond CSV | CSV exports; and cost drains send every line, as it is written or revised, to any HTTPS receiver (`shpyrd-ctl costs drains add`, Console › Cost drains). The Billing app is one receiver |

Nothing of the text is pending: the price sheet and the developer-side command were
overtaken by the decision that whoever sells the platform prices it outside it, and the
monthly projection is the customer's, in the Billing app.

The proposal below is the historical record.

## Summary

A cost figure per project and per process derived from reserved resources (requests over
time) and volume sizes, priced by a cluster-wide price sheet (per CPU-hour, GiB-hour,
GiB-month of storage), shown on the project page and the Cluster page, with a monthly
projection.

## Motivation

"How much does this project cost" has no answer today; sizes and counts are abstract until
they have a price.

### Goals

- `shpyrd projects cost shop` and a Cost card: this month so far, projected, by process and
  resource.
- Prices editable per cluster; cloud profiles preload a provider's list prices.

### Non-Goals

- Billing, invoices, chargeback exports beyond CSV; actual cloud bills (usage-based
  services like load balancers are estimated, not metered).

## Proposal

- `SHPYRD_PRICES` ConfigMap: `cpuHour`, `memoryGiBHour`, `storageGiBMonth`, `currency`; the
  Cluster page edits it.
- Metering from Prometheus: `kube_pod_container_resource_requests` summed per project and
  process over time, `kubelet_volume_stats_capacity_bytes` for volumes, database and cache
  sizes from their specs.
- API `GET /api/projects/{slug}/cost?range=month`; CLI `shpyrd projects cost`; the Cluster page
  lists projects by cost.

## Implementation History

- 2026-09-22: RFC written.
- 2026-09-27: absorbed by RFC-0075 (usage, billing and sleep): the same measurement from
  the cluster, specified from raw usage to gross margin; the price sheet became plan prices.
- 2026-10-04 (v0.9.73, #96): billing left the platform. The cost half became the
  enterprise Costs extension (`ee/costs`): usage, estimated (OpenCost) and real (OCI) cost
  lines, Console › Costs and Cost drains, `shpyrd-ctl costs`, the drains' delivery to an
  HTTPS receiver. The Billing app's cost sink (`POST /api/v1/costs`) landed the same day:
  the lines become usage records at plan prices and the platform's COGS.
- 2026-10-05 (v0.9.74, #98): lines every quarter of an hour; the drains name projects by
  id and name.
- 2026-10-14 (v0.9.95, #197): the cost explorer: by workspace, project, workload, process,
  resource type, resource or provider service; categories (project, system, idle,
  unattributed); search, paging, CSV exports; the OCI key set from the console.
- 2026-10-28: closed as implemented, complete; the Current decision section records where
  each promise landed.
