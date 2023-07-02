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
	"time"

	log "maunium.net/go/maulogger/v2"

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
		SELECT conv_id, conv_receiver, id, mxid, sender, timestamp FROM message
		WHERE conv_id=$1 AND conv_receiver=$2 AND id=$3
	`
	getMessageByMXIDQuery = `
		SELECT conv_id, conv_receiver, id, mxid, sender, timestamp FROM message
		WHERE mxid=$1
	`
)

func (mq *MessageQuery) GetByID(ctx context.Context, chat Key, messageID string) (*Message, error) {
	return get[*Message](mq, ctx, getMessageByIDQuery, chat.ID, chat.Receiver, messageID)
}

func (mq *MessageQuery) GetByMXID(ctx context.Context, mxid id.EventID) (*Message, error) {
	return get[*Message](mq, ctx, getMessageByMXIDQuery, mxid)
}

type Message struct {
	db  *Database
	log log.Logger

	Chat      Key
	ID        string
	MXID      id.EventID
	Sender    string
	Timestamp time.Time
}

func (msg *Message) Scan(row dbutil.Scannable) (*Message, error) {
	var ts int64
	err := row.Scan(&msg.Chat.ID, &msg.Chat.Receiver, &msg.ID, &msg.MXID, &msg.Sender, &ts)
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
	return []any{msg.Chat.ID, msg.Chat.Receiver, msg.ID, msg.MXID, msg.Sender, msg.Timestamp.UnixMicro()}
}

func (msg *Message) Insert(ctx context.Context) error {
	_, err := msg.db.Conn(ctx).ExecContext(ctx, `
		INSERT INTO message (conv_id, conv_receiver, id, mxid, sender, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, msg.sqlVariables()...)
	return err
}

func (msg *Message) Delete(ctx context.Context) error {
	_, err := msg.db.Conn(ctx).ExecContext(ctx, "DELETE FROM message WHERE conv_id=$1 AND conv_receiver=$2 AND id=$3", msg.Chat.ID, msg.Chat.Receiver, msg.ID)
	return err
}
