package events

import "go.mau.fi/mautrix-gmessages/libgm/binary"

type SettingEvent interface {
	GetSettings() *binary.Settings
}

type SETTINGS_UPDATED struct {
	Settings *binary.Settings
}

func (su *SETTINGS_UPDATED) GetSettings() *binary.Settings {
	return su.Settings
}

func NewSettingsUpdated(settings *binary.Settings) SettingEvent {
	return &SETTINGS_UPDATED{
		Settings: settings,
	}
}
