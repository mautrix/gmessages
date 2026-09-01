-- v4 (compatible with v3+): Add table for deduplicating DM conversations
CREATE TABLE gmessages_direct_conversation (
    bridge_id       TEXT NOT NULL,
    login_id        TEXT NOT NULL,
    phone_number    TEXT NOT NULL,
    portal_id       TEXT NOT NULL,
    last_message_ts BIGINT,

    PRIMARY KEY (bridge_id, login_id, phone_number),
    CONSTRAINT gmessages_direct_conversation_portal_id_unique UNIQUE (bridge_id, login_id, portal_id),
    CONSTRAINT gmessages_direct_conversation_login_id_fkey FOREIGN KEY (bridge_id, login_id)
        REFERENCES user_login (bridge_id, id) ON DELETE CASCADE ON UPDATE CASCADE
);

WITH last_message_by_room AS (
    SELECT room_id, room_receiver, MAX(timestamp) AS last_message_ts
    FROM message
    GROUP BY room_id, room_receiver
)
INSERT INTO gmessages_direct_conversation (bridge_id, login_id, phone_number, portal_id, last_message_ts)
SELECT bridge_id, receiver, metadata ->> 'dm_phone_number', id, lastmsg.last_message_ts/1000
FROM portal
LEFT JOIN last_message_by_room lastmsg ON lastmsg.room_id = portal.id AND lastmsg.room_receiver = portal.receiver
WHERE room_type = 'dm' AND metadata ->> 'dm_phone_number' IS NOT NULL
ORDER BY lastmsg.last_message_ts DESC NULLS LAST
ON CONFLICT (bridge_id, login_id, phone_number) DO NOTHING;
