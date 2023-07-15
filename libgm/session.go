package libgm

import (
	"fmt"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
)

func (c *Client) SetActiveSession() error {
	c.sessionHandler.ResetSessionID()
	actionType := binary.ActionType_GET_UPDATES
	return c.sessionHandler.sendMessageNoResponse(actionType, nil)
}

func (c *Client) IsBugleDefault() (*binary.IsBugleDefaultResponse, error) {
	c.sessionHandler.ResetSessionID()

	actionType := binary.ActionType_IS_BUGLE_DEFAULT

	response, err := c.sessionHandler.sendMessage(actionType, nil)
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

	_, err := c.sessionHandler.sendMessage(actionType, payload)
	return err
}
