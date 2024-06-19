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
	"encoding/json"
	"fmt"
	"sync"

	"go.mau.fi/util/dbutil"
	"golang.org/x/exp/slices"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-gmessages/libgm"
	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
)

type UserQuery struct {
	*dbutil.QueryHelper[*User]
}

func newUser(qh *dbutil.QueryHelper[*User]) *User {
	return &User{qh: qh}
}

const (
	getUserBaseQuery                 = `SELECT rowid, mxid, phone_id, session, self_participant_ids, sim_metadata, settings, management_room, space_room, access_token, disable_notify_battery, disable_notify_verbose FROM "user"`
	getAllUsersWithSessionQuery      = getUserBaseQuery + " WHERE session IS NOT NULL"
	getAllUsersWithDoublePuppetQuery = getUserBaseQuery + " WHERE access_token<>''"
	getUserByRowIDQuery              = getUserBaseQuery + " WHERE rowid=$1"
	getUserByMXIDQuery               = getUserBaseQuery + " WHERE mxid=$1"

	insertUserQuery = `
		INSERT INTO "user" (mxid, phone_id, session, self_participant_ids, sim_metadata, settings, management_room, space_room, access_token)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING rowid
	`
	updateUserQuery = `
		UPDATE "user"
		SET phone_id=$2, session=$3, self_participant_ids=$4, sim_metadata=$5, settings=$6,
		    management_room=$7, space_room=$8, access_token=$9,
		    disable_notify_battery=$10, disable_notify_verbose=$11
		WHERE mxid=$1
	`
	updateuserParticipantIDsQuery = `UPDATE "user" SET self_participant_ids=$2 WHERE mxid=$1`
)

func (uq *UserQuery) GetAllWithSession(ctx context.Context) ([]*User, error) {
	return uq.QueryMany(ctx, getAllUsersWithSessionQuery)
}

func (uq *UserQuery) GetAllWithDoublePuppet(ctx context.Context) ([]*User, error) {
	return uq.QueryMany(ctx, getAllUsersWithDoublePuppetQuery)
}

func (uq *UserQuery) GetByRowID(ctx context.Context, rowID int) (*User, error) {
	return uq.QueryOne(ctx, getUserByRowIDQuery, rowID)
}

func (uq *UserQuery) GetByMXID(ctx context.Context, userID id.UserID) (*User, error) {
	return uq.QueryOne(ctx, getUserByMXIDQuery, userID)
}

type Settings struct {
	SettingsReceived    bool `json:"settings_received"`
	RCSEnabled          bool `json:"rcs_enabled"`
	ReadReceipts        bool `json:"read_receipts"`
	TypingNotifications bool `json:"typing_notifications"`
	IsDefaultSMSApp     bool `json:"is_default_sms_app"`
}

type User struct {
	qh *dbutil.QueryHelper[*User]

	RowID   int
	MXID    id.UserID
	PhoneID string
	Session *libgm.AuthData

	ManagementRoom id.RoomID
	SpaceRoom      id.RoomID

	SelfParticipantIDs     []string
	selfParticipantIDsLock sync.RWMutex

	simMetadata     map[string]*gmproto.SIMCard
	simMetadataLock sync.RWMutex

	Settings Settings

	AccessToken string

	DisableNotifyBattery bool
	DisableNotifyVerbose bool
}

