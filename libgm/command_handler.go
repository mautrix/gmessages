package textgapi

import (
	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

func (c *Client) processSessionResponse(prepareSession []*Response, newSession []*Response) (*util.SessionResponse, error) {
	prepDecoded, prepDecodeErr := prepareSession[0].decryptData()
	if prepDecodeErr != nil {
		return nil, prepDecodeErr
	}

	sessDecoded, sessDecodeErr := newSession[0].decryptData()
	if sessDecodeErr != nil {
		return nil, sessDecodeErr
	}

	sess := sessDecoded.(*binary.NewSession)
	prep := prepDecoded.(*binary.PrepareNewSession)
	return &util.SessionResponse{
		Success:  prep.Success,
		Settings: sess.Settings,
	}, nil
}

func (c *Client) processFetchMessagesResponse(fetchMessagesRes []*Response, openConversationRes []*Response, setActiveConversationRes []*Response) (*binary.FetchMessagesResponse, error) {
	messagesDecoded, messagesDecodeErr := fetchMessagesRes[0].decryptData()
	if messagesDecodeErr != nil {
		return nil, messagesDecodeErr
	}
	return messagesDecoded.(*binary.FetchMessagesResponse), nil
}
