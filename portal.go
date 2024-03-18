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
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gabriel-vasile/mimetype"
	"github.com/rs/zerolog"
	"go.mau.fi/util/exerrors"
	"go.mau.fi/util/ffmpeg"
	"go.mau.fi/util/random"
	"go.mau.fi/util/variationselector"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/appservice"
	"maunium.net/go/mautrix/bridge"
	"maunium.net/go/mautrix/crypto/attachment"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-gmessages/database"
	"go.mau.fi/mautrix-gmessages/libgm"
	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
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

func (br *GMBridge) GetPortalByOtherUser(key database.Key) *Portal {
	br.portalsLock.Lock()
	defer br.portalsLock.Unlock()
	portal, ok := br.portalsByOtherUser[key]
	if !ok {
		dbPortal, err := br.DB.Portal.GetByOtherUser(context.TODO(), key)
		if err != nil {
			br.ZLog.Err(err).Object("portal_key", key).Msg("Failed to get portal from database")
			return nil
		}
		if dbPortal != nil {
			existingPortal, ok := br.portalsByKey[dbPortal.Key]
			if ok {
				return existingPortal
			}
		}
		return br.loadDBPortal(dbPortal, nil)
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
	if len(portal.OtherUserID) > 0 {
		br.portalsByOtherUser[database.Key{ID: portal.OtherUserID, Receiver: portal.Receiver}] = portal
	}
	return portal
}

func (portal *Portal) GetUsers() []*User {
	return nil
}

func (portal *Portal) updateLogger() {
	portal.zlog = portal.bridge.ZLog.With().
		Str("portal_id", portal.ID).
		Int("portal_receiver", portal.Receiver).
		Str("room_id", portal.MXID.String()).
		Logger()
}

func (br *GMBridge) NewPortal(dbPortal *database.Portal) *Portal {
	portal := &Portal{
		Portal: dbPortal,

		bridge: br,

		messages:       make(chan PortalMessage, br.Config.Bridge.PortalMessageBuffer),
		matrixMessages: make(chan PortalMatrixMessage, br.Config.Bridge.PortalMessageBuffer),

		outgoingMessages: make(map[string]*outgoingMessage),
	}
	portal.updateLogger()
	go portal.handleMessageLoop()
	return portal
}

const recentlyHandledLength = 100

type PortalMessage struct {
	evt    *gmproto.Message
	source *User
	raw    []byte
}

type PortalMatrixMessage struct {
	evt        *event.Event
	user       *User
	receivedAt time.Time
}

type outgoingMessage struct {
	*event.Event
	Saved        bool
	Checkpointed bool
}

type Portal struct {
	*database.Portal

	bridge *GMBridge
	zlog   zerolog.Logger

	roomCreateLock sync.Mutex
	encryptLock    sync.Mutex
	backfillLock   sync.Mutex
	avatarLock     sync.Mutex

	forwardBackfillLock sync.Mutex
	lastMessageTS       time.Time
	lastUserReadID      string
	hasSyncedThisRun    bool

	pendingRecentBackfill atomic.Pointer[pendingBackfill]

	recentlyHandled      [recentlyHandledLength]string
	recentlyHandledLock  sync.Mutex
	recentlyHandledIndex uint8

	outgoingMessages     map[string]*outgoingMessage
	outgoingMessagesLock sync.Mutex

	currentlyTyping     []id.UserID
	currentlyTypingLock sync.Mutex

	messages       chan PortalMessage
	matrixMessages chan PortalMatrixMessage

	cancelCreation atomic.Pointer[context.CancelCauseFunc]
}

var (
	_ bridge.Portal                    = (*Portal)(nil)
	_ bridge.ReadReceiptHandlingPortal = (*Portal)(nil)
	//_ bridge.MembershipHandlingPortal  = (*Portal)(nil)
	//_ bridge.MetaHandlingPortal        = (*Portal)(nil)
	//_ bridge.TypingPortal              = (*Portal)(nil)
)

func (portal *Portal) handleMessageLoopItem(msg PortalMessage) {
	if len(portal.MXID) == 0 {
		portal.zlog.Warn().Str("message_id", msg.evt.MessageID).Msg("Dropping message as portal is not yet created")
		return
	}
	portal.forwardBackfillLock.Lock()
	defer portal.forwardBackfillLock.Unlock()
	switch {
	case msg.evt != nil:
		portal.handleMessage(msg.source, msg.evt, msg.raw)
	default:
		portal.zlog.Warn().Interface("portal_message", msg).Msg("Unexpected PortalMessage with no message")
	}
}

func (portal *Portal) handleMatrixMessageLoopItem(msg PortalMatrixMessage) {
	if msg.user.RowID != portal.Receiver {
		go portal.sendMessageMetrics(context.TODO(), msg.user, msg.evt, errIncorrectUser, "Ignoring", nil)
		return
	} else if msg.user.Client == nil {
		go portal.sendMessageMetrics(context.TODO(), msg.user, msg.evt, errNotLoggedIn, "Ignoring", nil)
		return
	}
	portal.forwardBackfillLock.Lock()
	defer portal.forwardBackfillLock.Unlock()
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
	case event.EventRedaction:
		portal.HandleMatrixRedaction(msg.user, msg.evt)
	default:
		portal.zlog.Warn().
			Str("event_type", msg.evt.Type.Type).
			Msg("Unsupported event type in portal message channel")
	}
}

func (portal *Portal) handleMessageLoop() {
	for {
		portal.handleOneMessageLoopItem()
	}
}

func (portal *Portal) handleOneMessageLoopItem() {
	defer func() {
		if err := recover(); err != nil {
			logEvt := portal.zlog.WithLevel(zerolog.FatalLevel).
				Str(zerolog.ErrorStackFieldName, string(debug.Stack()))
			actualErr, ok := err.(error)
			if ok {
				logEvt = logEvt.Err(actualErr)
			} else {
				logEvt = logEvt.Any(zerolog.ErrorFieldName, err)
			}
			logEvt.Msg("Portal message handler panicked")
		}
	}()
	select {
	case msg := <-portal.messages:
		portal.handleMessageLoopItem(msg)
	case msg := <-portal.matrixMessages:
		portal.handleMatrixMessageLoopItem(msg)
	}
}

func (portal *Portal) isOutgoingMessage(msg *gmproto.Message) *database.Message {
	portal.outgoingMessagesLock.Lock()
	defer portal.outgoingMessagesLock.Unlock()
	out, ok := portal.outgoingMessages[msg.TmpID]
	if ok {
		delete(portal.outgoingMessages, msg.TmpID)
		return portal.markHandled(&ConvertedMessage{
			ID:        msg.MessageID,
			Timestamp: time.UnixMicro(msg.GetTimestamp()),
			SenderID:  msg.ParticipantID,
			PartCount: len(msg.GetMessageInfo()),
		}, out.ID, nil, true)
	}
	return nil
}

func hasInProgressMedia(msg *gmproto.Message) bool {
	for _, part := range msg.MessageInfo {
		media, ok := part.GetData().(*gmproto.MessageInfo_MediaContent)
		if ok && media.MediaContent.GetMediaID() == "" && media.MediaContent.GetMediaID2() == "" {
			return true
		}
	}
	return false
}

func isSuccessfullySentStatus(status gmproto.MessageStatusType) bool {
	switch status {
	case gmproto.MessageStatusType_OUTGOING_DELIVERED, gmproto.MessageStatusType_OUTGOING_COMPLETE, gmproto.MessageStatusType_OUTGOING_DISPLAYED:
		return true
	default:
		return false
	}
}

func downloadPendingStatusMessage(status gmproto.MessageStatusType) string {
	switch status {
	case gmproto.MessageStatusType_INCOMING_YET_TO_MANUAL_DOWNLOAD:
		return "Attachment message (auto-download is disabled, use Messages on Android to download)"
	case gmproto.MessageStatusType_INCOMING_MANUAL_DOWNLOADING,
		gmproto.MessageStatusType_INCOMING_AUTO_DOWNLOADING,
		gmproto.MessageStatusType_INCOMING_RETRYING_MANUAL_DOWNLOAD,
		gmproto.MessageStatusType_INCOMING_RETRYING_AUTO_DOWNLOAD:
		return "Downloading message..."
	case gmproto.MessageStatusType_INCOMING_DOWNLOAD_FAILED:
		return "Message download failed"
	case gmproto.MessageStatusType_INCOMING_DOWNLOAD_FAILED_TOO_LARGE:
		return "Message download failed (too large)"
	case gmproto.MessageStatusType_INCOMING_DOWNLOAD_FAILED_SIM_HAS_NO_DATA:
		return "Message download failed (no mobile data connection)"
	case gmproto.MessageStatusType_INCOMING_DOWNLOAD_CANCELED:
		return "Message download canceled"
	default:
		return ""
	}
}

func isFailSendStatus(status gmproto.MessageStatusType) bool {
	switch status {
	case gmproto.MessageStatusType_OUTGOING_FAILED_GENERIC,
		gmproto.MessageStatusType_OUTGOING_FAILED_EMERGENCY_NUMBER,
		gmproto.MessageStatusType_OUTGOING_CANCELED,
		gmproto.MessageStatusType_OUTGOING_FAILED_TOO_LARGE,
		gmproto.MessageStatusType_OUTGOING_FAILED_RECIPIENT_LOST_RCS,
		gmproto.MessageStatusType_OUTGOING_FAILED_NO_RETRY_NO_FALLBACK,
		gmproto.MessageStatusType_OUTGOING_FAILED_RECIPIENT_DID_NOT_DECRYPT,
		gmproto.MessageStatusType_OUTGOING_FAILED_RECIPIENT_LOST_ENCRYPTION,
		gmproto.MessageStatusType_OUTGOING_FAILED_RECIPIENT_DID_NOT_DECRYPT_NO_MORE_RETRY:
		return true
	default:
		return false
	}
}

