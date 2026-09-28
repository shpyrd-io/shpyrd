# RFC-0075 Usage, billing and sleep: the platform's economics and scale to zero

**Status:** in progress (v0.9.18: phases 1–2 shipped, 3–4 shipped opt-in, 5 scaffolding)

**Owner:** Patrick Negri

**Depends on:** RFC-0002 (implemented: extensions), RFC-0003 (implemented: resources),
RFC-0009 (implemented: metrics), RFC-0033 (in progress: workspaces, plans, the edge),
RFC-0038 (implemented: Postgres backups), RFC-0042 (implementable: quotas),
RFC-0047 (implementable: autoscaling), RFC-0048 (implementable: cost visibility)

**Supersedes / absorbs:** RFC-0048 (cost visibility — that RFC proposed the same usage
measurement from Prometheus; this one specifies the full stack, from raw usage to gross
margin, and replaces it).

**Extracts billing from:** RFC-0033 phase 8 (plans/subscriptions/billing), which named
`Metering` as a seam but left the design to a later RFC. That design is here.

**Creation date:** 2026-09-27

**Last update:** 2026-09-27

---

## Summary

Five things in one RFC, because they share one data model and would be incoherent apart:

1. **Usage ledger.** Every project's CPU, memory, storage and HTTP egress, measured from
   Prometheus into five-minute buckets in the control-plane store, quality-flagged,
   rolled up, kept 13 months. The shared input to everything below.

2. **Customer billing.** What the workspace owner owes: usage × plan price. Displayed live
   (a running total for the month, a projection); emailed as an invoice (phase 2, after
   payment is wired); the workspace's Plan card is the billing portal.

3. **Operator economics.** What it costs you to run a workspace: COGS via OpenCost (reads
   the OCI / AWS actual bill and allocates node cost per namespace), idle cost, shared
   infrastructure overhead. Visible only in the operator console. `revenue − COGS = gross
   margin` per workspace, per partner, per cluster. Never shown to customers.

4. **Sleep for HTTP apps.** A `web` process scales to zero after a quiet period; the first
   request wakes it. The person chooses: wait transparently, or see a branded resuming
   page. Built on the KEDA HTTP add-on's `InterceptorRoute` (v0.16).

5. **Sleep for PostgreSQL.** A database hibernates (CloudNativePG declarative hibernation)
   after a period with no sessions; the first connection wakes it through a TCP proxy that
   holds the socket and pipes bytes once the primary is ready. `DATABASE_URL` never changes.

Sleep is how the economics work for small plans: sleeping compute costs nothing. The ledger
is how billing and margin share one source of truth.

---

## Motivation

### Why billing is extracted from RFC-0033

RFC-0033 phase 8 said "plans/subscriptions/billing" and pointed at RFC-0042 and RFC-0048.
Neither specified how a customer is charged, what an invoice contains, or how a partner's
wholesale terms roll up to a margin. This RFC closes that gap. The design belongs here, not
in a "tenancy" RFC, because it is fundamentally about measurement and money, not about
identity and routing.

### Why operator economics is a separate concern from billing

A customer's invoice uses **plan prices** — `$0.02 per CPU-hour`, fixed by you, independent
of what OCI charges you. Your margin depends on how much of that price is left after the
node cost. These are different numbers from different sources that must never be conflated:

```
customer invoice  = usage  ×  plan_price       (your decision, public)
COGS per project  = usage  ×  node_cost / node_capacity  (your cost, private)
gross margin      = invoice − COGS
```

The ledger records usage in physical units. Billing multiplies by plan price. OpenCost
supplies node cost. The UI applies both multiplications to the same numbers for two
different audiences.

### Why sleep is here

A sleeping project consumes no CPU and no memory. Its storage still accrues. Its invoice
reflects this: the "active compute" line is zero for the hours it slept. Sleep is not
independent of billing; it is how small plans achieve an acceptable unit cost.

---

## What already exists

- **Prometheus** (kube-prometheus-stack, 7-day retention): cAdvisor
  (`container_cpu_usage_seconds_total`, `container_memory_working_set_bytes`),
  kube-state-metrics (pod labels `shpyrd.io/project`, `shpyrd.io/process`, PVC requests,
  node `node.kubernetes.io/instance-type`, `topology.kubernetes.io/zone`), ingress-nginx
  (`nginx_ingress_controller_response_size_sum`). The dashboard's metrics queries already
  use these.
- **`settings.limits`** on each workspace: the plan's ceilings (projects, instances, CPU,
  memory, storage). The Plan card shows them live. There is no price, no invoice, no
  history. This RFC adds all three.
- **Process Deployment**: `spec.processes.<type>.replicas`; the controller sets
  `Deployment.spec.replicas` directly. No HPA or KEDA today (RFC-0047 is unimplemented).
- **CNPG Cluster** for every database; `pg.Status.Endpoint = <name>-rw.<ns>.svc:5432`.
- **RFC-0042** plan ceilings enforced at the API layer: the workspace cannot exceed its
  plan limits when creating projects or scaling up.
