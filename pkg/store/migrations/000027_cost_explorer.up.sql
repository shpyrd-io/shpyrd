-- Match the stable base36 project IDs carried by cost records without loading
-- every project into the API process. The expression index supports the join.
CREATE OR REPLACE FUNCTION cost_project_short(id uuid) RETURNS text LANGUAGE plpgsql IMMUTABLE STRICT AS $$
DECLARE bytes bytea := uuid_send(id); value numeric := 0; result text := ''; digit integer;
BEGIN
 FOR i IN 0..15 LOOP value := value * 256 + get_byte(bytes,i); END LOOP;
 WHILE value > 0 LOOP
  digit := mod(value,36)::integer;
  result := substr('0123456789abcdefghijklmnopqrstuvwxyz',digit+1,1) || result;
  value := div(value,36);
 END LOOP;
 RETURN lpad(result,25,'0');
END $$;
CREATE INDEX projects_cost_short ON projects(cost_project_short(id));
-- Daily additive aggregates retain ownership and resource dimensions. Raw records
-- remain the audit trail. Corrections subtract OLD before adding NEW atomically.
CREATE OR REPLACE FUNCTION cost_category(project text, source text, process text, tags jsonb)
RETURNS text LANGUAGE sql IMMUTABLE AS $$
 SELECT CASE WHEN project <> '' THEN 'project'
 WHEN source = 'opencost' AND process = 'idle' THEN 'idle'
 WHEN source = 'opencost' AND (tags->>'cost.category' = 'system' OR process = 'kube-system') THEN 'system'
 ELSE 'unattributed' END
$$;
CREATE OR REPLACE FUNCTION cost_workload(project text, process text, metric text)
RETURNS text LANGUAGE sql IMMUTABLE AS $$
 SELECT CASE WHEN metric = 'storage' OR process = 'volumes' THEN 'storage'
 WHEN process LIKE 'postgres/%' OR process LIKE 'redis/%' THEN 'databases'
 WHEN process = 'build' THEN 'builds'
 WHEN project <> '' AND process NOT IN ('', 'other') THEN 'applications' ELSE 'other' END
$$;
CREATE TABLE cost_daily (
 day date NOT NULL, kind text NOT NULL, workspace_id text NOT NULL,
 project text NOT NULL, category text NOT NULL, workload text NOT NULL,
 process text NOT NULL, resource_type text NOT NULL, resource text NOT NULL,
 resource_name text NOT NULL, service text NOT NULL, currency text NOT NULL,
 records bigint NOT NULL, priced_records bigint NOT NULL, amount numeric NOT NULL,
 PRIMARY KEY(day,kind,workspace_id,project,category,workload,process,resource_type,resource,resource_name,service,currency)
);
CREATE INDEX cost_daily_filter ON cost_daily(kind,day,workspace_id,project);
CREATE TABLE cost_collections(kind text PRIMARY KEY, through timestamptz NOT NULL, collected_at timestamptz NOT NULL);
CREATE OR REPLACE FUNCTION update_cost_daily() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE l cost_lines; sign integer;
BEGIN
 FOR sign IN -1..1 BY 2 LOOP
  IF sign = -1 THEN
   IF TG_OP = 'INSERT' THEN CONTINUE; END IF;
   l := OLD;
  ELSE
   IF TG_OP = 'DELETE' THEN CONTINUE; END IF;
   l := NEW;
  END IF;
  INSERT INTO cost_daily VALUES (
   (l.period_start AT TIME ZONE 'UTC')::date,l.kind,l.workspace_id,l.project,
   cost_category(l.project,l.source,l.process,l.tags),cost_workload(l.project,l.process,l.metric),
   l.process,l.resource_type,l.resource,coalesce(l.tags->>'cost.resourceName',''),l.service,l.currency,
   sign,sign * (l.cost IS NOT NULL)::integer,sign * coalesce(l.cost,0)::numeric
  ) ON CONFLICT(day,kind,workspace_id,project,category,workload,process,resource_type,resource,resource_name,service,currency)
  DO UPDATE SET records=cost_daily.records+EXCLUDED.records,
   priced_records=cost_daily.priced_records+EXCLUDED.priced_records,amount=cost_daily.amount+EXCLUDED.amount;
 END LOOP;
 RETURN NULL;
END $$;
CREATE TRIGGER cost_daily_change AFTER INSERT OR UPDATE OR DELETE ON cost_lines FOR EACH ROW EXECUTE FUNCTION update_cost_daily();
INSERT INTO cost_daily
 SELECT (period_start AT TIME ZONE 'UTC')::date,kind,workspace_id,project,
 cost_category(project,source,process,tags),cost_workload(project,process,metric),process,resource_type,resource,
 coalesce(tags->>'cost.resourceName',''),service,currency,count(*),count(cost),coalesce(sum(cost::numeric),0)
 FROM cost_lines GROUP BY 1,2,3,4,5,6,7,8,9,10,11,12;
CREATE TABLE cost_exports (
 id text PRIMARY KEY, query jsonb NOT NULL, status text NOT NULL DEFAULT 'queued',
 created_at timestamptz NOT NULL DEFAULT now(), expires_at timestamptz NOT NULL DEFAULT now()+interval '24 hours'
);
CREATE TABLE cost_export_chunks (
 export_id text NOT NULL REFERENCES cost_exports(id) ON DELETE CASCADE,
 sequence integer NOT NULL, data bytea NOT NULL, PRIMARY KEY(export_id,sequence)
);
