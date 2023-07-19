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
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/maulogger/v2"
	"maunium.net/go/maulogger/v2/maulogadapt"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/appservice"
	"maunium.net/go/mautrix/bridge"
	"maunium.net/go/mautrix/bridge/bridgeconfig"
	"maunium.net/go/mautrix/bridge/status"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/format"
	"maunium.net/go/mautrix/id"
	"maunium.net/go/mautrix/pushrules"

	"go.mau.fi/mautrix-gmessages/database"
	"go.mau.fi/mautrix-gmessages/libgm"
	"go.mau.fi/mautrix-gmessages/libgm/events"
	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
)

type User struct {
	*database.User
	Client *libgm.Client

	bridge *GMBridge
	zlog   zerolog.Logger
	// Deprecated
	log maulogger.Logger

	Admin           bool
	Whitelisted     bool
	PermissionLevel bridgeconfig.PermissionLevel

	mgmtCreateLock  sync.Mutex
	spaceCreateLock sync.Mutex
	connLock        sync.Mutex

	BridgeState *bridge.BridgeStateQueue

	spaceMembershipChecked bool

	longPollingError    error
	browserInactiveType status.BridgeStateErrorCode
	batteryLow          bool
	mobileData          bool

	loginInProgress   atomic.Bool
	pairSuccessChan   chan struct{}
	ongoingLoginChan  <-chan qrChannelItem
	loginChanReadLock sync.Mutex
	lastQRCode        string
	cancelLogin       func()

	DoublePuppetIntent *appservice.IntentAPI
}

func (br *GMBridge) getUserByMXID(userID id.UserID, onlyIfExists bool) *User {
	_, isPuppet := br.ParsePuppetMXID(userID)
	if isPuppet || userID == br.Bot.UserID {
		return nil
	}
	br.usersLock.Lock()
	defer br.usersLock.Unlock()
	user, ok := br.usersByMXID[userID]
	if !ok {
		userIDPtr := &userID
		if onlyIfExists {
			userIDPtr = nil
		}
		dbUser, err := br.DB.User.GetByMXID(context.TODO(), userID)
		if err != nil {
			br.ZLog.Err(err).
				Str("user_id", userID.String()).
				Msg("Failed to load user from database")
			return nil
		}
		return br.loadDBUser(dbUser, userIDPtr)
	}
	return user
}

func (br *GMBridge) GetUserByMXID(userID id.UserID) *User {
	return br.getUserByMXID(userID, false)
}

func (br *GMBridge) GetIUser(userID id.UserID, create bool) bridge.User {
	u := br.getUserByMXID(userID, !create)
	if u == nil {
		return nil
	}
	return u
}

func (user *User) GetPuppetByID(id, phone string) *Puppet {
	return user.bridge.GetPuppetByKey(database.Key{Receiver: user.RowID, ID: id}, phone)
}

func (user *User) GetPortalByID(id string) *Portal {
	return user.bridge.GetPortalByKey(database.Key{Receiver: user.RowID, ID: id})
}

func (user *User) GetIDoublePuppet() bridge.DoublePuppet {
	return user
}

func (user *User) GetIGhost() bridge.Ghost {
	return nil
}

func (user *User) GetPermissionLevel() bridgeconfig.PermissionLevel {
	return user.PermissionLevel
}

func (user *User) GetManagementRoomID() id.RoomID {
	return user.ManagementRoom
}

func (user *User) GetMXID() id.UserID {
	return user.MXID
}

func (user *User) GetCommandState() map[string]interface{} {
	return nil
}

func (br *GMBridge) GetUserByMXIDIfExists(userID id.UserID) *User {
	return br.getUserByMXID(userID, true)
}

func (br *GMBridge) GetUserByPhone(phone string) *User {
	br.usersLock.Lock()
	defer br.usersLock.Unlock()
	user, ok := br.usersByPhone[phone]
	if !ok {
		dbUser, err := br.DB.User.GetByPhone(context.TODO(), phone)
		if err != nil {
			br.ZLog.Err(err).
				Str("phone", phone).
				Msg("Failed to load user from database")
		}
		return br.loadDBUser(dbUser, nil)
	}
	return user
}

func (user *User) addToPhoneMap() {
	user.bridge.usersLock.Lock()
	user.bridge.usersByPhone[user.PhoneID] = user
	user.bridge.usersLock.Unlock()
}

