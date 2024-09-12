package libgm

import (
	"encoding/base64"
	"fmt"
	"time"

	sync "github.com/sasha-s/go-deadlock"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"golang.org/x/exp/slices"
	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/util"
)

type SessionHandler struct {
	client *Client

	responseWaiters     map[string]chan<- *IncomingRPCMessage
	responseWaitersLock sync.Mutex

	ackMapLock sync.Mutex
	ackMap     []string
	ackTicker  *time.Ticker

	sessionID string
}

func (s *SessionHandler) ResetSessionID() {
	s.sessionID = uuid.NewString()
}

func (s *SessionHandler) sendMessageNoResponse(params SendMessageParams) error {
	requestID, payload, err := s.buildMessage(params)
	if err != nil {
		return err
	}

	url := util.SendMessageURL
	if s.client.AuthData.HasCookies() {
		url = util.SendMessageURLGoogle
	}
	s.client.Logger.Debug().
		Stringer("message_action", params.Action).
		Str("message_id", requestID).
		Msg("Sending request to phone (not expecting response)")
	_, err = typedHTTPResponse[*gmproto.OutgoingRPCResponse](
		s.client.makeProtobufHTTPRequest(url, payload, ContentTypePBLite),
	)
	return err
}

func (s *SessionHandler) sendAsyncMessage(params SendMessageParams) (<-chan *IncomingRPCMessage, error) {
	requestID, payload, err := s.buildMessage(params)
	if err != nil {
		return nil, err
	}

	ch := s.waitResponse(requestID)
	url := util.SendMessageURL
	if s.client.AuthData.HasCookies() {
		url = util.SendMessageURLGoogle
	}
	s.client.Logger.Debug().
		Stringer("message_action", params.Action).
		Str("message_id", requestID).
		Msg("Sending request to phone")
	_, err = typedHTTPResponse[*gmproto.OutgoingRPCResponse](
		s.client.makeProtobufHTTPRequest(url, payload, ContentTypePBLite),
	)
	if err != nil {
		s.cancelResponse(requestID, ch)
		return nil, err
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
		retErr = fmt.Errorf("unexpected response type %T for %s, expected %T", resp.DecryptedMessage, resp.ResponseID, casted)
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
	if s.client.AuthData.HasCookies() {
		switch msg.Message.Action {
		case gmproto.ActionType_CREATE_GAIA_PAIRING_CLIENT_INIT, gmproto.ActionType_CREATE_GAIA_PAIRING_CLIENT_FINISHED:
		default:
			// Very hacky way to ignore weird messages that come before real responses
			// TODO figure out how to properly handle these
			if msg.Message.UnencryptedData != nil && msg.Message.EncryptedData == nil {
				return false
			}
		}
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
	evt := s.client.Logger.Debug().
		Str("request_message_id", requestID).
		Str("response_message_id", msg.ResponseID)
	if msg.Message != nil {
		evt.Stringer("message_action", msg.Message.Action)
	}
	if s.client.Logger.GetLevel() == zerolog.TraceLevel {
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

	select {
	case resp := <-ch:
		return resp, nil
	case <-time.After(5 * time.Second):
		// Notify the pinger in order to trigger an event that the phone isn't responding
		select {
		case s.client.pingShortCircuit <- struct{}{}:
		default:
		}
	}
	// TODO hard timeout?
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

	RequestID   string
	OmitTTL     bool
	CustomTTL   int64
	DontEncrypt bool
	MessageType gmproto.MessageType
}

func (s *SessionHandler) buildMessage(params SendMessageParams) (string, proto.Message, error) {
	var err error
	sessionID := s.client.sessionHandler.sessionID

	requestID := params.RequestID
	if requestID == "" {
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
		DestRegistrationIDs: []string{},
	}
	if s.client.AuthData != nil && s.client.AuthData.DestRegID != uuid.Nil {
		message.DestRegistrationIDs = append(message.DestRegistrationIDs, s.client.AuthData.DestRegID.String())
	}
	if params.CustomTTL != 0 {
		message.TTL = params.CustomTTL
	} else if !params.OmitTTL {
		message.TTL = s.client.AuthData.TachyonTTL
	}
	var encryptedData, unencryptedData []byte
	if params.Data != nil {
		var serializedData []byte
		serializedData, err = proto.Marshal(params.Data)
		if err != nil {
			return "", nil, err
		}
		if params.DontEncrypt {
			unencryptedData = serializedData
		} else {
			encryptedData, err = s.client.AuthData.RequestCrypto.Encrypt(serializedData)
			if err != nil {
				return "", nil, err
			}
		}
	}
	message.Data.MessageData, err = proto.Marshal(&gmproto.OutgoingRPCData{
		RequestID:            requestID,
		Action:               params.Action,
		UnencryptedProtoData: unencryptedData,
		EncryptedProtoData:   encryptedData,
		SessionID:            sessionID,
	})
	if err != nil {
		return "", nil, err
	}

	return requestID, message, err
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
		return
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
	payload := &gmproto.AckMessageRequest{
		AuthData: &gmproto.AuthMessage{
			RequestID:        uuid.NewString(),
			TachyonAuthToken: s.client.AuthData.TachyonAuthToken,
			Network:          s.client.AuthData.AuthNetwork(),
			ConfigVersion:    util.ConfigMessage,
		},
		EmptyArr: &gmproto.EmptyArr{},
		Acks:     ackMessages,
	}
	url := util.AckMessagesURL
	if s.client.AuthData.HasCookies() {
		url = util.AckMessagesURLGoogle
	}
	_, err := typedHTTPResponse[*gmproto.OutgoingRPCResponse](
		s.client.makeProtobufHTTPRequest(url, payload, ContentTypePBLite),
	)
	if err != nil {
		// TODO retry?
		s.client.Logger.Err(err).Strs("message_ids", dataToAck).Msg("Failed to send acks")
	} else {
		s.client.Logger.Trace().Strs("message_ids", dataToAck).Msg("Sent acks")
	}
}
