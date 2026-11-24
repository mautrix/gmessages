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
	"go.mau.fi/util/l10n"
)

var L = l10n.NewNamespace("mautrix-gmessages")

var (
	msgFlowGoogleName           = L.Define("login.flow.google.name", "Google Account")
	msgFlowGoogleDescription    = L.Define("login.flow.google.description", "Log in with your Google account and pair by tapping an emoji on your phone")
	msgStepQRInstructions       = L.Define("login.step.qr.instructions", "Scan the QR code using the Google Messages app on your Android phone")
	msgStepCookiesInstructions  = L.Define("login.step.cookies.instructions", "Enter a JSON object with your cookies, or a cURL command copied from browser devtools.")
	msgStepEmojiInstructions    = L.Define("login.step.emoji.instructions", "Tap the emoji on the Google Messages app on your phone")
	msgStepReauthInstructions   = L.Define("login.step.reauth.instructions", "Successfully re-authenticated")
	msgStepCompleteInstructions = L.Define("login.step.complete.instructions", "Successfully logged in")
)
