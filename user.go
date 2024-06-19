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
	"google.golang.org/protobuf/proto"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/appservice"
	"maunium.net/go/mautrix/bridge"
	"maunium.net/go/mautrix/bridge/bridgeconfig"
	"maunium.net/go/mautrix/bridge/commands"
	"maunium.net/go/mautrix/bridge/status"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/format"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-gmessages/database"
	"go.mau.fi/mautrix-gmessages/libgm"
	"go.mau.fi/mautrix-gmessages/libgm/events"
	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

type User struct {
	*database.User
	Client *libgm.Client

	bridge *GMBridge
	zlog   zerolog.Logger

	Admin           bool
	Whitelisted     bool
	PermissionLevel bridgeconfig.PermissionLevel

	mgmtCreateLock  sync.Mutex
	spaceCreateLock sync.Mutex
	connLock        sync.Mutex

	BridgeState  *bridge.BridgeStateQueue
	CommandState *commands.CommandState

	spaceMembershipChecked bool

	longPollingError            error
	browserInactiveType         status.BridgeStateErrorCode
	switchedToGoogleLogin       bool
	batteryLow                  bool
	mobileData                  bool
	phoneResponding             bool
	ready                       bool
	sessionID                   string
	batteryLowAlertSent         time.Time
	pollErrorAlertSent          bool
	phoneNotRespondingAlertSent bool
	didHackySetActive           bool
	noDataReceivedRecently      bool
	recentlyDisconnected        bool
	lastDataReceived            time.Time
	gaiaHackyDeviceSwitcher     int

	loginInProgress  atomic.Bool
	pairSuccessChan  chan struct{}
	ongoingLoginChan <-chan qrChannelItem
	lastQRCode       string
	cancelLogin      func()

	googleAsyncPairErrChan atomic.Pointer[chan error]

	DoublePuppetIntent *appservice.IntentAPI
}

func (user *User) GetCommandState() *commands.CommandState {
	return user.CommandState
}

func (user *User) SetCommandState(state *commands.CommandState) {
	user.CommandState = state
}

var _ commands.CommandingUser = (*User)(nil)

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

func (br *GMBridge) GetUserByMXIDIfExists(userID id.UserID) *User {
	return br.getUserByMXID(userID, true)
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
	user.longPollingError = errors.New("not connected")
	user.phoneResponding = true

	user.PermissionLevel = user.bridge.Config.Bridge.Permissions.Get(user.MXID)
	user.Whitelisted = user.PermissionLevel >= bridgeconfig.PermissionLevelUser
	user.Admin = user.PermissionLevel >= bridgeconfig.PermissionLevelAdmin
	user.BridgeState = br.NewBridgeStateQueue(user)
	return user
}

func (user *User) ensureInvited(ctx context.Context, intent *appservice.IntentAPI, roomID id.RoomID, isDirect bool) (ok bool) {
	extraContent := make(map[string]any)
	if isDirect {
		extraContent["is_direct"] = true
	}
	if user.DoublePuppetIntent != nil {
		extraContent["fi.mau.will_auto_accept"] = true
	}
	_, err := intent.InviteUser(ctx, roomID, &mautrix.ReqInviteUser{UserID: user.MXID}, extraContent)
	var httpErr mautrix.HTTPError
	if err != nil && errors.As(err, &httpErr) && httpErr.RespError != nil && strings.Contains(httpErr.RespError.Err, "is already in the room") {
		// TODO log errors from SetMembership
		user.bridge.StateStore.SetMembership(ctx, roomID, user.MXID, event.MembershipJoin)
		ok = true
		return
	} else if err != nil {
		user.zlog.Warn().Err(err).Str("room_id", roomID.String()).Msg("Failed to invite user to room")
	} else {
		ok = true
	}

	if user.DoublePuppetIntent != nil {
		err = user.DoublePuppetIntent.EnsureJoined(ctx, roomID, appservice.EnsureJoinedParams{IgnoreCache: true})
		if err != nil {
			user.zlog.Warn().Err(err).Str("room_id", roomID.String()).Msg("Failed to auto-join room")
			ok = false
		} else {
			ok = true
		}
	}
	return
}

