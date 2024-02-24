// mautrix-gmessages - A Matrix-Google Messages puppeting bridge.
// Copyright (C) 2024 Tulir Asokan
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
	"strings"
	"time"

	"go.mau.fi/util/dbutil"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
)

type MessageQuery struct {
	*dbutil.QueryHelper[*Message]
}

func newMessage(qh *dbutil.QueryHelper[*Message]) *Message {
	return &Message{qh: qh}
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
	deleteAllMessagesInChatQuery = `
		DELETE FROM message WHERE conv_id=$1 AND conv_receiver=$2
	`
	insertMessageQuery = `
		INSERT INTO message (conv_id, conv_receiver, id, mxid, mx_room, sender, timestamp, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	updateMessageQuery = `
		UPDATE message
		SET conv_id=$1, mxid=$4, mx_room=$5, sender=$6, timestamp=$7, status=$8
		WHERE conv_receiver=$2 AND id=$3
	`
	updateMessageStatusQuery = "UPDATE message SET status=$1, timestamp=$2 WHERE conv_receiver=$3 AND id=$4"
	deleteMessageQuery       = "DELETE FROM message WHERE conv_id=$1 AND conv_receiver=$2 AND id=$3"
)

var massInsertMessageBuilder = dbutil.NewMassInsertBuilder[*Message, [3]any](insertMessageQuery, "($1, $2, $%d, $%d, $3, $%d, $%d, $%d)")

func (mq *MessageQuery) GetByID(ctx context.Context, receiver int, messageID string) (*Message, error) {
	return mq.QueryOne(ctx, getMessageByIDQuery, receiver, messageID)
}

func (mq *MessageQuery) GetByMXID(ctx context.Context, mxid id.EventID) (*Message, error) {
	return mq.QueryOne(ctx, getMessageByMXIDQuery, mxid)
}

func (mq *MessageQuery) GetLastInChat(ctx context.Context, chat Key) (*Message, error) {
	return mq.QueryOne(ctx, getLastMessageInChatQuery, chat.ID, chat.Receiver)
}

func (mq *MessageQuery) GetLastInChatWithMXID(ctx context.Context, chat Key) (*Message, error) {
	return mq.QueryOne(ctx, getLastMessageInChatWithMXIDQuery, chat.ID, chat.Receiver)
}

func (mq *MessageQuery) DeleteAllInChat(ctx context.Context, chat Key) error {
	return mq.Exec(ctx, deleteAllMessagesInChatQuery, chat.ID, chat.Receiver)
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
	qh *dbutil.QueryHelper[*Message]

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
	if err != nil {
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
	return msg.qh.Exec(ctx, insertMessageQuery, msg.sqlVariables()...)
}

func (msg *Message) GetMassInsertValues() [5]any {
	return [...]any{msg.ID, msg.MXID, msg.Sender, msg.Timestamp.UnixMicro(), dbutil.JSON{Data: &msg.Status}}
}

func (mq *MessageQuery) MassInsert(ctx context.Context, messages []*Message) error {
	query, params := massInsertMessageBuilder.Build([3]any{messages[0].Chat.ID, messages[0].Chat.Receiver, messages[0].RoomID}, messages)
	return mq.Exec(ctx, query, params...)
}

func (msg *Message) Update(ctx context.Context) error {
	return msg.qh.Exec(ctx, updateMessageQuery, msg.sqlVariables()...)
}

func (msg *Message) UpdateStatus(ctx context.Context) error {
	return msg.qh.Exec(ctx, updateMessageStatusQuery, dbutil.JSON{Data: &msg.Status}, msg.Timestamp.UnixMicro(), msg.Chat.Receiver, msg.ID)
}

func (msg *Message) Delete(ctx context.Context) error {
	return msg.qh.Exec(ctx, deleteMessageQuery, msg.Chat.ID, msg.Chat.Receiver, msg.ID)
}

func (msg *Message) IsFakeMXID() bool {
	return strings.HasPrefix(msg.MXID.String(), "$fake:")
}
