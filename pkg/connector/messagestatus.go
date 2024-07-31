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
	"strings"

	"maunium.net/go/mautrix/bridgev2"

	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
)

func isSuccessfullySentStatus(status gmproto.MessageStatusType) bool {
	switch status {
	case gmproto.MessageStatusType_OUTGOING_DELIVERED, gmproto.MessageStatusType_OUTGOING_COMPLETE, gmproto.MessageStatusType_OUTGOING_DISPLAYED:
		return true
	default:
		return false
	}
}

func getFailMessage(status gmproto.MessageStatusType) string {
	switch status {
	case gmproto.MessageStatusType_OUTGOING_FAILED_TOO_LARGE:
		return "too large"
	case gmproto.MessageStatusType_OUTGOING_FAILED_RECIPIENT_LOST_RCS:
		return "recipient lost RCS support"
	case gmproto.MessageStatusType_OUTGOING_FAILED_RECIPIENT_LOST_ENCRYPTION:
		return "recipient lost encryption support"
	case gmproto.MessageStatusType_OUTGOING_FAILED_RECIPIENT_DID_NOT_DECRYPT,
		gmproto.MessageStatusType_OUTGOING_FAILED_RECIPIENT_DID_NOT_DECRYPT_NO_MORE_RETRY:
		return "recipient failed to decrypt message"
	case gmproto.MessageStatusType_OUTGOING_FAILED_GENERIC:
		return "generic carrier error, check google messages and try again"
	case gmproto.MessageStatusType_OUTGOING_FAILED_NO_RETRY_NO_FALLBACK:
		return "no fallback error"
	case gmproto.MessageStatusType_OUTGOING_FAILED_EMERGENCY_NUMBER:
		return "emergency number error"
	case gmproto.MessageStatusType_OUTGOING_CANCELED:
		return "canceled"
	default:
		return ""
	}
}

func wrapStatusInError(status gmproto.MessageStatusType) error {
	errorMessage := getFailMessage(status)
	if errorMessage == "" {
		return nil
	}
	errCode := errors.New(strings.TrimPrefix(status.String(), "OUTGOING_"))
	return bridgev2.WrapErrorInStatus(errCode).
		WithMessage(errorMessage).
		WithSendNotice(true)
}
