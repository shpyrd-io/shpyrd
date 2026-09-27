-- Identifiers become native uuid columns (RFC-0033): they were UUID strings
-- in TEXT columns, 36 bytes each and unchecked. Foreign keys are dropped
-- for the type change and put back; the grants uniqueness index spelled
-- "no team" as '' and now as the nil uuid. New rows get an id from the
-- database when the caller gives none. Session ids stay TEXT: they are
-- random secrets, not identifiers.
ALTER TABLE identities   DROP CONSTRAINT identities_workspace_id_fkey;
ALTER TABLE teams        DROP CONSTRAINT teams_workspace_id_fkey;
ALTER TABLE grants       DROP CONSTRAINT grants_workspace_id_fkey;
ALTER TABLE grants       DROP CONSTRAINT grants_team_id_fkey;
ALTER TABLE sessions     DROP CONSTRAINT sessions_workspace_id_fkey;
ALTER TABLE domain_claims DROP CONSTRAINT domain_claims_workspace_id_fkey;
ALTER TABLE api_tokens   DROP CONSTRAINT api_tokens_workspace_id_fkey;
DROP INDEX grants_unique;

ALTER TABLE workspaces
    ALTER COLUMN id TYPE uuid USING id::uuid,
    ALTER COLUMN id SET DEFAULT gen_random_uuid();
ALTER TABLE identities
    ALTER COLUMN id TYPE uuid USING id::uuid,
    ALTER COLUMN id SET DEFAULT gen_random_uuid(),
    ALTER COLUMN workspace_id TYPE uuid USING workspace_id::uuid;
ALTER TABLE teams
    ALTER COLUMN id TYPE uuid USING id::uuid,
    ALTER COLUMN id SET DEFAULT gen_random_uuid(),
    ALTER COLUMN workspace_id TYPE uuid USING workspace_id::uuid;
ALTER TABLE grants
    ALTER COLUMN id TYPE uuid USING id::uuid,
    ALTER COLUMN id SET DEFAULT gen_random_uuid(),
    ALTER COLUMN workspace_id TYPE uuid USING workspace_id::uuid,
    ALTER COLUMN team_id TYPE uuid USING team_id::uuid;
ALTER TABLE sessions
    ALTER COLUMN workspace_id TYPE uuid USING workspace_id::uuid;
ALTER TABLE domain_claims
    ALTER COLUMN id TYPE uuid USING id::uuid,
    ALTER COLUMN id SET DEFAULT gen_random_uuid(),
    ALTER COLUMN workspace_id TYPE uuid USING workspace_id::uuid;
ALTER TABLE api_tokens
    ALTER COLUMN id TYPE uuid USING id::uuid,
    ALTER COLUMN id SET DEFAULT gen_random_uuid(),
    ALTER COLUMN workspace_id TYPE uuid USING workspace_id::uuid;

ALTER TABLE identities    ADD CONSTRAINT identities_workspace_id_fkey    FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE;
ALTER TABLE teams         ADD CONSTRAINT teams_workspace_id_fkey         FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE;
ALTER TABLE grants        ADD CONSTRAINT grants_workspace_id_fkey        FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE;
ALTER TABLE grants        ADD CONSTRAINT grants_team_id_fkey             FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE;
ALTER TABLE sessions      ADD CONSTRAINT sessions_workspace_id_fkey      FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE;
ALTER TABLE domain_claims ADD CONSTRAINT domain_claims_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE;
ALTER TABLE api_tokens    ADD CONSTRAINT api_tokens_workspace_id_fkey    FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE;
CREATE UNIQUE INDEX grants_unique ON grants (workspace_id, project, role, user_email, COALESCE(team_id, '00000000-0000-0000-0000-000000000000'::uuid));
