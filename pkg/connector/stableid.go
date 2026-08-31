// mautrix-gmessages - A Matrix-Google Messages puppeting bridge.
// Copyright (C) 2026 Tulir Asokan
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
	"strings"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"

	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

// Sentinel value found in the Google Messages APK, seemingly used for numbers which are no
// longer reachable (e.g. previously RCS-enabled numbers that no longer are). Educated guess.
const unknownSenderNumber = "UNKNOWN_SENDER!"

func normalizeNumber(number string) string {
	// SMS senders can also be alphanumeric shortcodes (e.g. "GOOGLE") rather than phone numbers,
	// which the cleaner rejects - keep those as-is apart from casing.
	cleaned, err := bridgev2.CleanNonInternationalPhoneNumber(number)
	if err != nil {
		return strings.ToLower(number)
	}
	return cleaned
}

func normalizeIdentifier(id *gmproto.SmallInfo) string {
	number := id.GetNumber()
	if number == "" || number == unknownSenderNumber {
		return ""
	}
	// Identities can be email addresses, don't mangle those.
	if id.GetType() == gmproto.IdentifierType_EMAIL || strings.ContainsRune(number, '@') {
		return strings.ToLower(number)
	}
	return normalizeNumber(number)
}

func (gc *GMClient) conversationIDForPortal(ctx context.Context, portal *bridgev2.Portal) (string, error) {
	return gc.ParsePortalID(portal.ID)
}

func (gc *GMClient) portalKeyForConv(conv *gmproto.Conversation) networkid.PortalKey {
	return gc.MakePortalKey(conv.GetConversationID())
}

func (gc *GMClient) PortalKeyForConversation(ctx context.Context, conversationID string) networkid.PortalKey {
	return gc.MakePortalKey(conversationID)
}