- **`WorkspaceSummary.Usage`** is declared in the cloud extension but never filled (noted
  as a gap in the private RFC's audit). This RFC fills it.

---

## 1. Usage ledger (shared foundation)

### What is measured per project, every 5 minutes

| Metric | Unit | Prometheus source | Notes |
| --- | --- | --- | --- |
| `cpu_used` | core-seconds | `increase(container_cpu_usage_seconds_total{ns}[5m])` | actual use, not reserved |
| `memory_used` | GiB-seconds | `avg_over_time(container_memory_working_set_bytes{ns}[5m]) × 300` | working set |
| `cpu_reserved` | core-seconds | `kube_pod_container_resource_requests{resource="cpu",ns} × 300` | for billing floor decisions |
| `storage` | GiB-seconds | `kube_persistentvolumeclaim_resource_requests_storage_bytes{ns} × 300` | provisioned, always, awake or asleep |
| `egress_http` | bytes | `increase(nginx_ingress_controller_response_size_sum{exported_ns, ingress}[5m])` | HTTP out at the front door; labelled "estimate" |
| `instance_seconds` | process-seconds | `count(kube_pod_status_ready) × 300` by `shpyrd.io/process` | for the "what ran" view |
| `state_seconds` | state-seconds | from the controller: `awake`/`sleeping`/`waking` per process and database | feeds the cost line "you saved" |

Each bucket carries a `component` dimension (`web`, `worker`, `postgres/<name>`,
`redis/<name>`, `volume/<name>`) from pod labels so cost can be split by component.

### Storage schema

```sql
-- Five-minute usage, kept 14 days, then rolled to hourly.
usage_buckets(
  workspace_id, project, component, metric,
  period_start TIMESTAMPTZ, period_end TIMESTAMPTZ,
  quantity    NUMERIC,      -- null when missing
  unit        TEXT,         -- 'core_seconds', 'gib_seconds', 'bytes'
  quality     TEXT,         -- 'complete' | 'partial' | 'missing'
  revision    INTEGER,      -- 1 on creation; catch-up increments this
  source      TEXT,         -- query family + version
  PRIMARY KEY (workspace_id, project, component, metric, period_start, revision)
);

-- Hourly rollup, kept 13 months.
usage_hourly(  -- same columns, aggregated; replaces 5-min rows after 14 days
  workspace_id, project, component, metric,
  period_start TIMESTAMPTZ, period_end TIMESTAMPTZ,
  quantity NUMERIC, unit TEXT, quality TEXT, revision INTEGER,
  PRIMARY KEY (workspace_id, project, component, metric, period_start)
);
```

### Pipeline

A metering loop runs leader-elected in the controller (one process, same lease as the
reconcilers). It closes the bucket `[t, t+5m)` at `t+5m+2min` (the 2-minute wait absorbs
late Prometheus scrapes and avoids querying an incomplete window).

It queries each metric family once per bucket, grouped by `(namespace,
label_shpyrd_io_process)` through the cAdvisor + kube-state-metrics join. Namespaces are
resolved to `(workspace, project)` through the `shpyrd.io/workspace` label on the namespace.

`quality = 'missing'` when Prometheus is unreachable; a **catch-up pass** re-queries up to
7 days back (full Prometheus retention) hourly and on controller start, writing new
revisions — never updating existing rows. Revisions are append-only; the highest revision
is truth.

After 14 days, five-minute rows are aggregated into `usage_hourly` and deleted. Hourly
rows older than 13 months are deleted. A destroyed project's rows are kept until the
retention expires (the workspace still needs to see what it cost).

`metering_missing_intervals_total` counter; Grafana alert at >5 consecutive missing buckets.

---

## 2. Customer billing

### The plan and its price

A workspace has a **plan**: a name, a set of limits (RFC-0042's existing `settings.limits`)
and a **price schedule** — what the workspace owner pays, per unit of use, in the platform's
published prices. Plan prices are set by the operator and are **public** (the customer sees
them; they have nothing to do with what OCI or AWS charges):

```sql
plans(
  id, name,                          -- 'starter', 'grow', 'scale'
  cpu_hour_usd    NUMERIC,           -- per core-hour of actual use
  memory_gib_hour_usd NUMERIC,       -- per GiB-hour of working set
  storage_gib_month_usd NUMERIC,     -- per GiB-month provisioned
  egress_gib_usd  NUMERIC,           -- per GiB of HTTP egress
  min_monthly_usd NUMERIC DEFAULT 0, -- floor (e.g. $5/month for starter)
  effective_from  TIMESTAMPTZ,
  currency        TEXT DEFAULT 'USD'
);

workspace_plan(
  workspace_id, plan_id,
  starts_at TIMESTAMPTZ,
  ends_at   TIMESTAMPTZ  -- null = current
);
```

### Invoice line computation

At the end of each calendar month (and on demand for the month-to-date preview):

```
invoice_line(
  workspace_id, period_start, period_end,
  component, metric,
  quantity, unit, unit_price, gross_amount,
  plan_id, quality, revision
)
```

`gross_amount = quantity × unit_price` using the plan price effective at each bucket's
`period_start`. Storage is converted from GiB-seconds to GiB-months. A floor of
`min_monthly_usd` applies to the workspace total. Revisions of usage buckets generate
revised invoice lines (the original is kept, never deleted).

### What the workspace owner sees

**Plan card (Workspace › Overview)** — month to date:
- Usage by component with the price per unit and the running cost.
- Days elapsed / days in month, projected total.
- The plan name, limits and a link to change plan (operator-side for now; self-serve
  comes with the payment provider in phase 2).

**Invoice page (Workspace › Billing)** — past months:
- Month total, component breakdown, download as PDF/CSV.
- Estimated next month based on the current run rate.

**API:** `GET /api/workspace/billing/current` (month-to-date preview) and
`GET /api/workspace/billing/invoices?month=2026-10`.

**CLI:** `shpyrd billing [project|workspace] [--month 2026-10]`.

### Phase 2: actual invoices (after payment is wired)

The ledger and the invoice lines are built now. Charging money (Stripe, payment methods,
dunning) is a separate integration that reads the `invoice_line` table and marks each line
`paid` / `overdue`. Until then the invoice is "estimated" and no money moves. The Plan card
says "invoicing coming soon" until phase 2.

### Partner billing

A partner's wholesale terms are negotiated and stored in the `Partner` object (RFC-0033
phase 8 B, the partner object slice). The partner pays for all its workspaces under a
committed-capacity + overage model or per-plan — distinct from what the partner's customers
pay. This RFC's invoice lines aggregate to the partner level once the partner object exists;
the partner console shows cost per workspace so the partner can price its own customers.
Partners bill their customers directly; the platform never shows a retail price inside a
partner's workspace.

---

## 3. Operator economics (COGS and gross margin)

### OpenCost for infrastructure cost

**What OpenCost gives that our rate card cannot:**
- It reads the cloud provider's actual billing API (OCI Usage API, AWS Cost & Usage Report)
  and reconciles on-demand list prices with what was actually charged — including reserved
  instances, committed-use discounts and negotiated rates.
- It implements the OpenCost Specification's cost model: `workload_cost = max(request,
  usage) × node_price / node_allocatable`; `idle_cost = node_cost − Σ workload_costs`;
  shared infrastructure overhead (kube-system, monitoring, the platform itself) distributed
  proportionally. A project that reserves a large instance and uses half of it is charged
  for the reservation, not the usage — correctly.
- OCI is natively supported: `node.spec.providerID` auto-detected, OCI Price List API
  queried for on-demand prices without any key, OCI Usage API for reconciliation with the
  actual bill. AWS is the same.
- Persistent volume costs, load balancer costs and network egress costs are covered by the
  specification and pulled from the cloud bill, not estimated.

**OpenCost as an `opencost` extension** (RFC-0002):

```yaml
# deploy/components/opencost/component.yaml
name: opencost
description: Infrastructure cost allocation (CNCF OpenCost) — operator visibility only
namespace: opencost
helm:
  repo: https://opencost.github.io/opencost-helm-chart
  chart: opencost
  version: 2.5.32
