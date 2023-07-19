package libgm

import (
	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
)

func (c *Client) ListConversations(count int, folder gmproto.ListConversationsRequest_Folder) (*gmproto.ListConversationsResponse, error) {
	msgType := gmproto.MessageType_BUGLE_MESSAGE
	if !c.conversationsFetchedOnce {
		msgType = gmproto.MessageType_BUGLE_ANNOTATION
		c.conversationsFetchedOnce = true
	}
	return typedResponse[*gmproto.ListConversationsResponse](c.sessionHandler.sendMessageWithParams(SendMessageParams{
		Action:      gmproto.ActionType_LIST_CONVERSATIONS,
		Data:        &gmproto.ListConversationsRequest{Count: int64(count), Folder: folder},
		MessageType: msgType,
	}))
}

func (c *Client) ListContacts() (*gmproto.ListContactsResponse, error) {
	payload := &gmproto.ListContactsRequest{
		I1: 1,
		I2: 350,
		I3: 50,
	}
	actionType := gmproto.ActionType_LIST_CONTACTS
	return typedResponse[*gmproto.ListContactsResponse](c.sessionHandler.sendMessage(actionType, payload))
}

func (c *Client) ListTopContacts() (*gmproto.ListTopContactsResponse, error) {
	payload := &gmproto.ListTopContactsRequest{
		Count: 8,
	}
	actionType := gmproto.ActionType_LIST_TOP_CONTACTS
	return typedResponse[*gmproto.ListTopContactsResponse](c.sessionHandler.sendMessage(actionType, payload))
}

func (c *Client) GetOrCreateConversation(req *gmproto.GetOrCreateConversationRequest) (*gmproto.GetOrCreateConversationResponse, error) {
	actionType := gmproto.ActionType_GET_OR_CREATE_CONVERSATION
	return typedResponse[*gmproto.GetOrCreateConversationResponse](c.sessionHandler.sendMessage(actionType, req))
}

func (c *Client) GetConversationType(conversationID string) (*gmproto.GetConversationTypeResponse, error) {
	payload := &gmproto.ConversationTypeRequest{ConversationID: conversationID}
	actionType := gmproto.ActionType_GET_CONVERSATION_TYPE
	return typedResponse[*gmproto.GetConversationTypeResponse](c.sessionHandler.sendMessage(actionType, payload))
}

func (c *Client) GetConversation(conversationID string) (*gmproto.Conversation, error) {
	payload := &gmproto.GetConversationRequest{ConversationID: conversationID}
	actionType := gmproto.ActionType_GET_CONVERSATION
	resp, err := typedResponse[*gmproto.GetConversationResponse](c.sessionHandler.sendMessage(actionType, payload))
	if err != nil {
		return nil, err
	}
	return resp.GetConversation(), nil
}

func (c *Client) FetchMessages(conversationID string, count int64, cursor *gmproto.Cursor) (*gmproto.ListMessagesResponse, error) {
	payload := &gmproto.ListMessagesRequest{ConversationID: conversationID, Count: count}
	if cursor != nil {
		payload.Cursor = cursor
	}
	actionType := gmproto.ActionType_LIST_MESSAGES
	return typedResponse[*gmproto.ListMessagesResponse](c.sessionHandler.sendMessage(actionType, payload))
}

func (c *Client) SendMessage(payload *gmproto.SendMessageRequest) (*gmproto.SendMessageResponse, error) {
	actionType := gmproto.ActionType_SEND_MESSAGE
	return typedResponse[*gmproto.SendMessageResponse](c.sessionHandler.sendMessage(actionType, payload))
}

func (c *Client) GetParticipantThumbnail(convID string) (*gmproto.GetParticipantThumbnailResponse, error) {
	payload := &gmproto.GetParticipantThumbnailRequest{ConversationID: convID}
	actionType := gmproto.ActionType_GET_PARTICIPANTS_THUMBNAIL
	return typedResponse[*gmproto.GetParticipantThumbnailResponse](c.sessionHandler.sendMessage(actionType, payload))
}

func (c *Client) UpdateConversation(payload *gmproto.UpdateConversationRequest) (*gmproto.UpdateConversationResponse, error) {
	actionType := gmproto.ActionType_UPDATE_CONVERSATION
	return typedResponse[*gmproto.UpdateConversationResponse](c.sessionHandler.sendMessage(actionType, payload))
}

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

func (c *Client) SetTyping(convID string) error {
	payload := &gmproto.TypingUpdateRequest{
		Data: &gmproto.TypingUpdateRequest_Data{ConversationID: convID, Typing: true},
	}
	actionType := gmproto.ActionType_TYPING_UPDATES

	_, err := c.sessionHandler.sendMessage(actionType, payload)
	return err
}

func (c *Client) SetActiveSession() error {
	c.sessionHandler.ResetSessionID()
	return c.sessionHandler.sendMessageNoResponse(SendMessageParams{
		Action:       gmproto.ActionType_GET_UPDATES,
		OmitTTL:      true,
		UseSessionID: true,
	})
}

func (c *Client) IsBugleDefault() (*gmproto.IsBugleDefaultResponse, error) {
	c.sessionHandler.ResetSessionID()
	actionType := gmproto.ActionType_IS_BUGLE_DEFAULT
	return typedResponse[*gmproto.IsBugleDefaultResponse](c.sessionHandler.sendMessage(actionType, nil))
}

func (c *Client) NotifyDittoActivity() error {
	payload := &gmproto.NotifyDittoActivityRequest{Success: true}
	actionType := gmproto.ActionType_NOTIFY_DITTO_ACTIVITY

	_, err := c.sessionHandler.sendMessage(actionType, payload)
	return err
}
