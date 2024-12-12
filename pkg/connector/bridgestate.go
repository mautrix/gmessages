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
	"maunium.net/go/mautrix/bridge/status"
)

const (
	GMListenError               status.BridgeStateErrorCode = "gm-listen-error"
	GMFatalError                status.BridgeStateErrorCode = "gm-listen-fatal-error"
	GMUnpaired                  status.BridgeStateErrorCode = "gm-unpaired"
	GMUnpairedGaia              status.BridgeStateErrorCode = "gm-unpaired-gaia"
	GMUnpaired404               status.BridgeStateErrorCode = "gm-unpaired-entity-not-found"
	GMUnpaired401               status.BridgeStateErrorCode = "gm-unpaired-401-polling"
	GMUnpairedInvalidCreds      status.BridgeStateErrorCode = "gm-unpaired-invalid-credentials"
	GMUnpairedNoEmailInConfig   status.BridgeStateErrorCode = "gm-unpaired-no-email-in-config"
	GMNotLoggedIn               status.BridgeStateErrorCode = "gm-not-logged-in"
	GMNotConnected              status.BridgeStateErrorCode = "gm-not-connected"
	GMConnecting                status.BridgeStateErrorCode = "gm-connecting"
	GMConnectionFailed          status.BridgeStateErrorCode = "gm-connection-failed"
	GMConfigFetchFailed         status.BridgeStateErrorCode = "gm-config-fetch-failed"
	GMPingFailed                status.BridgeStateErrorCode = "gm-ping-failed"
	GMNotDefaultSMSApp          status.BridgeStateErrorCode = "gm-not-default-sms-app"
	GMBrowserInactive           status.BridgeStateErrorCode = "gm-browser-inactive"
	GMBrowserInactiveTimeout    status.BridgeStateErrorCode = "gm-browser-inactive-timeout"
	GMBrowserInactiveInactivity status.BridgeStateErrorCode = "gm-browser-inactive-inactivity"
	GMPhoneNotResponding        status.BridgeStateErrorCode = "gm-phone-not-responding"
	GMSwitchedToGoogleLogin     status.BridgeStateErrorCode = "gm-switched-to-google-login"
)

func init() {
	status.BridgeStateHumanErrors.Update(status.BridgeStateErrorMap{
		GMListenError:               "Error polling messages from Google Messages server, the bridge will try to reconnect",
		GMFatalError:                "Fatal error polling messages from Google Messages server",
		GMConnectionFailed:          "Failed to connect to Google Messages",
		GMNotLoggedIn:               "Unpaired from Google Messages, please re-link the connection to continue using SMS/RCS",
		GMUnpaired:                  "Unpaired from Google Messages, please re-link the connection to continue using SMS/RCS",
		GMUnpaired404:               "Unpaired from Google Messages, please re-link the connection to continue using SMS/RCS",
		GMUnpaired401:               "Unpaired from Google Messages, please re-link the connection to continue using SMS/RCS",
		GMUnpairedInvalidCreds:      "Unpaired from Google Messages, please re-link the connection to continue using SMS/RCS",
		GMUnpairedNoEmailInConfig:   "Unpaired from Google Messages, please re-link the connection to continue using SMS/RCS",
		GMUnpairedGaia:              "Unpaired from Google Messages, please re-link the connection to continue using SMS/RCS",
		GMNotDefaultSMSApp:          "Google Messages isn't set as the default SMS app. Please set the default SMS app on your Android phone to Google Messages to continue using SMS/RCS.",
		GMBrowserInactive:           "Google Messages opened in another browser",
		GMBrowserInactiveTimeout:    "Google Messages disconnected due to timeout",
		GMBrowserInactiveInactivity: "Google Messages disconnected due to inactivity",
		GMPhoneNotResponding:        "Your Google Messages app is not responding. You may need to open the Messages app on your phone and/or disable battery optimizations for it to reconnect.",
		GMSwitchedToGoogleLogin:     "You switched to Google account pairing, please log in to continue using SMS/RCS",
	})
}

func (gc *GMClient) FillBridgeState(state status.BridgeState) status.BridgeState {
	if state.Info == nil {
		state.Info = make(map[string]any)
	}
	if state.StateEvent == status.StateConnected {
		state.Info["sims"] = gc.Meta.GetSIMsForBridgeState()
		state.Info["settings"] = gc.Meta.Settings
		state.Info["battery_low"] = gc.batteryLow
		state.Info["mobile_data"] = gc.mobileData
		state.Info["browser_active"] = gc.browserInactiveType == ""
		state.Info["google_account_pairing"] = gc.SwitchedToGoogleLogin
		if !gc.ready {
			state.StateEvent = status.StateConnecting
			state.Error = GMConnecting
		}
		if !gc.PhoneResponding {
			state.StateEvent = status.StateBadCredentials
			state.Error = GMPhoneNotResponding
		}
		if gc.SwitchedToGoogleLogin {
			state.StateEvent = status.StateBadCredentials
			state.Error = GMSwitchedToGoogleLogin
		}
		if gc.longPollingError != nil {
			state.StateEvent = status.StateTransientDisconnect
			state.Error = GMListenError
			state.Info["go_error"] = gc.longPollingError.Error()
		}
		if gc.browserInactiveType != "" {
			if gc.Main.Config.AggressiveReconnect {
				state.StateEvent = status.StateTransientDisconnect
			} else {
				state.StateEvent = status.StateBadCredentials
			}
			state.Error = gc.browserInactiveType
		}
	}
	return state
}
