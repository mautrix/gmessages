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
	"errors"
	"fmt"
	"time"

	"github.com/gabriel-vasile/mimetype"
	"github.com/rs/zerolog"
	"go.mau.fi/util/ffmpeg"
	"go.mau.fi/util/variationselector"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-gmessages/pkg/libgm"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/util"
)

var (
	_ bridgev2.ReactionHandlingNetworkAPI    = (*GMClient)(nil)
	_ bridgev2.RedactionHandlingNetworkAPI   = (*GMClient)(nil)
	_ bridgev2.ReadReceiptHandlingNetworkAPI = (*GMClient)(nil)
	_ bridgev2.TypingHandlingNetworkAPI      = (*GMClient)(nil)
	_ bridgev2.DeleteChatHandlingNetworkAPI  = (*GMClient)(nil)
)

var _ bridgev2.TransactionIDGeneratingNetwork = (*GMConnector)(nil)

func (gc *GMConnector) GenerateTransactionID(userID id.UserID, roomID id.RoomID, eventType event.Type) networkid.RawTransactionID {
	return networkid.RawTransactionID(util.GenerateTmpID())
}

func (gc *GMClient) HandleMatrixMessage(ctx context.Context, msg *bridgev2.MatrixMessage) (message *bridgev2.MatrixMessageResponse, err error) {
	if gc.Client == nil {
		return nil, bridgev2.ErrNotLoggedIn
	}
	txnID := networkid.TransactionID(util.GenerateTmpID())
	if msg.InputTransactionID != "" {
		txnID = networkid.TransactionID(msg.InputTransactionID)
	}
	req, err := gc.ConvertMatrixMessage(ctx, msg, txnID)
	if err != nil {
		return nil, err
	}
	msg.AddPendingToSave(nil, txnID, gc.handleRemoteEcho)
	zerolog.Ctx(ctx).Debug().
		Str("tmp_id", string(txnID)).
		Str("participant_id", req.GetMessagePayload().GetParticipantID()).
		Msg("Sending Matrix message to Google Messages")
	resp, err := gc.Client.SendMessage(ctx, req)
	for attempt := 0; err == nil && isTransientSendFailure(resp.Status) && attempt < len(sendRetryBackoff); attempt++ {
		zerolog.Ctx(ctx).Warn().
			Str("response_status", resp.GetStatus().String()).
			Int("attempt", attempt+1).
			Dur("retry_in", sendRetryBackoff[attempt]).
			Msg("Phone rejected message send with transient status, retrying")
		select {
		case <-ctx.Done():
			msg.RemovePending(txnID)
			return nil, ctx.Err()
		case <-time.After(sendRetryBackoff[attempt]):
		}
		resp, err = gc.Client.SendMessage(ctx, req)
	}
	if err != nil {
		if errors.Is(err, libgm.ErrPhoneNotResponding) {
			// The server accepted the message, so the phone may still send it whenever
			// it comes back online. Keep the pending entry so the remote echo can
			// resolve the original event (and correct the failure status) if that happens.
			gc.trackPendingSend(txnID, req.GetConversationID())
			return nil, bridgev2.WrapErrorInStatus(err).
				WithMessage(PhoneNotRespondingMessage).
				WithErrorReason(event.MessageStatusTooOld).
				WithSendNotice(true)
		}
		msg.RemovePending(txnID)
		return nil, err
	} else if resp.Status != gmproto.SendMessageResponse_SUCCESS {
		zerolog.Ctx(ctx).Warn().
			Str("response_status", resp.GetStatus().String()).
			Str("google_account_switch", resp.GetGoogleAccountSwitch().GetAccount()).
			Msg("Phone rejected message send")
		msg.RemovePending(txnID)
		return nil, bridgev2.WrapErrorInStatus((*responseStatusError)(resp)).
			WithIsCertain(!isTransientSendFailure(resp.Status)).WithSendNotice(true).WithErrorAsMessage()
	}
	gc.trackPendingSend(txnID, req.GetConversationID())
	return &bridgev2.MatrixMessageResponse{Pending: true}, nil
}

// sendRetryBackoff bounds the total in-bridge retry time so the portal event
// loop isn't blocked for long.
var sendRetryBackoff = []time.Duration{3 * time.Second, 8 * time.Second, 20 * time.Second}

// isTransientSendFailure reports whether the phone's rejection status has been
// observed to clear on its own (the phone rejects sends during network/RCS
// state flux and accepts identical sends minutes later).
func isTransientSendFailure(status gmproto.SendMessageResponse_Status) bool {
	switch status {
	case gmproto.SendMessageResponse_FAILURE_2, gmproto.SendMessageResponse_FAILURE_3:
		return true
	default:
		return false
	}
}

