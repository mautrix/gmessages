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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/rs/zerolog/hlog"
	"go.mau.fi/util/exerrors"
	"go.mau.fi/util/exslices"
	"go.mau.fi/util/exsync"

	"maunium.net/go/mautrix/bridge/status"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-gmessages/pkg/connector"
)

type inProgressLogin struct {
	old  *bridgev2.UserLogin
	proc bridgev2.LoginProcess
	step *bridgev2.LoginStep
}

var logins = exsync.NewMap[id.UserID, *inProgressLogin]()

const (
	pairingErrMsgNoDevices       = "No devices found. Make sure you've enabled account pairing in the Google Messages app on your phone."
	pairingErrPhoneNotResponding = "Phone not responding. Make sure your phone is connected to the internet and that account pairing is enabled in the Google Messages app. You may need to keep the app open and/or disable battery optimizations. Alternatively, try QR pairing"
	pairingErrMsgIncorrectEmoji  = "Incorrect emoji chosen on phone, please try again"
	pairingErrMsgCancelled       = "Pairing cancelled on phone"
	pairingErrMsgTimeout         = "Pairing timed out, please try again"
)

type Error struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
	ErrCode string `json:"errcode"`
}

type Response struct {
	Success bool   `json:"success"`
	Status  string `json:"status"`
}