func (user *User) removeFromPhoneMap(state status.BridgeState) {
	user.bridge.usersLock.Lock()
	phoneUser, ok := user.bridge.usersByPhone[user.PhoneID]
	if ok && user == phoneUser {
		delete(user.bridge.usersByPhone, user.PhoneID)
	}
	user.bridge.usersLock.Unlock()
	user.BridgeState.Send(state)
}

func (br *GMBridge) GetAllUsersWithSession() []*User {
	return br.loadManyUsers(br.DB.User.GetAllWithSession)
}

func (br *GMBridge) GetAllUsersWithDoublePuppet() []*User {
	return br.loadManyUsers(br.DB.User.GetAllWithDoublePuppet)
}

func (br *GMBridge) loadManyUsers(query func(ctx context.Context) ([]*database.User, error)) []*User {
	br.usersLock.Lock()
	defer br.usersLock.Unlock()
	dbUsers, err := query(context.TODO())
	if err != nil {
		br.ZLog.Err(err).Msg("Failed to all load users from database")
		return []*User{}
	}
	output := make([]*User, len(dbUsers))
	for index, dbUser := range dbUsers {
		user, ok := br.usersByMXID[dbUser.MXID]
		if !ok {
			user = br.loadDBUser(dbUser, nil)
		}
		output[index] = user
	}
	return output
}

func (br *GMBridge) loadDBUser(dbUser *database.User, mxid *id.UserID) *User {
	if dbUser == nil {
		if mxid == nil {
			return nil
		}
		dbUser = br.DB.User.New()
		dbUser.MXID = *mxid
		err := dbUser.Insert(context.TODO())
		if err != nil {
			br.ZLog.Err(err).
				Str("user_id", mxid.String()).
				Msg("Failed to insert user to database")
			return nil
		}
	}
	user := br.NewUser(dbUser)
	br.usersByMXID[user.MXID] = user
	if user.Session != nil && user.PhoneID != "" {
		br.usersByPhone[user.PhoneID] = user
	} else {
		user.Session = nil
		user.PhoneID = ""
	}
	if len(user.ManagementRoom) > 0 {
		br.managementRooms[user.ManagementRoom] = user
	}
	return user
}

func (br *GMBridge) NewUser(dbUser *database.User) *User {
	user := &User{
		User:   dbUser,
		bridge: br,
		zlog:   br.ZLog.With().Str("user_id", dbUser.MXID.String()).Logger(),
	}
	user.log = maulogadapt.ZeroAsMau(&user.zlog)
	user.longPollingError = errors.New("not connected")

	user.PermissionLevel = user.bridge.Config.Bridge.Permissions.Get(user.MXID)
	user.Whitelisted = user.PermissionLevel >= bridgeconfig.PermissionLevelUser
	user.Admin = user.PermissionLevel >= bridgeconfig.PermissionLevelAdmin
	user.BridgeState = br.NewBridgeStateQueue(user)
	return user
}

func (user *User) ensureInvited(intent *appservice.IntentAPI, roomID id.RoomID, isDirect bool) (ok bool) {
	extraContent := make(map[string]any)
	if isDirect {
		extraContent["is_direct"] = true
	}
	if user.DoublePuppetIntent != nil {
		extraContent["fi.mau.will_auto_accept"] = true
	}
	_, err := intent.InviteUser(roomID, &mautrix.ReqInviteUser{UserID: user.MXID}, extraContent)
	var httpErr mautrix.HTTPError
	if err != nil && errors.As(err, &httpErr) && httpErr.RespError != nil && strings.Contains(httpErr.RespError.Err, "is already in the room") {
		user.bridge.StateStore.SetMembership(roomID, user.MXID, event.MembershipJoin)
		ok = true
		return
	} else if err != nil {
		user.zlog.Warn().Err(err).Str("room_id", roomID.String()).Msg("Failed to invite user to room")
	} else {
		ok = true
	}

	if user.DoublePuppetIntent != nil {
		err = user.DoublePuppetIntent.EnsureJoined(roomID, appservice.EnsureJoinedParams{IgnoreCache: true})
		if err != nil {
			user.zlog.Warn().Err(err).Str("room_id", roomID.String()).Msg("Failed to auto-join room")
			ok = false
		} else {
			ok = true
		}
	}
	return
}

