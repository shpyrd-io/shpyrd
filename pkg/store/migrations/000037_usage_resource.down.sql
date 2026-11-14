-- Back to one line per component: the disks of one component fold into
-- one row (the newest revision of each is kept; quantities are summed into
-- it), then the column goes.
ALTER TABLE usage_buckets DROP CONSTRAINT IF EXISTS usage_buckets_pkey;
UPDATE usage_buckets b SET quantity = s.quantity
  FROM (SELECT workspace_id, project, component, metric, period_start, revision, sum(quantity) AS quantity
        FROM usage_buckets GROUP BY workspace_id, project, component, metric, period_start, revision HAVING count(*) > 1) s
  WHERE b.workspace_id = s.workspace_id AND b.project = s.project AND b.component = s.component AND b.metric = s.metric
    AND b.period_start = s.period_start AND b.revision = s.revision AND b.resource = '';
DELETE FROM usage_buckets b USING usage_buckets o
  WHERE b.workspace_id = o.workspace_id AND b.project = o.project AND b.component = o.component AND b.metric = o.metric
    AND b.period_start = o.period_start AND b.revision = o.revision AND b.resource > o.resource;
ALTER TABLE usage_buckets DROP COLUMN IF EXISTS resource;
ALTER TABLE usage_buckets ADD PRIMARY KEY (workspace_id, project, component, metric, period_start, revision);

ALTER TABLE usage_hourly DROP CONSTRAINT IF EXISTS usage_hourly_pkey;
UPDATE usage_hourly b SET quantity = s.quantity
  FROM (SELECT workspace_id, project, component, metric, period_start, sum(quantity) AS quantity
        FROM usage_hourly GROUP BY workspace_id, project, component, metric, period_start HAVING count(*) > 1) s
  WHERE b.workspace_id = s.workspace_id AND b.project = s.project AND b.component = s.component AND b.metric = s.metric
    AND b.period_start = s.period_start AND b.resource = '';
DELETE FROM usage_hourly b USING usage_hourly o
  WHERE b.workspace_id = o.workspace_id AND b.project = o.project AND b.component = o.component AND b.metric = o.metric
    AND b.period_start = o.period_start AND b.resource > o.resource;
ALTER TABLE usage_hourly DROP COLUMN IF EXISTS resource;
ALTER TABLE usage_hourly ADD PRIMARY KEY (workspace_id, project, component, metric, period_start);
