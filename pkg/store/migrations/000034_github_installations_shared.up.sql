-- Several workspaces may connect the same GitHub App installation
-- (RFC-0054): a row is one workspace's connection of it. Each is made by an
-- owner of the GitHub account, checked on GitHub.
ALTER TABLE github_installations DROP CONSTRAINT github_installations_pkey;
ALTER TABLE github_installations ADD PRIMARY KEY (id, workspace_id);
