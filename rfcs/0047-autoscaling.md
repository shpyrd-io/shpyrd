# RFC-0047 Autoscaling

**Status:** implemented (web processes; see Implementation status)

**Owner:** Patrick Negri (shpyrd-io/shpyrd main)

**Depends on:** RFC-0019 (implemented), RFC-0042 (workspace ceilings shipped), RFC-0075 (sleep: the same KEDA objects)

**Creation date:** 2026-09-22

**Last update:** 2026-10-27

## Summary

A web process runs either with a fixed instance count (as before) or between a
**minimum and a maximum**: the count follows the load. Two signals drive it, and KEDA
follows whichever asks for more instances: the **requests in flight per instance**, counted
by the KEDA HTTP add-on's interceptor that sleep already puts in front of the app, and the
**CPU** an instance uses, as the Kubernetes Metrics Server measures it. The dashboard shows
"shared-s × 3 (auto 2–10)".

## Motivation

Traffic varies; fixed instance counts either waste money or fall over.

### Goals

- `autoscale: {min: 2, max: 10}` on the web process, and the count follows load; `cpu`
  (percent of the size's CPU, 70 by default) and `concurrency` (requests in flight per
  instance, 20 by default) when the defaults will not do.
- Sleep (RFC-0075) and bounds on one process: zero while asleep, the minimum on a wake, the
  bounds under load.

### Non-Goals

- Scale-to-zero on its own (sleep is that); cluster autoscaling (RFC-0077, the node side).
- Other process types, and queue-driven workers (Redis list length, a Prometheus query, a
  cron window): decided out on 2026-10-27. Nothing in the design forbids them later; the
  CPU half would work for a worker as it is.
