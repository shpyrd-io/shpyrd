-- The workspace lifecycle (#274): why and when a workspace was suspended,
-- the date one reaper deletes it, an expiry for one nobody has claimed,
-- what made it, a hold against policies, when it was last used, and the
-- tombstone a deleted workspace leaves (its row stays: the ledger rows
-- reference it and are a bill).
ALTER TABLE workspaces
    ADD COLUMN IF NOT EXISTS suspended_at     TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS suspend_reason   TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS delete_after     TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS delete_reason    TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS expires_at       TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS origin           TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS created_by       TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS protected        BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS last_activity_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS deleted_at       TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS deletion         JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS tombstone_owners JSONB NOT NULL DEFAULT '[]';

-- A workspace suspended before reasons were recorded: the time is the
-- row's last change, the reason stays unrecorded ('').
UPDATE workspaces SET suspended_at = updated_at WHERE status = 'suspended' AND suspended_at IS NULL;

CREATE INDEX IF NOT EXISTS workspaces_delete_after ON workspaces (delete_after) WHERE delete_after IS NOT NULL;
CREATE INDEX IF NOT EXISTS workspaces_expires_at ON workspaces (expires_at) WHERE expires_at IS NOT NULL;
