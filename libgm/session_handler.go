package libgm

import (
	"fmt"
	"sync"
	"time"

	"golang.org/x/exp/slices"
	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-gmessages/libgm/pblite"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/payload"
	"go.mau.fi/mautrix-gmessages/libgm/routes"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

/*
type Response struct {
	client *Client
	ResponseId string
	RoutingOpCode int64
	Data *binary.EncodedResponse // base64 encoded (decode -> protomessage)

	StartExecute string
	FinishExecute string
	DevicePair *pblite.DevicePair
}
*/

type SessionHandler struct {
	client   *Client
	requests map[string]map[binary.ActionType]*ResponseChan

	ackMapLock sync.Mutex
	ackMap     []string
	ackTicker  *time.Ticker

	sessionId string

	responseTimeout time.Duration
}

func (s *SessionHandler) SetResponseTimeout(milliSeconds int) {
	s.responseTimeout = time.Duration(milliSeconds) * time.Millisecond
}

func (s *SessionHandler) ResetSessionId() {
	s.sessionId = util.RandomUUIDv4()
}

func (s *SessionHandler) completeSendMessage(actionType binary.ActionType, addToChannel bool, encryptedData proto.Message) (string, error) {
	requestId, payload, action, buildErr := s.buildMessage(actionType, encryptedData)
	if buildErr != nil {
		return "", buildErr
	}

	if addToChannel {
		s.addRequestToChannel(requestId, action)
	}
	_, reqErr := s.client.rpc.sendMessageRequest(util.SEND_MESSAGE, payload)
	if reqErr != nil {
		return "", reqErr
	}
	return requestId, nil
}

func (s *SessionHandler) buildMessage(actionType binary.ActionType, encryptedData proto.Message) (string, []byte, binary.ActionType, error) {
	var requestId string
	pairedDevice := s.client.authData.DevicePair.Mobile
	sessionId := s.client.sessionHandler.sessionId
	token := s.client.authData.TachyonAuthToken

	routeInfo, ok := routes.Routes[actionType]
	if !ok {
		return "", nil, 0, fmt.Errorf("failed to build message: could not find route %d", actionType)
	}

	if routeInfo.UseSessionID {
		requestId = s.sessionId
	} else {
		requestId = util.RandomUUIDv4()
	}

	tmpMessage := payload.NewSendMessageBuilder(token, pairedDevice, requestId, sessionId).SetRoute(routeInfo.Action).SetSessionId(s.sessionId)

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

	return requestId, message, routeInfo.Action, nil
}

func (s *SessionHandler) addResponseAck(responseId string) {
	s.client.Logger.Debug().Any("responseId", responseId).Msg("Added to ack map")
	s.ackMapLock.Lock()
	defer s.ackMapLock.Unlock()
	hasResponseId := slices.Contains(s.ackMap, responseId)
	if !hasResponseId {
		s.ackMap = append(s.ackMap, responseId)
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
			RequestID:        util.RandomUUIDv4(),
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
	_, err = s.client.rpc.sendMessageRequest(util.ACK_MESSAGES, jsonData)
	if err != nil {
		panic(err)
	}
	s.client.Logger.Debug().Any("payload", jsonData).Msg("Sent acks")
}
