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

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	log "maunium.net/go/maulogger/v2"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/appservice"
	"maunium.net/go/mautrix/bridge"
	"maunium.net/go/mautrix/crypto/attachment"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-gmessages/database"
	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

func (br *GMBridge) GetPortalByMXID(mxid id.RoomID) *Portal {
	br.portalsLock.Lock()
	defer br.portalsLock.Unlock()
	portal, ok := br.portalsByMXID[mxid]
	if !ok {
		dbPortal, err := br.DB.Portal.GetByMXID(context.TODO(), mxid)
		if err != nil {
			br.ZLog.Err(err).Str("mxid", mxid.String()).Msg("Failed to get portal from database")
			return nil
		}
		return br.loadDBPortal(dbPortal, nil)
	}
	return portal
}

func (br *GMBridge) GetIPortal(mxid id.RoomID) bridge.Portal {
	p := br.GetPortalByMXID(mxid)
	if p == nil {
		return nil
	}
	return p
}

func (portal *Portal) IsEncrypted() bool {
	return portal.Encrypted
}

func (portal *Portal) MarkEncrypted() {
	portal.Encrypted = true
	err := portal.Update(context.TODO())
	if err != nil {
		portal.zlog.Err(err).Msg("Failed to save portal to database after marking it as encrypted")
	}
}

func (portal *Portal) ReceiveMatrixEvent(brUser bridge.User, evt *event.Event) {
	user := brUser.(*User)
	if user.RowID == portal.Receiver {
		portal.matrixMessages <- PortalMatrixMessage{user: user, evt: evt, receivedAt: time.Now()}
	}
}

func (br *GMBridge) GetPortalByKey(key database.Key) *Portal {
	br.portalsLock.Lock()
	defer br.portalsLock.Unlock()
	portal, ok := br.portalsByKey[key]
	if !ok {
		dbPortal, err := br.DB.Portal.GetByKey(context.TODO(), key)
		if err != nil {
			br.ZLog.Err(err).Object("portal_key", key).Msg("Failed to get portal from database")
			return nil
		}
		return br.loadDBPortal(dbPortal, &key)
	}
	return portal
}

func (br *GMBridge) GetExistingPortalByKey(key database.Key) *Portal {
	br.portalsLock.Lock()
	defer br.portalsLock.Unlock()
	portal, ok := br.portalsByKey[key]
	if !ok {
		dbPortal, err := br.DB.Portal.GetByKey(context.TODO(), key)
		if err != nil {
			br.ZLog.Err(err).Object("portal_key", key).Msg("Failed to get portal from database")
			return nil
		}
		return br.loadDBPortal(dbPortal, nil)
	}
	return portal
}

func (br *GMBridge) GetAllPortals() []*Portal {
	return br.loadManyPortals(br.DB.Portal.GetAll)
}

func (br *GMBridge) GetAllPortalsForUser(userID int) []*Portal {
	return br.loadManyPortals(func(ctx context.Context) ([]*database.Portal, error) {
		return br.DB.Portal.GetAllForUser(ctx, userID)
	})
}

func (br *GMBridge) GetAllIPortals() (iportals []bridge.Portal) {
	portals := br.GetAllPortals()
	iportals = make([]bridge.Portal, len(portals))
	for i, portal := range portals {
		iportals[i] = portal
	}
	return iportals
}

func (br *GMBridge) loadManyPortals(query func(ctx context.Context) ([]*database.Portal, error)) []*Portal {
	br.portalsLock.Lock()
	defer br.portalsLock.Unlock()
	dbPortals, err := query(context.TODO())
	if err != nil {
		br.ZLog.Err(err).Msg("Failed to load all portals from database")
		return []*Portal{}
	}
	output := make([]*Portal, len(dbPortals))
	for index, dbPortal := range dbPortals {
		if dbPortal == nil {
			continue
		}
		portal, ok := br.portalsByKey[dbPortal.Key]
		if !ok {
			portal = br.loadDBPortal(dbPortal, nil)
		}
		output[index] = portal
	}
	return output
}

func (br *GMBridge) loadDBPortal(dbPortal *database.Portal, key *database.Key) *Portal {
	if dbPortal == nil {
		if key == nil {
			return nil
		}
		dbPortal = br.DB.Portal.New()
		dbPortal.Key = *key
		err := dbPortal.Insert(context.TODO())
		if err != nil {
			br.ZLog.Err(err).Object("portal_key", key).Msg("Failed to insert portal")
			return nil
		}
	}
	portal := br.NewPortal(dbPortal)
	br.portalsByKey[portal.Key] = portal
	if len(portal.MXID) > 0 {
		br.portalsByMXID[portal.MXID] = portal
	}
	return portal
}

