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
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/rs/zerolog"
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

func (gc *GMClient) rememberPortalID(conversationID string, portalID networkid.PortalID) {
	if conversationID == "" || portalID == "" {
		return
	}
	gc.portalIDByConv.Set(conversationID, portalID)
}

func (gc *GMClient) loadPortalIDs(ctx context.Context) {
	if !gc.portalIDsLoaded.CompareAndSwap(false, true) {
		return
	}
	log := zerolog.Ctx(ctx)
	portals, err := gc.Main.br.DB.Portal.GetAll(ctx)
	if err != nil {
		gc.portalIDsLoaded.Store(false)
		log.Err(err).Msg("Failed to load portals for conversation ID map")
		return
	}
	var loaded, legacy int
	for _, portal := range portals {
		if portal.Receiver != gc.UserLogin.ID {
			continue
		}
		meta, ok := portal.Metadata.(*PortalMetadata)
		if !ok {
			continue
		}
		if gc.isLegacyPortalID(portal.ID, meta) {
			// Not migrated, so it's still keyed on the conversation ID and events have to keep
			// going there until repointPortal moves it.
			if convID, err := gc.ParsePortalID(portal.ID); err == nil {
				gc.rememberPortalID(convID, portal.ID)
				legacy++
			}
			if meta.ParticipantHash != "" {
				gc.legacyPortalKeyByParticipantHash.Set(meta.ParticipantHash, portal.PortalKey)
			}
			continue
		}
		gc.rememberPortalID(meta.ConversationID, portal.ID)
		loaded++
	}
	log.Debug().
		Int("stable_count", loaded).
		Int("legacy_count", legacy).
		Msg("Loaded conversation ID to portal ID mappings")
}

func (gc *GMClient) resolvePortalID(ctx context.Context, conversationID string) networkid.PortalID {
	if conversationID == "" {
		return ""
	}
	if portalID, ok := gc.portalIDByConv.Get(conversationID); ok {
		return portalID
	}
	// TODO store ids in local db and check that before querying the phone
	conv := gc.getChatInfoWithFetch(ctx, conversationID)
	if conv == nil {
		return ""
	}
	stableID := gc.computeStableIdentity(conv).StableID
	if stableID == "" {
		return ""
	}
	portalID := gc.MakePortalID(stableID)
	gc.rememberPortalID(conversationID, portalID)
	return portalID
}

var ErrNoConversationID = bridgev2.
	WrapErrorInStatus(errors.New("this chat is not ready to send yet, please try again shortly")).
	WithErrorAsMessage().
	WithIsCertain(true).
	WithSendNotice(true)

func (gc *GMClient) conversationIDForPortal(ctx context.Context, portal *bridgev2.Portal) (string, error) {
	meta, ok := portal.Metadata.(*PortalMetadata)
	if !ok {
		return "", fmt.Errorf("unexpected portal metadata type %T", portal.Metadata)
	}
	if meta.ConversationID != "" {
		return meta.ConversationID, nil
	}
	// Portals that haven't been migrated yet are still keyed on the conversation ID.
	if gc.isLegacyPortalID(portal.ID, meta) {
		return gc.ParsePortalID(portal.ID)
	}
	zerolog.Ctx(ctx).Warn().
		Stringer("portal_key", portal.PortalKey).
		Str("stable_id", meta.StableID).
		Msg("Portal has a stable ID but no conversation ID")
	return "", ErrNoConversationID
}

func (gc *GMClient) portalKeyForConv(conv *gmproto.Conversation) networkid.PortalKey {
	convID := conv.GetConversationID()
	if portalID, ok := gc.portalIDByConv.Get(convID); ok {
		return networkid.PortalKey{ID: portalID, Receiver: gc.UserLogin.ID}
	}
	stableID := gc.computeStableIdentity(conv).StableID
	if stableID == "" {
		gc.UserLogin.Log.Warn().
			Str("conversation_id", convID).
			Msg("Conversation has no stable identity, falling back to legacy portal key")
		return gc.makeLegacyPortalKey(convID)
	}
	portalID := gc.MakePortalID(stableID)
	gc.rememberPortalID(convID, portalID)
	return networkid.PortalKey{ID: portalID, Receiver: gc.UserLogin.ID}
}

func (gc *GMClient) PortalKeyForConversation(ctx context.Context, conversationID string) networkid.PortalKey {
	if portalID := gc.resolvePortalID(ctx, conversationID); portalID != "" {
		return networkid.PortalKey{ID: portalID, Receiver: gc.UserLogin.ID}
	}
	gc.UserLogin.Log.Warn().
		Str("conversation_id", conversationID).
		Msg("Failed to resolve portal for conversation, falling back to legacy portal key")
	return gc.makeLegacyPortalKey(conversationID)
}

