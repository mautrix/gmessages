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
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/util/jsontime"
	"go.mau.fi/util/ptr"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"

	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

var _ bridgev2.PortalBridgeInfoFillingNetwork = (*GMConnector)(nil)

func (gc *GMConnector) FillPortalBridgeInfo(portal *bridgev2.Portal, content *event.BridgeEventContent) {
	switch portal.Metadata.(*PortalMetadata).Type {
	case gmproto.ConversationType_SMS:
		content.Protocol.ID = "gmessages-sms"
		content.Protocol.DisplayName = "Google Messages (SMS)"
	case gmproto.ConversationType_RCS:
		content.Protocol.ID = "gmessages-rcs"
		content.Protocol.DisplayName = "Google Messages (RCS)"
	}
}

func (gc *GMClient) GetChatInfo(ctx context.Context, portal *bridgev2.Portal) (*bridgev2.ChatInfo, error) {
	conversationID, err := gc.ParsePortalID(portal.ID)
	if err != nil {
		return nil, err
	}
	zerolog.Ctx(ctx).Info().Str("conversation_id", conversationID).Msg("Manually fetching chat info")
	conv, err := gc.Client.GetConversation(conversationID)
	if err != nil {
		return nil, err
	}
	switch conv.GetStatus() {
	case gmproto.ConversationStatus_SPAM_FOLDER, gmproto.ConversationStatus_BLOCKED_FOLDER, gmproto.ConversationStatus_DELETED:
		return nil, fmt.Errorf("conversation is in a blocked status: %s", conv.GetStatus())
	}
	return gc.wrapChatInfo(ctx, conv), nil
}

func (gc *GMClient) GetUserInfo(ctx context.Context, ghost *bridgev2.Ghost) (*bridgev2.UserInfo, error) {
	return nil, nil
}

func (gc *GMClient) wrapChatInfo(ctx context.Context, conv *gmproto.Conversation) *bridgev2.ChatInfo {
	log := zerolog.Ctx(ctx)
	roomType := database.RoomTypeDefault
	if !conv.IsGroupChat {
		roomType = database.RoomTypeDM
	}
	var name *string
	if conv.IsGroupChat {
		name = &conv.Name
	} else {
		name = bridgev2.DefaultChatName
	}
	userLoginChanged := false
	eventsDefaultPL := 0
	if conv.ReadOnly {
		eventsDefaultPL = 50
	}
	members := &bridgev2.ChatMemberList{
		IsFull: true,
		MemberMap: map[networkid.UserID]bridgev2.ChatMember{
			"": {EventSender: bridgev2.EventSender{IsFromMe: true}},
		},
		PowerLevels: &bridgev2.PowerLevelOverrides{
			Events: map[event.Type]int{
				event.StateRoomName:   0,
				event.StateRoomAvatar: 0,
				event.EventReaction:   eventsDefaultPL,
				event.EventRedaction:  0,
			},
			UsersDefault:  ptr.Ptr(0),
			EventsDefault: ptr.Ptr(eventsDefaultPL),
			StateDefault:  ptr.Ptr(99),
			Invite:        ptr.Ptr(99),
			Kick:          ptr.Ptr(99),
			Ban:           ptr.Ptr(99),
			Redact:        ptr.Ptr(0),
		},
	}
	hasSelf := false
	for _, pcp := range conv.Participants {
		if pcp.IsMe {
			hasSelf = true
			if gc.Meta.AddSelfParticipantID(pcp.ID.ParticipantID) {
				log.Debug().Any("participant", pcp).Msg("Added conversation participant to self participant IDs")
				userLoginChanged = true
			}
		} else if pcp.ID.Number == "" {
			log.Warn().Any("participant", pcp).Msg("No number found in non-self participant entry")
		} else if !pcp.IsVisible {
			log.Debug().Any("participant", pcp).Msg("Ignoring fake participant")
		} else {
			userID := gc.MakeUserID(pcp.ID.ParticipantID)
			members.MemberMap[userID] = bridgev2.ChatMember{
				EventSender: bridgev2.EventSender{Sender: gc.MakeUserID(pcp.ID.ParticipantID)},
				UserInfo:    gc.wrapParticipantInfo(pcp),
				PowerLevel:  ptr.Ptr(50),
			}
		}
	}
	// Override read-only flag for group chats to avoid race conditions. When created, groups are
	// initially read-only and turn writable very quickly. They're not read-only in any other case
	// except when leaving groups, so if we're in the group, treat it as writable.
	if conv.ReadOnly && conv.IsGroupChat && hasSelf {
		members.PowerLevels.Events[event.EventReaction] = 0
		members.PowerLevels.EventsDefault = ptr.Ptr(0)
	}
	if userLoginChanged {
		err := gc.UserLogin.Save(ctx)
		if err != nil {
			log.Warn().Msg("Failed to save user login")
		}
	}
	var tag event.RoomTag
	if conv.Pinned {
		tag = event.RoomTagFavourite
	} else if conv.Status == gmproto.ConversationStatus_ARCHIVED || conv.Status == gmproto.ConversationStatus_KEEP_ARCHIVED {
		tag = event.RoomTagLowPriority
	}
	return &bridgev2.ChatInfo{
		Name:    name,
		Members: members,
		Type:    &roomType,
		UserLocal: &bridgev2.UserLocalPortalInfo{
			Tag: &tag,
		},
		CanBackfill: true,
		ExtraUpdates: func(ctx context.Context, portal *bridgev2.Portal) (changed bool) {
			meta := portal.Metadata.(*PortalMetadata)
			if meta.Type != conv.Type {
				meta.Type = conv.Type
				changed = true
			}
			if meta.SendMode != conv.SendMode {
				meta.SendMode = conv.SendMode
				changed = true
			}
			if meta.OutgoingID != conv.DefaultOutgoingID {
				meta.OutgoingID = conv.DefaultOutgoingID
				changed = true
			}
			return
		},
	}
}

