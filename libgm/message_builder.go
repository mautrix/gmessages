package libgm

import (
	"errors"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

var (
	errContentNotSet           = errors.New("failed to build MessageBuilder: content must be larger than length 0")
	errConversationIdNotSet    = errors.New("failed to build MessageBuilder: conversationID is empty")
	errSelfParticipantIdNotSet = errors.New("failed to build MessageBuilder: selfParticipantID is empty")
)

type MessageBuilder struct {
	client *Client

	content           string
	conversationID    string
	tmpID             string
	selfParticipantID string

	replyToMessageID string

	images []*MediaUpload

	err error
}

func (mb *MessageBuilder) Err() error {
	return mb.err
}

func (mb *MessageBuilder) GetImages() []*MediaUpload {
	return mb.images
}

func (mb *MessageBuilder) GetContent() string {
	return mb.content
}

func (mb *MessageBuilder) GetConversationID() string {
	return mb.conversationID
}

func (mb *MessageBuilder) GetSelfParticipantID() string {
	return mb.selfParticipantID
}

func (mb *MessageBuilder) GetTmpID() string {
	return mb.tmpID
}

func (mb *MessageBuilder) SetContent(content string) *MessageBuilder {
	mb.content = content
	return mb
}

func (mb *MessageBuilder) SetConversationID(conversationId string) *MessageBuilder {
	mb.conversationID = conversationId
	return mb
}

// sendmessage function will set this automatically but if u want to set it yourself feel free
func (mb *MessageBuilder) SetSelfParticipantID(participantId string) *MessageBuilder {
	mb.selfParticipantID = participantId
	return mb
}

// messageID of the message to reply to
func (mb *MessageBuilder) SetReplyMessage(messageId string) *MessageBuilder {
	mb.replyToMessageID = messageId
	return mb
}

// sendmessage function will set this automatically but if u want to set it yourself feel free
func (mb *MessageBuilder) SetTmpID(tmpId string) *MessageBuilder {
	mb.tmpID = tmpId
	return mb
}

func (mb *MessageBuilder) Build() (*binary.SendMessagePayload, error) {

	if mb.conversationID == "" {
		return nil, errConversationIdNotSet
	}

	if mb.selfParticipantID == "" {
		return nil, errSelfParticipantIdNotSet
	}

	if mb.content == "" {
		return nil, errContentNotSet
	}

	if mb.tmpID == "" {
		mb.tmpID = util.GenerateTmpId()
	}

	return mb.newSendConversationMessage(), nil
}

func (c *Client) NewMessageBuilder() *MessageBuilder {
	mb := &MessageBuilder{
		client: c,
	}

	tmpId := util.GenerateTmpId()
	mb.SetTmpID(tmpId)

	return mb
}

func (mb *MessageBuilder) newSendConversationMessage() *binary.SendMessagePayload {

	convId := mb.GetConversationID()
	content := mb.GetContent()
	selfParticipantId := mb.GetSelfParticipantID()
	tmpId := mb.GetTmpID()

	messageInfo := make([]*binary.MessageInfo, 0)
	messageInfo = append(messageInfo, &binary.MessageInfo{Data: &binary.MessageInfo_MessageContent{
		MessageContent: &binary.MessageContent{
			Content: content,
		},
	}})

	mb.appendImagesPayload(&messageInfo)

	sendMsgPayload := &binary.SendMessagePayload{
		ConversationID: convId,
		MessagePayload: &binary.MessagePayload{
			TmpID:             tmpId,
			ConversationID:    convId,
			SelfParticipantID: selfParticipantId,
			MessageInfo:       messageInfo,
			TmpID2:            tmpId,
		},
		TmpID: tmpId,
	}

	if len(content) > 0 {
		sendMsgPayload.MessagePayload.MessagePayloadContent = &binary.MessagePayloadContent{
			MessageContent: &binary.MessageContent{
				Content: content,
			},
		}
	}

	if mb.replyToMessageID != "" {
		sendMsgPayload.IsReply = true
		sendMsgPayload.Reply = &binary.ReplyPayload{MessageID: mb.replyToMessageID}
	}

	mb.client.Logger.Debug().Any("sendMsgPayload", sendMsgPayload).Msg("sendMessagePayload")

	return sendMsgPayload
}

func (mb *MessageBuilder) appendImagesPayload(messageInfo *[]*binary.MessageInfo) {
	if len(mb.images) <= 0 {
		return
	}

	for _, media := range mb.images {
		imgData := mb.newImageContent(media)
		*messageInfo = append(*messageInfo, imgData)
	}
}

func (mb *MessageBuilder) newImageContent(media *MediaUpload) *binary.MessageInfo {
	imageMessage := &binary.MessageInfo{
		Data: &binary.MessageInfo_MediaContent{
			MediaContent: &binary.MediaContent{
				Format:        binary.MediaFormats(media.Image.GetImageType().Type),
				MediaID:       media.MediaID,
				MediaName:     media.Image.GetImageName(),
				Size:          media.Image.GetImageSize(),
				DecryptionKey: media.Image.GetImageCryptor().GetKey(),
			},
		},
	}
	mb.client.Logger.Debug().Any("imageMessage", imageMessage).Msg("New Media Content")
	return imageMessage
}
