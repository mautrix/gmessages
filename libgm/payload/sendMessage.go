package payload

import "go.mau.fi/mautrix-gmessages/libgm/binary"

func NewMessageData(requestID string, encodedStr string, routingOpCode int64, msgType int64) *binary.MessageData {
	return &binary.MessageData{
		RequestID:     requestID,
		RoutingOpCode: routingOpCode,
		EncodedData:   encodedStr,
		MsgTypeArr: &binary.MsgTypeArr{
			EmptyArr: &binary.EmptyArr{},
			MsgType:  msgType,
		},
	}
}

func NewEncodedPayload(requestId string, opCode int64, encryptedData []byte, sessionID string) *binary.EncodedPayload {
	return &binary.EncodedPayload{
		RequestID:     requestId,
		Opcode:        opCode,
		EncryptedData: encryptedData,
		SessionID:     sessionID,
	}
}

func NewAuthData(requestId string, rpcKey []byte, date *binary.Date) *binary.AuthMessage {
	return &binary.AuthMessage{
		RequestID: requestId,
		RpcKey:    rpcKey,
		Date:      date,
	}
}

func NewSendMessage(pairedDevice *binary.Device, messageData *binary.MessageData, authData *binary.AuthMessage, ttl int64) *binary.SendMessage {
	return &binary.SendMessage{
		PairedDevice: pairedDevice,
		MessageData:  messageData,
		AuthData:     authData,
		TTL:          ttl,
		EmptyArr:     &binary.EmptyArr{},
	}
}
