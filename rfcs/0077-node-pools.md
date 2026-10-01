# RFC-0077 Node pools: a fixed platform pool and an autoscaled apps pool

**Status:** implemented (v0.9.41), gaps — see Implementation status below

**Owner:** Patrick Negri

**Depends on:** RFC-0035 (implemented: cloud profiles), RFC-0060 (implemented: volumes),
RFC-0075 (implemented: sleep, economics, cluster autoscaler)

**Relates to:** RFC-0047 (implementable: autoscaling) — this RFC is the node side of
scaling; RFC-0047 is the pod side.

**Creation date:** 2026-09-28

**Last update:** 2026-09-28 (implemented)

---

## Summary

Cloud clusters get two node pools. The **platform pool** is fixed in size and carries
everything stateful: the platform's own components (ingress, Prometheus, the control-plane
database, KEDA, cert-manager, kpack, the CNPG operator, the wake proxy, the autoscaler)
and customers' databases and stores. The **apps pool** is autoscaled between a minimum and
a maximum and carries only what can always be moved: application processes, build pods and
one-off runs. The cluster autoscaler manages the apps pool alone.

## Motivation

The first day with the cluster autoscaler on the cloud (RFC-0075, v0.9.34) showed it
could never remove a node. Every node held something it will not evict: an awake customer
database (CNPG's pods are not a controller it knows), Prometheus and the control-plane
database (StatefulSets with one replica), the autoscaler itself. Application pods — the
only thing that comes and goes with load and sleep — were spread across all three nodes
next to those anchors, so putting apps to sleep freed CPU but never a node. Idle capacity
stayed at ~38% of the cluster's cost and could not fall.

Separating the two kinds of workload gives the autoscaler nodes it can actually drain, and
gives the operator a fixed, knowable platform cost next to a variable one that tracks
demand.

## Design

### Pools

| Pool | Size | Shape | Carries |
| --- | --- | --- | --- |
| `platform` (the existing `workers` pool) | fixed, `node_count` | as today | platform components; Postgres, Redis and any stateful extension resource; anything without a pool selector |
| `apps` | autoscaled `apps_min_count..apps_max_count` | smaller flexible shape, e.g. 2 OCPU / 8 GB | web/worker processes, release and one-off Jobs, kpack build pods |

Both pools are told apart by one node label, `shpyrd.io/pool`, set by the node pool
(`apps` and `platform`). The label works on both sides: app pods carry a hard node
selector for `apps`; the platform's stateful pods (databases, stores, Prometheus, the
control-plane database) carry a selector or a preferred affinity for `platform`. OKE's node
pool API has no taint field, so the design does not lean on taints: a pod with no selector
at all can still land on an apps node and be evicted with it — acceptable for platform
Deployments that are replicated or stateless, which is why only the stateful ones are
pinned. Smaller apps nodes let the autoscaler scale in finer steps and pack sleeping apps'
neighbours tighter.

Databases stay on the platform pool because their eviction is a 30-second outage a
customer notices, and because their volumes (RFC-0060) attach to whatever node the pod
lands on.

### What the controller schedules where

`Config.AppsPool` (from `SHPYRD_APPS_POOL`, the label value; empty = single pool, as today)
makes the App controller add to every process Deployment, release Job, one-off run and
kpack Image build pod template:

```yaml
nodeSelector:
  shpyrd.io/pool: apps
```

The Postgres and Redis reconcilers pin CNPG clusters and Redis StatefulSets to the platform
pool (`Config.PlatformPool`, from `SHPYRD_PLATFORM_POOL`) with the same kind of selector;
Prometheus and the control-plane database prefer it through the profile's values. A future
`data` pool would be the same mechanism with a different label on the datastore
reconcilers.

### Autoscaler

The cluster autoscaler component (RFC-0075) is pointed at the apps pool only:
`--nodes=<min>:<max>:<apps pool OCID>`. The platform pool is not a node group it knows;
it never touches it. Scale-down parameters stay: a node is removable ten minutes after it
became unneeded, at 50% utilisation or less. With sleeping apps and a pool that holds only
Deployments and Jobs, that is now reachable.

**Minimum of 0 or 1.** At 0, the first request into a fully idle cluster pays node boot
(60–90 s on OCI) plus image pull plus app start before the app answers — two minutes
behind the resuming page. At 1, wake stays at the ~7 s of RFC-0075 and one node stays warm.
Default: **1 on the cloud** (customers), **0 acceptable for a dev cloud**. The Terraform
variable decides.

### Migration

