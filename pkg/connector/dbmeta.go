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

package connector

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"

	"go.mau.fi/util/jsontime"
	"golang.org/x/exp/maps"
	"maunium.net/go/mautrix/bridgev2/database"

	"go.mau.fi/mautrix-gmessages/pkg/libgm"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

func (gc *GMConnector) GetDBMetaTypes() database.MetaTypes {
	return database.MetaTypes{
		Portal: func() any {
			return &PortalMetadata{}
		},
		Ghost: func() any {
			return &GhostMetadata{}
		},
		Message: func() any {
			return &MessageMetadata{}
		},
		Reaction: nil,
		UserLogin: func() any {
			return &UserLoginMetadata{}
		},
	}
}

type PortalMetadata struct {
	Type     gmproto.ConversationType     `json:"type"`
	SendMode gmproto.ConversationSendMode `json:"send_mode"`
	ForceRCS bool                         `json:"force_rcs"`

	OutgoingID string `json:"outgoing_id"`
}

type GhostMetadata struct {
	Phone          string             `json:"phone"`
	ContactID      string             `json:"contact_id"`
	AvatarUpdateTS jsontime.UnixMilli `json:"avatar_update_ts"`
}

type MessageMetadata struct {
	Type gmproto.MessageStatusType `json:"type,omitempty"`

	GlobalMediaStatus string `json:"media_status,omitempty"`
	GlobalPartCount   int    `json:"part_count,omitempty"`

	TextHash     string `json:"text_hash,omitempty"`
	MediaPartID  string `json:"media_part_id,omitempty"`
	MediaID      string `json:"media_id,omitempty"`
	MediaPending bool   `json:"media_pending,omitempty"`

	MSSSent         bool `json:"mss_sent,omitempty"`
	MSSFailSent     bool `json:"mss_fail_sent,omitempty"`
	MSSDeliverySent bool `json:"mss_delivery_sent,omitempty"`
	ReadReceiptSent bool `json:"read_receipt_sent,omitempty"`
}

type UserLoginMetadata struct {
	lock               sync.RWMutex
	Session            *libgm.AuthData
	selfParticipantIDs []string
	simMetadata        map[string]*gmproto.SIMCard
	Settings           UserSettings
	IDPrefix           string
}

type UserSettings struct {
	SettingsReceived    bool `json:"settings_received"`
	RCSEnabled          bool `json:"rcs_enabled"`
	ReadReceipts        bool `json:"read_receipts"`
	TypingNotifications bool `json:"typing_notifications"`
	IsDefaultSMSApp     bool `json:"is_default_sms_app"`
}

type bridgeStateSIMMeta struct {
	CarrierName   string `json:"carrier_name"`
	ColorHex      string `json:"color_hex"`
	ParticipantID string `json:"participant_id"`
	RCSEnabled    bool   `json:"rcs_enabled"`
	PhoneNumber   string `json:"phone_number"`
}

type serializableUserLoginMetadata struct {
	Session            *libgm.AuthData             `json:"session"`
	SelfParticipantIDs []string                    `json:"self_participant_ids"`
	SimMetadata        map[string]*gmproto.SIMCard `json:"sim_metadata"`
	Settings           UserSettings                `json:"settings"`
	IDPrefix           string                      `json:"id_prefix"`
}

func (ulm *UserLoginMetadata) CopyFrom(other any) {
	otherULM, ok := other.(*UserLoginMetadata)
	if !ok || otherULM == nil {
		panic(fmt.Errorf("invalid type %T provided to UserLoginMetadata.CopyFrom", other))
	}
	ulm.Session = otherULM.Session
}

func (ulm *UserLoginMetadata) MarshalJSON() ([]byte, error) {
	ulm.lock.RLock()
	defer ulm.lock.RUnlock()
	return json.Marshal(serializableUserLoginMetadata{
		Session:            ulm.Session,
		SelfParticipantIDs: ulm.selfParticipantIDs,
		SimMetadata:        ulm.simMetadata,
		Settings:           ulm.Settings,
		IDPrefix:           ulm.IDPrefix,
	})
}

