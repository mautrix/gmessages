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
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gabriel-vasile/mimetype"
	"github.com/rs/zerolog"
	"go.mau.fi/util/ffmpeg"
	"golang.org/x/exp/maps"
	"maunium.net/go/mautrix/bridge/status"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/simplevent"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-gmessages/libgm"
	"go.mau.fi/mautrix-gmessages/libgm/events"
	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
)

func (gc *GMClient) handleGMEvent(rawEvt any) {
	log := gc.UserLogin.Log.With().Str("action", "handle gmessages event").Logger()
	ctx := log.WithContext(context.TODO())
	switch evt := rawEvt.(type) {
	case *events.ListenFatalError:
		go gc.invalidateSession(ctx, status.BridgeState{
			StateEvent: status.StateUnknownError,
			Error:      GMFatalError,
			Info:       map[string]any{"go_error": evt.Error.Error()},
		})
	case *events.ListenTemporaryError:
		gc.longPollingError = evt.Error
		gc.UserLogin.BridgeState.Send(status.BridgeState{
			StateEvent: status.StateTransientDisconnect,
			Error:      GMListenError,
			Info:       map[string]any{"go_error": evt.Error.Error()},
		})
		if !gc.pollErrorAlertSent {
			//go gc.sendMarkdownBridgeAlert(ctx, false, "Temporary error while listening to Google Messages: %v", evt.Error)
			gc.pollErrorAlertSent = true
		}
	case *events.ListenRecovered:
		gc.longPollingError = nil
		gc.UserLogin.BridgeState.Send(status.BridgeState{StateEvent: status.StateConnected})
		if gc.pollErrorAlertSent {
			//go gc.sendMarkdownBridgeAlert(ctx, false, "Reconnected to Google Messages")
			gc.pollErrorAlertSent = false
		}
	case *events.PhoneNotResponding:
		gc.PhoneResponding = false
		gc.UserLogin.BridgeState.Send(status.BridgeState{StateEvent: status.StateConnected})
		// TODO make this properly configurable
		if log.Trace().Enabled() && !gc.phoneNotRespondingAlertSent {
			//go gc.sendMarkdownBridgeAlert(ctx, false, "Phone is not responding")
			gc.phoneNotRespondingAlertSent = true
		}
	case *events.PhoneRespondingAgain:
		gc.PhoneResponding = true
		gc.UserLogin.BridgeState.Send(status.BridgeState{StateEvent: status.StateConnected})
		if gc.phoneNotRespondingAlertSent {
			//go gc.sendMarkdownBridgeAlert(ctx, false, "Phone is responding again")
			gc.phoneNotRespondingAlertSent = false
		}
	case *events.HackySetActiveMayFail:
		go gc.hackyResetActive()
	case *events.PingFailed:
		if errors.Is(evt.Error, events.ErrRequestedEntityNotFound) {
			go gc.invalidateSession(ctx, status.BridgeState{
				StateEvent: status.StateBadCredentials,
				Error:      GMUnpaired404,
				Info: map[string]any{
					"go_error": evt.Error.Error(),
				},
			})
		} else if evt.ErrorCount > 1 {
			gc.UserLogin.BridgeState.Send(status.BridgeState{
				StateEvent: status.StateUnknownError,
				Error:      GMPingFailed,
				Info:       map[string]any{"go_error": evt.Error.Error()},
			})
		} else {
			log.Debug().Msg("Not sending unknown error for first ping fail")
		}
	case *gmproto.RevokePairData:
		log.Info().Any("revoked_device", evt.GetRevokedDevice()).Msg("Got pair revoked event")
		go gc.invalidateSession(ctx, status.BridgeState{
			StateEvent: status.StateBadCredentials,
			Error:      GMUnpaired,
		})
		//go gc.sendMarkdownBridgeAlert(ctx, true, "Unpaired from Google Messages. Log in again to continue using the bridge.")
	case *events.GaiaLoggedOut:
		log.Info().Msg("Got gaia logout event")
		go gc.invalidateSession(ctx, status.BridgeState{
			StateEvent: status.StateBadCredentials,
			Error:      GMUnpaired,
		})
		//go gc.sendMarkdownBridgeAlert(ctx, true, "Unpaired from Google Messages. Log in again to continue using the bridge.")
	case *events.AuthTokenRefreshed:
		go func() {
			err := gc.UserLogin.Save(ctx)
			if err != nil {
				log.Err(err).Msg("Failed to update session in database")
			}
		}()
	case *gmproto.Conversation:
		gc.noDataReceivedRecently = false
		gc.lastDataReceived = time.Now()
		go gc.syncConversation(ctx, evt, "event")
	//case *gmproto.Message:
	case *libgm.WrappedMessage:
		gc.noDataReceivedRecently = false
		gc.lastDataReceived = time.Now()
		if evt.GetTimestamp() > gc.lastDataReceived.UnixMicro() {
			gc.lastDataReceived = time.UnixMicro(evt.GetTimestamp())
		}
		log.Debug().
			Str("conversation_id", evt.GetConversationID()).
			Str("participant_id", evt.GetParticipantID()).
			Str("message_id", evt.GetMessageID()).
			Str("message_status", evt.GetMessageStatus().GetStatus().String()).
			Int64("message_ts", evt.GetTimestamp()).
			Str("tmp_id", evt.GetTmpID()).
			Bool("is_old", evt.IsOld).
			Msg("Received message")
		gc.Main.br.QueueRemoteEvent(gc.UserLogin, &MessageEvent{
			WrappedMessage: evt,
			g:              gc,
		})
	case *gmproto.UserAlertEvent:
		gc.handleUserAlert(ctx, evt)
	case *gmproto.Settings:
		// Don't reset last data received until a BROWSER_ACTIVE event if there hasn't been data recently,
		// otherwise the resync won't have the right timestamp.
		if !gc.noDataReceivedRecently {
			gc.lastDataReceived = time.Now()
		}
		gc.handleSettings(ctx, evt)
	case *events.AccountChange:
		gc.handleAccountChange(ctx, evt)
	case *events.NoDataReceived:
		gc.noDataReceivedRecently = true
	case *events.PairSuccessful:
		log.Warn().Any("data", evt).Msg("Unexpected pair successful event")
	default:
		log.Trace().Any("data", evt).Type("data_type", evt).Msg("Unknown event")
	}
}

