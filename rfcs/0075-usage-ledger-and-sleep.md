# RFC-0075 Usage ledger, cost monitor and sleep: scale to zero for HTTP apps and PostgreSQL

**Status:** provisional

**Owner:** Patrick Negri

**Depends on:** RFC-0003 (implemented: resources), RFC-0009 (implemented: metrics),
RFC-0033 (in progress: workspaces, the edge, plans), RFC-0038 (implemented: Postgres
backups), RFC-0042 (implementable: quotas), RFC-0060 (implemented: volumes)

**Creation date:** 2026-09-27

**Last update:** 2026-09-27

## Summary

Three things that make many small, rarely used apps affordable to run, and let their owners
see what they cost:

1. **A usage ledger.** Every project's CPU, memory, storage and outbound bytes, measured
   from what already runs (Prometheus with cAdvisor, kube-state-metrics and the front
   door), closed into immutable five-minute buckets in the control-plane database, rolled
   up, kept for a year, and shown per project and per workspace — with an *estimated cost*
   when the operator sets a rate card. No invoices, credits or payments: a ledger and a
   monitor, in the physical units a bill would later use.
2. **Sleep for HTTP apps.** A `web` process nobody has asked for in a while scales to zero;
   the first request wakes it and waits, behind a branded "waking up" page when it takes
   longer than a moment. Built on the KEDA HTTP add-on's interceptor (its `InterceptorRoute`,
   v0.16), with the platform's edge still deciding who may enter.
