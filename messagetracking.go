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
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/bridge/status"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
)

var (
	errMNoticeDisabled             = errors.New("bridging m.notice messages is disabled")
	errUnexpectedParsedContentType = errors.New("unexpected parsed content type")
	errUnknownMsgType              = errors.New("unknown msgtype")
	errMediaUnsupportedType        = errors.New("unsupported media type")
	errTargetNotFound              = errors.New("target event not found")
	errMissingMediaURL             = errors.New("missing media URL")
	errMediaDownloadFailed         = errors.New("failed to download media")
	errMediaDecryptFailed          = errors.New("failed to decrypt media")
	errMediaReuploadFailed         = errors.New("failed to upload media to google")

	errMessageTakingLong = errors.New("bridging the message is taking longer than usual")
)

type OutgoingStatusError gmproto.MessageStatusType

func (ose OutgoingStatusError) Error() string {
	return strings.TrimPrefix(gmproto.MessageStatusType(ose).String(), "OUTGOING_")
}

func (ose OutgoingStatusError) HumanError() string {
	switch gmproto.MessageStatusType(ose) {
	case gmproto.MessageStatusType_OUTGOING_FAILED_TOO_LARGE:
		return "too large"
	case gmproto.MessageStatusType_OUTGOING_FAILED_RECIPIENT_LOST_RCS:
		return "recipient lost RCS support"
	case gmproto.MessageStatusType_OUTGOING_FAILED_RECIPIENT_LOST_ENCRYPTION:
		return "recipient lost encryption support"
	case gmproto.MessageStatusType_OUTGOING_FAILED_RECIPIENT_DID_NOT_DECRYPT,
		gmproto.MessageStatusType_OUTGOING_FAILED_RECIPIENT_DID_NOT_DECRYPT_NO_MORE_RETRY:
		return "recipient failed to decrypt message"
	}
	return ""
}

func (ose OutgoingStatusError) Is(other error) bool {
	otherOSE, ok := other.(OutgoingStatusError)
	if !ok {
		return false
	}
	return int(ose) == int(otherOSE)
}

func errorToStatusReason(err error) (reason event.MessageStatusReason, status event.MessageStatus, isCertain, sendNotice bool, humanMessage string) {
	var ose OutgoingStatusError
	switch {
	case errors.Is(err, errUnexpectedParsedContentType),
		errors.Is(err, errUnknownMsgType):
		return event.MessageStatusUnsupported, event.MessageStatusFail, true, true, ""
	case errors.Is(err, errMNoticeDisabled):
		return event.MessageStatusUnsupported, event.MessageStatusFail, true, false, ""
	case errors.Is(err, errMediaUnsupportedType):
		return event.MessageStatusUnsupported, event.MessageStatusFail, true, true, err.Error()
	case errors.Is(err, context.DeadlineExceeded):
		return event.MessageStatusTooOld, event.MessageStatusRetriable, false, true, "handling the message took too long and was cancelled"
	case errors.Is(err, errTargetNotFound):
		return event.MessageStatusGenericError, event.MessageStatusFail, true, false, ""
	case errors.As(err, &ose):
		return event.MessageStatusNetworkError, event.MessageStatusFail, true, true, ose.HumanError()
	default:
		return event.MessageStatusGenericError, event.MessageStatusRetriable, false, true, ""
	}
}

func (portal *Portal) sendErrorMessage(evt *event.Event, err error, msgType string, confirmed bool, editID id.EventID) id.EventID {
	if !portal.bridge.Config.Bridge.MessageErrorNotices {
		return ""
	}
	certainty := "may not have been"
	if confirmed {
		certainty = "was not"
	}
	msg := fmt.Sprintf("\u26a0 Your %s %s bridged: %v", msgType, certainty, err)
	if errors.Is(err, errMessageTakingLong) {
		msg = fmt.Sprintf("\u26a0 Bridging your %s is taking longer than usual", msgType)
	}
	content := &event.MessageEventContent{
		MsgType: event.MsgNotice,
		Body:    msg,
	}
	if editID != "" {
		content.SetEdit(editID)
	} else {
		content.SetReply(evt)
	}
	resp, err := portal.sendMainIntentMessage(content)
	if err != nil {
		portal.zlog.Warn().Err(err).Str("event_id", evt.ID.String()).Msg("Failed to send bridging error message")
		return ""
	}
	return resp.EventID
}

func (portal *Portal) sendStatusEvent(evtID, lastRetry id.EventID, err error, deliveredTo *[]id.UserID) {
	if !portal.bridge.Config.Bridge.MessageStatusEvents {
		return
	}
	if lastRetry == evtID {
		lastRetry = ""
	}
	intent := portal.bridge.Bot
	if !portal.Encrypted {
		// Bridge bot isn't present in unencrypted DMs
		intent = portal.MainIntent()
	}
	content := event.BeeperMessageStatusEventContent{
		Network: portal.getBridgeInfoStateKey(),
		RelatesTo: event.RelatesTo{
			Type:    event.RelReference,
			EventID: evtID,
		},
		LastRetry: lastRetry,

		DeliveredToUsers: deliveredTo,
	}
	if err == nil {
		content.Status = event.MessageStatusSuccess
	} else {
		content.Reason, content.Status, _, _, content.Message = errorToStatusReason(err)
		content.Error = err.Error()
	}
	_, err = intent.SendMessageEvent(portal.MXID, event.BeeperMessageStatus, &content)
	if err != nil {
		portal.zlog.Warn().Err(err).Str("event_id", evtID.String()).Msg("Failed to send message status event")
	}
}

