-- v2: Store all self-participant IDs
ALTER TABLE "user" RENAME COLUMN phone TO phone_id;
ALTER TABLE "user" ADD COLUMN self_participant_ids jsonb NOT NULL DEFAULT '[]';
