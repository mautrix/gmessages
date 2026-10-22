// mautrix-gmessages - A Matrix-Google Messages puppeting bridge.
// Copyright (C) 2024 Tulir Asokan
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package connector

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/event"

	"go.mau.fi/mautrix-gmessages/pkg/libgm"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/events"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

type responseStatusError gmproto.SendMessageResponse

func (rse *responseStatusError) isAccountSwitch() bool {
	return rse.Status == 0 && rse.GoogleAccountSwitch != nil && strings.ContainsRune(rse.GoogleAccountSwitch.GetAccount(), '@')
}

func (rse *responseStatusError) Error() string {
	switch rse.Status {
	case 0:
		if rse.isAccountSwitch() {
			return "Switch back to QR pairing or log in with Google account to send messages"
		}
		return "Google Messages on your phone rejected the message. Check that you can send from the Messages app, or try re-pairing"
	case gmproto.SendMessageResponse_FAILURE_2:
		return "Unknown permanent error"
	case gmproto.SendMessageResponse_FAILURE_3:
		return "Unknown temporary error"
	case gmproto.SendMessageResponse_FAILURE_4:
		return "Google Messages is not your default SMS app"
	}
	return fmt.Sprintf("Unrecognized response status %d", rse.Status)
}

// needsUserAction returns true for rejections that won't go away by retrying until the user changes something on their phone.
func (rse *responseStatusError) needsUserAction() bool {
	return rse.Status == gmproto.SendMessageResponse_FAILURE_4 || rse.isAccountSwitch()
}

func isCredentialError(err error) bool {
	var httpErr events.HTTPError
	return errors.Is(err, events.ErrInvalidCredentials) ||
		errors.Is(err, events.ErrRequestedEntityNotFound) ||
		(errors.As(err, &httpErr) && httpErr.Resp.StatusCode == http.StatusUnauthorized)
}

// wrapSendError turns an error from a Google Messages request made while handling a Matrix event into a message status.
func wrapSendError(err error) error {
	if err == nil || errors.As(err, &bridgev2.MessageStatus{}) {
		return err
	}
	var httpErr events.HTTPError
	switch {
	case errors.Is(err, libgm.ErrPhoneNotResponding):
		// The server accepted the request, so the phone may still process it if it comes back online.
		return bridgev2.WrapErrorInStatus(err).
			WithMessage(PhoneNotRespondingMessage).
			WithErrorReason(event.MessageStatusTooOld).
			WithSendNotice(true)
	case errors.Is(err, libgm.ErrConnectionClosed):
		return bridgev2.WrapErrorInStatus(err).
			WithMessage("disconnected from Google Messages before your phone responded").
			WithIsCertain(false).
			WithSendNotice(true)
	case errors.Is(err, libgm.ErrClientIsNil):
		return bridgev2.ErrNotLoggedIn
	case isCredentialError(err):
		return bridgev2.WrapErrorInStatus(err).
			WithMessage("Unpaired from Google Messages, please re-link the connection").
			WithIsCertain(true).
			WithStatus(event.MessageStatusFail).
			WithSendNotice(true)
	case errors.As(err, &httpErr):
		return bridgev2.WrapErrorInStatus(err).
			WithMessage("Google Messages server returned an error").
			WithErrorReason(event.MessageStatusNetworkError).
			WithIsCertain(httpErr.Resp.StatusCode < 500).
			WithSendNotice(true)
	default:
		return bridgev2.WrapErrorInStatus(err).
			WithMessage("failed to send request to Google Messages").
			WithSendNotice(true)
	}
}

// wrapRespError turns an error from a Google Messages request made during provisioning into an HTTP error,
// keeping the original error in the chain.
func wrapRespError(err error) error {
	var mapped bridgev2.RespError
	switch {
	case errors.Is(err, libgm.ErrPhoneNotResponding):
		mapped = bridgev2.RespError(mautrix.MUnknown.WithMessage(PhoneNotRespondingMessage))
	case errors.Is(err, libgm.ErrClientIsNil), isCredentialError(err):
		mapped = bridgev2.RespError(mautrix.MForbidden.WithMessage("Unpaired from Google Messages, please re-link the connection"))
	default:
		mapped = bridgev2.RespError(mautrix.MUnknown.WithMessage("Request to Google Messages failed"))
	}
	return fmt.Errorf("%w: %w", mapped, err)
}
