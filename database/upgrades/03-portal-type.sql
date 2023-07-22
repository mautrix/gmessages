-- v3: Store portal conversation type
ALTER TABLE portal ADD COLUMN type INTEGER NOT NULL DEFAULT 0;
