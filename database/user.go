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
	"encoding/json"
	"errors"
	"fmt"

	"maunium.net/go/mautrix/id"
	"maunium.net/go/mautrix/util/dbutil"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
)

type UserQuery struct {
	db *Database
}

func (uq *UserQuery) New() *User {
	return &User{
		db: uq.db,
	}
}

func (uq *UserQuery) getDB() *Database {
	return uq.db
}

func (uq *UserQuery) GetAllWithSession(ctx context.Context) ([]*User, error) {
	return getAll[*User](uq, ctx, `SELECT rowid, mxid, phone, session, management_room, space_room, access_token FROM "user" WHERE phone<>'' AND session IS NOT NULL`)
}

func (uq *UserQuery) GetAllWithDoublePuppet(ctx context.Context) ([]*User, error) {
	return getAll[*User](uq, ctx, `SELECT rowid, mxid, phone, session, management_room, space_room, access_token FROM "user" WHERE access_token<>''`)
}

func (uq *UserQuery) GetByRowID(ctx context.Context, rowID int) (*User, error) {
	return get[*User](uq, ctx, `SELECT rowid, mxid, phone, session, management_room, space_room, access_token FROM "user" WHERE rowid=$1`, rowID)
}

func (uq *UserQuery) GetByMXID(ctx context.Context, userID id.UserID) (*User, error) {
	return get[*User](uq, ctx, `SELECT rowid, mxid, phone, session, management_room, space_room, access_token FROM "user" WHERE mxid=$1`, userID)
}

func (uq *UserQuery) GetByPhone(ctx context.Context, phone string) (*User, error) {
	return get[*User](uq, ctx, `SELECT rowid, mxid, phone, session, management_room, space_room, access_token FROM "user" WHERE phone=$1`, phone)
}

type Session struct {
	WebAuthKey []byte `json:"web_auth_key"`
	AESKey     []byte `json:"aes_key"`
	HMACKey    []byte `json:"hmac_key"`

	PhoneInfo   *binary.Device `json:"phone_info"`
	BrowserInfo *binary.Device `json:"browser_info"`
}

type User struct {
	db *Database

	RowID   int
	MXID    id.UserID
	Phone   string
	Session *Session

	ManagementRoom id.RoomID
	SpaceRoom      id.RoomID

	AccessToken string
}

func (user *User) Scan(row dbutil.Scannable) (*User, error) {
	var phone, session, managementRoom, spaceRoom, accessToken sql.NullString
	err := row.Scan(&user.RowID, &user.MXID, &phone, &session, &managementRoom, &spaceRoom, &accessToken)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	if session.String != "" {
		var sess Session
		err = json.Unmarshal([]byte(session.String), &sess)
		if err != nil {
			return nil, fmt.Errorf("failed to parse session: %w", err)
		}
		user.Session = &sess
	}
	user.Phone = phone.String
	user.AccessToken = accessToken.String
	user.ManagementRoom = id.RoomID(managementRoom.String)
	user.SpaceRoom = id.RoomID(spaceRoom.String)
	return user, nil
}

func (user *User) sqlVariables() []any {
	var phone, session, managementRoom, spaceRoom, accessToken *string
	if user.Phone != "" {
		phone = &user.Phone
	}
	if user.Session != nil {
		data, _ := json.Marshal(user.Session)
		strData := string(data)
		session = &strData
	}
	if user.ManagementRoom != "" {
		managementRoom = (*string)(&user.ManagementRoom)
	}
	if user.SpaceRoom != "" {
		spaceRoom = (*string)(&user.SpaceRoom)
	}
	if user.AccessToken != "" {
		accessToken = &user.AccessToken
	}
	return []any{user.MXID, phone, session, managementRoom, spaceRoom, accessToken}
}

func (user *User) Insert(ctx context.Context) error {
	err := user.db.Conn(ctx).
		QueryRowContext(ctx, `INSERT INTO "user" (mxid, phone, session, management_room, space_room, access_token) VALUES ($1, $2, $3, $4, $5, $6) RETURNING rowid`, user.sqlVariables()...).
		Scan(&user.RowID)
	return err
}

func (user *User) Update(ctx context.Context) error {
	_, err := user.db.Conn(ctx).ExecContext(ctx, `UPDATE "user" SET phone=$2, session=$3, management_room=$4, space_room=$5, access_token=$6 WHERE mxid=$1`, user.sqlVariables()...)
	return err
}
