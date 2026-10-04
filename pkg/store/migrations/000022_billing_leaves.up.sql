-- Billing leaves the platform: plans, invoices and the operator's COGS go.
-- A workspace carries its own ceilings and sleep defaults from now on.
-- Those that followed a plan keep what its version in force gave them, as
-- their own, before the plans are dropped. The budget state goes too.
WITH current AS (
    SELECT wp.workspace_id, p.limits, p.sleep_after, p.sleep_resuming, p.postgres_sleep_after
    FROM workspace_plans wp
    JOIN plans cur ON cur.id = wp.plan_id
    JOIN LATERAL (
        SELECT v.limits, v.sleep_after, v.sleep_resuming, v.postgres_sleep_after
        FROM plans v
        WHERE v.name = cur.name
        ORDER BY (v.effective_from <= now()) DESC,
                 CASE WHEN v.effective_from <= now() THEN v.effective_from END DESC,
                 v.effective_from ASC
        LIMIT 1
    ) p ON true
    WHERE wp.ends_at IS NULL
)
UPDATE workspaces w SET settings = w.settings
    || CASE WHEN NOT (w.settings ? 'limits') AND c.limits IS NOT NULL
            THEN jsonb_build_object('limits', c.limits) ELSE '{}'::jsonb END
    || CASE WHEN NOT (w.settings ? 'sleep') AND (c.sleep_after <> '' OR c.postgres_sleep_after <> '')
            THEN jsonb_build_object('sleep', jsonb_strip_nulls(jsonb_build_object(
                'appsAfter', NULLIF(c.sleep_after, ''),
                'appsResuming', NULLIF(c.sleep_resuming, ''),
                'databasesAfter', NULLIF(c.postgres_sleep_after, ''))))
            ELSE '{}'::jsonb END
FROM current c
WHERE c.workspace_id = w.id;

UPDATE workspaces SET settings = settings - 'budget' WHERE settings ? 'budget';

DROP TABLE IF EXISTS invoice_lines;
DROP TABLE IF EXISTS cogs_buckets;
DROP TABLE IF EXISTS workspace_plans;
DROP TABLE IF EXISTS plans;
