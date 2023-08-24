package util

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"time"

	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
)

func GenerateTmpID() string {
	src := rand.NewSource(time.Now().UnixNano())
	r := rand.New(src)
	randNum := r.Int63n(1e12)
	return fmt.Sprintf("tmp_%012d", randNum)
}

func BuildRelayHeaders(req *http.Request, contentType string, accept string) {
	req.Header.Add("host", "instantmessaging-pa.googleapis.com")
	req.Header.Add("connection", "keep-alive")
	req.Header.Add("sec-ch-ua", SecUA)
	req.Header.Add("x-user-agent", XUserAgent)
	req.Header.Add("x-goog-api-key", GoogleAPIKey)
	if len(contentType) > 0 {
		req.Header.Add("content-type", contentType)
	}
	req.Header.Add("sec-ch-ua-mobile", SecUAMobile)
	req.Header.Add("user-agent", UserAgent)
	req.Header.Add("sec-ch-ua-platform", "\""+UAPlatform+"\"")
	req.Header.Add("accept", accept)
	req.Header.Add("origin", "https://messages.google.com")
	req.Header.Add("sec-fetch-site", "cross-site")
	req.Header.Add("sec-fetch-mode", "cors")
	req.Header.Add("sec-fetch-dest", "empty")
	req.Header.Add("referer", "https://messages.google.com/")
	req.Header.Add("accept-language", "en-US,en;q=0.9")
}

func BuildUploadHeaders(req *http.Request, metadata string) {
	req.Header.Add("host", "instantmessaging-pa.googleapis.com")
	req.Header.Add("connection", "keep-alive")
	req.Header.Add("x-goog-download-metadata", metadata)
	req.Header.Add("sec-ch-ua", SecUA)
	req.Header.Add("sec-ch-ua-mobile", SecUAMobile)
	req.Header.Add("user-agent", UserAgent)
	req.Header.Add("sec-ch-ua-platform", "\""+UAPlatform+"\"")
	req.Header.Add("accept", "*/*")
	req.Header.Add("origin", "https://messages.google.com")
	req.Header.Add("sec-fetch-site", "cross-site")
	req.Header.Add("sec-fetch-mode", "cors")
	req.Header.Add("sec-fetch-dest", "empty")
	req.Header.Add("referer", "https://messages.google.com/")
	req.Header.Add("accept-encoding", "gzip, deflate, br")
	req.Header.Add("accept-language", "en-US,en;q=0.9")
}

func NewMediaUploadHeaders(imageSize string, command string, uploadOffset string, imageContentType string, protocol string) *http.Header {
	headers := &http.Header{}

	headers.Add("host", "instantmessaging-pa.googleapis.com")
	headers.Add("connection", "keep-alive")
	headers.Add("sec-ch-ua", SecUA)
	if protocol != "" {
		headers.Add("x-goog-upload-protocol", protocol)
	}
	headers.Add("x-goog-upload-header-content-length", imageSize)
	headers.Add("sec-ch-ua-mobile", SecUAMobile)
	headers.Add("user-agent", UserAgent)
	if imageContentType != "" {
		headers.Add("x-goog-upload-header-content-type", imageContentType)
	}
	headers.Add("content-type", "application/x-www-form-urlencoded;charset=UTF-8")
	if command != "" {
		headers.Add("x-goog-upload-command", command)
	}
	if uploadOffset != "" {
		headers.Add("x-goog-upload-offset", uploadOffset)
	}
	headers.Add("sec-ch-ua-platform", "\""+UAPlatform+"\"")
	headers.Add("accept", "*/*")
	headers.Add("origin", "https://messages.google.com")
	headers.Add("sec-fetch-site", "cross-site")
	headers.Add("sec-fetch-mode", "cors")
	headers.Add("sec-fetch-dest", "empty")
	headers.Add("referer", "https://messages.google.com/")
	headers.Add("accept-encoding", "gzip, deflate, br")
	headers.Add("accept-language", "en-US,en;q=0.9")
	return headers
}

func ParseConfigVersion(res []byte) (*gmproto.ConfigVersion, error) {
	var data []interface{}

	marshalErr := json.Unmarshal(res, &data)
	if marshalErr != nil {
		return nil, marshalErr
	}

	version := data[0].(string)
	v1 := version[0:4]
	v2 := version[4:6]
	v3 := version[6:8]

	if v2[0] == 48 {
		v2 = string(v2[1])
	}
	if v3[0] == 48 {
		v3 = string(v3[1])
	}

	first, e := strconv.Atoi(v1)
	if e != nil {
		return nil, e
	}

	second, e1 := strconv.Atoi(v2)
	if e1 != nil {
		return nil, e1
	}

	third, e2 := strconv.Atoi(v3)
	if e2 != nil {
		return nil, e2
	}

	configMessage := &gmproto.ConfigVersion{
		Year:  int32(first),
		Month: int32(second),
		Day:   int32(third),
		V1:    4,
		V2:    6,
	}
	return configMessage, nil
}
