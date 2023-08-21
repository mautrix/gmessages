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

	"go.mau.fi/util/dbutil"
	"maunium.net/go/mautrix/id"
)

type ReactionQuery struct {
	db *Database
}

func (rq *ReactionQuery) New() *Reaction {
	return &Reaction{
		db: rq.db,
	}
}

func (rq *ReactionQuery) getDB() *Database {
	return rq.db
}

const (
	getReactionByIDQuery = `
		SELECT conv_id, conv_receiver, msg_id, sender, reaction, mxid FROM reaction
		WHERE conv_receiver=$1 AND msg_id=$2 AND sender=$3
	`
	getReactionByMXIDQuery = `
		SELECT conv_id, conv_receiver, msg_id, sender, reaction, mxid FROM reaction
		WHERE mxid=$1
	`
	getReactionsByMessageIDQuery = `
		SELECT conv_id, conv_receiver, msg_id, sender, reaction, mxid FROM reaction
		WHERE conv_receiver=$1 AND msg_id=$2
	`
	deleteReactionsByMessageIDQuery = `
		SELECT conv_id, conv_receiver, msg_id, sender, reaction, mxid FROM reaction
		WHERE conv_id=$1 AND conv_receiver=$2 AND msg_id=$3
	`
	insertReaction = `
		INSERT INTO reaction (conv_id, conv_receiver, msg_id, sender, reaction, mxid)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (conv_receiver, msg_id, sender)
		DO UPDATE SET reaction=excluded.reaction, mxid=excluded.mxid
	`
)

func (rq *ReactionQuery) GetByID(ctx context.Context, receiver int, messageID, sender string) (*Reaction, error) {
	return get[*Reaction](rq, ctx, getReactionByIDQuery, receiver, messageID, sender)
}

func (rq *ReactionQuery) GetByMXID(ctx context.Context, mxid id.EventID) (*Reaction, error) {
	return get[*Reaction](rq, ctx, getReactionByMXIDQuery, mxid)
}

func (rq *ReactionQuery) GetAllByMessage(ctx context.Context, receiver int, messageID string) ([]*Reaction, error) {
	return getAll[*Reaction](rq, ctx, getReactionsByMessageIDQuery, receiver, messageID)
}

func (rq *ReactionQuery) DeleteAllByMessage(ctx context.Context, chat Key, messageID string) error {
	_, err := rq.db.Conn(ctx).ExecContext(ctx, deleteReactionsByMessageIDQuery, chat.ID, chat.Receiver, messageID)
	return err
}

type Reaction struct {
	db *Database

	Chat      Key
	MessageID string
	Sender    string
	Reaction  string
	MXID      id.EventID
}

func (r *Reaction) Scan(row dbutil.Scannable) (*Reaction, error) {
	err := row.Scan(&r.Chat.ID, &r.Chat.Receiver, &r.MessageID, &r.Sender, &r.Reaction, &r.MXID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Reaction) Insert(ctx context.Context) error {
	_, err := r.db.Conn(ctx).ExecContext(ctx, insertReaction, r.Chat.ID, r.Chat.Receiver, r.MessageID, r.Sender, r.Reaction, r.MXID)
	return err
}

func (rq *ReactionQuery) MassInsert(ctx context.Context, reactions []*Reaction) error {
	valueStringFormat := "($1, $2, $%d, $%d, $%d, $%d)"
	if rq.db.Dialect == dbutil.SQLite {
		valueStringFormat = strings.ReplaceAll(valueStringFormat, "$", "?")
	}
	placeholders := make([]string, len(reactions))
	params := make([]any, 2+len(reactions)*4)
	params[0] = reactions[0].Chat.ID
	params[1] = reactions[0].Chat.Receiver
	for i, msg := range reactions {
		baseIndex := 2 + i*4
		params[baseIndex] = msg.MessageID
		params[baseIndex+1] = msg.Sender
		params[baseIndex+2] = msg.Reaction
		params[baseIndex+3] = msg.MXID
		placeholders[i] = fmt.Sprintf(valueStringFormat, baseIndex+1, baseIndex+2, baseIndex+3, baseIndex+4)
	}
	query := strings.Replace(insertReaction, "($1, $2, $3, $4, $5, $6)", strings.Join(placeholders, ","), 1)
	_, err := rq.db.Conn(ctx).ExecContext(ctx, query, params...)
	return err
}

func (r *Reaction) Delete(ctx context.Context) error {
	_, err := r.db.Conn(ctx).ExecContext(ctx, "DELETE FROM reaction WHERE conv_id=$1 AND conv_receiver=$2 AND msg_id=$3 AND sender=$4", r.Chat.ID, r.Chat.Receiver, r.MessageID, r.Sender)
	return err
}
