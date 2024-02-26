// mautrix-gmessages - A Matrix-Google Messages puppeting bridge.
// Copyright (C) 2023 Tulir Asokan
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
	"context"
	"encoding/json"
	"errors"
	"net/http"
	_ "net/http/pprof"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"

	log "maunium.net/go/maulogger/v2"

	"maunium.net/go/mautrix/bridge/status"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-gmessages/libgm"
	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
)

type ProvisioningAPI struct {
	bridge *GMBridge
	log    log.Logger
	zlog   zerolog.Logger
}

func (prov *ProvisioningAPI) Init() {
	prov.log = prov.bridge.Log.Sub("Provisioning")
	prov.zlog = prov.bridge.ZLog.With().Str("component", "provisioning").Logger()

	prov.log.Debugln("Enabling provisioning API at", prov.bridge.Config.Bridge.Provisioning.Prefix)
	r := prov.bridge.AS.Router.PathPrefix(prov.bridge.Config.Bridge.Provisioning.Prefix).Subrouter()
	r.Use(prov.AuthMiddleware)
	r.HandleFunc("/v1/ping", prov.Ping).Methods(http.MethodGet)
	r.HandleFunc("/v1/login", prov.Login).Methods(http.MethodPost)
	r.HandleFunc("/v1/google_login/emoji", prov.GoogleLoginStart).Methods(http.MethodPost)
	r.HandleFunc("/v1/google_login/wait", prov.GoogleLoginWait).Methods(http.MethodPost)
	r.HandleFunc("/v1/logout", prov.Logout).Methods(http.MethodPost)
	r.HandleFunc("/v1/delete_session", prov.DeleteSession).Methods(http.MethodPost)
	r.HandleFunc("/v1/disconnect", prov.Disconnect).Methods(http.MethodPost)
	r.HandleFunc("/v1/reconnect", prov.Reconnect).Methods(http.MethodPost)
	r.HandleFunc("/v1/contacts", prov.ListContacts).Methods(http.MethodGet)
	r.HandleFunc("/v1/start_chat", prov.StartChat).Methods(http.MethodPost)
	prov.bridge.AS.Router.HandleFunc("/_matrix/app/com.beeper.asmux/ping", prov.BridgeStatePing).Methods(http.MethodPost)
	prov.bridge.AS.Router.HandleFunc("/_matrix/app/com.beeper.bridge_state", prov.BridgeStatePing).Methods(http.MethodPost)

	if prov.bridge.Config.Bridge.Provisioning.DebugEndpoints {
		prov.zlog.Debug().Msg("Enabling debug API at /debug")
		r := prov.bridge.AS.Router.PathPrefix("/debug").Subrouter()
		r.Use(prov.AuthMiddleware)
		r.PathPrefix("/pprof").Handler(http.DefaultServeMux)
	}

	// Deprecated, just use /disconnect
	r.HandleFunc("/v1/delete_connection", prov.Disconnect).Methods(http.MethodPost)
}

type responseWrap struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWrap) WriteHeader(statusCode int) {
	rw.ResponseWriter.WriteHeader(statusCode)
	rw.statusCode = statusCode
}

func (prov *ProvisioningAPI) AuthMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if strings.HasPrefix(auth, "Bearer ") {
			auth = auth[len("Bearer "):]
		}
		if auth != prov.bridge.Config.Bridge.Provisioning.SharedSecret {
			prov.log.Infof("Authentication token does not match shared secret")
			jsonResponse(w, http.StatusForbidden, map[string]interface{}{
				"error":   "Authentication token does not match shared secret",
				"errcode": "M_FORBIDDEN",
			})
			return
		}
		userID := r.URL.Query().Get("user_id")
		user := prov.bridge.GetUserByMXID(id.UserID(userID))
		start := time.Now()
		wWrap := &responseWrap{w, 200}
		h.ServeHTTP(wWrap, r.WithContext(context.WithValue(r.Context(), "user", user)))
		duration := time.Now().Sub(start).Seconds()
		prov.log.Infofln("%s %s from %s took %.2f seconds and returned status %d", r.Method, r.URL.Path, user.MXID, duration, wWrap.statusCode)
	})
}

type Error struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
	ErrCode string `json:"errcode"`
}

type Response struct {
	Success bool   `json:"success"`
	Status  string `json:"status"`
}

