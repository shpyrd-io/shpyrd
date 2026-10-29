-- A project export's one-time download (storage plan step 6) is a file on
-- the replica that built it. This row says which replica that is, keyed by
-- the hash of the download ticket, so a download that lands on another
-- replica is fetched from the right one. Rows outlive their 5-minute TTL
-- only until the next export sweeps them.
CREATE TABLE IF NOT EXISTS archive_downloads (
  hash       text PRIMARY KEY,
  payload    jsonb NOT NULL,
  expires_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS archive_downloads_expires ON archive_downloads (expires_at);
