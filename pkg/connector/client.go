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
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/util/exsync"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/status"

	"go.mau.fi/mautrix-gmessages/pkg/libgm"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/events"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

type conversationMeta struct {
	markedSpamAt          time.Time
	cancelPendingBackfill atomic.Pointer[context.CancelFunc]
	unread                bool
	readUpTo              string
	readUpToTS            time.Time
}

type GMClient struct {
	Main      *GMConnector
	UserLogin *bridgev2.UserLogin
	Client    *libgm.Client
	Meta      *UserLoginMetadata

	fullMediaRequests *exsync.Set[fullMediaRequestKey]

	longPollingError            error
	browserInactiveType         status.BridgeStateErrorCode
	SwitchedToGoogleLogin       bool
	batteryLow                  bool
	mobileData                  bool
	PhoneResponding             bool
	ready                       bool
	sessionID                   string
	batteryLowAlertSent         time.Time
	pollErrorAlertSent          bool
	phoneNotRespondingAlertSent bool
	didHackySetActive           bool
	noDataReceivedRecently      bool
	lastDataReceived            time.Time

	conversationMeta     map[string]*conversationMeta
	conversationMetaLock sync.Mutex
}

var _ bridgev2.NetworkAPI = &GMClient{}

func (gc *GMConnector) LoadUserLogin(ctx context.Context, login *bridgev2.UserLogin) error {
	gcli := &GMClient{
		Main:      gc,
		UserLogin: login,
		Meta:      login.Metadata.(*UserLoginMetadata),

		longPollingError:  errors.New("not connected"),
		PhoneResponding:   true,
		fullMediaRequests: exsync.NewSet[fullMediaRequestKey](),
		conversationMeta:  make(map[string]*conversationMeta),
	}
	gcli.NewClient()
	login.Client = gcli
	return nil
}

func (gc *GMClient) Connect(ctx context.Context) {
	if gc.Client == nil {
		gc.UserLogin.BridgeState.Send(status.BridgeState{
			StateEvent: status.StateBadCredentials,
			Error:      GMNotLoggedIn,
		})
		return
	} else if gc.Meta.Session.IsGoogleAccount() && !gc.Meta.Session.HasCookies() {
		gc.UserLogin.BridgeState.Send(status.BridgeState{
			StateEvent: status.StateBadCredentials,
			Error:      GMNotLoggedInCanReauth,
		})
		return
	}
	err := gc.Client.FetchConfig(ctx)
	if err != nil {
		zerolog.Ctx(ctx).Err(err).Msg("Failed to fetch config")
		/*gc.UserLogin.BridgeState.Send(status.BridgeState{
			StateEvent: status.StateUnknownError,
			Error:      GMConfigFetchFailed,
			Info: map[string]any{
				"go_error": err.Error(),
			},
		})
		return*/
	} else if gc.Meta.Session.IsGoogleAccount() && gc.Client.Config.GetDeviceInfo().GetEmail() == "" {
		zerolog.Ctx(ctx).Error().Msg("No email in config, invalidating session")
		go gc.invalidateSession(ctx, status.BridgeState{
			StateEvent: status.StateBadCredentials,
			Error:      GMLoggedOutNoEmailInConfig,
		}, false)
		return
	}
	err = gc.Client.Connect()
	if err != nil {
		if errors.Is(err, events.ErrRequestedEntityNotFound) {
			go gc.invalidateSession(ctx, status.BridgeState{
				StateEvent: status.StateBadCredentials,
				Error:      GMUnpaired404,
				Info: map[string]any{
					"go_error": err.Error(),
				},
			}, true)
		} else if errors.Is(err, events.ErrInvalidCredentials) {
			go gc.invalidateSession(ctx, status.BridgeState{
				StateEvent: status.StateBadCredentials,
				Error:      GMLoggedOutInvalidCreds,
				Info: map[string]any{
					"go_error": err.Error(),
				},
			}, false)
		} else {
			gc.UserLogin.BridgeState.Send(status.BridgeState{
				StateEvent: status.StateUnknownError,
				Error:      GMConnectionFailed,
				Info: map[string]any{
					"go_error": err.Error(),
				},
			})
		}
	}
}

func (gc *GMClient) Disconnect() {
	gc.longPollingError = errors.New("not connected")
	gc.PhoneResponding = true
	gc.batteryLow = false
	gc.SwitchedToGoogleLogin = false
	gc.ready = false
	gc.browserInactiveType = ""
	if cli := gc.Client; cli != nil {
		cli.Disconnect()
	}
}

func (gc *GMClient) ResetClient() {
	gc.Disconnect()
	if cli := gc.Client; cli != nil {
		cli.SetEventHandler(nil)
		gc.Client = nil
	}
	gc.NewClient()
}

func (gc *GMClient) NewClient() {
	sess := gc.Meta.Session
	if sess != nil {
		gc.Client = libgm.NewClient(sess, gc.UserLogin.Log.With().Str("component", "libgm").Logger())
		gc.Client.SetEventHandler(gc.handleGMEvent)
	}
}

func (gc *GMClient) IsLoggedIn() bool {
	return gc.Client.IsLoggedIn()
}

func (gc *GMClient) LogoutRemote(ctx context.Context) {
	if cli := gc.Client; cli != nil {
		err := cli.Unpair()
		if err != nil {
			zerolog.Ctx(ctx).Err(err).Msg("Failed to send unpair request")
		}
	}
	gc.Disconnect()
	gc.Meta.Session = nil
	gc.Client = nil
}

func (gc *GMClient) IsThisUser(ctx context.Context, userID networkid.UserID) bool {
	participantID, err := gc.ParseUserID(userID)
	return err == nil && (participantID == "1" || gc.Meta.IsSelfParticipantID(participantID))
}

func (gc *GMClient) GetSIM(portal *bridgev2.Portal) *gmproto.SIMCard {
	return gc.Meta.GetSIM(portal.Metadata.(*PortalMetadata).OutgoingID)
}
