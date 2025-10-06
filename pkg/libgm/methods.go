package libgm

import (
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
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

func (c *Client) DeleteConversation(conversationID, phone string) error {
	msgType := gmproto.MessageType_BUGLE_MESSAGE
	deleteData := gmproto.DeleteConversationData{
		ConversationID: conversationID,
	}
	if phone != "" {
		deleteData.Phone = phone
	}
	_, err := c.sessionHandler.sendMessageWithParams(SendMessageParams{
		Action: gmproto.ActionType_UPDATE_CONVERSATION,
		Data: &gmproto.UpdateConversationRequest{
			Action:         gmproto.ConversationActionStatus_DELETE,
			ConversationID: conversationID,
			Data: &gmproto.UpdateConversationRequest_DeleteData{
				DeleteData: &deleteData,
			},
		},
		MessageType: msgType,
	})
	return err
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
	payload := &gmproto.GetConversationTypeRequest{ConversationID: conversationID}
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
	payload := &gmproto.ListMessagesRequest{ConversationID: conversationID, Count: count, Cursor: cursor}
	actionType := gmproto.ActionType_LIST_MESSAGES
	return typedResponse[*gmproto.ListMessagesResponse](c.sessionHandler.sendMessage(actionType, payload))
}

func (c *Client) SendMessage(payload *gmproto.SendMessageRequest) (*gmproto.SendMessageResponse, error) {
	actionType := gmproto.ActionType_SEND_MESSAGE
	return typedResponse[*gmproto.SendMessageResponse](c.sessionHandler.sendMessage(actionType, payload))
}

func (c *Client) GetParticipantThumbnail(participantIDs ...string) (*gmproto.GetThumbnailResponse, error) {
	payload := &gmproto.GetThumbnailRequest{Identifiers: participantIDs}
	actionType := gmproto.ActionType_GET_PARTICIPANTS_THUMBNAIL
	return typedResponse[*gmproto.GetThumbnailResponse](c.sessionHandler.sendMessage(actionType, payload))
}

func (c *Client) GetContactThumbnail(contactIDs ...string) (*gmproto.GetThumbnailResponse, error) {
	payload := &gmproto.GetThumbnailRequest{Identifiers: contactIDs}
	actionType := gmproto.ActionType_GET_CONTACTS_THUMBNAIL
	return typedResponse[*gmproto.GetThumbnailResponse](c.sessionHandler.sendMessage(actionType, payload))
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

func (c *Client) SetTyping(convID string, simPayload *gmproto.SIMPayload) error {
	return c.sessionHandler.sendMessageNoResponse(SendMessageParams{
		Action: gmproto.ActionType_TYPING_UPDATES,
		Data: &gmproto.TypingUpdateRequest{
			Data:       &gmproto.TypingUpdateRequest_Data{ConversationID: convID, Typing: true},
			SIMPayload: simPayload,
		},
	})
}

func (c *Client) UpdateSettings(payload *gmproto.SettingsUpdateRequest) error {
	return c.sessionHandler.sendMessageNoResponse(SendMessageParams{
		Action: gmproto.ActionType_SETTINGS_UPDATE,
		Data:   payload,
	})
}

func (c *Client) SetActiveSession() error {
	c.sessionHandler.ResetSessionID()
	return c.sessionHandler.sendMessageNoResponse(SendMessageParams{
		Action:    gmproto.ActionType_GET_UPDATES,
		OmitTTL:   true,
		RequestID: c.sessionHandler.sessionID,
	})
}

func (c *Client) IsBugleDefault() (*gmproto.IsBugleDefaultResponse, error) {
	actionType := gmproto.ActionType_IS_BUGLE_DEFAULT
	return typedResponse[*gmproto.IsBugleDefaultResponse](c.sessionHandler.sendMessage(actionType, nil))
}

func (c *Client) NotifyDittoActivity() (<-chan *IncomingRPCMessage, error) {
	return c.sessionHandler.sendAsyncMessage(SendMessageParams{
		Action: gmproto.ActionType_NOTIFY_DITTO_ACTIVITY,
		Data:   &gmproto.NotifyDittoActivityRequest{Success: true},
	})
}

func (c *Client) GetFullSizeImage(messageID, actionMessageID string) (*gmproto.GetFullSizeImageResponse, error) {
	payload := &gmproto.GetFullSizeImageRequest{MessageID: messageID, ActionMessageID: actionMessageID}
	actionType := gmproto.ActionType_GET_FULL_SIZE_IMAGE

	return typedResponse[*gmproto.GetFullSizeImageResponse](c.sessionHandler.sendMessage(actionType, payload))
}
