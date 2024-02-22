package libgm

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-gmessages/libgm/events"
	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
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
	ctx := c.Logger.WithContext(context.TODO())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	util.BuildRelayHeaders(req, contentType, "*/*")
	c.AddCookieHeaders(req)
	res, reqErr := c.http.Do(req)
	if reqErr != nil {
		return res, reqErr
	}
	c.HandleCookieUpdates(res)
	return res, nil
}

func (c *Client) AddCookieHeaders(req *http.Request) {
	if c.AuthData == nil || c.AuthData.Cookies == nil {
		return
	}
	for k, v := range c.AuthData.Cookies {
		req.AddCookie(&http.Cookie{Name: k, Value: v})
	}
	sapisid, ok := c.AuthData.Cookies["SAPISID"]
	if ok {
		req.Header.Set("Authorization", sapisidHash(util.MessagesBaseURL, sapisid))
	}
}

func (c *Client) HandleCookieUpdates(resp *http.Response) {
	if c.AuthData.Cookies == nil {
		return
	}
	for _, cookie := range resp.Cookies() {
		c.AuthData.Cookies[cookie.Name] = cookie.Value
	}
}

func sapisidHash(origin, sapisid string) string {
	ts := time.Now().Unix()
	hash := sha1.Sum([]byte(fmt.Sprintf("%d %s %s", ts, sapisid, origin)))
	return fmt.Sprintf("SAPISIDHASH %d_%x", ts, hash[:])
}

func decodeProtoResp(body []byte, contentType string, into proto.Message) error {
	contentType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return fmt.Errorf("failed to parse content-type: %w", err)
	}
	switch contentType {
	case ContentTypeProtobuf:
		return proto.Unmarshal(body, into)
	case ContentTypePBLite, "text/plain":
		return pblite.Unmarshal(body, into)
	default:
		return fmt.Errorf("unknown content type %s in response", contentType)
	}
}

func typedHTTPResponse[T proto.Message](resp *http.Response, err error) (parsed T, retErr error) {
	if err != nil {
		retErr = err
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		retErr = fmt.Errorf("failed to read response body: %w", err)
		return
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logEvt := zerolog.Ctx(resp.Request.Context()).Debug().
			Int("status_code", resp.StatusCode).
			Str("url", resp.Request.URL.String()).
			Str("response_body", base64.StdEncoding.EncodeToString(body))
		httpErr := events.HTTPError{Resp: resp, Body: body}
		retErr = httpErr
		var errorResp gmproto.ErrorResponse
		errErr := decodeProtoResp(body, resp.Header.Get("Content-Type"), &errorResp)
		if errErr == nil && errorResp.Message != "" {
			logEvt = logEvt.Any("response_proto_err", &errorResp)
			retErr = events.RequestError{
				HTTP: &httpErr,
				Data: &errorResp,
			}
		} else {
			logEvt = logEvt.AnErr("proto_parse_err", errErr)
		}
		logEvt.Msg("HTTP request to Google Messages failed")
		return
	}
	parsed = parsed.ProtoReflect().New().Interface().(T)
	retErr = decodeProtoResp(body, resp.Header.Get("Content-Type"), parsed)
	successEvt := zerolog.Ctx(resp.Request.Context()).Trace()
	if successEvt.Enabled() {
		successEvt.
			Int("status_code", resp.StatusCode).
			Str("url", resp.Request.URL.String()).
			Str("response_body", base64.StdEncoding.EncodeToString(body)).
			Bool("parsed_has_unknown_fields", len(parsed.ProtoReflect().GetUnknown()) > 0).
			Type("parsed_data_type", parsed).
			Any("parsed_data", parsed).
			Msg("HTTP request to Google Messages succeeded")
	}
	return
}