func (gc *GMClient) handleAccountChange(ctx context.Context, v *events.AccountChange) {
	log := zerolog.Ctx(ctx)
	log.Debug().
		Str("account", v.GetAccount()).
		Bool("enabled", v.GetEnabled()).
		Bool("fake", v.IsFake).
		Msg("Got account change event")
	gc.SwitchedToGoogleLogin = v.GetEnabled() || v.IsFake
	if !v.IsFake {
		if gc.SwitchedToGoogleLogin {
			//go gc.sendMarkdownBridgeAlert(ctx, true, "Switched to Google account pairing, please switch back or relogin with `login-google`.")
		} else {
			//go gc.sendMarkdownBridgeAlert(ctx, false, "Switched back to QR pairing, bridge should be reconnected")
			// Assume connection is ready now even if it wasn't before
			gc.ready = true
		}
	}
	gc.UserLogin.BridgeState.Send(status.BridgeState{StateEvent: status.StateConnected})
}

func (gc *GMClient) handleUserAlert(ctx context.Context, v *gmproto.UserAlertEvent) {
	log := zerolog.Ctx(ctx)
	log.Debug().Str("alert_type", v.GetAlertType().String()).Msg("Got user alert event")
	becameInactive := false
	// Don't reset last data received until a BROWSER_ACTIVE event if there hasn't been data recently,
	// otherwise the resync won't have the right timestamp.
	if !gc.noDataReceivedRecently {
		gc.lastDataReceived = time.Now()
	}
	switch v.GetAlertType() {
	case gmproto.AlertType_BROWSER_INACTIVE:
		gc.browserInactiveType = GMBrowserInactive
		becameInactive = true
	case gmproto.AlertType_BROWSER_ACTIVE:
		wasInactive := gc.browserInactiveType != "" || !gc.ready
		gc.pollErrorAlertSent = false
		gc.browserInactiveType = ""
		gc.ready = true
		newSessionID := gc.Client.CurrentSessionID()
		sessionIDChanged := gc.sessionID != newSessionID
		if sessionIDChanged || wasInactive || gc.noDataReceivedRecently {
			log.Debug().
				Str("old_session_id", gc.sessionID).
				Str("new_session_id", newSessionID).
				Bool("was_inactive", wasInactive).
				Bool("had_no_data_received", gc.noDataReceivedRecently).
				Time("last_data_received", gc.lastDataReceived).
				Msg("Session ID changed for browser active event, resyncing")
			gc.sessionID = newSessionID
			go gc.SyncConversations(ctx, gc.lastDataReceived, !sessionIDChanged && !wasInactive)
			//go gc.sendMarkdownBridgeAlert(ctx, false, "Connected to Google Messages")
		} else {
			log.Debug().
				Str("session_id", gc.sessionID).
				Bool("was_inactive", wasInactive).
				Bool("had_no_data_received", gc.noDataReceivedRecently).
				Time("last_data_received", gc.lastDataReceived).
				Msg("Session ID didn't change for browser active event, not resyncing")
		}
		gc.noDataReceivedRecently = false
		gc.lastDataReceived = time.Now()
	case gmproto.AlertType_BROWSER_INACTIVE_FROM_TIMEOUT:
		gc.browserInactiveType = GMBrowserInactiveTimeout
		becameInactive = true
	case gmproto.AlertType_BROWSER_INACTIVE_FROM_INACTIVITY:
		gc.browserInactiveType = GMBrowserInactiveInactivity
		becameInactive = true
	case gmproto.AlertType_MOBILE_DATA_CONNECTION:
		gc.mobileData = true
	case gmproto.AlertType_MOBILE_WIFI_CONNECTION:
		gc.mobileData = false
	case gmproto.AlertType_MOBILE_BATTERY_LOW:
		gc.batteryLow = true
		if time.Since(gc.batteryLowAlertSent) > 30*time.Minute {
			//go gc.sendMarkdownBridgeAlert(ctx, true, "Your phone's battery is low")
			gc.batteryLowAlertSent = time.Now()
		}
	case gmproto.AlertType_MOBILE_BATTERY_RESTORED:
		gc.batteryLow = false
		if !gc.batteryLowAlertSent.IsZero() {
			//go gc.sendMarkdownBridgeAlert(ctx, false, "Phone battery restored")
			gc.batteryLowAlertSent = time.Time{}
		}
	default:
		return
	}
	if becameInactive {
		if gc.Main.Config.AggressiveReconnect {
			go gc.aggressiveSetActive()
		} /* else {
			go gc.sendMarkdownBridgeAlert(ctx, true, "Google Messages was opened in another browser. Use `set-active` to reconnect the bridge.")
		}*/
	}
	gc.UserLogin.BridgeState.Send(status.BridgeState{StateEvent: status.StateConnected})
}

func (gc *GMClient) aggressiveSetActive() {
	sleepTimes := []int{5, 10, 30}
	for i := 0; i < 3; i++ {
		sleep := time.Duration(sleepTimes[i]) * time.Second
		gc.UserLogin.Log.Info().
			Int("sleep_seconds", int(sleep.Seconds())).
			Msg("Aggressively reactivating bridge session after sleep")
		time.Sleep(sleep)
		if gc.browserInactiveType == "" {
			gc.UserLogin.Log.Info().Msg("Bridge session became active on its own, not reactivating")
			return
		}
		gc.UserLogin.Log.Info().Msg("Now reactivating bridge session")
		err := gc.Client.SetActiveSession()
		if err != nil {
			gc.UserLogin.Log.Warn().Err(err).Msg("Failed to set self as active session")
		} else {
			break
		}
	}
}