func downloadStatusRank(status gmproto.MessageStatusType) int {
	switch status {
	case gmproto.MessageStatusType_INCOMING_AUTO_DOWNLOADING:
		return 0
	case gmproto.MessageStatusType_INCOMING_MANUAL_DOWNLOADING,
		gmproto.MessageStatusType_INCOMING_RETRYING_AUTO_DOWNLOAD,
		gmproto.MessageStatusType_INCOMING_DOWNLOAD_FAILED,
		gmproto.MessageStatusType_INCOMING_YET_TO_MANUAL_DOWNLOAD,
		gmproto.MessageStatusType_INCOMING_RETRYING_MANUAL_DOWNLOAD,
		gmproto.MessageStatusType_INCOMING_DOWNLOAD_FAILED_SIM_HAS_NO_DATA,
		gmproto.MessageStatusType_INCOMING_DOWNLOAD_FAILED_TOO_LARGE,
		gmproto.MessageStatusType_INCOMING_DOWNLOAD_CANCELED:
		return 1
	default:
		return 100
	}
}

func (portal *Portal) redactMessage(ctx context.Context, msg *database.Message) {
	if msg.IsFakeMXID() {
		return
	}
	log := zerolog.Ctx(ctx)
	intent := portal.MainIntent()
	if msg.Chat.ID != portal.ID {
		otherPortal := portal.bridge.GetExistingPortalByKey(msg.Chat)
		if otherPortal != nil {
			intent = otherPortal.MainIntent()
		}
	}
	for partID, part := range msg.Status.MediaParts {
		if part.EventID != "" {
			if _, err := intent.RedactEvent(ctx, msg.RoomID, part.EventID); err != nil {
				log.Err(err).Str("part_id", partID).Msg("Failed to redact part of message")
			}
			part.EventID = ""
			msg.Status.MediaParts[partID] = part
		}
	}
	if _, err := intent.RedactEvent(ctx, msg.RoomID, msg.MXID); err != nil {
		log.Err(err).Msg("Failed to redact message")
	}
	msg.MXID = ""
}

func (portal *Portal) handleExistingMessageUpdate(ctx context.Context, source *User, dbMsg *database.Message, evt *gmproto.Message, raw []byte) {
	log := *zerolog.Ctx(ctx)
	newStatus := evt.GetMessageStatus().GetStatus()
	// Messages in different portals may have race conditions, ignore the most common case
	// (group MMS event happens in DM after group).
	if downloadStatusRank(newStatus) < downloadStatusRank(dbMsg.Status.Type) {
		log.Debug().
			Str("old_status", dbMsg.Status.Type.String()).
			Str("new_status", newStatus.String()).
			Msg("Ignoring message status change as it's a downgrade")
		return
	}
	chatIDChanged := dbMsg.Chat.ID != portal.ID
	hasPendingMedia := dbMsg.Status.HasPendingMediaParts()
	updatedMediaIsComplete := !hasInProgressMedia(evt)
	if dbMsg.Status.Type == newStatus && !chatIDChanged && !(hasPendingMedia && updatedMediaIsComplete) {
		logEvt := log.Debug().
			Str("old_status", dbMsg.Status.Type.String()).
			Bool("has_pending_media", hasPendingMedia).
			Bool("updated_media_is_complete", updatedMediaIsComplete)
		if hasPendingMedia {
			debugData := zerolog.Dict()
			for _, part := range evt.MessageInfo {
				media, ok := part.GetData().(*gmproto.MessageInfo_MediaContent)
				if ok {
					debugData.Dict(
						part.GetActionMessageID(),
						zerolog.Dict().
							Str("media_id_1", media.MediaContent.GetMediaID()).
							Str("media_id_2", media.MediaContent.GetMediaID2()).
							Int64("size", media.MediaContent.GetSize()).
							Int64("width", media.MediaContent.GetDimensions().GetWidth()).
							Int64("height", media.MediaContent.GetDimensions().GetHeight()).
							Bool("has_key_1", len(media.MediaContent.GetDecryptionKey()) > 0).
							Bool("has_key_2", len(media.MediaContent.GetDecryptionKey2()) > 0).
							Bool("has_unknown_fields", len(media.MediaContent.ProtoReflect().GetUnknown()) > 0),
					)
				} else {
					debugData.Str(part.GetActionMessageID(), "not media")
				}
			}
			logEvt = logEvt.Dict("pending_media_debug_data", debugData)
		}
		logEvt.Msg("Nothing changed in message update, just syncing reactions")
		portal.syncReactions(ctx, source, dbMsg, evt.Reactions)
		return
	}
	if chatIDChanged {
		log = log.With().Str("old_chat_id", dbMsg.Chat.ID).Logger()
		if downloadPendingStatusMessage(newStatus) != "" && !portal.IsPrivateChat() {
			log.Debug().Msg("Ignoring chat ID change from group chat as update is a pending download")
			return
		}
		log.Debug().
			Str("old_room_id", dbMsg.RoomID.String()).
			Str("sender_id", dbMsg.Sender).
			Msg("Redacting events from old room")
		ctx = log.WithContext(ctx)
		err := portal.bridge.DB.Reaction.DeleteAllByMessage(ctx, dbMsg.Chat, dbMsg.ID)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to delete db reactions for message that moved to another room")
		}
		portal.redactMessage(ctx, dbMsg)
	}
	log.Debug().
		Str("old_status", dbMsg.Status.Type.String()).
		Bool("has_pending_media", hasPendingMedia).
		Bool("updated_media_is_complete", updatedMediaIsComplete).
		Msg("Message status changed")
	switch {
	case newStatus == gmproto.MessageStatusType_MESSAGE_DELETED:
		portal.redactMessage(ctx, dbMsg)
		if err := dbMsg.Delete(ctx); err != nil {
			log.Err(err).Msg("Failed to delete message from database")
		} else {
			log.Debug().Msg("Handled message deletion")
		}
		return
	case chatIDChanged,
		dbMsg.Status.MediaStatus != downloadPendingStatusMessage(newStatus),
		hasPendingMedia && updatedMediaIsComplete,
		dbMsg.Status.PartCount != len(evt.MessageInfo):
		converted := portal.convertGoogleMessage(ctx, source, evt, false, raw)
		if converted == nil {
			log.Warn().Msg("Didn't get converted parts for updated event")
			return
		}
		dbMsg.Status.MediaStatus = converted.MediaStatus
		if dbMsg.Status.MediaParts == nil {
			dbMsg.Status.MediaParts = make(map[string]database.MediaPart)
		}
		eventIDs := make([]id.EventID, 0, len(converted.Parts))
		for i, part := range converted.Parts {
			isEdit := true
			ts := time.Now().UnixMilli()
			if chatIDChanged {
				isEdit = false
			} else if i == 0 {
				part.SetEdit(dbMsg.MXID)
			} else if existingPart, ok := dbMsg.Status.MediaParts[part.ID]; ok {
				part.SetEdit(existingPart.EventID)
			} else {
				ts = converted.Timestamp.UnixMilli()
				isEdit = false
			}
			resp, err := portal.sendMessage(ctx, converted.Intent, event.EventMessage, part.Content, part.Extra, ts)
			if err != nil {
				log.Err(err).Msg("Failed to send message")
				continue
			} else {
				eventIDs = append(eventIDs, resp.EventID)
			}
			if i == 0 {
				if chatIDChanged {
					dbMsg.MXID = resp.EventID
				}
				dbMsg.Status.MediaParts[""] = database.MediaPart{PendingMedia: part.PendingMedia}
			} else if !isEdit {
				dbMsg.Status.MediaParts[part.ID] = database.MediaPart{EventID: resp.EventID, PendingMedia: part.PendingMedia}
			}
		}
		if len(eventIDs) > 0 {
			portal.sendDeliveryReceipt(ctx, eventIDs[len(eventIDs)-1])
			log.Debug().Interface("event_ids", eventIDs).Msg("Handled update to message")
		}
	case !dbMsg.Status.ReadReceiptSent && portal.IsPrivateChat() && newStatus == gmproto.MessageStatusType_OUTGOING_DISPLAYED:
		dbMsg.Status.ReadReceiptSent = true
		if !dbMsg.Status.MSSSent {
			portal.sendCheckpoint(dbMsg, nil, false)
		}
		if !dbMsg.Status.MSSDeliverySent {
			dbMsg.Status.MSSDeliverySent = true
			dbMsg.Status.MSSSent = true
			go portal.sendStatusEvent(ctx, dbMsg.MXID, "", nil, &[]id.UserID{portal.MainIntent().UserID})
			portal.sendCheckpoint(dbMsg, nil, true)
		}
		err := portal.MainIntent().MarkRead(ctx, portal.MXID, dbMsg.MXID)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to mark message as read")
		}
	case !dbMsg.Status.MSSDeliverySent && portal.IsPrivateChat() && newStatus == gmproto.MessageStatusType_OUTGOING_DELIVERED:
		if !dbMsg.Status.MSSSent {
			portal.sendCheckpoint(dbMsg, nil, false)
		}
		portal.sendCheckpoint(dbMsg, nil, true)
		dbMsg.Status.MSSDeliverySent = true
		dbMsg.Status.MSSSent = true
		go portal.sendStatusEvent(ctx, dbMsg.MXID, "", nil, &[]id.UserID{portal.MainIntent().UserID})
	case !dbMsg.Status.MSSSent && isSuccessfullySentStatus(newStatus):
		dbMsg.Status.MSSSent = true
		var deliveredTo *[]id.UserID
		// TODO SMSes can enable delivery receipts too, but can it be detected?
		if portal.IsPrivateChat() && portal.Type == gmproto.ConversationType_RCS {
			deliveredTo = &[]id.UserID{}
		}
		go portal.sendStatusEvent(ctx, dbMsg.MXID, "", nil, deliveredTo)
		portal.sendCheckpoint(dbMsg, nil, false)
	case !dbMsg.Status.MSSFailSent && !dbMsg.Status.MSSSent && isFailSendStatus(newStatus):
		go portal.sendStatusEvent(ctx, dbMsg.MXID, "", OutgoingStatusError(newStatus), nil)
		portal.sendCheckpoint(dbMsg, OutgoingStatusError(newStatus), false)
		// TODO error notice
	default:
		log.Debug().Msg("Ignored message update")
		// TODO do something?
	}
	dbMsg.Status.Type = newStatus
	dbMsg.Status.PartCount = len(evt.MessageInfo)
	dbMsg.Timestamp = time.UnixMicro(evt.GetTimestamp())
	var err error
	if chatIDChanged {
		dbMsg.Chat = portal.Key
		dbMsg.RoomID = portal.MXID
		err = dbMsg.Update(ctx)
	} else {
		err = dbMsg.UpdateStatus(ctx)
	}
	if err != nil {
		log.Warn().Err(err).Msg("Failed to save updated message status to database")
	}
	portal.syncReactions(ctx, source, dbMsg, evt.Reactions)
}

