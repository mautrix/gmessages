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
	"sync"

	"go.mau.fi/util/dbutil"
	"golang.org/x/exp/slices"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-gmessages/libgm"
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
	return getAll[*User](uq, ctx, `SELECT rowid, mxid, phone_id, session, self_participant_ids, management_room, space_room, access_token FROM "user" WHERE session IS NOT NULL`)
}

func (uq *UserQuery) GetAllWithDoublePuppet(ctx context.Context) ([]*User, error) {
	return getAll[*User](uq, ctx, `SELECT rowid, mxid, phone_id, session, self_participant_ids, management_room, space_room, access_token FROM "user" WHERE access_token<>''`)
}

func (uq *UserQuery) GetByRowID(ctx context.Context, rowID int) (*User, error) {
	return get[*User](uq, ctx, `SELECT rowid, mxid, phone_id, session, self_participant_ids, management_room, space_room, access_token FROM "user" WHERE rowid=$1`, rowID)
}

func (uq *UserQuery) GetByMXID(ctx context.Context, userID id.UserID) (*User, error) {
	return get[*User](uq, ctx, `SELECT rowid, mxid, phone_id, session, self_participant_ids, management_room, space_room, access_token FROM "user" WHERE mxid=$1`, userID)
}

func (uq *UserQuery) GetByPhone(ctx context.Context, phone string) (*User, error) {
	return get[*User](uq, ctx, `SELECT rowid, mxid, phone_id, session, self_participant_ids, management_room, space_room, access_token FROM "user" WHERE phone_id=$1`, phone)
}

type User struct {
	db *Database

	RowID   int
	MXID    id.UserID
	PhoneID string
	Session *libgm.AuthData

	ManagementRoom id.RoomID
	SpaceRoom      id.RoomID

	SelfParticipantIDs     []string
	selfParticipantIDsLock sync.RWMutex

	AccessToken string
}

func (user *User) Scan(row dbutil.Scannable) (*User, error) {
	var phoneID, session, managementRoom, spaceRoom, accessToken sql.NullString
	var selfParticipantIDs string
	err := row.Scan(&user.RowID, &user.MXID, &phoneID, &session, &selfParticipantIDs, &managementRoom, &spaceRoom, &accessToken)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	if session.String != "" {
		var sess libgm.AuthData
		err = json.Unmarshal([]byte(session.String), &sess)
		if err != nil {
			return nil, fmt.Errorf("failed to parse session: %w", err)
		}
		user.Session = &sess
	}
	user.selfParticipantIDsLock.Lock()
	err = json.Unmarshal([]byte(selfParticipantIDs), &user.SelfParticipantIDs)
	user.selfParticipantIDsLock.Unlock()
	if err != nil {
		return nil, fmt.Errorf("failed to parse self participant IDs: %w", err)
	}
	user.PhoneID = phoneID.String
	user.AccessToken = accessToken.String
	user.ManagementRoom = id.RoomID(managementRoom.String)
	user.SpaceRoom = id.RoomID(spaceRoom.String)
	return user, nil
}

func (user *User) sqlVariables() []any {
	var phoneID, session, managementRoom, spaceRoom, accessToken *string
	if user.PhoneID != "" {
		phoneID = &user.PhoneID
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
	user.selfParticipantIDsLock.RLock()
	selfParticipantIDs, _ := json.Marshal(user.SelfParticipantIDs)
	user.selfParticipantIDsLock.RUnlock()
	return []any{user.MXID, phoneID, session, string(selfParticipantIDs), managementRoom, spaceRoom, accessToken}
}

func (user *User) IsSelfParticipantID(id string) bool {
	user.selfParticipantIDsLock.RLock()
	defer user.selfParticipantIDsLock.RUnlock()
	return slices.Contains(user.SelfParticipantIDs, id)
}

func (user *User) AddSelfParticipantID(ctx context.Context, id string) error {
	user.selfParticipantIDsLock.Lock()
	defer user.selfParticipantIDsLock.Unlock()
	if !slices.Contains(user.SelfParticipantIDs, id) {
		user.SelfParticipantIDs = append(user.SelfParticipantIDs, id)
		selfParticipantIDs, _ := json.Marshal(user.SelfParticipantIDs)
		_, err := user.db.Conn(ctx).ExecContext(ctx, `UPDATE "user" SET self_participant_ids=$2 WHERE mxid=$1`, user.MXID, selfParticipantIDs)
		return err
	}
	return nil
}

func (user *User) Insert(ctx context.Context) error {
	err := user.db.Conn(ctx).
		QueryRowContext(ctx, `INSERT INTO "user" (mxid, phone_id, session, self_participant_ids, management_room, space_room, access_token) VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING rowid`, user.sqlVariables()...).
		Scan(&user.RowID)
	return err
}

func (user *User) Update(ctx context.Context) error {
	_, err := user.db.Conn(ctx).ExecContext(ctx, `UPDATE "user" SET phone_id=$2, session=$3, self_participant_ids=$4, management_room=$5, space_room=$6, access_token=$7 WHERE mxid=$1`, user.sqlVariables()...)
	return err
}
