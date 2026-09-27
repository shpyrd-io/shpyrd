-- The workspace's OAuth 2.1 server (RFC-0032): registered clients (MCP
-- clients such as Claude), authorization codes and refresh tokens. Secrets
-- are stored hashed; codes live minutes, refresh tokens weeks.
CREATE TABLE oauth_clients (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id  uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    client_id     TEXT NOT NULL UNIQUE,
    secret_hash   TEXT NOT NULL DEFAULT '',
    name          TEXT NOT NULL DEFAULT '',
    redirect_uris JSONB NOT NULL DEFAULT '[]',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE oauth_codes (
    hash           TEXT PRIMARY KEY,
    workspace_id   uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    client_id      TEXT NOT NULL,
    email          TEXT NOT NULL,
    subject        TEXT NOT NULL DEFAULT '',
    scope          TEXT NOT NULL DEFAULT '',
    redirect_uri   TEXT NOT NULL,
    code_challenge TEXT NOT NULL,
    resource       TEXT NOT NULL DEFAULT '',
    expires_at     TIMESTAMPTZ NOT NULL
);

CREATE TABLE oauth_tokens (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    client_id    TEXT NOT NULL,
    email        TEXT NOT NULL,
    scope        TEXT NOT NULL DEFAULT '',
    hash         TEXT NOT NULL UNIQUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at   TIMESTAMPTZ NOT NULL,
    last_used_at TIMESTAMPTZ
);
CREATE INDEX oauth_tokens_owner ON oauth_tokens (workspace_id, email);
