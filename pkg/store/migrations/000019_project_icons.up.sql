-- The image a project sent for its card on the launcher: an SVG drawn as a
-- symbol in the colour chosen, or a PNG or WebP shown as it is. Keyed by
-- the project's stable ID, so it survives a change of slug.
CREATE TABLE IF NOT EXISTS project_icons (
    project_id UUID PRIMARY KEY,
    data       BYTEA NOT NULL,
    type       TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
