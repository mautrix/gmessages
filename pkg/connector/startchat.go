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
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"

	"github.com/rs/zerolog"
	"go.mau.fi/util/ptr"
	"google.golang.org/protobuf/proto"
	"maunium.net/go/mautrix"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"

	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

var (
	_ bridgev2.IdentifierResolvingNetworkAPI = (*GMClient)(nil)
	_ bridgev2.GroupCreatingNetworkAPI       = (*GMClient)(nil)
	_ bridgev2.ContactListingNetworkAPI      = (*GMClient)(nil)
	_ bridgev2.IdentifierValidatingNetwork   = (*GMConnector)(nil)
)

func isNumber(char rune) bool {
	return char >= '0' && char <= '9'
}

func isHexLetter(char rune) bool {
	return char >= 'a' && char <= 'f'
}

func (gc *GMConnector) ValidateUserID(id networkid.UserID) bool {
	p1, p2 := parseAnyID(string(id))
	if len(p1) == 0 || len(p2) == 0 {
		return false
	}
	for _, d := range p1 {
		if !isNumber(d) && (!gc.Config.DeterministicIDPrefix || !isHexLetter(d)) {
			return false
		}
	}
	for _, d := range p2 {
		if !isNumber(d) {
			return false
		}
	}
	return true
}

func (gc *GMClient) ResolveIdentifier(ctx context.Context, identifier string, createChat bool) (*bridgev2.ResolveIdentifierResponse, error) {
	log := zerolog.Ctx(ctx)

	var phone string
	netID := networkid.UserID(identifier)
	if gc.Main.ValidateUserID(netID) {
		ghost, err := gc.Main.br.GetExistingGhostByID(ctx, netID)
		if err != nil {
			return nil, fmt.Errorf("failed to get ghost by ID: %w", err)
		} else if ghost != nil {
			prefix, _ := parseAnyID(string(ghost.ID))
			if prefix != gc.Meta.IDPrefix {
				return nil, fmt.Errorf("%w: prefix mismatch", bridgev2.ErrResolveIdentifierTryNext)
			}
			phone = ghost.Metadata.(*GhostMetadata).Phone
			if phone == "" {
				return nil, fmt.Errorf("phone number of ghost %s not known", netID)
			}
			if !createChat {
				log.Debug().Str("ghost_id", string(ghost.ID)).Msg("Returning ghost identifier")
				return &bridgev2.ResolveIdentifierResponse{
					Ghost:  ghost,
					UserID: ghost.ID,
				}, nil
			}
		}
	}
	if phone == "" {
		var err error
		phone, err = bridgev2.CleanNonInternationalPhoneNumber(identifier)
		if err != nil {
			log.Debug().Str("input_identifier", identifier).Msg("Invalid phone number passed to ResolveIdentifier")
			return nil, bridgev2.WrapRespErrManual(err, mautrix.MInvalidParam.ErrCode, http.StatusBadRequest)
		}
	}
	if !createChat {
		// All phone numbers are probably reachable, just return a fake response
		log.Debug().Str("phone", phone).Msg("Returning fake phone number response")
		return &bridgev2.ResolveIdentifierResponse{
			UserID: networkid.UserID(phone),
		}, nil
	}
	resp, err := gc.Client.GetOrCreateConversation(&gmproto.GetOrCreateConversationRequest{
		Numbers: []*gmproto.ContactNumber{{
			// This should maybe sometimes be 7
			MysteriousInt: 2,
			Number:        phone,
			Number2:       phone,
		}},
	})
	if err != nil {
		return nil, err
	}
	convCopy := proto.Clone(resp.Conversation).(*gmproto.Conversation)
	convCopy.LatestMessage = nil
	log.Debug().Any("conversation_data", convCopy).Msg("Got conversation data for DM")
	if resp.GetConversation().GetConversationID() == "" {
		return nil, fmt.Errorf("no conversation ID in response")
	}
	portalKey := gc.MakePortalKey(resp.Conversation.ConversationID)
	portalInfo := gc.wrapChatInfo(ctx, resp.Conversation)
	var otherUserID networkid.UserID
	var otherUserInfo *gmproto.Participant
	for _, member := range resp.Conversation.Participants {
		if member.IsMe || !member.IsVisible {
			continue
		}
		if otherUserID != "" {
			log.Warn().
				Str("portal_id", string(portalKey.ID)).
				Str("previous_other_user_id", string(otherUserID)).
				Str("new_other_user_id", string(gc.MakeUserID(member.GetID().GetParticipantID()))).
				Msg("Multiple visible participants in DM")
		}
		otherUserID = gc.MakeUserID(member.GetID().GetParticipantID())
		otherUserInfo = member
	}
	var ghost *bridgev2.Ghost
	if otherUserID == "" {
		log.Warn().
			Str("portal_id", string(portalKey.ID)).
			Msg("No visible participants in DM")
	} else {
		ghost, err = gc.Main.br.GetGhostByID(ctx, otherUserID)
		if err != nil {
			return nil, fmt.Errorf("failed to get ghost: %w", err)
		}
	}
	log.Debug().Str("other_user_id", string(otherUserID)).Str("portal_key", string(portalKey.ID)).Msg("Returning new chat response")
	return &bridgev2.ResolveIdentifierResponse{
		Ghost:    ghost,
		UserID:   otherUserID,
		UserInfo: gc.wrapParticipantInfo(otherUserInfo),
		Chat: &bridgev2.CreateChatResponse{
			PortalKey:  portalKey,
			PortalInfo: portalInfo,
		},
	}, nil
}

