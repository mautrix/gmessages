package textgapi

import (
	"fmt"
	"log"
	"sync"
)

type ResponseChan struct {
	responses         []*Response
	receivedResponses int64
	wg                sync.WaitGroup
	mu                sync.Mutex
}

func (s *SessionHandler) addRequestToChannel(requestId string, opCode int64) {
	instruction, notOk := s.client.instructions.GetInstruction(opCode)
	if !notOk {
		log.Fatal(notOk)
	}
	if msgMap, ok := s.requests[requestId]; ok {
		responseChan := &ResponseChan{
			responses:         make([]*Response, 0, instruction.ExpectedResponses),
			receivedResponses: 0,
			wg:                sync.WaitGroup{},
			mu:                sync.Mutex{},
		}
		msgMap[opCode] = responseChan
		responseChan.wg.Add(int(instruction.ExpectedResponses))
		responseChan.mu.Lock()
	} else {
		s.requests[requestId] = make(map[int64]*ResponseChan)
		responseChan := &ResponseChan{
			responses:         make([]*Response, 0, instruction.ExpectedResponses),
			receivedResponses: 0,
			wg:                sync.WaitGroup{},
			mu:                sync.Mutex{},
		}
		s.requests[requestId][opCode] = responseChan
		responseChan.wg.Add(int(instruction.ExpectedResponses))
		responseChan.mu.Lock()
	}
}

func (s *SessionHandler) respondToRequestChannel(res *Response) {
	requestId := res.Data.RequestId
	reqChannel, ok := s.requests[requestId]
	if !ok {
		return
	}
	opCodeResponseChan, ok2 := reqChannel[res.Data.Opcode]
	if !ok2 {
		return
	}

	opCodeResponseChan.mu.Lock()

	opCodeResponseChan.responses = append(opCodeResponseChan.responses, res)

	s.client.Logger.Debug().Any("opcode", res.Data.Opcode).Msg("Got response")

	instruction, ok3 := s.client.instructions.GetInstruction(res.Data.Opcode)
	if opCodeResponseChan.receivedResponses >= instruction.ExpectedResponses {
		s.client.Logger.Debug().Any("opcode", res.Data.Opcode).Msg("Ignoring opcode")
		return
	}
	opCodeResponseChan.receivedResponses++
	opCodeResponseChan.wg.Done()
	if !ok3 {
		log.Fatal(ok3)
		opCodeResponseChan.mu.Unlock()
		return
	}
	if opCodeResponseChan.receivedResponses >= instruction.ExpectedResponses {
		delete(reqChannel, res.Data.Opcode)
		if len(reqChannel) == 0 {
			delete(s.requests, requestId)
		}
	}

	opCodeResponseChan.mu.Unlock()
}

func (s *SessionHandler) WaitForResponse(requestId string, opCode int64) ([]*Response, error) {
	requestResponses, ok := s.requests[requestId]
	if !ok {
		return nil, fmt.Errorf("no response channel found for request ID: %s (opcode: %v)", requestId, opCode)
	}
	responseChan, ok2 := requestResponses[opCode]
	if !ok2 {
		return nil, fmt.Errorf("no response channel found for opCode: %v (requestId: %s)", opCode, requestId)
	}

	// Unlock so responses can be received
	responseChan.mu.Unlock()

	// Wait for all responses to be received
	responseChan.wg.Wait()

	return responseChan.responses, nil
}
