-- A workspace's readiness: what the controller found the last time it
-- looked at the front door (readiness, JSON), and when every check first
-- passed (ready_at). The owner's invitation may wait for that moment: the
-- email to send it to is kept in owner_invite_pending until it goes out.
ALTER TABLE workspaces ADD COLUMN IF NOT EXISTS readiness            JSONB NOT NULL DEFAULT '{}';
ALTER TABLE workspaces ADD COLUMN IF NOT EXISTS ready_at             TIMESTAMPTZ;
ALTER TABLE workspaces ADD COLUMN IF NOT EXISTS owner_invite_pending TEXT NOT NULL DEFAULT '';

-- A plan's default sleep for databases (RFC-0075), its monthly budget at
-- plan prices (0: none) and whether people may pick it for themselves.
ALTER TABLE plans ADD COLUMN IF NOT EXISTS postgres_sleep_after TEXT NOT NULL DEFAULT '';
ALTER TABLE plans ADD COLUMN IF NOT EXISTS monthly_budget       NUMERIC NOT NULL DEFAULT 0;
ALTER TABLE plans ADD COLUMN IF NOT EXISTS self_serve           BOOLEAN NOT NULL DEFAULT false;
-- The ceilings a workspace starts with on this plan (RFC-0042's limits);
-- null: none. Set per workspace afterwards as before.
ALTER TABLE plans ADD COLUMN IF NOT EXISTS limits               JSONB;
-- A free plan: nothing to pay, the bill shows consumption alone, and the
-- budget is measured against what the workspace costs the platform (COGS).
ALTER TABLE plans ADD COLUMN IF NOT EXISTS free                 BOOLEAN NOT NULL DEFAULT false;
-- The operator's cap on what a workspace on the plan may cost the platform
-- in a month (OpenCost's COGS, RFC-0075 phase 3); 0: none. Never shown.
ALTER TABLE plans ADD COLUMN IF NOT EXISTS cost_budget          NUMERIC NOT NULL DEFAULT 0;
-- A plan's prices change by versions: one row per (name, effective_from).
-- Lookups by name give the version in force; the invoice prices each
-- bucket at the version of its time, so a change never rewrites a month.
ALTER TABLE plans DROP CONSTRAINT IF EXISTS plans_name_key;
CREATE UNIQUE INDEX IF NOT EXISTS plans_name_effective ON plans (name, effective_from);