func (portal *Portal) sendDeliveryReceipt(eventID id.EventID) {
	if portal.bridge.Config.Bridge.DeliveryReceipts {
		err := portal.bridge.Bot.SendReceipt(portal.MXID, eventID, event.ReceiptTypeRead, nil)
		if err != nil {
			portal.zlog.Warn().Err(err).Str("event_id", eventID.String()).Msg("Failed to send delivery receipt")
		}
	}
}

func (portal *Portal) sendMessageMetrics(evt *event.Event, err error, part string, ms *metricSender) {
	var msgType string
	switch evt.Type {
	case event.EventMessage:
		msgType = "message"
	case event.EventReaction:
		msgType = "reaction"
	case event.EventRedaction:
		msgType = "redaction"
	default:
		msgType = "unknown event"
	}
	origEvtID := evt.ID
	if retryMeta := evt.Content.AsMessage().MessageSendRetry; retryMeta != nil {
		origEvtID = retryMeta.OriginalEventID
	}
	if err != nil {
		logEvt := portal.zlog.Error()
		if part == "Ignoring" {
			logEvt = portal.zlog.Debug()
		}
		logEvt.Err(err).
			Str("event_id", evt.ID.String()).
			Str("part", part).
			Str("event_sender", evt.Sender.String()).
			Str("event_type", evt.Type.Type).
			Msg("Failed to handle Matrix event")
		reason, statusCode, isCertain, sendNotice, _ := errorToStatusReason(err)
		checkpointStatus := status.ReasonToCheckpointStatus(reason, statusCode)
		portal.bridge.SendMessageCheckpoint(evt, status.MsgStepRemote, err, checkpointStatus, ms.getRetryNum())
		if sendNotice {
			ms.setNoticeID(portal.sendErrorMessage(evt, err, msgType, isCertain, ms.getNoticeID()))
		}
		portal.sendStatusEvent(origEvtID, evt.ID, err, nil)
	} else {
		portal.zlog.Debug().
			Str("event_id", evt.ID.String()).
			Str("event_type", evt.Type.Type).
			Msg("Handled Matrix event")
		portal.sendDeliveryReceipt(evt.ID)
		portal.bridge.SendMessageSuccessCheckpoint(evt, status.MsgStepRemote, ms.getRetryNum())
		if msgType != "message" {
			portal.sendStatusEvent(origEvtID, evt.ID, nil, nil)
		}
		if prevNotice := ms.popNoticeID(); prevNotice != "" {
			_, _ = portal.MainIntent().RedactEvent(portal.MXID, prevNotice, mautrix.ReqRedact{
				Reason: "error resolved",
			})
		}
	}
	if ms != nil {
		portal.zlog.Debug().EmbedObject(ms.timings).Str("event_id", evt.ID.String()).Msg("Timings for Matrix event")
	}
}

type messageTimings struct {
	initReceive  time.Duration
	decrypt      time.Duration
	portalQueue  time.Duration
	totalReceive time.Duration

	convert time.Duration
	send    time.Duration
}

func (mt *messageTimings) MarshalZerologObject(evt *zerolog.Event) {
	evt.Dur("receive", mt.initReceive).
		Dur("decrypt", mt.decrypt).
		Dur("queue", mt.portalQueue).
		Dur("total_hs_to_portal", mt.totalReceive).
		Dur("convert", mt.convert).
		Dur("send", mt.send)
}

type metricSender struct {
	portal         *Portal
	previousNotice id.EventID
	lock           sync.Mutex
	completed      bool
	retryNum       int
	timings        *messageTimings
}

func (ms *metricSender) getRetryNum() int {
	if ms != nil {
		return ms.retryNum
	}
	return 0
}

func (ms *metricSender) getNoticeID() id.EventID {
	if ms == nil {
		return ""
	}
	return ms.previousNotice
}

func (ms *metricSender) popNoticeID() id.EventID {
	if ms == nil {
		return ""
	}
	evtID := ms.previousNotice
	ms.previousNotice = ""
	return evtID
}

func (ms *metricSender) setNoticeID(evtID id.EventID) {
	if ms != nil && ms.previousNotice == "" {
		ms.previousNotice = evtID
	}
}

func (ms *metricSender) sendMessageMetrics(evt *event.Event, err error, part string, completed bool) {
	ms.lock.Lock()
	defer ms.lock.Unlock()
	if !completed && ms.completed {
		return
	}
	ms.portal.sendMessageMetrics(evt, err, part, ms)
	ms.retryNum++
	ms.completed = completed
}
