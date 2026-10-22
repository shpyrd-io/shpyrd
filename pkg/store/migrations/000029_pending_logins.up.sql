-- Sign-ins in flight (RFC-0080) kept in the database, so the identity
-- provider's answer may land on any replica of the server: the shared
-- callback, which hands the browser on, and the callback at the host the
-- login started at, which completes it. Keyed by the hash of the state;
-- rows outlive their 10-minute TTL only until the next login sweeps them.
CREATE TABLE IF NOT EXISTS pending_logins (
  hash       text PRIMARY KEY,
  payload    jsonb NOT NULL,
  expires_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS pending_logins_expires ON pending_logins (expires_at);
