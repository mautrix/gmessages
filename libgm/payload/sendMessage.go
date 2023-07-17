package payload

import (
	"fmt"

	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-gmessages/libgm/crypto"
	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
	"go.mau.fi/mautrix-gmessages/libgm/pblite"
	"go.mau.fi/mautrix-gmessages/libgm/routes"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

type SendMessageBuilder struct {
	message    *gmproto.SendMessage
	b64Message *gmproto.SendMessageInternal

	err error
}

func (sm *SendMessageBuilder) Err() error {
	return sm.err
}

func NewSendMessageBuilder(tachyonAuthToken []byte, pairedDevice *gmproto.Device, requestId string, sessionId string) *SendMessageBuilder {
	return &SendMessageBuilder{
		message: &gmproto.SendMessage{
			Mobile: pairedDevice,
			MessageData: &gmproto.SendMessageData{
				RequestID: requestId,
			},
			MessageAuth: &gmproto.SendMessageAuth{
				RequestID:        requestId,
				TachyonAuthToken: tachyonAuthToken,
				ConfigVersion:    util.ConfigMessage,
			},
			EmptyArr: &gmproto.EmptyArr{},
		},
		b64Message: &gmproto.SendMessageInternal{
			RequestID: requestId,
			SessionID: sessionId,
		},
	}
}

func (sm *SendMessageBuilder) SetPairedDevice(device *gmproto.Device) *SendMessageBuilder {
	sm.message.Mobile = device
	return sm
}

func (sm *SendMessageBuilder) setBugleRoute(bugleRoute gmproto.BugleRoute) *SendMessageBuilder {
	sm.message.MessageData.BugleRoute = bugleRoute
	return sm
}

func (sm *SendMessageBuilder) SetRequestId(requestId string) *SendMessageBuilder {
	sm.message.MessageAuth.RequestID = requestId
	sm.message.MessageData.RequestID = requestId
	sm.b64Message.RequestID = requestId
	return sm
}

func (sm *SendMessageBuilder) SetSessionId(sessionId string) *SendMessageBuilder {
	sm.b64Message.SessionID = sessionId
	return sm
}

func (sm *SendMessageBuilder) SetRoute(actionType gmproto.ActionType) *SendMessageBuilder {
	action, ok := routes.Routes[actionType]
	if !ok {
		sm.err = fmt.Errorf("invalid action type")
		return sm
	}

	sm.setBugleRoute(action.BugleRoute)
	sm.setMessageType(action.MessageType)
	sm.b64Message.Action = action.Action
	return sm
}

func (sm *SendMessageBuilder) setMessageType(eventType gmproto.MessageType) *SendMessageBuilder {
	sm.message.MessageData.MessageTypeData = &gmproto.MessageTypeData{
		EmptyArr:    &gmproto.EmptyArr{},
		MessageType: eventType,
	}
	return sm
}

func (sm *SendMessageBuilder) SetTTL(ttl int64) *SendMessageBuilder {
	sm.message.TTL = ttl
	return sm
}

func (sm *SendMessageBuilder) SetEncryptedProtoMessage(message proto.Message, cryptor *crypto.AESCTRHelper) *SendMessageBuilder {
	plaintextBytes, err := proto.Marshal(message)
	if err != nil {
		sm.err = err
		return sm
	}

	encryptedBytes, err := cryptor.Encrypt(plaintextBytes)
	if err != nil {
		sm.err = err
		return sm
	}

	sm.b64Message.EncryptedProtoData = encryptedBytes
	return sm
}

func (sm *SendMessageBuilder) Build() ([]byte, error) {
	if sm.err != nil {
		return nil, sm.err
	}
	encodedMessage, err := proto.Marshal(sm.b64Message)
	if err != nil {
		return nil, err
	}
	sm.message.MessageData.ProtobufData = encodedMessage

	protoJSONBytes, serializeErr := pblite.Marshal(sm.message)
	if serializeErr != nil {
		panic(serializeErr)
		return nil, serializeErr
	}

	return protoJSONBytes, nil
}
