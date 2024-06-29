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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/skip2/go-qrcode"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/bridge/commands"
	"maunium.net/go/mautrix/bridge/status"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-gmessages/libgm"
	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
)

type WrappedCommandEvent struct {
	*commands.Event
	Bridge *GMBridge
	User   *User
	Portal *Portal
}

func (br *GMBridge) RegisterCommands() {
	proc := br.CommandProcessor.(*commands.Processor)
	proc.AddHandlers(
		cmdLoginQR,
		cmdLoginGoogle,
		cmdDeleteSession,
		cmdLogout,
		cmdReconnect,
		cmdDisconnect,
		cmdSetActive,
		cmdPing,
		cmdToggleBatteryNotifications,
		cmdToggleVerboseNotifications,
		cmdPM,
		cmdDeletePortal,
		cmdDeleteAllPortals,
	)
}

func wrapCommand(handler func(*WrappedCommandEvent)) func(*commands.Event) {
	return func(ce *commands.Event) {
		user := ce.User.(*User)
		var portal *Portal
		if ce.Portal != nil {
			portal = ce.Portal.(*Portal)
		}
		br := ce.Bridge.Child.(*GMBridge)
		handler(&WrappedCommandEvent{ce, br, user, portal})
	}
}

var (
	HelpSectionConnectionManagement = commands.HelpSection{Name: "Connection management", Order: 11}
	HelpSectionPortalManagement     = commands.HelpSection{Name: "Portal management", Order: 20}
)

var cmdLoginQR = &commands.FullHandler{
	Func:    wrapCommand(fnLoginQR),
	Name:    "login-qr",
	Aliases: []string{"login"},
	Help: commands.HelpMeta{
		Section:     commands.HelpSectionAuth,
		Description: "Link the bridge to Google Messages on your Android phone by scanning a QR code.",
	},
}

func fnLoginQR(ce *WrappedCommandEvent) {
	if ce.User.Session != nil {
		if ce.User.IsConnected() {
			ce.Reply("You're already logged in")
		} else {
			ce.Reply("You're already logged in. Perhaps you wanted to `reconnect`?")
		}
		return
	} else if ce.User.pairSuccessChan != nil {
		ce.Reply("You already have a login in progress")
		return
	}

	ch, err := ce.User.Login(6)
	if err != nil {
		ce.ZLog.Err(err).Msg("Failed to start login")
		ce.Reply("Failed to start login: %v", err)
		return
	}
	var prevEvent id.EventID
	for item := range ch {
		switch {
		case item.qr != "":
			ce.ZLog.Debug().Msg("Got code in QR channel")
			prevEvent = ce.User.sendQR(ce, item.qr, prevEvent)
		case item.err != nil:
			ce.ZLog.Err(err).Msg("Error in QR channel")
			prevEvent = ce.User.sendQREdit(ce, &event.MessageEventContent{
				MsgType: event.MsgNotice,
				Body:    fmt.Sprintf("Failed to log in: %v", err),
			}, prevEvent)
		case item.success:
			ce.ZLog.Debug().Msg("Got pair success in QR channel")
			prevEvent = ce.User.sendQREdit(ce, &event.MessageEventContent{
				MsgType: event.MsgNotice,
				Body:    "Successfully logged in",
			}, prevEvent)
		default:
			ce.ZLog.Error().Any("item_data", item).Msg("Unknown item in QR channel")
		}
	}
	ce.ZLog.Trace().Msg("Login command finished")
}

var cmdLoginGoogle = &commands.FullHandler{
	Func:    wrapCommand(fnLoginGoogle),
	Name:    "login-google",
	Aliases: []string{"login"},
	Help: commands.HelpMeta{
		Section:     commands.HelpSectionAuth,
		Description: "Link the bridge to Google Messages on your Android phone by logging in with your Google account.",
	},
}

