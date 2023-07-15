package libgm

import (
	"fmt"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
)

func (c *Client) SendReaction(payload *binary.SendReactionPayload) (*binary.SendReactionResponse, error) {
	actionType := binary.ActionType_SEND_REACTION

	response, err := c.sessionHandler.sendMessage(actionType, payload)
	if err != nil {
		return nil, err
	}

	res, ok := response.Data.Decrypted.(*binary.SendReactionResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T, expected *binary.SendReactionResponse", response.Data.Decrypted)
	}

	return res, nil
}

func (c *Client) DeleteMessage(messageID string) (*binary.DeleteMessageResponse, error) {
	payload := &binary.DeleteMessagePayload{MessageID: messageID}
	actionType := binary.ActionType_DELETE_MESSAGE

	response, err := c.sessionHandler.sendMessage(actionType, payload)
	if err != nil {
		return nil, err
	}

	res, ok := response.Data.Decrypted.(*binary.DeleteMessageResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T, expected *binary.DeleteMessagesResponse", response.Data.Decrypted)
	}

	return res, nil
}

func (c *Client) MarkRead(conversationID, messageID string) error {
	payload := &binary.MessageReadPayload{ConversationID: conversationID, MessageID: messageID}
	actionType := binary.ActionType_MESSAGE_READ

	_, err := c.sessionHandler.sendMessage(actionType, payload)
	return err
}
