// mautrix-gmessages - A Matrix-Google Messages puppeting bridge.
// Copyright (C) 2026 Nick Mills-Barrett, Beeper
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
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// one time marker in the database
const keyStablePortalIDs database.Key = "gmessages_stable_portal_ids"

type migrationCandidate struct {
	portal *database.Portal
	meta   *PortalMetadata
	// convID is the conversation ID this portal is currently keyed on.
	convID string
	// lastMessage is only populated for portals that collide, since it costs a query each.
	lastMessage time.Time
}

type portalTombstone struct {
	source id.RoomID
	target id.RoomID
}

// migrationTempIDPrefix marks the parking keys used to vacate a portal's old key before moving it
// to its stable one. Nothing outside a single migration transaction ever sees these.
const migrationTempIDPrefix = "__stableidmigration__"

type pendingMove struct {
	temp   networkid.PortalKey
	target networkid.PortalKey
}

// migratePortalsToStableIDs re-keys portals from Google's mutable conversation ID onto the stable
// conversation identity, merging portals that turn out to be the same chat.
//
// This must run before any user login connects, so that no incoming conversation update can land
// while portals are half-migrated.
func (gc *GMConnector) migratePortalsToStableIDs(ctx context.Context) (postMigrate func(), err error) {
	log := gc.br.Log.With().Str("action", "migrate portals to stable ids").Logger()
	ctx = log.WithContext(ctx)
	if gc.br.DB.KV.Get(ctx, keyStablePortalIDs) == "true" {
		return nil, nil
	}
	portals, err := gc.br.DB.Portal.GetAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get portals: %w", err)
	}
	groups, skipped := planStableIDMigration(portals, &log)

	var tombstones []portalTombstone
	var moves []pendingMove
	var reIDd, merged int
	err = gc.br.DB.DoTxn(ctx, nil, func(ctx context.Context) error {
		tombstones, moves = nil, nil
		reIDd, merged = 0, 0
		for target, group := range groups {
			if len(group) > 1 {
				if err := gc.loadCandidateRecency(ctx, group); err != nil {
					return err
				}
			}
			slices.SortFunc(group, compareMigrationCandidates)
			winner, losers := group[0], group[1:]
			for _, loser := range losers {
				log.Info().
					Stringer("loser_portal_key", loser.portal.PortalKey).
					Stringer("winner_portal_key", winner.portal.PortalKey).
					Stringer("target_portal_key", target).
					Stringer("loser_mxid", loser.portal.MXID).
					Time("loser_last_message", loser.lastMessage).
					Time("winner_last_message", winner.lastMessage).
					Msg("Merging split portal into the more recently active one")
				if loser.portal.MXID != "" && winner.portal.MXID != "" {
					tombstones = append(tombstones, portalTombstone{
						source: loser.portal.MXID,
						target: winner.portal.MXID,
					})
				}
				if err := gc.br.DB.Portal.Delete(ctx, loser.portal.PortalKey); err != nil {
					return fmt.Errorf("failed to delete merged portal %s: %w", loser.portal.PortalKey, err)
				}
				merged++
			}
			// The conversation ID stops being derivable from the portal key here, so it has to be
			// persisted before the re-ID or sending breaks.
			if winner.convID != "" {
				winner.meta.ConversationID = winner.convID
			}
			if err := gc.br.DB.Portal.Update(ctx, winner.portal); err != nil {
				return fmt.Errorf("failed to update portal %s: %w", winner.portal.PortalKey, err)
			}
			if winner.portal.PortalKey == target {
				continue
			}
			// Keep the old key clear to avoid conflicts via a temporary key
			prefix, _ := parseAnyID(string(winner.portal.PortalKey.ID))
			temp := networkid.PortalKey{
				ID:       networkid.PortalID(fmt.Sprintf("%s.%s%d", prefix, migrationTempIDPrefix, len(moves))),
				Receiver: winner.portal.Receiver,
			}
			if err := gc.br.DB.Portal.ReID(ctx, winner.portal.PortalKey, temp); err != nil {
				return fmt.Errorf("failed to re-ID portal %s to temporary key: %w", winner.portal.PortalKey, err)
			}
			moves = append(moves, pendingMove{temp: temp, target: target})
		}
		for _, move := range moves {
			if err := gc.br.DB.Portal.ReID(ctx, move.temp, move.target); err != nil {
				return fmt.Errorf("failed to re-ID portal %s to %s: %w", move.temp, move.target, err)
			}
			reIDd++
		}
		// Inside the transaction, so a crash between the re-IDs and the flag can't leave the
		// migration marked done when it isn't.
		gc.br.DB.KV.Set(ctx, keyStablePortalIDs, "true")
		return nil
	})
	if err != nil {
		return nil, err
	}
	log.Info().
		Int("re_ided", reIDd).
		Int("merged", merged).
		Int("skipped", skipped).
		Int("tombstones", len(tombstones)).
		Msg("Migrated portals to stable IDs")

	if len(tombstones) == 0 {
		return nil, nil
	}
	// Detached from the startup context, which may be cancelled once the bridge has started.
	cleanupCtx := context.WithoutCancel(ctx)
	return func() {
		gc.tombstoneMergedPortals(cleanupCtx, tombstones)
	}, nil
}

