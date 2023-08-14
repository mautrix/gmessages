-- v4: Drop conversation ID from message primary key
-- transaction: off

PRAGMA foreign_keys = OFF;
BEGIN TRANSACTION;

CREATE TABLE message_new (
    conv_id       TEXT   NOT NULL,
    conv_receiver BIGINT NOT NULL,
    id            TEXT   NOT NULL,
    mxid          TEXT   NOT NULL,
    mx_room       TEXT   NOT NULL,
    sender        TEXT   NOT NULL,
    timestamp     BIGINT NOT NULL,
    status        jsonb  NOT NULL,

    PRIMARY KEY (conv_receiver, id),
    CONSTRAINT message_portal_fkey FOREIGN KEY (conv_id, conv_receiver) REFERENCES portal(id, receiver) ON DELETE CASCADE,
    CONSTRAINT message_mxid_unique UNIQUE (mxid)
);
INSERT INTO message_new (conv_id, conv_receiver, id, mxid, mx_room, sender, timestamp, status)
    SELECT conv_id, conv_receiver, id, mxid, (SELECT mxid FROM portal WHERE id=conv_id AND receiver=conv_receiver), sender, timestamp, status
    FROM message
    WHERE status->>'type' NOT IN (101, 102, 103, 104, 105, 106, 107, 110, 111, 112, 113, 114);
DROP TABLE message;
ALTER TABLE message_new RENAME TO message;
CREATE INDEX message_conv_timestamp_idx ON message(conv_id, conv_receiver, timestamp);


CREATE TABLE reaction_new (
    conv_id       TEXT   NOT NULL,
    conv_receiver BIGINT NOT NULL,
    msg_id        TEXT   NOT NULL,
    sender        TEXT   NOT NULL,
    reaction      TEXT   NOT NULL,
    mxid          TEXT   NOT NULL,

    PRIMARY KEY (conv_receiver, msg_id, sender),
    CONSTRAINT reaction_message_fkey FOREIGN KEY (conv_receiver, msg_id) REFERENCES message(conv_receiver, id) ON DELETE CASCADE,
    CONSTRAINT reaction_mxid_unique  UNIQUE (mxid)
);
INSERT INTO reaction_new (conv_id, conv_receiver, msg_id, sender, reaction, mxid)
SELECT conv_id, conv_receiver, msg_id, sender, reaction, mxid
FROM reaction;
DROP TABLE reaction;
ALTER TABLE reaction_new RENAME TO reaction;

PRAGMA foreign_key_check;
COMMIT;
PRAGMA FOREIGN_KEYS = ON;