const MinAvatarUpdateInterval = 24 * time.Hour

func (gc *GMClient) updateGhostAvatar(ctx context.Context, ghost *bridgev2.Ghost) (bool, error) {
	meta := ghost.Metadata.(*GhostMetadata)
	if time.Since(meta.AvatarUpdateTS.Time) < MinAvatarUpdateInterval {
		return false, nil
	} else if meta.ContactID == "" && !phoneNumberMightHaveAvatar(meta.Phone) {
		return false, nil
	}
	participantID, err := gc.ParseUserID(ghost.ID)
	if err != nil {
		return false, err
	}
	resp, err := gc.Client.GetParticipantThumbnail(participantID)
	if err != nil {
		return false, fmt.Errorf("failed to get participant thumbnail: %w", err)
	}
	meta.AvatarUpdateTS = jsontime.UnixMilliNow()
	if len(resp.Thumbnail) == 0 || len(resp.Thumbnail[0].GetData().GetImageBuffer()) == 0 {
		ghost.UpdateAvatar(ctx, &bridgev2.Avatar{Remove: true})
	} else {
		data := resp.Thumbnail[0].GetData().GetImageBuffer()
		ghost.UpdateAvatar(ctx, &bridgev2.Avatar{
			ID: networkid.AvatarID(fmt.Sprintf("hash:%x", sha256.Sum256(data))),
			Get: func(ctx context.Context) ([]byte, error) {
				return data, nil
			},
		})
	}
	return true, nil
}

const GeminiPhoneNumber = "+18339913448"

func phoneNumberMightHaveAvatar(phone string) bool {
	return strings.HasSuffix(phone, ".goog") || phone == GeminiPhoneNumber
}

func (gc *GMClient) wrapParticipantInfo(contact *gmproto.Participant) *bridgev2.UserInfo {
	return gc.makeUserInfo(
		contact.GetID().GetNumber(),
		contact.GetFormattedNumber(),
		contact.GetContactID(),
		contact.GetFullName(),
		contact.GetFirstName(),
	)
}

func (gc *GMClient) wrapContactInfo(contact *gmproto.Contact) *bridgev2.UserInfo {
	return gc.makeUserInfo(
		contact.GetNumber().GetNumber(),
		contact.GetNumber().GetFormattedNumber(),
		contact.GetContactID(),
		contact.GetName(),
		"",
	)
}

func (gc *GMClient) makeUserInfo(phone, formattedNumber, contactID, fullName, firstName string) *bridgev2.UserInfo {
	var identifiers []string
	if phone != "" {
		identifiers = append(identifiers, fmt.Sprintf("tel:%s", phone))
	}
	return &bridgev2.UserInfo{
		Identifiers: identifiers,
		Name:        ptr.Ptr(gc.Main.Config.FormatDisplayname(formattedNumber, fullName, firstName)),
		IsBot:       ptr.Ptr(false),
		ExtraUpdates: func(ctx context.Context, ghost *bridgev2.Ghost) (changed bool) {
			meta := ghost.Metadata.(*GhostMetadata)
			if meta.ContactID != contactID {
				changed = true
				meta.ContactID = contactID
			}
			if meta.Phone != phone {
				changed = true
				meta.Phone = phone
			}
			avatarChanged, err := gc.updateGhostAvatar(ctx, ghost)
			if err != nil {
				zerolog.Ctx(ctx).Err(err).Msg("Failed to update ghost avatar")
			}
			changed = changed || avatarChanged
			return
		},
	}
}
