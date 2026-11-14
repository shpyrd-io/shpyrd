-- Usage per disk (shpyrd-cloud#70): a storage bucket names the disk it is
-- about (the PersistentVolume's name), so two disks of one component (a
-- database's instances) are two lines, each billed for at least the plan's
-- minimum, and a disk replaced under the same name is another disk. Every
-- other metric leaves it empty. The key grows with it, in both tables.
ALTER TABLE usage_buckets ADD COLUMN IF NOT EXISTS resource TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_buckets DROP CONSTRAINT IF EXISTS usage_buckets_pkey;
ALTER TABLE usage_buckets ADD PRIMARY KEY (workspace_id, project, component, resource, metric, period_start, revision);

ALTER TABLE usage_hourly ADD COLUMN IF NOT EXISTS resource TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_hourly DROP CONSTRAINT IF EXISTS usage_hourly_pkey;
ALTER TABLE usage_hourly ADD PRIMARY KEY (workspace_id, project, component, resource, metric, period_start);