func (user *User) GetSpaceRoom() id.RoomID {
	if !user.bridge.Config.Bridge.PersonalFilteringSpaces {
		return ""
	}

	if len(user.SpaceRoom) == 0 {
		user.spaceCreateLock.Lock()
		defer user.spaceCreateLock.Unlock()
		if len(user.SpaceRoom) > 0 {
			return user.SpaceRoom
		}

		resp, err := user.bridge.Bot.CreateRoom(&mautrix.ReqCreateRoom{
			Visibility: "private",
			Name:       "Google Messages",
			Topic:      "Your Google Messages bridged chats",
			InitialState: []*event.Event{{
				Type: event.StateRoomAvatar,
				Content: event.Content{
					Parsed: &event.RoomAvatarEventContent{
						URL: user.bridge.Config.AppService.Bot.ParsedAvatar,
					},
				},
			}},
			CreationContent: map[string]interface{}{
				"type": event.RoomTypeSpace,
			},
			PowerLevelOverride: &event.PowerLevelsEventContent{
				Users: map[id.UserID]int{
					user.bridge.Bot.UserID: 9001,
					user.MXID:              50,
				},
			},
		})

		if err != nil {
			user.zlog.Err(err).Msg("Failed to auto-create space room")
		} else {
			user.SpaceRoom = resp.RoomID
			err = user.Update(context.TODO())
			if err != nil {
				user.zlog.Err(err).Msg("Failed to update database after creating space room")
			}
			user.ensureInvited(user.bridge.Bot, user.SpaceRoom, false)
		}
	} else if !user.spaceMembershipChecked && !user.bridge.StateStore.IsInRoom(user.SpaceRoom, user.MXID) {
		user.ensureInvited(user.bridge.Bot, user.SpaceRoom, false)
	}
	user.spaceMembershipChecked = true

	return user.SpaceRoom
}

func (user *User) GetManagementRoom() id.RoomID {
	if len(user.ManagementRoom) == 0 {
		user.mgmtCreateLock.Lock()
		defer user.mgmtCreateLock.Unlock()
		if len(user.ManagementRoom) > 0 {
			return user.ManagementRoom
		}
		creationContent := make(map[string]interface{})
		if !user.bridge.Config.Bridge.FederateRooms {
			creationContent["m.federate"] = false
		}
		resp, err := user.bridge.Bot.CreateRoom(&mautrix.ReqCreateRoom{
			Topic:           "Google Messages bridge notices",
			IsDirect:        true,
			CreationContent: creationContent,
		})
		if err != nil {
			user.zlog.Err(err).Msg("Failed to auto-create management room")
		} else {
			user.SetManagementRoom(resp.RoomID)
		}
	}
	return user.ManagementRoom
}

func (user *User) SetManagementRoom(roomID id.RoomID) {
	log := user.zlog.With().
		Str("management_room_id", roomID.String()).
		Str("action", "SetManagementRoom").
		Logger()
	existingUser, ok := user.bridge.managementRooms[roomID]
	if ok {
		existingUser.ManagementRoom = ""
		err := existingUser.Update(context.TODO())
		if err != nil {
			log.Err(err).
				Str("prev_user_id", existingUser.MXID.String()).
				Msg("Failed to clear management room from previous user")
		}
	}

	user.ManagementRoom = roomID
	user.bridge.managementRooms[user.ManagementRoom] = user
	err := user.Update(context.TODO())
	if err != nil {
		log.Err(err).Msg("Failed to update database with management room ID")
	}
}

var ErrAlreadyLoggedIn = errors.New("already logged in")
var ErrLoginInProgress = errors.New("login already in progress")
var ErrLoginTimeout = errors.New("login timed out")

func (user *User) createClient(sess *libgm.AuthData) {
	user.Client = libgm.NewClient(sess, user.zlog.With().Str("component", "libgm").Logger())
	user.Client.SetEventHandler(user.syncHandleEvent)
}

func (user *User) syncHandleEvent(ev any) {
	go user.HandleEvent(ev)
}

type qrChannelItem struct {
	success bool
	qr      string
	err     error
}

func (qci qrChannelItem) IsEmpty() bool {
	return !qci.success && qci.qr == "" && qci.err == nil
}