// planStableIDMigration groups portals by the stable key they should end up on. Portals that
// collapse onto the same key are the split we're fixing: one chat, several conversation IDs.
// Portals with no stable ID metadata are counted as skipped and left where they are.
func planStableIDMigration(
	portals []*database.Portal,
	log *zerolog.Logger,
) (groups map[networkid.PortalKey][]*migrationCandidate, skipped int) {
	byKey := make(map[networkid.PortalKey]*database.Portal, len(portals))
	for _, portal := range portals {
		byKey[portal.PortalKey] = portal
	}
	groups = make(map[networkid.PortalKey][]*migrationCandidate)
	for _, portal := range portals {
		meta, ok := portal.Metadata.(*PortalMetadata)
		if !ok || portal.Receiver == "" {
			continue
		}
		if meta.StableID == "" {
			skipped++
			continue
		}
		prefix, convID := parseAnyID(string(portal.ID))
		if prefix == "" {
			log.Warn().Str("portal_id", string(portal.ID)).Msg("Skipping portal with unparseable ID")
			skipped++
			continue
		}
		target := networkid.PortalKey{
			ID:       networkid.PortalID(fmt.Sprintf("%s.%s", prefix, meta.StableID)),
			Receiver: portal.Receiver,
		}
		if portal.PortalKey == target {
			convID = meta.ConversationID
		}
		groups[target] = append(groups[target], &migrationCandidate{
			portal: portal,
			meta:   meta,
			convID: convID,
		})
	}

	// A portal already sitting on the target key (a re-run, or a conversation ID that happens to
	// equal its stable ID) competes with the rest of its group rather than being clobbered.
	for target, group := range groups {
		if slices.ContainsFunc(group, func(c *migrationCandidate) bool { return c.portal.PortalKey == target }) {
			continue
		}
		existing, ok := byKey[target]
		if !ok {
			continue
		}
		meta, ok := existing.Metadata.(*PortalMetadata)
		if !ok {
			continue
		}
		groups[target] = append(group, &migrationCandidate{
			portal: existing,
			meta:   meta,
			convID: meta.ConversationID,
		})
	}
	return groups, skipped
}

func (gc *GMConnector) loadCandidateRecency(ctx context.Context, group []*migrationCandidate) error {
	for _, candidate := range group {
		messages, err := gc.br.DB.Message.GetLastNInPortal(ctx, candidate.portal.PortalKey, 1)
		if err != nil {
			return fmt.Errorf("failed to get last message in portal %s: %w", candidate.portal.PortalKey, err)
		}
		if len(messages) > 0 {
			candidate.lastMessage = messages[0].Timestamp
		}
	}
	return nil
}

func compareMigrationCandidates(a, b *migrationCandidate) int {
	if res := cmp.Compare(boolToInt(b.portal.MXID != ""), boolToInt(a.portal.MXID != "")); res != 0 {
		return res
	}
	if res := b.lastMessage.Compare(a.lastMessage); res != 0 {
		return res
	}
	return cmp.Compare(a.portal.ID, b.portal.ID)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (gc *GMConnector) tombstoneMergedPortals(ctx context.Context, tombstones []portalTombstone) {
	log := zerolog.Ctx(ctx)
	for _, ts := range tombstones {
		_, err := gc.br.Bot.SendState(ctx, ts.source, event.StateTombstone, "", &event.Content{
			Parsed: &event.TombstoneEventContent{
				Body:            "This chat has been merged",
				ReplacementRoom: ts.target,
			},
		}, time.Now())
		if err != nil {
			log.Err(err).Stringer("source_mxid", ts.source).Msg("Failed to tombstone merged portal room")
		}
		if err = gc.br.Bot.DeleteRoom(ctx, ts.source, err == nil); err != nil {
			log.Err(err).Stringer("source_mxid", ts.source).Msg("Failed to delete merged portal room")
		}
	}
	log.Info().Int("room_count", len(tombstones)).Msg("Finished cleaning up merged portal rooms")
}
