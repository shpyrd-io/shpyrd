-- Two doors (RFC-0080): a session belongs to a realm. Console sessions have
-- no workspace; workspace sessions keep theirs. Existing rows are workspace
-- sessions (the console was the default workspace until now).
ALTER TABLE sessions ALTER COLUMN workspace_id DROP NOT NULL;
ALTER TABLE sessions ADD COLUMN realm TEXT NOT NULL DEFAULT 'workspace';
