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
	"context"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/simplevent"

	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

func (gc *GMClient) SyncConversations(ctx context.Context, lastDataReceived time.Time, minimalSync bool) {
	gc.syncingConversations.Store(true)
	defer gc.syncingConversations.Store(false)
	log := zerolog.Ctx(ctx)
	log.Info().Msg("Fetching conversation list")
	resp, err := gc.Client.ListConversations(gc.Main.Config.InitialChatSyncCount, gmproto.ListConversationsRequest_INBOX)
	if err != nil {
		log.Err(err).Msg("Failed to get conversation list")
		return
	}
	log.Info().Int("count", len(resp.GetConversations())).Msg("Syncing conversations")
	if !lastDataReceived.IsZero() {
		for _, conv := range resp.GetConversations() {
			lastMessageTS := time.UnixMicro(conv.GetLastMessageTimestamp())
			if lastMessageTS.After(lastDataReceived) {
				log.Warn().
					Time("last_message_ts", lastMessageTS).
					Time("last_data_received", lastDataReceived).
					Msg("Conversation's last message is newer than last data received time")
				minimalSync = false
			}
		}
	} else if minimalSync {
		log.Warn().Msg("Minimal sync called without last data received time")
	}
	if minimalSync {
		log.Debug().Msg("Minimal sync with no recent messages, not syncing conversations")
		return
	}
	for _, conv := range resp.GetConversations() {
		gc.chatInfoCache.GetOrSet(conv.ConversationID, conv)
		gc.syncConversation(ctx, conv, "sync")
	}
}

func (gc *GMClient) syncConversationMeta(v *gmproto.Conversation) (meta *conversationMeta, suspiciousUnmarkedSpam bool) {
	gc.conversationMetaLock.Lock()
	defer gc.conversationMetaLock.Unlock()
	var ok bool
	meta, ok = gc.conversationMeta[v.ConversationID]
	if !ok {
		meta = &conversationMeta{}
		gc.conversationMeta[v.ConversationID] = meta
	}
	meta.unread = v.Unread
	if !v.Unread {
		meta.readUpTo = v.LatestMessageID
		meta.readUpToTS = time.UnixMicro(v.LastMessageTimestamp)
	} else if meta.readUpTo == v.LatestMessageID {
		meta.readUpTo = ""
		meta.readUpToTS = time.Time{}
	}
	switch v.Status {
	case gmproto.ConversationStatus_SPAM_FOLDER, gmproto.ConversationStatus_BLOCKED_FOLDER:
		if meta.markedSpamAt.IsZero() {
			meta.markedSpamAt = time.Now()
		}
	case gmproto.ConversationStatus_DELETED:
		// no-op
	default:
		suspiciousUnmarkedSpam = time.Since(meta.markedSpamAt) < 1*time.Minute
	}
	return
}