func (portal *Portal) GetUsers() []*User {
	return nil
}

func (br *GMBridge) newBlankPortal(key database.Key) *Portal {
	portal := &Portal{
		bridge: br,
		log:    br.Log.Sub(fmt.Sprintf("Portal/%s", key.ID)),
		zlog:   br.ZLog.With().Str("portal_id", key.ID).Int("portal_receiver", key.Receiver).Logger(),

		messages:       make(chan PortalMessage, br.Config.Bridge.PortalMessageBuffer),
		matrixMessages: make(chan PortalMatrixMessage, br.Config.Bridge.PortalMessageBuffer),

		outgoingMessages: make(map[string]id.EventID),
	}
	go portal.handleMessageLoop()
	return portal
}

func (br *GMBridge) NewPortal(dbPortal *database.Portal) *Portal {
	portal := br.newBlankPortal(dbPortal.Key)
	portal.Portal = dbPortal
	return portal
}

const recentlyHandledLength = 100

type PortalMessage struct {
	evt    *binary.Message
	source *User
}

type PortalMatrixMessage struct {
	evt        *event.Event
	user       *User
	receivedAt time.Time
}

type Portal struct {
	*database.Portal

	bridge *GMBridge
	// Deprecated: use zerolog
	log  log.Logger
	zlog zerolog.Logger

	roomCreateLock sync.Mutex
	encryptLock    sync.Mutex
	backfillLock   sync.Mutex
	avatarLock     sync.Mutex

	latestEventBackfillLock sync.Mutex

	recentlyHandled      [recentlyHandledLength]string
	recentlyHandledLock  sync.Mutex
	recentlyHandledIndex uint8

	outgoingMessages     map[string]id.EventID
	outgoingMessagesLock sync.Mutex

	currentlyTyping     []id.UserID
	currentlyTypingLock sync.Mutex

	messages       chan PortalMessage
	matrixMessages chan PortalMatrixMessage
}

var (
	_ bridge.Portal = (*Portal)(nil)
	//_ bridge.ReadReceiptHandlingPortal = (*Portal)(nil)
	//_ bridge.MembershipHandlingPortal  = (*Portal)(nil)
	//_ bridge.MetaHandlingPortal        = (*Portal)(nil)
	//_ bridge.TypingPortal              = (*Portal)(nil)
)

func (portal *Portal) handleMessageLoopItem(msg PortalMessage) {
	if len(portal.MXID) == 0 {
		return
	}
	portal.latestEventBackfillLock.Lock()
	defer portal.latestEventBackfillLock.Unlock()
	switch {
	case msg.evt != nil:
		portal.handleMessage(msg.source, msg.evt)
	//case msg.receipt != nil:
	//	portal.handleReceipt(msg.receipt, msg.source)
	default:
		portal.zlog.Warn().Interface("portal_message", msg).Msg("Unexpected PortalMessage with no message")
	}
}

func (portal *Portal) handleMatrixMessageLoopItem(msg PortalMatrixMessage) {
	portal.latestEventBackfillLock.Lock()
	defer portal.latestEventBackfillLock.Unlock()
	evtTS := time.UnixMilli(msg.evt.Timestamp)
	timings := messageTimings{
		initReceive:  msg.evt.Mautrix.ReceivedAt.Sub(evtTS),
		decrypt:      msg.evt.Mautrix.DecryptionDuration,
		portalQueue:  time.Since(msg.receivedAt),
		totalReceive: time.Since(evtTS),
	}
	switch msg.evt.Type {
	case event.EventMessage, event.EventSticker:
		portal.HandleMatrixMessage(msg.user, msg.evt, timings)
	case event.EventReaction:
		portal.HandleMatrixReaction(msg.user, msg.evt)
	default:
		portal.zlog.Warn().
			Str("event_type", msg.evt.Type.Type).
			Msg("Unsupported event type in portal message channel")
	}
}

func (portal *Portal) handleMessageLoop() {
	for {
		select {
		case msg := <-portal.messages:
			portal.handleMessageLoopItem(msg)
		case msg := <-portal.matrixMessages:
			portal.handleMatrixMessageLoopItem(msg)
		}
	}
}