wait:
  - deployment: opencost/opencost
```

Disabled by default; the operator enables it on the cloud (`shpyrd-ctl extensions enable
opencost`). Configuration: a `cloud-integration.json` Secret in the `opencost` namespace
with OCI or AWS credentials for actual-bill reconciliation; without it, OpenCost uses
public list prices (still better than manual table entry).

### COGS buckets

Once per hour, the metering loop calls the OpenCost Allocation API:

```
GET /model/allocation?window=1h&aggregate=label:shpyrd.io/workspace,label:shpyrd.io/project
    &includeIdle=true&shareIdle=proportional&shareNamespaces=shpyrd-system,monitoring,keda
    &shareSplit=weighted
```

It writes:

```sql
cogs_buckets(
  workspace_id, project,
  period_start, period_end,
  cpu_cost_usd        NUMERIC,
  memory_cost_usd     NUMERIC,
  storage_cost_usd    NUMERIC,
  network_cost_usd    NUMERIC,
  shared_cost_usd     NUMERIC,   -- platform infra allocated to this workload
  idle_cost_usd       NUMERIC,   -- idle capacity charged to this workload
  total_cost_usd      NUMERIC,   -- sum of above
  currency            TEXT,
  allocation_policy   TEXT,      -- 'shareIdle=proportional,shareNamespaces=...' for audit
  opencost_revision   TEXT       -- OpenCost's window/version for reproducibility
);
```

When OpenCost is not installed, `cogs_buckets` is empty and the operator console shows
"install the opencost extension for cost data".

### Operator console — economics dashboard

Visible to the operator only (the Cluster page, intranet-only per RFC-0063 if enabled; the
Cluster page today is admin-only and not intranet-gated — this remains so for now):

**Per-workspace economics card:**

```
Workspace: acme          Month: October 2026
────────────────────────────────────────────
Revenue (billed)         $124.00
  COGS — direct          $ 38.40
  COGS — shared infra    $  9.10
  COGS — idle allocated  $ 12.50
  ─────────────────────────────
  Total COGS             $ 60.00
  ─────────────────────────────
  Gross margin           $ 64.00   (51.6 %)