func (user *User) Login(maxAttempts int) (<-chan qrChannelItem, error) {
	user.connLock.Lock()
	defer user.connLock.Unlock()
	if user.Session != nil {
		return nil, ErrAlreadyLoggedIn
	} else if !user.loginInProgress.CompareAndSwap(false, true) {
		return user.ongoingLoginChan, ErrLoginInProgress
	}
	if user.Client != nil {
		user.unlockedDeleteConnection()
	}
	pairSuccessChan := make(chan struct{})
	user.pairSuccessChan = pairSuccessChan
	user.createClient(libgm.NewAuthData())
	qr, err := user.Client.StartLogin()
	if err != nil {
		user.DeleteConnection()
		user.pairSuccessChan = nil
		user.loginInProgress.Store(false)
		return nil, fmt.Errorf("failed to connect to Google Messages: %w", err)
	}
	Segment.Track(user.MXID, "$login_start")
	ch := make(chan qrChannelItem, maxAttempts+2)
	ctx, cancel := context.WithCancel(context.Background())
	user.cancelLogin = cancel
	user.ongoingLoginChan = ch
	ch <- qrChannelItem{qr: qr}
	user.lastQRCode = qr
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		success := false
		defer func() {
			ticker.Stop()
			if !success {
				user.zlog.Debug().Msg("Deleting connection as login wasn't successful")
				user.DeleteConnection()
			}
			user.pairSuccessChan = nil
			user.ongoingLoginChan = nil
			user.lastQRCode = ""
			close(ch)
			user.loginInProgress.Store(false)
			cancel()
			user.cancelLogin = nil
		}()
		for {
			maxAttempts--
			select {
			case <-ctx.Done():
				user.zlog.Debug().Err(ctx.Err()).Msg("Login context cancelled")
				return
			case <-ticker.C:
				if maxAttempts <= 0 {
					ch <- qrChannelItem{err: ErrLoginTimeout}
					return
				}
				qr, err := user.Client.RefreshPhoneRelay()
				if err != nil {
					ch <- qrChannelItem{err: fmt.Errorf("failed to refresh QR code: %w", err)}
					return
				}
				ch <- qrChannelItem{qr: qr}
				user.lastQRCode = qr
			case <-pairSuccessChan:
				ch <- qrChannelItem{success: true}
				success = true
				return
			}
		}
	}()
	return ch, nil
}

func (user *User) Connect() bool {
	user.connLock.Lock()
	defer user.connLock.Unlock()
	if user.Client != nil {
		return true
	} else if user.Session == nil {
		return false
	}
	if len(user.AccessToken) == 0 {
		user.tryAutomaticDoublePuppeting()
	}
	user.zlog.Debug().Msg("Connecting to Google Messages")
	user.BridgeState.Send(status.BridgeState{StateEvent: status.StateConnecting, Error: GMConnecting})
	user.createClient(user.Session)
	err := user.Client.Connect()
	if err != nil {
		user.zlog.Err(err).Msg("Error connecting to Google Messages")
		user.BridgeState.Send(status.BridgeState{
			StateEvent: status.StateUnknownError,
			Error:      GMConnectionFailed,
			Info: map[string]interface{}{
				"go_error": err.Error(),
			},
		})
		return false
	}
	return true
}

func (user *User) unlockedDeleteConnection() {
	if user.Client == nil {
		return
	}
	user.Client.Disconnect()
	user.Client.SetEventHandler(nil)
	user.Client = nil
}

func (user *User) DeleteConnection() {
	user.connLock.Lock()
	defer user.connLock.Unlock()
	user.unlockedDeleteConnection()
	user.longPollingError = errors.New("not connected")
}

func (user *User) HasSession() bool {
	return user.Session != nil
}

func (user *User) DeleteSession() {
	user.Session = nil
	user.SelfParticipantIDs = []string{}
	user.PhoneID = ""
	err := user.Update(context.TODO())
	if err != nil {
		user.zlog.Err(err).Msg("Failed to delete session from database")
	}
}

func (user *User) IsConnected() bool {
	return user.Client != nil && user.Client.IsConnected()
}

func (user *User) IsLoggedIn() bool {
	return user.IsConnected() && user.Client.IsLoggedIn()
}

func (user *User) tryAutomaticDoublePuppeting() {
	if !user.bridge.Config.CanAutoDoublePuppet(user.MXID) || user.DoublePuppetIntent != nil {
		return
	}

	if err := user.loginWithSharedSecret(); err != nil {
		user.zlog.Warn().Err(err).Msg("Failed to login with shared secret for double puppeting")
	} else if err = user.startCustomMXID(false); err != nil {
		user.zlog.Warn().Err(err).Msg("Failed to start double puppet after logging in with shared secret")
	} else {
		user.zlog.Info().Msg("Successfully automatically enabled double puppet")
	}
}