func (portal *Portal) isOutgoingMessage(evt *binary.Message) id.EventID {
	portal.outgoingMessagesLock.Lock()
	defer portal.outgoingMessagesLock.Unlock()
	evtID, ok := portal.outgoingMessages[evt.TmpID]
	if ok {
		delete(portal.outgoingMessages, evt.TmpID)
		portal.markHandled(evt, map[string]id.EventID{"": evtID}, true)
		return evtID
	}
	return ""
}

func (portal *Portal) handleMessage(source *User, evt *binary.Message) {
	if len(portal.MXID) == 0 {
		portal.zlog.Warn().Msg("handleMessage called even though portal.MXID is empty")
		return
	}
	log := portal.zlog.With().
		Str("message_id", evt.MessageID).
		Str("participant_id", evt.ParticipantID).
		Str("action", "handleMessage").
		Logger()
	if evtID := portal.isOutgoingMessage(evt); evtID != "" {
		log.Debug().Str("event_id", evtID.String()).Msg("Got echo for outgoing message")
		return
	} else if portal.isRecentlyHandled(evt.MessageID) {
		log.Debug().Msg("Not handling recent duplicate message")
		return
	}
	existingMsg, err := portal.bridge.DB.Message.GetByID(context.TODO(), portal.Key, evt.MessageID)
	if err != nil {
		log.Err(err).Msg("Failed to check if message is duplicate")
	} else if existingMsg != nil {
		log.Debug().Msg("Not handling duplicate message")
		return
	}

	var intent *appservice.IntentAPI
	// TODO is there a fromMe flag?
	if evt.GetParticipantID() == portal.SelfUserID {
		intent = source.DoublePuppetIntent
		if intent == nil {
			log.Debug().Msg("Dropping message from self as double puppeting is not enabled")
			return
		}
	} else {
		puppet := source.GetPuppetByID(evt.ParticipantID, "")
		if puppet == nil {
			log.Debug().Msg("Dropping message from unknown participant")
			return
		}
		intent = puppet.IntentFor(portal)
	}

	eventIDs := make(map[string]id.EventID)
	var lastEventID id.EventID
	ts := time.UnixMicro(evt.Timestamp).UnixMilli()
	for _, part := range evt.MessageInfo {
		var content event.MessageEventContent
		switch data := part.GetData().(type) {
		case *binary.MessageInfo_MessageContent:
			content = event.MessageEventContent{
				MsgType: event.MsgText,
				Body:    data.MessageContent.GetContent(),
			}
		case *binary.MessageInfo_MediaContent:
			content = event.MessageEventContent{
				MsgType: event.MsgNotice,
				Body:    fmt.Sprintf("Attachment %s", data.MediaContent.GetMediaName()),
			}
		}
		resp, err := portal.sendMessage(intent, event.EventMessage, &content, nil, ts)
		if err != nil {
			log.Err(err).Msg("Failed to send message")
		} else {
			eventIDs[part.GetActionMessageID()] = resp.EventID
			lastEventID = resp.EventID
		}
	}
	portal.markHandled(evt, eventIDs, true)
	portal.sendDeliveryReceipt(lastEventID)
	log.Debug().Interface("event_ids", eventIDs).Msg("Handled message")
}

func (portal *Portal) isRecentlyHandled(id string) bool {
	start := portal.recentlyHandledIndex
	for i := start; i != start; i = (i - 1) % recentlyHandledLength {
		if portal.recentlyHandled[i] == id {
			return true
		}
	}
	return false
}

func (portal *Portal) markHandled(info *binary.Message, mxids map[string]id.EventID, recent bool) *database.Message {
	msg := portal.bridge.DB.Message.New()
	msg.Chat = portal.Key
	msg.ID = info.MessageID
	for _, evtID := range mxids {
		msg.MXID = evtID
	}
	msg.Timestamp = time.UnixMicro(info.Timestamp)
	msg.Sender = info.ParticipantID
	err := msg.Insert(context.TODO())
	if err != nil {
		portal.zlog.Err(err).Str("message_id", info.MessageID).Msg("Failed to insert message to database")
	}

	if recent {
		portal.recentlyHandledLock.Lock()
		index := portal.recentlyHandledIndex
		portal.recentlyHandledIndex = (portal.recentlyHandledIndex + 1) % recentlyHandledLength
		portal.recentlyHandledLock.Unlock()
		portal.recentlyHandled[index] = info.MessageID
	}
	return msg
}