func (gc *GMClient) handleRemoteEcho(rawEvt bridgev2.RemoteMessage, dbMessage *database.Message) (saveMessage bool, statusErr error) {
	if txnEvt, ok := rawEvt.(bridgev2.RemoteMessageWithTransactionID); ok {
		gc.untrackPendingSend(txnEvt.GetTransactionID())
	}
	var meta *MessageMetadata
	switch evt := rawEvt.(type) {
	case *MessageEvent:
		_, textHash := getTextPart(evt.Message)
		meta = &MessageMetadata{
			IsOutgoing:      true,
			Type:            evt.GetMessageStatus().GetStatus(),
			TextHash:        textHash,
			GlobalPartCount: len(evt.MessageInfo),
		}
		for _, part := range evt.GetMessageInfo() {
			if part.GetMediaContent() != nil {
				meta.MediaPartID = part.GetActionMessageID()
				meta.MediaID = part.GetMediaContent().GetMediaID()
			}
		}
	case *bridgev2.BackfillMessage:
		if len(evt.ConvertedMessage.Parts) > 0 {
			if len(evt.ConvertedMessage.Parts) > 1 {
				gc.UserLogin.Log.Warn().
					Str("message_id", string(dbMessage.ID)).
					Msg("Got remote echo for pending message with multiple parts")
			}
			meta = evt.ConvertedMessage.Parts[0].DBMetadata.(*MessageMetadata)
			meta.IsOutgoing = true
		} else {
			gc.UserLogin.Log.Warn().
				Str("message_id", string(dbMessage.ID)).
				Msg("Got remote echo for pending message with no parts")
		}
	default:
		panic(fmt.Errorf("unexpected event type in remote echo handler: %T", rawEvt))
	}
	if meta == nil {
		return true, bridgev2.ErrNoStatus
	}
	if gc.Main.br.Config.OutgoingMessageReID {
		meta.OrigMXID = dbMessage.MXID
	}
	dbMessage.Metadata = meta
	// Normally the echo arrives while the message is still sending, so the send status is left
	// to handleExistingMessageUpdate once the phone confirms it. An echo that already carries a
	// sent status has no further update coming - which is the usual shape of a late echo that
	// only came back after the phone stopped throttling pushes - so send the status here
	// instead. Without this the message keeps whatever remote echo timeout error it picked up
	// while waiting, even though it was delivered.
	if meta.IsOutgoing && isSuccessfullySentStatus(meta.Type) {
		meta.MSSSent = true
		return true, nil
	}
	return true, bridgev2.ErrNoStatus
}

func (gc *GMClient) ConvertMatrixMessage(ctx context.Context, msg *bridgev2.MatrixMessage, txnID networkid.TransactionID) (*gmproto.SendMessageRequest, error) {
	portalMeta := msg.Portal.Metadata.(*PortalMetadata)
	sim := gc.GetSIM(msg.Portal)
	conversationID, err := gc.conversationIDForPortal(ctx, msg.Portal)
	if err != nil {
		return nil, err
	}
	req := &gmproto.SendMessageRequest{
		ConversationID: conversationID,
		MessagePayload: &gmproto.MessagePayload{
			TmpID:                 string(txnID),
			MessagePayloadContent: nil,
			ConversationID:        conversationID,
			ParticipantID:         portalMeta.OutgoingID,
			TmpID2:                string(txnID),
		},
		SIMPayload: sim.GetSIMData().GetSIMPayload(),
		TmpID:      string(txnID),
		ForceRCS: portalMeta.Type == gmproto.ConversationType_RCS &&
			portalMeta.SendMode == gmproto.ConversationSendMode_SEND_MODE_AUTO &&
			portalMeta.ForceRCS,
		Reply: nil,
	}
	if msg.ReplyTo != nil {
		replyToID, err := gc.ParseMessageID(msg.ReplyTo.ID)
		if err != nil {
			return nil, fmt.Errorf("%w in reply to event", err)
		}
		req.Reply = &gmproto.ReplyPayload{MessageID: replyToID}
	}
	if req.ForceRCS && !sim.GetRCSChats().GetEnabled() {
		zerolog.Ctx(ctx).Warn().Msg("Forcing RCS but RCS is disabled on sim")
	}
	switch msg.Content.MsgType {
	case event.MsgText, event.MsgEmote, event.MsgNotice:
		text := msg.Content.Body
		if msg.Content.MsgType == event.MsgEmote {
			text = "/me " + text
		}
		req.MessagePayload.MessageInfo = []*gmproto.MessageInfo{{
			Data: &gmproto.MessageInfo_MessageContent{MessageContent: &gmproto.MessageContent{
				Content: text,
			}},
		}}
	case event.MsgImage, event.MsgVideo, event.MsgAudio, event.MsgFile:
		resp, err := gc.reuploadMedia(ctx, msg.Content)
		if err != nil {
			return nil, err
		}
		req.MessagePayload.MessageInfo = []*gmproto.MessageInfo{{
			Data: &gmproto.MessageInfo_MediaContent{MediaContent: resp},
		}}
		if msg.Content.FileName != "" && msg.Content.FileName != msg.Content.Body {
			req.MessagePayload.MessageInfo = append(req.MessagePayload.MessageInfo, &gmproto.MessageInfo{
				Data: &gmproto.MessageInfo_MessageContent{MessageContent: &gmproto.MessageContent{
					Content: msg.Content.Body,
				}},
			})
		}
	default:
		return nil, fmt.Errorf("%w %s", bridgev2.ErrUnsupportedMessageType, msg.Content.MsgType)
	}
	return req, nil
}

