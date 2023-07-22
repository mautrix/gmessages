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

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/id"
	"maunium.net/go/mautrix/util/dbutil"

	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
)

type PortalQuery struct {
	db *Database
}

func (pq *PortalQuery) New() *Portal {
	return &Portal{
		db: pq.db,
	}
}

func (pq *PortalQuery) getDB() *Database {
	return pq.db
}

func (pq *PortalQuery) GetAll(ctx context.Context) ([]*Portal, error) {
	return getAll[*Portal](pq, ctx, "SELECT id, receiver, self_user, other_user, type, mxid, name, name_set, avatar_id, avatar_mxc, avatar_set, encrypted, in_space FROM portal")
}

func (pq *PortalQuery) GetAllForUser(ctx context.Context, receiver int) ([]*Portal, error) {
	return getAll[*Portal](pq, ctx, "SELECT id, receiver, self_user, other_user, type, mxid, name, name_set, avatar_id, avatar_mxc, avatar_set, encrypted, in_space FROM portal WHERE receiver=$1", receiver)
}

func (pq *PortalQuery) GetByKey(ctx context.Context, key Key) (*Portal, error) {
	return get[*Portal](pq, ctx, "SELECT id, receiver, self_user, other_user, type, mxid, name, name_set, avatar_id, avatar_mxc, avatar_set, encrypted, in_space FROM portal WHERE id=$1 AND receiver=$2", key.ID, key.Receiver)
}

func (pq *PortalQuery) GetByMXID(ctx context.Context, mxid id.RoomID) (*Portal, error) {
	return get[*Portal](pq, ctx, "SELECT id, receiver, self_user, other_user, type, mxid, name, name_set, avatar_id, avatar_mxc, avatar_set, encrypted, in_space FROM portal WHERE mxid=$1", mxid)
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
	db *Database

	Key
	OutgoingID  string
	OtherUserID string
	MXID        id.RoomID

	Type      gmproto.ConversationType
	Name      string
	NameSet   bool
	AvatarID  string
	AvatarMXC id.ContentURI
	AvatarSet bool
	Encrypted bool
	InSpace   bool
}

func (portal *Portal) Scan(row dbutil.Scannable) (*Portal, error) {
	var mxid, selfUserID, otherUserID sql.NullString
	var convType int
	err := row.Scan(&portal.ID, &portal.Receiver, &selfUserID, &otherUserID, &convType, &mxid, &portal.Name, &portal.NameSet, &portal.AvatarID, &portal.AvatarMXC, &portal.AvatarSet, &portal.Encrypted, &portal.InSpace)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	portal.Type = gmproto.ConversationType(convType)
	portal.MXID = id.RoomID(mxid.String)
	portal.OutgoingID = selfUserID.String
	portal.OtherUserID = otherUserID.String
	return portal, nil
}

func (portal *Portal) sqlVariables() []any {
	var mxid, selfUserID, otherUserID *string
	if portal.MXID != "" {
		mxid = (*string)(&portal.MXID)
	}
	if portal.OutgoingID != "" {
		selfUserID = &portal.OutgoingID
	}
	if portal.OtherUserID != "" {
		otherUserID = &portal.OtherUserID
	}
	return []any{portal.ID, portal.Receiver, selfUserID, otherUserID, int(portal.Type), mxid, portal.Name, portal.NameSet, portal.AvatarID, &portal.AvatarMXC, portal.AvatarSet, portal.Encrypted, portal.InSpace}
}

func (portal *Portal) Insert(ctx context.Context) error {
	_, err := portal.db.Conn(ctx).ExecContext(ctx, `
		INSERT INTO portal (id, receiver, self_user, other_user, type, mxid, name, name_set, avatar_id, avatar_mxc, avatar_set, encrypted, in_space)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`, portal.sqlVariables()...)
	return err
}

func (portal *Portal) Update(ctx context.Context) error {
	_, err := portal.db.Conn(ctx).ExecContext(ctx, `
		UPDATE portal
		SET self_user=$3, other_user=$4, type=$5, mxid=$6, name=$7, name_set=$8, avatar_id=$9, avatar_mxc=$10, avatar_set=$11, encrypted=$12, in_space=$13
		WHERE id=$1 AND receiver=$2
	`, portal.sqlVariables()...)
	return err
}

func (portal *Portal) Delete(ctx context.Context) error {
	_, err := portal.db.Conn(ctx).ExecContext(ctx, "DELETE FROM portal WHERE id=$1 AND receiver=$2", portal.ID, portal.Receiver)
	return err
}
