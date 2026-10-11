-- Reverting would restore email-based authorization and can merge identities.
DO $$ BEGIN RAISE EXCEPTION 'principal migration is irreversible; restore a pre-migration database backup'; END $$;
