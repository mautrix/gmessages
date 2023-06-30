package libgm

import "go.mau.fi/mautrix-gmessages/libgm/util"

type Session struct {
	client *Client

	prepareNewSession prepareNewSession
	newSession        newSession
}

func (s *Session) SetActiveSession() (*util.SessionResponse, error) {
	s.client.sessionHandler.ResetSessionId()

	prepareResponses, prepareSessionErr := s.prepareNewSession.Execute()
	if prepareSessionErr != nil {
		return nil, prepareSessionErr
	}

	newSessionResponses, newSessionErr := s.newSession.Execute()
	if newSessionErr != nil {
		return nil, newSessionErr
	}

	sessionResponse, processFail := s.client.processSessionResponse(prepareResponses, newSessionResponses)
	if processFail != nil {
		return nil, processFail
	}

	return sessionResponse, nil
}

type prepareNewSession struct {
	client *Client
}

func (p *prepareNewSession) Execute() ([]*Response, error) {
	instruction, _ := p.client.instructions.GetInstruction(PREPARE_NEW_SESSION_OPCODE)
	sentRequestId, _ := p.client.createAndSendRequest(instruction.Opcode, p.client.ttl, false, nil)

	responses, err := p.client.sessionHandler.WaitForResponse(sentRequestId, instruction.Opcode)
	if err != nil {
		return nil, err
	}

	return responses, nil
}

type newSession struct {
	client *Client
}

func (n *newSession) Execute() ([]*Response, error) {
	instruction, _ := n.client.instructions.GetInstruction(NEW_SESSION_OPCODE)
	sentRequestId, _ := n.client.createAndSendRequest(instruction.Opcode, 0, true, nil)

	responses, err := n.client.sessionHandler.WaitForResponse(sentRequestId, instruction.Opcode)
	if err != nil {
		return nil, err
	}

	// Rest of the processing...

	return responses, nil
}
