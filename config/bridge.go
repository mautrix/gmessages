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

package config

import (
	"errors"
	"fmt"
	"strings"
	"text/template"

	"maunium.net/go/mautrix/bridge/bridgeconfig"
)

type BridgeConfig struct {
	UsernameTemplate    string `yaml:"username_template"`
	DisplaynameTemplate string `yaml:"displayname_template"`

	PersonalFilteringSpaces bool `yaml:"personal_filtering_spaces"`

	DeliveryReceipts    bool `yaml:"delivery_receipts"`
	MessageStatusEvents bool `yaml:"message_status_events"`
	MessageErrorNotices bool `yaml:"message_error_notices"`
	PortalMessageBuffer int  `yaml:"portal_message_buffer"`

	SyncDirectChatList   bool `yaml:"sync_direct_chat_list"`
	InitialChatSyncCount int  `yaml:"initial_chat_sync_count"`

	Backfill struct {
		InitialLimit int `yaml:"initial_limit"`
		MissedLimit  int `yaml:"missed_limit"`
	} `yaml:"backfill"`

	DoublePuppetConfig bridgeconfig.DoublePuppetConfig `yaml:",inline"`

	PrivateChatPortalMeta string `yaml:"private_chat_portal_meta"`
	BridgeNotices         bool   `yaml:"bridge_notices"`
	ResendBridgeInfo      bool   `yaml:"resend_bridge_info"`
	MuteBridging          bool   `yaml:"mute_bridging"`
	ArchiveTag            string `yaml:"archive_tag"`
	PinnedTag             string `yaml:"pinned_tag"`
	TagOnlyOnCreate       bool   `yaml:"tag_only_on_create"`
	FederateRooms         bool   `yaml:"federate_rooms"`
	CaptionInMessage      bool   `yaml:"caption_in_message"`
	BeeperGalleries       bool   `yaml:"beeper_galleries"`

	DisableBridgeAlerts bool `yaml:"disable_bridge_alerts"`

	CommandPrefix string `yaml:"command_prefix"`

	ManagementRoomText bridgeconfig.ManagementRoomTexts `yaml:"management_room_text"`

	Encryption bridgeconfig.EncryptionConfig `yaml:"encryption"`

	Provisioning struct {
		Prefix         string `yaml:"prefix"`
		SharedSecret   string `yaml:"shared_secret"`
		DebugEndpoints bool   `yaml:"debug_endpoints"`
	} `yaml:"provisioning"`

	Permissions bridgeconfig.PermissionConfig `yaml:"permissions"`

	ParsedUsernameTemplate *template.Template `yaml:"-"`
	displaynameTemplate    *template.Template `yaml:"-"`
}

func (bc BridgeConfig) GetDoublePuppetConfig() bridgeconfig.DoublePuppetConfig {
	return bc.DoublePuppetConfig
}

func (bc BridgeConfig) GetEncryptionConfig() bridgeconfig.EncryptionConfig {
	return bc.Encryption
}

func (bc BridgeConfig) EnableMessageStatusEvents() bool {
	return bc.MessageStatusEvents
}

func (bc BridgeConfig) EnableMessageErrorNotices() bool {
	return bc.MessageErrorNotices
}

func (bc BridgeConfig) GetCommandPrefix() string {
	return bc.CommandPrefix
}

func (bc BridgeConfig) GetManagementRoomTexts() bridgeconfig.ManagementRoomTexts {
	return bc.ManagementRoomText
}

func (bc BridgeConfig) GetResendBridgeInfo() bool {
	return bc.ResendBridgeInfo
}

func boolToInt(val bool) int {
	if val {
		return 1
	}
	return 0
}

func (bc BridgeConfig) Validate() error {
	_, hasWildcard := bc.Permissions["*"]
	_, hasExampleDomain := bc.Permissions["example.com"]
	_, hasExampleUser := bc.Permissions["@admin:example.com"]
	exampleLen := boolToInt(hasWildcard) + boolToInt(hasExampleUser) + boolToInt(hasExampleDomain)
	if len(bc.Permissions) <= exampleLen {
		return errors.New("bridge.permissions not configured")
	}
	return nil
}

type umBridgeConfig BridgeConfig

func (bc *BridgeConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	err := unmarshal((*umBridgeConfig)(bc))
	if err != nil {
		return err
	}

	bc.ParsedUsernameTemplate, err = template.New("username").Parse(bc.UsernameTemplate)
	if err != nil {
		return err
	} else if !strings.Contains(bc.FormatUsername("1.1234567890"), "1.1234567890") {
		return fmt.Errorf("username template is missing user ID placeholder")
	}

	bc.displaynameTemplate, err = template.New("displayname").Parse(bc.DisplaynameTemplate)
	if err != nil {
		return err
	}

	return nil
}

type DisplaynameTemplateArgs struct {
	PhoneNumber string
	FullName    string
	FirstName   string
}

func (bc BridgeConfig) FormatDisplayname(phone, fullName, firstName string) string {
	var buf strings.Builder
	_ = bc.displaynameTemplate.Execute(&buf, DisplaynameTemplateArgs{
		PhoneNumber: phone,
		FullName:    fullName,
		FirstName:   firstName,
	})
	return buf.String()
}

func (bc BridgeConfig) FormatUsername(username string) string {
	var buf strings.Builder
	_ = bc.ParsedUsernameTemplate.Execute(&buf, username)
	return buf.String()
}