func legacyProvDeleteSession(w http.ResponseWriter, r *http.Request) {
	user := m.Matrix.Provisioning.GetUser(r)
	logins := user.GetUserLogins()
	if len(logins) == 0 {
		jsonResponse(w, http.StatusNotFound, Error{
			Error:   "Nothing to purge: no session information stored and no active connection.",
			ErrCode: "no session",
		})
		return
	}
	for _, login := range logins {
		login.Delete(r.Context(), status.BridgeState{StateEvent: status.StateLoggedOut}, bridgev2.DeleteOpts{})
	}
	jsonResponse(w, http.StatusOK, Response{true, "Session information purged"})
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
		resp, err = api.CreateGroup(r.Context(), req.RCSGroupName, exslices.CastToString[networkid.UserID](req.Numbers)...)
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

func legacyProvPing(w http.ResponseWriter, r *http.Request) {
	user := m.Matrix.Provisioning.GetUser(r)
	login := user.GetDefaultLogin()
	gm := map[string]interface{}{
		"has_session": false,
		"conn":        nil,
	}
	if login != nil && login.Client != nil {
		gm["has_session"] = true
		var isConnected bool
		if client := login.Client.(*connector.GMClient).Client; client != nil {
			isConnected = client.IsConnected()
			gm["phone_id"] = client.AuthData.Mobile.SourceID
			gm["browser_id"] = client.AuthData.Browser.SourceID
		}
		gm["conn"] = map[string]interface{}{
			"is_connected": isConnected,
			"is_logged_in": login.Client.IsLoggedIn(),
		}
	}
	resp := map[string]interface{}{
		"mxid":            user.MXID,
		"admin":           user.Permissions.Admin,
		"whitelisted":     user.Permissions.Login,
		"management_room": user.ManagementRoom,
		"gmessages":       gm,
	}
	if login != nil {
		resp["space_room"] = login.SpaceRoom
	}
	jsonResponse(w, http.StatusOK, resp)
}

func jsonResponse(w http.ResponseWriter, status int, response interface{}) {
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}

func legacyProvLogout(w http.ResponseWriter, r *http.Request) {
	user := m.Matrix.Provisioning.GetUser(r)
	allLogins := user.GetUserLogins()
	if len(allLogins) == 0 {
		jsonResponse(w, http.StatusOK, Error{
			Error:   "You're not logged in",
			ErrCode: "not logged in",
		})
		return
	}
	for _, login := range allLogins {
		meta := login.Metadata.(*connector.UserLoginMetadata)
		if meta.Session != nil && meta.Session.IsGoogleAccount() && !meta.Session.HasCookies() {
			// Don't log out google accounts with no cookies, they can be relogined
			continue
		}
		login.Client.LogoutRemote(r.Context())
	}
	jsonResponse(w, http.StatusOK, Response{true, "Logged out successfully"})
}

type ReqGoogleLoginStart struct {
	Cookies map[string]string
}

type RespGoogleLoginStart struct {
	Status   string `json:"status"`
	Emoji    string `json:"emoji"`
	EmojiURL string `json:"emoji_url"`
}

func findMissingCookies(cookies map[string]string) string {
	for _, requiredCookie := range []string{"SID", "SSID", "HSID", "OSID", "APISID", "SAPISID"} {
		if _, ok := cookies[requiredCookie]; !ok {
			return requiredCookie
		}
	}
	return ""
}

func legacyProvGoogleLoginStart(w http.ResponseWriter, r *http.Request) {
	user := m.Matrix.Provisioning.GetUser(r)
	existingLogin := user.GetDefaultLogin()
	var existingClient *connector.GMClient
	if existingLogin != nil {
		existingClient = existingLogin.Client.(*connector.GMClient)
	}
	if _, alreadyExists := logins.Get(user.MXID); alreadyExists {
		jsonResponse(w, http.StatusBadRequest, Error{
			Error:   "Login already in progress",
			ErrCode: "login-in-progress",
		})
		return
	}

	log := hlog.FromRequest(r)

	if existingClient != nil && existingClient.IsLoggedIn() && !existingClient.SwitchedToGoogleLogin {
		log.Warn().Msg("User is already logged in, ignoring new login request")
		if !existingClient.PhoneResponding {
			jsonResponse(w, http.StatusConflict, LoginResponse{
				Error:   "You're already logged in, but the Google Messages app on your phone is not responding",
				ErrCode: "already logged in",
			})
		} else {
			jsonResponse(w, http.StatusConflict, LoginResponse{
				Status:  "success",
				Error:   "You're already logged in",
				ErrCode: "already logged in",
			})
		}
		return
	}
	var req ReqGoogleLoginStart
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Warn().Err(err).Msg("Failed to parse request JSON")
		jsonResponse(w, http.StatusBadRequest, Error{
			Error:   "Failed to parse request JSON",
			ErrCode: "bad json",
		})
		return
	} else if len(req.Cookies) == 0 {
		log.Warn().Msg("No cookies in request")
		jsonResponse(w, http.StatusBadRequest, Error{
			Error:   "No cookies in request",
			ErrCode: "missing cookies",
		})
		return
	} else if missingCookie := findMissingCookies(req.Cookies); missingCookie != "" {
		log.Warn().Msg("Missing cookies in request")
		jsonResponse(w, http.StatusBadRequest, Error{
			Error:   fmt.Sprintf("Missing %s cookie", missingCookie),
			ErrCode: "missing cookies",
		})
		return
	}
	login := exerrors.Must(m.Connector.CreateLogin(r.Context(), user, connector.LoginFlowIDGoogle))
	nextStep := exerrors.Must(login.(bridgev2.LoginProcessWithOverride).StartWithOverride(r.Context(), existingLogin))
	if nextStep.StepID != connector.LoginStepIDGoogle {
		log.Warn().Str("step_id", nextStep.StepID).Msg("Unexpected step after starting login")
		jsonResponse(w, http.StatusInternalServerError, Error{
			Error:   "Unexpected step after starting login",
			ErrCode: "unknown",
		})
		return
	}
	nextStep, err := login.(bridgev2.LoginProcessCookies).SubmitCookies(r.Context(), req.Cookies)
	if err != nil {
		log.Err(err).Msg("Failed to start login")
		switch {
		case errors.Is(err, connector.ErrPairNoDevices):
			jsonResponse(w, http.StatusBadRequest, Error{
				Error:   pairingErrMsgNoDevices,
				ErrCode: "no-devices-found",
			})
		case errors.Is(err, connector.ErrPairPhoneNotResponding):
			errMsg := pairingErrPhoneNotResponding
			if strings.Contains(r.UserAgent(), "; Android") {
				errMsg += " using the desktop app"
			}
			jsonResponse(w, http.StatusBadRequest, Error{
				Error:   errMsg,
				ErrCode: "timeout",
			})
		default:
			jsonResponse(w, http.StatusInternalServerError, Error{
				Error:   "Failed to start login",
				ErrCode: "unknown",
			})
		}
		return
	} else if nextStep.StepID == connector.LoginStepIDComplete {
		go handleLoginComplete(context.WithoutCancel(r.Context()), user, nextStep.CompleteParams.UserLogin)
		jsonResponse(w, http.StatusOK, LoginResponse{Status: "success"})
	} else if nextStep.StepID != connector.LoginStepIDEmoji {
		log.Warn().Str("step_id", nextStep.StepID).Msg("Unexpected step after submitting cookies")
		jsonResponse(w, http.StatusInternalServerError, Error{
			Error:   "Unexpected step after submitting cookies",
			ErrCode: "unknown",
		})
		return
	}

	logins.Set(user.MXID, &inProgressLogin{
		proc: login,
		step: nextStep,
		old:  existingLogin,
	})
	jsonResponse(w, http.StatusOK, &RespGoogleLoginStart{
		Status:   "emoji",
		Emoji:    nextStep.DisplayAndWaitParams.Data,
		EmojiURL: nextStep.DisplayAndWaitParams.ImageURL,
	})
}

