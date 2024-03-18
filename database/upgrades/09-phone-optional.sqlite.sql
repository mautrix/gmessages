-- v9: Make phone nullable for puppets
-- transaction: off

PRAGMA foreign_keys = OFF;
BEGIN TRANSACTION;

CREATE TABLE puppet_new (
    id               TEXT    NOT NULL,
    receiver         BIGINT  NOT NULL,
    phone            TEXT,
    contact_id       TEXT    NOT NULL,
    name             TEXT    NOT NULL,
    name_set         BOOLEAN NOT NULL DEFAULT false,
    avatar_hash      bytea            CHECK ( LENGTH(avatar_hash) = 32 ),
    avatar_update_ts BIGINT  NOT NULL,
    avatar_mxc       TEXT    NOT NULL,
    avatar_set       BOOLEAN NOT NULL DEFAULT false,
    contact_info_set BOOLEAN NOT NULL DEFAULT false,

    PRIMARY KEY (id, receiver),
    CONSTRAINT puppet_user_fkey    FOREIGN KEY (receiver) REFERENCES "user"(rowid) ON DELETE CASCADE,
    CONSTRAINT puppet_phone_unique UNIQUE (phone, receiver)
);
INSERT INTO puppet_new (id, receiver, phone, contact_id, name, name_set, avatar_hash, avatar_update_ts, avatar_mxc, avatar_set, contact_info_set)
SELECT id, receiver, phone, contact_id, name, name_set, avatar_hash, avatar_update_ts, avatar_mxc, avatar_set, contact_info_set
FROM puppet;
DROP TABLE puppet;
ALTER TABLE puppet_new RENAME TO puppet;

PRAGMA foreign_key_check;
COMMIT;
PRAGMA FOREIGN_KEYS = ON;
