package libgm

import (
	"fmt"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
)

func (c *Client) SendReaction(payload *binary.SendReactionPayload) (*binary.SendReactionResponse, error) {
	actionType := binary.ActionType_SEND_REACTION

	sentRequestId, sendErr := c.sessionHandler.completeSendMessage(actionType, true, payload)
	if sendErr != nil {
		return nil, sendErr
	}

	response, err := c.sessionHandler.WaitForResponse(sentRequestId, actionType)
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

	sentRequestId, sendErr := c.sessionHandler.completeSendMessage(actionType, true, payload)
	if sendErr != nil {
		return nil, sendErr
	}

	response, err := c.sessionHandler.WaitForResponse(sentRequestId, actionType)
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
