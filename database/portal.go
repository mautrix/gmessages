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
	"database/sql"
	"fmt"

	"github.com/rs/zerolog"
	"go.mau.fi/util/dbutil"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
)

type PortalQuery struct {
	*dbutil.QueryHelper[*Portal]
}

func newPortal(qh *dbutil.QueryHelper[*Portal]) *Portal {
	return &Portal{qh: qh}
}

const (
	getAllPortalsQuery        = "SELECT id, receiver, self_user, other_user, type, send_mode, force_rcs, mxid, name, name_set, encrypted, in_space FROM portal"
	getAllPortalsForUserQuery = getAllPortalsQuery + " WHERE receiver=$1"
	getPortalByKeyQuery       = getAllPortalsQuery + " WHERE id=$1 AND receiver=$2"
	getPortalByOtherUserQuery = getAllPortalsQuery + " WHERE other_user=$1 AND receiver=$2"
	getPortalByMXIDQuery      = getAllPortalsQuery + " WHERE mxid=$1"
	insertPortalQuery         = `
		INSERT INTO portal (id, receiver, self_user, other_user, type, send_mode, force_rcs, mxid, name, name_set, encrypted, in_space)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`
	updatePortalQuery = `
		UPDATE portal
		SET self_user=$3, other_user=$4, type=$5, send_mode=$6, force_rcs=$7, mxid=$8, name=$9, name_set=$10, encrypted=$11, in_space=$12
		WHERE id=$1 AND receiver=$2
	`
	deletePortalQuery = "DELETE FROM portal WHERE id=$1 AND receiver=$2"
)

func (pq *PortalQuery) GetAll(ctx context.Context) ([]*Portal, error) {
	return pq.QueryMany(ctx, getAllPortalsQuery)
}

func (pq *PortalQuery) GetAllForUser(ctx context.Context, receiver int) ([]*Portal, error) {
	return pq.QueryMany(ctx, getAllPortalsForUserQuery, receiver)
}

func (pq *PortalQuery) GetByKey(ctx context.Context, key Key) (*Portal, error) {
	return pq.QueryOne(ctx, getPortalByKeyQuery, key.ID, key.Receiver)
}

func (pq *PortalQuery) GetByOtherUser(ctx context.Context, key Key) (*Portal, error) {
	return pq.QueryOne(ctx, getPortalByOtherUserQuery, key.ID, key.Receiver)
}

func (pq *PortalQuery) GetByMXID(ctx context.Context, mxid id.RoomID) (*Portal, error) {
	return pq.QueryOne(ctx, getPortalByMXIDQuery, mxid)
}

type Key struct {
	ID       string
	Receiver int
}

func (p Key) String() string {
	return fmt.Sprintf("%d.%s", p.Receiver, p.ID)
}

func (p Key) MarshalZerologObject(e *zerolog.Event) {
	e.Str("id", p.ID).Int("receiver", p.Receiver)
}

type Portal struct {
	qh *dbutil.QueryHelper[*Portal]

	Key
	OutgoingID  string
	OtherUserID string
	MXID        id.RoomID

	Type      gmproto.ConversationType
	SendMode  gmproto.ConversationSendMode
	ForceRCS  bool
	Name      string
	NameSet   bool
	Encrypted bool
	InSpace   bool
}

func (portal *Portal) Scan(row dbutil.Scannable) (*Portal, error) {
	var mxid, selfUserID, otherUserID sql.NullString
	var convType, sendMode int
	err := row.Scan(
		&portal.ID, &portal.Receiver, &selfUserID, &otherUserID, &convType, &sendMode, &portal.ForceRCS, &mxid,
		&portal.Name, &portal.NameSet, &portal.Encrypted, &portal.InSpace,
	)
	if err != nil {
		return nil, err
	}
	portal.Type = gmproto.ConversationType(convType)
	portal.SendMode = gmproto.ConversationSendMode(sendMode)
	portal.MXID = id.RoomID(mxid.String)
	portal.OutgoingID = selfUserID.String
	portal.OtherUserID = otherUserID.String
	return portal, nil
}

func (portal *Portal) sqlVariables() []any {
	return []any{
		portal.ID, portal.Receiver, dbutil.StrPtr(portal.OutgoingID), dbutil.StrPtr(portal.OtherUserID),
		int(portal.Type), int(portal.SendMode), portal.ForceRCS, dbutil.StrPtr(portal.MXID),
		portal.Name, portal.NameSet, portal.Encrypted, portal.InSpace,
	}
}

func (portal *Portal) Insert(ctx context.Context) error {
	return portal.qh.Exec(ctx, insertPortalQuery, portal.sqlVariables()...)
}

func (portal *Portal) Update(ctx context.Context) error {
	return portal.qh.Exec(ctx, updatePortalQuery, portal.sqlVariables()...)
}

func (portal *Portal) Delete(ctx context.Context) error {
	return portal.qh.Exec(ctx, deletePortalQuery, portal.ID, portal.Receiver)
}