```

**Cluster economics:**

```
Cluster: shpyrd-dev    Month: October 2026
────────────────────────────────────────────
Total revenue            $2,840.00
Total COGS (OpenCost)    $1,240.00
  Direct                  $880.00
  Shared (platform)       $180.00
  Idle (unattributed)     $180.00
Gross margin              $1,600.00   (56.3 %)
─────────────────────────────────────────────
Idle / total node cost         14.5 %   ← alert >25%
Sleeping workloads                42   ← saving $X/h
```

**API:** `GET /api/cluster/economics?month=2026-10` (cluster admins only).
**CLI:** `shpyrd-ctl economics [--month 2026-10]`.

This data is **never shown to workspace owners or customers**. The customer sees their
invoice (plan prices). The operator sees the margin.

---

## 4. Sleep for HTTP apps

### Policy

```yaml
# shpyrd.yaml
sleep:
  after: 15m         # quiet period; "off" = never sleep
  resuming: page     # "page" (branded waking screen) or "wait" (hold the connection)
```

Workspace default: `off` (open source); 15 min with `resuming: page` (cloud small plans).
Only `web` sleeps. Minimum 5 min, maximum 24 h.

### Mechanics (KEDA HTTP add-on, v0.16)

The controller renders per sleeping-enabled web process, in the app's namespace:

**ExternalName Service** `web-sleep` → `keda-add-ons-http-interceptor-proxy.keda.svc.cluster.local`
(cross-namespace bridge; the documented Kubernetes Ingress pattern for the add-on).

**`InterceptorRoute`** (`http.keda.sh/v1beta1`):
- `target.service`: the web Service (real one).
- `rules.hosts`: the app's hosts (default, workspace aliases, custom domains).
- `scalingMetric.concurrency.targetValue`: large (1000) — concurrency is the activity
  signal, not the scale target; the ScaledObject controls replicas.
- `staticRoutes`: health-check path and `/.shpyrd/*` — never count as activity, answered
  statically (200 or the probe response) whether the app is awake or asleep.
- `coldStart.placeholder` (when `resuming: page`): `StaticResponse` 503 with
  `Retry-After: 2` from a ConfigMap the controller renders with the workspace's logo and
  colour, the app's name, and `<meta http-equiv="refresh" content="3">`.
- `coldStart.maxPendingRequests` (when `resuming: wait`): bounded queue, no placeholder;
  the connection is held until the pod is ready. Overflow: 503.
- `timeouts.readiness`: 120 s by default, configurable per app.

**`ScaledObject`** (KEDA core) on the web Deployment:
- `minReplicaCount: 0`
- `maxReplicaCount`: the process's `replicas` (person's `scale` controls this, as always)
- `cooldownPeriod`: `sleep.after` in seconds
- external scaler: the HTTP add-on's scaler

**App's Ingress**: backend switches from the web Service to the ExternalName `web-sleep`.
Everything else stays: TLS, the edge's `auth-url` subrequest (runs before the interceptor,
identity headers forwarded through), `custom-http-errors`.

**Auth-cache duration**: reduced from 20 s to 5 s for sleeping-enabled Ingresses
(`"200 5s, 401 5s, 403 5s"`). A woken pod appears to nginx within 5 s of ready.

When sleep is removed: the controller restores the original Ingress backend, removes the
ExternalName, InterceptorRoute and ScaledObject, restores the 20 s cache. No restart.

**RFC-0047 co-existence**: `sleep` and autoscaling (`autoscale.min > 0`) are mutually
exclusive per process. The controller adds a warning condition (`AutoscaleSleepConflict`)
and ignores `sleep` when autoscaling is active. The `keda` extension installation is shared.

**Activity signal**: any request the interceptor forwards counts as concurrency. Static
routes and `/.shpyrd/*` do not. WebSocket connections count as long-lived concurrency —
correct. The edge's `auth-url` subrequest is to the platform server, not the interceptor;
it does not count.

**Billing implication**: sleeping hours appear in `state_seconds{state=sleeping}`. The
`cpu_used` and `memory_used` buckets are zero while sleeping; storage still accrues. The
invoice line for active compute is zero for those hours; the storage line is not.

---

## 5. Sleep for PostgreSQL

### Policy

```yaml
sleep:
  after: 30m    # "off" = never sleep; workspace default applies
```

Single-instance databases only. HA databases (`instances ≥ 2`) are always on.

### States

| State | Meaning | Invariant |
| --- | --- | --- |
| `awake` | CNPG pods running, `Ready=True`, shpyrd Service → pods | `readyInstances ≥ 1` |
| `sleeping` | `cnpg.io/hibernation=on`, no pods, PVCs kept; shpyrd Service → wake proxy | CNPG `Hibernated` condition; PVCs exist |
| `waking` | annotation removed, pods scheduling; connections held by proxy | one wake per generation |
| `suspended` | manual; no wake-on-connect; explicit resume required | — |

Errors are conditions (`Ready=False`, `Degraded`), not states.

### Service ownership

The controller creates a shpyrd-owned Service `<name>` alongside CNPG's `-rw`. New
databases get `DATABASE_HOST=<name>.<ns>.svc`; existing databases keep `-rw` until the
operator runs `shpyrd pg migrate-service <name>` (one-line change to `DATABASE_HOST`,
rolling restart of the app). The shpyrd Service's selector flips between CNPG's primary pod
(awake) and the wake proxy pod (sleeping/waking).

Port allocated to the database: `pg.Status.Sleep.WakePort`, assigned once from a range
(15000–16000) by the reconciler and stored in the CRD status. The `pg-gateway` builds its
listener table from the Postgres CRDs — no ConfigMap, crash-safe reconstruction on restart.

### Going to sleep

1. `sleep.after` elapsed since `pg.Status.Sleep.LastActivityAt`. Activity is the latest
   of: `cnpg_backends_total{datname="app", state!="idle"} > 0` (active sessions on the
   app database; `postgres` datname excludes health checks), an increase of
   `xact_commit + xact_rollback` on `datname="app"`, a connection through the wake proxy,
   a `shpyrd pg` command, a backup completion. Requires `monitoring.enablePodMonitor:
   true` on the Cluster (the controller sets this).
2. Under a per-database controller lease: re-read signals; abort if anything changed or
   Prometheus is unreachable.
3. If a scheduled backup falls within `sleep.after` from now, take an on-demand backup
   first (a hibernated CNPG cluster archives no WAL; the clean shutdown flushes the last
   segment, but the next backup must not be lost).
4. Set `cnpg.io/hibernation=on`; wait for the `Hibernated` condition; flip the Service
   selector to the wake proxy; record `sleeping`.

### The wake proxy (`pg-gateway`)

A Go Deployment (2 replicas, `shpyrd-system`, PodDisruptionBudget `minAvailable: 1`; no
restart drops in-flight wakes):

1. TCP connection arrives on `pg.Status.Sleep.WakePort`.
2. Gateway calls `EnsureAwake(namespace, name, generation)` — idempotent; concurrent
   connections wait on the same operation.
3. Raw socket held open. No bytes read, no bytes written.
4. Polls until `pg.Status.Sleep.State == awake` and a TCP dial to the primary succeeds,
   within `maxWakeWait` (60 s by default). Past the deadline: close cleanly. Clients see
   "connection refused" and retry.
5. `io.Copy` bidirectionally for the session's life. TLS, `SSLRequest`, `StartupMessage`,
   `CancelRequest`, streaming — all pass through unmodified.
6. Controller flips the Service selector back to CNPG pods after the first connection;
   in-flight proxy sessions complete naturally.

Metrics: `pg_gateway_connections_active{ns,name}`, `pg_gateway_connections_pending`,
`pg_gateway_wake_duration_seconds{phase=schedule|attach|ready}`,
`pg_gateway_wake_total{outcome=ok|timeout|error}`, `pg_gateway_bytes_total{direction}`.

### Billing implication

While sleeping: `cpu_used` and `memory_used` are zero; `storage` (the PVC) accrues. The
customer's compute cost is zero for sleeping hours; the storage line is not. The
`state_seconds{state=sleeping}` line lets the invoice show "saved X hours of compute".

---

## 6. What the person sees vs what the operator sees

### Customer (workspace owner, in the dashboard)

- **Plan card (Workspace › Overview)**: month-to-date usage and estimated cost at plan
  prices, by component; days elapsed, projected monthly total; plan name and limits.
- **Billing page (Workspace › Billing)**: past invoices (once phase 2 ships), current
  month preview, download.
- **Project page**: Usage card (this month, last 30 days by day, by component); sleep
  state per process and database; wake duration.
- **Launcher**: moon icon on sleeping apps.
- **Resuming page**: workspace branding, "Waking up — one moment", meta-refresh.

### Operator (cluster admin, in the Cluster page)

- **Economics card** (Cluster page): revenue, COGS (direct + shared + idle), gross margin,
  per workspace and cluster-wide; idle % alert; sleeping-workload count with estimated
  savings.
- **Node cost** (Cluster page, existing Nodes section): cost per node from OpenCost.
- **CLI**: `shpyrd-ctl economics [--month]`.

The customer **never** sees node prices, COGS, idle cost or gross margin.
The operator **never** sees what one customer is invoiced (that is in the customer's
workspace, accessible to workspace admins).

---

## Design Details

### Metering loop

Leader-elected in the controller; one PromQL query per metric family per bucket, grouped by
`(namespace, label_shpyrd_io_process)` using kube-state-metrics `kube_pod_labels` join;
namespace resolved to `(workspace, project)` via `kube_namespace_labels`. Catch-up: 7 days
back (full Prometheus retention), hourly, and on controller start. Closing delay: 2 min.

### KEDA

One `keda` extension installs KEDA core and the HTTP add-on. RFC-0047's autoscaling uses
the same KEDA core with HPA or queue-driven `ScaledObject` with `minReplicaCount ≥ 1`. The
controller never creates both a sleep `ScaledObject` and an RFC-0047 `ScaledObject` for the
same Deployment — mutual exclusion in `processObjects()`.

### OpenCost Allocation API call

```
GET /model/allocation
  ?window=1h
  &aggregate=label:shpyrd.io/workspace,label:shpyrd.io/project
  &includeIdle=true
  &shareIdle=proportional
  &shareNamespaces=shpyrd-system,monitoring,keda,cnpg-system,opencost
  &shareSplit=weighted
```

The metering loop calls this once per hour, immediately after the usage bucket for the same
hour is written, and stores the result in `cogs_buckets`. When the query fails, the row is
written with nulls and a `quality = 'missing'` flag; no retries within the same hour (the
OpenCost API windows are immutable).

### Plan management

`shpyrd-ctl plans create --name starter --cpu-hour 0.02 --memory-gib-hour 0.003
--storage-gib-month 0.10 --egress-gib 0.05 --min-monthly 5.00 --currency USD
--effective 2026-10-01`; `shpyrd-ctl plans assign <workspace> starter`.
`GET /api/cluster/plans` (operator); `GET /api/workspace/billing/plan` (workspace admin).

### Rollout

| Phase | Delivers | Gate |
| --- | --- | --- |
| 0 — bench | Wake p95/p99 per StorageClass; driver connect-timeout matrix; CNPG hibernate loops | Numbers in this RFC's status; safe defaults chosen |
| 1 — ledger | Metering loop, tables, Usage cards, `shpyrd billing`, catch-up | Totals match metrics page over a known load; no silent gaps |
| 2 — customer billing | Plan schema, invoice lines, Plan card, Billing page (preview, no payment) | Month-to-date matches the ledger; plan card shows estimated invoice |
| 3 — OpenCost + operator economics | OpenCost extension, COGS buckets, economics card | Totals reconcile against OCI/AWS bill; idle % visible; margin computable |
| 4 — HTTP sleep | KEDA extension, sleep policy, resuming page / wait mode | Example apps sleep and wake for two weeks; no failed wake; auth-cache 5 s confirmed |
| 5 — Postgres sleep | Shpyrd-owned Service, pg-gateway, hibernation reconciler | p95 wake under deadline; data check; backup interaction; no lost connections |
| 6 — economics on | Small plans sleep by default; COGS-based margin visible | One month's margin computable; idle alert triggers on test cluster |
| later | Payment provider (Stripe), actual invoices, dunning; partner wholesale billing | Out of scope for this RFC |

---

## Implementation status

Updated 2026-09-28 (v0.9.21).

| Phase | State | What ships | Known gaps |
| --- | --- | --- | --- |
| 1 — ledger | **shipped, live on the first cloud** | Migration 000011; metering loop (leader-elected, 5-min buckets, 7-day catch-up, quality flags); components named after what the customer created — `web`/`worker`/`release` (process label), `postgres/<name>` (`cnpg.io/cluster`), `redis/<name>`, `build` (kpack build pods), `build-cache` (the buildpack cache claim), `volume/<name>`, `other`; storage is the capacity of the Bound volume behind each claim (what was provisioned: a 2 Gi request is a 50 Gi volume on OCI), not the request; `GET /api/workspace/usage`, `/api/projects/:slug/usage`; `shpyrd billing` | v0.9.17's loop wrote nothing (namespace mapping depended on a KSM allowlist that does not export namespace labels) — fixed in v0.9.18 by reading the Kubernetes API. Attribution needs the monitoring component's KSM allowlist from v0.9.19 (`cnpg.io/cluster`, `kpack.io/build`, `shpyrd.io/redis` on pods; claim labels); older monitoring installs attribute those to `other`. The build-cache claim is recognised by kpack's `<project>-cache` name, not a label. `usage_hourly` rollup job not written; egress attributed to `web` only; no Usage card in the UI yet. The dev cluster's first week of `default`-attributed buckets was truncated before the re-run (no customer was ever billed from it). |
| 2 — customer billing | **shipped (preview)** | `plans`, `workspace_plans`, `invoice_lines`; `shpyrd-ctl plans create/list/assign`; `GET /api/workspace/billing/current` (month-to-date at plan prices, one line per project → component → metric, per-project subtotals, `min_monthly` floor, run-rate projection; storage in GiB-months, egress in GiB); Billing card on Workspace › Overview; `shpyrd-ctl economics` takes revenue from the same preview until lines are finalised | Invoice lines are computed on read, not finalised by a monthly job; no payment provider (by design). **Finding on the first cloud**: the buildpack cache claim (2 Gi requested, rounded to OCI's 50 Gi block minimum) is the largest line of every project — $5.00/month at $0.10/GiB-month against cents of compute. Decision (Patrick, 2026-09-28): bill it, shown as `build-cache`, and redesign the cache (shared claim with per-app sub-paths, a cheaper class, or a registry-backed cache) rather than hide the cost. |
| 3 — OpenCost + economics | **shipped, not enabled** | `opencost` component (chart 2.5.32) and extension with the hourly COGS writer; `GET /api/cluster/economics`; Economics card; `shpyrd-ctl economics` | Not yet enabled on the first cloud; totals not reconciled against the OCI bill; the card is operator-only and shows an explicit empty state until then. |
| 4 — HTTP sleep | **shipped, opt-in, not yet benched** | `sleep` extension installing the `keda` and `keda-http` components; `SleepSpec` on the web process; `POST /api/projects/:slug/processes` with `sleep`; `shpyrd sleep <project> --after --resuming`; controller renders ExternalName + `InterceptorRoute` + `ScaledObject` (+ resuming-page ConfigMap) and only then moves the Ingress backend to the interceptor and shortens the auth cache to 5 s; while the policy is active KEDA owns the web Deployment's replica count (the process's instance count is the ceiling, restored on wake); a ScaledObject not Ready for 3 min pauses sleep for the app with the reason in its status; `status.processes.web.sleep` says awake / sleeping / unavailable and the UI and `projects info` show "sleeping" instead of 0 of 1 | The API refuses a policy when the cluster lacks the add-on's CRDs, and the controller keeps routing to the app's own Service if they disappear — sleep never takes an app down. Enabled on the first cloud (`sleep` extension); first measured wakes 6.1 s (wait mode) and 7.3 s (page mode) for the Go example, one sample each — the bench is still to do; `example-go` on demo sleeps with page mode from 2026-09-28 as the long-running check; `lastWakeAt`/`lastWakeDuration` not filled; workspace-level default (`off`) not implemented — every project opts in. RFC-0047 autoscaling exclusion not enforced (RFC-0047 not shipped). |
| 5 — Postgres sleep | **scaffolding only** | CRD fields (`spec.sleep.after`, `status.sleep.{state,lastActivityAt,wakePort}`); hibernation reconciler; `cmd/pg-gateway` wake-proxy; `pg-gateway` component | Not usable yet and deliberately unreachable: `shpyrd pg sleep/suspend/resume` are hidden and refuse; hibernation only runs when the `pg-gateway` Deployment is ready, and the server image does not ship `/pg-gateway`. Still missing: an activity signal that maintains `lastActivityAt` (CNPG `cnpg_backends_total` / xact metrics via Prometheus), binding host switching from `<name>-rw` to the shpyrd Service, the `waking → awake` transition, `suspend`/`resume` API routes, backup-before-sleep. The v0.9.17 build created an endpoint-less `<name>` Service for every database; v0.9.18 creates it only for databases with a policy and removes the others. |
| 0 — bench | **not started** | — | Wake times per StorageClass, driver connect-timeout matrix, CNPG hibernate loops. Gate for phase 6. |
| 6 — economics on | **not started** | — | Small plans sleep by default once phases 0 and 4 have two clean weeks. |

---

## Open questions

1. **Plan price unit for memory**: per working-set GiB-hour, or per reserved
   (request) GiB-hour? Default: **working set** (customers pay for what they use, not what
   they reserved; a floor can be added as a min-monthly).
2. **Storage cost**: charged on provisioned capacity (what the PVC requests), not the
   logical database size. This is simpler and consistent with how providers charge.
   Default: **provisioned capacity**; logical size is a display-only metric.
3. **Minimum monthly**: applied at the workspace level (the whole workspace has a $5 floor)
   or per resource? Default: **workspace level**.
4. **OpenCost without cloud credentials**: falls back to public on-demand list prices
   (OCI Price List API, AWS EC2 pricing endpoint). Good enough for the margin estimate;
   not reconciled against actual discounts. Default: **accept the approximation** until
   cloud credentials are provided.
5. **Idle allocation policy**: proportional to workload cost share (OpenCost's
   `shareIdle=proportional`) or uniform per workspace? Default: **proportional** — a
   workspace using more resources gets more of the idle cost.
6. **RFC-0047 autoscaling + sleep**: mutually exclusive per process. Default: if
   `autoscale.min > 0` is set, sleep is ignored with a warning condition. A future
   revision may allow sleep with `autoscale.min = 0`.
7. **HA databases and sleep**: `instances ≥ 2` means always-on. Default: **enforce**; an
   HA database exists to be available; sleeping it defeats its purpose.
8. **Service migration for existing databases**: opt-in (`shpyrd pg migrate-service`),
   required before sleep is enabled on a pre-existing database. New databases get the
   shpyrd Service from birth.

---

## Relationship to other RFCs

- **RFC-0033 phase 8**: this RFC specifies the billing objects (plans, invoice lines) and
  the metering that feeds them. Phase 8's "plans/subscriptions/billing" is complete when
  this RFC ships and a payment provider is wired (that integration is a separate, small RFC
  not yet written).
- **RFC-0042**: plan limits (ceilings) exist today. Plan prices are new here.
- **RFC-0047**: autoscaling. Sleep and autoscaling share the KEDA installation and are
  mutually exclusive per process.
- **RFC-0048**: superseded. This RFC is a superset; RFC-0048's proposed price sheet maps
  to this RFC's plan prices.
- **RFC-0071 (AI gateway)**: its usage events (token counts, model, cost) would add rows
  to `usage_buckets` with a `component = 'ai/<model>'` dimension and join with the
  gateway's cost table. The ledger schema accommodates this without changes.
- **RFC-0073 (data in backups)**: the workspace data export includes the billing history
  (`invoice_line` rows for the workspace) so a customer can always retrieve their invoices.
- **RFC-0033 partner billing (phase 8 D)**: once the `Partner` object exists, `cogs_buckets`
  aggregate to the partner level and the partner console shows cost per workspace.
  `invoice_lines` aggregate to the partner's wholesale terms. No changes to the ledger schema.

---

## Implementation History

- 2026-09-27: RFC written, extracting billing from RFC-0033 phase 8 and superseding
  RFC-0048.
- 2026-09-27: foundation implemented in v0.9.17: migration 000011 (usage ledger,
  plans, invoice lines, COGS, sleep events), metering loop, plans/billing APIs,
  Billing card UI, shpyrd billing / shpyrd-ctl plans+economics CLIs, HTTP sleep
  (KEDA HTTP add-on, InterceptorRoute + ScaledObject + ConfigMap, SleepSpec),
  Postgres sleep (SleepSpec + WakePort, shpyrd-owned Service, hibernation
  reconciler, pg-gateway TCP wake-proxy), OpenCost extension. Bench phase runs
  before defaults are switched on.
- 2026-09-27 (v0.9.18): first-cloud check found the metering loop silent (namespace
  mapping depended on KSM label export) and the economics card crashing the Cluster
  page (field name mismatch); both fixed. HTTP sleep loop closed: KEDA objects first,
  Ingress backend follows, API refuses without the add-on. Postgres sleep gated behind
  the gateway and hidden in the CLI; "Implementation status" section added.
- 2026-09-28 (v0.9.19): the first real preview showed everything as `default` summed
  across projects. Components are now named from labels (process, `postgres/<name>`,
  `redis/<name>`, `build`, `build-cache`, `volume/<name>`), storage counts Bound claims
  only, the preview is per project with subtotals, units are GiB-months and GiB. The
  build cache turned out to be the dominant line (OCI's 50 Gi minimum); decision: bill
  it and redesign the cache. `sleep` extension; KEDA owns replicas while a policy is
  active; `SleepStatus` wired into the process status.
- 2026-09-28 (v0.9.20): storage was metered from the claim's request (2 Gi) instead of
  the volume's capacity (50 Gi on OCI) — a 25× under-bill on the cloud's largest line.
  The metric is now the Bound PersistentVolume's capacity via its claimRef. First real
  sleep cycle on the cloud (sleep extension enabled on OKE): the server's ClusterRole
  lacked the KEDA kinds, and the ScaledObject trigger used the deprecated
  HTTPScaledObject keys — the v0.16 external scaler wants `interceptorRoute: <name>`.
  KEDA treats a trigger it cannot evaluate as inactive and scaled the app to zero at
  once, so the controller now watches the ScaledObject's Ready condition: not Ready
  for 3 minutes tears sleep down (objects deleted, instances restored, Ingress back on
  the app's Service) and pauses it for that spec generation with the reason in the
  process status; a new policy retries. Then the first wake: **6.1 s** from zero to a
  200 for a Go example (wait mode), 129 ms for the next request. Page mode did not wake
  at all — the placeholder is answered at once, so the concurrency the scaler polls is
  back to zero before it looks; page-mode routes now scale on request rate (1-minute
  window), wait-mode routes on concurrency. `initialCooldownPeriod` set, so a freshly
  enabled policy waits the quiet period instead of sleeping the app immediately. The usage ledger, the two-sided cost model (customer billing vs operator COGS),
  the OpenCost integration and the sleep mechanics (KEDA HTTP add-on + CNPG hibernation)
  are designed together because they share one data source and are incoherent apart.
  Source: internal specification 2026-09-27 (`shpyrd-cloud/docs/research/`); OpenCost
  specification v0.1 and OCI configuration docs; KEDA HTTP add-on v0.16 `InterceptorRoute`
  reference; CloudNativePG 1.27 declarative hibernation; the platform's existing metrics
  queries and controller code.
