-- RFC-0076 completion (v0.9.47): grants.project and api_tokens.project_roles
-- keys move from project slug to the project's stable short base36 ID.
-- The actual rekey is performed by store.RekeyGrantsToIDs() called from
-- Migrate(), which has access to ids.Short(). This migration records the
-- schema version; no DDL change is needed (project remains TEXT, now
-- carrying a base36 ID instead of a slug).
SELECT 1; -- intentional no-op; rekey happens in Go
