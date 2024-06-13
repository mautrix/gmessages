-- v10 (compatible with v9+): Store send mode for portals
ALTER TABLE portal ADD COLUMN send_mode INTEGER NOT NULL DEFAULT 0;
ALTER TABLE portal ADD COLUMN force_rcs BOOLEAN NOT NULL DEFAULT false;