func fnLoginGoogle(ce *WrappedCommandEvent) {
	if ce.User.Session != nil {
		if ce.User.IsConnected() {
			ce.Reply("You're already logged in")
		} else {
			ce.Reply("You're already logged in. Perhaps you wanted to `reconnect`?")
		}
		return
	} else if ce.User.pairSuccessChan != nil {
		ce.Reply("You already have a login in progress")
		return
	}
	ce.User.CommandState = &commands.CommandState{
		Next:   commands.MinimalHandlerFunc(wrapCommand(fnLoginGoogleCookies)),
		Action: "Login",
	}
	ce.Reply("Send your Google cookies here, formatted as a key-value JSON object (see <https://docs.mau.fi/bridges/go/gmessages/authentication.html> for details)")
}

const (
	pairingErrMsgNoDevices       = "No devices found. Make sure you've enabled account pairing in the Google Messages app on your phone."
	pairingErrPhoneNotResponding = "Phone not responding. Make sure your phone is connected to the internet and that account pairing is enabled in the Google Messages app. You may need to keep the app open and/or disable battery optimizations. Alternatively, try QR pairing"
	pairingErrMsgIncorrectEmoji  = "Incorrect emoji chosen on phone, please try again"
	pairingErrMsgCancelled       = "Pairing cancelled on phone"
	pairingErrMsgTimeout         = "Pairing timed out, please try again"
)

func fnLoginGoogleCookies(ce *WrappedCommandEvent) {
	ce.User.CommandState = nil
	if ce.User.Session != nil {
		if ce.User.IsConnected() {
			ce.Reply("You're already logged in")
		} else {
			ce.Reply("You're already logged in. Perhaps you wanted to `reconnect`?")
		}
		return
	} else if ce.User.pairSuccessChan != nil {
		ce.Reply("You already have a login in progress")
		return
	}
	var cookies map[string]string
	err := json.Unmarshal([]byte(ce.RawArgs), &cookies)
	if err != nil {
		ce.Reply("Failed to parse cookies: %v", err)
		return
	} else if missingCookie := findMissingCookies(cookies); missingCookie != "" {
		ce.Reply("Missing %s cookie", missingCookie)
		return
	}
	ce.Redact()
	err = ce.User.LoginGoogle(ce.Ctx, cookies, func(emoji string) {
		ce.Reply(emoji)
	})
	if err != nil {
		if errors.Is(err, libgm.ErrNoDevicesFound) {
			ce.Reply(pairingErrMsgNoDevices)
		} else if errors.Is(err, libgm.ErrPairingInitTimeout) {
			ce.Reply(pairingErrPhoneNotResponding)
		} else if errors.Is(err, libgm.ErrIncorrectEmoji) {
			ce.Reply(pairingErrMsgIncorrectEmoji)
		} else if errors.Is(err, libgm.ErrPairingCancelled) {
			ce.Reply(pairingErrMsgCancelled)
		} else if errors.Is(err, libgm.ErrPairingTimeout) {
			ce.Reply(pairingErrMsgTimeout)
		} else {
			ce.Reply("Login failed: %v", err)
		}
	} else {
		ce.Reply("Login successful")
	}
}

func (user *User) sendQREdit(ce *WrappedCommandEvent, content *event.MessageEventContent, prevEvent id.EventID) id.EventID {
	if len(prevEvent) != 0 {
		content.SetEdit(prevEvent)
	}
	resp, err := ce.Bot.SendMessageEvent(ce.Ctx, ce.RoomID, event.EventMessage, &content)
	if err != nil {
		ce.ZLog.Err(err).Msg("Failed to send edited QR code")
	} else if len(prevEvent) == 0 {
		prevEvent = resp.EventID
	}
	return prevEvent
}

func (user *User) sendQR(ce *WrappedCommandEvent, code string, prevEvent id.EventID) id.EventID {
	var content event.MessageEventContent
	url, err := user.uploadQR(ce.Ctx, code)
	if err != nil {
		ce.ZLog.Err(err).Msg("Failed to upload QR code")
		content = event.MessageEventContent{
			MsgType: event.MsgNotice,
			Body:    fmt.Sprintf("Failed to upload QR code: %v", err),
		}
	} else {
		content = event.MessageEventContent{
			MsgType: event.MsgImage,
			Body:    code,
			URL:     url.CUString(),
		}
	}
	return user.sendQREdit(ce, &content, prevEvent)
}

