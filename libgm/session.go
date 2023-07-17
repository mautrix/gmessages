package libgm

import (
	"fmt"

	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
)

func (c *Client) SetActiveSession() error {
	c.sessionHandler.ResetSessionID()
	actionType := gmproto.ActionType_GET_UPDATES
	return c.sessionHandler.sendMessageNoResponse(actionType, nil)
}

func (c *Client) IsBugleDefault() (*gmproto.IsBugleDefaultResponse, error) {
	c.sessionHandler.ResetSessionID()

	actionType := gmproto.ActionType_IS_BUGLE_DEFAULT

	response, err := c.sessionHandler.sendMessage(actionType, nil)
	if err != nil {
		return nil, err
	}

	res, ok := response.DecryptedMessage.(*gmproto.IsBugleDefaultResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T, expected *gmproto.IsBugleDefaultResponse", response.DecryptedMessage)
	}

	return res, nil
}

func (c *Client) NotifyDittoActivity() error {
	payload := &gmproto.NotifyDittoActivityPayload{Success: true}
	actionType := gmproto.ActionType_NOTIFY_DITTO_ACTIVITY

	_, err := c.sessionHandler.sendMessage(actionType, payload)
	return err
}
