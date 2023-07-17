package libgm

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"

	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-gmessages/libgm/routes"

	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
)

type IncomingRPCMessage struct {
	*gmproto.IncomingRPCMessage

	Pair *gmproto.RPCPairData

	Message          *gmproto.RPCMessageData
	DecryptedData    []byte
	DecryptedMessage proto.Message
}

func (r *RPC) decryptInternalMessage(data *gmproto.IncomingRPCMessage) (*IncomingRPCMessage, error) {
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
		if msg.Message.EncryptedData != nil {
			msg.DecryptedData, err = r.client.AuthData.RequestCrypto.Decrypt(msg.Message.EncryptedData)
			if err != nil {
				return nil, err
			}
			responseStruct := routes.Routes[msg.Message.GetAction()].ResponseStruct
			msg.DecryptedMessage = responseStruct.ProtoReflect().New().Interface()
			err = proto.Unmarshal(msg.DecryptedData, msg.DecryptedMessage)
			if err != nil {
				return nil, err
			}
		}
	default:
		return nil, fmt.Errorf("unknown bugle route %d", data.BugleRoute)
	}
	return msg, nil
}

func (r *RPC) deduplicateHash(hash [32]byte) bool {
	const recentUpdatesLen = len(r.recentUpdates)
	for i := r.recentUpdatesPtr + recentUpdatesLen - 1; i >= r.recentUpdatesPtr; i-- {
		if r.recentUpdates[i%recentUpdatesLen] == hash {
			return true
		}
	}
	r.recentUpdates[r.recentUpdatesPtr] = hash
	r.recentUpdatesPtr = (r.recentUpdatesPtr + 1) % recentUpdatesLen
	return false
}

func (r *RPC) logContent(res *IncomingRPCMessage) {
	if r.client.Logger.Trace().Enabled() && res.DecryptedData != nil {
		evt := r.client.Logger.Trace()
		if res.DecryptedMessage != nil {
			evt.Str("proto_name", string(res.DecryptedMessage.ProtoReflect().Descriptor().FullName()))
		}
		if res.DecryptedData != nil {
			evt.Str("data", base64.StdEncoding.EncodeToString(res.DecryptedData))
		} else {
			evt.Str("data", "<null>")
		}
		evt.Msg("Got event")
	}
}

func (r *RPC) deduplicateUpdate(msg *IncomingRPCMessage) bool {
	if msg.DecryptedData != nil {
		contentHash := sha256.Sum256(msg.DecryptedData)
		if r.deduplicateHash(contentHash) {
			r.client.Logger.Trace().Hex("data_hash", contentHash[:]).Msg("Ignoring duplicate update")
			return true
		}
		r.logContent(msg)
	}
	return false
}

func (r *RPC) HandleRPCMsg(rawMsg *gmproto.IncomingRPCMessage) {
	msg, err := r.decryptInternalMessage(rawMsg)
	if err != nil {
		r.client.Logger.Err(err).Msg("Failed to decode incoming RPC message")
		return
	}

	r.client.sessionHandler.queueMessageAck(msg.ResponseID)
	if r.client.sessionHandler.receiveResponse(msg) {
		return
	}
	switch msg.BugleRoute {
	case gmproto.BugleRoute_PairEvent:
		go r.client.handlePairingEvent(msg)
	case gmproto.BugleRoute_DataEvent:
		if r.skipCount > 0 {
			r.skipCount--
			r.client.Logger.Debug().
				Any("action", msg.Message.GetAction()).
				Int("remaining_skip_count", r.skipCount).
				Msg("Skipped DataEvent")
			if msg.DecryptedMessage != nil {
				r.client.Logger.Trace().
					Str("proto_name", string(msg.DecryptedMessage.ProtoReflect().Descriptor().FullName())).
					Str("data", base64.StdEncoding.EncodeToString(msg.DecryptedData)).
					Msg("Skipped event data")
			}
			return
		}
		r.client.handleUpdatesEvent(msg)
	}
}