var (
	ErrRCSGroupRequiresName = bridgev2.WrapRespErrManual(errors.New("RCS group creation requires a name"), "FI.MAU.GMESSAGES.RCS_REQUIRES_NAME", http.StatusBadRequest)
	ErrMinimumTwoUsers      = bridgev2.WrapRespErr(errors.New("need at least 2 users to create a group"), mautrix.MInvalidParam)
)

func (gc *GMClient) CreateGroup(ctx context.Context, params *bridgev2.GroupCreateParams) (*bridgev2.CreateChatResponse, error) {
	log := zerolog.Ctx(ctx)

	if len(params.Participants) < 2 {
		return nil, ErrMinimumTwoUsers
	}
	reqData := &gmproto.GetOrCreateConversationRequest{
		Numbers:      make([]*gmproto.ContactNumber, len(params.Participants)),
		RCSGroupName: ptr.NonZero(ptr.Val(params.Name).Name),
	}
	for i, user := range params.Participants {
		var phone string
		_, err := gc.ParseUserID(user)
		if err == nil {
			ghost, err := gc.Main.br.GetExistingGhostByID(ctx, user)
			if err != nil {
				return nil, fmt.Errorf("failed to get ghost %s: %w", user, err)
			}
			phone = ghost.Metadata.(*GhostMetadata).Phone
			if phone == "" {
				return nil, fmt.Errorf("phone number of ghost %s not known", ghost.ID)
			}
		} else {
			// Hack to allow ResolveIdentifier results (raw phone numbers) here
			phone = string(user)
		}
		reqData.Numbers[i] = &gmproto.ContactNumber{
			MysteriousInt: 2,
			Number:        phone,
			Number2:       phone,
		}
	}
	resp, err := gc.Client.GetOrCreateConversation(reqData)
	if resp.GetStatus() == gmproto.GetOrCreateConversationResponse_CREATE_RCS {
		if reqData.RCSGroupName == nil {
			reqData.RCSGroupName = ptr.Ptr("")
		}
		reqData.CreateRCSGroup = ptr.Ptr(true)
		resp, err = gc.Client.GetOrCreateConversation(reqData)
	}
	if err != nil {
		return nil, err
	}
	if resp.Conversation == nil {
		rawProto, _ := proto.Marshal(resp)
		log.Warn().Str("raw_response", base64.StdEncoding.EncodeToString(rawProto)).Msg("No conversation data in create group response?")
		return nil, fmt.Errorf("no conversation data in response (status: %s)", resp.GetStatus())
	}
	convCopy := proto.Clone(resp.Conversation).(*gmproto.Conversation)
	convCopy.LatestMessage = nil
	log.Debug().Any("conversation_data", convCopy).Msg("Got conversation data for new group")
	if resp.GetConversation().GetConversationID() == "" {
		return nil, fmt.Errorf("no conversation ID in response (status: %s)", resp.GetStatus())
	}
	portalKey := gc.MakePortalKey(resp.Conversation.ConversationID)
	portalInfo := gc.wrapChatInfo(ctx, resp.Conversation)
	var portal *bridgev2.Portal
	if params.RoomID != "" {
		portal, err = gc.Main.br.GetPortalByKey(ctx, portalKey)
		if err != nil {
			return nil, fmt.Errorf("failed to get portal by key: %w", err)
		}
		err = portal.UpdateMatrixRoomID(ctx, params.RoomID, bridgev2.UpdateMatrixRoomIDParams{
			SyncDBMetadata: func() {
				portal.Name = ptr.Val(params.Name).Name
				portal.NameSet = true
			},
			OverwriteOldPortal: true,
			TombstoneOldRoom:   true,
			DeleteOldRoom:      true,
			ChatInfo:           portalInfo,
			ChatInfoSource:     gc.UserLogin,
		})
		if err != nil {
			return nil, err
		}
	}
	return &bridgev2.CreateChatResponse{
		PortalKey:  portalKey,
		PortalInfo: portalInfo,
		Portal:     portal,
	}, nil
}

func (gc *GMClient) GetContactList(ctx context.Context) ([]*bridgev2.ResolveIdentifierResponse, error) {
	contacts, err := gc.Client.ListContacts()
	if err != nil {
		return nil, err
	}
	resp := make([]*bridgev2.ResolveIdentifierResponse, len(contacts.Contacts))
	for i, contact := range contacts.Contacts {
		userID := gc.MakeUserID(contact.GetParticipantID())
		ghost, err := gc.Main.br.GetGhostByID(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("failed to get ghost %s: %w", userID, err)
		}
		resp[i] = &bridgev2.ResolveIdentifierResponse{
			Ghost:    ghost,
			UserID:   userID,
			UserInfo: gc.wrapContactInfo(contact),
		}
	}
	return resp, nil
}
