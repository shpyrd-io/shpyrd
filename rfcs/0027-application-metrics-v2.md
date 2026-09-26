# RFC-0027 Application metrics v2

**Status:** in progress

**Owner:** Marcelo Paez Sequeira (branch `rfc-0027-app-metrics-v2`)

**Depends on:** RFC-0011

**Creation date:** 2026-09-22

**Last update:** 2026-09-26

## Summary

The Metrics tab gains per-instance series, an instance filter, aggregation (none, sum,
average, max), a percentage/total toggle showing the process allocation, a time range
picker and per-process views. Instances are named `web.1`, never by pod.

## Motivation

The current charts show one line per process; a hot instance or an uneven load balance is
invisible, and tooltips leak pod names.

### Goals

- Compare instances, spot outliers, read absolute values against the allocation.
- Ranges from 15 minutes to 7 days with adequate resolution.

### Non-Goals

- Custom application metrics (RFC-0029) and alerting (RFC-0030 events).
- Per-instance throughput and latency: those are measured at the edge and cannot be
  attributed to an instance (see Design Details).
- Workspace-level usage against plan limits (RFC-0033). Those limits are totals for a
  whole workspace — projects, instances, CPU, memory, storage — not a per-process ceiling,
  so they are not a reference line on these charts.

## Proposal

- API `GET /api/projects/{slug}/metrics?range=15m|1h|6h|24h|7d&process=web&by=process|instance&agg=none|sum|avg|max&mode=percent|total`.
  `by` defaults to `process`, so the call the dashboard makes today keeps returning what it
  returns today; every new parameter is additive. `process` absent means all process types.
  `agg` applies only to `by=instance`: `none` draws one series per instance, the others
  collapse them to one. Passed with `by=process` it is ignored rather than refused, since
  the aggregation is already implied.
- Only CPU, memory and network can be broken down by instance. Each chart reports whether
  it can (`instanceCapable`), and the UI disables the instance controls on the charts that
  cannot rather than accepting a parameter it will ignore.
- Which parameters each chart honours, so that nothing is silently half-applied: `process`,
  `by` and `agg` apply to CPU, memory and network, which carry a pod and therefore a
  process; throughput and latency are measured at the edge and ignore all three. `mode`
  applies to CPU and memory, the only charts expressed as a proportion of an allocation;
  the rest are always absolute and ignore it.
- Each **series** carries a `reference`: the allocation it is measured against in absolute
  mode (for example 64 MiB for a `shared-s` process), plus a `burst` when the size sets a
  higher CPU ceiling. It cannot live on the chart, because one chart draws several
  processes and they may have different sizes. In percentage mode neither is set: the line
  is 100%.
- UI controls: Instances (All, or a subset), Aggregation, Percentage / Total. Release
  markers stay.
- Percentage is relative to the process's request, which is the allocation the project pays
  for; shared sizes may legitimately exceed 100%.
- Time range: 15m, 1h, 6h, 24h, 7d with the step chosen by range.

## Design Details

- PromQL groups by `pod`: `container_memory_working_set_bytes{namespace=…}` joined to
  `kube_pod_labels` on `(namespace, pod)` to pick up `label_shpyrd_io_app` and
  `label_shpyrd_io_process`, aggregated with `sum by`, `avg by` or `max by` when `agg` asks
  for it. Verified against the dev cluster: `kube_pod_labels` does carry both labels
  together with `pod`, so the join this rests on works rather than being assumed.
- **Instance names are resolved in Go, not in PromQL.** `kube_pod_annotations` exposes
  nothing on a default install (kube-state-metrics needs an annotation allowlist), so the
  `shpyrd.io/instance` annotation cannot be joined in a query. The handler maps `pod` to a
  name with the same `logs.InstanceNames` the log viewer uses, so the Logs and Metrics tabs
  cannot disagree.
- **Instances that no longer exist.** A pod in the metric data but absent from the live pod
  list was replaced during the window. Each becomes its own series labelled `replaced 1`,
  `replaced 2`, … in order of first appearance — never a pod name (RFC-0011). They are
  hidden behind an "Include replaced instances" toggle, off by default, because a week of a
  frequently deployed project would otherwise bury the live instances.
- **A known limitation, accepted deliberately.** Instance names are not stable over time:
  `labelRunningInstances` recomputes them from the live pod list on every reconcile, so when
  `web.1` goes away the surviving `web.2` is renamed `web.1`. Over a long range a series
  labelled `web.1` may therefore have been a different pod earlier in the window. Making
  ordinals stable was considered and rejected for now: it would change the naming contract
  RFC-0022a's log labels depend on, and touches the four callers that compute names from the
  live list (the controller, `pkg/api/logs.go`, and two CLI paths). Precisely: a name is
  reliable for an instance that existed for the whole window, and the `replaced` series are
  what make the churn visible when one did not. The charts do not claim more than that.
- Throughput and latency come from `nginx_ingress_controller_*`, whose series identify the
  ingress controller's own pod (`controller_pod`) and the backend *service*, never the
  backend pod. Per-instance request rates would need application-side instrumentation,
  which is RFC-0029.
- The reference line is the request, not a limit. For memory every size sets
  `Limits.memory == Requests.memory`, so there is nothing to choose between the two. For CPU
  a shared size's limit is `BurstFactor` (4) times its request — every shared size has one,
  unconditionally — so labelling that line "Limit" would tell someone their ceiling is 2
  cores when they bought 0.5: it is a burst ceiling, not the allocation. That is why it is
  drawn separately as `burst` and why the reference stays the request. Dedicated sizes set
  the CPU limit equal to the request, which is why `burst` is zero for them. A practical
  consequence worth stating plainly: because every shared size carries that 4x CPU limit, a
  CPU chart in total mode for a shared-size process always draws a burst line — not an
  occasional extra one.
- Remove pod names from every tooltip and legend (RFC-0011).
- `metrics.go` splits: the handler and the response types stay, and the chart table with its
  PromQL builders moves to `metricqueries.go`. `by` × `agg` × `mode` multiplies query
  variants, and the table is worth keeping declarative rather than branching inside the
  handler.
- Metrics have no tests today. `PromClient` is `{BaseURL, HTTP}`, so pointing it at an
  `httptest.Server` serving canned Prometheus JSON tests query construction, instance
  naming, the replaced-instance path, aggregation and the percent/total switch without a
  cluster. This change is mostly query construction, which is what fails silently.

## Implementation History

- 2026-09-22: RFC written.
- 2026-09-26: picked up. Settled while designing, each grounded against the dev cluster
  rather than assumed: per-instance cannot cover throughput or latency, because edge metrics
  carry no backend-pod label; instance naming happens in Go, because `kube_pod_annotations`
  is empty on a default install; replaced instances get their own labelled series, hidden by
  default, and instance names are knowingly unstable over long ranges; and the reference
  line is the allocation rather than the "limit" the first draft named, since a shared size's
  CPU limit is a 4x burst ceiling, not the allocation, and would mislabel what someone
  actually bought.
