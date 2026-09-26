# RFC-0027 Application metrics v2

**Status:** implemented, gaps — see Implementation status below

**Owner:** Marcelo Paez Sequeira (PR shpyrd-io/shpyrd#7, branch `rfc-0027-app-metrics-v2`)

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
- Which parameters each chart honours, so that nothing is silently half-applied. `by` and
  `agg` break a chart down per instance, which only CPU, memory and network can do, because
  those are the ones that carry a pod; throughput and latency are measured at the edge and
  ignore both. `process` is broader: CPU, memory and network honour it because they carry a
  pod and therefore a process label, but so does instances, which identifies a process by
  deployment name (`<app>-<process>`) instead of a pod label and constrains its query to that
  one deployment — throughput and latency are still the only charts that ignore it. `mode`
  applies to CPU and memory, the only charts expressed as a proportion of an allocation; the
  rest are always absolute and ignore it.
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
  `replaced 2`, … — never a pod name (RFC-0011). They are hidden behind an "Include replaced
  instances" toggle, off by default, because a week of a frequently deployed project would
  otherwise bury the live instances. The numbering is assigned once per request, after every
  chart has answered, over the union of the pods they returned, in one sorted order. What
  that guarantees is worth stating exactly, because the first implementation claimed more:
  within one response `replaced 2` is the same pod on every chart, and the same data numbers
  the same way on the next poll. What it is not is an order anyone can read meaning into
  (pod names end in a hash, so this is not "order of first appearance"), nor a number that
  survives a change to the set — a replacement sorting earlier renumbers the ones after it.
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
- 2026-09-26: verified on the kind dev cluster (`kind-shpyrd`), against the running project
  `shelltest` (namespace `app-shelltest`) extended with a second process type, `batch` at
  `shared-xs`, alongside `worker` at `shared-s` (two instances each, prebuilt image, two
  genuinely different allocations). After rebuilding and redeploying `shpyrd-server` from
  this branch: `?range=1h` returns the unchanged chart set with series named `worker` and
  `batch`; `?range=1h&by=instance` names series `worker.1`/`worker.2`/`batch.1`/`batch.2`,
  and those names matched the Logs tab exactly for the same pods (checked by writing a line
  to each pod's stdout and comparing `/api/projects/shelltest/logs?format=json`'s `i`/`p`
  fields against the metrics series names); `?range=15m` reports `step: 60`; `?mode=total`
  reports `cores`/`bytes` with `reference` 0.5/67108864 for `worker` and 0.25/33554432 for
  `batch`, CPU series also carrying `burst` (2 and 1); `?by=instance&agg=sum` returns one
  series whose values equal the sum of the per-instance series, with `mode=total` scaling
  `reference` by instance count (1 core / 134217728 bytes for two `worker` instances, 0.5 /
  67108864 for two `batch`); `?by=instance&process=worker` returned only worker instances
  while throughput and latency still returned their (empty, this project has no traffic)
  series rather than an error; deleting a live worker pod and waiting for its replacement
  showed the dead pod as `replaced N` under `replaced=true` while the live instances kept
  their names, and the chart's `note` reported the hidden count without it. Every response
  from all of the above, piped through `grep -c 'shelltest-worker-\|shelltest-batch-'`,
  returned 0. `go test ./...`, `go vet ./...` and `go build ./...` pass; `cd ui && npm run
  build && npm run lint` pass. The Metrics tab itself was verified by UI build and lint
  only, not by rendering it in a browser.
- 2026-09-26: manual test against the live cluster with `?process=worker` on a two-process
  project found two gaps the plan's "carries a pod" test for the process filter had missed.
  The instances chart ignored `process` outright: it groups by `deployment`, and deployments
  are named `<app>-<process>`, so the process was already in the label the chart names its
  series from — the fix constrains the query to that one deployment when `process` is set.
  `instanceCapable` stays false; a replica count has no per-instance breakdown. Separately,
  the network chart's query already filtered correctly but its series stayed named `in`/`out`
  regardless, so the chart looked unfiltered on screen; with a single process selected and
  `by=process` its series are now named `"<process> in"`/`"<process> out"` — `directionOf`,
  `processOf` and `aggregate` were each checked against the new names and are unaffected (see
  `pkg/api/metrics_test.go`; `processOf` itself was removed by the review pass below, which
  is what stopped a series' process being read out of its name at all). The Proposal's "which parameters each chart honours" bullet is
  corrected to match: `process` is not limited to the pod-carrying charts the way `by` and
  `agg` are.

- 2026-09-26: whole-branch review. The API half held — the backward-compatibility
  constraint, no pod name on any path, `-race` clean — and a PromQL injection found earlier
  in the pass was already fixed. Seven findings were fixed in one wave: three of them
  (a replaced instance losing its reference, aggregation blending process types, `replaced N`
  depending on which chart's goroutine reached a shared counter first) came down to reading
  a series' identity back out of its display name, so the process and the pod now travel
  with the series and the naming happens once per request after every chart has answered —
  which is also what the "Instances that no longer exist" bullet above now describes
  accurately. On the UI half, the allocation and burst lines were not rendering at all in
  the common case (Recharts discards a `ReferenceLine` outside the Y domain, and the domain
  was computed from the data alone), a project with two process sizes was told it had no
  allocation set, and the "these controls do not apply" note was keyed off a list of chart
  ids that missed the instances chart instead of off the `instanceCapable` the API already
  sends. The default request's query text and response were diffed against the previous
  commit and are unchanged.

## Implementation status

Audited on 2026-09-26 against the cluster run above, and re-audited after the whole-branch
review. Every gap below was found or named during review and deliberately deferred rather
than fixed on this branch; the list is meant to be complete, since the Status line claims
gaps rather than silence about them.

- **Known limitation, not a bug:** instance names are not stable over a long range — see
  "A known limitation, accepted deliberately" in Design Details, which explains why and what
  it does and does not claim.
- **Not fixed:** the CPU and memory PromQL join `container_cpu_usage_seconds_total` /
  `container_memory_working_set_bytes` to `kube_pod_labels` on `(namespace, pod)` without
  also requiring `label_shpyrd_io_process` to be non-empty, so a running Dockerfile-build
  pod in the same namespace (it carries `label_shpyrd_io_app` but no process label of its
  own process type) contributes its CPU and memory to a project's charts. Found during this
  branch's cluster verification; two symptoms identified during review are what make it
  load-bearing rather than cosmetic. In the default view `sum by (label_shpyrd_io_process)`
  groups the build pod under the empty label value, which the handler renames `all` — a
  phantom series indistinguishable from the no-requests fallback series of the same name. In
  by-instance view the same pod is not an instance under the selector the Logs tab uses, so
  it becomes a `replaced` series and is counted in the note: **every build makes a chart
  claim "1 replaced instance hidden" when nothing was replaced.** Left unfixed because the
  fix changes the default query text (`by=process`, no filters) that an earlier review
  certified byte-identical against a hard backward-compatibility constraint. That
  constraint is worth naming precisely, because it is what blocks the fix: byte-identical
  query text is a proxy for an identical default *response*, which is the guarantee anyone
  actually depends on, and adding `label_shpyrd_io_process!=""` changes the text while
  changing the response only by removing series that should never have been in it. It is
  the proxy that blocks this, not the goal. Fixing it belongs to whichever task next takes
  the constraint itself on deliberately.

- **Not built:** the instance subset. The Summary and the Proposal ask for "Instances (All,
  or a subset)"; what shipped is a process filter plus a by-process / by-instance toggle,
  with no way to isolate one instance — or three — out of forty. On a project at the
  40-instance cap that leaves the charts hardest to read in exactly the case the RFC set out
  to serve, one hot instance among many. Ruled out for now rather than overlooked, and it is
  cheap when it comes: the shape that fits is click-to-isolate on the legend, which needs no
  API change at all, since the API already returns one named series per instance and the
  filtering would be entirely in the browser.