func legacyProvGoogleLoginWait(w http.ResponseWriter, r *http.Request) {
	user := m.Matrix.Provisioning.GetUser(r)
	ipl, ok := logins.Pop(user.MXID)
	if !ok {
		jsonResponse(w, http.StatusBadRequest, Error{
			Error:   "No login in progress",
			ErrCode: "login-not-in-progress",
		})
		return
	}

	log := hlog.FromRequest(r)
	nextStep, err := ipl.proc.(bridgev2.LoginProcessDisplayAndWait).Wait(r.Context())
	if err != nil {
		log.Err(err).Msg("Failed to wait for google login")
		switch {
		case errors.Is(err, connector.ErrPairIncorrectEmoji):
			jsonResponse(w, http.StatusBadRequest, Error{
				Error:   pairingErrMsgIncorrectEmoji,
				ErrCode: "incorrect-emoji",
			})
		case errors.Is(err, connector.ErrPairCancelled):
			jsonResponse(w, http.StatusBadRequest, Error{
				Error:   pairingErrMsgCancelled,
				ErrCode: "pairing-cancelled",
			})
		case errors.Is(err, connector.ErrPairTimeout):
			jsonResponse(w, http.StatusBadRequest, Error{
				Error:   pairingErrMsgTimeout,
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
	} else if nextStep.StepID != connector.LoginStepIDComplete {
		log.Warn().Str("step_id", nextStep.StepID).Msg("Unexpected step after waiting for google login")
		jsonResponse(w, http.StatusInternalServerError, Error{
			Error:   "Unexpected step after waiting for google login",
			ErrCode: "unknown",
		})
		return
	}
	go handleLoginComplete(context.WithoutCancel(r.Context()), user, nextStep.CompleteParams.UserLogin)
	jsonResponse(w, http.StatusOK, LoginResponse{Status: "success"})
}

type LoginResponse struct {
	Status  string `json:"status"`
	Code    string `json:"code,omitempty"`
	ErrCode string `json:"errcode,omitempty"`
	Error   string `json:"error,omitempty"`
}

func legacyProvQRLogin(w http.ResponseWriter, r *http.Request) {
	user := m.Matrix.Provisioning.GetUser(r)
	log := hlog.FromRequest(r)
	ipl, ok := logins.Get(user.MXID)
	if !ok {
		existingLogin := user.GetDefaultLogin()
		var existingClient *connector.GMClient
		if existingLogin != nil {
			existingClient = existingLogin.Client.(*connector.GMClient)
		}
		if existingClient != nil && existingClient.IsLoggedIn() && !existingClient.SwitchedToGoogleLogin {
			log.Warn().Msg("User is already logged in, ignoring new login request")
			if !existingClient.PhoneResponding {
				jsonResponse(w, http.StatusConflict, LoginResponse{
					Error:   "You're already logged in, but the Google Messages app on your phone is not responding",
					ErrCode: "already logged in",
				})
			} else {
				jsonResponse(w, http.StatusConflict, LoginResponse{
					Status:  "success",
					Error:   "You're already logged in",
					ErrCode: "already logged in",
				})
			}
			return
		}
		login := exerrors.Must(m.Connector.CreateLogin(r.Context(), user, connector.LoginFlowIDQR))
		nextStep, err := login.Start(r.Context())
		if err != nil {
			log.Err(err).Msg("Failed to start login")
			jsonResponse(w, http.StatusInternalServerError, Error{
				Error:   "Failed to start login",
				ErrCode: "unknown",
			})
			return
		}
		ipl = &inProgressLogin{
			proc: login,
			step: nextStep,
			old:  existingLogin,
		}
		logins.Set(user.MXID, ipl)
		jsonResponse(w, http.StatusOK, LoginResponse{Status: "qr", Code: ipl.step.DisplayAndWaitParams.Data})
		return
	}
	if ipl.step.StepID != connector.LoginStepIDQR {
		jsonResponse(w, http.StatusBadRequest, Error{
			Error:   "Non-QR login already in progress",
			ErrCode: "login-in-progress",
		})
		return
	}

	if r.URL.Query().Get("return_immediately") == "true" {
		jsonResponse(w, http.StatusOK, LoginResponse{Status: "qr", Code: ipl.step.DisplayAndWaitParams.Data})
		return
	}

	nextStep, err := ipl.proc.(bridgev2.LoginProcessDisplayAndWait).Wait(r.Context())
	ipl.step = nextStep
	if err != nil {
		logins.Delete(user.MXID)
		if errors.Is(err, connector.ErrPairQRTimeout) {
			jsonResponse(w, http.StatusOK, LoginResponse{Status: "fail", ErrCode: "timeout", Error: "Scanning QR code timed out"})
			return
		} else {
			log.Err(err).Msg("Failed to finish login")
			jsonResponse(w, http.StatusInternalServerError, Error{Error: "Failed to finish login", ErrCode: "unknown"})
			return
		}
	} else if nextStep.StepID == connector.LoginStepIDQR {
		jsonResponse(w, http.StatusOK, LoginResponse{Status: "qr", Code: nextStep.DisplayAndWaitParams.Data})
	} else if nextStep.StepID == connector.LoginStepIDComplete {
		logins.Delete(user.MXID)
		go handleLoginComplete(context.WithoutCancel(r.Context()), user, nextStep.CompleteParams.UserLogin)
		jsonResponse(w, http.StatusOK, LoginResponse{Status: "success"})
	} else {
		logins.Delete(user.MXID)
		log.Warn().Str("step_id", nextStep.StepID).Msg("Unexpected step after waiting for QR login")
		jsonResponse(w, http.StatusInternalServerError, Error{Error: "Unexpected step after waiting for QR login", ErrCode: "unknown"})
	}
}

func handleLoginComplete(ctx context.Context, user *bridgev2.User, newLogin *bridgev2.UserLogin) {
	allLogins := user.GetUserLogins()
	for _, login := range allLogins {
		if login.ID != newLogin.ID {
			login.Delete(ctx, status.BridgeState{StateEvent: status.StateLoggedOut, Reason: "LOGIN_OVERRIDDEN"}, bridgev2.DeleteOpts{})
		}
	}
}
