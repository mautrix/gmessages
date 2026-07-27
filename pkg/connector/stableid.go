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
	"time"

	"go.mau.fi/util/exmaps"
	"maunium.net/go/mautrix/bridgev2"

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

const (
	stableIDSweepInitialDelay  = 5 * time.Minute
	stableIDSweepFetchInterval = 3 * time.Second
)

// One time sweep of all portals to backfill the stable identifiers, this can be removed once
// we switch over.
func (gc *GMClient) runStableIDSweep(ctx context.Context) {
	if !gc.stableIDSweepStarted.CompareAndSwap(false, true) {
		return
	}
	log := gc.UserLogin.Log.With().Str("action", "stable id sweep").Logger()
	ctx = log.WithContext(context.WithoutCancel(ctx))
	select {
	case <-time.After(stableIDSweepInitialDelay):
	case <-ctx.Done():
		gc.stableIDSweepStarted.Store(false)
		return
	}
	portals, err := gc.Main.br.DB.Portal.GetAll(ctx)
	if err != nil {
		log.Err(err).Msg("Failed to get portals for stable ID sweep")
		gc.stableIDSweepStarted.Store(false)
		return
	}
	var candidates []string
	for _, portal := range portals {
		if portal.Receiver != gc.UserLogin.ID || portal.MXID == "" {
			continue
		}
		meta, ok := portal.Metadata.(*PortalMetadata)
		if !ok || meta.StableID != "" {
			continue
		}
		convID, err := gc.ParsePortalID(portal.ID)
		if err != nil {
			continue
		}
		candidates = append(candidates, convID)
	}
	if len(candidates) == 0 {
		log.Debug().Msg("No portals missing stable ID metadata")
		return
	}
	log.Info().Int("portal_count", len(candidates)).Msg("Sweeping portals missing stable ID metadata")
	var updated, failed int
	for _, convID := range candidates {
		select {
		case <-time.After(stableIDSweepFetchInterval):
		case <-ctx.Done():
			return
		}
		if gc.Client == nil || !gc.ready || !gc.PhoneResponding {
			// Allow a later browser active event to restart the sweep.
			log.Debug().Msg("Client not ready, aborting stable ID sweep")
			gc.stableIDSweepStarted.Store(false)
			return
		}
		conv, ok := gc.chatInfoCache.Get(convID)
		if !ok {
			var err error
			conv, err = gc.Client.GetConversation(ctx, convID)
			if err != nil || conv.GetConversationID() == "" {
				log.Debug().Err(err).Str("conversation_id", convID).
					Msg("Failed to fetch conversation for stable ID sweep")
				failed++
				continue
			}
			gc.chatInfoCache.Set(convID, conv)
		}
		// Update portal directly in the database rather than round tripping through Matrix layers
		portal, err := gc.Main.br.GetExistingPortalByKey(ctx, gc.MakePortalKey(conv.GetConversationID()))
		if err != nil {
			log.Err(err).Str("conversation_id", convID).Msg("Failed to get portal for stable ID sweep")
			failed++
			continue
		} else if portal == nil {
			continue
		}
		if !portal.Metadata.(*PortalMetadata).updateFromStableIdentity(gc.computeStableIdentity(conv)) {
			continue
		}
		if err = portal.Save(ctx); err != nil {
			log.Err(err).Str("conversation_id", convID).Msg("Failed to save portal in stable ID sweep")
			failed++
			continue
		}
		updated++
	}
	log.Info().Int("updated", updated).Int("failed", failed).Msg("Stable ID sweep finished")
}
