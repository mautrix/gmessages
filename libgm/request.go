package textgapi

import (
	"bytes"
	"net/http"
	"reflect"

	"go.mau.fi/mautrix-gmessages/libgm/util"
)

func (c *Client) PostRequest(url string, payload []byte, headers interface{}) (*http.Response, error) {
	req, err := http.NewRequest("POST", url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	reqHeaders := &http.Header{}
	SetHeaders(reqHeaders, headers)
	req.Header = *reqHeaders
	//c.Logger.Info().Any("headers", req.Header).Msg("POST Request Headers")
	res, reqErr := c.http.Do(req)
	if reqErr != nil {
		return res, reqErr
	}
	return res, nil
}

func (c *Client) GetRequest(url string, headers interface{}) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)

	if err != nil {
		return nil, err
	}
	reqHeaders := &http.Header{}
	SetHeaders(reqHeaders, headers)
	req.Header = *reqHeaders
	//c.Logger.Info().Any("headers", req.Header).Msg("GET Request Headers")
	res, reqErr := c.http.Do(req)
	if reqErr != nil {
		return res, reqErr
	}
	return res, nil
}

func (c *Client) MakeRelayRequest(url string, body []byte) (*http.Response, error) {
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	util.BuildRelayHeaders(req, "application/x-protobuf", "*/*")
	res, reqErr := c.http.Do(req)
	//c.Logger.Info().Any("bodyLength", len(body)).Any("url", url).Any("headers", res.Request.Header).Msg("Relay Request Headers")
	if reqErr != nil {
		return res, reqErr
	}
	return res, nil
}

func SetHeaders(h *http.Header, headers interface{}) {
	if headers == nil {
		return
	}
	v := reflect.ValueOf(headers)
	for i := 0; i < v.NumField(); i++ {
		field := v.Type().Field(i)
		value := v.Field(i).String()
		if !v.Field(i).IsZero() {
			h.Set(field.Tag.Get("header"), value)
		}
	}
}
