package libgm

import (
	"bytes"
	"net/http"

	"go.mau.fi/mautrix-gmessages/libgm/util"
)

func (c *Client) MakeRelayRequest(url string, body []byte) (*http.Response, error) {
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	util.BuildRelayHeaders(req, "application/x-protobuf", "*/*")
	res, reqErr := c.http.Do(req)
	if reqErr != nil {
		return res, reqErr
	}
	return res, nil
}
