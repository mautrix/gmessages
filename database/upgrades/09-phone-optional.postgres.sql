-- v9: Make phone nullable for puppets
-- transaction: off
ALTER TABLE puppet ALTER COLUMN phone DROP NOT NULL;