func (gc *GMClient) handleSettings(ctx context.Context, settings *gmproto.Settings) {
	if settings.SIMCards == nil {
		return
	}
	log := zerolog.Ctx(ctx)
	changed := gc.Meta.SetSIMs(settings.SIMCards)
	newRCSSettings := settings.GetRCSSettings()
	if gc.Meta.Settings.RCSEnabled != newRCSSettings.GetIsEnabled() ||
		gc.Meta.Settings.ReadReceipts != newRCSSettings.GetSendReadReceipts() ||
		gc.Meta.Settings.TypingNotifications != newRCSSettings.GetShowTypingIndicators() ||
		gc.Meta.Settings.IsDefaultSMSApp != newRCSSettings.GetIsDefaultSMSApp() ||
		!gc.Meta.Settings.SettingsReceived {
		gc.Meta.Settings = UserSettings{
			SettingsReceived:    true,
			RCSEnabled:          newRCSSettings.GetIsEnabled(),
			ReadReceipts:        newRCSSettings.GetSendReadReceipts(),
			TypingNotifications: newRCSSettings.GetShowTypingIndicators(),
			IsDefaultSMSApp:     newRCSSettings.GetIsDefaultSMSApp(),
		}
		changed = true
	}
	if changed {
		err := gc.UserLogin.Save(ctx)
		if err != nil {
			log.Err(err).Msg("Failed to save SIM details")
		}
		gc.UserLogin.BridgeState.Send(status.BridgeState{StateEvent: status.StateConnected})
	}
}

func (gc *GMClient) invalidateSession(ctx context.Context, state status.BridgeState) {
	gc.Meta.Session = nil
	err := gc.UserLogin.Save(ctx)
	if err != nil {
		zerolog.Ctx(ctx).Err(err).Msg("Failed to save user login after invalidating session")
	}
	gc.Disconnect()
	gc.Client = nil
	gc.UserLogin.BridgeState.Send(state)
}

func (gc *GMClient) hackyResetActive() {
	if gc.didHackySetActive {
		return
	}
	gc.didHackySetActive = true
	gc.noDataReceivedRecently = false
	gc.lastDataReceived = time.Time{}
	time.Sleep(7 * time.Second)
	if !gc.ready && gc.PhoneResponding && gc.Client != nil {
		gc.UserLogin.Log.Warn().Msg("Client is still not ready, trying to re-set active session")
		err := gc.Client.SetActiveSession()
		if err != nil {
			gc.UserLogin.Log.Err(err).Msg("Failed to re-set active session")
		}
		time.Sleep(7 * time.Second)
		if !gc.ready && gc.PhoneResponding && gc.Client != nil {
			gc.UserLogin.Log.Warn().Msg("Client is still not ready, reconnecting")
			gc.ResetClient()
			err = gc.Connect(context.TODO())
			if err != nil {
				gc.UserLogin.Log.Err(err).Msg("Failed to reconnect after force reset")
			}
		}
	}
}

type ReactionSyncEvent struct {
	*gmproto.Message
	g *GMClient
}

var _ bridgev2.RemoteReactionSync = (*ReactionSyncEvent)(nil)

func (r *ReactionSyncEvent) GetType() bridgev2.RemoteEventType {
	return bridgev2.RemoteEventReactionSync
}

func (r *ReactionSyncEvent) GetPortalKey() networkid.PortalKey {
	return r.g.MakePortalKey(r.ConversationID)
}

func (r *ReactionSyncEvent) AddLogContext(c zerolog.Context) zerolog.Context {
	return c.
		Str("message_id", r.MessageID).
		Stringer("message_status", r.GetMessageStatus().GetStatus())
}

func (r *ReactionSyncEvent) GetSender() bridgev2.EventSender {
	return bridgev2.EventSender{}
}

func (r *ReactionSyncEvent) GetTargetMessage() networkid.MessageID {
	return r.g.MakeMessageID(r.MessageID)
}

func (r *ReactionSyncEvent) GetReactions() *bridgev2.ReactionSyncData {
	data := bridgev2.ReactionSyncData{
		Users:       make(map[networkid.UserID]*bridgev2.ReactionSyncUser),
		HasAllUsers: true,
	}
	addReaction := func(participantID, emoji string) {
		userID := r.g.MakeUserID(participantID)
		reacts, ok := data.Users[userID]
		if !ok {
			reacts = &bridgev2.ReactionSyncUser{
				HasAllReactions: true,
			}
			data.Users[userID] = reacts
		}
		reacts.Reactions = append(reacts.Reactions, &bridgev2.BackfillReaction{
			Sender: r.g.makeEventSender(participantID),
			Emoji:  emoji,
		})
	}
	for _, reaction := range r.Reactions {
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
			addReaction(participant, emoji)
		}
	}
	return &data
}

type MessageUpdateEvent struct {
	*libgm.WrappedMessage
	g             *GMClient
	bundle        []*database.Message
	chatIDChanged bool
}

var (
	_ bridgev2.RemoteEdit                  = (*MessageUpdateEvent)(nil)
	_ bridgev2.RemoteEventWithBundledParts = (*MessageUpdateEvent)(nil)
	_ bridgev2.RemoteEventWithTimestamp    = (*MessageUpdateEvent)(nil)
)

func (m *MessageUpdateEvent) GetType() bridgev2.RemoteEventType {
	return bridgev2.RemoteEventEdit
}

func (m *MessageUpdateEvent) GetPortalKey() networkid.PortalKey {
	return m.g.MakePortalKey(m.ConversationID)
}

func (m *MessageUpdateEvent) AddLogContext(c zerolog.Context) zerolog.Context {
	return c.
		Str("message_id", m.MessageID).
		Stringer("message_status", m.GetMessageStatus().GetStatus()).
		Time("message_ts", time.UnixMicro(m.Timestamp))
}

func (m *MessageUpdateEvent) GetSender() bridgev2.EventSender {
	return m.g.getEventSenderFromMessage(m.Message)
}

func (m *MessageUpdateEvent) GetTargetMessage() networkid.MessageID {
	return m.g.MakeMessageID(m.MessageID)
}

