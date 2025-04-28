// mautrix-gmessages - A Matrix-Google Messages puppeting bridge.
// Copyright (C) 2025 Tulir Asokan
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
	"fmt"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
)

var (
	_ bridgev2.PushableNetworkAPI          = (*GMClient)(nil)
	_ bridgev2.BackgroundSyncingNetworkAPI = (*GMClient)(nil)
)

func (gc *GMClient) RegisterPushNotifications(ctx context.Context, pushType bridgev2.PushType, token string) error {
	if pushType != bridgev2.PushTypeWeb {
		return fmt.Errorf("unsupported push type")
	}
	if gc.Meta.PushKeys == nil {
		gc.Meta.GeneratePushKeys()
	}
	needsUpdate := gc.Meta.PushKeys.Token != token
	gc.Meta.PushKeys.Token = token
	err := gc.Client.RegisterPush(gc.Meta.PublicPushKeys())
	if err != nil {
		gc.Meta.PushKeys.Token = ""
		return err
	}
	if needsUpdate {
		err = gc.UserLogin.Save(ctx)
		if err != nil {
			return fmt.Errorf("failed to save user login after updating push keys: %w", err)
		}
	}
	return nil
}

var pushCfg = &bridgev2.PushConfig{
	Web: &bridgev2.WebPushConfig{
		VapidKey: "BKXiqRFb-3xiLFDOB8MjGiShfSfD2mf5TEeOtL9FLiI3gGxxm5LDb4pOmrsv4cY_6n4TD_GQ67uCtblfErqu9d0",
	},
}

func (gc *GMClient) GetPushConfigs() *bridgev2.PushConfig {
	return pushCfg
}

func (gc *GMClient) ConnectBackground(ctx context.Context, params *bridgev2.ConnectBackgroundParams) error {
	if gc.Client == nil {
		zerolog.Ctx(ctx).Warn().Msg("No client for ConnectBackground")
		return nil
	} else if gc.Meta.Session.IsGoogleAccount() && !gc.Meta.Session.HasCookies() {
		zerolog.Ctx(ctx).Warn().Msg("No cookies for Google account in ConnectBackground")
		return nil
	}
	err := gc.Client.ConnectBackground()
	if err != nil {
		zerolog.Ctx(ctx).Err(err).Msg("Error in ConnectBackground")
	}
	return nil
}
