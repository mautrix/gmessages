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

	"maunium.net/go/mautrix/id"
	"maunium.net/go/mautrix/util/dbutil"
)

type PuppetQuery struct {
	db *Database
}

func (pq *PuppetQuery) New() *Puppet {
	return &Puppet{
		db: pq.db,
	}
}

func (pq *PuppetQuery) getDB() *Database {
	return pq.db
}

func (pq *PuppetQuery) GetAll(ctx context.Context) ([]*Puppet, error) {
	return getAll[*Puppet](pq, ctx, "SELECT id, receiver, phone, name, name_set, avatar_id, avatar_mxc, avatar_set, contact_info_set FROM puppet")
}

func (pq *PuppetQuery) Get(ctx context.Context, key Key) (*Puppet, error) {
	return get[*Puppet](pq, ctx, "SELECT id, receiver, phone, name, name_set, avatar_id, avatar_mxc, avatar_set, contact_info_set FROM puppet WHERE phone=$1 AND receiver=$2", key.ID, key.Receiver)
}

type Puppet struct {
	db *Database

	Key
	Phone          string
	Name           string
	NameSet        bool
	AvatarID       string
	AvatarMXC      id.ContentURI
	AvatarSet      bool
	ContactInfoSet bool
}

func (puppet *Puppet) Scan(row dbutil.Scannable) (*Puppet, error) {
	err := row.Scan(&puppet.ID, &puppet.Receiver, &puppet.Phone, &puppet.Name, &puppet.NameSet, &puppet.AvatarID, &puppet.AvatarMXC, &puppet.AvatarSet, &puppet.ContactInfoSet)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return puppet, nil
}

func (puppet *Puppet) sqlVariables() []any {
	return []any{puppet.ID, puppet.Receiver, puppet.Phone, puppet.Name, puppet.NameSet, puppet.AvatarID, puppet.AvatarMXC, puppet.AvatarSet, puppet.ContactInfoSet}
}

func (puppet *Puppet) Insert(ctx context.Context) error {
	_, err := puppet.db.Conn(ctx).ExecContext(ctx, `
		INSERT INTO puppet (id, receiver, phone, name, name_set, avatar_id, avatar_mxc, avatar_set, contact_info_set)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, puppet.sqlVariables()...)
	return err
}

func (puppet *Puppet) Update(ctx context.Context) error {
	_, err := puppet.db.Conn(ctx).ExecContext(ctx, `
		UPDATE puppet
		SET phone=$3, name=$4, name_set=$5, avatar_id=$6, avatar_mxc=$7, avatar_set=$8, contact_info_set=$9
		WHERE id=$1 AND receiver=$2
	`, puppet.sqlVariables()...)
	return err
}