func (m *MessageUpdateEvent) GetTargetDBMessage() []*database.Message {
	return m.bundle
}

func (m *MessageUpdateEvent) GetTimestamp() time.Time {
	if m.chatIDChanged {
		return time.UnixMicro(m.Timestamp)
	}
	return time.Time{}
}

const MaxDelayForNewMessagePart = 24 * time.Hour

func (m *MessageUpdateEvent) ConvertEdit(ctx context.Context, portal *bridgev2.Portal, intent bridgev2.MatrixAPI, existing []*database.Message) (*bridgev2.ConvertedEdit, error) {
	converted := m.g.ConvertGoogleMessage(ctx, portal, intent, m.WrappedMessage, len(existing) == 1)
	if converted == nil {
		zerolog.Ctx(ctx).Warn().Msg("Didn't get converted parts for updated event")
		return nil, nil
	}
	existingPartMap := make(map[networkid.PartID]*database.Message)
	for _, msg := range existing {
		existingPartMap[msg.PartID] = msg
	}
	modifiedParts := make([]*bridgev2.ConvertedEditPart, 0, len(converted.Parts))
	newParts := converted.Parts[:0]

	prevMainMeta := existing[0].Metadata.(*MessageMetadata)
	newMainMeta := converted.Parts[0].DBMetadata.(*MessageMetadata)
	newMainMeta.MSSSent = prevMainMeta.MSSSent
	newMainMeta.MSSFailSent = prevMainMeta.MSSFailSent
	newMainMeta.MSSDeliverySent = prevMainMeta.MSSDeliverySent
	newMainMeta.ReadReceiptSent = prevMainMeta.ReadReceiptSent

	for _, part := range converted.Parts {
		if existingPart, ok := existingPartMap[part.ID]; ok {
			delete(existingPartMap, part.ID)
			editPart := part.ToEditPart(existingPart)
			if editPart.TopLevelExtra == nil {
				editPart.TopLevelExtra = make(map[string]any)
			}
			editPart.TopLevelExtra["com.beeper.dont_render_edited"] = true
			modifiedParts = append(modifiedParts, editPart)
		} else if m.chatIDChanged || time.Since(time.UnixMicro(m.Timestamp)) < MaxDelayForNewMessagePart {
			newParts = append(newParts, part)
		} else {
			zerolog.Ctx(ctx).Warn().
				Any("existing_parts", maps.Keys(existingPartMap)).
				Str("part_id", string(part.ID)).
				Msg("Dropping message part in edit as old message doesn't have enough parts to edit")
		}
	}
	converted.Parts = newParts
	if len(newParts) == 0 {
		converted = nil
	}
	return &bridgev2.ConvertedEdit{
		ModifiedParts: modifiedParts,
		DeletedParts:  maps.Values(existingPartMap),
		AddedParts:    converted,
	}, nil
}

type MessageEvent struct {
	*libgm.WrappedMessage
	g *GMClient
}

var (
	_ bridgev2.RemoteMessage                  = (*MessageEvent)(nil)
	_ bridgev2.RemoteMessageWithTransactionID = (*MessageEvent)(nil)
	_ bridgev2.RemoteMessageUpsert            = (*MessageEvent)(nil)
	_ bridgev2.RemoteMessageRemove            = (*MessageEvent)(nil)
	_ bridgev2.RemoteEventWithTimestamp       = (*MessageEvent)(nil)
)

func (m *MessageEvent) GetType() bridgev2.RemoteEventType {
	switch m.GetMessageStatus().GetStatus() {
	case gmproto.MessageStatusType_MESSAGE_DELETED:
		return bridgev2.RemoteEventMessageRemove
	default:
		return bridgev2.RemoteEventMessageUpsert
	}
}

func (m *MessageEvent) GetPortalKey() networkid.PortalKey {
	return m.g.MakePortalKey(m.ConversationID)
}

func (m *MessageEvent) AddLogContext(c zerolog.Context) zerolog.Context {
	return c.
		Str("message_id", m.MessageID).
		Stringer("message_status", m.GetMessageStatus().GetStatus()).
		Time("message_ts", m.GetTimestamp())
}

func (m *MessageEvent) GetSender() bridgev2.EventSender {
	return m.g.getEventSenderFromMessage(m.Message)
}

func (gc *GMClient) getEventSenderFromMessage(m *gmproto.Message) bridgev2.EventSender {
	status := m.GetMessageStatus().GetStatus()
	// Tombstone events should be sent by the bot
	if status >= 200 && status < 300 {
		return bridgev2.EventSender{}
	}
	return gc.makeEventSender(m.ParticipantID)
}

func (gc *GMClient) makeEventSender(participantID string) bridgev2.EventSender {
	return bridgev2.EventSender{
		IsFromMe:    participantID == "1" || gc.Meta.IsSelfParticipantID(participantID),
		Sender:      gc.MakeUserID(participantID),
		ForceDMUser: true,
	}
}

func (m *MessageEvent) GetID() networkid.MessageID {
	return m.g.MakeMessageID(m.MessageID)
}

func (m *MessageEvent) GetTransactionID() networkid.TransactionID {
	return networkid.TransactionID(m.TmpID)
}

func (m *MessageEvent) GetTargetMessage() networkid.MessageID {
	return m.GetID()
}

func (m *MessageEvent) GetTimestamp() time.Time {
	return time.UnixMicro(m.Timestamp)
}

