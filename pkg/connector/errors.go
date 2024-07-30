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
	"fmt"
	"strings"

	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
)

var ()

type responseStatusError gmproto.SendMessageResponse

func (rse *responseStatusError) Error() string {
	switch rse.Status {
	case 0:
		if rse.GoogleAccountSwitch != nil && strings.ContainsRune(rse.GoogleAccountSwitch.GetAccount(), '@') {
			return "Switch back to QR pairing or log in with Google account to send messages"
		}
	case gmproto.SendMessageResponse_FAILURE_2:
		return "Unknown permanent error"
	case gmproto.SendMessageResponse_FAILURE_3:
		return "Unknown temporary error"
	case gmproto.SendMessageResponse_FAILURE_4:
		return "Google Messages is not your default SMS app"
	}
	return fmt.Sprintf("Unrecognized response status %d", rse.Status)
}
