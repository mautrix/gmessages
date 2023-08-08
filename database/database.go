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

	_ "github.com/mattn/go-sqlite3"
	"go.mau.fi/util/dbutil"

	"go.mau.fi/mautrix-gmessages/database/upgrades"
)

type Database struct {
	*dbutil.Database

	User     *UserQuery
	Portal   *PortalQuery
	Puppet   *PuppetQuery
	Message  *MessageQuery
	Reaction *ReactionQuery
}

func New(baseDB *dbutil.Database) *Database {
	db := &Database{Database: baseDB}
	db.UpgradeTable = upgrades.Table
	db.User = &UserQuery{db: db}
	db.Portal = &PortalQuery{db: db}
	db.Puppet = &PuppetQuery{db: db}
	db.Message = &MessageQuery{db: db}
	db.Reaction = &ReactionQuery{db: db}
	return db
}

type dataStruct[T any] interface {
	Scan(row dbutil.Scannable) (T, error)
}

type queryStruct[T dataStruct[T]] interface {
	New() T
	getDB() *Database
}

func get[T dataStruct[T]](qs queryStruct[T], ctx context.Context, query string, args ...any) (T, error) {
	return qs.New().Scan(qs.getDB().Conn(ctx).QueryRowContext(ctx, query, args...))
}

func getAll[T dataStruct[T]](qs queryStruct[T], ctx context.Context, query string, args ...any) ([]T, error) {
	rows, err := qs.getDB().Conn(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	items := make([]T, 0)
	defer func() {
		_ = rows.Close()
	}()
	for rows.Next() {
		item, err := qs.New().Scan(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