// repointPortalIfNeeded reconciles a conversation against the stable-ID portal keyspace. It re-points the
// send-routing handle when Google re-keys a chat, and migrates (merging if necessary) any portal
// still living on the legacy conversation-ID key.
//
// Must be called before queueing a resync for the conversation, otherwise the resync creates a
// fresh portal at the stable key while the legacy one is still around.
func (gc *GMClient) repointPortalIfNeeded(ctx context.Context, conv *gmproto.Conversation) {
	convID := conv.GetConversationID()
	ident := gc.computeStableIdentity(conv)
	stableID := ident.StableID
	if convID == "" || stableID == "" {
		return
	}

	// Serialise per stable ID, otherwise two updates for the same chat can both decide to migrate
	// the legacy portal and race inside ReIDPortal.
	lock, _ := gc.stableIDRepointLocks.GetOrSet(stableID, &sync.Mutex{})
	lock.Lock()
	defer lock.Unlock()

	log := zerolog.Ctx(ctx).With().
		Str("action", "repoint portal").
		Str("conversation_id", convID).
		Str("stable_id", stableID).
		Logger()
	ctx = log.WithContext(ctx)

	stableKey := gc.MakePortalKey(stableID)
	legacyKey := gc.makeLegacyPortalKey(convID)
	if legacyKey != stableKey {
		legacyPortal, err := gc.Main.br.GetExistingPortalByKey(ctx, legacyKey)
		if err != nil {
			log.Err(err).Msg("Failed to look up legacy portal")
			// Leave the routing map alone: we don't know whether the legacy portal is still there,
			// and pointing at the stable key while it is would split the chat.
			return
		} else if legacyPortal != nil {
			log.Info().
				Stringer("legacy_portal_key", legacyKey).
				Stringer("stable_portal_key", stableKey).
				Msg("Migrating portal from legacy conversation ID key to stable ID key")
			result, _, err := gc.Main.br.ReIDPortal(ctx, legacyKey, stableKey)
			if err != nil {
				log.Err(err).Msg("Failed to re-ID legacy portal onto stable key")
				// Keep routing to the legacy portal until the move actually succeeds.
				gc.rememberPortalID(convID, legacyKey.ID)
				return
			}
			log.Info().Stringer("reid_result", result).Msg("Re-ID'd legacy portal onto stable key")
		}
	}
	if strings.HasPrefix(stableID, stableIDPrefixRCSGroup) {
		gc.reIDRCSGroupPredecessor(ctx, ident, stableKey)
	}
	gc.rememberPortalID(convID, stableKey.ID)

	// Update the send-routing handle if Google re-keyed the conversation.
	portal, err := gc.Main.br.GetExistingPortalByKey(ctx, stableKey)
	if err != nil {
		log.Err(err).Msg("Failed to look up stable portal to update conversation ID")
		return
	} else if portal == nil {
		return
	}
	meta, ok := portal.Metadata.(*PortalMetadata)
	if !ok || !meta.updateConversationID(convID) {
		return
	}
	log.Info().Msg("Re-pointed portal at new conversation ID")
	if err = portal.Save(ctx); err != nil {
		log.Err(err).Msg("Failed to save portal after re-pointing conversation ID")
	}
}

// reIDRCSGroupPredecessor merges the SMS/MMS portal a group was upgraded from onto its new RCS
// group key. Google issues an upgraded RCS group as a fresh conversation with a new identity, so
// without this the existing room and history would be abandoned and a new portal backfilled.
//
// The predecessor is matched by participant hash, guarded on participant count, and only when the
// RCS portal doesn't exist yet, so a still-live SMS group is never merged into a coincidental RCS
// group with the same members.
func (gc *GMClient) reIDRCSGroupPredecessor(ctx context.Context, ident stableIdentity, rcsKey networkid.PortalKey) {
	if ident.ParticipantHash == "" {
		return
	}
	log := zerolog.Ctx(ctx)
	rcsPortal, err := gc.Main.br.GetExistingPortalByKey(ctx, rcsKey)
	if err != nil {
		log.Err(err).Msg("Failed to look up RCS group portal before merging predecessor")
		return
	} else if rcsPortal != nil {
		return
	}
	predecessorKey, ok := gc.findParticipantHashPredecessor(ctx, ident, rcsKey)
	if !ok {
		return
	}
	log.Info().
		Stringer("predecessor_portal_key", predecessorKey).
		Stringer("rcs_portal_key", rcsKey).
		Msg("Merging SMS/MMS predecessor portal into upgraded RCS group")
	result, _, err := gc.Main.br.ReIDPortal(ctx, predecessorKey, rcsKey)
	if err != nil {
		log.Err(err).Msg("Failed to re-ID SMS/MMS predecessor onto RCS group key")
		return
	}
	log.Info().Stringer("reid_result", result).Msg("Re-ID'd SMS/MMS predecessor onto RCS group key")
}

func (gc *GMClient) findParticipantHashPredecessor(ctx context.Context, ident stableIdentity, rcsKey networkid.PortalKey) (networkid.PortalKey, bool) {
	candidates := []networkid.PortalKey{gc.MakePortalKey(stableIDPrefixParticipants + ident.ParticipantHash)}
	if legacyKey, ok := gc.legacyPortalKeyByParticipantHash.Get(ident.ParticipantHash); ok {
		candidates = append(candidates, legacyKey)
	}
	log := zerolog.Ctx(ctx)
	for _, key := range candidates {
		if key == rcsKey {
			continue
		}
		portal, err := gc.Main.br.GetExistingPortalByKey(ctx, key)
		if err != nil {
			log.Err(err).Stringer("candidate_portal_key", key).Msg("Failed to look up predecessor portal candidate")
			continue
		}
		if portal == nil {
			continue
		}
		meta, ok := portal.Metadata.(*PortalMetadata)
		if !ok || meta.ParticipantCount != ident.ParticipantCount {
			continue
		}
		return key, true
	}
	return networkid.PortalKey{}, false
}

// isLegacyPortalID reports whether a portal is still keyed on the mutable conversation ID rather
// than its stable identity.
func (gc *GMClient) isLegacyPortalID(id networkid.PortalID, meta *PortalMetadata) bool {
	return meta.StableID == "" || id != gc.MakePortalID(meta.StableID)
}