func (portal *Portal) SyncParticipants(source *User, metadata *binary.Conversation) (userIDs []id.UserID, changed bool) {
	var firstParticipant *binary.Participant
	var manyParticipants bool
	for _, participant := range metadata.Participants {
		if participant.IsMe {
			continue
		} else if participant.ID.Number == "" {
			portal.zlog.Warn().Interface("participant", participant).Msg("No number found in non-self participant entry")
			continue
		}
		if firstParticipant == nil {
			firstParticipant = participant
		} else {
			manyParticipants = true
		}
		portal.zlog.Debug().Interface("participant", participant).Msg("Syncing participant")
		puppet := source.GetPuppetByID(participant.ID.ParticipantID, participant.ID.Number)
		userIDs = append(userIDs, puppet.MXID)
		puppet.Sync(source, participant)
		if portal.MXID != "" {
			err := puppet.IntentFor(portal).EnsureJoined(portal.MXID)
			if err != nil {
				portal.zlog.Err(err).
					Str("user_id", puppet.MXID.String()).
					Msg("Failed to ensure ghost is joined to portal")
			}
		}
	}
	if !metadata.IsGroupChat && !manyParticipants && portal.OtherUserID != firstParticipant.ID.ParticipantID {
		portal.zlog.Info().
			Str("old_other_user_id", portal.OtherUserID).
			Str("new_other_user_id", firstParticipant.ID.ParticipantID).
			Msg("Found other user ID in DM")
		portal.OtherUserID = firstParticipant.ID.ParticipantID
		changed = true
	}
	return userIDs, changed
}

func (portal *Portal) UpdateName(name string, updateInfo bool) bool {
	if portal.Name != name || (!portal.NameSet && len(portal.MXID) > 0 && portal.shouldSetDMRoomMetadata()) {
		portal.zlog.Debug().Str("old_name", portal.Name).Str("new_name", name).Msg("Updating name")
		portal.Name = name
		portal.NameSet = false
		if updateInfo {
			defer func() {
				err := portal.Update(context.TODO())
				if err != nil {
					portal.zlog.Err(err).Msg("Failed to save portal after updating name")
				}
			}()
		}

		if len(portal.MXID) > 0 && !portal.shouldSetDMRoomMetadata() {
			portal.UpdateBridgeInfo()
		} else if len(portal.MXID) > 0 {
			intent := portal.MainIntent()
			_, err := intent.SetRoomName(portal.MXID, name)
			if errors.Is(err, mautrix.MForbidden) && intent != portal.MainIntent() {
				_, err = portal.MainIntent().SetRoomName(portal.MXID, name)
			}
			if err == nil {
				portal.NameSet = true
				if updateInfo {
					portal.UpdateBridgeInfo()
				}
				return true
			} else {
				portal.zlog.Warn().Err(err).Msg("Failed to set room name")
			}
		}
	}
	return false
}

func (portal *Portal) UpdateMetadata(user *User, info *binary.Conversation) []id.UserID {
	participants, update := portal.SyncParticipants(user, info)
	if portal.SelfUserID != info.SelfParticipantID {
		portal.SelfUserID = info.SelfParticipantID
		update = true
	}
	if portal.MXID != "" {
		update = portal.addToPersonalSpace(user, false) || update
	}
	if portal.shouldSetDMRoomMetadata() {
		update = portal.UpdateName(info.Name, false) || update
	}
	// TODO avatar
	if update {
		err := portal.Update(context.TODO())
		if err != nil {
			portal.zlog.Err(err).Msg("Failed to save portal after updating metadata")
		}
		if portal.MXID != "" {
			portal.UpdateBridgeInfo()
		}
	}
	return participants
}

func (portal *Portal) ensureUserInvited(user *User) bool {
	return user.ensureInvited(portal.MainIntent(), portal.MXID, portal.IsPrivateChat())
}

func (portal *Portal) UpdateMatrixRoom(user *User, groupInfo *binary.Conversation) bool {
	if len(portal.MXID) == 0 {
		return false
	}

	portal.ensureUserInvited(user)
	portal.UpdateMetadata(user, groupInfo)
	return true
}

