-- CLI sign-in (RFC-0052): a token of kind "session" is the person's own
-- credential, minted by the browser approval of a `shpyrd login`; it acts
-- with the owner's roles as they are, not with roles fixed at creation.
ALTER TABLE api_tokens ADD COLUMN kind TEXT NOT NULL DEFAULT 'token';
