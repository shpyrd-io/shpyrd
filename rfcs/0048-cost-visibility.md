# RFC-0048 Cost visibility

**Status:** replaced by RFC-0075 (usage, billing and sleep), which measures the same usage as one piece of the full economics stack

**Owner:** unassigned

**Depends on:** RFC-0042

**Creation date:** 2026-09-22

**Last update:** 2026-09-30 (replaced by RFC-0075)

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
- 2026-09-30: replaced by RFC-0075, whose usage ledger and OpenCost economics cover this scope.
