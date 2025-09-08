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
	"time"

	"go.mau.fi/util/ffmpeg"
	"go.mau.fi/util/ptr"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/event"

	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

var generalCaps = &bridgev2.NetworkGeneralCapabilities{
	DisappearingMessages: false,
	AggressiveUpdateInfo: false,
	OutgoingMessageTimeouts: &bridgev2.OutgoingTimeoutConfig{
		NoEchoTimeout: 1 * time.Minute,
		NoEchoMessage: "phone has not confirmed message delivery",
		NoAckTimeout:  3 * time.Minute,
		NoAckMessage:  "phone is not responding",
		CheckInterval: 1 * time.Minute,
	},
	Provisioning: bridgev2.ProvisioningCapabilities{
		ResolveIdentifier: bridgev2.ResolveIdentifierCapabilities{
			CreateDM:    true,
			LookupPhone: false,
			AnyPhone:    true,
			ContactList: false, // we don't support pagination yet
		},
		GroupCreation: map[string]bridgev2.GroupTypeCapabilities{
			// TODO allow choosing rcs or mms?
			"group": {
				TypeDescription: "mms/rcs group",
				Name:            bridgev2.GroupFieldCapability{Allowed: true},
				Participants:    bridgev2.GroupFieldCapability{Allowed: true, Required: true, MinLength: 2},
			},
		},
	},
}

func (gc *GMConnector) GetCapabilities() *bridgev2.NetworkGeneralCapabilities {
	return generalCaps
}

func (gc *GMConnector) GetBridgeInfoVersion() (info, caps int) {
	return 1, 2
}

const MaxFileSizeRCS = 100 * 1024 * 1024
const MaxFileSizeMMS = 1 * 1024 * 1024

func supportedIfFFmpeg() event.CapabilitySupportLevel {
	if ffmpeg.Supported() {
		return event.CapLevelPartialSupport
	}
	return event.CapLevelRejected
}

func capID(chatType string) string {
	base := "fi.mau.gmessages.capabilities.2025_01_10." + chatType
	if ffmpeg.Supported() {
		return base + "+ffmpeg"
	}
	return base
}

var imageMimes = map[string]event.CapabilitySupportLevel{
	"image/png":  event.CapLevelFullySupported,
	"image/jpeg": event.CapLevelFullySupported,
	"image/gif":  event.CapLevelFullySupported,
	"image/bmp":  event.CapLevelFullySupported,
	"image/wbmp": event.CapLevelFullySupported,
	"image/webp": event.CapLevelFullySupported,
}

var audioMimes = map[string]event.CapabilitySupportLevel{
	"audio/aac":      event.CapLevelFullySupported,
	"audio/amr":      event.CapLevelFullySupported,
	"audio/mpeg":     event.CapLevelFullySupported,
	"audio/mp4":      event.CapLevelFullySupported,
	"audio/mp4-latm": event.CapLevelFullySupported,
	"audio/3gpp":     event.CapLevelFullySupported,
	"audio/ogg":      event.CapLevelFullySupported,
}

var videoMimes = map[string]event.CapabilitySupportLevel{
	"video/mp4":  event.CapLevelFullySupported,
	"video/3gpp": event.CapLevelFullySupported,
	"video/webm": event.CapLevelFullySupported,
}

var fileMimes = map[string]event.CapabilitySupportLevel{
	"application/*": event.CapLevelFullySupported,
	"text/*":        event.CapLevelFullySupported,
}

var voiceMimes = map[string]event.CapabilitySupportLevel{
	"audio/ogg": supportedIfFFmpeg(),
	"audio/mp4": event.CapLevelFullySupported,
}

var gifMimes = map[string]event.CapabilitySupportLevel{
	"image/gif": event.CapLevelFullySupported,
}

var rcsDMCaps = &event.RoomFeatures{
	ID: capID("rcs_dm+1"),
	File: event.FileFeatureMap{
		event.MsgImage: {
			MimeTypes: imageMimes,
			MaxSize:   MaxFileSizeRCS,
		},
		event.MsgAudio: {
			MimeTypes: audioMimes,
			MaxSize:   MaxFileSizeRCS,
		},
		event.MsgVideo: {
			MimeTypes: videoMimes,
			MaxSize:   MaxFileSizeRCS,
		},
		event.MsgFile: {
			MimeTypes: fileMimes,
			MaxSize:   MaxFileSizeRCS,
		},
		event.CapMsgVoice: {
			MimeTypes: voiceMimes,
			MaxSize:   MaxFileSizeRCS,
		},
		event.CapMsgGIF: {
			MimeTypes: gifMimes,
			MaxSize:   MaxFileSizeRCS,
		},
	},
	Reply:               event.CapLevelFullySupported,
	DeleteForMe:         true,
	Reaction:            event.CapLevelFullySupported,
	ReactionCount:       1,
	ReadReceipts:        true,
	TypingNotifications: true,
}

var rcsGroupCaps *event.RoomFeatures

func init() {
	rcsGroupCaps = ptr.Clone(rcsDMCaps)
	rcsGroupCaps.ID = capID("rcs_group+1")
}

var smsRoomCaps = &event.RoomFeatures{
	ID: capID("sms"),
	File: event.FileFeatureMap{
		event.MsgImage: {
			MimeTypes: imageMimes,
			Caption:   event.CapLevelFullySupported,
			MaxSize:   MaxFileSizeMMS,
		},
		event.MsgAudio: {
			MimeTypes: audioMimes,
			Caption:   event.CapLevelFullySupported,
			MaxSize:   MaxFileSizeMMS,
		},
		event.MsgVideo: {
			MimeTypes: videoMimes,
			Caption:   event.CapLevelFullySupported,
			MaxSize:   MaxFileSizeMMS,
		},
		event.MsgFile: {
			MimeTypes: fileMimes,
			Caption:   event.CapLevelFullySupported,
			MaxSize:   MaxFileSizeMMS,
		},
		event.CapMsgVoice: {
			MimeTypes: voiceMimes,
			MaxSize:   MaxFileSizeRCS,
		},
		event.CapMsgGIF: {
			MimeTypes: gifMimes,
			Caption:   event.CapLevelFullySupported,
			MaxSize:   MaxFileSizeMMS,
		},
	},
	DeleteForMe:   true,
	Reaction:      event.CapLevelPartialSupport,
	ReactionCount: 1,
	ReadReceipts:  true,
}

func (gc *GMClient) GetCapabilities(ctx context.Context, portal *bridgev2.Portal) *event.RoomFeatures {
	if portal.Metadata.(*PortalMetadata).Type == gmproto.ConversationType_RCS {
		if portal.RoomType == database.RoomTypeDM {
			return rcsDMCaps
		} else {
			return rcsGroupCaps
		}
	} else {
		return smsRoomCaps
	}
}