func (portal *Portal) handleExistingMessage(ctx context.Context, source *User, evt *gmproto.Message, outgoingOnly bool, raw []byte) bool {
	log := zerolog.Ctx(ctx)
	if existingMsg := portal.isOutgoingMessage(evt); existingMsg != nil {
		log.Debug().Str("event_id", existingMsg.MXID.String()).Msg("Got echo for outgoing message")
		portal.handleExistingMessageUpdate(ctx, source, existingMsg, evt, raw)
		return true
	} else if outgoingOnly {
		return false
	}
	existingMsg, err := portal.bridge.DB.Message.GetByID(ctx, portal.Receiver, evt.MessageID)
	if err != nil {
		log.Err(err).Msg("Failed to check if message is duplicate")
	} else if existingMsg != nil {
		portal.handleExistingMessageUpdate(ctx, source, existingMsg, evt, raw)
		return true
	}
	return false
}

func idToInt(id string) int {
	i, err := strconv.Atoi(id)
	if err != nil {
		return 0
	}
	return i
}

func (portal *Portal) handleMessage(source *User, evt *gmproto.Message, raw []byte) {
	eventTS := time.UnixMicro(evt.GetTimestamp())
	if eventTS.After(portal.lastMessageTS) {
		portal.lastMessageTS = eventTS
	}
	log := portal.zlog.With().
		Str("message_id", evt.MessageID).
		Str("participant_id", evt.ParticipantID).
		Str("status", evt.GetMessageStatus().GetStatus().String()).
		Time("message_timestamp", eventTS).
		Str("action", "handle google message").
		Logger()
	ctx := log.WithContext(context.TODO())
	if portal.handleExistingMessage(ctx, source, evt, false, raw) {
		return
	}
	switch evt.GetMessageStatus().GetStatus() {
	case gmproto.MessageStatusType_MESSAGE_DELETED:
		log.Debug().Msg("Not handling unknown deleted message")
		return
	case gmproto.MessageStatusType_INCOMING_AUTO_DOWNLOADING, gmproto.MessageStatusType_INCOMING_RETRYING_AUTO_DOWNLOAD:
		log.Debug().Msg("Not handling incoming auto-downloading MMS")
		return
	}
	if eventTS.Add(24 * time.Hour).Before(time.Now()) {
		lastMessage, err := portal.bridge.DB.Message.GetLastInChat(ctx, portal.Key)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to get last message to check if received old message is too old")
		} else if lastMessage != nil && lastMessage.Timestamp.After(eventTS) && idToInt(lastMessage.ID) > idToInt(evt.MessageID) {
			log.Debug().Msg("Not handling old message")
			return
		}
	}

	converted := portal.convertGoogleMessage(ctx, source, evt, false, raw)
	eventIDs := portal.sendMessageParts(ctx, converted, nil)
	if len(eventIDs) > 0 {
		portal.sendDeliveryReceipt(ctx, eventIDs[len(eventIDs)-1])
		log.Debug().Interface("event_ids", eventIDs).Msg("Handled message")
	}
}

func (portal *Portal) sendMessageParts(ctx context.Context, converted *ConvertedMessage, replyToMap map[string]id.EventID) []id.EventID {
	if converted == nil {
		return nil
	} else if len(converted.Parts) == 0 {
		zerolog.Ctx(ctx).Debug().Msg("Didn't get any converted parts from message")
		return nil
	} else if converted.DontBridge {
		zerolog.Ctx(ctx).Debug().Msg("Ignored incoming tombstone message")
		portal.markHandled(converted, id.EventID(fmt.Sprintf("$fake::%s", random.String(37))), nil, true)
		return nil
	}
	eventIDs := make([]id.EventID, 0, len(converted.Parts))
	mediaParts := make(map[string]database.MediaPart, len(converted.Parts)-1)
	for i, part := range converted.Parts {
		if replyToMap != nil && converted.ReplyTo != "" && part.Content.RelatesTo == nil {
			replyToEvent, ok := replyToMap[converted.ReplyTo]
			if ok {
				part.Content.RelatesTo = &event.RelatesTo{
					InReplyTo: &event.InReplyTo{EventID: replyToEvent},
				}
			}
		}
		resp, err := portal.sendMessage(ctx, converted.Intent, event.EventMessage, part.Content, part.Extra, converted.Timestamp.UnixMilli())
		if err != nil {
			zerolog.Ctx(ctx).Err(err).Int("part_index", i).Str("part_id", part.ID).Msg("Failed to send message")
		} else {
			eventIDs = append(eventIDs, resp.EventID)
			if len(eventIDs) > 1 {
				mediaParts[part.ID] = database.MediaPart{
					EventID:      resp.EventID,
					PendingMedia: part.PendingMedia,
				}
			} else if part.PendingMedia {
				mediaParts[""] = database.MediaPart{PendingMedia: true}
			}
		}
	}
	if len(eventIDs) > 0 {
		portal.markHandled(converted, eventIDs[0], mediaParts, true)
	} else {
		zerolog.Ctx(ctx).Warn().Msg("All parts of message failed to send")
	}
	return eventIDs
}

func (portal *Portal) syncReactions(ctx context.Context, source *User, message *database.Message, reactions []*gmproto.ReactionEntry) {
	log := zerolog.Ctx(ctx)
	existing, err := portal.bridge.DB.Reaction.GetAllByMessage(ctx, portal.Receiver, message.ID)
	if err != nil {
		log.Err(err).Msg("Failed to get existing reactions from db to sync reactions")
		return
	}
	remove := make(map[string]*database.Reaction, len(existing))
	for _, reaction := range existing {
		remove[reaction.Sender] = reaction
	}
	for _, reaction := range reactions {
		var emoji string
		switch reaction.GetData().GetType() {
		case gmproto.EmojiType_EMOTIFY:
			emoji = ":custom:"
		case gmproto.EmojiType_CUSTOM:
			emoji = reaction.GetData().GetUnicode()
		default:
			emoji = reaction.GetData().GetType().Unicode()
			if emoji == "" {
				continue
			}
		}
		for _, participant := range reaction.GetParticipantIDs() {
			dbReaction, ok := remove[participant]
			if ok {
				delete(remove, participant)
				if dbReaction.Reaction == emoji {
					continue
				}
			}
			intent := portal.getIntent(ctx, source, participant)
			if intent == nil {
				continue
			}
			var resp *mautrix.RespSendEvent
			resp, err = intent.SendMessageEvent(ctx, portal.MXID, event.EventReaction, &event.ReactionEventContent{
				RelatesTo: event.RelatesTo{
					EventID: message.MXID,
					Type:    event.RelAnnotation,
					Key:     variationselector.Add(emoji),
				},
			})
			if err != nil {
				log.Err(err).Str("reaction_sender_id", participant).Msg("Failed to send reaction")
				continue
			}
			if dbReaction == nil {
				dbReaction = portal.bridge.DB.Reaction.New()
				dbReaction.Chat = portal.Key
				dbReaction.Sender = participant
				dbReaction.MessageID = message.ID
			} else if _, err = intent.RedactEvent(ctx, portal.MXID, dbReaction.MXID); err != nil {
				log.Err(err).Str("reaction_sender_id", participant).Msg("Failed to redact old reaction after adding new one")
			}
			dbReaction.Reaction = emoji
			dbReaction.MXID = resp.EventID
			err = dbReaction.Insert(ctx)
			if err != nil {
				log.Err(err).Str("reaction_sender_id", participant).Msg("Failed to insert added reaction into db")
			}
		}
	}
	for _, reaction := range remove {
		intent := portal.getIntent(ctx, source, reaction.Sender)
		if intent == nil {
			continue
		}
		_, err = intent.RedactEvent(ctx, portal.MXID, reaction.MXID)
		if err != nil {
			log.Err(err).Str("reaction_sender_id", reaction.Sender).Msg("Failed to redact removed reaction")
		} else if err = reaction.Delete(ctx); err != nil {
			log.Err(err).Str("reaction_sender_id", reaction.Sender).Msg("Failed to remove reaction from db")
		}
	}
}

type ConvertedMessagePart struct {
	ID           string
	PendingMedia bool
	Content      *event.MessageEventContent
	Extra        map[string]any
}

func (cmp *ConvertedMessagePart) SetEdit(eventID id.EventID) {
	cmp.Content.SetEdit(eventID)
	if cmp.Extra != nil {
		cmp.Extra = map[string]any{
			"m.new_content": cmp.Extra,
		}
	}
}

type ConvertedMessage struct {
	ID       string
	SenderID string

	Intent    *appservice.IntentAPI
	Timestamp time.Time
	ReplyTo   string
	Parts     []ConvertedMessagePart
	PartCount int

	DontBridge bool

	Status      gmproto.MessageStatusType
	MediaStatus string
}

func (portal *Portal) getIntent(ctx context.Context, source *User, participant string) *appservice.IntentAPI {
	if source.IsSelfParticipantID(participant) {
		intent := source.DoublePuppetIntent
		if intent == nil {
			zerolog.Ctx(ctx).Debug().Msg("Dropping message from self as double puppeting is not enabled")
			return nil
		}
		return intent
	} else if portal.IsPrivateChat() {
		if participant != portal.OtherUserID {
			zerolog.Ctx(ctx).Warn().
				Str("participant_id", participant).
				Str("portal_other_user_id", portal.OtherUserID).
				Msg("Got unexpected participant ID for message in DM portal, forcing to main intent")
		}
		return portal.MainIntent()
	} else {
		puppet := source.GetPuppetByID(participant, "")
		if puppet == nil {
			zerolog.Ctx(ctx).Debug().Str("participant_id", participant).Msg("Dropping message from unknown participant")
			return nil
		}
		return puppet.IntentFor(portal)
	}
}

