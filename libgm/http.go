package libgm

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"net/http"

	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-gmessages/libgm/events"
	"go.mau.fi/mautrix-gmessages/libgm/pblite"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

const ContentTypeProtobuf = "application/x-protobuf"
const ContentTypePBLite = "application/json+protobuf"

func (c *Client) makeProtobufHTTPRequest(url string, data proto.Message, contentType string) (*http.Response, error) {
	var body []byte
	var err error
	switch contentType {
	case ContentTypeProtobuf:
		body, err = proto.Marshal(data)
	case ContentTypePBLite:
		body, err = pblite.Marshal(data)
	default:
		return nil, fmt.Errorf("unknown request content type %s", contentType)
	}
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	util.BuildRelayHeaders(req, contentType, "*/*")
	res, reqErr := c.http.Do(req)
	if reqErr != nil {
		return res, reqErr
	}
	return res, nil
}

func typedHTTPResponse[T proto.Message](resp *http.Response, err error) (parsed T, retErr error) {
	if err != nil {
		retErr = err
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		retErr = events.HTTPError{Resp: resp}
		return
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		retErr = fmt.Errorf("failed to read response body: %w", err)
		return
	}
	contentType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil {
		retErr = fmt.Errorf("failed to parse content-type: %w", err)
		return
	}
	parsed = parsed.ProtoReflect().New().Interface().(T)
	switch contentType {
	case ContentTypeProtobuf:
		retErr = proto.Unmarshal(body, parsed)
	case ContentTypePBLite:
		retErr = pblite.Unmarshal(body, parsed)
	default:
		retErr = fmt.Errorf("unknown content type %s in response", contentType)
	}
	return
}
