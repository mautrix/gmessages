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

	"maunium.net/go/mautrix/bridgev2"

	"go.mau.fi/mautrix-gmessages/pkg/connector/gmdb"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/util"
)

type GMConnector struct {
	br     *bridgev2.Bridge
	DB     *gmdb.GMDB
	Config Config
}

var _ bridgev2.NetworkConnector = (*GMConnector)(nil)

func (gc *GMConnector) Init(bridge *bridgev2.Bridge) {
	gc.DB = gmdb.New(bridge.DB.Database, bridge.Log.With().Str("db_section", "gmessages").Logger())
	gc.br = bridge

	util.BrowserDetailsMessage.OS = gc.Config.DeviceMeta.OS
	browserVal, ok := gmproto.BrowserType_value[gc.Config.DeviceMeta.Browser]
	if !ok {
		gc.br.Log.Error().Str("browser_value", gc.Config.DeviceMeta.Browser).Msg("Invalid browser value")
	} else {
		util.BrowserDetailsMessage.BrowserType = gmproto.BrowserType(browserVal)
	}
	deviceVal, ok := gmproto.DeviceType_value[gc.Config.DeviceMeta.Type]
	if !ok {
		gc.br.Log.Error().Str("device_type_value", gc.Config.DeviceMeta.Type).Msg("Invalid device type value")
	} else {
		util.BrowserDetailsMessage.DeviceType = gmproto.DeviceType(deviceVal)
	}
}

func (gc *GMConnector) Start(ctx context.Context) error {
	return gc.DB.Upgrade(ctx)
}

func (gc *GMConnector) GetName() bridgev2.BridgeName {
	return bridgev2.BridgeName{
		DisplayName:          "Google Messages",
		NetworkURL:           "https://messages.google.com",
		NetworkIcon:          "mxc://maunium.net/yGOdcrJcwqARZqdzbfuxfhzb",
		NetworkID:            "gmessages",
		BeeperBridgeType:     "gmessages",
		DefaultPort:          29336,
		DefaultCommandPrefix: "!gm",
	}
}