func addSubject(content *event.MessageEventContent, subject string) {
	content.Format = event.FormatHTML
	content.FormattedBody = fmt.Sprintf("<strong>%s</strong><br>%s", event.TextToHTML(subject), event.TextToHTML(content.Body))
	content.Body = fmt.Sprintf("**%s**\n%s", subject, content.Body)
}

func addDownloadStatus(content *event.MessageEventContent, status string) {
	content.Body = fmt.Sprintf("%s\n\n%s", content.Body, status)
	if content.Format == event.FormatHTML {
		content.FormattedBody = fmt.Sprintf("<p>%s</p><p>%s</p>", content.FormattedBody, event.TextToHTML(status))
	}
}

func (portal *Portal) shouldIgnoreStatus(status gmproto.MessageStatusType) bool {
	switch status {
	case gmproto.MessageStatusType_TOMBSTONE_PROTOCOL_SWITCH_TO_TEXT,
		gmproto.MessageStatusType_TOMBSTONE_PROTOCOL_SWITCH_TO_RCS,
		gmproto.MessageStatusType_TOMBSTONE_PROTOCOL_SWITCH_TO_ENCRYPTED_RCS,
		gmproto.MessageStatusType_TOMBSTONE_PROTOCOL_SWITCH_TO_ENCRYPTED_RCS_INFO,
		gmproto.MessageStatusType_TOMBSTONE_ONE_ON_ONE_SMS_CREATED,
		gmproto.MessageStatusType_TOMBSTONE_ONE_ON_ONE_RCS_CREATED,
		gmproto.MessageStatusType_TOMBSTONE_ENCRYPTED_ONE_ON_ONE_RCS_CREATED,
		gmproto.MessageStatusType_MESSAGE_STATUS_TOMBSTONE_PROTOCOL_SWITCH_TEXT_TO_E2EE,
		gmproto.MessageStatusType_MESSAGE_STATUS_TOMBSTONE_PROTOCOL_SWITCH_E2EE_TO_TEXT,
		gmproto.MessageStatusType_MESSAGE_STATUS_TOMBSTONE_PROTOCOL_SWITCH_RCS_TO_E2EE,
		gmproto.MessageStatusType_MESSAGE_STATUS_TOMBSTONE_PROTOCOL_SWITCH_E2EE_TO_RCS:
		return portal.IsPrivateChat()
	case gmproto.MessageStatusType_MESSAGE_STATUS_TOMBSTONE_ENCRYPTED_GROUP_CREATED,
		gmproto.MessageStatusType_MESSAGE_STATUS_TOMBSTONE_GROUP_PROTOCOL_SWITCH_E2EE_TO_RCS,
		gmproto.MessageStatusType_MESSAGE_STATUS_TOMBSTONE_GROUP_PROTOCOL_SWITCH_RCS_TO_E2EE,
		gmproto.MessageStatusType_TOMBSTONE_RCS_GROUP_CREATED,
		gmproto.MessageStatusType_TOMBSTONE_MMS_GROUP_CREATED,
		gmproto.MessageStatusType_TOMBSTONE_SMS_BROADCAST_CREATED:
		return true
	case gmproto.MessageStatusType_TOMBSTONE_SHOW_LINK_PREVIEWS:
		return true
	default:
		return false
	}
}

func (portal *Portal) convertGoogleMessage(ctx context.Context, source *User, evt *gmproto.Message, backfill bool, raw []byte) *ConvertedMessage {
	log := zerolog.Ctx(ctx)

	var cm ConvertedMessage
	cm.Status = evt.GetMessageStatus().GetStatus()
	cm.SenderID = evt.ParticipantID
	cm.ID = evt.MessageID
	cm.PartCount = len(evt.GetMessageInfo())
	cm.Timestamp = time.UnixMicro(evt.Timestamp)
	cm.DontBridge = portal.shouldIgnoreStatus(cm.Status)
	if cm.Status >= 200 && cm.Status < 300 {
		cm.Intent = portal.bridge.Bot
		if !portal.Encrypted && portal.IsPrivateChat() {
			cm.Intent = portal.MainIntent()
		}
	} else {
		cm.Intent = portal.getIntent(ctx, source, evt.ParticipantID)
		if cm.Intent == nil {
			return nil
		}
	}

	var replyTo id.EventID
	if evt.GetReplyMessage() != nil {
		cm.ReplyTo = evt.GetReplyMessage().GetMessageID()
		msg, err := portal.bridge.DB.Message.GetByID(ctx, portal.Receiver, cm.ReplyTo)
		if err != nil {
			log.Err(err).Str("reply_to_id", cm.ReplyTo).Msg("Failed to get reply target message")
		} else if msg == nil {
			if backfill {
				replyTo = portal.deterministicEventID(cm.ReplyTo, 0)
			} else {
				log.Warn().Str("reply_to_id", cm.ReplyTo).Msg("Reply target message not found")
			}
		} else if msg.IsFakeMXID() {
			log.Debug().Str("reply_to_id", msg.ID).Msg("Ignoring reply to non-bridged message")
		} else {
			replyTo = msg.MXID
		}
	}

	subject := evt.GetSubject()
	downloadStatus := downloadPendingStatusMessage(evt.GetMessageStatus().GetStatus())
	cm.MediaStatus = downloadStatus
	for _, part := range evt.MessageInfo {
		var content event.MessageEventContent
		var extra map[string]any
		pendingMedia := false
		switch data := part.GetData().(type) {
		case *gmproto.MessageInfo_MessageContent:
			content = event.MessageEventContent{
				MsgType: event.MsgText,
				Body:    data.MessageContent.GetContent(),
			}
			if subject != "" {
				addSubject(&content, subject)
				subject = ""
			}
			if downloadStatus != "" {
				addDownloadStatus(&content, downloadStatus)
				downloadStatus = ""
			}
		case *gmproto.MessageInfo_MediaContent:
			if data.MediaContent.MediaID == "" && data.MediaContent.MediaID2 == "" {
				pendingMedia = true
				content = event.MessageEventContent{
					MsgType: event.MsgNotice,
					Body:    fmt.Sprintf("Waiting for attachment %s", data.MediaContent.GetMediaName()),
				}
			} else if contentPtr, extraMap, err := portal.convertGoogleMedia(ctx, source, cm.Intent, data.MediaContent); err != nil {
				pendingMedia = true
				log.Err(err).Msg("Failed to copy attachment")
				content = event.MessageEventContent{
					MsgType: event.MsgNotice,
					Body:    fmt.Sprintf("Failed to transfer attachment %s", data.MediaContent.GetMediaName()),
				}
			} else {
				content = *contentPtr
				extra = extraMap
			}
		default:
			continue
		}
		if replyTo != "" {
			content.RelatesTo = &event.RelatesTo{InReplyTo: &event.InReplyTo{EventID: replyTo}}
		}
		cm.Parts = append(cm.Parts, ConvertedMessagePart{
			ID:           part.GetActionMessageID(),
			PendingMedia: pendingMedia,
			Content:      &content,
			Extra:        extra,
		})
	}
	if downloadStatus != "" {
		content := event.MessageEventContent{
			MsgType: event.MsgNotice,
			Body:    downloadStatus,
		}
		if subject != "" {
			addSubject(&content, subject)
			subject = ""
		}
		cm.Parts = append(cm.Parts, ConvertedMessagePart{Content: &content})
	}
	if subject != "" {
		cm.Parts = append(cm.Parts, ConvertedMessagePart{
			Content: &event.MessageEventContent{
				MsgType: event.MsgText,
				Body:    subject,
			},
		})
	}
	if portal.bridge.Config.Bridge.BeeperGalleries {
		cm.MergeGallery()
	}
	if portal.bridge.Config.Bridge.CaptionInMessage {
		cm.MergeCaption()
	}
	if raw != nil && base64.StdEncoding.EncodedLen(len(raw)) < 8192 && len(cm.Parts) > 0 {
		extra := cm.Parts[0].Extra
		if extra == nil {
			extra = make(map[string]any)
		}
		extra["fi.mau.gmessages.raw_debug_data"] = base64.StdEncoding.EncodeToString(raw)
		cm.Parts[0].Extra = extra
	}
	return &cm
}

func (msg *ConvertedMessage) MergeGallery() {
	var textPart *ConvertedMessagePart
	var pendingImageParts, pendingImagePartsHTML []string
	var imageParts []*event.MessageEventContent
	var pendingMedia bool

	for _, part := range msg.Parts {
		pendingMedia = pendingMedia || part.PendingMedia
		switch part.Content.MsgType {
		case event.MsgText:
			textPart = &part
		case event.MsgNotice:
			// TODO this doesn't handle formatted bodies in pending/failed media parts
			pendingImageParts = append(pendingImageParts, part.Content.Body)
			pendingImagePartsHTML = append(pendingImagePartsHTML, fmt.Sprintf("<p>%s</p>", event.TextToHTML(part.Content.Body)))
		case event.MsgImage, event.MsgVideo, event.MsgAudio, event.MsgFile:
			// TODO this ignores extra content in media parts
			imageParts = append(imageParts, part.Content)
		default:
			return
		}
	}

	if len(imageParts)+len(pendingImageParts) < 2 {
		return
	}

	var caption, captionHTML string
	if textPart != nil {
		caption = textPart.Content.Body
		captionHTML = textPart.Content.FormattedBody
		if captionHTML == "" {
			captionHTML = event.TextToHTML(caption)
		}
		if len(pendingImageParts) > 0 {
			caption = fmt.Sprintf("%s\n\n%s", caption, strings.Join(pendingImageParts, "\n\n"))
			captionHTML = fmt.Sprintf("%s%s", ensureParagraph(captionHTML), strings.Join(pendingImagePartsHTML, ""))
		}
	}

	if len(imageParts) == 0 {
		msg.Parts = []ConvertedMessagePart{{
			ID:           msg.Parts[0].ID,
			PendingMedia: pendingMedia,
			Content: &event.MessageEventContent{
				MsgType:       event.MsgText,
				Body:          caption,
				Format:        event.FormatHTML,
				FormattedBody: captionHTML,
			},
		}}
	} else {
		msg.Parts = []ConvertedMessagePart{{
			ID:           msg.Parts[0].ID,
			PendingMedia: pendingMedia,
			Content: &event.MessageEventContent{
				MsgType: event.MsgBeeperGallery,
				Body:    "Sent a gallery",

				BeeperGalleryImages:      imageParts,
				BeeperGalleryCaption:     caption,
				BeeperGalleryCaptionHTML: captionHTML,
			},
		}}
	}
}

