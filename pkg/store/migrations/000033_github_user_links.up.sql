-- A person's GitHub account, linked through the GitHub App's user
-- authorization (RFC-0054): one row per shpyrd identity (the provider and
-- subject a session or the Connect ticket carries). refresh_token is sealed
-- with AES-256-GCM under the key in the Secret shpyrd-token-key; the access
-- token is never stored.
CREATE TABLE github_user_links (
 provider TEXT NOT NULL,
 subject TEXT NOT NULL,
 github_user_id BIGINT NOT NULL,
 github_login TEXT NOT NULL,
 refresh_token TEXT NOT NULL,
 refresh_expires_at TIMESTAMPTZ NOT NULL,
 linked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY (provider, subject)
);
