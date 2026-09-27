-- RFC-0075: usage ledger, customer billing and operator economics.
-- Plans define what a workspace is charged; usage_buckets record physical
-- consumption; invoice_lines compute cost at plan prices; cogs_buckets
-- record infrastructure cost from OpenCost; sleep_events log sleep/wake.

-- ---- Plans -----------------------------------------------------------------

-- A plan names the per-unit prices the workspace owner pays. Platform-level
-- (not per-workspace); the operator creates and assigns plans.
CREATE TABLE plans (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name                 TEXT NOT NULL UNIQUE,
    cpu_hour             NUMERIC(12,6) NOT NULL DEFAULT 0,   -- per core-hour of use
    memory_gib_hour      NUMERIC(12,6) NOT NULL DEFAULT 0,   -- per GiB-hour working set
    storage_gib_month    NUMERIC(12,6) NOT NULL DEFAULT 0,   -- per GiB-month provisioned
    egress_gib           NUMERIC(12,6) NOT NULL DEFAULT 0,   -- per GiB of HTTP egress
    min_monthly          NUMERIC(12,2) NOT NULL DEFAULT 0,   -- workspace floor, e.g. 5.00
    currency             TEXT NOT NULL DEFAULT 'USD',
    effective_from       TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A workspace's current and historical plan assignments.
CREATE TABLE workspace_plans (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    plan_id      uuid NOT NULL REFERENCES plans(id),
    starts_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    ends_at      TIMESTAMPTZ,   -- NULL = current
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX workspace_plans_current ON workspace_plans (workspace_id, ends_at NULLS LAST);

-- ---- Usage ledger ----------------------------------------------------------

-- Five-minute physical-usage buckets, kept 14 days, then rolled to hourly.
-- quantity is null when quality = 'missing'.
CREATE TABLE usage_buckets (
    workspace_id  uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    project       TEXT NOT NULL,
    component     TEXT NOT NULL,   -- 'web', 'worker', 'postgres/mydb', 'volume/vol', …
    metric        TEXT NOT NULL,   -- 'cpu_used', 'memory_used', 'cpu_reserved',
                                   -- 'storage', 'egress_http', 'instance_seconds', 'state_seconds'
    period_start  TIMESTAMPTZ NOT NULL,
    period_end    TIMESTAMPTZ NOT NULL,
    quantity      NUMERIC(18,6),   -- null when missing
    unit          TEXT NOT NULL,   -- 'core_seconds', 'gib_seconds', 'bytes', 'seconds'
    quality       TEXT NOT NULL CHECK (quality IN ('complete','partial','missing')),
    revision      INTEGER NOT NULL DEFAULT 1,
    source        TEXT NOT NULL DEFAULT '',   -- e.g. 'prom_v1'
    labels        JSONB NOT NULL DEFAULT '{}', -- e.g. {"state": "sleeping"}
    PRIMARY KEY (workspace_id, project, component, metric, period_start, revision)
);
CREATE INDEX usage_buckets_project ON usage_buckets (workspace_id, project, period_start);

-- Hourly rollup, kept 13 months. Five-minute rows are aggregated here after
-- 14 days and deleted. Range-partitioned by month in production; a single
-- table is fine for the bench and beta phases.
CREATE TABLE usage_hourly (
    workspace_id  uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    project       TEXT NOT NULL,
    component     TEXT NOT NULL,
    metric        TEXT NOT NULL,
    period_start  TIMESTAMPTZ NOT NULL,
    period_end    TIMESTAMPTZ NOT NULL,
    quantity      NUMERIC(18,6),
    unit          TEXT NOT NULL,
    quality       TEXT NOT NULL CHECK (quality IN ('complete','partial','missing')),
    revision      INTEGER NOT NULL DEFAULT 1,
    source        TEXT NOT NULL DEFAULT '',
    labels        JSONB NOT NULL DEFAULT '{}',
    PRIMARY KEY (workspace_id, project, component, metric, period_start)
);

-- ---- Invoice lines ---------------------------------------------------------

-- Computed at billing cycle end (or on demand for month-to-date preview).
-- One row per (workspace, period, component, metric); revision allows
-- recomputation when usage revisions arrive.
CREATE TABLE invoice_lines (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id  uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    period_start  TIMESTAMPTZ NOT NULL,
    period_end    TIMESTAMPTZ NOT NULL,
    component     TEXT NOT NULL,
    metric        TEXT NOT NULL,
    quantity      NUMERIC(18,6) NOT NULL DEFAULT 0,
    unit          TEXT NOT NULL,
    unit_price    NUMERIC(12,6) NOT NULL,
    gross_amount  NUMERIC(12,4) NOT NULL,
    plan_id       uuid REFERENCES plans(id),
    quality       TEXT NOT NULL DEFAULT 'complete',
    revision      INTEGER NOT NULL DEFAULT 1,
    finalized     BOOLEAN NOT NULL DEFAULT false,  -- true once the month is closed
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX invoice_lines_workspace ON invoice_lines (workspace_id, period_start, finalized);

-- ---- COGS buckets (operator economics) ------------------------------------

-- Populated by the OpenCost Allocation API query (one row per workspace per
-- hour). Never shown to customers.
CREATE TABLE cogs_buckets (
    workspace_id       uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    project            TEXT NOT NULL DEFAULT '',  -- '' = workspace aggregate
    period_start       TIMESTAMPTZ NOT NULL,
    period_end         TIMESTAMPTZ NOT NULL,
    cpu_cost           NUMERIC(12,6) NOT NULL DEFAULT 0,
    memory_cost        NUMERIC(12,6) NOT NULL DEFAULT 0,
    storage_cost       NUMERIC(12,6) NOT NULL DEFAULT 0,
    network_cost       NUMERIC(12,6) NOT NULL DEFAULT 0,
    shared_cost        NUMERIC(12,6) NOT NULL DEFAULT 0,
    idle_cost          NUMERIC(12,6) NOT NULL DEFAULT 0,
    total_cost         NUMERIC(12,6) NOT NULL DEFAULT 0,
    currency           TEXT NOT NULL DEFAULT 'USD',
    allocation_policy  TEXT NOT NULL DEFAULT '',
    quality            TEXT NOT NULL DEFAULT 'complete',
    PRIMARY KEY (workspace_id, project, period_start)
);

-- ---- Sleep events ----------------------------------------------------------

-- Every sleep and wake of a process or database.
CREATE TABLE sleep_events (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id     uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    project          TEXT NOT NULL,
    component        TEXT NOT NULL,   -- 'web', 'postgres/mydb'
    event            TEXT NOT NULL CHECK (event IN ('sleep','wake')),
    at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    duration_seconds INTEGER,         -- wake duration (seconds); null for sleep events
    reason           TEXT NOT NULL DEFAULT ''
);
CREATE INDEX sleep_events_project ON sleep_events (workspace_id, project, at DESC);
