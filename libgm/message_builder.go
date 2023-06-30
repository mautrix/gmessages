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

	images []*MediaUpload

	err error
}

// Add this method to retrieve the stored error
func (mb *MessageBuilder) Err() error {
	return mb.err
}

func (mb *MessageBuilder) GetImages() []*MediaUpload {
	return mb.images
}

func (mb *MessageBuilder) GetContent() string {
	return mb.content
}

func (mb *MessageBuilder) SetContent(content string) *MessageBuilder {
	mb.content = content
	return mb
}

func (mb *MessageBuilder) GetConversationID() string {
	return mb.conversationID
}

func (mb *MessageBuilder) SetConversationID(conversationId string) *MessageBuilder {
	mb.conversationID = conversationId
	return mb
}

func (mb *MessageBuilder) GetSelfParticipantID() string {
	return mb.selfParticipantID
}

// sendmessage function will set this automatically but if u want to set it yourself feel free
func (mb *MessageBuilder) SetSelfParticipantID(participantId string) *MessageBuilder {
	mb.selfParticipantID = participantId
	return mb
}

func (mb *MessageBuilder) GetTmpID() string {
	return mb.tmpID
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

	convID := mb.GetConversationID()
	content := mb.GetContent()
	selfParticipantID := mb.GetSelfParticipantID()
	tmpID := mb.GetTmpID()

	messageInfo := make([]*binary.MessageInfo, 0)
	messageInfo = append(messageInfo, &binary.MessageInfo{Data: &binary.MessageInfo_MessageContent{
		MessageContent: &binary.MessageContent{
			Content: content,
		},
	}})

	mb.appendImagesPayload(&messageInfo)

	sendMsgPayload := &binary.SendMessagePayload{
		ConversationID: convID,
		MessagePayload: &binary.MessagePayload{
			TmpID:             tmpID,
			ConversationID:    convID,
			SelfParticipantID: selfParticipantID,
			MessageInfo:       messageInfo,
			TmpID2:            tmpID,
		},
		TmpID: tmpID,
	}
	if len(content) > 0 {
		sendMsgPayload.MessagePayload.MessagePayloadContent = &binary.MessagePayloadContent{
			MessageContent: &binary.MessageContent{
				Content: content,
			},
		}
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
		Data: &binary.MessageInfo_ImageContent{
			ImageContent: &binary.ImageContent{
				SomeNumber:    media.Image.GetImageType().Type,
				ImageID:       media.MediaID,
				ImageName:     media.Image.GetImageName(),
				Size:          media.Image.GetImageSize(),
				DecryptionKey: media.Image.GetImageCryptor().GetKey(),
			},
		},
	}
	mb.client.Logger.Debug().Any("imageMessage", imageMessage).Msg("New Image Content")
	return imageMessage
}