func (gc *GMClient) reuploadMedia(ctx context.Context, content *event.MessageEventContent) (*gmproto.MediaContent, error) {
	data, err := gc.Main.br.Bot.DownloadMedia(ctx, content.URL, content.File)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", bridgev2.ErrMediaDownloadFailed, err)
	}
	if content.Info.MimeType == "" {
		content.Info.MimeType = mimetype.Detect(data).String()
	}
	fileName := content.Body
	if content.FileName != "" {
		fileName = content.FileName
	}
	if content.MsgType == event.MsgAudio && content.MSC3245Voice != nil && content.Info.MimeType != "audio/mp4" && ffmpeg.Supported() {
		data, err = ffmpeg.ConvertBytes(ctx, data, ".m4a", []string{}, []string{"-c:a", "aac"}, content.Info.MimeType)
		if err != nil {
			return nil, fmt.Errorf("%w (ogg to m4a): %w", bridgev2.ErrMediaConvertFailed, err)
		}
		fileName += ".m4a"
		content.Info.MimeType = "audio/mp4"
	}
	resp, err := gc.Client.UploadMedia(data, fileName, content.Info.MimeType)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", bridgev2.ErrMediaReuploadFailed, err)
	}
	return resp, nil
}

var ErrNonSuccessResponse = bridgev2.WrapErrorInStatus(errors.New("got non-success response")).WithErrorAsMessage().WithSendNotice(true)

func (gc *GMClient) HandleMatrixMessageRemove(ctx context.Context, msg *bridgev2.MatrixMessageRemove) error {
	if gc.Client == nil {
		return bridgev2.ErrNotLoggedIn
	}
	msgID, err := gc.ParseMessageID(msg.TargetMessage.ID)
	if err != nil {
		return err
	}
	resp, err := gc.Client.DeleteMessage(ctx, msgID)
	if err != nil {
		return err
	} else if !resp.Success {
		return ErrNonSuccessResponse
	}
	return nil
}

func (gc *GMClient) PreHandleMatrixReaction(ctx context.Context, msg *bridgev2.MatrixReaction) (bridgev2.MatrixReactionPreResponse, error) {
	return bridgev2.MatrixReactionPreResponse{
		SenderID: gc.MakeUserID(msg.Portal.Metadata.(*PortalMetadata).OutgoingID),
		Emoji:    variationselector.FullyQualify(msg.Content.RelatesTo.Key),
	}, nil
}

func (gc *GMClient) HandleMatrixReaction(ctx context.Context, msg *bridgev2.MatrixReaction) (reaction *database.Reaction, err error) {
	if gc.Client == nil {
		return nil, bridgev2.ErrNotLoggedIn
	}
	action := gmproto.SendReactionRequest_ADD
	if msg.ReactionToOverride != nil {
		action = gmproto.SendReactionRequest_SWITCH
	}
	msgID, err := gc.ParseMessageID(msg.TargetMessage.ID)
	if err != nil {
		return nil, err
	}
	resp, err := gc.Client.SendReaction(ctx, &gmproto.SendReactionRequest{
		MessageID:    msgID,
		ReactionData: gmproto.MakeReactionData(msg.PreHandleResp.Emoji),
		Action:       action,
		SIMPayload:   gc.GetSIM(msg.Portal).GetSIMData().GetSIMPayload(),
	})
	if err != nil {
		return nil, err
	} else if !resp.Success {
		return nil, ErrNonSuccessResponse
	}
	return &database.Reaction{}, nil
}

