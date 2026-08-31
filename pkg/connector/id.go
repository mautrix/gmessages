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
	"strings"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
)

func parseAnyID(id string) (prefix, realID string) {
	parts := strings.SplitN(id, ".", 2)
	if len(parts) == 2 {
		prefix = parts[0]
		realID = parts[1]
	}
	return
}

var ErrPrefixMismatch = bridgev2.WrapErrorInStatus(errors.New("internal error: account mismatch")).
	WithErrorAsMessage().WithIsCertain(true).WithSendNotice(true)

func (gc *GMClient) parseAnyID(id string) (string, error) {
	prefix, realID := parseAnyID(id)
	if prefix != gc.Meta.IDPrefix {
		return "", ErrPrefixMismatch
	}
	return realID, nil
}

func (gc *GMClient) makeAnyID(realID string) string {
	return fmt.Sprintf("%s.%s", gc.Meta.IDPrefix, realID)
}

func (gc *GMClient) ParseUserID(id networkid.UserID) (participantID string, err error) {
	return gc.parseAnyID(string(id))
}

func (gc *GMClient) ParseMessageID(id networkid.MessageID) (messageID string, err error) {
	return gc.parseAnyID(string(id))
}

func (gc *GMClient) ParsePortalID(id networkid.PortalID) (participantID string, err error) {
	return gc.parseAnyID(string(id))
}

func (gc *GMClient) MakeUserID(participantID string) networkid.UserID {
	return networkid.UserID(gc.makeAnyID(participantID))
}

func (gc *GMClient) MakeMessageID(messageID string) networkid.MessageID {
	return networkid.MessageID(gc.makeAnyID(messageID))
}

func (gc *GMClient) MakePortalID(conversationID string) networkid.PortalID {
	return networkid.PortalID(gc.makeAnyID(conversationID))
}

func (gc *GMClient) MakePortalKey(conversationID string) networkid.PortalKey {
	return networkid.PortalKey{
		ID:       gc.MakePortalID(conversationID),
		Receiver: gc.UserLogin.ID,
	}
}
