package pblite

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/crypto"
	"go.mau.fi/mautrix-gmessages/libgm/routes"
)

type DevicePair struct {
	Mobile  *binary.Device `json:"mobile,omitempty"`
	Browser *binary.Device `json:"browser,omitempty"`
}

type RequestData struct {
	RequestID     string            `json:"requestId,omitempty"`
	Timestamp     int64             `json:"timestamp,omitempty"`
	Action        binary.ActionType `json:"action,omitempty"`
	Bool1         bool              `json:"bool1,omitempty"`
	Bool2         bool              `json:"bool2,omitempty"`
	EncryptedData []byte            `json:"requestData,omitempty"`
	RawDecrypted  []byte            `json:"-,omitempty"`
	Decrypted     proto.Message     `json:"decrypted,omitempty"`
	Bool3         bool              `json:"bool3,omitempty"`
}

type Response struct {
	ResponseID        string             `json:"responseId,omitempty"`
	BugleRoute        binary.BugleRoute  `json:"bugleRoute,omitempty"`
	StartExecute      string             `json:"startExecute,omitempty"`
	MessageType       binary.MessageType `json:"eventType,omitempty"`
	FinishExecute     string             `json:"finishExecute,omitempty"`
	MillisecondsTaken string             `json:"millisecondsTaken,omitempty"`
	Devices           *DevicePair        `json:"devices,omitempty"`
	Data              RequestData        `json:"data,omitempty"`
	SignatureId       string             `json:"signatureId,omitempty"`
	Timestamp         string             `json:"timestamp"`
}

func DecryptInternalMessage(internalMessage *binary.InternalMessage, cryptor *crypto.Cryptor) (*Response, error) {
	var resp *Response
	switch internalMessage.Data.BugleRoute {
	case binary.BugleRoute_PairEvent:
		decodedData := &binary.PairEvents{}
		decodeErr := proto.Unmarshal(internalMessage.Data.ProtobufData, decodedData)
		if decodeErr != nil {
			return nil, decodeErr
		}
		resp = newResponseFromPairEvent(internalMessage.GetData(), decodedData)
	case binary.BugleRoute_DataEvent:
		internalRequestData := &binary.InternalRequestData{}
		decodeErr := proto.Unmarshal(internalMessage.Data.ProtobufData, internalRequestData)
		if decodeErr != nil {
			return nil, decodeErr
		}
		if internalRequestData.EncryptedData != nil {
			decryptedBytes, err := cryptor.Decrypt(internalRequestData.EncryptedData)
			if err != nil {
				return nil, err
			}
			responseStruct := routes.Routes[internalRequestData.GetAction()].ResponseStruct
			deserializedData := responseStruct.ProtoReflect().New().Interface()
			err = proto.Unmarshal(decryptedBytes, deserializedData)
			if err != nil {
				return nil, err
			}
			resp = newResponseFromDataEvent(internalMessage.GetData(), internalRequestData, decryptedBytes, deserializedData)
		} else {
			resp = newResponseFromDataEvent(internalMessage.GetData(), internalRequestData, nil, nil)
		}
	}
	return resp, nil
}

func newResponseFromPairEvent(internalMsg *binary.InternalMessageData, data *binary.PairEvents) *Response {
	resp := &Response{
		ResponseID:        internalMsg.GetResponseID(),
		BugleRoute:        internalMsg.GetBugleRoute(),
		StartExecute:      internalMsg.GetStartExecute(),
		MessageType:       internalMsg.GetMessageType(),
		FinishExecute:     internalMsg.GetFinishExecute(),
		MillisecondsTaken: internalMsg.GetMillisecondsTaken(),
		Devices: &DevicePair{
			Mobile:  internalMsg.GetMobile(),
			Browser: internalMsg.GetBrowser(),
		},
		Data: RequestData{
			Decrypted: data,
		},
		Timestamp:   internalMsg.GetTimestamp(),
		SignatureId: internalMsg.GetSignatureID(),
	}

	return resp
}

func newResponseFromDataEvent(internalMsg *binary.InternalMessageData, internalRequestData *binary.InternalRequestData, rawData []byte, decrypted protoreflect.ProtoMessage) *Response {
	resp := &Response{
		ResponseID:        internalMsg.GetResponseID(),
		BugleRoute:        internalMsg.GetBugleRoute(),
		StartExecute:      internalMsg.GetStartExecute(),
		MessageType:       internalMsg.GetMessageType(),
		FinishExecute:     internalMsg.GetFinishExecute(),
		MillisecondsTaken: internalMsg.GetMillisecondsTaken(),
		Devices: &DevicePair{
			Mobile:  internalMsg.GetMobile(),
			Browser: internalMsg.GetBrowser(),
		},
		Data: RequestData{
			RequestID:     internalRequestData.GetSessionID(),
			Timestamp:     internalRequestData.GetTimestamp(),
			Action:        internalRequestData.GetAction(),
			Bool1:         internalRequestData.GetBool1(),
			Bool2:         internalRequestData.GetBool2(),
			EncryptedData: internalRequestData.GetEncryptedData(),
			Decrypted:     decrypted,
			RawDecrypted:  rawData,
			Bool3:         internalRequestData.GetBool3(),
		},
		SignatureId: internalMsg.GetSignatureID(),
		Timestamp:   internalMsg.GetTimestamp(),
	}

	return resp
}
