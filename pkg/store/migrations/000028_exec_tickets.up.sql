-- One-time shell tickets (RFC-0026) kept in the database, so a WebSocket may
-- land on any replica of the server (storage plan step 7). Rows outlive
-- their 30-second TTL only until the next mint or redeem sweeps them.
CREATE TABLE IF NOT EXISTS exec_tickets (
  hash       text PRIMARY KEY,
  payload    jsonb NOT NULL,
  expires_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS exec_tickets_expires ON exec_tickets (expires_at);
