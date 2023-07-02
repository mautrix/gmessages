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
	return getAll[*Portal](pq, ctx, "SELECT id, receiver, self_user, other_user, mxid, name, name_set, avatar_id, avatar_mxc, avatar_set, encrypted, in_space FROM portal")
}

func (pq *PortalQuery) GetAllForUser(ctx context.Context, receiver int) ([]*Portal, error) {
	return getAll[*Portal](pq, ctx, "SELECT id, receiver, self_user, other_user, mxid, name, name_set, avatar_id, avatar_mxc, avatar_set, encrypted, in_space FROM portal WHERE receiver=$1", receiver)
}

func (pq *PortalQuery) GetByKey(ctx context.Context, key Key) (*Portal, error) {
	return get[*Portal](pq, ctx, "SELECT id, receiver, self_user, other_user, mxid, name, name_set, avatar_id, avatar_mxc, avatar_set, encrypted, in_space FROM portal WHERE id=$1 AND receiver=$2", key.ID, key.Receiver)
}

func (pq *PortalQuery) GetByMXID(ctx context.Context, mxid id.RoomID) (*Portal, error) {
	return get[*Portal](pq, ctx, "SELECT id, receiver, self_user, other_user, mxid, name, name_set, avatar_id, avatar_mxc, avatar_set, encrypted, in_space FROM portal WHERE mxid=$1", mxid)
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
	SelfUserID  string
	OtherUserID string
	MXID        id.RoomID

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
	err := row.Scan(&portal.ID, &portal.Receiver, &selfUserID, &otherUserID, &mxid, &portal.Name, &portal.NameSet, &portal.AvatarID, &portal.AvatarMXC, &portal.AvatarSet, &portal.Encrypted, &portal.InSpace)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	portal.MXID = id.RoomID(mxid.String)
	portal.SelfUserID = selfUserID.String
	portal.OtherUserID = otherUserID.String
	return portal, nil
}

func (portal *Portal) sqlVariables() []any {
	var mxid, selfUserID, otherUserID *string
	if portal.MXID != "" {
		mxid = (*string)(&portal.MXID)
	}
	if portal.SelfUserID != "" {
		selfUserID = &portal.SelfUserID
	}
	if portal.OtherUserID != "" {
		otherUserID = &portal.OtherUserID
	}
	return []any{portal.ID, portal.Receiver, selfUserID, otherUserID, mxid, portal.Name, portal.NameSet, portal.AvatarID, portal.AvatarMXC, portal.AvatarSet, portal.Encrypted, portal.InSpace}
}

func (portal *Portal) Insert(ctx context.Context) error {
	_, err := portal.db.Conn(ctx).ExecContext(ctx, `
		INSERT INTO portal (id, receiver, self_user, other_user, mxid, name, name_set, avatar_id, avatar_mxc, avatar_set, encrypted, in_space)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`, portal.sqlVariables()...)
	return err
}

func (portal *Portal) Update(ctx context.Context) error {
	_, err := portal.db.Conn(ctx).ExecContext(ctx, `
		UPDATE portal
		SET self_user=$3, other_user=$4, mxid=$5, name=$6, name_set=$7, avatar_id=$8, avatar_mxc=$9, avatar_set=$10, encrypted=$11, in_space=$12
		WHERE id=$1 AND receiver=$2
	`, portal.sqlVariables()...)
	return err
}

func (portal *Portal) Delete(ctx context.Context) error {
	_, err := portal.db.Conn(ctx).ExecContext(ctx, "DELETE FROM portal WHERE id=$1 AND receiver=$2", portal.ID, portal.Receiver)
	return err
}