func (portal *Portal) GetBasePowerLevels() *event.PowerLevelsEventContent {
	anyone := 0
	nope := 99
	invite := 50
	return &event.PowerLevelsEventContent{
		UsersDefault:    anyone,
		EventsDefault:   anyone,
		RedactPtr:       &anyone,
		StateDefaultPtr: &nope,
		BanPtr:          &nope,
		InvitePtr:       &invite,
		Users: map[id.UserID]int{
			portal.MainIntent().UserID: 100,
		},
		Events: map[string]int{
			event.StateRoomName.Type:   anyone,
			event.StateRoomAvatar.Type: anyone,
			event.EventReaction.Type:   anyone, // TODO only allow reactions in RCS rooms
		},
	}
}

func (portal *Portal) getBridgeInfoStateKey() string {
	return fmt.Sprintf("fi.mau.gmessages://gmessages/%s", portal.ID)
}

func (portal *Portal) getBridgeInfo() (string, event.BridgeEventContent) {
	return portal.getBridgeInfoStateKey(), event.BridgeEventContent{
		BridgeBot: portal.bridge.Bot.UserID,
		Creator:   portal.MainIntent().UserID,
		Protocol: event.BridgeInfoSection{
			ID:          "gmessages",
			DisplayName: "Google Messages",
			AvatarURL:   portal.bridge.Config.AppService.Bot.ParsedAvatar.CUString(),
			ExternalURL: "https://messages.google.com/",
		},
		Channel: event.BridgeInfoSection{
			ID:          portal.ID,
			DisplayName: portal.Name,
			AvatarURL:   portal.AvatarMXC.CUString(),
		},
	}
}

func (portal *Portal) UpdateBridgeInfo() {
	if len(portal.MXID) == 0 {
		portal.zlog.Debug().Msg("Not updating bridge info: no Matrix room created")
		return
	}
	portal.zlog.Debug().Msg("Updating bridge info...")
	stateKey, content := portal.getBridgeInfo()
	_, err := portal.MainIntent().SendStateEvent(portal.MXID, event.StateBridge, stateKey, content)
	if err != nil {
		portal.zlog.Warn().Err(err).Msg("Failed to update m.bridge")
	}
	// TODO remove this once https://github.com/matrix-org/matrix-doc/pull/2346 is in spec
	_, err = portal.MainIntent().SendStateEvent(portal.MXID, event.StateHalfShotBridge, stateKey, content)
	if err != nil {
		portal.zlog.Warn().Err(err).Msg("Failed to update uk.half-shot.bridge")
	}
}

func (portal *Portal) shouldSetDMRoomMetadata() bool {
	return !portal.IsPrivateChat() ||
		portal.bridge.Config.Bridge.PrivateChatPortalMeta == "always" ||
		(portal.IsEncrypted() && portal.bridge.Config.Bridge.PrivateChatPortalMeta != "never")
}

func (portal *Portal) GetEncryptionEventContent() (evt *event.EncryptionEventContent) {
	evt = &event.EncryptionEventContent{Algorithm: id.AlgorithmMegolmV1}
	if rot := portal.bridge.Config.Bridge.Encryption.Rotation; rot.EnableCustom {
		evt.RotationPeriodMillis = rot.Milliseconds
		evt.RotationPeriodMessages = rot.Messages
	}
	return
}

