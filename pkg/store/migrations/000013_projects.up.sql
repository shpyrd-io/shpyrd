-- RFC-0076: projects exist in the store, keyed by their stable ID. The
-- controller mirrors every App custom resource here (identity, current slug
-- and name, namespace); deleted projects stay, marked, so invoices can name
-- them. The ledger's project columns hold the ID in its short base36 form.
CREATE TABLE projects (
    id           UUID PRIMARY KEY,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    slug         TEXT NOT NULL,
    name         TEXT NOT NULL DEFAULT '',
    namespace    TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at   TIMESTAMPTZ
);
CREATE UNIQUE INDEX projects_live_slug ON projects (workspace_id, slug) WHERE deleted_at IS NULL;
CREATE INDEX projects_workspace ON projects (workspace_id, created_at);