func (user *User) sendMarkdownBridgeAlert(formatString string, args ...interface{}) {
	if user.bridge.Config.Bridge.DisableBridgeAlerts {
		return
	}
	notice := fmt.Sprintf(formatString, args...)
	content := format.RenderMarkdown(notice, true, false)
	_, err := user.bridge.Bot.SendMessageEvent(user.GetManagementRoom(), event.EventMessage, content)
	if err != nil {
		user.zlog.Warn().Err(err).Str("notice", notice).Msg("Failed to send bridge alert")
	}
}

func (user *User) HandleEvent(event interface{}) {
	switch v := event.(type) {
	case *events.ListenFatalError:
		user.Logout(status.BridgeState{
			StateEvent: status.StateBadCredentials,
			Error:      GMFatalError,
			Info:       map[string]any{"go_error": v.Error.Error()},
		}, false)
	case *events.ListenTemporaryError:
		user.longPollingError = v.Error
		user.BridgeState.Send(status.BridgeState{
			StateEvent: status.StateTransientDisconnect,
			Error:      GMListenError,
			Info:       map[string]any{"go_error": v.Error.Error()},
		})
	case *events.ListenRecovered:
		user.longPollingError = nil
		user.BridgeState.Send(status.BridgeState{StateEvent: status.StateConnected})
	case *events.PairSuccessful:
		user.Session = user.Client.AuthData
		user.PhoneID = v.GetMobile().GetSourceID()
		user.tryAutomaticDoublePuppeting()
		user.addToPhoneMap()
		err := user.Update(context.TODO())
		if err != nil {
			user.zlog.Err(err).Msg("Failed to update session in database")
		}
		if ch := user.pairSuccessChan; ch != nil {
			close(ch)
		}
	case *gmproto.RevokePairData:
		user.zlog.Info().Any("revoked_device", v.GetRevokedDevice()).Msg("Got pair revoked event")
		user.Logout(status.BridgeState{
			StateEvent: status.StateBadCredentials,
			Error:      GMUnpaired,
		}, false)
	case *events.AuthTokenRefreshed:
		err := user.Update(context.TODO())
		if err != nil {
			user.zlog.Err(err).Msg("Failed to update session in database")
		}
	case *gmproto.Conversation:
		user.syncConversation(v)
	case *gmproto.Message:
		portal := user.GetPortalByID(v.GetConversationID())
		portal.messages <- PortalMessage{evt: v, source: user}
	case *events.ClientReady:
		user.browserInactiveType = ""
		user.BridgeState.Send(status.BridgeState{StateEvent: status.StateConnected})
		go func() {
			for _, conv := range v.Conversations {
				user.syncConversation(conv)
			}
		}()
	case *events.BrowserActive:
		user.zlog.Trace().Any("data", v).Msg("Browser active")
		user.browserInactiveType = ""
	case *gmproto.UserAlertEvent:
		user.handleUserAlert(v)
	default:
		user.zlog.Trace().Any("data", v).Type("data_type", v).Msg("Unknown event")
	}
}

func (user *User) handleUserAlert(v *gmproto.UserAlertEvent) {
	user.zlog.Debug().Any("data", v).Msg("Got user alert event")
	switch v.GetAlertType() {
	case gmproto.AlertType_BROWSER_INACTIVE:
		// TODO aggressively reactivate if configured to do so
		user.browserInactiveType = GMBrowserInactive
	case gmproto.AlertType_BROWSER_INACTIVE_FROM_TIMEOUT:
		user.browserInactiveType = GMBrowserInactiveTimeout
	case gmproto.AlertType_BROWSER_INACTIVE_FROM_INACTIVITY:
		user.browserInactiveType = GMBrowserInactiveInactivity
	case gmproto.AlertType_MOBILE_DATA_CONNECTION:
		user.mobileData = true
	case gmproto.AlertType_MOBILE_WIFI_CONNECTION:
		user.mobileData = false
	case gmproto.AlertType_MOBILE_BATTERY_LOW:
		user.batteryLow = true
	case gmproto.AlertType_MOBILE_BATTERY_RESTORED:
		user.batteryLow = false
	default:
		return
	}
	user.BridgeState.Send(status.BridgeState{StateEvent: status.StateConnected})
}

