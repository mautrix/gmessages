-- v4: Drop conversation ID from message primary key
-- transaction: off
BEGIN TRANSACTION;

ALTER TABLE reaction DROP CONSTRAINT reaction_pkey;
ALTER TABLE reaction DROP CONSTRAINT reaction_message_fkey;
ALTER TABLE reaction ADD PRIMARY KEY (conv_receiver, msg_id, sender);

ALTER TABLE message DROP CONSTRAINT message_pkey;
DELETE FROM message WHERE (status->>'type')::integer IN (101, 102, 103, 104, 105, 106, 107, 110, 111, 112, 113, 114);
ALTER TABLE message ADD PRIMARY KEY (conv_receiver, id);
CREATE INDEX message_conv_timestamp_idx ON message(conv_id, conv_receiver, timestamp);

ALTER TABLE reaction ADD CONSTRAINT reaction_message_fkey FOREIGN KEY (conv_receiver, msg_id) REFERENCES message (conv_receiver, id) ON DELETE CASCADE;

ALTER TABLE message ADD COLUMN mx_room TEXT NOT NULL DEFAULT '';
UPDATE message SET mx_room = (SELECT mxid FROM portal WHERE id=message.conv_id AND receiver=message.conv_receiver);
ALTER TABLE message ALTER COLUMN mx_room DROP DEFAULT;

COMMIT;
