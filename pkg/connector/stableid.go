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
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"slices"
	"strings"

	"go.mau.fi/util/exmaps"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"

	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

// Sentinel value found in the Google Messages APK, seemingly used for numbers which are no
// longer reachable (e.g. previously RCS-enabled numbers that no longer are). Educated guess.
const unknownSenderNumber = "UNKNOWN_SENDER!"

const (
	stableIDPrefixRCSGroup     = "rcs:"
	stableIDPrefixDM           = "dm:"
	stableIDPrefixParticipants = "ph:"
)

type stableIdentity struct {
	StableID         string
	RCSGroupID       string
	RCSConferenceURI string
	ParticipantHash  string
	ParticipantCount int
}

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

func (gc *GMClient) selfNumbers() exmaps.Set[string] {
	selfNumbers := make(exmaps.Set[string])
	for _, sim := range gc.Meta.GetSIMs() {
		if num := normalizeNumber(sim.GetSIMData().GetFormattedPhoneNumber()); num != "" {
			selfNumbers.Add(num)
		}
		if num := normalizeNumber(sim.GetSIMData().GetInternationalPhoneNumber()); num != "" {
			selfNumbers.Add(num)
		}
	}
	return selfNumbers
}

func (gc *GMClient) computeStableIdentity(conv *gmproto.Conversation) stableIdentity {
	ident := stableIdentity{
		RCSGroupID:       conv.GetSomeKindOfGroupID().GetNestedID().GetId(),
		RCSConferenceURI: conv.GetSomeKindOfGroupID().GetNestedID().GetConferenceUri(),
	}
	selfNumbers := gc.selfNumbers()
	for _, pcp := range conv.GetParticipants() {
		if pcp.GetIsMe() {
			if num := normalizeIdentifier(pcp.GetID()); num != "" {
				selfNumbers.Add(num)
			}
		}
	}
	// Drop self entries, duplicate-self rows that aren't flagged isMe, and unknown senders
	all := make(exmaps.Set[string])
	others := make(exmaps.Set[string])
	for _, pcp := range conv.GetParticipants() {
		num := normalizeIdentifier(pcp.GetID())
		if num == "" {
			continue
		}
		all.Add(num)
		if pcp.GetIsMe() || selfNumbers.Has(num) {
			continue
		}
		others.Add(num)
	}
	// Chats with only self participants (note to self, or a chat between your own numbers on a
	// dual SIM phone) would otherwise end up with no identity at all, so fall back to hashing
	// the full participant set.
	if len(others) == 0 {
		others = all
	}
	ident.ParticipantCount = len(others)
	if len(others) > 0 {
		hash := sha256.Sum256([]byte(strings.Join(slices.Sorted(maps.Keys(others)), "\n")))
		ident.ParticipantHash = hex.EncodeToString(hash[:])
	}
	switch {
	case ident.RCSGroupID != "":
		ident.StableID = stableIDPrefixRCSGroup + ident.RCSGroupID
	case ident.ParticipantCount == 1:
		ident.StableID = stableIDPrefixDM + ident.ParticipantHash
	case ident.ParticipantCount > 1:
		ident.StableID = stableIDPrefixParticipants + ident.ParticipantHash
	}
	return ident
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
