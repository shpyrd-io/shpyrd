DROP INDEX IF EXISTS workspaces_address;
ALTER TABLE workspaces DROP COLUMN IF EXISTS status;
ALTER TABLE workspaces DROP COLUMN IF EXISTS address;
