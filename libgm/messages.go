package libgm

import (
	"fmt"

	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
)

func (c *Client) SendReaction(payload *gmproto.SendReactionPayload) (*gmproto.SendReactionResponse, error) {
	actionType := gmproto.ActionType_SEND_REACTION

	response, err := c.sessionHandler.sendMessage(actionType, payload)
	if err != nil {
		return nil, err
	}

	res, ok := response.Data.Decrypted.(*gmproto.SendReactionResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T, expected *gmproto.SendReactionResponse", response.Data.Decrypted)
	}

	return res, nil
}

func (c *Client) DeleteMessage(messageID string) (*gmproto.DeleteMessageResponse, error) {
	payload := &gmproto.DeleteMessagePayload{MessageID: messageID}
	actionType := gmproto.ActionType_DELETE_MESSAGE

	response, err := c.sessionHandler.sendMessage(actionType, payload)
	if err != nil {
		return nil, err
	}

	res, ok := response.Data.Decrypted.(*gmproto.DeleteMessageResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T, expected *gmproto.DeleteMessagesResponse", response.Data.Decrypted)
	}

	return res, nil
}

func (c *Client) MarkRead(conversationID, messageID string) error {
	payload := &gmproto.MessageReadPayload{ConversationID: conversationID, MessageID: messageID}
	actionType := gmproto.ActionType_MESSAGE_READ

	_, err := c.sessionHandler.sendMessage(actionType, payload)
	return err
}