func (user *User) FillBridgeState(state status.BridgeState) status.BridgeState {
	if state.Info == nil {
		state.Info = make(map[string]any)
	}
	if state.StateEvent == status.StateConnected {
		state.Info["battery_low"] = user.batteryLow
		state.Info["mobile_data"] = user.mobileData
		state.Info["browser_active"] = user.browserInactiveType == ""
		if user.longPollingError != nil {
			state.StateEvent = status.StateTransientDisconnect
			state.Error = GMListenError
			state.Info["go_error"] = user.longPollingError.Error()
		}
		if user.browserInactiveType != "" {
			state.StateEvent = status.StateTransientDisconnect
			state.Error = user.browserInactiveType
		}
	}
	return state
}

func (user *User) Logout(state status.BridgeState, unpair bool) (logoutOK bool) {
	if user.Client != nil && unpair {
		_, err := user.Client.Unpair()
		if err != nil {
			user.zlog.Debug().Err(err).Msg("Error sending unpair request")
		} else {
			logoutOK = true
		}
	}
	user.removeFromPhoneMap(state)
	user.DeleteConnection()
	user.DeleteSession()
	return
}

func (user *User) syncConversation(v *gmproto.Conversation) {
	updateType := v.GetStatus()
	portal := user.GetPortalByID(v.GetConversationID())
	if portal.MXID != "" {
		switch updateType {
		// TODO also delete if blocked?
		case gmproto.ConvUpdateTypes_DELETED:
			user.zlog.Info().Str("conversation_id", portal.ID).Msg("Got delete event, cleaning up portal")
			portal.Delete()
			portal.Cleanup(false)
		default:
			portal.UpdateMetadata(user, v)
			portal.missedForwardBackfill(user, time.UnixMicro(v.LastMessageTimestamp), v.LatestMessageID, !v.GetUnread())
			// TODO sync read status if there was nothing backfilled
		}
	} else if updateType == gmproto.ConvUpdateTypes_UNARCHIVED || updateType == gmproto.ConvUpdateTypes_ARCHIVED {
		err := portal.CreateMatrixRoom(user, v)
		if err != nil {
			user.zlog.Err(err).Msg("Error creating Matrix room from conversation event")
		}
	} else {
		user.zlog.Debug().Str("update_type", updateType.String()).Msg("Not creating portal for conversation")
	}
}

func (user *User) updateChatMute(portal *Portal, mutedUntil time.Time) {
	intent := user.DoublePuppetIntent
	if intent == nil || len(portal.MXID) == 0 {
		return
	}
	var err error
	if mutedUntil.IsZero() && mutedUntil.Before(time.Now()) {
		user.log.Debugfln("Portal %s is muted until %s, unmuting...", portal.MXID, mutedUntil)
		err = intent.DeletePushRule("global", pushrules.RoomRule, string(portal.MXID))
	} else {
		user.log.Debugfln("Portal %s is muted until %s, muting...", portal.MXID, mutedUntil)
		err = intent.PutPushRule("global", pushrules.RoomRule, string(portal.MXID), &mautrix.ReqPutPushRule{
			Actions: []pushrules.PushActionType{pushrules.ActionDontNotify},
		})
	}
	if err != nil && !errors.Is(err, mautrix.MNotFound) {
		user.log.Warnfln("Failed to update push rule for %s through double puppet: %v", portal.MXID, err)
	}
}

type CustomTagData struct {
	Order        json.Number `json:"order"`
	DoublePuppet string      `json:"fi.mau.double_puppet_source"`
}

type CustomTagEventContent struct {
	Tags map[string]CustomTagData `json:"tags"`
}

func (user *User) updateChatTag(portal *Portal, tag string, active bool) {
	intent := user.DoublePuppetIntent
	if intent == nil || len(portal.MXID) == 0 {
		return
	}
	var existingTags CustomTagEventContent
	err := intent.GetTagsWithCustomData(portal.MXID, &existingTags)
	if err != nil && !errors.Is(err, mautrix.MNotFound) {
		user.log.Warnfln("Failed to get tags of %s: %v", portal.MXID, err)
	}
	currentTag, ok := existingTags.Tags[tag]
	if active && !ok {
		user.log.Debugln("Adding tag", tag, "to", portal.MXID)
		data := CustomTagData{Order: "0.5", DoublePuppet: user.bridge.Name}
		err = intent.AddTagWithCustomData(portal.MXID, tag, &data)
	} else if !active && ok && currentTag.DoublePuppet == user.bridge.Name {
		user.log.Debugln("Removing tag", tag, "from", portal.MXID)
		err = intent.RemoveTag(portal.MXID, tag)
	} else {
		err = nil
	}
	if err != nil {
		user.log.Warnfln("Failed to update tag %s for %s through double puppet: %v", tag, portal.MXID, err)
	}
}

