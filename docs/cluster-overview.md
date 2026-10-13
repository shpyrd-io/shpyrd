# Cluster overview operations and limits

The console's cluster overview reads `/api/cluster/overview`, not the legacy
project-counting cluster metrics path. It groups nodes by `shpyrd.io/pool` and
keeps host utilization separate from requests divided by allocatable capacity.
Unlabeled nodes remain a separate group. Missing samples are never zero-filled.

## Enabling metrics on an existing installation

Deploy the updated server and console, then reconcile the `monitoring` component
with the updated CLI/manifests using the existing installation context and vars:

```sh
shpyrd cluster init --context <cluster-context> --only monitoring
```

This adds `shpyrd-cluster-overview` recording rules and permits the pool label in
kube-state-metrics. Wait for both 30-second rule groups to evaluate. Until then,
the console reports unavailable telemetry; it does not fall back to querying
raw pod inventory. Check both rule-group health and exporter availability.
No deployment is performed by building the console.

## Data and interpretation

- CPU busy uses physical host cores. Memory used uses host total minus available
  memory. Requests use Kubernetes allocatable CPU/memory as their denominator.
- Scheduler pod request metrics are preferred. When unavailable, kube-state-metrics
  supplies the maximum of summed regular containers and the largest init container,
  plus pod overhead, for Running/Pending assigned pods. That fallback is an estimate
  for native sidecars and pod-level resources; unrequested capacity is not a promise
  that a workload can be placed. DaemonSets consume requests and pod slots too.
- Pool percentages weight only nodes reporting both numerator and denominator.
  Coverage accompanies each percentage. Current thresholds are informational;
  CPU activity alone is not a pressure condition.
- Storage rows identify the node's filesystem with least available percentage.
  Used = size - free; available excludes filesystem reservations. Node filesystem
  capacity is not persistent-volume capacity. Filesystem capacities are not summed.
- Host disk I/O includes sd/vd/xvd and NVMe whole disks, excluding partitions,
  loop devices and device-mapper layers. Interface totals include eth/en names,
  excluding loopback, bridges, veth and bond aggregate interfaces. Other device
  naming schemes need an explicit collector/query policy before inclusion.
  Network totals include internal traffic and are not bandwidth utilization or
  cluster ingress/egress. Device latency divides time by completed operations;
  idle devices have no latency sample, not a zero-millisecond measurement.
- Platform placement diagnostics inspect only installed component namespaces,
  exempt DaemonSets/provider DNS and explicit pool selectors, and stop at 5,000
  scanned pods. Partial findings are explicitly marked. Tenant project namespaces
  and cluster-wide pod inventory are not enumerated by the overview.

## Request and collection budgets

Node inventory is loaded in Kubernetes chunks of 500 into a shared immutable
snapshot. A request may initiate one background refresh every 30 seconds; concurrent
viewers share that work and read the previous snapshot while it runs. A refresh has
20 seconds to finish. Ten snapshots are retained for pagination. This is a bounded
lazy cache, not a continuously running informer. Collection remains proportional
to node count and rule evaluation still processes pod metrics once per interval.

The browser receives pool summaries, up to ten initial attention rows, and a page
of 50 nodes (hard maximum 100). Search, filtering and sorting happen before slicing
the page. Later pages require their original snapshot ID; expired snapshots return
409 and require a refresh. Project counts and phase inventories have been removed
from `/api/cluster` as well; those obsolete fields are omitted.

Charts use pool recordings by default or at most five selected nodes, with at most
240 evaluation timestamps. Device charts require exactly one node. Device detail
queries limit each result to 100 filesystem/device series and cap the combined
response at 200 rows. Hitting a limit is visible. Detail and chart responses share
a 64-entry, 30-second cache with at most four active loads and a 12-second deadline.
Busy requests return 429 with Retry-After. Errors are briefly cached to avoid retry
storms. Overview samples older than two minutes are discarded; host recordings
also require a healthy node exporter. The UI marks old snapshots stale.

Pod drill-down is loaded only when its tab is selected. It sends an exact
`spec.nodeName` field selector, optional namespace/phase filters, a limit of 50
(maximum 100), and Kubernetes continuation tokens. There is no exact total count
or background pod refresh. Hidden browser tabs do not poll; obsolete fetches are
cancelled. Shared server refreshes finish under their own deadline.

## Verification boundary

Tests cover concurrent snapshot reuse without tenant lists, 100,000-node synthetic
pagination, sorting over the complete filtered pool, stable cursors across refresh,
partial/stale metrics, cache reuse, chart limits, and node-scoped pod listing.
These are correctness and response-budget tests, not proof of production throughput
at 100,000 pods. Before a large deployment, measure rule evaluation duration, series
cardinality, snapshot collection time, API memory, and concurrent-viewer latency
against representative workloads. A mock browser preview cannot verify live
collector labels or device topology.

### Local kind verification (2026-10-13)

Deployed the current console/server and reconciled monitoring on
`kind-dev-local-machine` (two Ready arm64 nodes, Kubernetes v1.37.0). All 46
recording rules evaluated successfully. Temporarily labeled one node `platform`
and the other `apps`; each pool returned exactly its own node, capacity, and
compute/storage/network chart series. Restored the original unlabeled nodes after
these checks.

Live checks covered filesystem used/free/available values, disk read/write IOPS
and throughput, disk latency, eth0 receive/transmit traffic, per-device and mount
histories, bounded chart points, and consecutive node-scoped pod pages without
duplicates. Browser verification used the deployed operator panel with a normal
CLI sign-in. This exposed container metadata bind mounts being selected as the
worst filesystem; both recording rules and detail queries now exclude
`/etc/hostname`, `/etc/hosts`, and `/etc/resolv.conf`, leaving `/var` on these nodes.

Kind nodes share Docker's underlying VM and storage. These checks establish
functional integration, not independent physical capacity or production scale.
