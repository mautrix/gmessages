// mautrix-gmessages - A Matrix-Google Messages puppeting bridge.
// Copyright (C) 2023 Tulir Asokan
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mau.fi/util/dbutil"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
)

type MessageQuery struct {
	db *Database
}

func (mq *MessageQuery) New() *Message {
	return &Message{
		db: mq.db,
	}
}

func (mq *MessageQuery) getDB() *Database {
	return mq.db
}

const (
	getMessageByIDQuery = `
		SELECT conv_id, conv_receiver, id, mxid, mx_room, sender, timestamp, status FROM message
		WHERE conv_receiver=$1 AND id=$2
	`
	getLastMessageInChatQuery = `
		SELECT conv_id, conv_receiver, id, mxid, mx_room, sender, timestamp, status FROM message
		WHERE conv_id=$1 AND conv_receiver=$2
		ORDER BY timestamp DESC LIMIT 1
	`
	getLastMessageInChatWithMXIDQuery = `
		SELECT conv_id, conv_receiver, id, mxid, mx_room, sender, timestamp, status FROM message
		WHERE conv_id=$1 AND conv_receiver=$2 AND mxid NOT LIKE '$fake::%'
		ORDER BY timestamp DESC LIMIT 1
	`
	getMessageByMXIDQuery = `
		SELECT conv_id, conv_receiver, id, mxid, mx_room, sender, timestamp, status FROM message
		WHERE mxid=$1
	`
	deleteAllInChat = `
		DELETE FROM message WHERE conv_id=$1 AND conv_receiver=$2
	`
)

func (mq *MessageQuery) GetByID(ctx context.Context, receiver int, messageID string) (*Message, error) {
	return get[*Message](mq, ctx, getMessageByIDQuery, receiver, messageID)
}

func (mq *MessageQuery) GetByMXID(ctx context.Context, mxid id.EventID) (*Message, error) {
	return get[*Message](mq, ctx, getMessageByMXIDQuery, mxid)
}

func (mq *MessageQuery) GetLastInChat(ctx context.Context, chat Key) (*Message, error) {
	return get[*Message](mq, ctx, getLastMessageInChatQuery, chat.ID, chat.Receiver)
}

func (mq *MessageQuery) GetLastInChatWithMXID(ctx context.Context, chat Key) (*Message, error) {
	return get[*Message](mq, ctx, getLastMessageInChatWithMXIDQuery, chat.ID, chat.Receiver)
}

func (mq *MessageQuery) DeleteAllInChat(ctx context.Context, chat Key) error {
	_, err := mq.db.Conn(ctx).ExecContext(ctx, deleteAllInChat, chat.ID, chat.Receiver)
	return err
}

type MediaPart struct {
	EventID      id.EventID `json:"mxid,omitempty"`
	PendingMedia bool       `json:"pending_media,omitempty"`
}

type MessageStatus struct {
	Type gmproto.MessageStatusType `json:"type,omitempty"`

	MediaStatus string               `json:"media_status,omitempty"`
	MediaParts  map[string]MediaPart `json:"media_parts,omitempty"`
	PartCount   int                  `json:"part_count,omitempty"`

	MSSSent         bool `json:"mss_sent,omitempty"`
	MSSFailSent     bool `json:"mss_fail_sent,omitempty"`
	MSSDeliverySent bool `json:"mss_delivery_sent,omitempty"`
	ReadReceiptSent bool `json:"read_receipt_sent,omitempty"`
}

func (ms *MessageStatus) HasPendingMediaParts() bool {
	for _, part := range ms.MediaParts {
		if part.PendingMedia {
			return true
		}
	}
	return false
}

type Message struct {
	db *Database

	Chat      Key
	ID        string
	MXID      id.EventID
	RoomID    id.RoomID
	Sender    string
	Timestamp time.Time
	Status    MessageStatus
}

func (msg *Message) Scan(row dbutil.Scannable) (*Message, error) {
	var ts int64
	err := row.Scan(&msg.Chat.ID, &msg.Chat.Receiver, &msg.ID, &msg.MXID, &msg.RoomID, &msg.Sender, &ts, dbutil.JSON{Data: &msg.Status})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	if ts != 0 {
		msg.Timestamp = time.UnixMicro(ts)
	}
	return msg, nil
}

func (msg *Message) sqlVariables() []any {
	return []any{msg.Chat.ID, msg.Chat.Receiver, msg.ID, msg.MXID, msg.RoomID, msg.Sender, msg.Timestamp.UnixMicro(), dbutil.JSON{Data: &msg.Status}}
}

func (msg *Message) Insert(ctx context.Context) error {
	_, err := msg.db.Conn(ctx).ExecContext(ctx, `
		INSERT INTO message (conv_id, conv_receiver, id, mxid, mx_room, sender, timestamp, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, msg.sqlVariables()...)
	return err
}

func (mq *MessageQuery) MassInsert(ctx context.Context, messages []*Message) error {
	valueStringFormat := "($1, $2, $%d, $%d, $3, $%d, $%d, $%d)"
	if mq.db.Dialect == dbutil.SQLite {
		valueStringFormat = strings.ReplaceAll(valueStringFormat, "$", "?")
	}
	placeholders := make([]string, len(messages))
	params := make([]any, 3+len(messages)*5)
	params[0] = messages[0].Chat.ID
	params[1] = messages[0].Chat.Receiver
	params[2] = messages[0].RoomID
	for i, msg := range messages {
		baseIndex := 3 + i*5
		params[baseIndex] = msg.ID
		params[baseIndex+1] = msg.MXID
		params[baseIndex+2] = msg.Sender
		params[baseIndex+3] = msg.Timestamp.UnixMicro()
		params[baseIndex+4] = dbutil.JSON{Data: &msg.Status}
		placeholders[i] = fmt.Sprintf(valueStringFormat, baseIndex+1, baseIndex+2, baseIndex+3, baseIndex+4, baseIndex+5)
	}
	query := `
		INSERT INTO message (conv_id, conv_receiver, id, mxid, mx_room, sender, timestamp, status)
		VALUES
	` + strings.Join(placeholders, ",")
	_, err := mq.db.Conn(ctx).ExecContext(ctx, query, params...)
	return err
}

func (msg *Message) Update(ctx context.Context) error {
	_, err := msg.db.Conn(ctx).ExecContext(ctx, `
		UPDATE message
		SET conv_id=$1, mxid=$4, mx_room=$5, sender=$6, timestamp=$7, status=$8
		WHERE conv_receiver=$2 AND id=$3
	`, msg.sqlVariables()...)
	return err
}

func (msg *Message) UpdateStatus(ctx context.Context) error {
	_, err := msg.db.Conn(ctx).ExecContext(ctx, "UPDATE message SET status=$1, timestamp=$2 WHERE conv_receiver=$3 AND id=$4", dbutil.JSON{Data: &msg.Status}, msg.Timestamp.UnixMicro(), msg.Chat.Receiver, msg.ID)
	return err
}

func (msg *Message) Delete(ctx context.Context) error {
	_, err := msg.db.Conn(ctx).ExecContext(ctx, "DELETE FROM message WHERE conv_id=$1 AND conv_receiver=$2 AND id=$3", msg.Chat.ID, msg.Chat.Receiver, msg.ID)
	return err
}

func (msg *Message) IsFakeMXID() bool {
	return strings.HasPrefix(msg.MXID.String(), "$fake:")
}
