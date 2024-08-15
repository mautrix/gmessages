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
	"context"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"

	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

var generalCaps = &bridgev2.NetworkGeneralCapabilities{
	DisappearingMessages: false,
	AggressiveUpdateInfo: false,
}

func (gc *GMConnector) GetCapabilities() *bridgev2.NetworkGeneralCapabilities {
	return generalCaps
}

var rcsDMCaps = &bridgev2.NetworkRoomCapabilities{
	Replies:      true,
	Deletes:      true,
	ReadReceipts: true,
	Reactions:    true,
}

var rcsGroupCaps = &bridgev2.NetworkRoomCapabilities{
	Replies:   true,
	Deletes:   true,
	Reactions: true,
}

var smsRoomCaps = &bridgev2.NetworkRoomCapabilities{
	Captions:  true,
	Deletes:   true,
	Reactions: true,
}

func (gc *GMClient) GetCapabilities(ctx context.Context, portal *bridgev2.Portal) *bridgev2.NetworkRoomCapabilities {
	if portal.Metadata.(*PortalMetadata).Type == gmproto.ConversationType_RCS {
		if portal.RoomType == database.RoomTypeDM {
			return rcsDMCaps
		} else {
			return rcsGroupCaps
		}
	} else {
		return smsRoomCaps
	}
}