func (user *User) uploadQR(ctx context.Context, code string) (id.ContentURI, error) {
	qrCode, err := qrcode.Encode(code, qrcode.Low, 256)
	if err != nil {
		return id.ContentURI{}, err
	}
	resp, err := user.bridge.Bot.UploadBytes(ctx, qrCode, "image/png")
	if err != nil {
		return id.ContentURI{}, err
	}
	return resp.ContentURI, nil
}

var cmdLogout = &commands.FullHandler{
	Func: wrapCommand(fnLogout),
	Name: "logout",
	Help: commands.HelpMeta{
		Section:     commands.HelpSectionAuth,
		Description: "Unpair the bridge and delete session information.",
	},
}

func fnLogout(ce *WrappedCommandEvent) {
	if ce.User.Session == nil && ce.User.Client == nil {
		ce.Reply("You're not logged in")
		return
	}
	logoutOK := ce.User.Logout(status.BridgeState{StateEvent: status.StateLoggedOut}, true)
	if logoutOK {
		ce.Reply("Successfully logged out")
	} else {
		ce.Reply("Session information removed, but unpairing may not have succeeded")
	}
}

var cmdDeleteSession = &commands.FullHandler{
	Func: wrapCommand(fnDeleteSession),
	Name: "delete-session",
	Help: commands.HelpMeta{
		Section:     commands.HelpSectionAuth,
		Description: "Delete session information and disconnect from Google Messages without sending a logout request.",
	},
}

func fnDeleteSession(ce *WrappedCommandEvent) {
	if ce.User.Session == nil && ce.User.Client == nil {
		ce.Reply("Nothing to purge: no session information stored and no active connection.")
		return
	}
	ce.User.Logout(status.BridgeState{StateEvent: status.StateLoggedOut}, false)
	ce.Reply("Session information purged")
}

var cmdReconnect = &commands.FullHandler{
	Func: wrapCommand(fnReconnect),
	Name: "reconnect",
	Help: commands.HelpMeta{
		Section:     HelpSectionConnectionManagement,
		Description: "Reconnect to Google Messages.",
	},
}

func fnReconnect(ce *WrappedCommandEvent) {
	if ce.User.Client == nil {
		if ce.User.Session == nil {
			ce.Reply("You're not logged into Google Messages. Please log in first.")
		} else {
			ce.User.Connect()
			ce.Reply("Started connecting to Google Messages")
		}
	} else {
		ce.User.DeleteConnection()
		ce.User.BridgeState.Send(status.BridgeState{StateEvent: status.StateTransientDisconnect, Error: GMNotConnected})
		ce.User.Connect()
		ce.Reply("Restarted connection to Google Messages")
	}
}

var cmdDisconnect = &commands.FullHandler{
	Func: wrapCommand(fnDisconnect),
	Name: "disconnect",
	Help: commands.HelpMeta{
		Section:     HelpSectionConnectionManagement,
		Description: "Disconnect from Google Messages (without logging out).",
	},
}

func fnDisconnect(ce *WrappedCommandEvent) {
	if ce.User.Client == nil {
		ce.Reply("You don't have a Google Messages connection.")
		return
	}
	ce.User.DeleteConnection()
	ce.Reply("Successfully disconnected. Use the `reconnect` command to reconnect.")
	ce.User.BridgeState.Send(status.BridgeState{StateEvent: status.StateBadCredentials, Error: GMNotConnected})
}

var cmdSetActive = &commands.FullHandler{
	Func: wrapCommand(fnSetActive),
	Name: "set-active",
	Help: commands.HelpMeta{
		Section:     HelpSectionConnectionManagement,
		Description: "Set the bridge as the active browser (if you opened Google Messages in a real browser)",
	},
}

func fnSetActive(ce *WrappedCommandEvent) {
	if ce.User.Client == nil {
		ce.Reply("You don't have a Google Messages connection.")
		return
	}
	err := ce.User.Client.SetActiveSession()
	if err != nil {
		ce.Reply("Failed to set active session: %v", err)
	} else {
		ce.Reply("Set bridge as active session")
	}
}

