package libgm

import (
	"fmt"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
)

func (c *Client) SetActiveSession() error {
	c.sessionHandler.ResetSessionId()

	actionType := binary.ActionType_GET_UPDATES
	_, sendErr := c.sessionHandler.completeSendMessage(actionType, false, nil)
	if sendErr != nil {
		return sendErr
	}
	return nil
}

func (c *Client) IsBugleDefault() (*binary.IsBugleDefaultResponse, error) {
	c.sessionHandler.ResetSessionId()

	actionType := binary.ActionType_IS_BUGLE_DEFAULT
	sentRequestId, sendErr := c.sessionHandler.completeSendMessage(actionType, true, nil)
	if sendErr != nil {
		return nil, sendErr
	}

	response, err := c.sessionHandler.WaitForResponse(sentRequestId, actionType)
	if err != nil {
		return nil, err
	}

	res, ok := response.Data.Decrypted.(*binary.IsBugleDefaultResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T, expected *binary.IsBugleDefaultResponse", response.Data.Decrypted)
	}

	return res, nil
}

func (c *Client) NotifyDittoActivity() error {
	payload := &binary.NotifyDittoActivityPayload{Success: true}
	actionType := binary.ActionType_NOTIFY_DITTO_ACTIVITY

	sentRequestId, sendErr := c.sessionHandler.completeSendMessage(actionType, true, payload)
	if sendErr != nil {
		return sendErr
	}

	_, err := c.sessionHandler.WaitForResponse(sentRequestId, actionType)
	if err != nil {
		return err
	}

	return nil
}
