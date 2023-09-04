-- v8: Add index for DM portals
CREATE INDEX ON portal(receiver, other_user);