func getTextPart(msg *gmproto.Message) (*bridgev2.ConvertedMessagePart, string) {
	content := &event.MessageEventContent{
		MsgType: event.MsgText,
	}
	textHasher := sha256.New()
	textHasher.Write([]byte(msg.GetSubject()))
	textHasher.Write([]byte{0x00})
	if msg.GetSubject() != "" {
		content.Format = event.FormatHTML
		content.Body = fmt.Sprintf("\n**%s**", msg.GetSubject())
		content.FormattedBody = fmt.Sprintf("<strong>%s</strong>", event.TextToHTML(msg.GetSubject()))
	}
	for _, part := range msg.GetMessageInfo() {
		data, ok := part.Data.(*gmproto.MessageInfo_MessageContent)
		if !ok {
			continue
		}
		textHasher.Write([]byte(data.MessageContent.GetContent()))
		textHasher.Write([]byte{0x00})
		content.Body = fmt.Sprintf("%s\n%s", content.Body, data.MessageContent.GetContent())
		if content.Format == event.FormatHTML {
			content.FormattedBody += fmt.Sprintf("<br>%s", event.TextToHTML(data.MessageContent.GetContent()))
		}
	}
	content.Body = strings.TrimPrefix(content.Body, "\n")
	var textHash string
	if len(content.Body) > 0 {
		textHash = hex.EncodeToString(textHasher.Sum(nil))
	}
	downloadStatus := downloadPendingStatusMessage(msg.GetMessageStatus().GetStatus())
	if len(downloadStatus) > 0 {
		if len(content.Body) > 0 {
			content.EnsureHasHTML()
			content.Body = fmt.Sprintf("%s\n\n%s", content.Body, downloadStatus)
			content.FormattedBody = fmt.Sprintf("<p>%s</p><p>%s</p>", content.FormattedBody, event.TextToHTML(downloadStatus))
		} else {
			content.Body = downloadStatus
		}
	}
	if len(content.Body) == 0 {
		return nil, textHash
	}
	return &bridgev2.ConvertedMessagePart{
		ID:      "",
		Type:    event.EventMessage,
		Content: content,
		Extra:   nil,
		DBMetadata: &MessageMetadata{
			Type:              msg.GetMessageStatus().GetStatus(),
			TextHash:          textHash,
			GlobalMediaStatus: downloadStatus,
			GlobalPartCount:   len(msg.MessageInfo),
		},
	}, textHash
}