func (user *User) GetSpaceRoom(ctx context.Context) id.RoomID {
	if !user.bridge.Config.Bridge.PersonalFilteringSpaces {
		return ""
	}

	if len(user.SpaceRoom) == 0 {
		user.spaceCreateLock.Lock()
		defer user.spaceCreateLock.Unlock()
		if len(user.SpaceRoom) > 0 {
			return user.SpaceRoom
		}

		resp, err := user.bridge.Bot.CreateRoom(ctx, &mautrix.ReqCreateRoom{
			Visibility: "private",
			Name:       "Google Messages",
			Topic:      "Your Google Messages bridged chats",
			InitialState: []*event.Event{{
				Type: event.StateRoomAvatar,
				Content: event.Content{
					Parsed: &event.RoomAvatarEventContent{
						URL: user.bridge.Config.AppService.Bot.ParsedAvatar.CUString(),
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
			user.ensureInvited(ctx, user.bridge.Bot, user.SpaceRoom, false)
		}
	} else if !user.spaceMembershipChecked && !user.bridge.StateStore.IsInRoom(ctx, user.SpaceRoom, user.MXID) {
		user.ensureInvited(ctx, user.bridge.Bot, user.SpaceRoom, false)
	}
	user.spaceMembershipChecked = true

	return user.SpaceRoom
}

func (user *User) GetManagementRoom(ctx context.Context) id.RoomID {
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
		resp, err := user.bridge.Bot.CreateRoom(ctx, &mautrix.ReqCreateRoom{
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
	ctx := context.TODO()
	existingUser, ok := user.bridge.managementRooms[roomID]
	if ok {
		existingUser.ManagementRoom = ""
		err := existingUser.Update(ctx)
		if err != nil {
			log.Err(err).
				Str("prev_user_id", existingUser.MXID.String()).
				Msg("Failed to clear management room from previous user")
		}
	}

	user.ManagementRoom = roomID
	user.bridge.managementRooms[user.ManagementRoom] = user
	err := user.Update(ctx)
	if err != nil {
		log.Err(err).Msg("Failed to update database with management room ID")
	}
}

var ErrAlreadyLoggedIn = errors.New("already logged in")
var ErrLoginInProgress = errors.New("login already in progress")
var ErrNoLoginInProgress = errors.New("no login in progress")
var ErrLoginTimeout = errors.New("login timed out")

func (user *User) createClient(sess *libgm.AuthData) {
	user.Client = libgm.NewClient(sess, user.zlog.With().Str("component", "libgm").Logger())
	user.Client.SetEventHandler(user.syncHandleEvent)
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
		user.unlockedDeleteConnection()
		user.pairSuccessChan = nil
		user.loginInProgress.Store(false)
		return nil, fmt.Errorf("failed to connect to Google Messages: %w", err)
	}
	Analytics.Track(user.MXID, "$login_start", map[string]any{"mode": "qr"})
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

func (user *User) AsyncLoginGoogleStart(cookies map[string]string) (outEmoji string, outErr error) {
	errChan := make(chan error, 1)
	errChanPtr := &errChan
	if !user.googleAsyncPairErrChan.CompareAndSwap(nil, errChanPtr) {
		close(errChan)
		outErr = fmt.Errorf("%w: wait not called", ErrLoginInProgress)
		return
	}
	var callbackDone bool
	var initialWait sync.WaitGroup
	initialWait.Add(1)
	callback := func(emoji string) {
		user.zlog.Info().Msg("Async google login got emoji")
		callbackDone = true
		outEmoji = emoji
		initialWait.Done()
	}
	if cancelPrevLogin := user.cancelLogin; cancelPrevLogin != nil {
		user.zlog.Warn().Msg("Another async google login started while previous one was in progress")
		cancelPrevLogin()
	}
	var ctx context.Context
	ctx, user.cancelLogin = context.WithCancel(context.Background())
	go func() {
		defer func() {
			user.cancelLogin = nil
		}()
		err := user.LoginGoogle(ctx, cookies, callback)
		if !callbackDone {
			user.zlog.Err(err).Msg("Async google login failed before callback")
			initialWait.Done()
			outErr = err
			close(errChan)
			user.googleAsyncPairErrChan.Store(nil)
		} else {
			if err != nil {
				user.zlog.Err(err).Msg("Async google login failed after callback")
			} else {
				user.zlog.Info().Msg("Async google login succeeded")
			}
			errChan <- err
			if user.googleAsyncPairErrChan.Load() == errChanPtr {
				go func() {
					time.Sleep(5 * time.Second)
					if user.googleAsyncPairErrChan.CompareAndSwap(errChanPtr, nil) {
						user.zlog.Warn().Msg("Async login was never waited, clearing state")
					}
				}()
			}
		}
	}()
	initialWait.Wait()
	return
}

func (user *User) AsyncLoginGoogleWait(ctx context.Context) error {
	ch := user.googleAsyncPairErrChan.Swap(nil)
	if ch == nil {
		return ErrNoLoginInProgress
	}
	select {
	case ret := <-*ch:
		return ret
	case <-ctx.Done():
		user.zlog.Err(ctx.Err()).Msg("Login wait context canceled, canceling login")
		if cancelLogin := user.cancelLogin; cancelLogin != nil {
			cancelLogin()
		}
		return ctx.Err()
	}
}

func (user *User) LoginGoogle(ctx context.Context, cookies map[string]string, emojiCallback func(string)) error {
	user.connLock.Lock()
	defer user.connLock.Unlock()
	if user.Session != nil {
		return ErrAlreadyLoggedIn
	} else if !user.loginInProgress.CompareAndSwap(false, true) {
		return ErrLoginInProgress
	}
	defer user.loginInProgress.Store(false)
	if user.Client != nil {
		user.unlockedDeleteConnection()
	}
	user.pairSuccessChan = make(chan struct{})
	defer func() {
		user.pairSuccessChan = nil
	}()
	authData := libgm.NewAuthData()
	authData.SetCookies(cookies)
	user.createClient(authData)
	Analytics.Track(user.MXID, "$login_start", map[string]any{"mode": "google"})
	user.Client.GaiaHackyDeviceSwitcher = user.gaiaHackyDeviceSwitcher
	err := user.Client.DoGaiaPairing(ctx, emojiCallback)
	if err != nil {
		user.unlockedDeleteConnection()
		switch {
		case errors.Is(err, libgm.ErrNoDevicesFound):
			Analytics.Track(user.MXID, "$login_failure", map[string]any{"mode": "google", "error": "no devices"})
		case errors.Is(err, libgm.ErrIncorrectEmoji):
			Analytics.Track(user.MXID, "$login_failure", map[string]any{"mode": "google", "error": "incorrect emoji"})
		case errors.Is(err, libgm.ErrPairingCancelled):
			Analytics.Track(user.MXID, "$login_failure", map[string]any{"mode": "google", "error": "cancelled"})
		case errors.Is(err, libgm.ErrPairingTimeout):
			Analytics.Track(user.MXID, "$login_failure", map[string]any{"mode": "google", "error": "user timeout"})
		case errors.Is(err, libgm.ErrPairingInitTimeout):
			if errors.Is(err, libgm.ErrHadMultipleDevices) {
				user.gaiaHackyDeviceSwitcher++
				Analytics.Track(user.MXID, "$login_failure", map[string]any{"mode": "google", "error": "init timeout (multiple devices)"})
			} else {
				Analytics.Track(user.MXID, "$login_failure", map[string]any{"mode": "google", "error": "init timeout"})
			}
		default:
			Analytics.Track(user.MXID, "$login_failure", map[string]any{"mode": "google", "error": "unknown"})
		}
		return err
	}
	Analytics.Track(user.MXID, "$login_success", map[string]any{"mode": "google"})
	return nil
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
	if user.Session.Mobile.Network == util.GoogleNetwork {
		user.Session.Mobile.SourceID = strings.ToLower(user.Session.Mobile.SourceID)
	}
	user.createClient(user.Session)
	err := user.Client.Connect()
	if err != nil {
		user.zlog.Err(err).Msg("Error connecting to Google Messages")
		if errors.Is(err, events.ErrRequestedEntityNotFound) {
			go user.Logout(status.BridgeState{
				StateEvent: status.StateBadCredentials,
				Error:      GMUnpaired404,
				Info: map[string]any{
					"go_error": err.Error(),
				},
			}, false)
		} else {
			user.BridgeState.Send(status.BridgeState{
				StateEvent: status.StateUnknownError,
				Error:      GMConnectionFailed,
				Info: map[string]interface{}{
					"go_error": err.Error(),
				},
			})
		}
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
	user.phoneResponding = true
	user.batteryLow = false
	user.switchedToGoogleLogin = false
	user.ready = false
	user.browserInactiveType = ""
}

func (user *User) HasSession() bool {
	return user.Session != nil
}

func (user *User) DeleteSession() {
	user.Session = nil
	user.SelfParticipantIDs = []string{}
	user.didHackySetActive = false
	user.noDataReceivedRecently = false
	user.recentlyDisconnected = false
	user.lastDataReceived = time.Time{}
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

func (user *User) sendMarkdownBridgeAlert(ctx context.Context, important bool, formatString string, args ...interface{}) {
	if user.bridge.Config.Bridge.DisableBridgeAlerts {
		return
	}
	notice := fmt.Sprintf(formatString, args...)
	content := format.RenderMarkdown(notice, true, false)
	if !important {
		content.MsgType = event.MsgNotice
	}
	_, err := user.bridge.Bot.SendMessageEvent(ctx, user.GetManagementRoom(ctx), event.EventMessage, content)
	if err != nil {
		user.zlog.Warn().Err(err).Str("notice", notice).Msg("Failed to send bridge alert")
	}
}

func (user *User) hackyResetActive() {
	if user.didHackySetActive {
		return
	}
	user.didHackySetActive = true
	user.noDataReceivedRecently = false
	user.lastDataReceived = time.Time{}
	time.Sleep(7 * time.Second)
	if !user.ready && user.phoneResponding {
		user.zlog.Warn().Msg("Client is still not ready, trying to re-set active session")
		err := user.Client.SetActiveSession()
		if err != nil {
			user.zlog.Err(err).Msg("Failed to re-set active session")
		} else {
			time.Sleep(7 * time.Second)
		}
		if !user.ready && user.phoneResponding {
			user.zlog.Warn().Msg("Client is still not ready, reconnecting")
			user.DeleteConnection()
			user.Connect()
		}
	}
}

func (user *User) syncHandleEvent(event any) {
	ctx := context.TODO()
	switch v := event.(type) {
	case *events.ListenFatalError:
		go user.Logout(status.BridgeState{
			StateEvent: status.StateUnknownError,
			Error:      GMFatalError,
			Info:       map[string]any{"go_error": v.Error.Error()},
		}, false)
		go user.sendMarkdownBridgeAlert(ctx, true, "Fatal error while listening to Google Messages: %v - Log in again to continue using the bridge", v.Error)
	case *events.ListenTemporaryError:
		user.longPollingError = v.Error
		user.BridgeState.Send(status.BridgeState{
			StateEvent: status.StateTransientDisconnect,
			Error:      GMListenError,
			Info:       map[string]any{"go_error": v.Error.Error()},
		})
		if !user.pollErrorAlertSent {
			go user.sendMarkdownBridgeAlert(ctx, false, "Temporary error while listening to Google Messages: %v", v.Error)
			user.pollErrorAlertSent = true
		}
	case *events.ListenRecovered:
		user.longPollingError = nil
		user.BridgeState.Send(status.BridgeState{StateEvent: status.StateConnected})
		if user.pollErrorAlertSent {
			go user.sendMarkdownBridgeAlert(ctx, false, "Reconnected to Google Messages")
			user.pollErrorAlertSent = false
		}
	case *events.PhoneNotResponding:
		user.phoneResponding = false
		user.BridgeState.Send(status.BridgeState{StateEvent: status.StateConnected})
		// TODO make this properly configurable
		if user.zlog.Trace().Enabled() && !user.phoneNotRespondingAlertSent {
			go user.sendMarkdownBridgeAlert(ctx, false, "Phone is not responding")
			user.phoneNotRespondingAlertSent = true
		}
	case *events.PhoneRespondingAgain:
		user.phoneResponding = true
		user.BridgeState.Send(status.BridgeState{StateEvent: status.StateConnected})
		if user.phoneNotRespondingAlertSent {
			go user.sendMarkdownBridgeAlert(ctx, false, "Phone is responding again")
			user.phoneNotRespondingAlertSent = false
		}
	case *events.HackySetActiveMayFail:
		go user.hackyResetActive()
	case *events.PingFailed:
		if errors.Is(v.Error, events.ErrRequestedEntityNotFound) {
			go user.Logout(status.BridgeState{
				StateEvent: status.StateBadCredentials,
				Error:      GMUnpaired404,
				Info: map[string]any{
					"go_error": v.Error.Error(),
				},
			}, false)
		} else if v.ErrorCount > 1 {
			user.BridgeState.Send(status.BridgeState{
				StateEvent: status.StateUnknownError,
				Error:      GMPingFailed,
				Info:       map[string]any{"go_error": v.Error.Error()},
			})
		} else {
			user.zlog.Debug().Msg("Not sending unknown error for first ping fail")
		}
	case *events.PairSuccessful:
		user.Session = user.Client.AuthData
		if user.PhoneID != "" && user.PhoneID != v.PhoneID {
			user.zlog.Warn().
				Str("old_phone_id", user.PhoneID).
				Str("new_phone_id", v.PhoneID).
				Msg("Phone ID changed, resetting state")
			user.ResetState()
		}
		user.PhoneID = v.PhoneID
		err := user.Update(context.TODO())
		if err != nil {
			user.zlog.Err(err).Msg("Failed to update session in database")
		}
		if ch := user.pairSuccessChan; ch != nil {
			close(ch)
		}
		go user.tryAutomaticDoublePuppeting()
	case *gmproto.RevokePairData:
		user.zlog.Info().Any("revoked_device", v.GetRevokedDevice()).Msg("Got pair revoked event")
		go user.Logout(status.BridgeState{
			StateEvent: status.StateBadCredentials,
			Error:      GMUnpaired,
		}, false)
		go user.sendMarkdownBridgeAlert(ctx, true, "Unpaired from Google Messages. Log in again to continue using the bridge.")
	case *events.GaiaLoggedOut:
		user.zlog.Info().Msg("Got gaia logout event")
		go user.Logout(status.BridgeState{
			StateEvent: status.StateBadCredentials,
			Error:      GMUnpaired,
		}, false)
		go user.sendMarkdownBridgeAlert(ctx, true, "Unpaired from Google Messages. Log in again to continue using the bridge.")
	case *events.AuthTokenRefreshed:
		go func() {
			err := user.Update(context.TODO())
			if err != nil {
				user.zlog.Err(err).Msg("Failed to update session in database")
			}
		}()
	case *gmproto.Conversation:
		user.noDataReceivedRecently = false
		user.lastDataReceived = time.Now()
		go user.syncConversation(v, "event")
	//case *gmproto.Message:
	case *libgm.WrappedMessage:
		user.noDataReceivedRecently = false
		user.lastDataReceived = time.Now()
		if v.GetTimestamp() > user.lastDataReceived.UnixMicro() {
			user.lastDataReceived = time.UnixMicro(v.GetTimestamp())
		}
		user.zlog.Debug().
			Str("conversation_id", v.GetConversationID()).
			Str("participant_id", v.GetParticipantID()).
			Str("message_id", v.GetMessageID()).
			Str("message_status", v.GetMessageStatus().GetStatus().String()).
			Int64("message_ts", v.GetTimestamp()).
			Str("tmp_id", v.GetTmpID()).
			Bool("is_old", v.IsOld).
			Msg("Received message")
		portal := user.GetPortalByID(v.GetConversationID())
		portal.messages <- PortalMessage{evt: v.Message, source: user, raw: v.Data}
	case *gmproto.UserAlertEvent:
		user.handleUserAlert(v)
	case *gmproto.Settings:
		// Don't reset last data received until a BROWSER_ACTIVE event if there hasn't been data recently,
		// otherwise the resync won't have the right timestamp.
		if !user.noDataReceivedRecently {
			user.lastDataReceived = time.Now()
		}
		user.handleSettings(v)
	case *events.AccountChange:
		user.handleAccountChange(v)
	case *events.RecentlyDisconnected:
		user.recentlyDisconnected = true
	case *events.NoDataReceived:
		user.noDataReceivedRecently = true
	default:
		user.zlog.Trace().Any("data", v).Type("data_type", v).Msg("Unknown event")
	}
}

func (user *User) ResetState() {
	ctx := context.TODO()
	portals := user.bridge.GetAllPortalsForUser(user.RowID)
	user.zlog.Debug().Int("portal_count", len(portals)).Msg("Deleting portals")
	for _, portal := range portals {
		portal.Delete(ctx)
	}
	err := user.bridge.DB.Puppet.Reset(ctx, user.RowID)
	if err != nil {
		user.zlog.Err(err).Msg("Failed to reset puppet state")
	}
	user.PhoneID = ""
	go func() {
		user.zlog.Debug().Msg("Cleaning up portal rooms in background")
		for _, portal := range portals {
			portal.Cleanup(context.TODO())
		}
		user.zlog.Debug().Msg("Finished cleaning up portals")
	}()
}

func (user *User) aggressiveSetActive() {
	sleepTimes := []int{5, 10, 30}
	for i := 0; i < 3; i++ {
		sleep := time.Duration(sleepTimes[i]) * time.Second
		user.zlog.Info().
			Int("sleep_seconds", int(sleep.Seconds())).
			Msg("Aggressively reactivating bridge session after sleep")
		time.Sleep(sleep)
		if user.browserInactiveType == "" {
			user.zlog.Info().Msg("Bridge session became active on its own, not reactivating")
			return
		}
		user.zlog.Info().Msg("Now reactivating bridge session")
		err := user.Client.SetActiveSession()
		if err != nil {
			user.zlog.Warn().Err(err).Msg("Failed to set self as active session")
		} else {
			break
		}
	}
}

func (user *User) fetchAndSyncConversations(lastDataReceived time.Time, minimalSync bool) {
	user.zlog.Info().Msg("Fetching conversation list")
	resp, err := user.Client.ListConversations(user.bridge.Config.Bridge.InitialChatSyncCount, gmproto.ListConversationsRequest_INBOX)
	if err != nil {
		user.zlog.Err(err).Msg("Failed to get conversation list")
		return
	}
	user.zlog.Info().Int("count", len(resp.GetConversations())).Msg("Syncing conversations")
	if !lastDataReceived.IsZero() {
		for _, conv := range resp.GetConversations() {
			lastMessageTS := time.UnixMicro(conv.GetLastMessageTimestamp())
			if lastMessageTS.After(lastDataReceived) {
				user.zlog.Warn().
					Time("last_message_ts", lastMessageTS).
					Time("last_data_received", lastDataReceived).
					Msg("Conversation's last message is newer than last data received time")
				minimalSync = false
			}
		}
	} else if minimalSync {
		user.zlog.Warn().Msg("Minimal sync called without last data received time")
	}
	if minimalSync {
		user.zlog.Debug().Msg("Minimal sync with no recent messages, not syncing conversations")
		return
	}
	for _, conv := range resp.GetConversations() {
		user.syncConversation(conv, "sync")
	}
}

func (user *User) handleAccountChange(v *events.AccountChange) {
	user.zlog.Debug().
		Str("account", v.GetAccount()).
		Bool("enabled", v.GetEnabled()).
		Bool("fake", v.IsFake).
		Msg("Got account change event")
	user.switchedToGoogleLogin = v.GetEnabled() || v.IsFake
	ctx := context.TODO()
	if !v.IsFake {
		if user.switchedToGoogleLogin {
			go user.sendMarkdownBridgeAlert(ctx, true, "Switched to Google account pairing, please switch back or relogin with `login-google`.")
		} else {
			go user.sendMarkdownBridgeAlert(ctx, false, "Switched back to QR pairing, bridge should be reconnected")
			// Assume connection is ready now even if it wasn't before
			user.ready = true
		}
	}
	user.BridgeState.Send(status.BridgeState{StateEvent: status.StateConnected})
}

func (user *User) handleUserAlert(v *gmproto.UserAlertEvent) {
	ctx := context.TODO()
	user.zlog.Debug().Str("alert_type", v.GetAlertType().String()).Msg("Got user alert event")
	becameInactive := false
	// Don't reset last data received until a BROWSER_ACTIVE event if there hasn't been data recently,
	// otherwise the resync won't have the right timestamp.
	if !user.noDataReceivedRecently {
		user.lastDataReceived = time.Now()
	}
	switch v.GetAlertType() {
	case gmproto.AlertType_BROWSER_INACTIVE:
		user.browserInactiveType = GMBrowserInactive
		becameInactive = true
	case gmproto.AlertType_BROWSER_ACTIVE:
		wasInactive := user.browserInactiveType != "" || !user.ready
		user.pollErrorAlertSent = false
		user.browserInactiveType = ""
		user.ready = true
		newSessionID := user.Client.CurrentSessionID()
		sessionIDChanged := user.sessionID != newSessionID
		if sessionIDChanged || wasInactive || user.noDataReceivedRecently || user.recentlyDisconnected {
			user.zlog.Debug().
				Str("old_session_id", user.sessionID).
				Str("new_session_id", newSessionID).
				Bool("was_inactive", wasInactive).
				Bool("had_no_data_received", user.noDataReceivedRecently).
				Bool("recently_disconeccted", user.recentlyDisconnected).
				Time("last_data_received", user.lastDataReceived).
				Msg("Session ID changed for browser active event, resyncing")
			user.sessionID = newSessionID
			go user.fetchAndSyncConversations(user.lastDataReceived, !sessionIDChanged && !wasInactive)
			if (!user.DisableNotifyVerbose || wasInactive || user.recentlyDisconnected) {
				go user.sendMarkdownBridgeAlert(ctx, false, "Connected to Google Messages")
			}
		} else {
			user.zlog.Debug().
				Str("session_id", user.sessionID).
				Bool("was_inactive", wasInactive).
				Bool("had_no_data_received", user.noDataReceivedRecently).
				Time("last_data_received", user.lastDataReceived).
				Msg("Session ID didn't change for browser active event, not resyncing")
		}
		user.noDataReceivedRecently = false
		user.recentlyDisconnected = false
		user.lastDataReceived = time.Now()
	case gmproto.AlertType_BROWSER_INACTIVE_FROM_TIMEOUT:
		user.browserInactiveType = GMBrowserInactiveTimeout
		becameInactive = true
	case gmproto.AlertType_BROWSER_INACTIVE_FROM_INACTIVITY:
		user.browserInactiveType = GMBrowserInactiveInactivity
		becameInactive = true
	case gmproto.AlertType_MOBILE_DATA_CONNECTION:
		user.mobileData = true
	case gmproto.AlertType_MOBILE_WIFI_CONNECTION:
		user.mobileData = false
	case gmproto.AlertType_MOBILE_BATTERY_LOW:
		user.batteryLow = true
		if time.Since(user.batteryLowAlertSent) > 30*time.Minute {
			if (!user.DisableNotifyBattery) {
				go user.sendMarkdownBridgeAlert(ctx, true, "Your phone's battery is low")
			}
			user.batteryLowAlertSent = time.Now()
		}
	case gmproto.AlertType_MOBILE_BATTERY_RESTORED:
		user.batteryLow = false
		if !user.batteryLowAlertSent.IsZero() {
			if (!user.DisableNotifyBattery) {
				go user.sendMarkdownBridgeAlert(ctx, false, "Phone battery restored")
			}
			user.batteryLowAlertSent = time.Time{}
		}
	default:
		return
	}
	if becameInactive {
		if user.bridge.Config.GoogleMessages.AggressiveReconnect {
			go user.aggressiveSetActive()
		} else {
			go user.sendMarkdownBridgeAlert(ctx, true, "Google Messages was opened in another browser. Use `set-active` to reconnect the bridge.")
		}
	}
	user.BridgeState.Send(status.BridgeState{StateEvent: status.StateConnected})
}

func (user *User) toggleNotifyBattery() {
	user.DisableNotifyBattery = !user.DisableNotifyBattery
	err := user.Update(context.TODO())
	if err != nil {
		user.zlog.Err(err).Msg("Failed to save notify battery preference")
	}
}

func (user *User) toggleNotifyVerbose() {
	user.DisableNotifyVerbose = !user.DisableNotifyVerbose
	err := user.Update(context.TODO())
	if err != nil {
		user.zlog.Err(err).Msg("Failed to save notify verbose preference")
	}
}

func (user *User) handleSettings(settings *gmproto.Settings) {
	if settings.SIMCards == nil {
		return
	}
	ctx := context.TODO()
	changed := user.SetSIMs(settings.SIMCards)
	newRCSSettings := settings.GetRCSSettings()
	if user.Settings.RCSEnabled != newRCSSettings.GetIsEnabled() ||
		user.Settings.ReadReceipts != newRCSSettings.GetSendReadReceipts() ||
		user.Settings.TypingNotifications != newRCSSettings.GetShowTypingIndicators() ||
		user.Settings.IsDefaultSMSApp != newRCSSettings.GetIsDefaultSMSApp() ||
		!user.Settings.SettingsReceived {
		user.Settings = database.Settings{
			SettingsReceived:    true,
			RCSEnabled:          newRCSSettings.GetIsEnabled(),
			ReadReceipts:        newRCSSettings.GetSendReadReceipts(),
			TypingNotifications: newRCSSettings.GetShowTypingIndicators(),
			IsDefaultSMSApp:     newRCSSettings.GetIsDefaultSMSApp(),
		}
		changed = true
	}
	if changed {
		err := user.Update(ctx)
		if err != nil {
			user.zlog.Err(err).Msg("Failed to save SIM details")
		}
		user.BridgeState.Send(status.BridgeState{StateEvent: status.StateConnected})
	}
}

func (user *User) FillBridgeState(state status.BridgeState) status.BridgeState {
	if state.Info == nil {
		state.Info = make(map[string]any)
	}
	if state.StateEvent == status.StateConnected {
		state.Info["sims"] = user.GetSIMsForBridgeState()
		state.Info["settings"] = user.Settings
		state.Info["battery_low"] = user.batteryLow
		state.Info["mobile_data"] = user.mobileData
		state.Info["browser_active"] = user.browserInactiveType == ""
		state.Info["google_account_pairing"] = user.switchedToGoogleLogin
		if !user.ready {
			state.StateEvent = status.StateConnecting
			state.Error = GMConnecting
		}
		if !user.phoneResponding {
			state.StateEvent = status.StateBadCredentials
			state.Error = GMPhoneNotResponding
		}
		if user.switchedToGoogleLogin {
			state.StateEvent = status.StateBadCredentials
			state.Error = GMSwitchedToGoogleLogin
		}
		if user.longPollingError != nil {
			state.StateEvent = status.StateTransientDisconnect
			state.Error = GMListenError
			state.Info["go_error"] = user.longPollingError.Error()
		}
		if user.browserInactiveType != "" {
			if user.bridge.Config.GoogleMessages.AggressiveReconnect {
				state.StateEvent = status.StateTransientDisconnect
			} else {
				state.StateEvent = status.StateBadCredentials
			}
			state.Error = user.browserInactiveType
		}
	}
	return state
}

func (user *User) Logout(state status.BridgeState, unpair bool) (logoutOK bool) {
	if user.Client != nil && unpair {
		err := user.Client.Unpair()
		if err != nil {
			user.zlog.Debug().Err(err).Msg("Error sending unpair request")
		} else {
			logoutOK = true
		}
	}
	user.DeleteConnection()
	user.DeleteSession()
	user.BridgeState.Send(state)
	return
}

func (user *User) syncConversation(v *gmproto.Conversation, source string) {
	updateType := v.GetStatus()
	portal := user.GetPortalByID(v.GetConversationID())
	convCopy := proto.Clone(v).(*gmproto.Conversation)
	convCopy.LatestMessage = nil
	log := portal.zlog.With().
		Str("action", "sync conversation").
		Str("conversation_status", updateType.String()).
		Str("data_source", source).
		Logger()
	log.Debug().Any("conversation_data", convCopy).Msg("Got conversation update")
	ctx := log.WithContext(context.TODO())
	if updateType == gmproto.ConversationStatus_SPAM_FOLDER || updateType == gmproto.ConversationStatus_BLOCKED_FOLDER {
		portal.markedSpamAt = time.Now()
	}
	if cancel := portal.cancelCreation.Load(); cancel != nil {
		if updateType == gmproto.ConversationStatus_SPAM_FOLDER || updateType == gmproto.ConversationStatus_BLOCKED_FOLDER {
			(*cancel)(fmt.Errorf("conversation was moved to spam"))
		} else if updateType == gmproto.ConversationStatus_DELETED {
			(*cancel)(fmt.Errorf("conversation was deleted"))
			portal.Delete(ctx)
		} else {
			log.Debug().Msg("Conversation creation is still pending, ignoring new sync event")
			return
		}
	}
	if portal.MXID != "" {
		switch updateType {
		case gmproto.ConversationStatus_DELETED:
			log.Info().Msg("Got delete event, cleaning up portal")
			portal.Delete(ctx)
			portal.Cleanup(ctx)
		case gmproto.ConversationStatus_SPAM_FOLDER, gmproto.ConversationStatus_BLOCKED_FOLDER:
			log.Info().Msg("Got spam/block event, cleaning up portal")
			portal.Cleanup(ctx)
			portal.RemoveMXID(context.TODO())
		default:
			if v.Participants == nil {
				log.Debug().Msg("Not syncing conversation with nil participants")
				return
			}
			log.Debug().Msg("Syncing existing portal")
			portal.UpdateMetadata(ctx, user, v)
			user.syncChatDoublePuppetDetails(ctx, portal, v, false)
			go portal.missedForwardBackfill(
				ctx,
				user,
				time.UnixMicro(v.LastMessageTimestamp),
				v.LatestMessageID,
				!v.GetUnread(),
				source == "event",
			)
		}
	} else if updateType == gmproto.ConversationStatus_ACTIVE || updateType == gmproto.ConversationStatus_ARCHIVED {
		if v.Participants == nil {
			log.Debug().Msg("Not syncing conversation with nil participants")
			return
		} else if time.Since(portal.markedSpamAt) < 1*time.Minute {
			log.Warn().Msg("Dropping conversation update due to suspected race condition")
			return
		}
		if source == "event" {
			go func() {
				ctx, cancel := context.WithCancelCause(context.TODO())
				cancelPtr := &cancel
				defer func() {
					portal.cancelCreation.CompareAndSwap(cancelPtr, nil)
					cancel(nil)
				}()
				portal.cancelCreation.Store(cancelPtr)
				log.Debug().Msg("Creating portal for conversation in 5 seconds")
				select {
				case <-time.After(5 * time.Second):
				case <-ctx.Done():
					log.Debug().Err(ctx.Err()).Msg("Portal creation was cancelled")
					return
				}
				err := portal.CreateMatrixRoom(ctx, user, v, source == "sync")
				if err != nil {
					log.Err(err).Msg("Error creating Matrix room from conversation event")
				}
			}()
		} else {
			log.Debug().Msg("Creating portal for conversation")
			err := portal.CreateMatrixRoom(ctx, user, v, source == "sync")
			if err != nil {
				log.Err(err).Msg("Error creating Matrix room from conversation event")
			}
		}
	} else {
		log.Debug().Msg("Not creating portal for conversation")
	}
}

type CustomTagData struct {
	Order        json.Number `json:"order"`
	DoublePuppet string      `json:"fi.mau.double_puppet_source"`
}

type CustomTagEventContent struct {
	Tags map[string]CustomTagData `json:"tags"`
}

func (user *User) updateChatTag(ctx context.Context, portal *Portal, tag string, active bool, existingTags CustomTagEventContent) {
	if tag == "" {
		return
	}
	var err error
	currentTag, ok := existingTags.Tags[tag]
	if active && !ok {
		user.zlog.Debug().Str("tag", tag).Str("room_id", portal.MXID.String()).Msg("Adding room tag")
		data := CustomTagData{Order: "0.5", DoublePuppet: user.bridge.Name}
		err = user.DoublePuppetIntent.AddTagWithCustomData(ctx, portal.MXID, tag, &data)
	} else if !active && ok && currentTag.DoublePuppet == user.bridge.Name {
		user.zlog.Debug().Str("tag", tag).Str("room_id", portal.MXID.String()).Msg("Removing room tag")
		err = user.DoublePuppetIntent.RemoveTag(ctx, portal.MXID, tag)
	} else {
		err = nil
	}
	if err != nil {
		user.zlog.Warn().Err(err).Str("room_id", portal.MXID.String()).Msg("Failed to update room tag")
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

func (user *User) markSelfReadFull(ctx context.Context, portal *Portal, lastMessageID string) {
	if user.DoublePuppetIntent == nil || portal.lastUserReadID == lastMessageID {
		return
	}
	lastMessage, err := user.bridge.DB.Message.GetByID(ctx, portal.Receiver, lastMessageID)
	if err == nil && (lastMessage == nil || lastMessage.IsFakeMXID()) {
		lastMessage, err = user.bridge.DB.Message.GetLastInChatWithMXID(ctx, portal.Key)
		if lastMessage != nil && idToInt(lastMessage.ID) > idToInt(lastMessageID) {
			return
		}
	}
	if err != nil {
		user.zlog.Warn().Err(err).Msg("Failed to get last message in chat to mark it as read")
		return
	} else if lastMessage == nil || portal.lastUserReadID == lastMessage.ID {
		return
	}
	log := user.zlog.With().
		Str("conversation_id", portal.ID).
		Str("message_id", lastMessage.ID).
		Str("room_id", portal.ID).
		Str("event_id", lastMessage.MXID.String()).
		Logger()
	err = user.DoublePuppetIntent.SetReadMarkers(ctx, portal.MXID, &CustomReadMarkers{
		ReqSetReadMarkers: mautrix.ReqSetReadMarkers{
			Read:      lastMessage.MXID,
			FullyRead: lastMessage.MXID,
		},
		ReadExtra:      CustomReadReceipt{DoublePuppetSource: user.bridge.Name},
		FullyReadExtra: CustomReadReceipt{DoublePuppetSource: user.bridge.Name},
	})
	if err != nil {
		log.Warn().Err(err).Msg("Failed to mark last message in chat as read")
	} else {
		log.Debug().Msg("Marked last message in chat as read")
		portal.lastUserReadID = lastMessage.ID
	}
}

func (user *User) syncChatDoublePuppetDetails(ctx context.Context, portal *Portal, conv *gmproto.Conversation, justCreated bool) {
	if user.DoublePuppetIntent == nil || len(portal.MXID) == 0 {
		return
	}
	if justCreated || !user.bridge.Config.Bridge.TagOnlyOnCreate {
		var existingTags CustomTagEventContent
		err := user.DoublePuppetIntent.GetTagsWithCustomData(ctx, portal.MXID, &existingTags)
		if err != nil && !errors.Is(err, mautrix.MNotFound) {
			user.zlog.Warn().Err(err).Str("room_id", portal.MXID.String()).Msg("Failed to get existing room tags")
		}
		user.updateChatTag(ctx, portal, user.bridge.Config.Bridge.ArchiveTag, conv.Status == gmproto.ConversationStatus_ARCHIVED || conv.Status == gmproto.ConversationStatus_KEEP_ARCHIVED, existingTags)
		user.updateChatTag(ctx, portal, user.bridge.Config.Bridge.PinnedTag, conv.Pinned, existingTags)
	}
}

func (user *User) UpdateDirectChats(ctx context.Context, chats map[id.UserID][]id.RoomID) {
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
		_, err = intent.MakeFullRequest(ctx, mautrix.FullRequest{
			Method:      method,
			URL:         urlPath,
			Headers:     http.Header{"X-Asmux-Auth": {user.bridge.AS.Registration.AppToken}},
			RequestJSON: chats,
		})
	} else {
		existingChats := make(map[id.UserID][]id.RoomID)
		err = intent.GetAccountData(ctx, event.AccountDataDirectChats.Type, &existingChats)
		if err != nil {
			user.zlog.Err(err).Msg("Failed to get m.direct list to update it")
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
		err = intent.SetAccountData(ctx, event.AccountDataDirectChats.Type, &chats)
	}
	if err != nil {
		user.zlog.Err(err).Msg("Failed to update m.direct list")
	}
}

func (user *User) markUnread(ctx context.Context, portal *Portal, unread bool) {
	if user.DoublePuppetIntent == nil {
		return
	}

	log := user.zlog.With().Str("room_id", portal.MXID.String()).Logger()

	err := user.DoublePuppetIntent.SetRoomAccountData(ctx, portal.MXID, "m.marked_unread", map[string]bool{"unread": unread})
	if err != nil {
		log.Warn().Err(err).Str("event_type", "m.marked_unread").
			Msg("Failed to mark room as unread")
	} else {
		log.Debug().Str("event_type", "m.marked_unread").Msg("Marked room as unread")
	}

	err = user.DoublePuppetIntent.SetRoomAccountData(ctx, portal.MXID, "com.famedly.marked_unread", map[string]bool{"unread": unread})
	if err != nil {
		log.Warn().Err(err).Str("event_type", "com.famedly.marked_unread").
			Msg("Failed to mark room as unread")
	} else {
		log.Debug().Str("event_type", "com.famedly.marked_unread").Msg("Marked room as unread")
	}
}
