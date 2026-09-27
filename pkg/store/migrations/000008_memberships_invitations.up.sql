-- Workspace roles and invitations (RFC-0033). A membership is a person's
-- role in the workspace, by email, so it exists before they sign in and
-- outlives their sign-in record. An invitation is a pending membership
-- with a link: the token is shown once, only its SHA-256 is kept.

CREATE TABLE memberships (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    email        TEXT NOT NULL,
    role         TEXT NOT NULL CHECK (role IN ('owner', 'admin', 'member')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, email)
);

CREATE TABLE invitations (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    email        TEXT NOT NULL,
    role         TEXT NOT NULL CHECK (role IN ('owner', 'admin', 'member')),
    team_id      uuid REFERENCES teams(id) ON DELETE SET NULL,
    token_hash   TEXT NOT NULL UNIQUE,     -- SHA-256(token) hex
    invited_by   TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at   TIMESTAMPTZ NOT NULL,
    UNIQUE (workspace_id, email)
);