type CustomReadReceipt struct {
	Timestamp          int64  `json:"ts,omitempty"`
	DoublePuppetSource string `json:"fi.mau.double_puppet_source,omitempty"`
}

type CustomReadMarkers struct {
	mautrix.ReqSetReadMarkers
	ReadExtra      CustomReadReceipt `json:"com.beeper.read.extra"`
	FullyReadExtra CustomReadReceipt `json:"com.beeper.fully_read.extra"`
}

func (user *User) syncChatDoublePuppetDetails(portal *Portal, conv *gmproto.Conversation, justCreated bool) {
	if user.DoublePuppetIntent == nil || len(portal.MXID) == 0 {
		return
	}
	if justCreated || !user.bridge.Config.Bridge.TagOnlyOnCreate {
		//user.updateChatMute(portal, chat.MutedUntil)
		//user.updateChatTag(portal, user.bridge.Config.Bridge.ArchiveTag, conv.Status == 2)
		//user.updateChatTag(portal, user.bridge.Config.Bridge.PinnedTag, chat.Pinned)
	}
}

func (user *User) UpdateDirectChats(chats map[id.UserID][]id.RoomID) {
	if !user.bridge.Config.Bridge.SyncDirectChatList || user.DoublePuppetIntent == nil {
		return
	}
	intent := user.DoublePuppetIntent
	method := http.MethodPatch
	//if chats == nil {
	//	chats = user.getDirectChats()
	//	method = http.MethodPut
	//}
	user.zlog.Debug().Msg("Updating m.direct list on homeserver")
	var err error
	if user.bridge.Config.Homeserver.Software == bridgeconfig.SoftwareAsmux {
		urlPath := intent.BuildClientURL("unstable", "com.beeper.asmux", "dms")
		_, err = intent.MakeFullRequest(mautrix.FullRequest{
			Method:      method,
			URL:         urlPath,
			Headers:     http.Header{"X-Asmux-Auth": {user.bridge.AS.Registration.AppToken}},
			RequestJSON: chats,
		})
	} else {
		existingChats := make(map[id.UserID][]id.RoomID)
		err = intent.GetAccountData(event.AccountDataDirectChats.Type, &existingChats)
		if err != nil {
			user.log.Warnln("Failed to get m.direct list to update it:", err)
			return
		}
		for userID, rooms := range existingChats {
			if _, ok := user.bridge.ParsePuppetMXID(userID); !ok {
				// This is not a ghost user, include it in the new list
				chats[userID] = rooms
			} else if _, ok := chats[userID]; !ok && method == http.MethodPatch {
				// This is a ghost user, but we're not replacing the whole list, so include it too
				chats[userID] = rooms
			}
		}
		err = intent.SetAccountData(event.AccountDataDirectChats.Type, &chats)
	}
	if err != nil {
		user.log.Warnln("Failed to update m.direct list:", err)
	}
}

func (user *User) markUnread(portal *Portal, unread bool) {
	if user.DoublePuppetIntent == nil {
		return
	}

	log := user.zlog.With().Str("room_id", portal.MXID.String()).Logger()

	err := user.DoublePuppetIntent.SetRoomAccountData(portal.MXID, "m.marked_unread", map[string]bool{"unread": unread})
	if err != nil {
		log.Warn().Err(err).Str("event_type", "m.marked_unread").
			Msg("Failed to mark room as unread")
	} else {
		log.Debug().Str("event_type", "m.marked_unread").Msg("Marked room as unread")
	}

	err = user.DoublePuppetIntent.SetRoomAccountData(portal.MXID, "com.famedly.marked_unread", map[string]bool{"unread": unread})
	if err != nil {
		log.Warn().Err(err).Str("event_type", "com.famedly.marked_unread").
			Msg("Failed to mark room as unread")
	} else {
		log.Debug().Str("event_type", "com.famedly.marked_unread").Msg("Marked room as unread")
	}
}
