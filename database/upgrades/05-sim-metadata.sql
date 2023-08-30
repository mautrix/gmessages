-- v5: Store more SIM metadata for users
ALTER TABLE "user" ADD COLUMN sim_metadata jsonb NOT NULL DEFAULT '{}';