- A response-time target (Heroku's p95): the add-on exposes concurrency and request rate,
  not latency, and Heroku itself warns the approach needs low variance.

## Decisions (2026-10-27)

Looked at before deciding: Heroku (web dynos on a p95 target, predictive, one dyno per
event, workers through third parties), Fly.io (the proxy starts and stops existing machines
on a per-machine concurrency `soft_limit`, 20 by default; `fly-autoscaler` on any
Prometheus metric), Render (CPU and memory percent targets, immediate up, a few minutes
before down, a Pro plan). Render's model is the RFC's HPA model; Fly's is what the KEDA
HTTP add-on gives for free.

1. **One ScaledObject per web process, shared with sleep.** KEDA's admission webhook refuses
   a second ScaledObject or HPA on a Deployment, so sleep and bounds cannot be two objects.
   The object is named like the Deployment (`web`, or `<slug>-web` for a legacy App), as
   every object of a process is (RFC-0076); the earlier `<app>-sleep` and the bridge Service
   `web-sleep` are replaced on the first reconcile, without waking a sleeping app (KEDA is
   told not to restore the count it found).
2. **Two triggers, the larger wins.** The interceptor's `external-push` trigger on the
   InterceptorRoute's `scalingMetric.concurrency.targetValue`, and a `cpu` trigger of
   `metricType: AverageValue` whose value is the target percent of the size's CPU. An
   average value rather than a utilisation: a shared size requests an eighth of its CPU
   (`sizes.ShareFactor`), so "70% of the request" would be a ninth of what the person means.
   Scale-down waits five minutes (`behavior.scaleDown.stabilizationWindowSeconds: 300`,
   the HPA default, written out).
3. **The interceptor in the path of every process with bounds**, not only sleeping ones. It
   already runs for sleep on every cloud (two to five replicas on the platform pool); the
   Ingress backend moves to the bridge Service `web-interceptor` once the objects exist.
4. **The Metrics Server is a component** (`deploy/components/metrics-server`, chart 3.13.1,
   one pod of 20m CPU and 64Mi requested on the platform pool; `--kubelet-insecure-tls` on
   the local profile). Without it the object carries the request trigger alone and the
   process status says CPU is not measured. Checked before installing: the first cloud's
   platform pool had 58 of 70 pod slots used and 71% of one node's CPU requested.
5. **An `autoscale` extension** names `keda`, `metrics-server` and `keda-http`; the
   installer puts a component shared with the `sleep` extension in place once. Autoscaling
   is open source; auto sleep stays the enterprise's (RFC-0075).
6. **`shpyrd scale web=N` on a process with bounds sets the minimum** and keeps N as the
   count the process returns to when the bounds go; zero and a count above the maximum are
   refused in words. **The maximum counts against the workspace's instance ceiling**
   (RFC-0042). **A single-instance volume refuses bounds** as it refuses a count above one.

## Proposal

- `processes.web.autoscale: {min, max, cpu?, concurrency?}` (`api/v1alpha1.AutoscaleSpec`,
  with `TargetCPU` and `TargetConcurrency` for the defaults). `replicas` stays what it was:
  the count the process had, and returns to.
- The controller (`internal/controller/scaling.go`) renders the bridge Service, the
  InterceptorRoute and the ScaledObject for a web process with a sleep policy, bounds, or
  both; `mutateDeployment` leaves the replica count to KEDA while either is on
  (`scaledExternally`), and `ProcessStatus.Desired` is the count the scaler asks for.
- `ProcessStatus.Autoscale {min, max, cpu, concurrency, state, message}` reads the bounds
  back: `active`, or `unavailable` with why (no KEDA kinds, pinned by a volume, KEDA could
  not scale the app), and on an active one the CPU gap when the Metrics Server is missing.
- API: `POST /api/projects/:slug/processes` takes `{"web": {"autoscale": {"min": 2, "max":
  10, "cpu": 70, "concurrency": 20}}}` or `{"autoscale": {"off": true}}`, refused with 409
  until KEDA's kinds exist. The scale endpoint and a `replicas` change in a batch set the
  minimum on a process with bounds.
- CLI: `shpyrd autoscale <project> --min 2 --max 10 [--cpu 70] [--concurrency 20]`,
  `--off`, and the read-back without flags; `shpyrd scale` says when a count became a
  minimum.
- Dashboard: the Resources page shows `2–10 auto` in place of the steppers on the web row
  with an **Autoscale** dialog (a fixed count, or bounds and the two targets), and what the
  bounds do under the Now badge; the Overview says `shared-s × 3 (auto 2–10)`. In the
  dialog the bounds are the two thumbs of a range with the numbers typed at its ends, the
  line running to the workspace's instance ceiling, and each target has a slider beside its
  input (Heroku's and Judoscale's layout); `@shpyrd/ui` gained the `Slider` for it.

## Design Details

- With sleep and bounds together the ScaledObject has `idleReplicaCount: 0`,
  `minReplicaCount: min`, `maxReplicaCount: max`, `cooldownPeriod` and
  `initialCooldownPeriod` the quiet period, and both triggers. KEDA decides asleep versus
  awake on the interceptor's trigger alone (a cpu trigger never keeps an app up), then the
  HPA runs between the bounds. In page mode the InterceptorRoute carries the request-rate
  wake signal (target 1000, as before) beside the concurrency target.
- Without bounds the object is what sleep rendered before: the declared count as minimum
  and maximum, the interceptor's trigger with a target of 1000 so it only decides asleep
  versus awake.
- `restoreToOriginalReplicaCount` is false: the controller writes the count back itself when
  the object goes (bounds off, sleep off, the extension gone, a pause).
- Quotas: `pkg/api/limits.go` counts a process with bounds at its maximum.
- **How fast it moves.** The HPA evaluates every 15 seconds; each trigger says how many
  instances it wants (requests in flight divided by the target, rounded up; the CPU average
  over its target, applied to the current count) and the larger wins, with the HPA's 10%
  tolerance around the target. Scaling up has no delay and the Kubernetes default step, the
  larger of four instances or double the count per 15 seconds (1 → 5 → 10 → the maximum).
  Scaling down uses the highest count wanted in the last five minutes, then drops to it in
  one step. The quiet period is a cooldown for the step to zero only (KEDA's
  `cooldownPeriod`); scaling between the bounds while awake never waits for it. None of the
  three is settable yet (below).

## Implementation status

Done (2026-10-27): everything in the Proposal, with tests in `internal/controller`
(`autoscale_test.go`), `pkg/api` (`autoscale_test.go`), `internal/cli`, the workspace app
and `design/ui` (`slider.test.tsx`). Verified on a kind cluster: 60 requests held in flight
took one instance to three in about 20 seconds; CPU load took it to the maximum; with a
five-minute sleep policy the app reached zero five minutes after the last request and one
request woke it to its minimum, not its maximum, in six seconds. Not verified on OCI.

Not done, by decision: queue triggers; other process types; the live metric value in the
dashboard (the HPA's `status.currentMetrics` is not read; `desired` shows the count the
scaler asks for).

Not done, open for later, each with what it takes:

- **The sensitivity knobs** Judoscale exposes (how many instances a step adds, how often,
  how long before one is removed): the HPA's `behavior.scaleUp.policies`, its
  `periodSeconds`, and `scaleDown.stabilizationWindowSeconds`, fixed here at the defaults
  above. Three fields on `AutoscaleSpec` and the dialog, rendered into the ScaledObject's
  `horizontalPodAutoscalerConfig.behavior`.
- **A response-time target** (Heroku's p95): needs a latency source per app; the add-on's
  interceptor measures requests in flight and request rate, not latency.
- **Schedules** (a higher minimum in business hours): KEDA's `cron` trigger as a third
  trigger on the same object.
- **A cost range** beside the bounds (Heroku): the size's price times the two bounds, in the
  dialog.
- **A word when the process sits at its maximum** for a while: an event from the
  controller, where Heroku sends an email.

## Implementation History

- 2026-09-22: RFC written.
- 2026-10-22 (the storage plan of 2026-10-10 (shpyrd-cloud `docs/plans/2026-10-10-storage-durability-and-scale.md`)): not started as a feature. What exists around it: KEDA and
  its HTTP add-on drive the web process between 0 and its instance count for
  sleep (RFC-0075; the ScaledObject's maximum is the declared count, the
  exclusion this RFC promised); a single-instance volume caps a process at one
  instance; a size no machine can hold is refused before the rollout, and an
  instance that still finds no room fails in words instead of waiting (#127).
- 2026-10-27: the decisions above; implemented for web processes: the `autoscale` extension
  and the `metrics-server` component, the spec and status, the shared ScaledObject with the
  request and CPU triggers, the API, `shpyrd autoscale`, the dashboard, the ceiling on the
  maximum. On a cluster with sleeping apps the first reconcile replaces the `<app>-sleep`
  ScaledObject and the `web-sleep` Service; the apps stay asleep. The same day, after
  looking at Heroku's and Judoscale's screens: the dialog's bounds and targets on sliders
  (`Slider` added to `@shpyrd/ui`), the scaling behaviour written down, and the knobs,
  the p95 target, schedules, the cost range and the at-maximum event listed as left out.
