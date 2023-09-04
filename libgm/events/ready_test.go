package events_test

import (
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.mau.fi/mautrix-gmessages/libgm/events"
	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
	"go.mau.fi/mautrix-gmessages/libgm/pblite"
)

func TestRequestError_Is(t *testing.T) {
	dat, _ := base64.StdEncoding.DecodeString("WzUsIlJlcXVlc3RlZCBlbnRpdHkgd2FzIG5vdCBmb3VuZC4iLFtbInR5cGUuZ29vZ2xlYXBpcy5jb20vZ29vZ2xlLmludGVybmFsLmNvbW11bmljYXRpb25zLmluc3RhbnRtZXNzYWdpbmcudjEuVGFjaHlvbkVycm9yIixbMV1dXV0=")
	var errResp gmproto.ErrorResponse
	err := pblite.Unmarshal(dat, &errResp)
	require.NoError(t, err)
	assert.ErrorIs(t, events.RequestError{Data: &errResp}, events.ErrRequestedEntityNotFound)
	assert.ErrorIs(t, events.RequestError{Data: &errResp}, fmt.Errorf("meow: %w", events.ErrRequestedEntityNotFound))
	assert.NotErrorIs(t, events.RequestError{Data: &errResp}, events.RequestError{
		Data: &gmproto.ErrorResponse{
			Type:    5,
			Message: "meow.",
			Class: []*gmproto.ErrorResponse_ErrorClass{{
				Class: "type.googleapis.com/google.internal.communications.instantmessaging.v1.TachyonError",
			}},
		},
	})
}