var cmdPing = &commands.FullHandler{
	Func: wrapCommand(fnPing),
	Name: "ping",
	Help: commands.HelpMeta{
		Section:     HelpSectionConnectionManagement,
		Description: "Check your connection to Google Messages.",
	},
}

func fnPing(ce *WrappedCommandEvent) {
	if ce.User.Session == nil {
		if ce.User.Client != nil {
			ce.Reply("Connected to Google Messages, but not logged in.")
		} else {
			ce.Reply("You're not logged into Google Messages.")
		}
	} else if ce.User.Client == nil || !ce.User.Client.IsConnected() {
		ce.Reply("Linked to %s, but not connected to Google Messages.", ce.User.PhoneID)
	} else if ce.User.longPollingError != nil {
		ce.Reply("Linked to %s, but long polling is erroring (%v)", ce.User.PhoneID, ce.User.longPollingError)
	} else if ce.User.browserInactiveType != "" {
		ce.Reply("Linked to %s, but not active, use `set-active` to reconnect", ce.User.PhoneID)
	} else {
		modifiers := make([]string, 0, 3)
		if ce.User.batteryLow {
			modifiers = append(modifiers, "battery low")
		}
		if ce.User.mobileData {
			modifiers = append(modifiers, "using mobile data")
		}
		if !ce.User.phoneResponding {
			modifiers = append(modifiers, "phone not responding")
		}
		var modifierStr string
		if len(modifiers) > 0 {
			modifierStr = fmt.Sprintf(" (warnings: %s)", strings.Join(modifiers, ", "))
		}
		ce.Reply("Linked to %s and active as primary browser%s", ce.User.PhoneID, modifierStr)
	}
}

var cmdToggleBatteryNotifications = &commands.FullHandler{
	Func:    wrapCommand(fnToggleBatteryNotifications),
	Name:    "toggle-battery-notifications",
	Help: commands.HelpMeta{
		Section:     HelpSectionConnectionManagement,
		Description: "Silence Battery statuses.",
	},
}

func fnToggleBatteryNotifications(ce *WrappedCommandEvent) {
	ce.User.toggleNotifyBattery()
	if ce.User.DisableNotifyBattery {
		ce.Reply("Disabled battery notifications")
	} else {
		ce.Reply("Enabled battery notifications")
	}
	ce.ZLog.Trace().Msg("ToggleBatteryNotifications command finished")
}

var cmdToggleVerboseNotifications = &commands.FullHandler{
	Func:    wrapCommand(fnToggleVerboseNotifications),
	Name:    "toggle-verbose-notifications",
	Help: commands.HelpMeta{
		Section:     HelpSectionConnectionManagement,
		Description: "Silence Connected statuses when session changes and no data received recently.",
	},
}

func fnToggleVerboseNotifications(ce *WrappedCommandEvent) {
	ce.User.toggleNotifyVerbose()
	if ce.User.DisableNotifyVerbose {
		ce.Reply("Disabled verbose notifications")
	} else {
		ce.Reply("Enabled verbose notifications")
	}
	ce.ZLog.Trace().Msg("ToggleVerboseNotifications command finished")
}

var cmdPM = &commands.FullHandler{
	Func: wrapCommand(fnPM),
	Name: "pm",
	Help: commands.HelpMeta{
		Section:     HelpSectionPortalManagement,
		Description: "Create a chat on Google Messages",
		Args:        "<phone numbers...>",
	},
	RequiresLogin: true,
}