func (gc *GMClient) HandleMatrixReactionRemove(ctx context.Context, msg *bridgev2.MatrixReactionRemove) error {
	if gc.Client == nil {
		return bridgev2.ErrNotLoggedIn
	}
	msgID, err := gc.ParseMessageID(msg.TargetReaction.MessageID)
	if err != nil {
		return err
	}
	resp, err := gc.Client.SendReaction(ctx, &gmproto.SendReactionRequest{
		MessageID:    msgID,
		ReactionData: gmproto.MakeReactionData(msg.TargetReaction.Emoji),
		Action:       gmproto.SendReactionRequest_REMOVE,
	})
	if err != nil {
		return err
	} else if !resp.Success {
		return ErrNonSuccessResponse
	}
	return nil
}

func (gc *GMClient) HandleMatrixReadReceipt(ctx context.Context, msg *bridgev2.MatrixReadReceipt) error {
	if gc.Client == nil {
		return bridgev2.ErrNotLoggedIn
	}
	targetMessage := msg.ExactMessage
	if targetMessage == nil {
		var err error
		targetMessage, err = msg.Portal.Bridge.DB.Message.GetLastPartAtOrBeforeTime(ctx, msg.Portal.PortalKey, msg.ReadUpTo)
		if err != nil {
			return err
		}
	}
	if targetMessage == nil {
		return fmt.Errorf("read receipt target not found")
	}
	convID, err := gc.conversationIDForPortal(ctx, msg.Portal)
	if err != nil {
		return err
	}
	msgID, err := gc.ParseMessageID(targetMessage.ID)
	if err != nil {
		return err
	}
	return gc.Client.MarkRead(ctx, convID, msgID)
}

func (gc *GMClient) HandleMatrixTyping(ctx context.Context, msg *bridgev2.MatrixTyping) error {
	if !msg.IsTyping {
		return nil
	}
	if gc.Client == nil {
		return bridgev2.ErrNotLoggedIn
	}
	convID, err := gc.conversationIDForPortal(ctx, msg.Portal)
	if err != nil {
		return err
	}
	return gc.Client.SetTyping(ctx, convID, gc.GetSIM(msg.Portal).GetSIMData().GetSIMPayload())
}

func (gc *GMClient) HandleMatrixDeleteChat(ctx context.Context, chat *bridgev2.MatrixDeleteChat) error {
	if gc.Client == nil {
		return bridgev2.ErrNotLoggedIn
	}
	convID, err := gc.conversationIDForPortal(ctx, chat.Portal)
	if err != nil {
		return err
	}
	var phone string
	if chat.Portal.RoomType == database.RoomTypeDM {
		if chat.Portal.OtherUserID != "" {
			ghost, err := gc.Main.br.GetExistingGhostByID(ctx, chat.Portal.OtherUserID)
			if err != nil {
				return fmt.Errorf("failed to get ghost: %w", err)
			}
			if ghost != nil {
				phone = ghost.Metadata.(*GhostMetadata).Phone
			}
		}
		if phone == "" {
			// Fallback: fetch conversation from Google to get phone number
			conv, err := gc.Client.GetConversation(ctx, convID)
			if err != nil {
				return fmt.Errorf("failed to get conversation for phone number: %w", err)
			}
			if conv == nil || conv.GetStatus() == gmproto.ConversationStatus_DELETED {
				// The conversation is already gone on the phone; there's nothing to
				// delete remotely, so just let the portal be deleted.
				zerolog.Ctx(ctx).Debug().
					Str("conversation_id", convID).
					Msg("Conversation not found on phone, skipping remote delete")
				return nil
			}
			for _, pcp := range conv.Participants {
				if pcp.IsVisible && !pcp.IsMe && pcp.ID.Number != "" {
					phone = pcp.ID.Number
					break
				}
			}
		}
		if phone == "" {
			// Group chats are always deleted without a phone number, so when a DM has no
			// resolvable number, try the same instead of leaving an undeletable room.
			zerolog.Ctx(ctx).Warn().
				Str("conversation_id", convID).
				Msg("Phone number not available for conversation, attempting delete without it")
		}
	}
	if err := gc.Client.DeleteConversation(ctx, convID, phone); err != nil {
		return err
	}
	return nil
}
