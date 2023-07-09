package payload

import (
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/crypto"
	"go.mau.fi/mautrix-gmessages/libgm/pblite"
	"go.mau.fi/mautrix-gmessages/libgm/routes"
)

type SendMessageBuilder struct {
	message    *binary.SendMessage
	b64Message *binary.SendMessageInternal

	err error
}

func (sm *SendMessageBuilder) Err() error {
	return sm.err
}

func NewSendMessageBuilder(tachyonAuthToken []byte, pairedDevice *binary.Device, requestId string, sessionId string) *SendMessageBuilder {
	return &SendMessageBuilder{
		message: &binary.SendMessage{
			Mobile: pairedDevice,
			MessageData: &binary.SendMessageData{
				RequestID: requestId,
			},
			MessageAuth: &binary.SendMessageAuth{
				RequestID:        requestId,
				TachyonAuthToken: tachyonAuthToken,
				ConfigVersion:    ConfigMessage,
			},
			EmptyArr: &binary.EmptyArr{},
		},
		b64Message: &binary.SendMessageInternal{
			RequestID: requestId,
			SessionID: sessionId,
		},
	}
}

func (sm *SendMessageBuilder) SetPairedDevice(device *binary.Device) *SendMessageBuilder {
	sm.message.Mobile = device
	return sm
}

func (sm *SendMessageBuilder) setBugleRoute(bugleRoute binary.BugleRoute) *SendMessageBuilder {
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

func (sm *SendMessageBuilder) SetRoute(actionType binary.ActionType) *SendMessageBuilder {
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

func (sm *SendMessageBuilder) setMessageType(eventType binary.MessageType) *SendMessageBuilder {
	sm.message.MessageData.MessageTypeData = &binary.MessageTypeData{
		EmptyArr:    &binary.EmptyArr{},
		MessageType: eventType,
	}
	return sm
}

func (sm *SendMessageBuilder) SetTTL(ttl int64) *SendMessageBuilder {
	sm.message.TTL = ttl
	return sm
}

func (sm *SendMessageBuilder) SetEncryptedProtoMessage(message proto.Message, cryptor *crypto.Cryptor) *SendMessageBuilder {
	encryptedBytes, encryptErr := cryptor.EncodeAndEncryptData(message)
	if encryptErr != nil {
		sm.err = encryptErr
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

	messageProtoJSON, serializeErr := pblite.Serialize(sm.message.ProtoReflect())
	if serializeErr != nil {
		panic(serializeErr)
		return nil, serializeErr
	}

	protoJSONBytes, marshalErr := json.Marshal(messageProtoJSON)
	if marshalErr != nil {
		return nil, marshalErr
	}
	return protoJSONBytes, nil
}
