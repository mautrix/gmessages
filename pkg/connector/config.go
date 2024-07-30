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
	_ "embed"
	"strings"
	"text/template"

	up "go.mau.fi/util/configupgrade"
	"gopkg.in/yaml.v3"
)

//go:embed example-config.yaml
var ExampleConfig string

type DeviceMetaConfig struct {
	OS      string `yaml:"os"`
	Browser string `yaml:"browser"`
	Type    string `yaml:"type"`
}

type Config struct {
	DisplaynameTemplate  string           `yaml:"displayname_template"`
	DeviceMeta           DeviceMetaConfig `yaml:"device_meta"`
	AggressiveReconnect  bool             `yaml:"aggressive_reconnect"`
	InitialChatSyncCount int              `yaml:"initial_chat_sync_count"`

	displaynameTemplate *template.Template `yaml:"-"`
}

type umConfig Config

func (c *Config) UnmarshalYAML(node *yaml.Node) error {
	err := node.Decode((*umConfig)(c))
	if err != nil {
		return err
	}

	c.displaynameTemplate, err = template.New("displayname").Parse(c.DisplaynameTemplate)
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

func (bc *Config) FormatDisplayname(phone, fullName, firstName string) string {
	var buf strings.Builder
	_ = bc.displaynameTemplate.Execute(&buf, DisplaynameTemplateArgs{
		PhoneNumber: phone,
		FullName:    fullName,
		FirstName:   firstName,
	})
	return buf.String()
}

func (gc *GMConnector) GetConfig() (example string, data any, upgrader up.Upgrader) {
	return ExampleConfig, &gc.Config, up.SimpleUpgrader(upgradeConfig)
}

func upgradeConfig(helper up.Helper) {
	helper.Copy(up.Str, "displayname_template")
	helper.Copy(up.Str, "device_meta", "os")
	helper.Copy(up.Str, "device_meta", "browser")
	helper.Copy(up.Str, "device_meta", "type")
	helper.Copy(up.Bool, "aggressive_reconnect")
	helper.Copy(up.Int, "initial_chat_sync_count")
}
