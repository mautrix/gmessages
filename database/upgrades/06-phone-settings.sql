-- v6: Store phone settings for users
ALTER TABLE "user" ADD COLUMN settings jsonb NOT NULL DEFAULT '{}';
