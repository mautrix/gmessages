package libgm

import (
	"fmt"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
)

type Session struct {
	client *Client
}

// start receiving updates from mobile on this session
func (s *Session) SetActiveSession() error {
	s.client.sessionHandler.ResetSessionId()

	actionType := binary.ActionType_GET_UPDATES
	_, sendErr := s.client.sessionHandler.completeSendMessage(actionType, false, nil)
	if sendErr != nil {
		return sendErr
	}
	return nil
}

func (s *Session) IsBugleDefault() (*binary.IsBugleDefaultResponse, error) {
	s.client.sessionHandler.ResetSessionId()

	actionType := binary.ActionType_IS_BUGLE_DEFAULT
	sentRequestId, sendErr := s.client.sessionHandler.completeSendMessage(actionType, true, nil)
	if sendErr != nil {
		return nil, sendErr
	}

	response, err := s.client.sessionHandler.WaitForResponse(sentRequestId, actionType)
	if err != nil {
		return nil, err
	}

	res, ok := response.Data.Decrypted.(*binary.IsBugleDefaultResponse)
	if !ok {
		return nil, fmt.Errorf("failed to assert response into IsBugleDefaultResponse")
	}

	return res, nil
}

func (s *Session) NotifyDittoActivity() error {
	payload := &binary.NotifyDittoActivityPayload{Success: true}
	actionType := binary.ActionType_NOTIFY_DITTO_ACTIVITY

	sentRequestId, sendErr := s.client.sessionHandler.completeSendMessage(actionType, true, payload)
	if sendErr != nil {
		return sendErr
	}

	_, err := s.client.sessionHandler.WaitForResponse(sentRequestId, actionType)
	if err != nil {
		return err
	}

	return nil
}
