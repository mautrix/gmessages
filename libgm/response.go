package textgapi

import "go.mau.fi/mautrix-gmessages/libgm/binary"

func (c *Client) newMessagesResponse(responseData *Response) (*binary.FetchMessagesResponse, error) {
	messages := &binary.FetchMessagesResponse{}
	decryptErr := c.cryptor.DecryptAndDecodeData(responseData.Data.EncryptedData, messages)
	if decryptErr != nil {
		return nil, decryptErr
	}
	decryptErr = c.decryptImages(messages)
	if decryptErr != nil {
		return nil, decryptErr
	}
	return messages, nil
}