func (gc *GMClient) syncConversation(ctx context.Context, v *gmproto.Conversation, source string) {
	meta, suspiciousUnmarkedSpam := gc.syncConversationMeta(v)

	log := zerolog.Ctx(ctx).With().
		Str("action", "sync conversation").
		Str("conversation_id", v.ConversationID).
		Stringer("conversation_status", v.Status).
		Str("data_source", source).
		Logger()

	convCopy := proto.Clone(v).(*gmproto.Conversation)
	convCopy.LatestMessage = nil
	if suspiciousUnmarkedSpam {
		log.Debug().Any("conversation_data", convCopy).
			Msg("Dropping conversation update due to suspected race condition with spam flag")
		return
	}
	log.Debug().Any("conversation_data", convCopy).Msg("Got conversation update")
	evt := &GMChatResync{
		g:             gc,
		Conv:          v,
		AllowBackfill: time.Since(time.UnixMicro(v.LastMessageTimestamp)) > 5*time.Minute,
	}
	var markReadEvt *simplevent.Receipt
	if !v.Unread {
		markReadEvt = &simplevent.Receipt{
			EventMeta: simplevent.EventMeta{
				Type:      bridgev2.RemoteEventReadReceipt,
				PortalKey: gc.MakePortalKey(v.ConversationID),
				Sender:    bridgev2.EventSender{IsFromMe: true},
			},
			LastTarget: gc.MakeMessageID(meta.readUpTo),
			ReadUpTo:   meta.readUpToTS,
		}
	}
	switch v.Status {
	case gmproto.ConversationStatus_ACTIVE, gmproto.ConversationStatus_ARCHIVED:
		// Dedupe portal if a DM to work around gmessages conversation IDs changing over time
		gc.deduplicateDMPortal(ctx, v)
	}
	gc.Main.br.QueueRemoteEvent(gc.UserLogin, evt)
	switch v.Status {
	case gmproto.ConversationStatus_SPAM_FOLDER, gmproto.ConversationStatus_BLOCKED_FOLDER, gmproto.ConversationStatus_DELETED:
		// Don't send read/backfill events if the chat is being deleted
		return
	}
	if !evt.AllowBackfill {
		backfillEvt := &GMChatResync{
			g:             gc,
			Conv:          v,
			AllowBackfill: true,
			OnlyBackfill:  true,
		}
		backfillCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
		cancelPrev := meta.cancelPendingBackfill.Swap(&cancel)
		if cancelPrev != nil {
			(*cancelPrev)()
		}
		log.Debug().
			Str("latest_message_id", evt.Conv.LatestMessageID).
			Msg("Delaying missed forward backfill as latest message is new")
		go func() {
			select {
			case <-time.After(15 * time.Second):
			case <-backfillCtx.Done():
				log.Debug().
					Str("latest_message_id", evt.Conv.LatestMessageID).
					Msg("Backfill was cancelled by a newer backfill")
				return
			}
			gc.Main.br.QueueRemoteEvent(gc.UserLogin, backfillEvt)
			if markReadEvt != nil {
				gc.Main.br.QueueRemoteEvent(gc.UserLogin, markReadEvt)
			}
		}()
	} else if markReadEvt != nil {
		gc.Main.br.QueueRemoteEvent(gc.UserLogin, markReadEvt)
	}
}

// soleOtherParticipantID returns the single non-self, visible participant of a DM chat
func (gc *GMClient) soleOtherParticipantID(conv *gmproto.Conversation) string {
	if conv.IsGroupChat {
		return ""
	}
	var found string
	count := 0
	for _, pcp := range conv.Participants {
		if pcp.IsMe || !pcp.IsVisible || pcp.GetID().GetParticipantID() == "" {
			continue
		}
		found = pcp.GetID().GetParticipantID()
		count++
	}
	if count != 1 {
		return ""
	}
	return found
}

// deduplicateDMPortal migrates any stale duplicate DM portal for the same contact onto the given
// conversation's portal key. This works around Google rewriting conversation IDs from time to time
// which leaves orphaned rooms.
func (gc *GMClient) deduplicateDMPortal(ctx context.Context, conv *gmproto.Conversation) {
	log := zerolog.Ctx(ctx)
	participantID := gc.soleOtherParticipantID(conv)
	if participantID == "" {
		return
	}
	newKey := gc.MakePortalKey(conv.ConversationID)
	otherUserID := gc.MakeUserID(participantID)
	portals, err := gc.Main.br.GetDMPortalsWith(ctx, otherUserID)
	if err != nil {
		log.Err(err).
			Str("participant_id", participantID).
			Str("conversation_id", conv.ConversationID).
			Msg("Failed to look up existing DM portals for re-key dedup")
		return
	}
	for _, portal := range portals {
		// Skip the conversation we're currently syncing and portals belonging to other
		// user logins (GetDMPortalsWith is not scoped by receiver).
		if portal.PortalKey == newKey || portal.Receiver != gc.UserLogin.ID {
			continue
		}
		// Recency tiebreaker: only migrate an older portal into this conversation
		oldTS := int64(0)
		if meta, ok := portal.Metadata.(*PortalMetadata); ok {
			oldTS = meta.LastMessageTS
		}
		if conv.LastMessageTimestamp != 0 && oldTS > conv.LastMessageTimestamp {
			log.Debug().
				Stringer("stale_portal_key", portal.PortalKey).
				Stringer("new_portal_key", newKey).
				Int64("stale_last_message_ts", oldTS).
				Int64("new_last_message_ts", conv.LastMessageTimestamp).
				Msg("Skipping DM portal re-key dedup: existing portal is newer than this conversation")
			continue
		}
		log.Info().
			Stringer("stale_portal_key", portal.PortalKey).
			Stringer("new_portal_key", newKey).
			Str("participant_id", participantID).
			Msg("Migrating re-keyed duplicate DM portal onto new conversation key")
		result, _, err := gc.Main.br.ReIDPortal(ctx, portal.PortalKey, newKey)
		if err != nil {
			log.Err(err).
				Stringer("stale_portal_key", portal.PortalKey).
				Stringer("new_portal_key", newKey).
				Msg("Failed to re-ID duplicate DM portal")
			continue
		}
		log.Info().
			Stringer("stale_portal_key", portal.PortalKey).
			Stringer("new_portal_key", newKey).
			Int("reid_result", int(result)).
			Msg("Migrated re-keyed duplicate DM portal")
	}
}