func ensureParagraph(html string) string {
	if !strings.HasPrefix(html, "<p>") {
		return fmt.Sprintf("<p>%s</p>", html)
	}
	return html
}

func (msg *ConvertedMessage) MergeCaption() {
	if len(msg.Parts) != 2 {
		return
	}

	var textPart, filePart ConvertedMessagePart
	if msg.Parts[0].Content.MsgType == event.MsgText {
		textPart = msg.Parts[0]
		filePart = msg.Parts[1]
	} else {
		textPart = msg.Parts[1]
		filePart = msg.Parts[0]
	}

	if textPart.Content.MsgType != event.MsgText {
		return
	}
	switch filePart.Content.MsgType {
	case event.MsgImage, event.MsgVideo, event.MsgAudio, event.MsgFile:
		filePart.Content.FileName = filePart.Content.Body
		filePart.Content.Body = textPart.Content.Body
		filePart.Content.Format = textPart.Content.Format
		filePart.Content.FormattedBody = textPart.Content.FormattedBody
	case event.MsgNotice: // If it's a notice, the media failed or is pending
		if textPart.Content.Format == event.FormatHTML {
			filePart.Content.Format = event.FormatHTML
			filePart.Content.FormattedBody = fmt.Sprintf("<p>%s</p>%s", event.TextToHTML(filePart.Content.Body), ensureParagraph(textPart.Content.FormattedBody))
		}
		filePart.Content.Body = fmt.Sprintf("%s\n\n%s", filePart.Content.Body, textPart.Content.Body)
		filePart.Content.MsgType = event.MsgText
	default:
		return
	}
	msg.Parts = []ConvertedMessagePart{filePart}
}

func (portal *Portal) convertGoogleMedia(ctx context.Context, source *User, intent *appservice.IntentAPI, msg *gmproto.MediaContent) (*event.MessageEventContent, map[string]any, error) {
	var data []byte
	var err error
	if msg.MediaID != "" {
		data, err = source.Client.DownloadMedia(msg.MediaID, msg.DecryptionKey)
	} else if msg.MediaID2 != "" {
		data, err = source.Client.DownloadMedia(msg.MediaID2, msg.DecryptionKey2)
	} else {
		err = fmt.Errorf("no media ID found")
	}
	if err != nil {
		return nil, nil, err
	}
	mime := libgm.FormatToMediaType[msg.GetFormat()].Format
	if mime == "" {
		mime = mimetype.Detect(data).String()
	}
	fileName := msg.MediaName
	extra := make(map[string]any)
	msgtype := event.MsgFile
	switch strings.Split(mime, "/")[0] {
	case "image":
		msgtype = event.MsgImage
	case "video":
		msgtype = event.MsgVideo
		// TODO convert weird formats to mp4
	case "audio":
		msgtype = event.MsgAudio
		if mime != "audio/ogg" {
			data, err = ffmpeg.ConvertBytes(ctx, data, ".ogg", []string{}, []string{"-c:a", "libopus"}, mime)
			if err != nil {
				return nil, nil, fmt.Errorf("%w (%s to ogg): %w", errMediaConvertFailed, mime, err)
			}
			fileName += ".ogg"
			mime = "audio/ogg"
		}
		extra["org.matrix.msc3245.voice"] = map[string]any{}
	}
	content := &event.MessageEventContent{
		MsgType: msgtype,
		Body:    fileName,
		Info: &event.FileInfo{
			MimeType: mime,
			Size:     len(data),
		},
	}
	return content, extra, portal.uploadMedia(ctx, intent, data, content)
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

func (portal *Portal) markHandled(cm *ConvertedMessage, eventID id.EventID, mediaParts map[string]database.MediaPart, recent bool) *database.Message {
	msg := portal.bridge.DB.Message.New()
	msg.Chat = portal.Key
	msg.RoomID = portal.MXID
	msg.ID = cm.ID
	msg.MXID = eventID
	msg.Timestamp = cm.Timestamp
	msg.Sender = cm.SenderID
	msg.Status.Type = cm.Status
	msg.Status.PartCount = cm.PartCount
	msg.Status.MediaStatus = cm.MediaStatus
	msg.Status.MediaParts = mediaParts
	err := msg.Insert(context.TODO())
	if err != nil {
		portal.zlog.Err(err).Str("message_id", cm.ID).Msg("Failed to insert message to database")
	}

	if recent {
		portal.recentlyHandledLock.Lock()
		index := portal.recentlyHandledIndex
		portal.recentlyHandledIndex = (portal.recentlyHandledIndex + 1) % recentlyHandledLength
		portal.recentlyHandledLock.Unlock()
		portal.recentlyHandled[index] = cm.ID
	}
	return msg
}

func (portal *Portal) SyncParticipants(ctx context.Context, source *User, metadata *gmproto.Conversation) (userIDs []id.UserID, changed bool) {
	filteredParticipants := make([]*gmproto.Participant, 0, len(metadata.Participants))
	for _, participant := range metadata.Participants {
		if participant.IsMe {
			err := source.AddSelfParticipantID(ctx, participant.ID.ParticipantID)
			if err != nil {
				portal.zlog.Warn().Err(err).
					Str("participant_id", participant.ID.ParticipantID).
					Msg("Failed to save self participant ID")
			}
			continue
		} else if participant.ID.Number == "" {
			portal.zlog.Warn().Any("participant", participant).Msg("No number found in non-self participant entry")
			continue
		} else if !participant.IsVisible {
			portal.zlog.Debug().Any("participant", participant).Msg("Ignoring fake participant")
			continue
		}
		filteredParticipants = append(filteredParticipants, participant)
	}
	for _, participant := range filteredParticipants {
		puppet := source.GetPuppetByID(participant.ID.ParticipantID, participant.ID.Number)
		if puppet == nil {
			portal.zlog.Error().Any("participant_id", participant.ID).Msg("Failed to get puppet for participant")
			continue
		}
		userIDs = append(userIDs, puppet.MXID)
		puppet.Sync(ctx, source, participant)
		if portal.MXID != "" {
			err := puppet.IntentFor(portal).EnsureJoined(ctx, portal.MXID)
			if err != nil {
				portal.zlog.Err(err).
					Str("user_id", puppet.MXID.String()).
					Msg("Failed to ensure ghost is joined to portal")
			}
		}
	}
	if !metadata.IsGroupChat && len(filteredParticipants) == 1 && portal.OtherUserID != filteredParticipants[0].ID.ParticipantID {
		portal.zlog.Info().
			Str("old_other_user_id", portal.OtherUserID).
			Str("new_other_user_id", filteredParticipants[0].ID.ParticipantID).
			Msg("Found other user ID in DM")
		portal.OtherUserID = filteredParticipants[0].ID.ParticipantID
		portal.bridge.portalsLock.Lock()
		portal.bridge.portalsByOtherUser[database.Key{
			ID:       portal.OtherUserID,
			Receiver: portal.Receiver,
		}] = portal
		portal.bridge.portalsLock.Unlock()
		changed = true
	}
	if !metadata.IsGroupChat && portal.OtherUserID == "" {
		portal.zlog.Warn().Msg("No other user ID found in DM")
	}
	if portal.MXID != "" {
		members, err := portal.MainIntent().JoinedMembers(ctx, portal.MXID)
		if err != nil {
			portal.zlog.Warn().Err(err).Msg("Failed to get joined members")
		} else {
			delete(members.Joined, portal.bridge.Bot.UserID)
			delete(members.Joined, source.MXID)
			for _, userID := range userIDs {
				delete(members.Joined, userID)
			}
			for userID := range members.Joined {
				_, err = portal.MainIntent().KickUser(ctx, portal.MXID, &mautrix.ReqKickUser{
					UserID: userID,
					Reason: "User is not participating in chat",
				})
				if errors.Is(err, mautrix.MForbidden) && portal.MainIntent() != portal.bridge.Bot {
					_, err = portal.bridge.Bot.KickUser(ctx, portal.MXID, &mautrix.ReqKickUser{
						UserID: userID,
						Reason: "User is not participating in chat",
					})
				}
				if err != nil {
					portal.zlog.Warn().Err(err).
						Str("user_id", userID.String()).
						Msg("Failed to kick extra user from portal")
				} else {
					portal.zlog.Debug().
						Str("user_id", userID.String()).
						Msg("Kicked extra user from portal")
				}
			}
		}
	}
	return userIDs, changed
}

func (portal *Portal) UpdateName(ctx context.Context, name string, updateInfo bool) bool {
	if portal.Name != name || (!portal.NameSet && len(portal.MXID) > 0 && portal.shouldSetDMRoomMetadata()) {
		portal.zlog.Debug().Str("old_name", portal.Name).Str("new_name", name).Msg("Updating name")
		portal.Name = name
		portal.NameSet = false
		if updateInfo {
			defer func() {
				err := portal.Update(ctx)
				if err != nil {
					portal.zlog.Err(err).Msg("Failed to save portal after updating name")
				}
			}()
		}

		if len(portal.MXID) > 0 && !portal.shouldSetDMRoomMetadata() {
			portal.UpdateBridgeInfo(ctx)
		} else if len(portal.MXID) > 0 {
			intent := portal.MainIntent()
			_, err := intent.SetRoomName(ctx, portal.MXID, name)
			if errors.Is(err, mautrix.MForbidden) && intent != portal.MainIntent() {
				_, err = portal.MainIntent().SetRoomName(ctx, portal.MXID, name)
			}
			if err == nil {
				portal.NameSet = true
				if updateInfo {
					portal.UpdateBridgeInfo(ctx)
				}
				return true
			} else {
				portal.zlog.Warn().Err(err).Msg("Failed to set room name")
			}
		}
	}
	return false
}

func (portal *Portal) UpdateMetadata(ctx context.Context, user *User, info *gmproto.Conversation) []id.UserID {
	participants, update := portal.SyncParticipants(ctx, user, info)
	if portal.Type != info.Type {
		portal.zlog.Debug().
			Str("old_type", portal.Type.String()).
			Str("new_type", info.Type.String()).
			Msg("Conversation type changed")
		portal.Type = info.Type
		update = true
	}
	if portal.OutgoingID != info.DefaultOutgoingID {
		portal.zlog.Debug().
			Str("old_id", portal.OutgoingID).
			Str("new_id", info.DefaultOutgoingID).
			Msg("Default outgoing participant ID changed")
		portal.OutgoingID = info.DefaultOutgoingID
		update = true
	}
	if portal.MXID != "" {
		update = portal.addToPersonalSpace(ctx, user, false) || update
	}
	if portal.shouldSetDMRoomMetadata() {
		update = portal.UpdateName(ctx, info.Name, false) || update
	}
	if portal.MXID != "" {
		pls, err := portal.MainIntent().PowerLevels(ctx, portal.MXID)
		if err != nil {
			portal.zlog.Warn().Err(err).Msg("Failed to get power levels")
		} else if portal.updatePowerLevels(info, pls) {
			resp, err := portal.MainIntent().SetPowerLevels(ctx, portal.MXID, pls)
			if errors.Is(err, mautrix.MForbidden) && portal.MainIntent() != portal.bridge.Bot {
				resp, err = portal.bridge.Bot.SetPowerLevels(ctx, portal.MXID, pls)
			}
			if err != nil {
				portal.zlog.Warn().Err(err).Msg("Failed to update power levels")
			} else {
				portal.zlog.Debug().Str("event_id", resp.EventID.String()).Msg("Updated power levels")
			}
		}
	}

	// TODO avatar
	if update {
		err := portal.Update(ctx)
		if err != nil {
			portal.zlog.Err(err).Msg("Failed to save portal after updating metadata")
		}
		if portal.MXID != "" {
			portal.UpdateBridgeInfo(ctx)
		}
	}
	return participants
}

func (portal *Portal) ensureUserInvited(ctx context.Context, user *User) bool {
	return user.ensureInvited(ctx, portal.MainIntent(), portal.MXID, portal.IsPrivateChat())
}

func (portal *Portal) GetBasePowerLevels() *event.PowerLevelsEventContent {
	anyone := 0
	nope := 99
	return &event.PowerLevelsEventContent{
		UsersDefault:    anyone,
		EventsDefault:   anyone,
		RedactPtr:       &anyone,
		StateDefaultPtr: &nope,
		BanPtr:          &nope,
		KickPtr:         &nope,
		InvitePtr:       &nope,
		Users: map[id.UserID]int{
			portal.MainIntent().UserID: 100,
			portal.bridge.Bot.UserID:   100,
		},
		Events: map[string]int{
			event.StateRoomName.Type:   anyone,
			event.StateRoomAvatar.Type: anyone,
			event.EventReaction.Type:   anyone,
			event.EventRedaction.Type:  anyone,
		},
	}
}

func (portal *Portal) updatePowerLevels(conv *gmproto.Conversation, pl *event.PowerLevelsEventContent) bool {
	expectedEventsDefault := 0
	if conv.GetReadOnly() {
		expectedEventsDefault = 99
	}

	changed := false
	if pl.EventsDefault != expectedEventsDefault {
		pl.EventsDefault = expectedEventsDefault
		changed = true
	}
	changed = pl.EnsureEventLevel(event.EventReaction, expectedEventsDefault) || changed
	// Explicitly set m.room.redaction level to 0 so redactions work even if sending is disabled
	changed = pl.EnsureEventLevel(event.EventRedaction, 0) || changed
	changed = pl.EnsureUserLevel(portal.MainIntent().UserID, 100) || changed
	changed = pl.EnsureUserLevel(portal.bridge.Bot.UserID, 100) || changed
	return changed
}

func (portal *Portal) getBridgeInfoStateKey() string {
	return fmt.Sprintf("fi.mau.gmessages://gmessages/%s", portal.ID)
}

func (portal *Portal) getBridgeInfo() (string, event.BridgeEventContent) {
	content := event.BridgeEventContent{
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
		},
	}
	if portal.Type == gmproto.ConversationType_SMS {
		content.Protocol.ID = "gmessages-sms"
		content.Protocol.DisplayName = "Google Messages (SMS)"
	} else if portal.Type == gmproto.ConversationType_RCS {
		content.Protocol.ID = "gmessages-rcs"
		content.Protocol.DisplayName = "Google Messages (RCS)"
	}
	return portal.getBridgeInfoStateKey(), content
}

