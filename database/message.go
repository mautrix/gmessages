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

	log "maunium.net/go/maulogger/v2"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"maunium.net/go/mautrix/id"
	"maunium.net/go/mautrix/util/dbutil"
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
		SELECT conv_id, conv_receiver, id, mxid, sender, timestamp, status FROM message
		WHERE conv_id=$1 AND conv_receiver=$2 AND id=$3
	`
	getLastMessageInChatQuery = `
		SELECT conv_id, conv_receiver, id, mxid, sender, timestamp, status FROM message
		WHERE conv_id=$1 AND conv_receiver=$2
		ORDER BY timestamp DESC LIMIT 1
	`
	getMessageByMXIDQuery = `
		SELECT conv_id, conv_receiver, id, mxid, sender, timestamp, status FROM message
		WHERE mxid=$1
	`
)

func (mq *MessageQuery) GetByID(ctx context.Context, chat Key, messageID string) (*Message, error) {
	return get[*Message](mq, ctx, getMessageByIDQuery, chat.ID, chat.Receiver, messageID)
}

func (mq *MessageQuery) GetByMXID(ctx context.Context, mxid id.EventID) (*Message, error) {
	return get[*Message](mq, ctx, getMessageByMXIDQuery, mxid)
}

func (mq *MessageQuery) GetLastInChat(ctx context.Context, chat Key) (*Message, error) {
	return get[*Message](mq, ctx, getLastMessageInChatQuery, chat.ID, chat.Receiver)
}

type MessageStatus struct {
	Type binary.MessageStatusType
}

type Message struct {
	db  *Database
	log log.Logger

	Chat      Key
	ID        string
	MXID      id.EventID
	Sender    string
	Timestamp time.Time
	Status    MessageStatus
}

func (msg *Message) Scan(row dbutil.Scannable) (*Message, error) {
	var ts int64
	err := row.Scan(&msg.Chat.ID, &msg.Chat.Receiver, &msg.ID, &msg.MXID, &msg.Sender, &ts, dbutil.JSON{Data: &msg.Status})
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
	return []any{msg.Chat.ID, msg.Chat.Receiver, msg.ID, msg.MXID, msg.Sender, msg.Timestamp.UnixMicro(), dbutil.JSON{Data: &msg.Status}}
}

func (msg *Message) Insert(ctx context.Context) error {
	_, err := msg.db.Conn(ctx).ExecContext(ctx, `
		INSERT INTO message (conv_id, conv_receiver, id, mxid, sender, timestamp, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, msg.sqlVariables()...)
	return err
}

func (mq *MessageQuery) MassInsert(ctx context.Context, messages []*Message) error {
	valueStringFormat := "($1, $2, $%d, $%d, $%d, $%d, $%d)"
	if mq.db.Dialect == dbutil.SQLite {
		valueStringFormat = strings.ReplaceAll(valueStringFormat, "$", "?")
	}
	placeholders := make([]string, len(messages))
	params := make([]any, 2+len(messages)*5)
	params[0] = messages[0].Chat.ID
	params[1] = messages[0].Chat.Receiver
	for i, msg := range messages {
		baseIndex := 2 + i*5
		params[baseIndex] = msg.ID
		params[baseIndex+1] = msg.MXID
		params[baseIndex+2] = msg.Sender
		params[baseIndex+3] = msg.Timestamp.UnixMicro()
		params[baseIndex+4] = dbutil.JSON{Data: &msg.Status}
		placeholders[i] = fmt.Sprintf(valueStringFormat, baseIndex+1, baseIndex+2, baseIndex+3, baseIndex+4, baseIndex+5)
	}
	query := `
		INSERT INTO message (conv_id, conv_receiver, id, mxid, sender, timestamp, status)
		VALUES
	` + strings.Join(placeholders, ",")
	_, err := mq.db.Conn(ctx).ExecContext(ctx, query, params...)
	return err
}

func (msg *Message) UpdateStatus(ctx context.Context) error {
	_, err := msg.db.Conn(ctx).ExecContext(ctx, "UPDATE message SET status=$1 WHERE conv_id=$2 AND conv_receiver=$3 AND id=$4", dbutil.JSON{Data: &msg.Status}, msg.Chat.ID, msg.Chat.Receiver, msg.ID)
	return err
}

func (msg *Message) Delete(ctx context.Context) error {
	_, err := msg.db.Conn(ctx).ExecContext(ctx, "DELETE FROM message WHERE conv_id=$1 AND conv_receiver=$2 AND id=$3", msg.Chat.ID, msg.Chat.Receiver, msg.ID)
	return err
}
