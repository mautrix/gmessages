package events

import (
	"errors"
	"fmt"
	"net/http"

	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
)

type ClientReady struct {
	SessionID     string
	Conversations []*gmproto.Conversation
}

type AuthTokenRefreshed struct{}

var ErrRequestedEntityNotFound = RequestError{
	Data: &gmproto.ErrorResponse{
		Type:    5,
		Message: "Requested entity was not found.",
		Class: []*gmproto.ErrorResponse_ErrorClass{{
			Class: "type.googleapis.com/google.internal.communications.instantmessaging.v1.TachyonError",
		}},
	},
}

type RequestError struct {
	Data *gmproto.ErrorResponse
	HTTP *HTTPError
}

func (re RequestError) Unwrap() error {
	if re.HTTP == nil {
		return nil
	}
	return *re.HTTP
}

func (re RequestError) Error() string {
	if re.HTTP == nil {
		return fmt.Sprintf("%d: %s", re.Data.Type, re.Data.Message)
	}
	return fmt.Sprintf("HTTP %d: %d: %s", re.HTTP.Resp.StatusCode, re.Data.Type, re.Data.Message)
}

func (re RequestError) Is(other error) bool {
	var otherRe RequestError
	if !errors.As(other, &otherRe) {
		return re.HTTP != nil && errors.Is(*re.HTTP, other)
	}
	return otherRe.Data.GetType() == re.Data.GetType() &&
		otherRe.Data.GetMessage() == re.Data.GetMessage()
	// TODO check class?
}

type HTTPError struct {
	Action string
	Resp   *http.Response
	Body   []byte
}

func (he HTTPError) Error() string {
	if he.Action == "" {
		return fmt.Sprintf("unexpected http %d", he.Resp.StatusCode)
	}
	return fmt.Sprintf("http %d while %s", he.Resp.StatusCode, he.Action)
}

type ListenFatalError struct {
	Error error
}

type ListenTemporaryError struct {
	Error error
}

type ListenRecovered struct{}

type PhoneNotResponding struct{}

type PhoneRespondingAgain struct{}

type PingFailed struct {
	Error      error
	ErrorCount int
}
