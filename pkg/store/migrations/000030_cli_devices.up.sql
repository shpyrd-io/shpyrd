-- `shpyrd login` codes waiting for their approval in the browser (RFC-0052)
-- kept in the database, so the CLI's polls and the person's approval may
-- land on any replica of the server. Keyed by the hash of the device code;
-- the user code is unique within its workspace while it waits. An approved
-- row carries the minted credential only until the CLI's next poll takes
-- it, and at most ten minutes.
CREATE TABLE IF NOT EXISTS cli_devices (
  device_hash  text PRIMARY KEY,
  workspace_id text NOT NULL,
  user_code    text NOT NULL,
  name         text NOT NULL DEFAULT '',
  state        text NOT NULL DEFAULT '',
  token        text NOT NULL DEFAULT '',
  email        text NOT NULL DEFAULT '',
  token_expires_at timestamptz,
  last_poll_at timestamptz,
  expires_at   timestamptz NOT NULL,
  UNIQUE (workspace_id, user_code)
);
CREATE INDEX IF NOT EXISTS cli_devices_expires ON cli_devices (expires_at);
