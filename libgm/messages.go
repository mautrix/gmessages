package libgm

import (
	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
)

func (c *Client) SendReaction(payload *gmproto.SendReactionRequest) (*gmproto.SendReactionResponse, error) {
	actionType := gmproto.ActionType_SEND_REACTION
	return typedResponse[*gmproto.SendReactionResponse](c.sessionHandler.sendMessage(actionType, payload))
}

func (c *Client) DeleteMessage(messageID string) (*gmproto.DeleteMessageResponse, error) {
	payload := &gmproto.DeleteMessageRequest{MessageID: messageID}
	actionType := gmproto.ActionType_DELETE_MESSAGE

	return typedResponse[*gmproto.DeleteMessageResponse](c.sessionHandler.sendMessage(actionType, payload))
}

func (c *Client) MarkRead(conversationID, messageID string) error {
	payload := &gmproto.MessageReadRequest{ConversationID: conversationID, MessageID: messageID}
	actionType := gmproto.ActionType_MESSAGE_READ

	_, err := c.sessionHandler.sendMessage(actionType, payload)
	return err
}
