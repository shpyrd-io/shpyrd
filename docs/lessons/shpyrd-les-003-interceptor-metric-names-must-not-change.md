---
id: shpyrd-les-003
type: lesson
title: The metrics an InterceptorRoute declares must not change while its HPA lives
status: active
date: 2026-11-19
related: []
governs:
  - internal/controller/scaling.go
---

## What happened

The HorizontalPodAutoscaler that KEDA makes for a web process emitted
`FailedGetExternalMetric` warnings on every sync, "no matching metrics found
for s0-http_<id>_rate", for days. Sleeping and waking kept working, because
the KEDA operator and not the HPA moves a process between zero and one
instance, so nothing looked broken except the stream of warning events. With
autoscale bounds the same HPA would also have stopped following the load.

## Why it happened

The controller rendered a different `scalingMetric` on the InterceptorRoute
per configuration: the request rate alone when sleep resumes behind the
waiting page, the requests in flight alone when it holds the request, both
with bounds in page mode. The KEDA HTTP add-on's scaler derives the names of
the external metrics it serves from that field.

The ScaledObject, however, was the same in every configuration: one
`external-push` trigger that names the scaler and the route. KEDA builds the
HPA from the metric names the scaler reports when the ScaledObject's spec
changes, and not otherwise. So when the resume mode of an existing app
changed, the route started serving another metric name, the ScaledObject did
not change, and the HPA kept asking for a name that no longer existed.

No test caught it because the tests reconcile against a fake client: no HPA,
no metrics API and no scaler run in them. The first report also read the
symptom as "the metric answers an error while the app sleeps"; the events
were emitted awake as well, and the full event message, which names the
missing metric, is what pointed at the cause.

## What we changed

The InterceptorRoute declares both metrics, requests in flight and request
rate, in every configuration. Only their targets differ: the concurrency
target is the bounds' target when bounds apply and a wake-only number
otherwise; the rate always has the wake-only target. The names the scaler
serves therefore never change, whatever HPA exists, and a route rendered by
an earlier version is repaired by the next reconcile.

Making the ScaledObject change together with the metric was not chosen: KEDA
could ask the scaler for its names before the scaler had seen the new route,
and build the HPA from the old names again.

## How to notice next time

- An HPA named `keda-hpa-<process>` whose `ScalingActive` condition is False
  with reason `FailedGetExternalMetric`, while the ScaledObject is Ready.
- Compare the metric names in the HPA's spec with what the external metrics
  API answers for the namespace: a name the HPA lists that the API does not
  serve is this problem.

Before changing what `scalingMetric` holds, or anything else the scaler
derives metric names from, remember that HPAs already built keep the old
names until their ScaledObject's spec changes.

## Harness follow-up

None.