func (prov *ProvisioningAPI) DeleteSession(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*User)
	if user.Session == nil && user.Client == nil {
		jsonResponse(w, http.StatusNotFound, Error{
			Error:   "Nothing to purge: no session information stored and no active connection.",
			ErrCode: "no session",
		})
		return
	}
	user.Logout(status.BridgeState{StateEvent: status.StateLoggedOut}, false)
	jsonResponse(w, http.StatusOK, Response{true, "Session information purged"})
}

func (prov *ProvisioningAPI) Disconnect(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*User)
	if user.Client == nil {
		jsonResponse(w, http.StatusNotFound, Error{
			Error:   "You don't have a Google Messages connection.",
			ErrCode: "no connection",
		})
		return
	}
	user.DeleteConnection()
	jsonResponse(w, http.StatusOK, Response{true, "Disconnected from Google Messages"})
	user.BridgeState.Send(status.BridgeState{StateEvent: status.StateTransientDisconnect, Error: GMNotConnected})
}

func (prov *ProvisioningAPI) Reconnect(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*User)
	if user.Client == nil {
		if user.Session == nil {
			jsonResponse(w, http.StatusForbidden, Error{
				Error:   "No existing connection and no session. Please log in first.",
				ErrCode: "no session",
			})
		} else {
			user.Connect()
			jsonResponse(w, http.StatusAccepted, Response{true, "Created connection to Google Messages."})
		}
	} else {
		user.DeleteConnection()
		user.BridgeState.Send(status.BridgeState{StateEvent: status.StateTransientDisconnect, Error: GMNotConnected})
		user.Connect()
		jsonResponse(w, http.StatusAccepted, Response{true, "Restarted connection to Google Messages"})
	}
}

func (prov *ProvisioningAPI) ListContacts(w http.ResponseWriter, r *http.Request) {
	if user := r.Context().Value("user").(*User); user.Client == nil {
		jsonResponse(w, http.StatusBadRequest, Error{
			Error:   "User is not connected to Google Messages",
			ErrCode: "no session",
		})
	} else if contacts, err := user.Client.ListContacts(); err != nil {
		prov.log.Errorfln("Failed to fetch %s's contacts: %v", user.MXID, err)
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

func (prov *ProvisioningAPI) StartChat(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*User)
	if user.Client == nil {
		jsonResponse(w, http.StatusBadRequest, Error{
			Error:   "User is not connected to Google Messages",
			ErrCode: "no session",
		})
	}
	var req StartChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, Error{
			Error:   "Failed to parse request JSON",
			ErrCode: "bad json",
		})
	}
	var reqData gmproto.GetOrCreateConversationRequest
	reqData.Numbers = make([]*gmproto.ContactNumber, 0, len(req.Numbers))
	for _, number := range req.Numbers {
		reqData.Numbers = append(reqData.Numbers, &gmproto.ContactNumber{
			// This should maybe sometimes be 7
			MysteriousInt: 2,
			Number:        number,
			Number2:       number,
		})
	}
	if req.CreateRCSGroup {
		reqData.CreateRCSGroup = proto.Bool(true)
		reqData.RCSGroupName = proto.String(req.RCSGroupName)
	}
	resp, err := user.Client.GetOrCreateConversation(&reqData)
	if err != nil {
		prov.zlog.Err(err).Msg("Failed to start chat")
		jsonResponse(w, http.StatusInternalServerError, Error{
			Error:   "Failed to start chat",
			ErrCode: "unknown error",
		})
		return
	} else if len(req.Numbers) > 1 && resp.GetStatus() == gmproto.GetOrCreateConversationResponse_CREATE_RCS {
		jsonResponse(w, http.StatusBadRequest, Error{
			Error:   "All recipients are on RCS, please create a RCS group",
			ErrCode: "rcs group",
		})
		return
	}
	if resp.GetConversation() == nil {
		prov.zlog.Warn().
			Int("req_number_count", len(req.Numbers)).
			Str("status", resp.GetStatus().String()).
			Msg("No conversation in chat create response")
		jsonResponse(w, http.StatusInternalServerError, Error{
			Error:   "Failed to start chat",
			ErrCode: "unknown error",
		})
		return
	}
	convCopy := proto.Clone(resp.Conversation).(*gmproto.Conversation)
	convCopy.LatestMessage = nil
	prov.zlog.Debug().Any("conversation_data", convCopy).Msg("Got conversation data for start chat")
	portal := user.GetPortalByID(resp.Conversation.ConversationID)
	err = portal.CreateMatrixRoom(r.Context(), user, resp.Conversation, false)
	if err != nil {
		prov.zlog.Err(err).Msg("Failed to create matrix room")
		jsonResponse(w, http.StatusInternalServerError, Error{
			Error:   "Failed to create matrix room",
			ErrCode: "unknown error",
		})
		return
	}
	jsonResponse(w, http.StatusOK, StartChatResponse{portal.MXID})
}

