DELETE FROM sessions WHERE workspace_id IS NULL;
ALTER TABLE sessions DROP COLUMN realm;
ALTER TABLE sessions ALTER COLUMN workspace_id SET NOT NULL;
