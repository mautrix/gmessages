package libgm

import (
	"errors"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

var (
	errContentNotSet           = errors.New("failed to build MessageBuilder: content must be larger than length 0")
	errConversationIdNotSet    = errors.New("failed to build MessageBuilder: conversationId is empty")
	errSelfParticipantIdNotSet = errors.New("failed to build MessageBuilder: selfParticipantId is empty")
)

type MessageBuilder struct {
	client *Client

	content           string
	conversationId    string
	tmpId             string
	selfParticipantId string

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

func (mb *MessageBuilder) GetConversationId() string {
	return mb.conversationId
}

func (mb *MessageBuilder) SetConversationId(conversationId string) *MessageBuilder {
	mb.conversationId = conversationId
	return mb
}

func (mb *MessageBuilder) GetSelfParticipantId() string {
	return mb.selfParticipantId
}

// sendmessage function will set this automatically but if u want to set it yourself feel free
func (mb *MessageBuilder) SetSelfParticipantId(participantId string) *MessageBuilder {
	mb.selfParticipantId = participantId
	return mb
}

func (mb *MessageBuilder) GetTmpId() string {
	return mb.tmpId
}

// sendmessage function will set this automatically but if u want to set it yourself feel free
func (mb *MessageBuilder) SetTmpId(tmpId string) *MessageBuilder {
	mb.tmpId = tmpId
	return mb
}

func (mb *MessageBuilder) Build() (*binary.SendMessagePayload, error) {

	if mb.conversationId == "" {
		return nil, errConversationIdNotSet
	}

	if mb.selfParticipantId == "" {
		return nil, errSelfParticipantIdNotSet
	}

	if mb.content == "" {
		return nil, errContentNotSet
	}

	if mb.tmpId == "" {
		mb.tmpId = util.GenerateTmpId()
	}

	return mb.newSendConversationMessage(), nil
}

func (c *Client) NewMessageBuilder() *MessageBuilder {
	mb := &MessageBuilder{
		client: c,
	}

	tmpId := util.GenerateTmpId()
	mb.SetTmpId(tmpId)

	return mb
}

func (mb *MessageBuilder) newSendConversationMessage() *binary.SendMessagePayload {

	convId := mb.GetConversationId()
	content := mb.GetContent()
	selfParticipantId := mb.GetSelfParticipantId()
	tmpId := mb.GetTmpId()

	messageInfo := make([]*binary.MessageInfo, 0)
	messageInfo = append(messageInfo, &binary.MessageInfo{Data: &binary.MessageInfo_MessageContent{
		MessageContent: &binary.MessageContent{
			Content: content,
		},
	}})

	mb.appendImagesPayload(&messageInfo)

	sendMsgPayload := &binary.SendMessagePayload{
		ConversationId: convId,
		MessagePayload: &binary.MessagePayload{
			TmpId:             tmpId,
			ConversationId:    convId,
			SelfParticipantId: selfParticipantId,
			MessageInfo:       messageInfo,
			TmpId2:            tmpId,
		},
		TmpId: tmpId,
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
				ImageId:       media.MediaId,
				ImageName:     media.Image.GetImageName(),
				Size:          media.Image.GetImageSize(),
				DecryptionKey: media.Image.GetImageCryptor().GetKey(),
			},
		},
	}
	mb.client.Logger.Debug().Any("imageMessage", imageMessage).Msg("New Image Content")
	return imageMessage
}
