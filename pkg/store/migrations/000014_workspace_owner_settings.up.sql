-- RFC-0078: workspace ownership (operator vs customer) and a cluster-wide
-- settings table.

-- workspaces.owner distinguishes the operator's own workspaces (COGS, never
-- invoiced) from customer workspaces (revenue). Default 'customer' so every
-- existing workspace keeps its current behaviour on upgrade.
ALTER TABLE workspaces ADD COLUMN IF NOT EXISTS owner TEXT NOT NULL DEFAULT 'customer';

-- settings holds cluster-wide key→value pairs. The first entry is
-- default_workspace_id: the workspace whose dashboard the console host opens
-- (what a sign-in at the console host resolves to).
CREATE TABLE IF NOT EXISTS settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
