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

package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/rs/zerolog/hlog"
	"go.mau.fi/util/exslices"
	"maunium.net/go/mautrix/event"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-gmessages/pkg/connector"
)

type Error struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
	ErrCode string `json:"errcode"`
}

func legacyProvListContacts(w http.ResponseWriter, r *http.Request) {
	login := m.Matrix.Provisioning.GetLoginForRequest(w, r)
	if login == nil {
		return
	}
	if contacts, err := login.Client.(*connector.GMClient).Client.ListContacts(); err != nil {
		hlog.FromRequest(r).Err(err).Msg("Failed to fetch user's contacts")
		jsonResponse(w, http.StatusInternalServerError, Error{
			Error:   "Internal server error while fetching contact list",
			ErrCode: "failed to get contacts",
		})
	} else {
		jsonResponse(w, http.StatusOK, contacts)
	}
}

type StartChatRequest struct {
	Numbers []string `json:"numbers"`

	CreateRCSGroup bool   `json:"create_rcs_group"`
	RCSGroupName   string `json:"rcs_group_name"`
}

type StartChatResponse struct {
	RoomID id.RoomID `json:"room_id"`
}

func legacyProvStartChat(w http.ResponseWriter, r *http.Request) {
	userLogin := m.Matrix.Provisioning.GetLoginForRequest(w, r)
	var req StartChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, Error{
			Error:   "Failed to parse request JSON",
			ErrCode: "bad json",
		})
	}
	for i, num := range req.Numbers {
		req.Numbers[i] = strings.TrimPrefix(num, "tel:")
	}
	api := userLogin.Client.(bridgev2.GroupCreatingNetworkAPI)
	var err error
	var resp *bridgev2.CreateChatResponse
	if len(req.Numbers) == 1 {
		var resolveResp *bridgev2.ResolveIdentifierResponse
		resolveResp, err = api.ResolveIdentifier(r.Context(), req.Numbers[0], true)
		if resolveResp != nil {
			resp = resolveResp.Chat
		}
	} else {
		resp, err = api.CreateGroup(r.Context(), &bridgev2.GroupCreateParams{
			Type:         "group",
			Participants: exslices.CastToString[networkid.UserID](req.Numbers),
			Name:         &event.RoomNameEventContent{Name: req.RCSGroupName},
		})
	}
	if errors.Is(err, connector.ErrRCSGroupRequiresName) {
		jsonResponse(w, http.StatusBadRequest, Error{
			Error:   "All recipients are on RCS, please create a RCS group",
			ErrCode: "rcs group",
		})
		return
	} else if err != nil {
		hlog.FromRequest(r).Err(err).Msg("Failed to start chat")
		jsonResponse(w, http.StatusInternalServerError, Error{
			Error:   "Failed to start chat",
			ErrCode: "unknown error",
		})
		return
	}
	portal, err := m.Bridge.GetPortalByKey(r.Context(), resp.PortalKey)
	if err != nil {
		hlog.FromRequest(r).Err(err).Msg("Failed to get portal")
		jsonResponse(w, http.StatusInternalServerError, Error{
			Error:   "Failed to create matrix room",
			ErrCode: "unknown error",
		})
		return
	}
	err = portal.CreateMatrixRoom(r.Context(), userLogin, resp.PortalInfo)
	if err != nil {
		hlog.FromRequest(r).Err(err).Msg("Failed to create matrix room")
		jsonResponse(w, http.StatusInternalServerError, Error{
			Error:   "Failed to create matrix room",
			ErrCode: "unknown error",
		})
		return
	}
	jsonResponse(w, http.StatusOK, StartChatResponse{portal.MXID})
}

func jsonResponse(w http.ResponseWriter, status int, response interface{}) {
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}
