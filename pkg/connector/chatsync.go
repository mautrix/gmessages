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

func (gc *GMClient) SyncConversations(ctx context.Context, lastDataReceived time.Time, minimalSync bool) (foundGap bool) {
	gc.syncingConversations.Store(true)
	defer gc.syncingConversations.Store(false)
	log := zerolog.Ctx(ctx)
	log.Info().Msg("Fetching conversation list")
	resp, err := gc.Client.ListConversations(ctx, gc.Main.Config.InitialChatSyncCount, gmproto.ListConversationsRequest_INBOX)
	if err != nil {
		log.Err(err).Msg("Failed to get conversation list")
		return
	}
	log.Info().Int("count", len(resp.GetConversations())).Msg("Syncing conversations")
	var newestMessageTS time.Time
	if !lastDataReceived.IsZero() {
		for _, conv := range resp.GetConversations() {
			lastMessageTS := time.UnixMicro(conv.GetLastMessageTimestamp())
			if lastMessageTS.After(newestMessageTS) {
				newestMessageTS = lastMessageTS
			}
			if lastMessageTS.After(lastDataReceived) {
				log.Warn().
					Time("last_message_ts", lastMessageTS).
					Time("last_data_received", lastDataReceived).
					Msg("Conversation's last message is newer than last data received time")
				minimalSync = false
				foundGap = true
			}
		}
	} else if minimalSync {
		log.Warn().Msg("Minimal sync called without last data received time")
	}
	if foundGap && newestMessageTS.After(gc.lastDataReceived) {
		// The sync below covers everything up to newestMessageTS, so advance the marker
		// to avoid re-detecting the same gap on the next stall check.
		gc.lastDataReceived = newestMessageTS
	}
	if minimalSync {
		log.Debug().Msg("Minimal sync with no recent messages, not syncing conversations")
		return
	}
	for _, conv := range resp.GetConversations() {
		gc.chatInfoCache.GetOrSet(conv.ConversationID, conv)
		gc.syncConversation(ctx, conv, "sync")
	}
	return
}

func (gc *GMClient) resyncAfterDataResume(ctx context.Context, lastDataReceived time.Time) {
	if lastDataReceived.IsZero() || gc.syncingConversations.Load() || gc.stallRecoveryRunning.Load() {
		return
	}
	zerolog.Ctx(ctx).Debug().
		Time("last_data_received", lastDataReceived).
		Msg("Data resumed after quiet period, checking for missed events")
	gc.SyncConversations(ctx, lastDataReceived, true)
}

const (
	stallRecoveryInitialDelay = 2 * time.Minute
	stallRecoveryRecheckDelay = 5 * time.Minute
)

// recoverReceiveStall is triggered when libgm reports that no data has been received
// recently. The ditto pinger has just sent a GET_UPDATES poke, so give it a moment to
// deliver any queued events, then compare the phone's actual conversation state with the
// events we've received. If the phone has newer data than the event stream delivered,
// the push subscription has gone stale even though the connection looks healthy: sync
// the gap and re-arm the subscription, escalating to a full reconnect (which gets a new
// listener session) if that isn't enough.
func (gc *GMClient) recoverReceiveStall(ctx context.Context) {
	if !gc.Main.Config.HackyStallDetect || !gc.stallRecoveryRunning.CompareAndSwap(false, true) {
		return
	}
	defer gc.stallRecoveryRunning.Store(false)
	log := gc.UserLogin.Log.With().Str("action", "receive stall recovery").Logger()
	ctx = log.WithContext(ctx)
	delay := stallRecoveryInitialDelay
	for attempt := range 3 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return
		}
		delay = stallRecoveryRecheckDelay
		cli := gc.Client
		if cli == nil || !gc.ready || !gc.PhoneResponding || gc.browserInactiveType != "" {
			return
		}
		if !gc.noDataReceivedRecently {
			log.Debug().Msg("Data started flowing again, no stall recovery needed")
			return
		}
		anchor := gc.lastDataReceived
		if anchor.IsZero() {
			return
		}
		if !gc.SyncConversations(ctx, anchor, true) {
			log.Debug().Int("attempt", attempt).Msg("No receive stall detected")
			return
		}
		switch attempt {
		case 0:
			log.Warn().Msg("Receive stall detected, re-arming phone push subscription")
			if err := cli.SetActiveSession(ctx); err != nil {
				log.Err(err).Msg("Failed to set active session")
			}
		case 1:
			log.Warn().Msg("Receive stall persisted after setting active session, reconnecting")
			if err := cli.Reconnect(); err != nil {
				log.Err(err).Msg("Failed to reconnect")
			}
		case 2:
			log.Error().Msg("Receive stall persisted after reconnecting, giving up until next data receive check")
		}
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

type GMChatResync struct {
	g    *GMClient
	Conv *gmproto.Conversation

	AllowBackfill bool
	OnlyBackfill  bool
}

var (
	_ bridgev2.RemoteChatResyncWithInfo       = (*GMChatResync)(nil)
	_ bridgev2.RemoteChatResyncBackfill       = (*GMChatResync)(nil)
	_ bridgev2.RemoteChatResyncBackfillBundle = (*GMChatResync)(nil)
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

func (evt *GMChatResync) GetBundledBackfillData() any {
	if evt.AllowBackfill {
		return nil
	}
	return &backfillBundleData{LatestMessageID: evt.Conv.LatestMessageID}
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
