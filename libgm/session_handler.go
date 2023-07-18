package libgm

import (
	"encoding/base64"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/exp/slices"
	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-gmessages/libgm/pblite"

	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

type SessionHandler struct {
	client *Client

	responseWaiters     map[string]chan<- *IncomingRPCMessage
	responseWaitersLock sync.Mutex

	ackMapLock sync.Mutex
	ackMap     []string
	ackTicker  *time.Ticker

	sessionID string

	responseTimeout time.Duration
}

func (s *SessionHandler) ResetSessionID() {
	s.sessionID = uuid.NewString()
}

func (s *SessionHandler) sendMessageNoResponse(params SendMessageParams) error {
	_, payload, err := s.buildMessage(params)
	if err != nil {
		return err
	}

	_, err = s.client.rpc.sendMessageRequest(util.SendMessageURL, payload)
	return err
}

func (s *SessionHandler) sendAsyncMessage(params SendMessageParams) (<-chan *IncomingRPCMessage, error) {
	requestID, payload, err := s.buildMessage(params)
	if err != nil {
		return nil, err
	}

	ch := s.waitResponse(requestID)
	_, reqErr := s.client.rpc.sendMessageRequest(util.SendMessageURL, payload)
	if reqErr != nil {
		s.cancelResponse(requestID, ch)
		return nil, reqErr
	}
	return ch, nil
}

func typedResponse[T proto.Message](resp *IncomingRPCMessage, err error) (casted T, retErr error) {
	if err != nil {
		retErr = err
		return
	}
	var ok bool
	casted, ok = resp.DecryptedMessage.(T)
	if !ok {
		retErr = fmt.Errorf("unexpected response type %T, expected %T", resp.DecryptedMessage, casted)
	}
	return
}

func (s *SessionHandler) waitResponse(requestID string) chan *IncomingRPCMessage {
	ch := make(chan *IncomingRPCMessage, 1)
	s.responseWaitersLock.Lock()
	s.responseWaiters[requestID] = ch
	s.responseWaitersLock.Unlock()
	return ch
}

func (s *SessionHandler) cancelResponse(requestID string, ch chan *IncomingRPCMessage) {
	s.responseWaitersLock.Lock()
	close(ch)
	delete(s.responseWaiters, requestID)
	s.responseWaitersLock.Unlock()
}

func (s *SessionHandler) receiveResponse(msg *IncomingRPCMessage) bool {
	if msg.Message == nil {
		return false
	}
	requestID := msg.Message.SessionID
	s.responseWaitersLock.Lock()
	ch, ok := s.responseWaiters[requestID]
	if !ok {
		s.responseWaitersLock.Unlock()
		return false
	}
	delete(s.responseWaiters, requestID)
	s.responseWaitersLock.Unlock()
	evt := s.client.Logger.Trace().
		Str("request_id", requestID)
	if evt.Enabled() {
		if msg.DecryptedData != nil {
			evt.Str("data", base64.StdEncoding.EncodeToString(msg.DecryptedData))
		}
		if msg.DecryptedMessage != nil {
			evt.Str("proto_name", string(msg.DecryptedMessage.ProtoReflect().Descriptor().FullName()))
		}
	}
	evt.Msg("Received response")
	ch <- msg
	return true
}

func (s *SessionHandler) sendMessageWithParams(params SendMessageParams) (*IncomingRPCMessage, error) {
	ch, err := s.sendAsyncMessage(params)
	if err != nil {
		return nil, err
	}

	// TODO add timeout
	return <-ch, nil
}

func (s *SessionHandler) sendMessage(actionType gmproto.ActionType, encryptedData proto.Message) (*IncomingRPCMessage, error) {
	return s.sendMessageWithParams(SendMessageParams{
		Action: actionType,
		Data:   encryptedData,
	})
}

type SendMessageParams struct {
	Action gmproto.ActionType
	Data   proto.Message

	UseSessionID bool
	OmitTTL      bool
	MessageType  gmproto.MessageType
}

func (s *SessionHandler) buildMessage(params SendMessageParams) (string, []byte, error) {
	var requestID string
	var err error
	sessionID := s.client.sessionHandler.sessionID

	if params.UseSessionID {
		requestID = s.sessionID
	} else {
		requestID = uuid.NewString()
	}

	if params.MessageType == 0 {
		params.MessageType = gmproto.MessageType_BUGLE_MESSAGE
	}

	message := &gmproto.OutgoingRPCMessage{
		Mobile: s.client.AuthData.Mobile,
		Data: &gmproto.OutgoingRPCMessage_Data{
			RequestID:  requestID,
			BugleRoute: gmproto.BugleRoute_DataEvent,
			MessageTypeData: &gmproto.OutgoingRPCMessage_Data_Type{
				EmptyArr:    &gmproto.EmptyArr{},
				MessageType: params.MessageType,
			},
		},
		Auth: &gmproto.OutgoingRPCMessage_Auth{
			RequestID:        requestID,
			TachyonAuthToken: s.client.AuthData.TachyonAuthToken,
			ConfigVersion:    util.ConfigMessage,
		},
		EmptyArr: &gmproto.EmptyArr{},
	}
	if !params.OmitTTL {
		message.TTL = s.client.AuthData.TachyonTTL
	}
	var encryptedData []byte
	if params.Data != nil {
		var serializedData []byte
		serializedData, err = proto.Marshal(params.Data)
		if err != nil {
			return "", nil, err
		}
		encryptedData, err = s.client.AuthData.RequestCrypto.Encrypt(serializedData)
		if err != nil {
			return "", nil, err
		}
	}
	message.Data.MessageData, err = proto.Marshal(&gmproto.OutgoingRPCData{
		RequestID:          requestID,
		Action:             params.Action,
		EncryptedProtoData: encryptedData,
		SessionID:          sessionID,
	})
	if err != nil {
		return "", nil, err
	}

	var marshaledMessage []byte
	marshaledMessage, err = pblite.Marshal(message)
	return requestID, marshaledMessage, err
}

func (s *SessionHandler) queueMessageAck(messageID string) {
	s.ackMapLock.Lock()
	defer s.ackMapLock.Unlock()
	if !slices.Contains(s.ackMap, messageID) {
		s.ackMap = append(s.ackMap, messageID)
		s.client.Logger.Trace().Any("message_id", messageID).Msg("Queued ack for message")
	} else {
		s.client.Logger.Trace().Any("message_id", messageID).Msg("Ack for message was already queued")
	}
}

func (s *SessionHandler) startAckInterval() {
	if s.ackTicker != nil {
		s.ackTicker.Stop()
	}
	ticker := time.NewTicker(5 * time.Second)
	s.ackTicker = ticker
	go func() {
		for range ticker.C {
			s.sendAckRequest()
		}
	}()
}

func (s *SessionHandler) sendAckRequest() {
	s.ackMapLock.Lock()
	dataToAck := s.ackMap
	s.ackMap = nil
	s.ackMapLock.Unlock()
	if len(dataToAck) == 0 {
		return
	}
	ackMessages := make([]*gmproto.AckMessageRequest_Message, len(dataToAck))
	for i, reqID := range dataToAck {
		ackMessages[i] = &gmproto.AckMessageRequest_Message{
			RequestID: reqID,
			Device:    s.client.AuthData.Browser,
		}
	}
	ackMessagePayload := &gmproto.AckMessageRequest{
		AuthData: &gmproto.AuthMessage{
			RequestID:        uuid.NewString(),
			TachyonAuthToken: s.client.AuthData.TachyonAuthToken,
			ConfigVersion:    util.ConfigMessage,
		},
		EmptyArr: &gmproto.EmptyArr{},
		Acks:     ackMessages,
	}
	jsonData, err := pblite.Marshal(ackMessagePayload)
	if err != nil {
		panic(err)
	}
	_, err = s.client.rpc.sendMessageRequest(util.AckMessagesURL, jsonData)
	if err != nil {
		panic(err)
	}
	s.client.Logger.Debug().Strs("message_ids", dataToAck).Msg("Sent acks")
}