func (ulm *UserLoginMetadata) UnmarshalJSON(data []byte) error {
	var sulm serializableUserLoginMetadata
	err := json.Unmarshal(data, &sulm)
	if err != nil {
		return err
	}
	ulm.lock.Lock()
	defer ulm.lock.Unlock()
	ulm.Session = sulm.Session
	ulm.selfParticipantIDs = sulm.SelfParticipantIDs
	ulm.simMetadata = sulm.SimMetadata
	ulm.Settings = sulm.Settings
	ulm.IDPrefix = sulm.IDPrefix
	return nil
}

func (ulm *UserLoginMetadata) AddSelfParticipantID(id string) bool {
	if id == "" {
		return false
	}
	ulm.lock.Lock()
	defer ulm.lock.Unlock()
	if !slices.Contains(ulm.selfParticipantIDs, id) {
		ulm.selfParticipantIDs = append(ulm.selfParticipantIDs, id)
		return true
	}
	return false
}

func (ulm *UserLoginMetadata) IsSelfParticipantID(id string) bool {
	ulm.lock.RLock()
	defer ulm.lock.RUnlock()
	return slices.Contains(ulm.selfParticipantIDs, id)
}

func (ulm *UserLoginMetadata) SIMCount() int {
	ulm.lock.RLock()
	defer ulm.lock.RUnlock()
	return len(ulm.simMetadata)
}

func (ulm *UserLoginMetadata) GetSIMsForBridgeState() []bridgeStateSIMMeta {
	ulm.lock.RLock()
	defer ulm.lock.RUnlock()
	data := make([]bridgeStateSIMMeta, 0, len(ulm.simMetadata))
	for _, sim := range ulm.simMetadata {
		data = append(data, bridgeStateSIMMeta{
			CarrierName:   sim.GetSIMData().GetCarrierName(),
			ColorHex:      sim.GetSIMData().GetColorHex(),
			ParticipantID: sim.GetSIMParticipant().GetID(),
			RCSEnabled:    sim.GetRCSChats().GetEnabled(),
			PhoneNumber:   sim.GetSIMData().GetFormattedPhoneNumber(),
		})
	}
	slices.SortFunc(data, func(a, b bridgeStateSIMMeta) int {
		return strings.Compare(a.ParticipantID, b.ParticipantID)
	})
	return data
}

func (ulm *UserLoginMetadata) GetSIMs() []*gmproto.SIMCard {
	ulm.lock.RLock()
	defer ulm.lock.RUnlock()
	return maps.Values(ulm.simMetadata)
}

func (ulm *UserLoginMetadata) GetSIM(participantID string) *gmproto.SIMCard {
	ulm.lock.Lock()
	defer ulm.lock.Unlock()
	return ulm.simMetadata[participantID]
}

func (ulm *UserLoginMetadata) SetSIMs(sims []*gmproto.SIMCard) bool {
	ulm.lock.Lock()
	defer ulm.lock.Unlock()
	newMap := make(map[string]*gmproto.SIMCard)
	participantIDsChanged := false
	for _, sim := range sims {
		participantID := sim.GetSIMParticipant().GetID()
		newMap[sim.GetSIMParticipant().GetID()] = sim
		if !slices.Contains(ulm.selfParticipantIDs, participantID) {
			ulm.selfParticipantIDs = append(ulm.selfParticipantIDs, participantID)
			participantIDsChanged = true
		}
	}
	oldMap := ulm.simMetadata
	ulm.simMetadata = newMap
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

func simsAreEqualish(a, b *gmproto.SIMCard) bool {
	return a.GetRCSChats().GetEnabled() == b.GetRCSChats().GetEnabled() &&
		a.GetSIMData().GetCarrierName() == b.GetSIMData().GetCarrierName() &&
		a.GetSIMData().GetSIMPayload().GetSIMNumber() == b.GetSIMData().GetSIMPayload().GetSIMNumber() &&
		a.GetSIMData().GetSIMPayload().GetTwo() == b.GetSIMData().GetSIMPayload().GetTwo()
}
