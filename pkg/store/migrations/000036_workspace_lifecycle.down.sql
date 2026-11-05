DROP INDEX IF EXISTS workspaces_expires_at;
DROP INDEX IF EXISTS workspaces_delete_after;
ALTER TABLE workspaces
    DROP COLUMN IF EXISTS tombstone_owners,
    DROP COLUMN IF EXISTS deletion,
    DROP COLUMN IF EXISTS deleted_at,
    DROP COLUMN IF EXISTS last_activity_at,
    DROP COLUMN IF EXISTS protected,
    DROP COLUMN IF EXISTS created_by,
    DROP COLUMN IF EXISTS origin,
    DROP COLUMN IF EXISTS expires_at,
    DROP COLUMN IF EXISTS delete_reason,
    DROP COLUMN IF EXISTS delete_after,
    DROP COLUMN IF EXISTS suspend_reason,
    DROP COLUMN IF EXISTS suspended_at;