func (portal *Portal) UpdateBridgeInfo(ctx context.Context) {
	if len(portal.MXID) == 0 {
		portal.zlog.Debug().Msg("Not updating bridge info: no Matrix room created")
		return
	}
	portal.zlog.Debug().Msg("Updating bridge info...")
	stateKey, content := portal.getBridgeInfo()
	_, err := portal.MainIntent().SendStateEvent(ctx, portal.MXID, event.StateBridge, stateKey, content)
	if err != nil {
		portal.zlog.Warn().Err(err).Msg("Failed to update m.bridge")
	}
	// TODO remove this once https://github.com/matrix-org/matrix-doc/pull/2346 is in spec
	_, err = portal.MainIntent().SendStateEvent(ctx, portal.MXID, event.StateHalfShotBridge, stateKey, content)
	if err != nil {
		portal.zlog.Warn().Err(err).Msg("Failed to update uk.half-shot.bridge")
	}
}

func (portal *Portal) shouldSetDMRoomMetadata() bool {
	return !portal.IsPrivateChat() ||
		portal.bridge.Config.Bridge.PrivateChatPortalMeta == "always" ||
		((portal.IsEncrypted() || (portal.MXID == "" && portal.bridge.Config.Bridge.Encryption.Default)) &&
			portal.bridge.Config.Bridge.PrivateChatPortalMeta != "never")
}

func (portal *Portal) GetEncryptionEventContent() (evt *event.EncryptionEventContent) {
	evt = &event.EncryptionEventContent{Algorithm: id.AlgorithmMegolmV1}
	if rot := portal.bridge.Config.Bridge.Encryption.Rotation; rot.EnableCustom {
		evt.RotationPeriodMillis = rot.Milliseconds
		evt.RotationPeriodMessages = rot.Messages
	}
	return
}

