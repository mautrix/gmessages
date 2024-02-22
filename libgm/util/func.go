package util

import (
	"fmt"
	"math/rand"
	"net/http"
	"time"
)

func GenerateTmpID() string {
	src := rand.NewSource(time.Now().UnixNano())
	r := rand.New(src)
	randNum := r.Int63n(1e12)
	return fmt.Sprintf("tmp_%012d", randNum)
}

func BuildRelayHeaders(req *http.Request, contentType string, accept string) {
	//req.Header.Set("host", "instantmessaging-pa.googleapis.com")
	req.Header.Set("sec-ch-ua", SecUA)
	req.Header.Set("x-user-agent", XUserAgent)
	req.Header.Set("x-goog-api-key", GoogleAPIKey)
	if len(contentType) > 0 {
		req.Header.Set("content-type", contentType)
	}
	req.Header.Set("sec-ch-ua-mobile", SecUAMobile)
	req.Header.Set("user-agent", UserAgent)
	req.Header.Set("sec-ch-ua-platform", "\""+UAPlatform+"\"")
	req.Header.Set("accept", accept)
	req.Header.Set("origin", "https://messages.google.com")
	req.Header.Set("sec-fetch-site", "cross-site")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("referer", "https://messages.google.com/")
	req.Header.Set("accept-language", "en-US,en;q=0.9")
}

func BuildUploadHeaders(req *http.Request, metadata string) {
	//req.Header.Set("host", "instantmessaging-pa.googleapis.com")
	req.Header.Set("x-goog-download-metadata", metadata)
	req.Header.Set("sec-ch-ua", SecUA)
	req.Header.Set("sec-ch-ua-mobile", SecUAMobile)
	req.Header.Set("user-agent", UserAgent)
	req.Header.Set("sec-ch-ua-platform", "\""+UAPlatform+"\"")
	req.Header.Set("accept", "*/*")
	req.Header.Set("origin", "https://messages.google.com")
	req.Header.Set("sec-fetch-site", "cross-site")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("referer", "https://messages.google.com/")
	req.Header.Set("accept-encoding", "gzip, deflate, br")
	req.Header.Set("accept-language", "en-US,en;q=0.9")
}

func NewMediaUploadHeaders(imageSize string, command string, uploadOffset string, imageContentType string, protocol string) *http.Header {
	headers := &http.Header{}

	headers.Set("host", "instantmessaging-pa.googleapis.com")
	headers.Set("sec-ch-ua", SecUA)
	if protocol != "" {
		headers.Set("x-goog-upload-protocol", protocol)
	}
	headers.Set("x-goog-upload-header-content-length", imageSize)
	headers.Set("sec-ch-ua-mobile", SecUAMobile)
	headers.Set("user-agent", UserAgent)
	if imageContentType != "" {
		headers.Set("x-goog-upload-header-content-type", imageContentType)
	}
	headers.Set("content-type", "application/x-www-form-urlencoded;charset=UTF-8")
	if command != "" {
		headers.Set("x-goog-upload-command", command)
	}
	if uploadOffset != "" {
		headers.Set("x-goog-upload-offset", uploadOffset)
	}
	headers.Set("sec-ch-ua-platform", "\""+UAPlatform+"\"")
	headers.Set("accept", "*/*")
	headers.Set("origin", "https://messages.google.com")
	headers.Set("sec-fetch-site", "cross-site")
	headers.Set("sec-fetch-mode", "cors")
	headers.Set("sec-fetch-dest", "empty")
	headers.Set("referer", "https://messages.google.com/")
	headers.Set("accept-encoding", "gzip, deflate, br")
	headers.Set("accept-language", "en-US,en;q=0.9")
	return headers
}
