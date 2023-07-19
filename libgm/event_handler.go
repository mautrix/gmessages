package libgm

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"

	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
)

type IncomingRPCMessage struct {
	*gmproto.IncomingRPCMessage

	Pair *gmproto.RPCPairData

	Message          *gmproto.RPCMessageData
	DecryptedData    []byte
	DecryptedMessage proto.Message
}

var responseType = map[gmproto.ActionType]proto.Message{
	gmproto.ActionType_IS_BUGLE_DEFAULT:           &gmproto.IsBugleDefaultResponse{},
	gmproto.ActionType_GET_UPDATES:                &gmproto.UpdateEvents{},
	gmproto.ActionType_LIST_CONVERSATIONS:         &gmproto.ListConversationsResponse{},
	gmproto.ActionType_NOTIFY_DITTO_ACTIVITY:      &gmproto.NotifyDittoActivityResponse{},
	gmproto.ActionType_GET_CONVERSATION_TYPE:      &gmproto.GetConversationTypeResponse{},
	gmproto.ActionType_GET_CONVERSATION:           &gmproto.GetConversationResponse{},
	gmproto.ActionType_LIST_MESSAGES:              &gmproto.ListMessagesResponse{},
	gmproto.ActionType_SEND_MESSAGE:               &gmproto.SendMessageResponse{},
	gmproto.ActionType_SEND_REACTION:              &gmproto.SendReactionResponse{},
	gmproto.ActionType_DELETE_MESSAGE:             &gmproto.DeleteMessageResponse{},
	gmproto.ActionType_GET_PARTICIPANTS_THUMBNAIL: &gmproto.GetParticipantThumbnailResponse{},
	gmproto.ActionType_LIST_CONTACTS:              &gmproto.ListContactsResponse{},
	gmproto.ActionType_LIST_TOP_CONTACTS:          &gmproto.ListTopContactsResponse{},
	gmproto.ActionType_GET_OR_CREATE_CONVERSATION: &gmproto.GetOrCreateConversationResponse{},
	gmproto.ActionType_UPDATE_CONVERSATION:        &gmproto.UpdateConversationResponse{},
}

func (c *Client) decryptInternalMessage(data *gmproto.IncomingRPCMessage) (*IncomingRPCMessage, error) {
	msg := &IncomingRPCMessage{
		IncomingRPCMessage: data,
	}
	switch data.BugleRoute {
	case gmproto.BugleRoute_PairEvent:
		msg.Pair = &gmproto.RPCPairData{}
		err := proto.Unmarshal(data.GetMessageData(), msg.Pair)
		if err != nil {
			return nil, err
		}
	case gmproto.BugleRoute_DataEvent:
		msg.Message = &gmproto.RPCMessageData{}
		err := proto.Unmarshal(data.GetMessageData(), msg.Message)
		if err != nil {
			return nil, err
		}
		responseStruct, ok := responseType[msg.Message.GetAction()]
		if ok {
			msg.DecryptedMessage = responseStruct.ProtoReflect().New().Interface()
		}
		if msg.Message.EncryptedData != nil {
			msg.DecryptedData, err = c.AuthData.RequestCrypto.Decrypt(msg.Message.EncryptedData)
			if err != nil {
				return nil, err
			}
			if msg.DecryptedMessage != nil {
				err = proto.Unmarshal(msg.DecryptedData, msg.DecryptedMessage)
				if err != nil {
					return nil, err
				}
			}
		}
	default:
		return nil, fmt.Errorf("unknown bugle route %d", data.BugleRoute)
	}
	return msg, nil
}

func (c *Client) deduplicateHash(id string, hash [32]byte) bool {
	const recentUpdatesLen = len(c.recentUpdates)
	for i := c.recentUpdatesPtr + recentUpdatesLen - 1; i >= c.recentUpdatesPtr; i-- {
		if c.recentUpdates[i%recentUpdatesLen].id == id {
			if c.recentUpdates[i%recentUpdatesLen].hash == hash {
				return true
			} else {
				break
			}
		}
	}
	c.recentUpdates[c.recentUpdatesPtr] = updateDedupItem{id: id, hash: hash}
	c.recentUpdatesPtr = (c.recentUpdatesPtr + 1) % recentUpdatesLen
	return false
}

func (c *Client) logContent(res *IncomingRPCMessage, thingID string, contentHash []byte) {
	if c.Logger.Trace().Enabled() && (res.DecryptedData != nil || res.DecryptedMessage != nil) {
		evt := c.Logger.Trace()
		if res.DecryptedMessage != nil {
			evt.Str("proto_name", string(res.DecryptedMessage.ProtoReflect().Descriptor().FullName()))
		}
		if res.DecryptedData != nil {
			evt.Str("data", base64.StdEncoding.EncodeToString(res.DecryptedData))
			if contentHash != nil {
				evt.Str("thing_id", thingID)
				evt.Hex("data_hash", contentHash)
			}
		} else {
			evt.Str("data", "<null>")
		}
		evt.Msg("Got event")
	}
}

func (c *Client) deduplicateUpdate(id string, msg *IncomingRPCMessage) bool {
	if msg.DecryptedData != nil {
		contentHash := sha256.Sum256(msg.DecryptedData)
		if c.deduplicateHash(id, contentHash) {
			c.Logger.Trace().Str("thing_id", id).Hex("data_hash", contentHash[:]).Msg("Ignoring duplicate update")
			return true
		}
		c.logContent(msg, id, contentHash[:])
	}
	return false
}

func (c *Client) HandleRPCMsg(rawMsg *gmproto.IncomingRPCMessage) {
	msg, err := c.decryptInternalMessage(rawMsg)
	if err != nil {
		c.Logger.Err(err).Msg("Failed to decode incoming RPC message")
		return
	}

	c.sessionHandler.queueMessageAck(msg.ResponseID)
	if c.sessionHandler.receiveResponse(msg) {
		return
	}
	switch msg.BugleRoute {
	case gmproto.BugleRoute_PairEvent:
		go c.handlePairingEvent(msg)
	case gmproto.BugleRoute_DataEvent:
		if c.skipCount > 0 {
			c.skipCount--
			c.Logger.Debug().
				Any("action", msg.Message.GetAction()).
				Int("remaining_skip_count", c.skipCount).
				Msg("Skipped DataEvent")
			if msg.DecryptedMessage != nil {
				c.Logger.Trace().
					Str("proto_name", string(msg.DecryptedMessage.ProtoReflect().Descriptor().FullName())).
					Str("data", base64.StdEncoding.EncodeToString(msg.DecryptedData)).
					Msg("Skipped event data")
			}
			return
		}
		c.handleUpdatesEvent(msg)
	}
}
