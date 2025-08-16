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
	"maunium.net/go/mautrix/bridgev2/bridgeconfig"
	"maunium.net/go/mautrix/bridgev2/matrix/mxmain"

	"go.mau.fi/mautrix-gmessages/pkg/connector"
)

var (
	Tag       = "unknown"
	Commit    = "unknown"
	BuildTime = "unknown"
)

var c = &connector.GMConnector{}
var m = mxmain.BridgeMain{
	Name:        "mautrix-gmessages",
	Description: "A Matrix-Google Messages puppeting bridge",
	URL:         "https://github.com/mautrix/gmessages",
	Version:     "0.6.5",
	Connector:   c,
}

func main() {
	bridgeconfig.HackyMigrateLegacyNetworkConfig = migrateLegacyConfig
	m.PostInit = func() {
		m.CheckLegacyDB(
			10,
			"v0.4.3",
			"v0.5.0",
			m.LegacyMigrateSimple(legacyMigrateRenameTables, legacyMigrateCopyData, 14),
			true,
		)
	}
	m.PostStart = func() {
		if m.Matrix.Provisioning != nil {
			m.Matrix.Provisioning.Router.HandleFunc("GET /v1/ping", legacyProvPing)
			m.Matrix.Provisioning.Router.HandleFunc("POST /v1/login", legacyProvQRLogin)
			m.Matrix.Provisioning.Router.HandleFunc("POST /v1/google_login/emoji", legacyProvGoogleLoginStart)
			m.Matrix.Provisioning.Router.HandleFunc("POST /v1/google_login/wait", legacyProvGoogleLoginWait)
			m.Matrix.Provisioning.Router.HandleFunc("POST /v1/logout", legacyProvLogout)
			m.Matrix.Provisioning.Router.HandleFunc("POST /v1/delete_session", legacyProvDeleteSession)
			m.Matrix.Provisioning.Router.HandleFunc("POST /v1/contacts", legacyProvListContacts)
			m.Matrix.Provisioning.Router.HandleFunc("POST /v1/start_chat", legacyProvStartChat)
		}
	}
	m.InitVersion(Tag, Commit, BuildTime)
	m.Run()
}