func shouldIgnoreStatus(status gmproto.MessageStatusType, isDM bool) bool {
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
		return isDM
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

func idToInt(id networkid.MessageID) int {
	_, idPart := parseAnyID(string(id))
	i, err := strconv.Atoi(idPart)
	if err != nil {
		return 0
	}
	return i
}

type DBMessages []*database.Message

func (dbm DBMessages) GetMainMeta() *MessageMetadata {
	return dbm[0].Metadata.(*MessageMetadata)
}

func (dbm DBMessages) HasPendingMedia() bool {
	for _, msg := range dbm {
		if msg.Metadata.(*MessageMetadata).MediaPending {
			return true
		}
	}
	return false
}

func (dbm DBMessages) findMediaPart(actionMessageID string) *database.Message {
	for _, part := range dbm {
		if part.Metadata.(*MessageMetadata).MediaPartID == actionMessageID {
			return part
		}
	}
	return nil
}

func (dbm DBMessages) MessageWasEdited(new *gmproto.Message) (edited bool, changes *zerolog.Event) {
	changes = zerolog.Dict()
	for _, part := range new.GetMessageInfo() {
		data, ok := part.Data.(*gmproto.MessageInfo_MediaContent)
		if !ok {
			continue
		}
		subDict := zerolog.Dict()
		bestMediaID := data.MediaContent.GetMediaID()
		if bestMediaID == "" {
			bestMediaID = data.MediaContent.GetThumbnailMediaID()
		}
		subDict.Str("new_media_id", bestMediaID)
		existingPart := dbm.findMediaPart(part.GetActionMessageID())
		var didChange bool
		if existingPart == nil {
			subDict.Str("old_entry_type", "not found")
			didChange = true
		} else {
			if existingPart.PartID == "" {
				subDict.Str("old_entry_type", "merged")
			} else {
				subDict.Str("old_entry_type", "exact")
			}
			existingPartMeta := existingPart.Metadata.(*MessageMetadata)
			subDict.Str("old_media_id", existingPartMeta.MediaID)
			didChange = existingPartMeta.MediaID != bestMediaID
			subDict.Bool("did_change", didChange)
		}
		changes.Dict(part.GetActionMessageID(), subDict)
		edited = edited || didChange
	}
	_, newTextHash := getTextPart(new)
	oldTextHash := dbm.GetMainMeta().TextHash
	if oldTextHash != newTextHash {
		edited = true
		changes.
			Str("old_text_hash", oldTextHash).
			Str("new_text_hash", newTextHash)
	}
	return
}

func (m *MessageEvent) hasInProgressMedia() bool {
	for _, part := range m.MessageInfo {
		media, ok := part.GetData().(*gmproto.MessageInfo_MediaContent)
		if ok && media.MediaContent.GetMediaID() == "" && media.MediaContent.GetThumbnailMediaID() == "" {
			return true
		}
	}
	return false
}

const DeleteAndResendThreshold = 7 * 24 * time.Hour

func (m *MessageEvent) HandleExisting(ctx context.Context, portal *bridgev2.Portal, intent bridgev2.MatrixAPI, existing []*database.Message) (bridgev2.UpsertResult, error) {
	var result bridgev2.UpsertResult
	log := *zerolog.Ctx(ctx)
	dbm := DBMessages(existing)
	existingMeta := dbm.GetMainMeta()
	newStatus := m.GetMessageStatus().GetStatus()
	// Messages in different portals may have race conditions, ignore the most common case
	// (group MMS event happens in DM after group).
	if downloadStatusRank(newStatus) < downloadStatusRank(existingMeta.Type) {
		log.Debug().
			Stringer("old_status", existingMeta.Type).
			Stringer("new_status", newStatus).
			Msg("Ignoring message status change as it's a downgrade")
		return result, nil
	}
	if m.GetTimestamp().Sub(existing[0].Timestamp) > DeleteAndResendThreshold {
		err := m.g.Main.br.DB.Message.DeleteAllParts(ctx, portal.Receiver, existing[0].ID)
		log.Warn().
			AnErr("delete_error", err).
			Time("orig_timestamp", existing[0].Timestamp).
			Time("new_timestamp", m.GetTimestamp()).
			Msg("Got message update with very different timestamp, sending it as a new message")
		result.ContinueMessageHandling = true
		return result, nil
	}
	reactionSyncEvt := &ReactionSyncEvent{Message: m.Message, g: m.g}
	result.SubEvents = []bridgev2.RemoteEvent{reactionSyncEvt}
	chatIDChanged := dbm[0].Room.ID != portal.ID
	hasPendingMedia := dbm.HasPendingMedia()
	updatedMediaIsComplete := !m.hasInProgressMedia()
	messageWasEdited, changes := dbm.MessageWasEdited(m.Message)
	doMessageResync := chatIDChanged ||
		existingMeta.GlobalMediaStatus != downloadPendingStatusMessage(newStatus) ||
		(hasPendingMedia && updatedMediaIsComplete) ||
		messageWasEdited ||
		existingMeta.GlobalPartCount != len(m.MessageInfo)
	needsMSSEvent := !existingMeta.MSSSent && isSuccessfullySentStatus(newStatus)
	needsMSSFailureEvent := !existingMeta.MSSFailSent && !existingMeta.MSSSent && getFailMessage(newStatus) != ""
	needsMSSDeliveryEvent := !existingMeta.MSSDeliverySent && portal.RoomType == database.RoomTypeDM && (newStatus == gmproto.MessageStatusType_OUTGOING_DELIVERED || newStatus == gmproto.MessageStatusType_OUTGOING_DISPLAYED)
	needsReadReceipt := !existingMeta.ReadReceiptSent && portal.RoomType == database.RoomTypeDM && newStatus == gmproto.MessageStatusType_OUTGOING_DISPLAYED
	needsSomeMSSEvent := needsMSSEvent || needsMSSFailureEvent || needsMSSDeliveryEvent || needsReadReceipt
	if existingMeta.Type == newStatus && !doMessageResync && !needsSomeMSSEvent {
		logEvt := log.Debug().
			Stringer("old_status", existingMeta.Type).
			Bool("has_pending_media", hasPendingMedia).
			Bool("updated_media_is_complete", updatedMediaIsComplete).
			Dict("message_change_debug_data", changes)
		if hasPendingMedia {
			debugData := zerolog.Dict()
			for _, part := range m.MessageInfo {
				media, ok := part.GetData().(*gmproto.MessageInfo_MediaContent)
				if ok {
					debugData.Dict(
						part.GetActionMessageID(),
						zerolog.Dict().
							Str("media_id", media.MediaContent.GetMediaID()).
							Str("thumbnail_media_id", media.MediaContent.GetThumbnailMediaID()).
							Int64("size", media.MediaContent.GetSize()).
							Int64("width", media.MediaContent.GetDimensions().GetWidth()).
							Int64("height", media.MediaContent.GetDimensions().GetHeight()).
							Bool("has_key", len(media.MediaContent.GetDecryptionKey()) > 0).
							Bool("has_thumbnail_key", len(media.MediaContent.GetThumbnailDecryptionKey()) > 0).
							Bool("has_unknown_fields", len(media.MediaContent.ProtoReflect().GetUnknown()) > 0),
					)
				} else {
					debugData.Str(part.GetActionMessageID(), "not media")
				}
			}
			logEvt = logEvt.Dict("pending_media_debug_data", debugData)
		}
		logEvt.Msg("Nothing changed in message update, just syncing reactions")
		return result, nil
	}
	log.Debug().
		Str("old_status", existingMeta.Type.String()).
		Bool("has_pending_media", hasPendingMedia).
		Bool("updated_media_is_complete", updatedMediaIsComplete).
		Int("old_part_count", existingMeta.GlobalPartCount).
		Int("new_part_count", len(m.MessageInfo)).
		Dict("message_change_debug_data", changes).
		Bool("do_message_resync", doMessageResync).
		Bool("needs_some_mss_event", needsSomeMSSEvent).
		Msg("Message status changed")
	if chatIDChanged {
		log = log.With().Str("old_chat_id", string(dbm[0].Room.ID)).Logger()
		if downloadPendingStatusMessage(newStatus) != "" && portal.RoomType != database.RoomTypeDM {
			log.Debug().
				Dict("message_change_debug_data", changes).
				Msg("Ignoring chat ID change from group chat as update is a pending download")
			return bridgev2.UpsertResult{}, nil
		}
		ctx = log.WithContext(ctx)
		oldPortal, err := portal.Bridge.GetPortalByKey(ctx, dbm[0].Room)
		if err != nil {
			log.Err(err).Msg("Failed to get old portal to remove messages")
		} else if oldPortal == nil || oldPortal.MXID == "" {
			log.Warn().Msg("Old portal doesn't have a room")
		} else {
			log.Debug().
				Str("sender_id", string(dbm[0].SenderID)).
				Msg("Redacting events from old room")
			//lint:ignore SA1019 -
			oldPortal.Internal().RedactMessageParts(ctx, dbm, intent, time.Time{})
		}
	}
	if doMessageResync {
		editEvt := &MessageUpdateEvent{
			WrappedMessage: m.WrappedMessage,
			g:              m.g,
			bundle:         dbm,
			chatIDChanged:  chatIDChanged,
		}
		result.SubEvents = []bridgev2.RemoteEvent{editEvt, reactionSyncEvt}
	}

	if needsMSSEvent {
		existingMeta.MSSSent = true
		var deliveredTo []id.UserID
		if portal.RoomType == database.RoomTypeDM && portal.Metadata.(*PortalMetadata).Type == gmproto.ConversationType_RCS {
			deliveredTo = []id.UserID{}
		}
		portal.Bridge.Matrix.SendMessageStatus(ctx, &bridgev2.MessageStatus{
			Status:      event.MessageStatusSuccess,
			DeliveredTo: deliveredTo,
		}, &bridgev2.MessageStatusEventInfo{
			RoomID:  portal.MXID,
			EventID: dbm[0].MXID,
			Sender:  dbm[0].SenderMXID,
		})
	} else if needsMSSFailureEvent {
		existingMeta.MSSFailSent = true
		msgStatus := wrapStatusInError(newStatus).(bridgev2.MessageStatus)
		portal.Bridge.Matrix.SendMessageStatus(ctx, &msgStatus, &bridgev2.MessageStatusEventInfo{
			RoomID:  portal.MXID,
			EventID: dbm[0].MXID,
			Sender:  dbm[0].SenderMXID,
		})
	}
	if needsMSSDeliveryEvent {
		existingMeta.MSSDeliverySent = true
		result.SubEvents = append(result.SubEvents, &simplevent.Receipt{
			EventMeta: simplevent.EventMeta{
				Type:      bridgev2.RemoteEventDeliveryReceipt,
				PortalKey: portal.PortalKey,
				Sender:    bridgev2.EventSender{Sender: portal.OtherUserID},
			},
			Targets: []networkid.MessageID{dbm[0].ID},
		})
	}
	if needsReadReceipt {
		existingMeta.ReadReceiptSent = true
		result.SubEvents = append(result.SubEvents, &simplevent.Receipt{
			EventMeta: simplevent.EventMeta{
				Type:      bridgev2.RemoteEventReadReceipt,
				PortalKey: portal.PortalKey,
				Sender:    bridgev2.EventSender{Sender: portal.OtherUserID},
			},
			LastTarget: dbm[0].ID,
		})
	}
	result.SaveParts = true
	return result, nil
}

func (m *MessageEvent) ConvertMessage(ctx context.Context, portal *bridgev2.Portal, intent bridgev2.MatrixAPI) (*bridgev2.ConvertedMessage, error) {
	log := zerolog.Ctx(ctx)
	portalMeta := portal.Metadata.(*PortalMetadata)
	rcsStatusChanged := false
	switch m.GetMessageStatus().GetStatus() {
	case gmproto.MessageStatusType_INCOMING_AUTO_DOWNLOADING, gmproto.MessageStatusType_INCOMING_RETRYING_AUTO_DOWNLOAD:
		return nil, fmt.Errorf("%w: not handling incoming auto-downloading MMS", bridgev2.ErrIgnoringRemoteEvent)
	case gmproto.MessageStatusType_MESSAGE_STATUS_TOMBSTONE_PROTOCOL_SWITCH_RCS_TO_E2EE, gmproto.MessageStatusType_MESSAGE_STATUS_TOMBSTONE_PROTOCOL_SWITCH_TEXT_TO_E2EE:
		if !portalMeta.ForceRCS {
			portalMeta.ForceRCS = true
			rcsStatusChanged = true
		}
	case gmproto.MessageStatusType_MESSAGE_STATUS_TOMBSTONE_PROTOCOL_SWITCH_E2EE_TO_RCS, gmproto.MessageStatusType_MESSAGE_STATUS_TOMBSTONE_PROTOCOL_SWITCH_E2EE_TO_TEXT:
		if portalMeta.ForceRCS {
			portalMeta.ForceRCS = false
			rcsStatusChanged = true
		}
	}
	if rcsStatusChanged {
		err := portal.Save(ctx)
		if err != nil {
			log.Warn().Err(err).Bool("force_rcs", portalMeta.ForceRCS).Msg("Failed to update portal to set force RCS flag")
		} else {
			log.Debug().Bool("force_rcs", portalMeta.ForceRCS).Msg("Changed portal force RCS flag")
		}
	}
	if time.Since(m.GetTimestamp()) > 24*time.Hour {
		lastMessage, err := portal.Bridge.DB.Message.GetLastPartAtOrBeforeTime(ctx, portal.PortalKey, time.Now().Add(10*time.Second))
		if err != nil {
			log.Warn().Err(err).Msg("Failed to get last message to check if received old message is too old")
		} else if lastMessage != nil && lastMessage.Timestamp.After(m.GetTimestamp()) {
			if idToInt(lastMessage.ID) > idToInt(m.GetID()) {
				log.Warn().
					Str("last_message_id", string(lastMessage.ID)).
					Time("last_message_ts", lastMessage.Timestamp).
					Msg("Not handling old message even though it has higher ID than last new one")
			} else {
				log.Debug().
					Str("last_message_id", string(lastMessage.ID)).
					Time("last_message_ts", lastMessage.Timestamp).
					Msg("Not handling old message")
			}
			return nil, fmt.Errorf("%w: not handling old message", bridgev2.ErrIgnoringRemoteEvent)
		}
	}
	return m.g.ConvertGoogleMessage(ctx, portal, intent, m.WrappedMessage, true), nil
}

func (gc *GMClient) ConvertGoogleMessage(ctx context.Context, portal *bridgev2.Portal, intent bridgev2.MatrixAPI, m *libgm.WrappedMessage, allowMergeCaption bool) *bridgev2.ConvertedMessage {
	log := zerolog.Ctx(ctx)
	var cm bridgev2.ConvertedMessage
	dontBridge := shouldIgnoreStatus(m.GetMessageStatus().GetStatus(), portal.RoomType == database.RoomTypeDM)
	if m.GetReplyMessage() != nil {
		cm.ReplyTo = &networkid.MessageOptionalPartID{
			MessageID: gc.MakeMessageID(m.GetReplyMessage().GetMessageID()),
		}
	}
	textPart, _ := getTextPart(m.Message)
	if textPart != nil {
		textPart.DontBridge = dontBridge
		cm.Parts = append(cm.Parts, textPart)
	}
	isFirstPart := textPart == nil
	for _, part := range m.MessageInfo {
		data, ok := part.GetData().(*gmproto.MessageInfo_MediaContent)
		if !ok {
			continue
		}
		var content event.MessageEventContent
		dbMeta := &MessageMetadata{
			Type:        m.GetMessageStatus().GetStatus(),
			MediaPartID: part.GetActionMessageID(),
		}
		partID := networkid.PartID(part.GetActionMessageID())
		if isFirstPart {
			partID = ""
			dbMeta.GlobalPartCount = len(m.MessageInfo)
			dbMeta.GlobalMediaStatus = downloadPendingStatusMessage(m.GetMessageStatus().GetStatus())
			isFirstPart = false
		}
		if data.MediaContent.MediaID == "" && data.MediaContent.ThumbnailMediaID == "" {
			dbMeta.MediaPending = true
			content = event.MessageEventContent{
				MsgType: event.MsgNotice,
				Body:    fmt.Sprintf("Waiting for attachment %s", data.MediaContent.GetMediaName()),
			}
		} else if contentPtr, mediaID, isThumbnail, err := gc.convertGoogleMedia(ctx, portal, intent, data.MediaContent); err != nil {
			dbMeta.MediaPending = true
			dbMeta.MediaID = mediaID
			log.Err(err).Msg("Failed to copy attachment")
			content = event.MessageEventContent{
				MsgType: event.MsgNotice,
				Body:    fmt.Sprintf("Failed to transfer attachment %s", data.MediaContent.GetMediaName()),
			}
		} else {
			dbMeta.MediaID = mediaID
			log.Debug().
				Str("part_id", part.GetActionMessageID()).
				Str("media_id", mediaID).
				Bool("is_thumbnail", isThumbnail).
				Msg("Reuploaded media from Google Messages")
			if isThumbnail {
				go gc.requestFullMedia(ctx, m.MessageID, part.GetActionMessageID())
			}
			content = *contentPtr
		}
		cm.Parts = append(cm.Parts, &bridgev2.ConvertedMessagePart{
			ID:         partID,
			Type:       event.EventMessage,
			Content:    &content,
			DBMetadata: dbMeta,
			DontBridge: dontBridge,
		})
	}
	if allowMergeCaption && textPart != nil && cm.MergeCaption() {
		cm.Parts[0].ID = ""
		mergedMeta := cm.Parts[0].DBMetadata.(*MessageMetadata)
		textMeta := cm.Parts[0].DBMetadata.(*MessageMetadata)
		mergedMeta.GlobalMediaStatus = textMeta.GlobalMediaStatus
		mergedMeta.GlobalPartCount = textMeta.GlobalPartCount
		mergedMeta.TextHash = textMeta.TextHash
	}
	if m.Data != nil && base64.StdEncoding.EncodedLen(len(m.Data)) < 8192 && len(cm.Parts) > 0 {
		extra := cm.Parts[0].Extra
		if extra == nil {
			extra = make(map[string]any)
		}
		extra["fi.mau.gmessages.raw_debug_data"] = base64.StdEncoding.EncodeToString(m.Data)
		cm.Parts[0].Extra = extra
	}
	return &cm
}

func (gc *GMClient) convertGoogleMedia(ctx context.Context, portal *bridgev2.Portal, intent bridgev2.MatrixAPI, msg *gmproto.MediaContent) (content *event.MessageEventContent, mediaID string, isThumbnail bool, err error) {
	var data []byte
	if msg.MediaID != "" {
		mediaID = msg.MediaID
		data, err = gc.Client.DownloadMedia(msg.MediaID, msg.DecryptionKey)
	} else if msg.ThumbnailMediaID != "" {
		mediaID = msg.ThumbnailMediaID
		data, err = gc.Client.DownloadMedia(msg.ThumbnailMediaID, msg.ThumbnailDecryptionKey)
		isThumbnail = true
	} else {
		err = fmt.Errorf("no media ID found")
	}
	if err != nil {
		err = fmt.Errorf("%w: %w", bridgev2.ErrMediaDownloadFailed, err)
		return
	}
	content = &event.MessageEventContent{
		MsgType: event.MsgFile,
		Body:    msg.MediaName,
		Info: &event.FileInfo{
			MimeType: libgm.FormatToMediaType[msg.GetFormat()].Format,
			Size:     len(data),
		},
	}
	if content.Info.MimeType == "" {
		content.Info.MimeType = mimetype.Detect(data).String()
	}
	switch strings.Split(content.Info.MimeType, "/")[0] {
	case "image":
		content.MsgType = event.MsgImage
	case "video":
		content.MsgType = event.MsgVideo
		// TODO convert weird formats to mp4
	case "audio":
		content.MsgType = event.MsgAudio
		if content.Info.MimeType != "audio/ogg" && ffmpeg.Supported() {
			data, err = ffmpeg.ConvertBytes(ctx, data, ".ogg", []string{}, []string{"-c:a", "libopus"}, content.Info.MimeType)
			if err != nil {
				err = fmt.Errorf("%w (%s to ogg): %w", bridgev2.ErrMediaConvertFailed, content.Info.MimeType, err)
				return
			}
			content.FileName += ".ogg"
			content.Info.MimeType = "audio/ogg"
		}
		content.MSC3245Voice = &event.MSC3245Voice{}
	}
	content.URL, content.File, err = intent.UploadMedia(ctx, portal.MXID, data, content.FileName, content.Info.MimeType)
	if err != nil {
		err = fmt.Errorf("%w: %w", bridgev2.ErrMediaReuploadFailed, err)
	}
	return
}

type fullMediaRequestKey struct {
	MessageID       string
	ActionMessageID string
}

func (gc *GMClient) requestFullMedia(ctx context.Context, messageID, actionMessageID string) {
	if actionMessageID == "" {
		return
	}
	log := zerolog.Ctx(ctx)
	key := fullMediaRequestKey{MessageID: messageID, ActionMessageID: actionMessageID}
	if !gc.fullMediaRequests.Add(key) {
		log.Debug().
			Str("action", "request full size media").
			Str("message_id", messageID).
			Str("part_id", actionMessageID).
			Msg("Not re-requesting full size media")
		return
	}
	_, err := gc.Client.GetFullSizeImage(messageID, actionMessageID)
	if err != nil {
		log.Err(err).
			Str("action", "request full size media").
			Str("message_id", messageID).
			Str("part_id", actionMessageID).
			Msg("Failed to request full media")
	} else {
		log.Debug().
			Str("action", "request full size media").
			Str("message_id", messageID).
			Str("part_id", actionMessageID).
			Msg("Requested full size media")
	}
}
