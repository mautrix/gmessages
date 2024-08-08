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
	"time"

	"google.golang.org/protobuf/proto"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"

	"go.mau.fi/mautrix-gmessages/libgm"
	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
)

var _ bridgev2.BackfillingNetworkAPI = (*GMClient)(nil)

func (gc *GMClient) FetchMessages(ctx context.Context, params bridgev2.FetchMessagesParams) (*bridgev2.FetchMessagesResponse, error) {
	convID, err := gc.ParsePortalID(params.Portal.ID)
	if err != nil {
		return nil, err
	}
	var cursor *gmproto.Cursor
	if !params.Forward && params.AnchorMessage != nil {
		msgID, err := gc.ParseMessageID(params.AnchorMessage.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to parse anchor message ID: %w", err)
		}
		cursor = &gmproto.Cursor{
			LastItemID:        msgID,
			LastItemTimestamp: params.AnchorMessage.Timestamp.UnixMilli(),
		}
	}
	resp, err := gc.Client.FetchMessages(convID, int64(params.Count), cursor)
	if err != nil {
		return nil, err
	}
	slices.Reverse(resp.Messages)
	fetchResp := &bridgev2.FetchMessagesResponse{
		Messages:         make([]*bridgev2.BackfillMessage, 0, len(resp.Messages)),
		Forward:          cursor == nil,
		MarkRead:         false,
		ApproxTotalCount: int(resp.TotalMessages),
	}
	for _, msg := range resp.Messages {
		msgTS := time.UnixMicro(msg.Timestamp)
		if !params.Forward && !msgTS.Before(params.AnchorMessage.Timestamp) {
			continue
		}
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
			fetchResp.MarkRead = !meta.readUpToTS.Before(fetchResp.Messages[len(fetchResp.Messages)-1].Timestamp)
		}
		gc.conversationMetaLock.Unlock()
	}
	return fetchResp, nil
}
