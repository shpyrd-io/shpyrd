-- Old identities keep their IDs and permissions, but cannot be claimed by email.
-- The operator must explicitly associate an issuer/subject before sign-in.
ALTER TABLE identities ADD COLUMN issuer text NOT NULL DEFAULT '',
 ADD COLUMN subject text NOT NULL DEFAULT '', ADD COLUMN connector text NOT NULL DEFAULT '';
ALTER TABLE identities DROP CONSTRAINT identities_workspace_id_realm_email_key;
CREATE UNIQUE INDEX identities_external_key ON identities(workspace_id, realm, issuer, connector, subject) WHERE subject <> '';
CREATE UNIQUE INDEX identities_legacy_email ON identities(workspace_id, realm, email) WHERE subject = '';
ALTER TABLE memberships ADD COLUMN principal_id text NOT NULL DEFAULT '';
ALTER TABLE grants ADD COLUMN principal_id text NOT NULL DEFAULT '';
ALTER TABLE teams ADD COLUMN principal_ids jsonb NOT NULL DEFAULT '[]';
ALTER TABLE api_tokens ADD COLUMN principal_id text NOT NULL DEFAULT '';
ALTER TABLE oauth_tokens ADD COLUMN subject text NOT NULL DEFAULT '';
ALTER TABLE console_users ADD COLUMN principal_id text NOT NULL DEFAULT '';
-- Preserve grants for people who had never logged in as unbound principals.
-- An operator still has to map these IDs to trusted upstream subjects.
INSERT INTO identities(id,workspace_id,realm,email)
SELECT gen_random_uuid(), refs.workspace_id, 'workspace', refs.email
FROM (
 SELECT workspace_id,email FROM memberships
 UNION SELECT workspace_id,user_email FROM grants WHERE user_email <> ''
 UNION SELECT workspace_id,jsonb_array_elements_text(members) FROM teams
 UNION SELECT workspace_id,owner_email FROM api_tokens WHERE owner_email <> ''
 UNION SELECT workspace_id,email FROM oauth_tokens WHERE email <> ''
) refs
WHERE refs.email <> ''
AND NOT EXISTS (SELECT 1 FROM identities i WHERE i.workspace_id=refs.workspace_id AND i.realm='workspace' AND i.email=refs.email);
UPDATE memberships m SET principal_id = i.id::text FROM identities i WHERE i.workspace_id = m.workspace_id AND i.realm = 'workspace' AND i.email = m.email;
UPDATE grants g SET principal_id = i.id::text FROM identities i WHERE i.workspace_id = g.workspace_id AND i.realm = 'workspace' AND i.email = g.user_email;
UPDATE teams t SET principal_ids = COALESCE((SELECT jsonb_agg(i.id::text) FROM identities i WHERE i.workspace_id = t.workspace_id AND i.realm = 'workspace' AND t.members ? i.email), '[]');
UPDATE api_tokens t SET principal_id = i.id::text FROM identities i WHERE i.workspace_id = t.workspace_id AND i.realm = 'workspace' AND i.email = t.owner_email;
UPDATE oauth_tokens t SET subject = i.id::text FROM identities i WHERE i.workspace_id = t.workspace_id AND i.realm = 'workspace' AND i.email = t.email;
UPDATE console_users c SET principal_id = i.id::text FROM identities i WHERE i.realm = 'console' AND i.email = c.email;
-- Sessions issued before principal binding must authenticate again.
DELETE FROM sessions;
ALTER TABLE memberships DROP CONSTRAINT memberships_workspace_id_email_key;
CREATE UNIQUE INDEX memberships_principal_key ON memberships(workspace_id,principal_id) WHERE principal_id <> '';
CREATE UNIQUE INDEX memberships_pending_key ON memberships(workspace_id,email) WHERE principal_id = '';
DROP INDEX grants_unique;
CREATE UNIQUE INDEX grants_unique ON grants(workspace_id,project,role,principal_id,COALESCE(team_id,'00000000-0000-0000-0000-000000000000'::uuid)) WHERE principal_id <> '' OR team_id IS NOT NULL;
ALTER TABLE api_tokens DROP CONSTRAINT api_tokens_workspace_id_owner_email_name_key;
CREATE UNIQUE INDEX api_tokens_principal_name ON api_tokens(workspace_id,principal_id,name);

ALTER TABLE console_users DROP CONSTRAINT console_users_pkey;
CREATE UNIQUE INDEX console_users_principal_key ON console_users(principal_id) WHERE principal_id <> '';
CREATE UNIQUE INDEX console_users_pending_key ON console_users(email) WHERE principal_id = '';
