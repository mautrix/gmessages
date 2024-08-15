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
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"

	"go.mau.fi/mautrix-gmessages/libgm"
	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
)

var _ bridgev2.BackfillingNetworkAPI = (*GMClient)(nil)

func makePaginationCursor(cursor *gmproto.Cursor) networkid.PaginationCursor {
	if cursor == nil {
		return ""
	}
	return networkid.PaginationCursor(fmt.Sprintf("%s:%d", cursor.LastItemID, cursor.LastItemTimestamp))
}

func parsePaginationCursor(cursor networkid.PaginationCursor) (*gmproto.Cursor, error) {
	var id int64
	var ts int64
	_, err := fmt.Sscanf(string(cursor), "%d:%d", &id, &ts)
	if err != nil {
		return nil, fmt.Errorf("failed to parse pagination cursor: %w", err)
	}
	return &gmproto.Cursor{
		LastItemID:        strconv.FormatInt(id, 10),
		LastItemTimestamp: ts,
	}, nil
}

func (gc *GMClient) FetchMessages(ctx context.Context, params bridgev2.FetchMessagesParams) (*bridgev2.FetchMessagesResponse, error) {
	convID, err := gc.ParsePortalID(params.Portal.ID)
	if err != nil {
		return nil, err
	}
	var cursor, anchorMsgCursor *gmproto.Cursor
	if params.Cursor != "" {
		cursor, _ = parsePaginationCursor(params.Cursor)
	}
	var anchorTS time.Time
	var anchorMsgID string
	if params.AnchorMessage != nil {
		anchorMsgID, err = gc.ParseMessageID(params.AnchorMessage.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to parse anchor message ID: %w", err)
		}
		anchorTS = params.AnchorMessage.Timestamp
		if !params.Forward {
			tsMilli := anchorTS.UnixMilli()
			anchorMsgCursor = &gmproto.Cursor{
				LastItemID:        anchorMsgID,
				LastItemTimestamp: tsMilli,
			}
			if cursor == nil || tsMilli < cursor.LastItemTimestamp {
				cursor = anchorMsgCursor
			}
		}
	}
	resp, err := gc.Client.FetchMessages(convID, int64(params.Count), cursor)
	if err != nil {
		return nil, err
	}
	zerolog.Ctx(ctx).Debug().
		Str("param_cursor", string(params.Cursor)).
		Str("anchor_cursor", string(makePaginationCursor(anchorMsgCursor))).
		Str("used_cursor", string(makePaginationCursor(cursor))).
		Str("response_cursor", string(makePaginationCursor(resp.Cursor))).
		Int("message_count", len(resp.Messages)).
		Int64("total_messages", resp.TotalMessages).
		Bool("forward", params.Forward).
		Msg("Google Messages fetch response")
	slices.Reverse(resp.Messages)
	fetchResp := &bridgev2.FetchMessagesResponse{
		Messages:         make([]*bridgev2.BackfillMessage, 0, len(resp.Messages)),
		Forward:          cursor == nil,
		MarkRead:         false,
		ApproxTotalCount: int(resp.TotalMessages),
	}
	for _, msg := range resp.Messages {
		msgTS := time.UnixMicro(msg.Timestamp)
		log := zerolog.Ctx(ctx).With().Str("message_id", msg.MessageID).Time("message_ts", msgTS).Logger()
		if !params.Forward && cursor != nil && msgTS.UnixMilli() >= cursor.LastItemTimestamp {
			log.Debug().Int64("cursor_ms", cursor.LastItemTimestamp).Msg("Ignoring message newer than cursor")
			continue
		} else if params.Forward && msgTS.Before(anchorTS) || anchorMsgID == msg.MessageID {
			log.Debug().
				Time("anchor_ts", anchorTS).
				Str("anchor_message_id", anchorMsgID).
				Msg("Ignoring message older than anchor message")
			continue
		}
		ctx := log.WithContext(ctx)
		sender := gc.getEventSenderFromMessage(msg)
		intent := params.Portal.GetIntentFor(ctx, sender, gc.UserLogin, bridgev2.RemoteEventBackfill)
		rawData, _ := proto.Marshal(msg)
		backfillMsg := &bridgev2.BackfillMessage{
			ConvertedMessage: gc.ConvertGoogleMessage(ctx, params.Portal, intent, &libgm.WrappedMessage{
				Message: msg,
				Data:    rawData,
			}, true),
			Sender:    sender,
			ID:        gc.MakeMessageID(msg.MessageID),
			TxnID:     networkid.TransactionID(msg.TmpID),
			Timestamp: msgTS,
			Reactions: (&ReactionSyncEvent{Message: msg, g: gc}).GetReactions().ToBackfill(),
		}
		fetchResp.Messages = append(fetchResp.Messages, backfillMsg)
	}
	fetchResp.HasMore = len(fetchResp.Messages) > 0
	if params.Forward {
		gc.conversationMetaLock.Lock()
		meta := gc.conversationMeta[convID]
		if meta != nil {
			lastWrappedMsg := fetchResp.Messages[len(fetchResp.Messages)-1]
			lastRawMsg := resp.Messages[len(resp.Messages)-1]
			fetchResp.MarkRead = !meta.unread || !meta.readUpToTS.Before(lastWrappedMsg.Timestamp) || meta.readUpTo == lastRawMsg.MessageID
		}
		gc.conversationMetaLock.Unlock()
	} else {
		fetchResp.Cursor = makePaginationCursor(resp.Cursor)
		if fetchResp.Cursor == "" && len(resp.Messages) > 0 {
			fetchResp.Cursor = makePaginationCursor(&gmproto.Cursor{
				LastItemID:        resp.Messages[0].MessageID,
				LastItemTimestamp: time.UnixMicro(resp.Messages[0].Timestamp).UnixMilli(),
			})
		}
	}
	return fetchResp, nil
}
