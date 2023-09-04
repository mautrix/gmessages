-- v7: Fix contact ID field
ALTER TABLE puppet RENAME COLUMN avatar_id TO contact_id;
ALTER TABLE puppet ADD COLUMN avatar_hash bytea CHECK ( LENGTH(avatar_hash) = 32 );
ALTER TABLE puppet ADD COLUMN avatar_update_ts BIGINT NOT NULL DEFAULT 0;
ALTER TABLE portal DROP COLUMN avatar_id;
ALTER TABLE portal DROP COLUMN avatar_mxc;
ALTER TABLE portal DROP COLUMN avatar_set;

-- only: postgres
ALTER TABLE puppet ALTER COLUMN avatar_update_ts DROP DEFAULT;