func (portal *Portal) CreateMatrixRoom(ctx context.Context, user *User, conv *gmproto.Conversation, isFromSync bool) error {
	portal.roomCreateLock.Lock()
	defer portal.roomCreateLock.Unlock()
	if len(portal.MXID) > 0 {
		return nil
	}

	var err error
	if conv == nil {
		portal.zlog.Debug().Msg("CreateMatrixRoom called without conversation info, requesting from phone")
		conv, err = user.Client.GetConversation(portal.ID)
		if err != nil {
			return fmt.Errorf("failed to get conversation info: %w", err)
		}
	}

	members := portal.UpdateMetadata(ctx, user, conv)
	var avatarURL id.ContentURI

	if portal.IsPrivateChat() {
		puppet := portal.GetDMPuppet()
		if puppet == nil {
			portal.zlog.Error().Msg("Didn't find ghost of other user in DM :(")
			return fmt.Errorf("ghost not found")
		}
		if portal.shouldSetDMRoomMetadata() {
			avatarURL = puppet.AvatarMXC
		}
	}

	intent := portal.MainIntent()
	if err = intent.EnsureRegistered(ctx); err != nil {
		return err
	}

	portal.zlog.Info().Msg("Creating Matrix room")

	bridgeInfoStateKey, bridgeInfo := portal.getBridgeInfo()

	pl := portal.GetBasePowerLevels()
	portal.updatePowerLevels(conv, pl)

	initialState := []*event.Event{{
		Type:    event.StatePowerLevels,
		Content: event.Content{Parsed: pl},
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
	if !avatarURL.IsEmpty() {
		initialState = append(initialState, &event.Event{
			Type: event.StateRoomAvatar,
			Content: event.Content{
				Parsed: &event.RoomAvatarEventContent{URL: avatarURL},
			},
		})
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
	resp, err := intent.CreateRoom(ctx, req)
	if err != nil {
		return err
	}
	portal.zlog.Info().Str("new_room_id", resp.RoomID.String()).Msg("Matrix room created")
	portal.InSpace = false
	portal.NameSet = len(req.Name) > 0
	portal.forwardBackfillLock.Lock()
	portal.MXID = resp.RoomID
	portal.updateLogger()
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
		// TODO handle errors
		portal.bridge.StateStore.SetMembership(ctx, portal.MXID, userID, inviteMembership)
	}

	if !autoJoinInvites {
		if !portal.IsPrivateChat() {
			portal.SyncParticipants(ctx, user, conv)
		} else {
			if portal.bridge.Config.Bridge.Encryption.Default {
				err = portal.bridge.Bot.EnsureJoined(ctx, portal.MXID)
				if err != nil {
					portal.zlog.Err(err).Msg("Failed to join created portal with bridge bot for e2be")
				}
			}

			user.UpdateDirectChats(ctx, map[id.UserID][]id.RoomID{portal.GetDMPuppet().MXID: {portal.MXID}})
		}
		portal.ensureUserInvited(ctx, user)
	}
	user.syncChatDoublePuppetDetails(ctx, portal, conv, true)
	allowNotify := !isFromSync
	go portal.initialForwardBackfill(user, !conv.GetUnread(), allowNotify)
	go portal.addToPersonalSpace(context.TODO(), user, true)
	return nil
}

func (portal *Portal) addToPersonalSpace(ctx context.Context, user *User, updateInfo bool) bool {
	spaceID := user.GetSpaceRoom(ctx)
	if len(spaceID) == 0 || portal.InSpace {
		return false
	}
	_, err := portal.bridge.Bot.SendStateEvent(ctx, spaceID, event.StateSpaceChild, portal.MXID.String(), &event.SpaceChildEventContent{
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
		err = portal.Update(ctx)
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

func (portal *Portal) sendMainIntentMessage(ctx context.Context, content *event.MessageEventContent) (*mautrix.RespSendEvent, error) {
	return portal.sendMessage(ctx, portal.MainIntent(), event.EventMessage, content, nil, 0)
}

func (portal *Portal) encrypt(ctx context.Context, intent *appservice.IntentAPI, content *event.Content, eventType event.Type) (event.Type, error) {
	if !portal.Encrypted || portal.bridge.Crypto == nil {
		return eventType, nil
	}
	intent.AddDoublePuppetValue(content)
	// TODO maybe the locking should be inside mautrix-go?
	portal.encryptLock.Lock()
	defer portal.encryptLock.Unlock()
	err := portal.bridge.Crypto.Encrypt(ctx, portal.MXID, eventType, content)
	if err != nil {
		return eventType, fmt.Errorf("failed to encrypt event: %w", err)
	}
	return event.EventEncrypted, nil
}

func (portal *Portal) sendMessage(ctx context.Context, intent *appservice.IntentAPI, eventType event.Type, content *event.MessageEventContent, extraContent map[string]interface{}, timestamp int64) (*mautrix.RespSendEvent, error) {
	wrappedContent := event.Content{Parsed: content, Raw: extraContent}
	var err error
	eventType, err = portal.encrypt(ctx, intent, &wrappedContent, eventType)
	if err != nil {
		return nil, err
	}

	_, _ = intent.UserTyping(ctx, portal.MXID, false, 0)
	if timestamp == 0 {
		return intent.SendMessageEvent(ctx, portal.MXID, eventType, &wrappedContent)
	} else {
		return intent.SendMassagedMessageEvent(ctx, portal.MXID, eventType, &wrappedContent, timestamp)
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

func (portal *Portal) uploadMedia(ctx context.Context, intent *appservice.IntentAPI, data []byte, content *event.MessageEventContent) error {
	uploadMimeType, file := portal.encryptFileInPlace(data, content.Info.MimeType)

	req := mautrix.ReqUploadMedia{
		ContentBytes: data,
		ContentType:  uploadMimeType,
	}
	var mxc id.ContentURI
	if portal.bridge.Config.Homeserver.AsyncMedia {
		uploaded, err := intent.UploadAsync(ctx, req)
		if err != nil {
			return err
		}
		mxc = uploaded.ContentURI
	} else {
		uploaded, err := intent.UploadMedia(ctx, req)
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

func (portal *Portal) convertMatrixMessage(ctx context.Context, sender *User, content *event.MessageEventContent, raw map[string]any, txnID string) (*gmproto.SendMessageRequest, error) {
	log := zerolog.Ctx(ctx)
	req := &gmproto.SendMessageRequest{
		ConversationID: portal.ID,
		TmpID:          txnID,
		MessagePayload: &gmproto.MessagePayload{
			ConversationID: portal.ID,
			TmpID:          txnID,
			TmpID2:         txnID,
			ParticipantID:  portal.OutgoingID,
		},
		SIMPayload: sender.GetSIM(portal.OutgoingID).GetSIMData().GetSIMPayload(),
	}

	replyToMXID := content.RelatesTo.GetReplyTo()
	if replyToMXID != "" {
		replyToMsg, err := portal.bridge.DB.Message.GetByMXID(ctx, replyToMXID)
		if err != nil {
			log.Err(err).Str("reply_to_mxid", replyToMXID.String()).Msg("Failed to get reply target message")
		} else if replyToMsg == nil {
			log.Warn().Str("reply_to_mxid", replyToMXID.String()).Msg("Reply target message not found")
		} else {
			req.IsReply = true
			req.Reply = &gmproto.ReplyPayload{MessageID: replyToMsg.ID}
		}
	}

	switch content.MsgType {
	case event.MsgText, event.MsgEmote, event.MsgNotice:
		text := content.Body
		if content.MsgType == event.MsgEmote {
			text = "/me " + text
		}
		req.MessagePayload.MessageInfo = []*gmproto.MessageInfo{{
			Data: &gmproto.MessageInfo_MessageContent{MessageContent: &gmproto.MessageContent{
				Content: text,
			}},
		}}
	case event.MsgImage, event.MsgVideo, event.MsgAudio, event.MsgFile:
		resp, err := portal.reuploadMedia(ctx, sender, content, raw)
		if err != nil {
			return nil, err
		}
		req.MessagePayload.MessageInfo = []*gmproto.MessageInfo{{
			Data: &gmproto.MessageInfo_MediaContent{MediaContent: resp},
		}}
	case event.MsgBeeperGallery:
		for i, part := range content.BeeperGalleryImages {
			convertedPart, err := portal.reuploadMedia(ctx, sender, part, nil)
			if err != nil {
				return nil, fmt.Errorf("failed to reupload gallery image #%d: %w", i+1, err)
			}
			req.MessagePayload.MessageInfo = append(req.MessagePayload.MessageInfo, &gmproto.MessageInfo{
				Data: &gmproto.MessageInfo_MediaContent{MediaContent: convertedPart},
			})
		}
		if content.BeeperGalleryCaption != "" {
			req.MessagePayload.MessageInfo = append(req.MessagePayload.MessageInfo, &gmproto.MessageInfo{
				Data: &gmproto.MessageInfo_MessageContent{MessageContent: &gmproto.MessageContent{
					Content: content.BeeperGalleryCaption,
				}},
			})
		}
	default:
		return nil, fmt.Errorf("%w %s", errUnknownMsgType, content.MsgType)
	}
	return req, nil
}

func (portal *Portal) reuploadMedia(ctx context.Context, sender *User, content *event.MessageEventContent, raw map[string]any) (*gmproto.MediaContent, error) {
	var url id.ContentURI
	if content.File != nil {
		url = content.File.URL.ParseOrIgnore()
	} else {
		url = content.URL.ParseOrIgnore()
	}
	if url.IsEmpty() {
		return nil, errMissingMediaURL
	}
	data, err := portal.MainIntent().DownloadBytes(ctx, url)
	if err != nil {
		return nil, exerrors.NewDualError(errMediaDownloadFailed, err)
	}
	if content.File != nil {
		err = content.File.DecryptInPlace(data)
		if err != nil {
			return nil, exerrors.NewDualError(errMediaDecryptFailed, err)
		}
	}
	if content.Info.MimeType == "" {
		content.Info.MimeType = mimetype.Detect(data).String()
	}
	fileName := content.Body
	if content.FileName != "" {
		fileName = content.FileName
	}
	_, isVoice := raw["org.matrix.msc3245.voice"]
	if isVoice {
		data, err = ffmpeg.ConvertBytes(ctx, data, ".m4a", []string{}, []string{"-c:a", "aac"}, content.Info.MimeType)
		if err != nil {
			return nil, fmt.Errorf("%w (ogg to m4a): %w", errMediaConvertFailed, err)
		}
		fileName += ".m4a"
		content.Info.MimeType = "audio/mp4"
	}
	resp, err := sender.Client.UploadMedia(data, fileName, content.Info.MimeType)
	if err != nil {
		return nil, exerrors.NewDualError(errMediaReuploadFailed, err)
	}
	return resp, nil
}

func (portal *Portal) HandleMatrixMessage(sender *User, evt *event.Event, timings messageTimings) {
	ms := metricSender{portal: portal, timings: &timings}

	log := portal.zlog.With().
		Str("event_id", evt.ID.String()).
		Str("action", "handle matrix message").
		Logger()
	ctx := log.WithContext(context.TODO())
	log.Debug().Dur("age", timings.totalReceive).Msg("Handling Matrix message")

	content, ok := evt.Content.Parsed.(*event.MessageEventContent)
	if !ok {
		return
	}

	txnID := util.GenerateTmpID()
	portal.outgoingMessagesLock.Lock()
	portal.outgoingMessages[txnID] = &outgoingMessage{Event: evt}
	portal.outgoingMessagesLock.Unlock()
	if evt.Type == event.EventSticker {
		content.MsgType = event.MsgImage
	}

	start := time.Now()
	req, err := portal.convertMatrixMessage(ctx, sender, content, evt.Content.Raw, txnID)
	timings.convert = time.Since(start)
	if err != nil {
		go ms.sendMessageMetrics(ctx, sender, evt, err, "Error converting", true)
		return
	}
	log.Debug().
		Str("tmp_id", req.TmpID).
		Str("participant_id", req.GetMessagePayload().GetParticipantID()).
		Msg("Sending Matrix message to Google Messages")
	start = time.Now()
	resp, err := sender.Client.SendMessage(req)
	timings.send = time.Since(start)
	if err != nil {
		go ms.sendMessageMetrics(ctx, sender, evt, err, "Error sending", true)
	} else if resp.Status != gmproto.SendMessageResponse_SUCCESS {
		go ms.sendMessageMetrics(ctx, sender, evt, fmt.Errorf("response status %d", resp.Status), "Error sending", true)
	} else {
		go ms.sendMessageMetrics(ctx, sender, evt, nil, "", true)
	}
}

func (portal *Portal) HandleMatrixReadReceipt(brUser bridge.User, eventID id.EventID, receipt event.ReadReceipt) {
	user := brUser.(*User)
	log := portal.zlog.With().
		Str("event_id", eventID.String()).
		Time("receipt_ts", receipt.Timestamp).
		Str("action", "handle matrix read receipt").
		Logger()
	if user.Client == nil {
		log.Debug().Msg("User is not connected, ignoring read receipt")
		return
	}
	ctx := log.WithContext(context.TODO())
	log.Debug().Msg("Handling Matrix read receipt")
	targetMessage, err := portal.bridge.DB.Message.GetByMXID(ctx, eventID)
	if err != nil {
		log.Err(err).Msg("Failed to get target message to handle read receipt")
		return
	} else if targetMessage == nil {
		lastMessage, err := portal.bridge.DB.Message.GetLastInChatWithMXID(ctx, portal.Key)
		if err != nil {
			log.Err(err).Msg("Failed to get last message to handle read receipt")
			return
		} else if receipt.Timestamp.Before(lastMessage.Timestamp) {
			log.Debug().Msg("Ignoring read receipt for unknown message with timestamp before last message")
			return
		} else {
			log.Debug().Msg("Marking last message in chat as read for receipt targeting unknown message")
		}
		targetMessage = lastMessage
	}
	log = log.With().Str("message_id", targetMessage.ID).Logger()
	go func() {
		err = user.Client.MarkRead(portal.ID, targetMessage.ID)
		if err != nil {
			log.Err(err).Msg("Failed to mark message as read")
		} else {
			log.Debug().Msg("Marked message as read after Matrix read receipt")
		}
	}()
}

func (portal *Portal) HandleMatrixReaction(sender *User, evt *event.Event) {
	err := portal.handleMatrixReaction(sender, evt)
	go portal.sendMessageMetrics(context.TODO(), sender, evt, err, "Error sending", nil)
}

func (portal *Portal) handleMatrixReaction(sender *User, evt *event.Event) error {
	content, ok := evt.Content.Parsed.(*event.ReactionEventContent)
	if !ok {
		return fmt.Errorf("unexpected parsed content type %T", evt.Content.Parsed)
	}
	log := portal.zlog.With().
		Str("event_id", evt.ID.String()).
		Str("target_event_id", content.RelatesTo.EventID.String()).
		Str("action", "handle matrix reaction").
		Logger()
	ctx := log.WithContext(context.TODO())
	log.Debug().Msg("Handling Matrix reaction")

	msg, err := portal.bridge.DB.Message.GetByMXID(ctx, content.RelatesTo.EventID)
	if err != nil {
		log.Err(err).Msg("Failed to get reaction target event")
		return fmt.Errorf("failed to get event from database")
	} else if msg == nil {
		return errTargetNotFound
	}

	existingReaction, err := portal.bridge.DB.Reaction.GetByID(ctx, portal.Receiver, msg.ID, portal.OutgoingID)
	if err != nil {
		log.Err(err).Msg("Failed to get existing reaction")
		return fmt.Errorf("failed to get existing reaction from database")
	}

	emoji := variationselector.FullyQualify(content.RelatesTo.Key)
	action := gmproto.SendReactionRequest_ADD
	if existingReaction != nil {
		action = gmproto.SendReactionRequest_SWITCH
	}
	resp, err := sender.Client.SendReaction(&gmproto.SendReactionRequest{
		MessageID:    msg.ID,
		ReactionData: gmproto.MakeReactionData(emoji),
		Action:       action,
		SIMPayload:   sender.GetSIM(portal.OutgoingID).GetSIMData().GetSIMPayload(),
	})
	if err != nil {
		return fmt.Errorf("failed to send reaction: %w", err)
	} else if !resp.Success {
		return fmt.Errorf("got non-success response")
	}
	if existingReaction == nil {
		existingReaction = portal.bridge.DB.Reaction.New()
		existingReaction.Chat = portal.Key
		existingReaction.MessageID = msg.ID
		existingReaction.Sender = portal.OutgoingID
	} else if sender.DoublePuppetIntent != nil {
		_, err = sender.DoublePuppetIntent.RedactEvent(ctx, portal.MXID, existingReaction.MXID)
		if err != nil {
			log.Err(err).Msg("Failed to redact old reaction with double puppet after new Matrix reaction")
		}
	} else {
		_, err = portal.MainIntent().RedactEvent(ctx, portal.MXID, existingReaction.MXID)
		if err != nil {
			log.Err(err).Msg("Failed to redact old reaction with main intent after new Matrix reaction")
		}
	}
	existingReaction.Reaction = emoji
	existingReaction.MXID = evt.ID
	err = existingReaction.Insert(ctx)
	if err != nil {
		log.Err(err).Msg("Failed to save reaction from Matrix to database")
	}
	return nil
}

func (portal *Portal) HandleMatrixRedaction(sender *User, evt *event.Event) {
	err := portal.handleMatrixRedaction(sender, evt)
	go portal.sendMessageMetrics(context.TODO(), sender, evt, err, "Error sending", nil)
}

func (portal *Portal) handleMatrixMessageRedaction(ctx context.Context, sender *User, redacts id.EventID) error {
	log := zerolog.Ctx(ctx)
	msg, err := portal.bridge.DB.Message.GetByMXID(ctx, redacts)
	if err != nil {
		log.Err(err).Msg("Failed to get redaction target message")
		return fmt.Errorf("failed to get event from database")
	} else if msg == nil {
		return errTargetNotFound
	}
	resp, err := sender.Client.DeleteMessage(msg.ID)
	if err != nil {
		return fmt.Errorf("failed to send message removal: %w", err)
	} else if !resp.Success {
		return fmt.Errorf("got non-success response")
	}
	err = msg.Delete(ctx)
	if err != nil {
		log.Err(err).Msg("Failed to delete message from database after Matrix redaction")
	}
	return nil
}

func (portal *Portal) handleMatrixReactionRedaction(ctx context.Context, sender *User, redacts id.EventID) error {
	log := zerolog.Ctx(ctx)
	existingReaction, err := portal.bridge.DB.Reaction.GetByMXID(ctx, redacts)
	if err != nil {
		log.Err(err).Msg("Failed to get redaction target reaction")
		return fmt.Errorf("failed to get event from database")
	} else if existingReaction == nil {
		return errTargetNotFound
	}

	resp, err := sender.Client.SendReaction(&gmproto.SendReactionRequest{
		MessageID:    existingReaction.MessageID,
		ReactionData: gmproto.MakeReactionData(existingReaction.Reaction),
		Action:       gmproto.SendReactionRequest_REMOVE,
	})
	if err != nil {
		return fmt.Errorf("failed to send reaction removal: %w", err)
	} else if !resp.Success {
		return fmt.Errorf("got non-success response")
	}
	err = existingReaction.Delete(ctx)
	if err != nil {
		log.Err(err).Msg("Failed to remove reaction from database after Matrix redaction")
	}
	return nil
}

func (portal *Portal) handleMatrixRedaction(sender *User, evt *event.Event) error {
	log := portal.zlog.With().
		Str("event_id", evt.ID.String()).
		Str("target_event_id", evt.Redacts.String()).
		Str("action", "handle matrix redaction").
		Logger()
	ctx := log.WithContext(context.TODO())
	log.Debug().Msg("Handling Matrix redaction")

	err := portal.handleMatrixMessageRedaction(ctx, sender, evt.Redacts)
	if err == errTargetNotFound {
		err = portal.handleMatrixReactionRedaction(ctx, sender, evt.Redacts)
	}
	return err
}

func (portal *Portal) Delete(ctx context.Context) {
	err := portal.Portal.Delete(ctx)
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

func (portal *Portal) RemoveMXID(ctx context.Context) {
	portal.bridge.portalsLock.Lock()
	if len(portal.MXID) == 0 {
		portal.bridge.portalsLock.Unlock()
		return
	}
	delete(portal.bridge.portalsByMXID, portal.MXID)
	portal.MXID = ""
	portal.NameSet = false
	portal.InSpace = false
	portal.Encrypted = false
	portal.bridge.portalsLock.Unlock()
	err := portal.bridge.DB.Message.DeleteAllInChat(ctx, portal.Key)
	if err != nil {
		portal.zlog.Err(err).Msg("Failed to delete messages from database")
	}
	err = portal.Update(ctx)
	if err != nil {
		portal.zlog.Err(err).Msg("Failed to remove portal mxid from database")
	}
}

func (portal *Portal) Cleanup(ctx context.Context) {
	if len(portal.MXID) == 0 {
		return
	}
	intent := portal.bridge.Bot
	if portal.IsPrivateChat() {
		intent = portal.bridge.AS.Intent(portal.bridge.FormatPuppetMXID(database.Key{
			ID:       portal.OtherUserID,
			Receiver: portal.Receiver,
		}))
	}
	if portal.bridge.SpecVersions.Supports(mautrix.BeeperFeatureRoomYeeting) {
		err := intent.BeeperDeleteRoom(ctx, portal.MXID)
		if err != nil && !errors.Is(err, mautrix.MNotFound) {
			portal.zlog.Err(err).Msg("Failed to delete room using hungryserv yeet endpoint")
		}
		return
	}
	members, err := intent.JoinedMembers(ctx, portal.MXID)
	if err != nil {
		portal.zlog.Err(err).Msg("Failed to get portal members for cleanup")
		return
	}
	for member := range members.Joined {
		if member == intent.UserID {
			continue
		}
		if portal.bridge.IsGhost(member) {
			_, err = portal.bridge.AS.Intent(member).LeaveRoom(ctx, portal.MXID)
			if err != nil {
				portal.zlog.Err(err).Msg("Failed to leave as puppet while cleaning up portal")
			}
		} else {
			_, err = intent.KickUser(ctx, portal.MXID, &mautrix.ReqKickUser{UserID: member, Reason: "Deleting portal"})
			if err != nil {
				portal.zlog.Err(err).Msg("Failed to kick user while cleaning up portal")
			}
		}
	}
	_, err = intent.LeaveRoom(ctx, portal.MXID)
	if err != nil {
		portal.zlog.Err(err).Msg("Failed to leave with main intent while cleaning up portal")
	}
}
