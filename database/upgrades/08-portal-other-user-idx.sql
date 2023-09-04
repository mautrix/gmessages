-- v8: Add index for DM portals
CREATE INDEX portal_other_user_idx ON portal(receiver, other_user);
