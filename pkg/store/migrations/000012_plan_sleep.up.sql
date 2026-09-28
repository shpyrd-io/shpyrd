-- RFC-0075: a plan's default sleep policy for HTTP apps. Projects without a
-- policy of their own inherit it; an explicit "off" on a project opts out.
ALTER TABLE plans ADD COLUMN IF NOT EXISTS sleep_after    TEXT NOT NULL DEFAULT '';
ALTER TABLE plans ADD COLUMN IF NOT EXISTS sleep_resuming TEXT NOT NULL DEFAULT '';
