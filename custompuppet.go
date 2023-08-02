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
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/appservice"
	"maunium.net/go/mautrix/bridge"
	"maunium.net/go/mautrix/id"
)

var (
	ErrMismatchingMXID = errors.New("whoami result does not match custom mxid")
)

var _ bridge.DoublePuppet = (*User)(nil)

func (user *User) SwitchCustomMXID(accessToken string, mxid id.UserID) error {
	if mxid != user.MXID {
		return errors.New("mismatching mxid")
	}
	user.DoublePuppetIntent = nil
	user.AccessToken = accessToken
	err := user.startCustomMXID(false)
	if err != nil {
		return err
	}
	err = user.Update(context.TODO())
	if err != nil {
		return fmt.Errorf("failed to save access token to database: %w", err)
	}
	return nil
}

func (user *User) CustomIntent() *appservice.IntentAPI {
	return user.DoublePuppetIntent
}

func (user *User) loginWithSharedSecret() error {
	_, homeserver, _ := user.MXID.Parse()
	user.zlog.Debug().Msg("Logging into double puppet with shared secret")
	loginSecret := user.bridge.Config.Bridge.LoginSharedSecretMap[homeserver]
	client, err := user.bridge.newDoublePuppetClient(user.MXID, "")
	if err != nil {
		return err
	}
	req := mautrix.ReqLogin{
		Identifier:               mautrix.UserIdentifier{Type: mautrix.IdentifierTypeUser, User: string(user.MXID)},
		DeviceID:                 "Google Messages Bridge",
		InitialDeviceDisplayName: "Google Messages Bridge",
	}
	if loginSecret == "appservice" {
		client.AccessToken = user.bridge.AS.Registration.AppToken
		req.Type = mautrix.AuthTypeAppservice
	} else {
		mac := hmac.New(sha512.New, []byte(loginSecret))
		mac.Write([]byte(user.MXID))
		req.Password = hex.EncodeToString(mac.Sum(nil))
		req.Type = mautrix.AuthTypePassword
	}
	resp, err := client.Login(&req)
	if err != nil {
		return fmt.Errorf("failed to log in with shared secret: %w", err)
	}
	user.AccessToken = resp.AccessToken
	err = user.Update(context.TODO())
	if err != nil {
		return fmt.Errorf("failed to save access token: %w", err)
	}
	return nil
}

func (br *GMBridge) newDoublePuppetClient(mxid id.UserID, accessToken string) (*mautrix.Client, error) {
	_, homeserver, err := mxid.Parse()
	if err != nil {
		return nil, err
	}
	homeserverURL, found := br.Config.Bridge.DoublePuppetServerMap[homeserver]
	if !found {
		if homeserver == br.AS.HomeserverDomain {
			homeserverURL = ""
		} else if br.Config.Bridge.DoublePuppetAllowDiscovery {
			resp, err := mautrix.DiscoverClientAPI(homeserver)
			if err != nil {
				return nil, fmt.Errorf("failed to find homeserver URL for %s: %v", homeserver, err)
			}
			homeserverURL = resp.Homeserver.BaseURL
			br.ZLog.Debug().
				Str("server_name", homeserver).
				Str("base_url", homeserverURL).
				Str("user_id", mxid.String()).
				Msg("Discovered homeserver URL to enable double puppeting for external user")
		} else {
			return nil, fmt.Errorf("double puppeting from %s is not allowed", homeserver)
		}
	}
	return br.AS.NewExternalMautrixClient(mxid, accessToken, homeserverURL)
}

func (user *User) newDoublePuppetIntent() (*appservice.IntentAPI, error) {
	client, err := user.bridge.newDoublePuppetClient(user.MXID, user.AccessToken)
	if err != nil {
		return nil, err
	}

	ia := user.bridge.AS.NewIntentAPI("custom")
	ia.Client = client
	ia.Localpart, _, _ = user.MXID.Parse()
	ia.UserID = user.MXID
	ia.IsCustomPuppet = true
	return ia, nil
}

func (user *User) clearCustomMXID() {
	user.AccessToken = ""
	user.DoublePuppetIntent = nil
}

func (user *User) startCustomMXID(reloginOnFail bool) error {
	if len(user.AccessToken) == 0 || user.DoublePuppetIntent != nil {
		return nil
	}
	intent, err := user.newDoublePuppetIntent()
	if err != nil {
		user.clearCustomMXID()
		return fmt.Errorf("failed to create double puppet intent: %w", err)
	}
	resp, err := intent.Whoami()
	if err != nil {
		if !reloginOnFail || (errors.Is(err, mautrix.MUnknownToken) && !user.tryRelogin(err)) {
			user.clearCustomMXID()
			return fmt.Errorf("failed to ensure double puppet token is valid: %w", err)
		}
		intent.AccessToken = user.AccessToken
	}
	if resp.UserID != user.MXID {
		user.clearCustomMXID()
		return ErrMismatchingMXID
	}
	user.DoublePuppetIntent = intent
	return nil
}

func (user *User) tryRelogin(err error) bool {
	if !user.bridge.Config.CanAutoDoublePuppet(user.MXID) {
		return false
	}
	user.zlog.Debug().Err(err).Msg("Trying to relogin after error in double puppet")
	err = user.loginWithSharedSecret()
	if err != nil {
		user.zlog.Err(err).Msg("Failed to relogin after error in double puppet")
		return false
	}
	user.zlog.Info().Msg("Successfully relogined after error in double puppet")
	return true
}
