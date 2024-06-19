-- v11 (compatible with v10+): User settings 
ALTER TABLE "user" ADD COLUMN disable_notify_battery BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE "user" ADD COLUMN disable_notify_verbose BOOLEAN NOT NULL DEFAULT false;
