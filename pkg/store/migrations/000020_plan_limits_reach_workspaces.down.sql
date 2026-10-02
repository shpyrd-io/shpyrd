-- Back to copies: a workspace that follows its plan gets the ceilings of
-- the plan's version in force as its own.
UPDATE workspaces w SET settings = jsonb_set(w.settings, '{limits}', p.limits)
FROM workspace_plans wp
JOIN plans cur ON cur.id = wp.plan_id
JOIN LATERAL (
    SELECT v.limits FROM plans v
    WHERE v.name = cur.name AND v.effective_from <= now()
    ORDER BY v.effective_from DESC LIMIT 1
) p ON true
WHERE wp.workspace_id = w.id AND wp.ends_at IS NULL
  AND NOT (w.settings ? 'limits') AND p.limits IS NOT NULL;
