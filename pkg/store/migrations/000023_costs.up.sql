-- Costs (ee/costs): what the cluster uses and what it costs, line by line,
-- and the drains that send those lines elsewhere. A line is keyed by what
-- it is about (its id is a hash of that), so a re-read replaces it, and
-- changed_at moves only when its numbers change: a drain sends what
-- changed since its cursor.
CREATE TABLE IF NOT EXISTS cost_lines (
    id            TEXT PRIMARY KEY,
    kind          TEXT NOT NULL,             -- usage, estimated, real
    source        TEXT NOT NULL,             -- metering, opencost, oci
    period_start  TIMESTAMPTZ NOT NULL,
    period_end    TIMESTAMPTZ NOT NULL,
    workspace_id  TEXT NOT NULL DEFAULT '',
    project       TEXT NOT NULL DEFAULT '',
    process       TEXT NOT NULL DEFAULT '',
    metric        TEXT NOT NULL DEFAULT '',
    quantity      DOUBLE PRECISION,
    unit          TEXT NOT NULL DEFAULT '',
    cost          DOUBLE PRECISION,
    currency      TEXT NOT NULL DEFAULT '',
    resource      TEXT NOT NULL DEFAULT '',
    resource_type TEXT NOT NULL DEFAULT '',
    service       TEXT NOT NULL DEFAULT '',
    sku           TEXT NOT NULL DEFAULT '',
    tags          JSONB NOT NULL DEFAULT '{}',
    changed_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS cost_lines_period ON cost_lines (period_start);
CREATE INDEX IF NOT EXISTS cost_lines_changed ON cost_lines (changed_at, id);

-- A drain: where lines go (its headers' values are in a Secret, never
-- here), and how far it got.
CREATE TABLE IF NOT EXISTS cost_drains (
    id               TEXT PRIMARY KEY,
    name             TEXT NOT NULL UNIQUE,
    url              TEXT NOT NULL,
    header_names     JSONB NOT NULL DEFAULT '[]',
    cursor_at        TIMESTAMPTZ,
    cursor_id        TEXT NOT NULL DEFAULT '',
    last_delivery_at TIMESTAMPTZ,
    sent             BIGINT NOT NULL DEFAULT 0,
    errors           BIGINT NOT NULL DEFAULT 0,
    message          TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
