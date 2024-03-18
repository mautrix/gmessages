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
	"time"

	"go.mau.fi/util/dbutil"
	"maunium.net/go/mautrix/id"
)

type PuppetQuery struct {
	*dbutil.QueryHelper[*Puppet]
}

func newPuppet(qh *dbutil.QueryHelper[*Puppet]) *Puppet {
	return &Puppet{qh: qh}
}

const (
	getPuppetQuery    = "SELECT id, receiver, phone, contact_id, name, name_set, avatar_hash, avatar_mxc, avatar_set, avatar_update_ts, contact_info_set FROM puppet WHERE id=$1 AND receiver=$2"
	insertPuppetQuery = `
		INSERT INTO puppet (id, receiver, phone, contact_id, name, name_set, avatar_hash, avatar_mxc, avatar_set, avatar_update_ts, contact_info_set)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`
	updatePuppetQuery = `
		UPDATE puppet
		SET phone=$3, contact_id=$4, name=$5, name_set=$6, avatar_hash=$7, avatar_mxc=$8, avatar_set=$9, avatar_update_ts=$10, contact_info_set=$11
		WHERE id=$1 AND receiver=$2
	`
	resetPuppetMetadataQuery = `
		UPDATE puppet SET phone=NULL, contact_info_set=false, avatar_update_ts=0, contact_id='' WHERE receiver=$1
	`
)

func (pq *PuppetQuery) Get(ctx context.Context, key Key) (*Puppet, error) {
	return pq.QueryOne(ctx, getPuppetQuery, key.ID, key.Receiver)
}

// Reset clears the phone number of all puppets, so that puppets can be upserted safely on relogin
// even if the internal IDs shuffled around. This does *not* delete the puppets, because otherwise
// avatars wouldn't get reset appropriately.
func (pq *PuppetQuery) Reset(ctx context.Context, userID int) error {
	return pq.Exec(ctx, resetPuppetMetadataQuery, userID)
}

type Puppet struct {
	qh *dbutil.QueryHelper[*Puppet]

	Key
	Phone          string
	ContactID      string
	Name           string
	NameSet        bool
	AvatarHash     [32]byte
	AvatarMXC      id.ContentURI
	AvatarSet      bool
	AvatarUpdateTS time.Time
	ContactInfoSet bool
}

func (puppet *Puppet) Scan(row dbutil.Scannable) (*Puppet, error) {
	var avatarHash []byte
	var avatarUpdateTS int64
	var phone sql.NullString
	err := row.Scan(
		&puppet.ID, &puppet.Receiver, &phone, &puppet.ContactID, &puppet.Name, &puppet.NameSet,
		&avatarHash, &puppet.AvatarMXC, &puppet.AvatarSet, &avatarUpdateTS, &puppet.ContactInfoSet,
	)
	if err != nil {
		return nil, err
	}
	if len(avatarHash) == 32 {
		puppet.AvatarHash = *(*[32]byte)(avatarHash)
	}
	puppet.Phone = phone.String
	if avatarUpdateTS > 0 {
		puppet.AvatarUpdateTS = time.UnixMilli(avatarUpdateTS)
	}
	return puppet, nil
}

func (puppet *Puppet) sqlVariables() []any {
	var avatarUpdateTS int64
	if !puppet.AvatarUpdateTS.IsZero() {
		avatarUpdateTS = puppet.AvatarUpdateTS.UnixMilli()
	}
	return []any{
		puppet.ID, puppet.Receiver, dbutil.StrPtr(puppet.Phone), puppet.ContactID, puppet.Name, puppet.NameSet,
		puppet.AvatarHash[:], &puppet.AvatarMXC, puppet.AvatarSet, avatarUpdateTS, puppet.ContactInfoSet,
	}
}

func (puppet *Puppet) Insert(ctx context.Context) error {
	return puppet.qh.Exec(ctx, insertPuppetQuery, puppet.sqlVariables()...)
}

func (puppet *Puppet) Update(ctx context.Context) error {
	return puppet.qh.Exec(ctx, updatePuppetQuery, puppet.sqlVariables()...)
}