func (prov *ProvisioningAPI) Ping(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*User)
	gm := map[string]interface{}{
		"has_session": user.Session != nil,
		"conn":        nil,
	}
	if user.Session != nil {
		gm["phone_id"] = user.Session.Mobile.SourceID
		gm["browser_id"] = user.Session.Browser.SourceID
	}
	if user.Client != nil {
		gm["conn"] = map[string]interface{}{
			"is_connected": user.Client.IsConnected(),
			"is_logged_in": user.Client.IsLoggedIn(),
		}
	}
	resp := map[string]interface{}{
		"mxid":            user.MXID,
		"admin":           user.Admin,
		"whitelisted":     user.Whitelisted,
		"management_room": user.ManagementRoom,
		"space_room":      user.SpaceRoom,
		"gmessages":       gm,
	}
	jsonResponse(w, http.StatusOK, resp)
}

func jsonResponse(w http.ResponseWriter, status int, response interface{}) {
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}

func (prov *ProvisioningAPI) Logout(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*User)
	if user.Session == nil {
		jsonResponse(w, http.StatusOK, Error{
			Error:   "You're not logged in",
			ErrCode: "not logged in",
		})
		return
	}

	user.Logout(status.BridgeState{StateEvent: status.StateLoggedOut}, true)
	jsonResponse(w, http.StatusOK, Response{true, "Logged out successfully."})
}

type ReqGoogleLoginStart struct {
	Cookies map[string]string
}

type RespGoogleLoginStart struct {
	Status string `json:"status"`
	Emoji  string `json:"emoji"`
}

func (prov *ProvisioningAPI) GoogleLoginStart(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	user := prov.bridge.GetUserByMXID(id.UserID(userID))

	log := prov.zlog.With().Str("user_id", user.MXID.String()).Str("endpoint", "login").Logger()

	if user.IsLoggedIn() {
		jsonResponse(w, http.StatusOK, LoginResponse{Status: "success", ErrCode: "already logged in"})
		return
	}
	var req ReqGoogleLoginStart
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, Error{
			Error:   "Failed to parse request JSON",
			ErrCode: "bad json",
		})
	}
	emoji, err := user.AsyncLoginGoogleStart(req.Cookies)
	if err != nil {
		log.Err(err).Msg("Failed to start login")
		switch {
		case errors.Is(err, libgm.ErrNoDevicesFound):
			jsonResponse(w, http.StatusBadRequest, Error{
				Error:   err.Error(),
				ErrCode: "no-devices-found",
			})
		default:
			jsonResponse(w, http.StatusInternalServerError, Error{
				Error:   "Failed to start login",
				ErrCode: "unknown",
			})
		}
		return
	}
	jsonResponse(w, http.StatusOK, &RespGoogleLoginStart{Status: "emoji", Emoji: emoji})
}

