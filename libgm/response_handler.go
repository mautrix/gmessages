package libgm

import (
	"fmt"
	"log"
	"sync"

	"go.mau.fi/mautrix-gmessages/libgm/pblite"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/routes"
)

type ResponseChan struct {
	response *pblite.Response
	wg       sync.WaitGroup
	mu       sync.Mutex
}

func (s *SessionHandler) addRequestToChannel(requestId string, actionType binary.ActionType) {
	_, notOk := routes.Routes[actionType]
	if !notOk {
		log.Println("Missing action type: ", actionType)
		log.Fatal(notOk)
	}
	if msgMap, ok := s.requests[requestId]; ok {
		responseChan := &ResponseChan{
			response: &pblite.Response{},
			wg:       sync.WaitGroup{},
			mu:       sync.Mutex{},
		}
		responseChan.wg.Add(1)
		responseChan.mu.Lock()
		msgMap[actionType] = responseChan
	} else {
		s.requests[requestId] = make(map[binary.ActionType]*ResponseChan)
		responseChan := &ResponseChan{
			response: &pblite.Response{},
			wg:       sync.WaitGroup{},
			mu:       sync.Mutex{},
		}
		responseChan.wg.Add(1)
		responseChan.mu.Lock()
		s.requests[requestId][actionType] = responseChan
	}
}

func (s *SessionHandler) respondToRequestChannel(res *pblite.Response) {
	requestId := res.Data.RequestId
	reqChannel, ok := s.requests[requestId]
	actionType := res.Data.Action
	if !ok {
		s.client.Logger.Debug().Any("actionType", actionType).Any("requestId", requestId).Msg("Did not expect response for this requestId")
		return
	}
	actionResponseChan, ok2 := reqChannel[actionType]
	if !ok2 {
		s.client.Logger.Debug().Any("actionType", actionType).Any("requestId", requestId).Msg("Did not expect response for this actionType")
		return
	}
	actionResponseChan.mu.Lock()
	actionResponseChan, ok2 = reqChannel[actionType]
	if !ok2 {
		s.client.Logger.Debug().Any("actionType", actionType).Any("requestId", requestId).Msg("Ignoring request for action...")
		return
	}
	s.client.Logger.Debug().Any("actionType", actionType).Any("requestId", requestId).Msg("responding to request")
	actionResponseChan.response = res
	actionResponseChan.wg.Done()

	delete(reqChannel, actionType)
	if len(reqChannel) == 0 {
		delete(s.requests, requestId)
	}

	actionResponseChan.mu.Unlock()
}

func (s *SessionHandler) WaitForResponse(requestId string, actionType binary.ActionType) (*pblite.Response, error) {
	requestResponses, ok := s.requests[requestId]
	if !ok {
		return nil, fmt.Errorf("no response channel found for request ID: %s (actionType: %v)", requestId, actionType)
	}

	routeInfo, notFound := routes.Routes[actionType]
	if !notFound {
		return nil, fmt.Errorf("no action exists for actionType: %v (requestId: %s)", actionType, requestId)
	}

	responseChan, ok2 := requestResponses[routeInfo.Action]
	if !ok2 {
		return nil, fmt.Errorf("no response channel found for actionType: %v (requestId: %s)", routeInfo.Action, requestId)
	}

	responseChan.mu.Unlock()

	responseChan.wg.Wait()

	return responseChan.response, nil
}
