-- Back to one workspace an installation: the oldest connection stays.
DELETE FROM github_installations g USING github_installations o
 WHERE g.id = o.id AND (o.created_at, o.workspace_id::text) < (g.created_at, g.workspace_id::text);
ALTER TABLE github_installations DROP CONSTRAINT github_installations_pkey;
ALTER TABLE github_installations ADD PRIMARY KEY (id);