func (portal *Portal) CreateMatrixRoom(user *User, conv *binary.Conversation) error {
	portal.roomCreateLock.Lock()
	defer portal.roomCreateLock.Unlock()
	if len(portal.MXID) > 0 {
		return nil
	}

	members := portal.UpdateMetadata(user, conv)

	if portal.IsPrivateChat() && portal.GetDMPuppet() == nil {
		portal.zlog.Error().Msg("Didn't find ghost of other user in DM :(")
		return fmt.Errorf("ghost not found")
	}

	intent := portal.MainIntent()
	if err := intent.EnsureRegistered(); err != nil {
		return err
	}

	portal.zlog.Info().Msg("Creating Matrix room")

	bridgeInfoStateKey, bridgeInfo := portal.getBridgeInfo()

	initialState := []*event.Event{{
		Type: event.StatePowerLevels,
		Content: event.Content{
			Parsed: portal.GetBasePowerLevels(),
		},
	}, {
		Type:     event.StateBridge,
		Content:  event.Content{Parsed: bridgeInfo},
		StateKey: &bridgeInfoStateKey,
	}, {
		// TODO remove this once https://github.com/matrix-org/matrix-doc/pull/2346 is in spec
		Type:     event.StateHalfShotBridge,
		Content:  event.Content{Parsed: bridgeInfo},
		StateKey: &bridgeInfoStateKey,
	}}
	var invite []id.UserID
	if portal.bridge.Config.Bridge.Encryption.Default {
		initialState = append(initialState, &event.Event{
			Type: event.StateEncryption,
			Content: event.Content{
				Parsed: portal.GetEncryptionEventContent(),
			},
		})
		portal.Encrypted = true
		if portal.IsPrivateChat() {
			invite = append(invite, portal.bridge.Bot.UserID)
		}
	}
	if !portal.AvatarMXC.IsEmpty() && portal.shouldSetDMRoomMetadata() {
		initialState = append(initialState, &event.Event{
			Type: event.StateRoomAvatar,
			Content: event.Content{
				Parsed: event.RoomAvatarEventContent{URL: portal.AvatarMXC},
			},
		})
		portal.AvatarSet = true
	} else {
		portal.AvatarSet = false
	}

	creationContent := make(map[string]interface{})
	if !portal.bridge.Config.Bridge.FederateRooms {
		creationContent["m.federate"] = false
	}
	autoJoinInvites := portal.bridge.SpecVersions.Supports(mautrix.BeeperFeatureAutojoinInvites)
	if autoJoinInvites {
		portal.zlog.Debug().Msg("Hungryserv mode: adding all group members in create request")
		invite = append(invite, members...)
		invite = append(invite, user.MXID)
	}
	req := &mautrix.ReqCreateRoom{
		Visibility:      "private",
		Name:            portal.Name,
		Invite:          invite,
		Preset:          "private_chat",
		IsDirect:        portal.IsPrivateChat(),
		InitialState:    initialState,
		CreationContent: creationContent,

		BeeperAutoJoinInvites: autoJoinInvites,
	}
	if !portal.shouldSetDMRoomMetadata() {
		req.Name = ""
	}
	resp, err := intent.CreateRoom(req)
	if err != nil {
		return err
	}
	portal.zlog.Info().Str("room_id", resp.RoomID.String()).Msg("Matrix room created")
	portal.InSpace = false
	portal.NameSet = len(req.Name) > 0
	portal.MXID = resp.RoomID
	portal.bridge.portalsLock.Lock()
	portal.bridge.portalsByMXID[portal.MXID] = portal
	portal.bridge.portalsLock.Unlock()
	err = portal.Update(context.TODO())
	if err != nil {
		portal.zlog.Err(err).Msg("Failed to save portal after creating room")
	}

	// We set the memberships beforehand to make sure the encryption key exchange in initial backfill knows the users are here.
	inviteMembership := event.MembershipInvite
	if autoJoinInvites {
		inviteMembership = event.MembershipJoin
	}
	for _, userID := range invite {
		portal.bridge.StateStore.SetMembership(portal.MXID, userID, inviteMembership)
	}

	if !autoJoinInvites {
		if !portal.IsPrivateChat() {
			portal.SyncParticipants(user, conv)
		} else {
			if portal.bridge.Config.Bridge.Encryption.Default {
				err = portal.bridge.Bot.EnsureJoined(portal.MXID)
				if err != nil {
					portal.log.Errorln("Failed to join created portal with bridge bot for e2be:", err)
				}
			}

			user.UpdateDirectChats(map[id.UserID][]id.RoomID{portal.GetDMPuppet().MXID: {portal.MXID}})
		}
		portal.ensureUserInvited(user)
	}
	user.syncChatDoublePuppetDetails(portal, conv, true)

	go portal.addToPersonalSpace(user, true)
	return nil
}

func (portal *Portal) addToPersonalSpace(user *User, updateInfo bool) bool {
	spaceID := user.GetSpaceRoom()
	if len(spaceID) == 0 || portal.InSpace {
		return false
	}
	_, err := portal.bridge.Bot.SendStateEvent(spaceID, event.StateSpaceChild, portal.MXID.String(), &event.SpaceChildEventContent{
		Via: []string{portal.bridge.Config.Homeserver.Domain},
	})
	if err != nil {
		portal.zlog.Err(err).Str("space_id", spaceID.String()).Msg("Failed to add room to user's personal filtering space")
		portal.InSpace = false
	} else {
		portal.zlog.Debug().Str("space_id", spaceID.String()).Msg("Added room to user's personal filtering space")
		portal.InSpace = true
	}
	if updateInfo {
		err = portal.Update(context.TODO())
		if err != nil {
			portal.zlog.Err(err).Msg("Failed to update portal after adding to personal space")
		}
	}
	return true
}