3. **Sleep for PostgreSQL.** A database nobody has talked to in a while hibernates
   (CloudNativePG's declarative hibernation: the pods go, the volumes stay); the first
   connection wakes it. Apps keep `DATABASE_URL`: a small wake-on-connect proxy answers
   at the database's address while it sleeps and hands the connection over once it is
   ready.

Sleep is a policy the workspace or the project sets (off by default in the open source,
on by default for the cloud's small plans), never a surprise: what sleeps, when it woke and
how long it took are on the project page and in the ledger.

## Motivation

- The platform is for small software: internal tools, agents' apps, a dozen examples per
  workspace. Most are idle most of the day. A Postgres at rest costs the same as one at
  work; so does a web process. Fly.io, Neon and Supabase made scale to zero the norm for
  this class of workload; without it the cloud's plans cannot be cheap.
- Nobody can be charged for, or shown, what nobody measured. The dashboard's metrics
  (RFC-0009) are live series that Prometheus keeps for seven days; a plan's usage (RFC-0033)
  is computed from live requests. There is no record of what a project consumed last month.
- RFC-0033's cloud layer names `Metering` as the seam billing will need. This RFC builds the
  seam and its first consumer — a cost monitor — and stops before money changes hands.
- The internal specification of 2026-09-27 ("metering, scale-to-zero e PostgreSQL sob
  demanda", private repository, `docs/research/`) laid out the requirements, the state
  model and the failure modes this RFC adopts; where this RFC differs, it says why (the
  platform already has an edge and a CloudNativePG operator, so less needs building).

### Goals

- Usage per project and per workspace, by the hour and by the month, in core-seconds,
  GiB-seconds (memory and storage) and bytes out, with a quality flag when Prometheus had
  gaps; kept 13 months; exported through the API; never recomputed into a different number
  silently (revisions instead).
- An estimated cost next to the usage when the operator publishes a rate card; the plan
  card shows the month so far.
- HTTP apps that opt in (or whose workspace does) scale to zero after a quiet period and
  wake on the first request; probes and the platform's own calls never count as activity;
  the person sees a branded page during a long cold start rather than a timeout.
- Databases that opt in hibernate after a quiet period with no sessions and no transactions;
  a connection wakes them; the connection string does not change; data is never at risk
  from sleeping (clean shutdown, volumes kept, backups still taken).
- Every sleep and wake is visible: `Sleeping`/`Waking` on the project page, wake durations
  in the ledger, counters and alerts for the operator.

### Non-Goals

- Billing: prices per customer, invoices, credits, payments, taxes (RFC-0033 phase 8).
- Margin analysis against the cloud bill (Kubecost or similar): a later RFC in the private
  repository; the ledger's schema leaves room for cost buckets next to usage.
- Sleep for workers and for anything without an observable trigger (queues come later with
  KEDA's core scalers; RFC-0024's scheduled runs wake their target).
- Neon's storage architecture or its wake latency; branching.
- Terminating a client's idle session to let a database sleep: an open connection keeps the
  database awake in this version.
- A public `pg.shpyrd.app` endpoint for databases: apps reach their databases inside the
  cluster; external access to a sleeping database is a follow-up (the spec's TLS-terminating
  gateway).

## Research notes

**What exists here.** Prometheus (kube-prometheus-stack, 7-day retention) already scrapes
cAdvisor (`container_cpu_usage_seconds_total`, `container_memory_working_set_bytes`),
kube-state-metrics (pod labels including `shpyrd.io/project` and `shpyrd.io/process`, PVC
requests) and ingress-nginx (`nginx_ingress_controller_requests`,
`_request_duration_seconds`, `_response_size` per Ingress); the dashboard's metrics page
queries them (`pkg/api/metricqueries.go`). Sizes (RFC-0009 v0.9.9) make the CPU limit the
reference and a request of one eighth of it. Every app has an Ingress per host with the
edge's auth subrequest (RFC-0033); every database is a CloudNativePG `Cluster` in the
project's namespace, reached at `<name>-rw.<ns>.svc:5432`, backed up by Barman
(RFC-0038). The controller labels every pod and PVC with workspace and project.

**KEDA HTTP add-on v0.16 (September 2026).** Three components — an interceptor (a proxy the
Ingress points at), an external scaler and an operator — over KEDA core (v2.21). A route is
an `InterceptorRoute` (`http.keda.sh/v1beta1`; the older `HTTPScaledObject` is deprecated):
target Service, host and path rules, a scaling metric (concurrency or request rate), **static
routes** answered without counting as activity (health checks), a **cold-start placeholder**
response served while the backend has no ready endpoint, a fallback Service, a bounded
number of pending requests per replica (503 or placeholder past it), and per-route
`readiness`/`request`/`responseHeader` timeouts; a standard KEDA `ScaledObject` with
`minReplicaCount: 0` and `cooldownPeriod` scales the Deployment to zero when the metrics
are zero for that long. The Ingress must target the interceptor Service; from another
namespace, through an ExternalName Service. Cold start: the interceptor holds the request,
KEDA scales 0 → 1, the request is forwarded when a pod is ready.

**Why not our own activator.** The edge's auth subrequest runs before every request to an
authenticated app and could hold it while a Deployment scales up; nginx would then proxy to
endpoints that appeared meanwhile. It would save three Deployments and keep one proxy hop
less — and reimplement the interceptor's queueing, backpressure, timeouts, placeholder and
metrics, for public apps too. RFC-0067's rule applies: do not reinvent what a maintained
project provides. The activator stays as the fallback design if the add-on proves brittle
in the bench phase.

**CloudNativePG declarative hibernation.** `cnpg.io/hibernation=on` on a `Cluster` deletes
its pods (primary first, then replicas, no switchover) and keeps every PVC; the condition
`cnpg.io/hibernation` reports `Hibernated`; `off` (or removing the annotation) recreates the
pods. The operator's default monitoring queries export `cnpg_backends_total{datname,
usename, application_name, state}` and `cnpg_pg_stat_database_xact_commit|xact_rollback
{datname}`: sessions and transactions per database, the semantic idle signal the spec asks
for, once `monitoring.enablePodMonitor` is on. A hibernated cluster takes no backups and
archives no WAL: the reconciler must account for it.

**Neon and Supabase** (from their documentation): Neon suspends compute after a configurable
period, five minutes by default, and wakes on connection through its own proxy; Supabase
pauses free projects after a week of inactivity and does not pause paid ones. Both bill
storage for the whole lifecycle. This RFC follows Neon's shape (a configurable window, wake
on connect) with plain PostgreSQL and a longer default window, and does not promise Neon's
wake time: ours is a pod schedule, a volume attach and a PostgreSQL start — tens of seconds
on block storage, to be measured per StorageClass before any number is published.

## Proposal

### 1. The usage ledger

**What is measured**, per project (its namespace), every five minutes, from Prometheus:

| Metric | Unit | Query (sketch) | Notes |
| --- | --- | --- | --- |
| `cpu` | core-seconds | `sum(increase(container_cpu_usage_seconds_total{namespace, container!=""}[5m]))` | actual use; the reference (limit) and request are recorded alongside for the "reserved" view |
| `memory` | GiB-seconds | `avg_over_time(container_memory_working_set_bytes{...}[5m]) × 300` per container, summed | working set; a floor per size may apply later (rate card rule) |
| `storage` | GiB-seconds | `kube_persistentvolumeclaim_resource_requests_storage_bytes{namespace} × 300` | provisioned capacity, volumes and databases, awake or asleep |
| `egress` | bytes | `increase(nginx_ingress_controller_response_size_sum{exported_namespace, ingress}[5m])` | bytes the front door sent to clients; the platform's own hosts excluded; a proxy for internet egress, named as such |
| `instances` | instance-seconds | `count(kube_pod_status_ready{...}) × 300` by process | explains the bill: what ran, how long |
| `state` | seconds per state | from the controller (`awake`, `sleeping`, `waking`) per process and database | diagnosis, the "you saved" line |

Databases and Redis are pods and PVCs in the project's namespace: their CPU, memory and
storage land in the project's buckets with a `component` dimension (`web`, `worker`,
`postgres/<name>`, `redis/<name>`, `volume/<name>`) so the project page can split them.

**Pipeline.** A metering loop in the controller (one replica runs it, leader-elected like
the reconcilers) closes the bucket `[t, t+5m)` two minutes after `t+5m` (Prometheus scrape
interval is 30 s; the delay absorbs late scrapes), queries the six families for every
project namespace in one query each (grouped by namespace and component labels), and
writes rows:

```
usage_buckets(workspace_id, project, component, metric, period_start, period_end,
              quantity numeric, unit, quality text, revision int, source text,
              primary key (workspace_id, project, component, metric, period_start, revision))
```

`quality` is `complete`, `partial` (some scrapes missing in the window: the value is what
was seen, not an estimate) or `missing` (Prometheus unreachable: a row with quantity null,
filled by a **catch-up** pass that re-queries the last 24 hours while Prometheus still has
them — as a new revision, never an update). `source` names the query family and its
version. Buckets older than 14 days are rolled up into hourly rows (`usage_hourly`, same
shape) and the five-minute rows deleted; hourly rows older than 13 months are deleted. A
project that is destroyed keeps its rows (the workspace sees what it cost) until the
retention passes.

**API and pages.** `GET /api/projects/:slug/usage?from&to&step=hour|day&component=` and
`GET /api/workspace/usage?…` (admins; a member sees their projects) return series and
totals; the project page gets a **Usage** card (this month so far, last 30 days by day,
per component) and the workspace's Plan card gets the month's totals next to the limits.
`shpyrd usage [project] [--month 2026-09]` prints the table. The MCP server (RFC-0032) gets
`get_usage`.

**Cost monitor, not billing.** The operator may publish a **rate card**
(`shpyrd-ctl rates set --cpu-hour 0.0144 --memory-gib-hour 0.0018 --storage-gib-month 0.10
--egress-gib 0.02 --currency USD`; `GET /api/cluster/rates`), versioned with `effective_from`.
When one exists every usage view shows an **estimated cost** computed from the matching
rate version: an estimate, labelled so, with the rule (unit, rounding, floor) visible.
Nothing is owed or charged. The cloud's future billing (RFC-0033 phase 8) reads the same
buckets with its own prices per plan; the ledger does not change.

### 2. Sleep for HTTP apps

**Policy.** `sleep:` in `shpyrd.yaml` and on the project page:

```yaml
sleep:
  after: 15m        # quiet period before the web process scales to zero; "off" to never sleep
```

The workspace sets the default (`Workspace › Overview › Sleep`: off, or a duration); the
cloud's small plans set it on. Only the `web` process sleeps: workers, releases and
one-off runs are untouched. A deploy, a rollback, a scale, `shpyrd open` and the launcher
wake the app. The floor is 5 minutes, the ceiling 24 hours.

**Mechanics** (an `autosleep` extension, RFC-0002, with components `keda` and
`keda-http-add-on`, disabled by default):

- For a sleeping-enabled app the controller renders, in the project's namespace: an
  `InterceptorRoute` targeting the web Service with the app's hosts (the default host, the
  workspace's other domains, custom domains), `scalingMetric.concurrency.targetValue`
  large enough never to add replicas on its own (the person's `scale` still rules:
  `minReplicaCount 0`, `maxReplicaCount` = the desired instances), a `ScaledObject` on the
  web Deployment with `cooldownPeriod = sleep.after`, static routes for the app's health
  path and for `/.shpyrd/*` (never counted as activity), a cold-start placeholder (the
  branded "waking up" page, auto-refreshing, with the workspace's logo and colour) served
  after two seconds of waiting, `maxPendingRequests` per app, and timeouts (`readiness`
  120 s by default). An ExternalName Service `web-sleep` points at the interceptor; the
  app's Ingress backends switch to it. Everything else in the Ingress stays: TLS, the edge's
  auth subrequest, the identity headers — nginx decides who may enter, then hands the
  request to the interceptor, which holds it until the app is up and forwards it with the
  headers.
- When the policy is switched off the controller removes the objects and points the
  Ingress back at the web Service: the app is always on again, no restart.
- `status.processes.web.sleep`: `awake | sleeping | waking`, `lastActivityAt`,
  `lastWakeAt`, `lastWakeDuration`. The project page shows the state, the launcher tile a
  moon when asleep ("wakes on open"); `shpyrd projects info` prints it.
- The app's own identity (RFC-0033) is unaffected: the person is admitted by the edge
  before the interceptor; `identified` apps behave the same.
- The metrics page reads the interceptor's per-route series (requests in flight, cold
  starts, wake time) in the existing edge charts.

**Requests that keep an app awake:** any request the interceptor forwards, WebSockets
included (in-flight counts as concurrency). Requests that do not: static routes (probes,
`/.shpyrd/*`), and nothing else. A cron job (RFC-0024) that calls the app wakes it,
correctly.

### 3. Sleep for PostgreSQL

**Policy.** On the Postgres resource: `sleep.after` (default off; the workspace default
applies), `shpyrd pg sleep <name> --after 30m|off`. Only single-instance databases sleep
(an HA database exists to be available). A database with `backups` keeps them (below).

**States**, on the Postgres resource's status (`status.sleep`):

| State | Meaning | Invariant |
| --- | --- | --- |
| `awake` | pods run, PostgreSQL ready, endpoints are the pods | `Ready=True` |
| `sleeping` | `cnpg.io/hibernation=on`, no pods, PVCs kept; the wake proxy answers at the address | condition `Hibernated`; PVCs exist |
| `waking` | annotation removed, pods scheduling, PostgreSQL recovering; connections wait | one wake per generation |
| `suspended` | asleep by a person's or the operator's hand (`shpyrd pg suspend`); no wake on connect | resume is explicit |

Errors are conditions (`Ready=False`, `Degraded=True` with a reason), never a state:
a database that cannot wake is degraded, not asleep.

**Going to sleep** (the Postgres reconciler, every minute for candidates):

1. `sleep.after` elapsed since `lastActivityAt`, where activity is the newest of: any
   backend not `idle` in `cnpg_backends_total{datname="app"}` (the platform's own health
   checks run against the `postgres` database and are excluded by `datname`), an increase
   of `xact_commit + xact_rollback` on the app database, a session through the wake proxy,
   a backup or a `shpyrd pg` command. An open but idle client session keeps the database
   awake in this version: the app is the client, and a sleeping app has no sessions.
2. Under a lease per database, re-read the signals; abort (`suspend_abort_total{reason}`) if
   anything changed, if a backup is running, or if Prometheus is unreachable (no signal, no
   sleep).
3. If scheduled backups are on and the next one falls inside the sleep, take a backup first
   (RFC-0038's on-demand backup) — a hibernated cluster cannot be backed up and archives no
   WAL; the last WAL segment is archived by the clean shutdown.
4. Set `cnpg.io/hibernation=on`; wait for the `Hibernated` condition; switch the app-facing
   Service to the wake proxy; record `sleeping`.

**The address stays.** Apps read `DATABASE_HOST=<name>-rw.<ns>.svc` today, CNPG's own
Service. The controller takes ownership of the name apps use: a shpyrd-managed Service
`<name>` (CNPG's `managed.services` can disable or rename its defaults; existing
databases keep `-rw` as an alias) whose selector points at the primary while awake and at
the platform's wake proxy while asleep, with the database's own **port** on the proxy
(`targetPort` per database from a range the controller allocates), so the proxy knows
which database a connection is for **from the port**, without reading a byte of the
protocol.

**Waking** (`pg-gateway`, a small Deployment in the system namespace, two replicas, one
listener per sleeping database):

1. A connection arrives on the database's port. The proxy asks the controller to wake
   (`EnsureAwake(resource, generation)`, idempotent: the first caller removes the
   annotation, the others wait on the same operation) and keeps the socket open without
   reading it.
2. It waits for the Postgres resource to report `awake` (pods ready, PostgreSQL accepting
   connections, endpoints published) with a deadline (`maxWakeWait`, 60 s by default) and a
   bounded queue per database and globally (past it, connections are closed at once so the
   client retries rather than hangs).
3. It dials the primary pod and pipes bytes both ways for the life of the session — TLS,
   `SSLRequest`, `CancelRequest` and all: the proxy never interprets the stream, the same
   bytes reach PostgreSQL as if the client had dialled it. Sessions in flight when the
   Service switches back to the pods stay on the proxy until they end.
4. The controller records `lastWakeAt`, `lastWakeDuration` (schedule, attach, ready as
   phases in a histogram) and flips the Service back.

Clients see a slow first connection, not an error — provided their connect timeout exceeds
`maxWakeWait`; the docs say so and the app examples set `connect_timeout=90`. A
connection to a `suspended` database is closed after a PostgreSQL error message
("database suspended; resume it in the dashboard"), which drivers show as such.

**Data safety.** Hibernation is CNPG's clean shutdown (checkpoint, ordered pod removal);
the PVCs stay, and `shpyrd pg` refuses to delete a PVC on sleep. Backups continue as above.
Recovery on wake is PostgreSQL's normal start after a clean shutdown. Sleep is not a
backup: RFC-0038 is.

### 4. What a person sees

- Project page: Usage card (this month, by component, with the estimate when there is a
  rate card); each process and database with `awake`/`asleep`/`waking` and the last wake's
  duration; a Sleep setting per app and per database.
- Workspace: Plan card with the month's totals; Sleep default; a "Sleeping now" count.
- Launcher: a moon on asleep apps; opening wakes.
- The waking page: the workspace's logo and colour, "Waking up <app>…", refreshes itself,
  falls back to a plain "still starting" after the readiness timeout.
- CLI: `shpyrd usage`, `shpyrd projects info` (sleep state), `shpyrd sleep <project>
  --after 15m|off`, `shpyrd pg sleep|suspend|resume`, `shpyrd-ctl rates set|show`.

## Design Details

- **Store**: `usage_buckets`, `usage_hourly` (partitioned by month), `rate_cards`
  (`id, effective_from, currency, prices jsonb`), `sleep_events` (project, component,
  event `sleep|wake`, at, duration, reason); all keyed by workspace id (RFC-0033).
- **Metering loop**: one query per metric family per bucket, `sum by (namespace, component)`
  with `component` from pod labels (`shpyrd.io/process`, `shpyrd.io/postgres`,
  `shpyrd.io/redis`) via `kube_pod_labels`; namespaces resolved to `(workspace, project)`
  through labels, never by name. Two minutes of closing delay; a catch-up of the last 24 h
  on start and hourly; `metering_missing_intervals_total` and a Grafana panel.
- **Numbers**: `numeric` in the store; core-seconds and GiB-seconds as integers of
  millicore-seconds and MiB-seconds to avoid floating drift; the API converts to hours.
- **Rate card math**: `cost = Σ quantity × price(metric, version at period_start)`, rounding
  per rule (`half-up to cents`, floors per metric optional), shown as "estimated".
- **KEDA objects**: rendered by the App controller like Ingresses; `keda` and
  `keda-http-add-on` Helm charts as components (`rc3`); the interceptor with two replicas,
  `KEDA_HTTP_COLD_START_MAX_PENDING_REQUESTS` sized per cluster, a NetworkPolicy admitting
  the ingress controllers and admitting it into project namespaces (the base policy admits
  platform namespaces already).
- **Placeholder page**: the `InterceptorRoute` placeholder with `bodyFromConfigMap`; the
  controller renders the ConfigMap with the workspace's branding (RFC-0033) per project;
  `Retry-After: 2` and a meta refresh; 503 while waking so crawlers do not index it.
- **CNPG**: `monitoring.enablePodMonitor: true` on every cluster (Prometheus already has
  the operator's PodMonitor CRD); hibernation through the annotation; `managed.services`
  to own the app-facing name; readiness = `Ready` condition plus a TCP dial by the
  controller before `awake` is announced.
- **pg-gateway**: Go, `net.Listener` per allocated port from a ConfigMap the controller
  writes (`port → namespace/name`); leases and waiters per database; `pg_gateway_*`
  metrics as the spec lists (`connections_active`, `connections_pending`,
  `connection_errors_total{reason}`, `bytes_{in,out}_total`, `wake_duration_seconds{phase}`,
  `wake_deduplicated_total`); a PodDisruptionBudget; two replicas behind one Service (a
  wake is idempotent, so either replica may start it).
- **Security**: nothing new is exposed. The database Service stays in the project's
  namespace; the proxy is reachable only from project namespaces on their own ports; the
  proxy holds no credentials and reads no protocol. The interceptor is reachable from the
  ingress controllers only.
- **Metrics for operators**: `shpyrd_sleep_total{kind, outcome}`, `shpyrd_wake_total{kind,
  outcome}`, `shpyrd_wake_duration_seconds{kind, phase}`, `shpyrd_sleeping{kind}`
  (gauge), the metering counters above; alerts on wake failures and missing intervals.
- **Bench phase deliverables** (before any default is switched on): p50/p95/p99 wake for a
  web process (image pull warm/cold) and for Postgres per StorageClass (OCI block volume,
  EBS gp3, EFS/FSS), the interceptor's behaviour with WebSockets and long polls, drivers'
  behaviour with a 60 s connect (psycopg, pg (node), pgx, ActiveRecord, Prisma), CNPG
  hibernate/rehydrate loops with data checks.

## Rollout

| Phase | Delivers | Gate |
| --- | --- | --- |
| 0 — bench | wake times per StorageClass and image; driver matrix; CNPG loops | numbers published in the RFC's status; safe defaults chosen |
| 1 — ledger | metering loop, tables, API, Usage cards, `shpyrd usage` | totals match the metrics page and known load tests; gaps flagged, none silent |
| 2 — HTTP sleep | `autosleep` extension, `sleep:` policy, waking page, status; opt-in on the examples | example apps sleep and wake for a week without a failed request in logs beyond the placeholder |
| 3 — Postgres sleep | owned Service, pg-gateway, hibernation reconciler; internal databases first | wake under the deadline at p95, data checks, backups taken, no lost connection reports |
| 4 — cost monitor | rate card, estimates, workspace totals; sleep defaults on the cloud's small plans | one month of ledger reconciled against the cluster's resource totals |
| later | Kubecost or a cost model against the cloud bill (private); billing (RFC-0033 phase 8) | — |

## Open questions

1. **Own activator or KEDA's interceptor?** Default: **KEDA HTTP add-on**, as researched;
   the edge-held activator stays the fallback if the bench finds the add-on brittle.
2. **Bucket size**: five minutes closed with a two-minute delay, hourly after 14 days?
   Default: **yes**.
3. **Memory floor per size** in the estimate (a `small` costs at least its request)?
   Default: **no floor in the ledger** (it records what was used); a floor is a rate-card
   rule when billing arrives.
4. **Sleep defaults**: off in the open source, on (15 min apps, 30 min databases) for the
   cloud's small plans? Default: **yes**; always-on for plans that say so.
5. **Idle client sessions**: keep the database awake (this version) or terminate them
   after a longer window with a warning? Default: **keep awake**; revisit with pooling.
6. **The app-facing database Service**: shpyrd-owned name with CNPG's `-rw` kept as alias,
   or point everything at the proxy permanently (simpler, one more hop always)? Default:
   **owned Service, flipped**; measure the hop before choosing otherwise.
7. **Egress source**: the front door's response bytes now, a real egress gateway later?
   Default: **front door**, labelled "HTTP egress".
8. **Where the metering loop runs**: the controller (leader-elected, has Prometheus and the
   store) or a CronJob? Default: **controller**.

## Implementation History

- 2026-09-27: RFC written from the internal specification of the same day (research;
  provisional). Sources: KEDA HTTP add-on v0.16 `InterceptorRoute` reference and scaling
  concepts; CloudNativePG declarative hibernation and default monitoring queries; Neon
  and Supabase documentation on scale to zero and pausing; the platform's own metrics
  queries and controllers.
