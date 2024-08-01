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
	"maunium.net/go/mautrix/bridge/status"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"

	"go.mau.fi/mautrix-gmessages/libgm"
	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
)

type conversationMeta struct {
	markedSpamAt          time.Time
	cancelPendingBackfill atomic.Pointer[context.CancelFunc]
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
		phoneResponding:   true,
		fullMediaRequests: exsync.NewSet[fullMediaRequestKey](),
		conversationMeta:  make(map[string]*conversationMeta),
	}
	gcli.NewClient()
	login.Client = gcli
	return nil
}

func (gc *GMClient) Connect(ctx context.Context) error {
	if gc.Client == nil {
		gc.UserLogin.BridgeState.Send(status.BridgeState{
			StateEvent: status.StateBadCredentials,
			Error:      GMNotLoggedIn,
		})
		return nil
	}
	return gc.Client.Connect()
}

func (gc *GMClient) Disconnect() {
	gc.longPollingError = errors.New("not connected")
	gc.phoneResponding = true
	gc.batteryLow = false
	gc.switchedToGoogleLogin = false
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
	return gc.Client != nil
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
