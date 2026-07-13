package libgm

import (
	"context"

	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

func (c *Client) ListConversations(ctx context.Context, count int, folder gmproto.ListConversationsRequest_Folder) (*gmproto.ListConversationsResponse, error) {
	msgType := gmproto.MessageType_BUGLE_MESSAGE
	if !c.conversationsFetchedOnce {
		msgType = gmproto.MessageType_BUGLE_ANNOTATION
		c.conversationsFetchedOnce = true
	}
	return typedResponse[*gmproto.ListConversationsResponse](c.sessionHandler.sendMessageWithParams(ctx, SendMessageParams{
		Action:      gmproto.ActionType_LIST_CONVERSATIONS,
		Data:        &gmproto.ListConversationsRequest{Count: int64(count), Folder: folder},
		MessageType: msgType,
	}))
}

func (c *Client) DeleteConversation(ctx context.Context, conversationID, phone string) error {
	_, err := c.UpdateConversation(ctx, &gmproto.UpdateConversationRequest{
		Action:         gmproto.ConversationActionStatus_DELETE,
		ConversationID: conversationID,
		Data: &gmproto.UpdateConversationRequest_DeleteData{
			DeleteData: &gmproto.DeleteConversationData{
				ConversationID: conversationID,
				Phone:          phone,
			},
		},
	})
	return err
}

func (c *Client) ListContacts(ctx context.Context) (*gmproto.ListContactsResponse, error) {
	payload := &gmproto.ListContactsRequest{
		I1: 1,
		I2: 350,
		I3: 50,
	}
	actionType := gmproto.ActionType_LIST_CONTACTS
	return typedResponse[*gmproto.ListContactsResponse](c.sessionHandler.sendMessage(ctx, actionType, payload))
}

func (c *Client) ListTopContacts(ctx context.Context) (*gmproto.ListTopContactsResponse, error) {
	payload := &gmproto.ListTopContactsRequest{
		Count: 8,
	}
	actionType := gmproto.ActionType_LIST_TOP_CONTACTS
	return typedResponse[*gmproto.ListTopContactsResponse](c.sessionHandler.sendMessage(ctx, actionType, payload))
}

func (c *Client) GetOrCreateConversation(ctx context.Context, req *gmproto.GetOrCreateConversationRequest) (*gmproto.GetOrCreateConversationResponse, error) {
	actionType := gmproto.ActionType_GET_OR_CREATE_CONVERSATION
	return typedResponse[*gmproto.GetOrCreateConversationResponse](c.sessionHandler.sendMessage(ctx, actionType, req))
}

func (c *Client) GetConversationType(ctx context.Context, conversationID string) (*gmproto.GetConversationTypeResponse, error) {
	payload := &gmproto.GetConversationTypeRequest{ConversationID: conversationID}
	actionType := gmproto.ActionType_GET_CONVERSATION_TYPE
	return typedResponse[*gmproto.GetConversationTypeResponse](c.sessionHandler.sendMessage(ctx, actionType, payload))
}

func (c *Client) GetConversation(ctx context.Context, conversationID string) (*gmproto.Conversation, error) {
	payload := &gmproto.GetConversationRequest{ConversationID: conversationID}
	actionType := gmproto.ActionType_GET_CONVERSATION
	resp, err := typedResponse[*gmproto.GetConversationResponse](c.sessionHandler.sendMessage(ctx, actionType, payload))
	if err != nil {
		return nil, err
	}
	return resp.GetConversation(), nil
}

func (c *Client) FetchMessages(ctx context.Context, conversationID string, count int64, cursor *gmproto.Cursor) (*gmproto.ListMessagesResponse, error) {
	payload := &gmproto.ListMessagesRequest{ConversationID: conversationID, Count: count, Cursor: cursor}
	actionType := gmproto.ActionType_LIST_MESSAGES
	return typedResponse[*gmproto.ListMessagesResponse](c.sessionHandler.sendMessage(ctx, actionType, payload))
}

func (c *Client) SendMessage(ctx context.Context, payload *gmproto.SendMessageRequest) (*gmproto.SendMessageResponse, error) {
	actionType := gmproto.ActionType_SEND_MESSAGE
	return typedResponse[*gmproto.SendMessageResponse](c.sessionHandler.sendMessage(ctx, actionType, payload))
}

func (c *Client) GetParticipantThumbnail(ctx context.Context, participantIDs ...string) (*gmproto.GetThumbnailResponse, error) {
	payload := &gmproto.GetThumbnailRequest{Identifiers: participantIDs}
	actionType := gmproto.ActionType_GET_PARTICIPANTS_THUMBNAIL
	return typedResponse[*gmproto.GetThumbnailResponse](c.sessionHandler.sendMessage(ctx, actionType, payload))
}

func (c *Client) GetContactThumbnail(ctx context.Context, contactIDs ...string) (*gmproto.GetThumbnailResponse, error) {
	payload := &gmproto.GetThumbnailRequest{Identifiers: contactIDs}
	actionType := gmproto.ActionType_GET_CONTACTS_THUMBNAIL
	return typedResponse[*gmproto.GetThumbnailResponse](c.sessionHandler.sendMessage(ctx, actionType, payload))
}

func (c *Client) UpdateConversation(ctx context.Context, payload *gmproto.UpdateConversationRequest) (*gmproto.UpdateConversationResponse, error) {
	actionType := gmproto.ActionType_UPDATE_CONVERSATION
	return typedResponse[*gmproto.UpdateConversationResponse](c.sessionHandler.sendMessage(ctx, actionType, payload))
}

func (c *Client) SendReaction(ctx context.Context, payload *gmproto.SendReactionRequest) (*gmproto.SendReactionResponse, error) {
	actionType := gmproto.ActionType_SEND_REACTION
	return typedResponse[*gmproto.SendReactionResponse](c.sessionHandler.sendMessage(ctx, actionType, payload))
}

func (c *Client) DeleteMessage(ctx context.Context, messageID string) (*gmproto.DeleteMessageResponse, error) {
	payload := &gmproto.DeleteMessageRequest{MessageID: messageID}
	actionType := gmproto.ActionType_DELETE_MESSAGE

	return typedResponse[*gmproto.DeleteMessageResponse](c.sessionHandler.sendMessage(ctx, actionType, payload))
}

func (c *Client) MarkRead(ctx context.Context, conversationID, messageID string) error {
	payload := &gmproto.MessageReadRequest{ConversationID: conversationID, MessageID: messageID}
	actionType := gmproto.ActionType_MESSAGE_READ

	_, err := c.sessionHandler.sendMessage(ctx, actionType, payload)
	return err
}

func (c *Client) SetTyping(ctx context.Context, convID string, simPayload *gmproto.SIMPayload) error {
	return c.sessionHandler.sendMessageNoResponse(ctx, SendMessageParams{
		Action: gmproto.ActionType_TYPING_UPDATES,
		Data: &gmproto.TypingUpdateRequest{
			Data:       &gmproto.TypingUpdateRequest_Data{ConversationID: convID, Typing: true},
			SIMPayload: simPayload,
		},
	})
}

func (c *Client) UpdateSettings(ctx context.Context, payload *gmproto.SettingsUpdateRequest) error {
	return c.sessionHandler.sendMessageNoResponse(ctx, SendMessageParams{
		Action: gmproto.ActionType_SETTINGS_UPDATE,
		Data:   payload,
	})
}

func (c *Client) SetActiveSession(ctx context.Context) error {
	c.sessionHandler.ResetSessionID()
	return c.sessionHandler.sendMessageNoResponse(ctx, SendMessageParams{
		Action:    gmproto.ActionType_GET_UPDATES,
		OmitTTL:   true,
		RequestID: c.sessionHandler.sessionID,
	})
}

func (c *Client) IsBugleDefault(ctx context.Context) (*gmproto.IsBugleDefaultResponse, error) {
	actionType := gmproto.ActionType_IS_BUGLE_DEFAULT
	return typedResponse[*gmproto.IsBugleDefaultResponse](c.sessionHandler.sendMessage(ctx, actionType, nil))
}

func (c *Client) NotifyDittoActivity(ctx context.Context) (<-chan *IncomingRPCMessage, error) {
	return c.sessionHandler.sendAsyncMessage(ctx, SendMessageParams{
		Action: gmproto.ActionType_NOTIFY_DITTO_ACTIVITY,
		Data:   &gmproto.NotifyDittoActivityRequest{Success: true},
	})
}

func (c *Client) ackBrowserPresence(ctx context.Context) {
	err := c.sessionHandler.sendMessageNoResponse(ctx, SendMessageParams{
		Action: gmproto.ActionType_ACK_BROWSER_PRESENCE,
	})
	if err != nil {
		c.Logger.Warn().Err(err).Msg("Failed to ack browser presence")
	}
}

func (c *Client) GetFullSizeImage(ctx context.Context, messageID, actionMessageID string) (*gmproto.GetFullSizeImageResponse, error) {
	payload := &gmproto.GetFullSizeImageRequest{MessageID: messageID, ActionMessageID: actionMessageID}
	actionType := gmproto.ActionType_GET_FULL_SIZE_IMAGE

	return typedResponse[*gmproto.GetFullSizeImageResponse](c.sessionHandler.sendMessage(ctx, actionType, payload))
}