func (user *User) Scan(row dbutil.Scannable) (*User, error) {
	var phoneID, session, managementRoom, spaceRoom, accessToken sql.NullString
	var selfParticipantIDs, simMetadata, settings string
	err := row.Scan(
		&user.RowID, &user.MXID, &phoneID, &session, &selfParticipantIDs, &simMetadata,
		&settings, &managementRoom, &spaceRoom, &accessToken,
		&user.DisableNotifyBattery, &user.DisableNotifyVerbose,
	)
	if err != nil {
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
	user.simMetadataLock.Lock()
	err = json.Unmarshal([]byte(simMetadata), &user.simMetadata)
	user.simMetadataLock.Unlock()
	if err != nil {
		return nil, fmt.Errorf("failed to parse SIM metadata: %w", err)
	}
	err = json.Unmarshal([]byte(settings), &user.Settings)
	if err != nil {
		return nil, fmt.Errorf("failed to parse settings: %w", err)
	}
	user.PhoneID = phoneID.String
	user.AccessToken = accessToken.String
	user.ManagementRoom = id.RoomID(managementRoom.String)
	user.SpaceRoom = id.RoomID(spaceRoom.String)
	return user, nil
}

func (user *User) sqlVariables() []any {
	var session *string
	if user.Session != nil {
		data, _ := json.Marshal(user.Session)
		strData := string(data)
		session = &strData
	}
	user.selfParticipantIDsLock.RLock()
	selfParticipantIDs, _ := json.Marshal(user.SelfParticipantIDs)
	user.selfParticipantIDsLock.RUnlock()
	user.simMetadataLock.RLock()
	simMetadata, err := json.Marshal(user.simMetadata)
	if err != nil {
		panic(err)
	}
	user.simMetadataLock.RUnlock()
	settings, err := json.Marshal(&user.Settings)
	if err != nil {
		panic(err)
	}
	return []any{
		user.MXID, dbutil.StrPtr(user.PhoneID), session, string(selfParticipantIDs), string(simMetadata),
		string(settings), dbutil.StrPtr(user.ManagementRoom), dbutil.StrPtr(user.SpaceRoom), dbutil.StrPtr(user.AccessToken),
		user.DisableNotifyBattery, user.DisableNotifyVerbose,
	}
}

func (user *User) IsSelfParticipantID(id string) bool {
	user.selfParticipantIDsLock.RLock()
	defer user.selfParticipantIDsLock.RUnlock()
	return slices.Contains(user.SelfParticipantIDs, id)
}

type bridgeStateSIMMeta struct {
	CarrierName   string `json:"carrier_name"`
	ColorHex      string `json:"color_hex"`
	ParticipantID string `json:"participant_id"`
	RCSEnabled    bool   `json:"rcs_enabled"`
}

func (user *User) SIMCount() int {
	user.simMetadataLock.RLock()
	defer user.simMetadataLock.RUnlock()
	return len(user.simMetadata)
}

func (user *User) GetSIMsForBridgeState() []bridgeStateSIMMeta {
	user.simMetadataLock.RLock()
	data := make([]bridgeStateSIMMeta, 0, len(user.simMetadata))
	for _, sim := range user.simMetadata {
		data = append(data, bridgeStateSIMMeta{
			CarrierName:   sim.GetSIMData().GetCarrierName(),
			ColorHex:      sim.GetSIMData().GetHexHash(),
			ParticipantID: sim.GetSIMParticipant().GetID(),
			RCSEnabled:    sim.GetRCSChats().GetEnabled(),
		})
	}
	user.simMetadataLock.RUnlock()
	return data
}

func (user *User) GetSIM(participantID string) *gmproto.SIMCard {
	user.simMetadataLock.Lock()
	defer user.simMetadataLock.Unlock()
	return user.simMetadata[participantID]
}

func simsAreEqualish(a, b *gmproto.SIMCard) bool {
	return a.GetRCSChats().GetEnabled() != b.GetRCSChats().GetEnabled() ||
		a.GetSIMData().GetCarrierName() != b.GetSIMData().GetCarrierName() ||
		a.GetSIMData().GetSIMPayload().GetSIMNumber() != b.GetSIMData().GetSIMPayload().GetSIMNumber() ||
		a.GetSIMData().GetSIMPayload().GetTwo() != b.GetSIMData().GetSIMPayload().GetTwo()
}

func (user *User) SetSIMs(sims []*gmproto.SIMCard) bool {
	user.simMetadataLock.Lock()
	defer user.simMetadataLock.Unlock()
	user.selfParticipantIDsLock.Lock()
	defer user.selfParticipantIDsLock.Unlock()
	newMap := make(map[string]*gmproto.SIMCard)
	participantIDsChanged := false
	for _, sim := range sims {
		participantID := sim.GetSIMParticipant().GetID()
		newMap[sim.GetSIMParticipant().GetID()] = sim
		if !slices.Contains(user.SelfParticipantIDs, participantID) {
			user.SelfParticipantIDs = append(user.SelfParticipantIDs, participantID)
			participantIDsChanged = true
		}
	}
	oldMap := user.simMetadata
	user.simMetadata = newMap
	if participantIDsChanged || len(newMap) != len(oldMap) {
		return true
	}
	for participantID, sim := range newMap {
		existing, ok := oldMap[participantID]
		if !ok || !simsAreEqualish(existing, sim) {
			return true
		}
	}
	return false
}

func (user *User) AddSelfParticipantID(ctx context.Context, id string) error {
	user.selfParticipantIDsLock.Lock()
	defer user.selfParticipantIDsLock.Unlock()
	if !slices.Contains(user.SelfParticipantIDs, id) {
		user.SelfParticipantIDs = append(user.SelfParticipantIDs, id)
		selfParticipantIDs, _ := json.Marshal(user.SelfParticipantIDs)
		return user.qh.Exec(ctx, updateuserParticipantIDsQuery, user.MXID, selfParticipantIDs)
	}
	return nil
}

func (user *User) Insert(ctx context.Context) error {
	err := user.qh.GetDB().
		QueryRow(ctx, insertUserQuery, user.sqlVariables()...).
		Scan(&user.RowID)
	return err
}

func (user *User) Update(ctx context.Context) error {
	return user.qh.Exec(ctx, updateUserQuery, user.sqlVariables()...)
}
