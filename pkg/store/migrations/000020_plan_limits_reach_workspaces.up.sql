-- A plan's ceilings apply to every workspace on it that has none of its
-- own (#51): a workspace's settings.limits is an override from now on, nil
-- to follow the plan. Workspaces were given a copy of their plan's ceilings
-- when they were created; those whose ceilings equal one of the versions
-- of their current plan drop the copy and follow the plan. The others keep
-- theirs as an override (an exception the operator made).
UPDATE workspaces w SET settings = w.settings - 'limits'
WHERE w.settings ? 'limits' AND EXISTS (
    SELECT 1
    FROM workspace_plans wp
    JOIN plans cur ON cur.id = wp.plan_id
    JOIN plans v ON v.name = cur.name
    WHERE wp.workspace_id = w.id AND wp.ends_at IS NULL
      AND v.limits IS NOT NULL AND v.limits = w.settings->'limits'
);