Adding the pool is additive: Terraform creates it and labels the existing pool `platform`,
the installer sets `SHPYRD_APPS_POOL`/`SHPYRD_PLATFORM_POOL` and rebinds the autoscaler,
the controller's next reconcile of every App adds the selector, and the rolling update
moves app pods to the new pool as the autoscaler adds nodes for the Pending replacements
(old pods keep serving until the new ones are Ready). Nothing is drained by hand; the
platform pool's `node_count` is then lowered by Terraform to what the platform and the
databases need.

The order matters: **rebind the autoscaler before the apps node joins.** Until it is told
about the apps pool it still manages the platform pool, and the first free node there is
the one it drains — with the StatefulSets on it (Prometheus, the control-plane database).
On the first cloud that is exactly what happened (see History); the move itself was clean
because both are single-replica StatefulSets with volumes that reattach, but a customer
database on that node would have been a 30-second outage.

## Economics

On the first cloud (3 × 4 vCPU): the platform pool needs two nodes (Prometheus, two
ingress controllers, KEDA's six pods, monitoring, two databases); the apps pool runs 0–2
smaller nodes. Base cost falls from three fixed nodes to two plus whatever apps demand —
up to one node's worth a month at this size. The structural gain is what matters at 20+
nodes: the apps pool tracks load and sleep, the platform pool is a fixed line that can go
on committed pricing, and the two can differ in shape.

## Open questions

1. **A third `data` pool** for customer databases, separate from the platform's own
   stateful pods. Default: **not yet** — start with two; split data out when the databases
   fill a node of their own.
2. **Builds on the apps pool.** Build pods are bursty and large; they belong with apps
   (evictable, transient), but a busy build hour can trigger a scale-up that a sleeping
   night then unwinds. Default: **apps pool**; a `build` pool is the same mechanism later
   if it proves noisy.
3. **Apps pool shape.** Default: **2 OCPU / 8 GB** on OCI (`VM.Standard.E5.Flex`), half
   the platform node, so one small app does not hold a large node awake.

## Implementation status

| Part | Status |
| --- | --- |
| Terraform `apps` node pool: `apps_min_count`/`apps_max_count`/`apps_node_ocpus`/`apps_node_memory_gb`, label `shpyrd.io/pool=apps`, `ignore_changes size` (the autoscaler owns it); `workers` pool labelled `platform`; vars file emits `SHPYRD_APPS_POOL`, `SHPYRD_PLATFORM_POOL` and `SHPYRD_NODE_POOL_ID` = the apps pool | v0.9.41 |
| `Config.AppsPool`: node selector on process Deployments, release Jobs, Dockerfile build Jobs, kpack `spec.build.nodeSelector`; `shpyrd run` pods read the pool from the install record | v0.9.41 |
| `Config.PlatformPool`: CNPG `spec.affinity.nodeSelector`, Redis StatefulSet selector; Prometheus and the control-plane database prefer the platform pool (OCI profile values) | v0.9.41 |
| Cluster autoscaler bound to the apps pool alone (`--nodes=<min>:<max>:<apps pool>`) | v0.9.41 |
| `cluster init`: a value in `--vars-file` beats a `--set` recorded by an earlier run (a remembered pool OCID had pinned the autoscaler to the platform pool) | v0.9.42 |
| First cloud: platform pool 2 × 4 OCPU, apps pool 1–3 × 1 OCPU / 8 GB; apps moved, apps pool scaled 1 → 2 on the Pending replacements | applied |

Known gaps:

- The autoscaler logs `node pool not found for instance` for every platform node each loop:
  harmless (it only knows the apps pool) but noisy. A filter or an upstream flag later.
- Platform Deployments without a selector (ingress, KEDA, cert-manager, kpack, operators)
  may land on apps nodes and be evicted with them; they are replicated or stateless, so a
  scale-down is a restart, not an outage. Pin them if it shows up in the wake numbers.
- Apps pool minimum is 1 on the first cloud; 0 is untested end to end (node boot inside the
  wake path, RFC-0075's resuming page would need to cover ~2 min).
- `data` pool (open question 1) not started.

## History

- 2026-09-28: written the day the autoscaler first ran and could remove nothing.
- 2026-09-28: implemented and applied on the first cloud (v0.9.41). Taints dropped from
  the design: OKE node pools cannot set them, so both pools are told apart by the
  `shpyrd.io/pool` label alone, with selectors on both sides. The rollout ran the migration
  in the wrong order: the apps node joined while the autoscaler was still bound to the
  platform pool (a `--set` recorded from the first install beat the new vars file), and it
  drained `10.0.1.204` — Prometheus and the control-plane database moved cleanly, the
  platform pool went 3 → 2 on its own. Fixed in the CLI; the RFC now says rebind first.