type GMChatResync struct {
	g    *GMClient
	Conv *gmproto.Conversation

	AllowBackfill bool
	OnlyBackfill  bool
}

var (
	_ bridgev2.RemoteChatResyncWithInfo       = (*GMChatResync)(nil)
	_ bridgev2.RemoteChatResyncBackfill       = (*GMChatResync)(nil)
	_ bridgev2.RemoteChatDelete               = (*GMChatResync)(nil)
	_ bridgev2.RemoteEventThatMayCreatePortal = (*GMChatResync)(nil)
)

func (evt *GMChatResync) GetType() bridgev2.RemoteEventType {
	switch evt.Conv.GetStatus() {
	case gmproto.ConversationStatus_SPAM_FOLDER, gmproto.ConversationStatus_BLOCKED_FOLDER, gmproto.ConversationStatus_DELETED:
		return bridgev2.RemoteEventChatDelete
	case gmproto.ConversationStatus_ACTIVE, gmproto.ConversationStatus_ARCHIVED, gmproto.ConversationStatus_KEEP_ARCHIVED:
		return bridgev2.RemoteEventChatResync
	default:
		return bridgev2.RemoteEventUnknown
	}
}

func (evt *GMChatResync) ShouldCreatePortal() bool {
	if evt.OnlyBackfill {
		return false
	}
	if evt.Conv.Participants == nil {
		return false
	}
	switch evt.Conv.GetStatus() {
	case gmproto.ConversationStatus_ACTIVE, gmproto.ConversationStatus_ARCHIVED:
		// continue to other checks
	default:
		// Don't create portal for keep_archived/spam/blocked/deleted
		return false
	}
	return true
}

func (evt *GMChatResync) GetPortalKey() networkid.PortalKey {
	return evt.g.MakePortalKey(evt.Conv.ConversationID)
}

func (evt *GMChatResync) AddLogContext(c zerolog.Context) zerolog.Context {
	return c.
		Stringer("conversation_status", evt.Conv.GetStatus()).
		Bool("backfill_only_evt", evt.OnlyBackfill)
}

func (evt *GMChatResync) GetSender() bridgev2.EventSender {
	return bridgev2.EventSender{}
}

func (evt *GMChatResync) GetChatInfo(ctx context.Context, portal *bridgev2.Portal) (*bridgev2.ChatInfo, error) {
	if evt.OnlyBackfill {
		return nil, nil
	}
	return evt.g.wrapChatInfo(ctx, evt.Conv)
}

func (evt *GMChatResync) CheckNeedsBackfill(ctx context.Context, latestMessage *database.Message) (bool, error) {
	if !evt.AllowBackfill {
		return false, nil
	}
	needsBackfill := evt.Conv.LastMessageTimestamp != 0 && (latestMessage == nil ||
		(time.UnixMicro(evt.Conv.LastMessageTimestamp).After(latestMessage.Timestamp) &&
			evt.g.MakeMessageID(evt.Conv.LatestMessageID) != latestMessage.ID))
	if !needsBackfill {
		return false, nil
	}
	logEvt := zerolog.Ctx(ctx).Debug().
		Int64("conv_last_message_ts", evt.Conv.LastMessageTimestamp).
		Str("conv_latest_message_id", evt.Conv.LatestMessageID)
	if latestMessage != nil {
		logEvt.Time("bridged_latest_message_ts", latestMessage.Timestamp).
			Str("bridged_latest_message_id", string(latestMessage.ID))
	}
	logEvt.Msg("Chat needs backfill")
	return true, nil
}

func (evt *GMChatResync) DeleteOnlyForMe() bool {
	// All portals are already scoped to the user, so there's never a case where we're deleting a portal someone else is in
	return false
}