func (portal *Portal) IsPrivateChat() bool {
	return portal.OtherUserID != ""
}

func (portal *Portal) GetDMPuppet() *Puppet {
	if portal.IsPrivateChat() {
		puppet := portal.bridge.GetPuppetByKey(database.Key{Receiver: portal.Receiver, ID: portal.OtherUserID}, "")
		return puppet
	}
	return nil
}

func (portal *Portal) MainIntent() *appservice.IntentAPI {
	if puppet := portal.GetDMPuppet(); puppet != nil {
		return puppet.DefaultIntent()
	}
	return portal.bridge.Bot
}

func (portal *Portal) sendMainIntentMessage(content *event.MessageEventContent) (*mautrix.RespSendEvent, error) {
	return portal.sendMessage(portal.MainIntent(), event.EventMessage, content, nil, 0)
}

func (portal *Portal) encrypt(intent *appservice.IntentAPI, content *event.Content, eventType event.Type) (event.Type, error) {
	if !portal.Encrypted || portal.bridge.Crypto == nil {
		return eventType, nil
	}
	intent.AddDoublePuppetValue(content)
	// TODO maybe the locking should be inside mautrix-go?
	portal.encryptLock.Lock()
	defer portal.encryptLock.Unlock()
	err := portal.bridge.Crypto.Encrypt(portal.MXID, eventType, content)
	if err != nil {
		return eventType, fmt.Errorf("failed to encrypt event: %w", err)
	}
	return event.EventEncrypted, nil
}

func (portal *Portal) sendMessage(intent *appservice.IntentAPI, eventType event.Type, content *event.MessageEventContent, extraContent map[string]interface{}, timestamp int64) (*mautrix.RespSendEvent, error) {
	wrappedContent := event.Content{Parsed: content, Raw: extraContent}
	var err error
	eventType, err = portal.encrypt(intent, &wrappedContent, eventType)
	if err != nil {
		return nil, err
	}

	_, _ = intent.UserTyping(portal.MXID, false, 0)
	if timestamp == 0 {
		return intent.SendMessageEvent(portal.MXID, eventType, &wrappedContent)
	} else {
		return intent.SendMassagedMessageEvent(portal.MXID, eventType, &wrappedContent, timestamp)
	}
}

func (portal *Portal) encryptFileInPlace(data []byte, mimeType string) (string, *event.EncryptedFileInfo) {
	if !portal.Encrypted {
		return mimeType, nil
	}

	file := &event.EncryptedFileInfo{
		EncryptedFile: *attachment.NewEncryptedFile(),
		URL:           "",
	}
	file.EncryptInPlace(data)
	return "application/octet-stream", file
}

func (portal *Portal) uploadMedia(intent *appservice.IntentAPI, data []byte, content *event.MessageEventContent) error {
	uploadMimeType, file := portal.encryptFileInPlace(data, content.Info.MimeType)

	req := mautrix.ReqUploadMedia{
		ContentBytes: data,
		ContentType:  uploadMimeType,
	}
	var mxc id.ContentURI
	if portal.bridge.Config.Homeserver.AsyncMedia {
		uploaded, err := intent.UploadAsync(req)
		if err != nil {
			return err
		}
		mxc = uploaded.ContentURI
	} else {
		uploaded, err := intent.UploadMedia(req)
		if err != nil {
			return err
		}
		mxc = uploaded.ContentURI
	}

	if file != nil {
		file.URL = mxc.CUString()
		content.File = file
	} else {
		content.URL = mxc.CUString()
	}

	content.Info.Size = len(data)
	if content.Info.Width == 0 && content.Info.Height == 0 && strings.HasPrefix(content.Info.MimeType, "image/") {
		cfg, _, _ := image.DecodeConfig(bytes.NewReader(data))
		content.Info.Width, content.Info.Height = cfg.Width, cfg.Height
	}

	// This is a hack for bad clients like Element iOS that require a thumbnail (https://github.com/vector-im/element-ios/issues/4004)
	if strings.HasPrefix(content.Info.MimeType, "image/") && content.Info.ThumbnailInfo == nil {
		infoCopy := *content.Info
		content.Info.ThumbnailInfo = &infoCopy
		if content.File != nil {
			content.Info.ThumbnailFile = file
		} else {
			content.Info.ThumbnailURL = content.URL
		}
	}
	return nil
}

