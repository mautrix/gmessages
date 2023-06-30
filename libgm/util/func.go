package util

import (
	crand "crypto/rand"
	"encoding/hex"
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"github.com/google/uuid"
)

var Charset = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890")

func RandStr(length int) string {
	b := make([]rune, length)
	for i := range b {
		b[i] = Charset[rand.Intn(len(Charset))]
	}
	return string(b)
}

func GenerateImageId() string {
	part1 := RandomUUIDv4()
	part2 := RandStr(25)
	return part1 + "/" + part2
}

func GenerateTmpId() string {
	src := rand.NewSource(time.Now().UnixNano())
	r := rand.New(src)
	randNum := r.Int63n(1e12)
	return fmt.Sprintf("tmp_%012d", randNum)
}

func ParseTimestamp(unixTs int64) time.Time {
	seconds := unixTs / int64(time.Second/time.Microsecond)
	nanoseconds := (unixTs % int64(time.Second/time.Microsecond)) * int64(time.Microsecond/time.Nanosecond)
	return time.Unix(seconds, nanoseconds).UTC()
}

func RandomHex(n int) string {
	bytes := make([]byte, n)
	crand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func RandomUUIDv4() string {
	return uuid.New().String()
}

func RemoveFromSlice(s []string, v string) []string {
	newS := []string{}
	for _, i := range s {
		if i != v {
			newS = append(newS, i)
		}
	}
	return newS
}

func BuildRelayHeaders(req *http.Request, contentType string, accept string) {
	req.Header.Add("host", "instantmessaging-pa.googleapis.com")
	req.Header.Add("connection", "keep-alive")
	req.Header.Add("sec-ch-ua", "\"Google Chrome\";v=\"113\", \"Chromium\";v=\"113\", \"Not-A.Brand\";v=\"24\"")
	req.Header.Add("x-user-agent", X_USER_AGENT)
	req.Header.Add("x-goog-api-key", GOOG_API_KEY)
	if len(contentType) > 0 {
		req.Header.Add("content-type", contentType)
	}
	req.Header.Add("sec-ch-ua-mobile", "?0")
	req.Header.Add("user-agent", USER_AGENT)
	req.Header.Add("sec-ch-ua-platform", "\""+OS+"\"")
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
	req.Header.Add("sec-ch-ua", "\"Google Chrome\";v=\"113\", \"Chromium\";v=\"113\", \"Not-A.Brand\";v=\"24\"")
	req.Header.Add("sec-ch-ua-mobile", "?0")
	req.Header.Add("user-agent", USER_AGENT)
	req.Header.Add("sec-ch-ua-platform", "\""+OS+"\"")
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
	headers.Add("sec-ch-ua", "\"Google Chrome\";v=\"113\", \"Chromium\";v=\"113\", \"Not-A.Brand\";v=\"24\"")
	if protocol != "" {
		headers.Add("x-goog-upload-protocol", protocol)
	}
	headers.Add("x-goog-upload-header-content-length", imageSize)
	headers.Add("sec-ch-ua-mobile", "?0")
	headers.Add("user-agent", USER_AGENT)
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
	headers.Add("sec-ch-ua-platform", "\""+OS+"\"")
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
