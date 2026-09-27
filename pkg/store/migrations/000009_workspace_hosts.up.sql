-- Hosts of a workspace besides its address (RFC-0033 names): custom
-- domains in CNAME mode, and previous addresses that redirect for a while.
CREATE TABLE workspace_hosts (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    host         TEXT NOT NULL UNIQUE,
    kind         TEXT NOT NULL CHECK (kind IN ('custom', 'moved')),
    is_primary   BOOLEAN NOT NULL DEFAULT false,
    token        TEXT NOT NULL DEFAULT '',
    verified_at  TIMESTAMPTZ,
    expires_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX workspace_hosts_workspace ON workspace_hosts (workspace_id);
