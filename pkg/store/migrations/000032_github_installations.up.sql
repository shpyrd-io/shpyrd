-- GitHub App installations connected to workspaces (RFC-0054). The id is
-- GitHub's. A row marked deleted_at was uninstalled on GitHub; a disconnect
-- removes the row.
CREATE TABLE github_installations (
 id BIGINT PRIMARY KEY,
 workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 account_login TEXT NOT NULL,
 account_type TEXT NOT NULL,
 connected_by TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 suspended_at TIMESTAMPTZ,
 deleted_at TIMESTAMPTZ
);
CREATE INDEX github_installations_workspace ON github_installations(workspace_id) WHERE deleted_at IS NULL;
