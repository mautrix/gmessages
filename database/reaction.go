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
	"fmt"
	"strings"

	"go.mau.fi/util/dbutil"
	"maunium.net/go/mautrix/id"
)

type ReactionQuery struct {
	*dbutil.QueryHelper[*Reaction]
}

func newReaction(qh *dbutil.QueryHelper[*Reaction]) *Reaction {
	return &Reaction{qh: qh}
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
	insertReactionQuery = `
		INSERT INTO reaction (conv_id, conv_receiver, msg_id, sender, reaction, mxid)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (conv_receiver, msg_id, sender)
		DO UPDATE SET reaction=excluded.reaction, mxid=excluded.mxid
	`
	deleteReactionQuery = "DELETE FROM reaction WHERE conv_id=$1 AND conv_receiver=$2 AND msg_id=$3 AND sender=$4"
)

func (rq *ReactionQuery) GetByID(ctx context.Context, receiver int, messageID, sender string) (*Reaction, error) {
	return rq.QueryOne(ctx, getReactionByIDQuery, receiver, messageID, sender)
}

func (rq *ReactionQuery) GetByMXID(ctx context.Context, mxid id.EventID) (*Reaction, error) {
	return rq.QueryOne(ctx, getReactionByMXIDQuery, mxid)
}

func (rq *ReactionQuery) GetAllByMessage(ctx context.Context, receiver int, messageID string) ([]*Reaction, error) {
	return rq.QueryMany(ctx, getReactionsByMessageIDQuery, receiver, messageID)
}

func (rq *ReactionQuery) DeleteAllByMessage(ctx context.Context, chat Key, messageID string) error {
	return rq.Exec(ctx, deleteReactionsByMessageIDQuery, chat.ID, chat.Receiver, messageID)
}

type Reaction struct {
	qh *dbutil.QueryHelper[*Reaction]

	Chat      Key
	MessageID string
	Sender    string
	Reaction  string
	MXID      id.EventID
}

func (r *Reaction) Scan(row dbutil.Scannable) (*Reaction, error) {
	return dbutil.ValueOrErr(r, row.Scan(&r.Chat.ID, &r.Chat.Receiver, &r.MessageID, &r.Sender, &r.Reaction, &r.MXID))
}

func (r *Reaction) Insert(ctx context.Context) error {
	return r.qh.Exec(ctx, insertReactionQuery, r.Chat.ID, r.Chat.Receiver, r.MessageID, r.Sender, r.Reaction, r.MXID)
}

func (rq *ReactionQuery) MassInsert(ctx context.Context, reactions []*Reaction) error {
	valueStringFormat := "($1, $2, $%d, $%d, $%d, $%d)"
	if rq.GetDB().Dialect == dbutil.SQLite {
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
	query := strings.Replace(insertReactionQuery, "($1, $2, $3, $4, $5, $6)", strings.Join(placeholders, ","), 1)
	return rq.Exec(ctx, query, params...)
}

func (r *Reaction) Delete(ctx context.Context) error {
	return r.qh.Exec(ctx, deleteReactionQuery, r.Chat.ID, r.Chat.Receiver, r.MessageID, r.Sender)
}
