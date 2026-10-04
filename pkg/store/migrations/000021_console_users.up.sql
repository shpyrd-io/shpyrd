-- The console's own users: who may open the operator's console, by email,
-- independent of any workspace. Until now the console's admins were the
-- owners and admins of the operator's default workspace; they are copied
-- here so nobody loses the console on upgrade.
CREATE TABLE IF NOT EXISTS console_users (
    email    TEXT PRIMARY KEY,
    added_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    added_by TEXT NOT NULL DEFAULT ''
);

INSERT INTO console_users (email, added_by)
SELECT DISTINCT lower(m.email), 'migration'
FROM memberships m
JOIN workspaces w ON w.id = m.workspace_id
WHERE m.role IN ('owner', 'admin')
  AND w.slug = COALESCE(NULLIF((SELECT value FROM settings WHERE key = 'default_workspace_id'), ''), 'default')
ON CONFLICT (email) DO NOTHING;
