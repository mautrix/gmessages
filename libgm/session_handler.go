package libgm

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/exp/slices"
	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-gmessages/libgm/pblite"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/payload"
	"go.mau.fi/mautrix-gmessages/libgm/routes"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

type SessionHandler struct {
	client *Client

	responseWaiters     map[string]chan<- *pblite.Response
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

func (s *SessionHandler) sendMessageNoResponse(actionType binary.ActionType, encryptedData proto.Message) error {
	_, payload, _, err := s.buildMessage(actionType, encryptedData)
	if err != nil {
		return err
	}

	_, err = s.client.rpc.sendMessageRequest(util.SendMessageURL, payload)
	return err
}

func (s *SessionHandler) sendAsyncMessage(actionType binary.ActionType, encryptedData proto.Message) (<-chan *pblite.Response, error) {
	requestID, payload, _, buildErr := s.buildMessage(actionType, encryptedData)
	if buildErr != nil {
		return nil, buildErr
	}

	ch := s.waitResponse(requestID)
	_, reqErr := s.client.rpc.sendMessageRequest(util.SendMessageURL, payload)
	if reqErr != nil {
		s.cancelResponse(requestID, ch)
		return nil, reqErr
	}
	return ch, nil
}

func (s *SessionHandler) sendMessage(actionType binary.ActionType, encryptedData proto.Message) (*pblite.Response, error) {
	ch, err := s.sendAsyncMessage(actionType, encryptedData)
	if err != nil {
		return nil, err
	}

	// TODO add timeout
	return <-ch, nil
}

func (s *SessionHandler) buildMessage(actionType binary.ActionType, encryptedData proto.Message) (string, []byte, binary.ActionType, error) {
	var requestID string
	pairedDevice := s.client.authData.DevicePair.Mobile
	sessionId := s.client.sessionHandler.sessionID
	token := s.client.authData.TachyonAuthToken

	routeInfo, ok := routes.Routes[actionType]
	if !ok {
		return "", nil, 0, fmt.Errorf("failed to build message: could not find route %d", actionType)
	}

	if routeInfo.UseSessionID {
		requestID = s.sessionID
	} else {
		requestID = uuid.NewString()
	}

	tmpMessage := payload.NewSendMessageBuilder(token, pairedDevice, requestID, sessionId).SetRoute(routeInfo.Action).SetSessionId(s.sessionID)

	if encryptedData != nil {
		tmpMessage.SetEncryptedProtoMessage(encryptedData, s.client.authData.Cryptor)
	}

	if routeInfo.UseTTL {
		tmpMessage.SetTTL(s.client.authData.TTL)
	}

	message, buildErr := tmpMessage.Build()
	if buildErr != nil {
		return "", nil, 0, buildErr
	}

	return requestID, message, routeInfo.Action, nil
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
	ackMessages := make([]*binary.AckMessageData, len(dataToAck))
	for i, reqID := range dataToAck {
		ackMessages[i] = &binary.AckMessageData{
			RequestID: reqID,
			Device:    s.client.authData.DevicePair.Browser,
		}
	}
	ackMessagePayload := &binary.AckMessagePayload{
		AuthData: &binary.AuthMessage{
			RequestID:        uuid.NewString(),
			TachyonAuthToken: s.client.authData.TachyonAuthToken,
			ConfigVersion:    payload.ConfigMessage,
		},
		EmptyArr: &binary.EmptyArr{},
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
