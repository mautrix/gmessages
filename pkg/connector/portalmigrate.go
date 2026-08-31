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
	"fmt"

	"go.mau.fi/util/dbutil"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
)

const keyStablePortalIDs database.Key = "gmessages_stable_portal_ids"

func (gc *GMConnector) migratePortalsToUnstableIDs(ctx context.Context) error {
	log := gc.br.Log.With().Str("action", "revert stable ids").Logger()
	ctx = log.WithContext(ctx)
	if gc.br.DB.KV.Get(ctx, keyStablePortalIDs) != "true" {
		return nil
	}

	type portalInfo struct {
		ID             networkid.PortalID
		Receiver       networkid.UserLoginID
		ConversationID string
	}

	const findPortalsQuery = `
		WITH last_message_by_room AS (
			SELECT room_id, room_receiver, MAX(timestamp) AS last_message_ts
			FROM message
			WHERE bridge_id = $1
			GROUP BY room_id, room_receiver
		)
		SELECT portal.id, portal.receiver, portal.metadata->>'conversation_id'
		FROM portal
		LEFT JOIN last_message_by_room lastmsg ON lastmsg.room_id = portal.id AND lastmsg.room_receiver = portal.receiver
		WHERE portal.bridge_id = $1 AND portal.id LIKE '%:%'
		ORDER BY lastmsg.last_message_ts DESC NULLS LAST
	`

	portalInfos, err := dbutil.NewSimpleReflectRowIter[portalInfo](
		gc.br.DB.Query(ctx, findPortalsQuery, gc.br.ID),
	).AsList()
	if err != nil {
		return err
	}
	for _, info := range portalInfos {
		prefix, _ := parseAnyID(string(info.ID))
		if prefix == "" {
			return fmt.Errorf("missing ID prefix for portal %s", info.ID)
		} else if info.ConversationID == "" {
			log.Warn().Str("portal_id", string(info.ID)).Msg("Missing conversation ID for portal, skipping")
			continue
		}
		source := networkid.PortalKey{
			ID:       info.ID,
			Receiver: info.Receiver,
		}
		target := networkid.PortalKey{
			ID:       networkid.PortalID(fmt.Sprintf("%s.%s", prefix, info.ConversationID)),
			Receiver: info.Receiver,
		}
		res, _, err := gc.br.ReIDPortal(ctx, source, target)
		if err != nil {
			return fmt.Errorf("failed to re-ID portal %s to %s: %w", source, target, err)
		}
		log.Debug().
			Str("source", string(source.ID)).
			Str("target", string(target.ID)).
			Stringer("result", res).
			Msg("Reverted stable ID")
	}

	gc.br.DB.KV.Set(ctx, keyStablePortalIDs, "false")
	return nil
}