func (portal *Portal) HandleMatrixMessage(sender *User, evt *event.Event, timings messageTimings) {
	ms := metricSender{portal: portal, timings: &timings}

	log := portal.zlog.With().Str("event_id", evt.ID.String()).Logger()
	log.Debug().Dur("age", timings.totalReceive).Msg("Handling Matrix message")

	content, ok := evt.Content.Parsed.(*event.MessageEventContent)
	if !ok {
		return
	}

	txnID := util.GenerateTmpId()
	portal.outgoingMessagesLock.Lock()
	portal.outgoingMessages[txnID] = evt.ID
	portal.outgoingMessagesLock.Unlock()
	switch content.MsgType {
	case event.MsgText, event.MsgEmote, event.MsgNotice:
		text := content.Body
		if content.MsgType == event.MsgEmote {
			text = "/me " + text
		}
		_, err := sender.Client.Conversations.SendMessage(
			sender.Client.NewMessageBuilder().
				SetConversationID(portal.ID).
				SetSelfParticipantID(portal.SelfUserID).
				SetContent(text).
				SetTmpID(txnID),
		)
		if err != nil {
			go ms.sendMessageMetrics(evt, err, "Error sending", true)
		} else {
			go ms.sendMessageMetrics(evt, nil, "", true)
		}
	default:
		go ms.sendMessageMetrics(evt, fmt.Errorf("unsupported msgtype"), "Ignoring", true)
	}
}

func (portal *Portal) HandleMatrixReaction(sender *User, evt *event.Event) {

}

func (portal *Portal) Delete() {
	err := portal.Portal.Delete(context.TODO())
	if err != nil {
		portal.zlog.Err(err).Msg("Failed to delete portal from database")
	}
	portal.bridge.portalsLock.Lock()
	delete(portal.bridge.portalsByKey, portal.Key)
	if len(portal.MXID) > 0 {
		delete(portal.bridge.portalsByMXID, portal.MXID)
	}
	portal.bridge.portalsLock.Unlock()
}

func (portal *Portal) GetMatrixUsers() ([]id.UserID, error) {
	members, err := portal.MainIntent().JoinedMembers(portal.MXID)
	if err != nil {
		return nil, fmt.Errorf("failed to get member list: %w", err)
	}
	var users []id.UserID
	for userID := range members.Joined {
		_, isPuppet := portal.bridge.ParsePuppetMXID(userID)
		if !isPuppet && userID != portal.bridge.Bot.UserID {
			users = append(users, userID)
		}
	}
	return users, nil
}

func (portal *Portal) CleanupIfEmpty() {
	users, err := portal.GetMatrixUsers()
	if err != nil {
		portal.log.Errorfln("Failed to get Matrix user list to determine if portal needs to be cleaned up: %v", err)
		return
	}

	if len(users) == 0 {
		portal.log.Infoln("Room seems to be empty, cleaning up...")
		portal.Delete()
		portal.Cleanup(false)
	}
}

func (portal *Portal) Cleanup(puppetsOnly bool) {
	if len(portal.MXID) == 0 {
		return
	}
	intent := portal.MainIntent()
	if portal.bridge.SpecVersions.Supports(mautrix.BeeperFeatureRoomYeeting) {
		err := intent.BeeperDeleteRoom(portal.MXID)
		if err != nil && !errors.Is(err, mautrix.MNotFound) {
			portal.zlog.Err(err).Msg("Failed to delete room using hungryserv yeet endpoint")
		}
		return
	}
	members, err := intent.JoinedMembers(portal.MXID)
	if err != nil {
		portal.log.Errorln("Failed to get portal members for cleanup:", err)
		return
	}
	for member := range members.Joined {
		if member == intent.UserID {
			continue
		}
		puppet := portal.bridge.GetPuppetByMXID(member)
		if puppet != nil {
			_, err = puppet.DefaultIntent().LeaveRoom(portal.MXID)
			if err != nil {
				portal.log.Errorln("Error leaving as puppet while cleaning up portal:", err)
			}
		} else if !puppetsOnly {
			_, err = intent.KickUser(portal.MXID, &mautrix.ReqKickUser{UserID: member, Reason: "Deleting portal"})
			if err != nil {
				portal.log.Errorln("Error kicking user while cleaning up portal:", err)
			}
		}
	}
	_, err = intent.LeaveRoom(portal.MXID)
	if err != nil {
		portal.log.Errorln("Error leaving with main intent while cleaning up portal:", err)
	}
}
