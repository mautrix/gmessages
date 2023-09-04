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
	_ "embed"
	"sync"

	"go.mau.fi/util/configupgrade"
	"maunium.net/go/mautrix/bridge"
	"maunium.net/go/mautrix/bridge/commands"
	"maunium.net/go/mautrix/bridge/status"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-gmessages/config"
	"go.mau.fi/mautrix-gmessages/database"
	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

// Information to find out exactly which commit the bridge was built from.
// These are filled at build time with the -X linker flag.
var (
	Tag       = "unknown"
	Commit    = "unknown"
	BuildTime = "unknown"
)

//go:embed example-config.yaml
var ExampleConfig string

type GMBridge struct {
	bridge.Bridge
	Config       *config.Config
	DB           *database.Database
	Provisioning *ProvisioningAPI

	usersByMXID         map[id.UserID]*User
	usersLock           sync.Mutex
	spaceRooms          map[id.RoomID]*User
	spaceRoomsLock      sync.Mutex
	managementRooms     map[id.RoomID]*User
	managementRoomsLock sync.Mutex
	portalsByMXID       map[id.RoomID]*Portal
	portalsByKey        map[database.Key]*Portal
	portalsByOtherUser  map[database.Key]*Portal
	portalsLock         sync.Mutex
	puppetsByKey        map[database.Key]*Puppet
	puppetsLock         sync.Mutex
}

func (br *GMBridge) Init() {
	br.CommandProcessor = commands.NewProcessor(&br.Bridge)
	br.RegisterCommands()

	util.BrowserDetailsMessage.OS = br.Config.GoogleMessages.OS
	browserVal, ok := gmproto.BrowserType_value[br.Config.GoogleMessages.Browser]
	if !ok {
		br.ZLog.Error().Str("browser_value", br.Config.GoogleMessages.Browser).Msg("Invalid browser value")
	} else {
		util.BrowserDetailsMessage.BrowserType = gmproto.BrowserType(browserVal)
	}
	deviceVal, ok := gmproto.DeviceType_value[br.Config.GoogleMessages.Device]
	if !ok {
		br.ZLog.Error().Str("device_value", br.Config.GoogleMessages.Device).Msg("Invalid device value")
	} else {
		util.BrowserDetailsMessage.DeviceType = gmproto.DeviceType(deviceVal)
	}

	Segment.log = br.ZLog.With().Str("component", "segment").Logger()
	Segment.key = br.Config.SegmentKey
	Segment.userID = br.Config.SegmentUserID
	if Segment.IsEnabled() {
		Segment.log.Info().Msg("Segment metrics are enabled")
		if Segment.userID != "" {
			Segment.log.Info().Str("user_id", Segment.userID).Msg("Overriding Segment user ID")
		}
	}

	br.DB = database.New(br.Bridge.DB)

	ss := br.Config.Bridge.Provisioning.SharedSecret
	if len(ss) > 0 && ss != "disable" {
		br.Provisioning = &ProvisioningAPI{bridge: br}
	}
}

func (br *GMBridge) Start() {
	if br.Provisioning != nil {
		br.ZLog.Debug().Msg("Initializing provisioning API")
		br.Provisioning.Init()
	}
	br.WaitWebsocketConnected()
	go br.StartUsers()
}

func (br *GMBridge) StartUsers() {
	br.ZLog.Debug().Msg("Starting users")
	foundAnySessions := false
	for _, user := range br.GetAllUsersWithSession() {
		foundAnySessions = true
		go user.Connect()
	}
	if !foundAnySessions {
		br.SendGlobalBridgeState(status.BridgeState{StateEvent: status.StateUnconfigured}.Fill(nil))
	}
	br.ZLog.Debug().Msg("Starting custom puppets")
	for _, loopuser := range br.GetAllUsersWithDoublePuppet() {
		go func(user *User) {
			user.zlog.Debug().Msg("Starting double puppet")
			err := user.StartCustomMXID(true)
			if err != nil {
				user.zlog.Err(err).Msg("Failed to start double puppet")
			}
		}(loopuser)
	}
}

func (br *GMBridge) Stop() {
	for _, user := range br.usersByMXID {
		if user.Client == nil {
			continue
		}
		br.ZLog.Debug().Str("user_id", user.MXID.String()).Msg("Disconnecting user")
		user.Client.Disconnect()
	}
}

func (br *GMBridge) GetExampleConfig() string {
	return ExampleConfig
}

func (br *GMBridge) GetConfigPtr() interface{} {
	br.Config = &config.Config{
		BaseConfig: &br.Bridge.Config,
	}
	br.Config.BaseConfig.Bridge = &br.Config.Bridge
	return br.Config
}

func main() {
	br := &GMBridge{
		usersByMXID:        make(map[id.UserID]*User),
		spaceRooms:         make(map[id.RoomID]*User),
		managementRooms:    make(map[id.RoomID]*User),
		portalsByMXID:      make(map[id.RoomID]*Portal),
		portalsByKey:       make(map[database.Key]*Portal),
		portalsByOtherUser: make(map[database.Key]*Portal),
		puppetsByKey:       make(map[database.Key]*Puppet),
	}
	br.Bridge = bridge.Bridge{
		Name:              "mautrix-gmessages",
		URL:               "https://github.com/mautrix/gmessages",
		Description:       "A Matrix-Google Messages puppeting bridge.",
		Version:           "0.1.0",
		ProtocolName:      "Google Messages",
		BeeperServiceName: "gmessages",
		BeeperNetworkName: "gmessages",

		CryptoPickleKey: "go.mau.fi/mautrix-gmessages",

		ConfigUpgrader: &configupgrade.StructUpgrader{
			SimpleUpgrader: configupgrade.SimpleUpgrader(config.DoUpgrade),
			Blocks:         config.SpacedBlocks,
			Base:           ExampleConfig,
		},

		Child: br,
	}
	br.InitVersion(Tag, Commit, BuildTime)

	br.Main()
}

func (br *GMBridge) CreatePrivatePortal(roomID id.RoomID, brUser bridge.User, brGhost bridge.Ghost) {
	//TODO implement?
}