func fnPM(ce *WrappedCommandEvent) {
	var reqData gmproto.GetOrCreateConversationRequest
	reqData.Numbers = make([]*gmproto.ContactNumber, 0, len(ce.Args))
	for _, number := range ce.Args {
		number = strings.TrimSpace(number)
		if number == "" {
			continue
		}
		reqData.Numbers = append(reqData.Numbers, &gmproto.ContactNumber{
			// This should maybe sometimes be 7
			MysteriousInt: 2,
			Number:        number,
			Number2:       number,
		})
	}
	resp, err := ce.User.Client.GetOrCreateConversation(&reqData)
	if err != nil {
		ce.ZLog.Err(err).Msg("Failed to start chat")
		ce.Reply("Failed to start chat: request failed")
	} else if len(reqData.Numbers) > 1 && resp.GetStatus() == gmproto.GetOrCreateConversationResponse_CREATE_RCS {
		ce.Reply("All recipients are on RCS, but creating RCS groups via this command is not yet supported.")
	} else if resp.GetConversation() == nil {
		ce.ZLog.Warn().
			Int("req_number_count", len(reqData.Numbers)).
			Str("status", resp.GetStatus().String()).
			Msg("No conversation in chat create response")
		ce.Reply("Failed to start chat: no conversation in response")
	} else if portal := ce.User.GetPortalByID(resp.Conversation.ConversationID); portal.MXID != "" {
		ce.Reply("Chat already exists at [%s](https://matrix.to/#/%s)", portal.MXID, portal.MXID)
	} else if err = portal.CreateMatrixRoom(ce.Ctx, ce.User, resp.Conversation, false); err != nil {
		ce.ZLog.Err(err).Msg("Failed to create matrix room")
		ce.Reply("Failed to create portal room for conversation")
	} else {
		ce.Reply("Chat created: [%s](https://matrix.to/#/%s)", portal.MXID, portal.MXID)
	}
}

var cmdDeletePortal = &commands.FullHandler{
	Func: wrapCommand(fnDeletePortal),
	Name: "delete-portal",
	Help: commands.HelpMeta{
		Section:     HelpSectionPortalManagement,
		Description: "Delete the current portal. If the portal is used by other people, this is limited to bridge admins.",
	},
	RequiresPortal: true,
}

func fnDeletePortal(ce *WrappedCommandEvent) {
	if !ce.User.Admin && ce.Portal.Receiver != ce.User.RowID {
		ce.Reply("Only bridge admins can delete other users' portals")
		return
	}

	ce.ZLog.Info().Str("conversation_id", ce.Portal.ID).Msg("Deleting portal from command")
	ce.Portal.Delete(ce.Ctx)
	ce.Portal.Cleanup(ce.Ctx)
}

var cmdDeleteAllPortals = &commands.FullHandler{
	Func: wrapCommand(fnDeleteAllPortals),
	Name: "delete-all-portals",
	Help: commands.HelpMeta{
		Section:     HelpSectionPortalManagement,
		Description: "Delete all portals.",
	},
}

func fnDeleteAllPortals(ce *WrappedCommandEvent) {
	portals := ce.Bridge.GetAllPortalsForUser(ce.User.RowID)
	if len(portals) == 0 {
		ce.Reply("Didn't find any portals to delete")
		return
	}

	leave := func(portal *Portal) {
		if len(portal.MXID) > 0 {
			_, _ = portal.MainIntent().KickUser(ce.Ctx, portal.MXID, &mautrix.ReqKickUser{
				Reason: "Deleting portal",
				UserID: ce.User.MXID,
			})
		}
	}
	intent := ce.User.DoublePuppetIntent
	if intent != nil {
		leave = func(portal *Portal) {
			if len(portal.MXID) > 0 {
				_, _ = intent.LeaveRoom(ce.Ctx, portal.MXID)
				_, _ = intent.ForgetRoom(ce.Ctx, portal.MXID)
			}
		}
	}
	roomYeeting := ce.Bridge.SpecVersions.Supports(mautrix.BeeperFeatureRoomYeeting)
	if roomYeeting {
		leave = func(portal *Portal) {
			portal.Cleanup(ce.Ctx)
		}
	}
	ce.Reply("Found %d portals, deleting...", len(portals))
	for _, portal := range portals {
		portal.Delete(ce.Ctx)
		leave(portal)
	}
	if !roomYeeting {
		ce.Reply("Finished deleting portal info. Now cleaning up rooms in background.")
		go func() {
			for _, portal := range portals {
				portal.Cleanup(ce.Ctx)
			}
			ce.Reply("Finished background cleanup of deleted portal rooms.")
		}()
	} else {
		ce.Reply("Finished deleting portals.")
	}
}
