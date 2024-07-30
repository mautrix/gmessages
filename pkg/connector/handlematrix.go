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

	"github.com/gabriel-vasile/mimetype"
	"github.com/rs/zerolog"
	"go.mau.fi/util/ffmpeg"
	"go.mau.fi/util/variationselector"

	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
	"go.mau.fi/mautrix-gmessages/libgm/util"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"
)

var (
	_ bridgev2.ReactionHandlingNetworkAPI    = &GMClient{}
	_ bridgev2.RedactionHandlingNetworkAPI   = &GMClient{}
	_ bridgev2.ReadReceiptHandlingNetworkAPI = &GMClient{}
)

func (gc *GMClient) HandleMatrixMessage(ctx context.Context, msg *bridgev2.MatrixMessage) (message *bridgev2.MatrixMessageResponse, err error) {
	txnID := util.GenerateTmpID()
	req, err := gc.ConvertMatrixMessage(ctx, msg, txnID)
	if err != nil {
		return nil, err
	}
	zerolog.Ctx(ctx).Debug().
		Str("tmp_id", txnID).
		Str("participant_id", req.GetMessagePayload().GetParticipantID()).
		Msg("Sending Matrix message to Google Messages")
	resp, err := gc.Client.SendMessage(req)
	if err != nil {
		return nil, err
	} else if resp.Status != gmproto.SendMessageResponse_SUCCESS {
		return nil, bridgev2.WrapErrorInStatus((*responseStatusError)(resp)).
			WithIsCertain(true).WithSendNotice(true).WithErrorAsMessage()
	}
	return &bridgev2.MatrixMessageResponse{
		// TODO figure out how to get db metadata accurately
		DB:         &database.Message{},
		Pending:    networkid.TransactionID(txnID),
		HandleEcho: gc.handleRemoteEcho,
	}, nil
}

func (gc *GMClient) handleRemoteEcho(rawEvt bridgev2.RemoteMessage, dbMessage *database.Message) (saveMessage bool, statusErr error) {
	saveMessage = true
	evt := rawEvt.(*MessageEvent)
	_, textHash := getTextPart(evt.Message)
	isSuccess := isSuccessfullySentStatus(evt.GetMessageStatus().GetStatus())
	meta := &MessageMetadata{
		Type:            evt.GetMessageStatus().GetStatus(),
		TextHash:        textHash,
		GlobalPartCount: len(evt.MessageInfo),
		MSSSent:         isSuccess,
	}
	if !isSuccess {
		statusErr = wrapStatusInError(evt.GetMessageStatus().GetStatus())
		if statusErr == nil {
			statusErr = bridgev2.ErrNoStatus
		} else {
			meta.MSSFailSent = true
		}
	}
	dbMessage.Metadata = meta
	return
}

func (gc *GMClient) ConvertMatrixMessage(ctx context.Context, msg *bridgev2.MatrixMessage, txnID string) (*gmproto.SendMessageRequest, error) {
	portalMeta := msg.Portal.Metadata.(*PortalMetadata)
	sim := gc.GetSIM(msg.Portal)
	conversationID, err := gc.ParsePortalID(msg.Portal.ID)
	if err != nil {
		return nil, err
	}
	req := &gmproto.SendMessageRequest{
		ConversationID: conversationID,
		MessagePayload: &gmproto.MessagePayload{
			TmpID:                 txnID,
			MessagePayloadContent: nil,
			ConversationID:        conversationID,
			ParticipantID:         portalMeta.OutgoingID,
			TmpID2:                txnID,
		},
		SIMPayload: sim.GetSIMData().GetSIMPayload(),
		TmpID:      txnID,
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
	if content.MSC3245Voice != nil {
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
	msgID, err := gc.ParseMessageID(msg.TargetMessage.ID)
	if err != nil {
		return err
	}
	resp, err := gc.Client.DeleteMessage(msgID)
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
	action := gmproto.SendReactionRequest_ADD
	if msg.ReactionToOverride != nil {
		action = gmproto.SendReactionRequest_SWITCH
	}
	msgID, err := gc.ParseMessageID(msg.TargetMessage.ID)
	if err != nil {
		return nil, err
	}
	resp, err := gc.Client.SendReaction(&gmproto.SendReactionRequest{
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
	msgID, err := gc.ParseMessageID(msg.TargetReaction.MessageID)
	if err != nil {
		return err
	}
	resp, err := gc.Client.SendReaction(&gmproto.SendReactionRequest{
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
	convID, err := gc.ParsePortalID(msg.Portal.ID)
	if err != nil {
		return err
	}
	msgID, err := gc.ParseMessageID(targetMessage.ID)
	if err != nil {
		return err
	}
	return gc.Client.MarkRead(convID, msgID)
}
