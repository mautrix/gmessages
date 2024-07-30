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
	"net/http"

	"maunium.net/go/mautrix/bridge/status"
	"maunium.net/go/mautrix/id"
)

const (
	GMListenError      status.BridgeStateErrorCode = "gm-listen-error"
	GMFatalError       status.BridgeStateErrorCode = "gm-listen-fatal-error"
	GMUnpaired         status.BridgeStateErrorCode = "gm-unpaired"
	GMUnpairedGaia     status.BridgeStateErrorCode = "gm-unpaired-gaia"
	GMUnpaired404      status.BridgeStateErrorCode = "gm-unpaired-entity-not-found"
	GMNotConnected     status.BridgeStateErrorCode = "gm-not-connected"
	GMConnecting       status.BridgeStateErrorCode = "gm-connecting"
	GMConnectionFailed status.BridgeStateErrorCode = "gm-connection-failed"
	GMPingFailed       status.BridgeStateErrorCode = "gm-ping-failed"
	GMNotDefaultSMSApp status.BridgeStateErrorCode = "gm-not-default-sms-app"

	GMBrowserInactive           status.BridgeStateErrorCode = "gm-browser-inactive"
	GMBrowserInactiveTimeout    status.BridgeStateErrorCode = "gm-browser-inactive-timeout"
	GMBrowserInactiveInactivity status.BridgeStateErrorCode = "gm-browser-inactive-inactivity"

	GMPhoneNotResponding status.BridgeStateErrorCode = "gm-phone-not-responding"

	GMSwitchedToGoogleLogin status.BridgeStateErrorCode = "gm-switched-to-google-login"
)

func init() {
	status.BridgeStateHumanErrors.Update(status.BridgeStateErrorMap{
		GMListenError:               "Error polling messages from Google Messages server, the bridge will try to reconnect",
		GMFatalError:                "Fatal error polling messages from Google Messages server, please re-link the bridge",
		GMUnpaired:                  "Unpaired from Google Messages, please re-link the connection to continue using SMS/RCS",
		GMUnpaired404:               "Unpaired from Google Messages, please re-link the connection to continue using SMS/RCS",
		GMUnpairedGaia:              "Unpaired from Google Messages, please re-link the connection to continue using SMS/RCS",
		GMNotDefaultSMSApp:          "Google Messages isn't set as the default SMS app. Please set the default SMS app on your Android phone to Google Messages to continue using SMS/RCS.",
		GMBrowserInactive:           "Google Messages opened in another browser",
		GMBrowserInactiveTimeout:    "Google Messages disconnected due to timeout",
		GMBrowserInactiveInactivity: "Google Messages disconnected due to inactivity",
		GMPhoneNotResponding:        "Your Google Messages app is not responding. You may need to open the Messages app on your phone and/or disable battery optimizations for it to reconnect.",
		GMSwitchedToGoogleLogin:     "You switched to Google account pairing, please log in to continue using SMS/RCS",
	})
}

func (user *User) GetRemoteID() string {
	if user == nil {
		return ""
	}
	return user.PhoneID
}

func (user *User) GetRemoteName() string {
	return ""
}

func (prov *ProvisioningAPI) BridgeStatePing(w http.ResponseWriter, r *http.Request) {
	if !prov.bridge.AS.CheckServerToken(w, r) {
		return
	}
	userID := r.URL.Query().Get("user_id")
	user := prov.bridge.GetUserByMXID(id.UserID(userID))
	var global status.BridgeState
	global.StateEvent = status.StateRunning
	var remote status.BridgeState
	if user.IsConnected() {
		if user.Client.IsLoggedIn() {
			remote.StateEvent = status.StateConnected
		} else if user.Session != nil {
			remote.StateEvent = status.StateConnecting
			//remote.Error = WAConnecting
		} // else: unconfigured
	} else if user.Session != nil {
		remote.StateEvent = status.StateBadCredentials
		//remote.Error = WANotConnected
	} // else: unconfigured
	global = global.Fill(nil)
	resp := status.GlobalBridgeState{
		BridgeState:  global,
		RemoteStates: map[string]status.BridgeState{},
	}
	if len(remote.StateEvent) > 0 {
		remote = remote.Fill(user)
		resp.RemoteStates[remote.RemoteID] = remote
	}
	user.zlog.Debug().Any("bridge_state", &resp).Msg("Responding bridge state in bridge status endpoint")
	jsonResponse(w, http.StatusOK, &resp)
	if len(resp.RemoteStates) > 0 {
		user.BridgeState.SetPrev(remote)
	}
}
