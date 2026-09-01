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

package gmdb

import (
	"context"
	"embed"
	"strconv"

	"github.com/rs/zerolog"
	"go.mau.fi/util/dbutil"
	"go.mau.fi/util/jsontime"
	"maunium.net/go/mautrix/bridgev2/networkid"
)

type GMDB struct {
	*dbutil.Database
	DirectConversation *DirectConversationQuery
}

var table = dbutil.BuildUpgradeTable().WithFS(upgrades).Finish()

//go:embed *.sql
var upgrades embed.FS

func New(bridgeID networkid.BridgeID, db *dbutil.Database, log zerolog.Logger) *GMDB {
	db = db.Child("gmessages_version", table, dbutil.ZeroLogger(log))
	return &GMDB{
		Database: db,
		DirectConversation: &DirectConversationQuery{
			QueryHelper: dbutil.MakeQueryHelper[*DirectConversation](db, func(_ *dbutil.QueryHelper[*DirectConversation]) *DirectConversation {
				return &DirectConversation{}
			}),
			BridgeID: bridgeID,
		},
	}
}

func (db *GMDB) GetLoginPrefix(ctx context.Context, id networkid.UserLoginID) (string, error) {
	var rowID int64
	err := db.QueryRow(ctx, `
		INSERT INTO gmessages_login_prefix (login_id)
		VALUES ($1)
		ON CONFLICT (login_id) DO UPDATE SET login_id=gmessages_login_prefix.login_id
		RETURNING prefix
	`, id).Scan(&rowID)
	return strconv.FormatInt(rowID, 10), err
}

type DirectConversationQuery struct {
	*dbutil.QueryHelper[*DirectConversation]
	BridgeID networkid.BridgeID
}

func (dcq *DirectConversationQuery) GetAll(ctx context.Context, loginID networkid.UserLoginID) ([]*DirectConversation, error) {
	return dcq.QueryMany(ctx, `
		SELECT login_id, phone_number, portal_id, last_message_ts
		FROM gmessages_direct_conversation
		WHERE bridge_id = $1 AND login_id = $2
	`, dcq.BridgeID, loginID)
}

func (dcq *DirectConversationQuery) Delete(ctx context.Context, dc *DirectConversation) error {
	return dcq.Exec(ctx, `
		DELETE FROM gmessages_direct_conversation
		WHERE bridge_id = $1 AND login_id = $2 AND phone_number = $3 AND portal_id = $4
	`, dcq.BridgeID, dc.LoginID, dc.PhoneNumber, dc.PortalID)
}

func (dcq *DirectConversationQuery) Insert(ctx context.Context, dc *DirectConversation) error {
	return dcq.Exec(ctx, `
		INSERT INTO gmessages_direct_conversation (bridge_id, login_id, phone_number, portal_id, last_message_ts)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT DO NOTHING
	`, dcq.BridgeID, dc.LoginID, dc.PhoneNumber, dc.PortalID, dc.LastMessageTS)
}

func (dcq *DirectConversationQuery) UpdateLastMessage(ctx context.Context, dc *DirectConversation) error {
	return dcq.Exec(ctx, `
		UPDATE gmessages_direct_conversation
		SET last_message_ts = $5
		WHERE bridge_id = $1 AND login_id = $2 AND phone_number = $3 AND portal_id = $4
	`, dcq.BridgeID, dc.LoginID, dc.PhoneNumber, dc.PortalID, dc.LastMessageTS)
}

type DirectConversation struct {
	LoginID       networkid.UserLoginID
	PhoneNumber   string
	PortalID      networkid.PortalID
	LastMessageTS jsontime.UnixMicro
}

func (dc *DirectConversation) Scan(row dbutil.Scannable) (*DirectConversation, error) {
	err := row.Scan(&dc.LoginID, &dc.PhoneNumber, &dc.PortalID, &dc.LastMessageTS)
	return dbutil.ValueOrErr(dc, err)
}
