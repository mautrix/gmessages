package payload

import "go.mau.fi/mautrix-gmessages/libgm/binary"

func NewMessageData(requestID string, encodedStr string, routingOpCode int64, msgType int64) *binary.MessageData {
	return &binary.MessageData{
		RequestId:     requestID,
		RoutingOpCode: routingOpCode,
		EncodedData:   encodedStr,
		MsgTypeArr: &binary.MsgTypeArr{
			EmptyArr: &binary.EmptyArr{},
			MsgType:  msgType,
		},
	}
}

func NewEncodedPayload(requestId string, opCode int64, encryptedData []byte, sessionId string) *binary.EncodedPayload {
	return &binary.EncodedPayload{
		RequestId:     requestId,
		Opcode:        opCode,
		EncryptedData: encryptedData,
		SessionId:     sessionId,
	}
}

func NewAuthData(requestId string, rpcKey string, date *binary.Date) *binary.AuthMessage {
	return &binary.AuthMessage{
		RequestId: requestId,
		RpcKey:    rpcKey,
		Date:      date,
	}
}

func NewSendMessage(pairedDevice *binary.Device, messageData *binary.MessageData, authData *binary.AuthMessage, ttl int64) *binary.SendMessage {
	return &binary.SendMessage{
		PairedDevice: pairedDevice,
		MessageData:  messageData,
		AuthData:     authData,
		Ttl:          ttl,
		EmptyArr:     &binary.EmptyArr{},
	}
}
