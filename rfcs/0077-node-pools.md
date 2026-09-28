# RFC-0077 Node pools: a fixed platform pool and an autoscaled apps pool

**Status:** in progress

**Owner:** Patrick Negri

**Depends on:** RFC-0035 (implemented: cloud profiles), RFC-0060 (implemented: volumes),
RFC-0075 (in progress: sleep, economics, cluster autoscaler)

**Relates to:** RFC-0047 (implementable: autoscaling) — this RFC is the node side of
scaling; RFC-0047 is the pod side.

**Creation date:** 2026-09-28

**Last update:** 2026-09-28

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

The apps pool's nodes carry the label `shpyrd.io/pool=apps` and the taint
`shpyrd.io/pool=apps:NoSchedule`. The taint keeps platform and stateful pods off the pool
without touching their manifests; app pods get the matching toleration and a node
selector. Smaller nodes let the autoscaler scale in finer steps and pack sleeping apps'
neighbours tighter.

The platform pool is untainted and unlabelled, so everything that does not opt into the
apps pool lands there — including databases, whose eviction a customer would notice as a
30-second outage, and whose volumes (RFC-0060) attach to whatever node the pod lands on.

### What the controller schedules where

`Config.AppsPool` (from `SHPYRD_APPS_POOL`, the label value; empty = single pool, as today)
makes the App controller add to every process Deployment, release Job, one-off run and
kpack Image build pod template:

```yaml
nodeSelector:
  shpyrd.io/pool: apps
tolerations:
  - key: shpyrd.io/pool
    operator: Equal
    value: apps
    effect: NoSchedule
```

Postgres and Redis reconcilers add nothing: CNPG clusters and Redis StatefulSets stay on
the platform pool. A future `data` pool would be the same mechanism with a different label
on the datastore reconcilers.

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

Adding the pool is additive: Terraform creates it, the installer sets `SHPYRD_APPS_POOL`,
the controller's next reconcile of every App adds the selector and toleration, and the
rolling update moves app pods to the new pool as nodes join. Nothing is drained by hand;
the platform pool's `node_count` is then lowered by Terraform to what the platform and the
databases need, and the autoscaler removes the now-empty apps capacity from the old pool's
former share by never having to — the old nodes simply carry fewer pods until the count is
reduced.

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

v0.9.41: Terraform `apps` node pool (label, taint, shape, bounds; outputs and vars file);
`Config.AppsPool` with selector and toleration on process Deployments, release and run
Jobs and kpack build pods; `SHPYRD_APPS_POOL` installer variable; cluster autoscaler bound
to the apps pool. Applied on the first cloud. Gaps: none known; the dev cloud's platform
pool is left at its size until the apps have moved.

## History

- 2026-09-28: written the day the autoscaler first ran and could remove nothing.