func (prov *ProvisioningAPI) GoogleLoginWait(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	user := prov.bridge.GetUserByMXID(id.UserID(userID))

	log := prov.zlog.With().Str("user_id", user.MXID.String()).Str("endpoint", "login").Logger()

	err := user.AsyncLoginGoogleWait(r.Context())
	if err != nil {
		log.Err(err).Msg("Failed to wait for google login")
		switch {
		case errors.Is(err, ErrNoLoginInProgress):
			jsonResponse(w, http.StatusBadRequest, Error{
				Error:   "No login in progress",
				ErrCode: "login-not-in-progress",
			})
		case errors.Is(err, libgm.ErrIncorrectEmoji):
			jsonResponse(w, http.StatusBadRequest, Error{
				Error:   err.Error(),
				ErrCode: "incorrect-emoji",
			})
		case errors.Is(err, libgm.ErrPairingCancelled):
			jsonResponse(w, http.StatusBadRequest, Error{
				Error:   err.Error(),
				ErrCode: "pairing-cancelled",
			})
		case errors.Is(err, libgm.ErrPairingTimeout):
			jsonResponse(w, http.StatusBadRequest, Error{
				Error:   err.Error(),
				ErrCode: "timeout",
			})
		case errors.Is(err, context.Canceled):
			// This should only happen if the client already disconnected, so clients will probably never see this error code.
			jsonResponse(w, http.StatusBadRequest, Error{
				Error:   err.Error(),
				ErrCode: "context-cancelled",
			})
		default:
			jsonResponse(w, http.StatusInternalServerError, Error{
				Error:   "Failed to finish login",
				ErrCode: "unknown",
			})
		}
		return
	}
	jsonResponse(w, http.StatusOK, LoginResponse{Status: "success"})
}

type LoginResponse struct {
	Status  string `json:"status"`
	Code    string `json:"code,omitempty"`
	ErrCode string `json:"errcode,omitempty"`
	Error   string `json:"error,omitempty"`
}

func (prov *ProvisioningAPI) Login(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	user := prov.bridge.GetUserByMXID(id.UserID(userID))

	log := prov.zlog.With().Str("user_id", user.MXID.String()).Str("endpoint", "login").Logger()

	if user.IsLoggedIn() {
		jsonResponse(w, http.StatusOK, LoginResponse{Status: "success", ErrCode: "already logged in"})
		return
	}

	ch, err := user.Login(5)
	if err != nil && !errors.Is(err, ErrLoginInProgress) {
		log.Err(err).Msg("Failed to start login via provisioning API")
		jsonResponse(w, http.StatusInternalServerError, Error{
			Error:   "Failed to start login",
			ErrCode: "start login fail",
		})
		return
	}
	if errors.Is(err, ErrLoginInProgress) && ch == nil {
		log.Err(err).Msg("Tried to start QR login while non-QR login is in progress")
		jsonResponse(w, http.StatusBadRequest, Error{
			Error:   "Non-QR login already in progress",
			ErrCode: "unknown",
		})
		return
	}

	var item, prevItem qrChannelItem
	var hasItem bool
Loop:
	for {
		prevItem = item
		select {
		case item = <-ch:
			hasItem = true
		default:
			break Loop
		}
	}
	if !hasItem && r.URL.Query().Get("return_immediately") == "true" && user.lastQRCode != "" {
		log.Debug().Msg("Nothing in QR channel, returning last code immediately")
		item.qr = user.lastQRCode
	} else if !hasItem {
		log.Debug().Msg("Nothing in QR channel, waiting for next item")
		select {
		case item = <-ch:
		case <-r.Context().Done():
			log.Warn().Err(r.Context().Err()).Msg("Client left while waiting for QR code")
			return
		}
	} else if item.IsEmpty() && !prevItem.IsEmpty() {
		item = prevItem
	}

	switch {
	case item.qr != "":
		log.Debug().Msg("Got code in QR channel")
		Analytics.Track(user.MXID, "$qrcode_retrieved")
		jsonResponse(w, http.StatusOK, LoginResponse{Status: "qr", Code: item.qr})
	case item.err != nil:
		log.Err(item.err).Msg("Got error in QR channel")
		Analytics.Track(user.MXID, "$login_failure")
		var resp LoginResponse
		switch {
		case errors.Is(item.err, ErrLoginTimeout):
			resp = LoginResponse{ErrCode: "timeout", Error: "Scanning QR code timed out"}
		default:
			resp = LoginResponse{ErrCode: "unknown", Error: "Login failed"}
		}
		resp.Status = "fail"
		jsonResponse(w, http.StatusOK, resp)
	case item.success:
		log.Debug().Msg("Got pair success in QR channel")
		Analytics.Track(user.MXID, "$login_success")
		jsonResponse(w, http.StatusOK, LoginResponse{Status: "success"})
	default:
		log.Error().Any("item_data", item).Msg("Unknown item in QR channel")
		resp := LoginResponse{Status: "fail", ErrCode: "internal-error", Error: "Unknown item in login channel"}
		jsonResponse(w, http.StatusInternalServerError, resp)
	}
}
