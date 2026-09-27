ALTER TABLE identities   DROP CONSTRAINT identities_workspace_id_fkey;
ALTER TABLE teams        DROP CONSTRAINT teams_workspace_id_fkey;
ALTER TABLE grants       DROP CONSTRAINT grants_workspace_id_fkey;
ALTER TABLE grants       DROP CONSTRAINT grants_team_id_fkey;
ALTER TABLE sessions     DROP CONSTRAINT sessions_workspace_id_fkey;
ALTER TABLE domain_claims DROP CONSTRAINT domain_claims_workspace_id_fkey;
ALTER TABLE api_tokens   DROP CONSTRAINT api_tokens_workspace_id_fkey;
DROP INDEX grants_unique;

ALTER TABLE workspaces    ALTER COLUMN id DROP DEFAULT, ALTER COLUMN id TYPE TEXT USING id::text;
ALTER TABLE identities    ALTER COLUMN id DROP DEFAULT, ALTER COLUMN id TYPE TEXT USING id::text, ALTER COLUMN workspace_id TYPE TEXT USING workspace_id::text;
ALTER TABLE teams         ALTER COLUMN id DROP DEFAULT, ALTER COLUMN id TYPE TEXT USING id::text, ALTER COLUMN workspace_id TYPE TEXT USING workspace_id::text;
ALTER TABLE grants        ALTER COLUMN id DROP DEFAULT, ALTER COLUMN id TYPE TEXT USING id::text, ALTER COLUMN workspace_id TYPE TEXT USING workspace_id::text, ALTER COLUMN team_id TYPE TEXT USING team_id::text;
ALTER TABLE sessions      ALTER COLUMN workspace_id TYPE TEXT USING workspace_id::text;
ALTER TABLE domain_claims ALTER COLUMN id DROP DEFAULT, ALTER COLUMN id TYPE TEXT USING id::text, ALTER COLUMN workspace_id TYPE TEXT USING workspace_id::text;
ALTER TABLE api_tokens    ALTER COLUMN id DROP DEFAULT, ALTER COLUMN id TYPE TEXT USING id::text, ALTER COLUMN workspace_id TYPE TEXT USING workspace_id::text;

ALTER TABLE identities    ADD CONSTRAINT identities_workspace_id_fkey    FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE;
ALTER TABLE teams         ADD CONSTRAINT teams_workspace_id_fkey         FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE;
ALTER TABLE grants        ADD CONSTRAINT grants_workspace_id_fkey        FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE;
ALTER TABLE grants        ADD CONSTRAINT grants_team_id_fkey             FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE;
ALTER TABLE sessions      ADD CONSTRAINT sessions_workspace_id_fkey      FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE;
ALTER TABLE domain_claims ADD CONSTRAINT domain_claims_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE;
ALTER TABLE api_tokens    ADD CONSTRAINT api_tokens_workspace_id_fkey    FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE;
CREATE UNIQUE INDEX grants_unique ON grants (workspace_id, project, role, user_email, COALESCE(team_id, ''));
