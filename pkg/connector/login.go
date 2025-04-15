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
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"

	"go.mau.fi/mautrix-gmessages/pkg/libgm"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

const (
	LoginFlowIDGoogle = "google"
	LoginFlowIDQR     = "qr"
)

const (
	LoginStepIDQR       = "fi.mau.gmessages.qr"
	LoginStepIDGoogle   = "fi.mau.gmessages.google_account"
	LoginStepIDEmoji    = "fi.mau.gmessages.emoji"
	LoginStepIDComplete = "fi.mau.gmessages.complete"
)

const (
	pairingErrMsgNoDevices        = "No devices found. Make sure you've enabled account pairing in the Google Messages app on your phone."
	pairingErrPhoneNotResponding  = "Phone not responding. Make sure your phone is connected to the internet and that account pairing is enabled in the Google Messages app. You may need to keep the app open and/or disable battery optimizations. Alternatively, try QR pairing"
	pairingErrMsgIncorrectEmoji   = "Incorrect emoji chosen on phone, please try again"
	pairingErrMsgCancelled        = "Pairing cancelled on phone"
	pairingErrMsgTimeout          = "Pairing timed out, please try again"
	pairingErrMsgQRTimeout        = "Scanning QR code timed out, please try again"
	pairingErrMsgStartUnknown     = "Failed to start login"
	pairingErrMsgWaitUnknown      = "Failed to finish login"
	pairingErrMsgQRRefreshUnknown = "Failed to refresh QR code"
)

var (
	ErrPairNoDevices          = bridgev2.RespError{Err: pairingErrMsgNoDevices, ErrCode: "FI.MAU.GMESSAGES.PAIR_NO_DEVICES", StatusCode: http.StatusBadRequest}
	ErrPairPhoneNotResponding = bridgev2.RespError{Err: pairingErrPhoneNotResponding, ErrCode: "FI.MAU.GMESSAGES.PAIR_INIT_TIMEOUT", StatusCode: http.StatusBadRequest}
	ErrPairIncorrectEmoji     = bridgev2.RespError{Err: pairingErrMsgIncorrectEmoji, ErrCode: "FI.MAU.GMESSAGES.PAIR_INCORRECT_EMOJI", StatusCode: http.StatusBadRequest}
	ErrPairCancelled          = bridgev2.RespError{Err: pairingErrMsgCancelled, ErrCode: "FI.MAU.GMESSAGES.PAIR_CANCELLED", StatusCode: http.StatusBadRequest}
	ErrPairTimeout            = bridgev2.RespError{Err: pairingErrMsgTimeout, ErrCode: "FI.MAU.GMESSAGES.PAIR_TIMEOUT", StatusCode: http.StatusBadRequest}
	ErrPairQRTimeout          = bridgev2.RespError{Err: pairingErrMsgQRTimeout, ErrCode: "FI.MAU.GMESSAGES.PAIR_QR_TIMEOUT", StatusCode: http.StatusBadRequest}
	ErrPairStartUnknown       = bridgev2.RespError{Err: pairingErrMsgStartUnknown, ErrCode: "M_UNKNOWN", StatusCode: http.StatusInternalServerError}
	ErrPairWaitUnknown        = bridgev2.RespError{Err: pairingErrMsgWaitUnknown, ErrCode: "M_UNKNOWN", StatusCode: http.StatusInternalServerError}
	ErrPairQRRefreshUnknown   = bridgev2.RespError{Err: pairingErrMsgQRRefreshUnknown, ErrCode: "M_UNKNOWN", StatusCode: http.StatusInternalServerError}
)

var loginFlows = []bridgev2.LoginFlow{{
	Name:        "Google Account",
	Description: "Log in with your Google account and pair by tapping an emoji on your phone",
	ID:          LoginFlowIDGoogle,
}, {
	Name:        "QR",
	Description: "Pair by scanning a QR code on your phone",
	ID:          LoginFlowIDQR,
}}

func (gc *GMConnector) GetLoginFlows() []bridgev2.LoginFlow {
	return loginFlows
}

func (gc *GMConnector) CreateLogin(ctx context.Context, user *bridgev2.User, flowID string) (bridgev2.LoginProcess, error) {
	switch flowID {
	case LoginFlowIDGoogle:
		return &GoogleLoginProcess{Main: gc, User: user}, nil
	case LoginFlowIDQR:
		return &QRLoginProcess{Main: gc, User: user, MaxAttempts: 6}, nil
	default:
		return nil, fmt.Errorf("unknown login flow %s", flowID)
	}
}

type QRLoginProcess struct {
	Main        *GMConnector
	User        *bridgev2.User
	Client      *libgm.Client
	PairSuccess chan *gmproto.PairedData
	PrevStart   time.Time
	MaxAttempts int
}

var _ bridgev2.LoginProcessDisplayAndWait = (*QRLoginProcess)(nil)

func (ql *QRLoginProcess) Start(ctx context.Context) (*bridgev2.LoginStep, error) {
	ql.PairSuccess = make(chan *gmproto.PairedData)
	ql.Client = libgm.NewClient(libgm.NewAuthData(), ql.User.Log.With().Str("component", "libgm").Str("parent_action", "qr pair").Logger())
	ql.Client.SetEventHandler(func(evt any) {
		ql.Client.Logger.Warn().Type("event_type", evt).Msg("Unexpected pre-pairing event")
	})
	callback := func(data *gmproto.PairedData) {
		ql.PairSuccess <- data
	}
	ql.Client.PairCallback.Store(&callback)
	err := ql.Client.FetchConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPairStartUnknown, err)
	}
	qr, err := ql.Client.StartLogin()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPairStartUnknown, err)
	}
	ql.PrevStart = time.Now()
	return &bridgev2.LoginStep{
		Type:         bridgev2.LoginStepTypeDisplayAndWait,
		StepID:       LoginStepIDQR,
		Instructions: "Scan the QR code using the Google Messages app on your Android phone",
		DisplayAndWaitParams: &bridgev2.LoginDisplayAndWaitParams{
			Type: bridgev2.LoginDisplayTypeQR,
			Data: qr,
		},
	}, nil
}

func (ql *QRLoginProcess) Cancel() {
	if ql.Client != nil {
		ql.Client.Disconnect()
	}
}

const QRExpiryTime = 30 * time.Second

func (ql *QRLoginProcess) Wait(ctx context.Context) (*bridgev2.LoginStep, error) {
	after := time.NewTimer(time.Until(ql.PrevStart.Add(QRExpiryTime)))
	select {
	case data := <-ql.PairSuccess:
		return ql.Main.finishLogin(ctx, ql.User, ql.Client, true, data.GetMobile().GetSourceID(), "")
	case <-after.C:
		ql.MaxAttempts--
		if ql.MaxAttempts <= 0 {
			ql.Client.Disconnect()
			return nil, ErrPairQRTimeout
		}
		newQR, err := ql.Client.RefreshPhoneRelay()
		if err != nil {
			ql.Client.Disconnect()
			return nil, fmt.Errorf("%w: %w", ErrPairQRRefreshUnknown, err)
		}
		ql.PrevStart = time.Now()
		return &bridgev2.LoginStep{
			Type:         bridgev2.LoginStepTypeDisplayAndWait,
			StepID:       LoginStepIDQR,
			Instructions: "Scan the QR code using the Google Messages app on your Android phone",
			DisplayAndWaitParams: &bridgev2.LoginDisplayAndWaitParams{
				Type: bridgev2.LoginDisplayTypeQR,
				Data: newQR,
			},
		}, nil
	case <-ctx.Done():
		ql.Client.Disconnect()
		return nil, ctx.Err()
	}
}

type GoogleLoginProcess struct {
	Main     *GMConnector
	User     *bridgev2.User
	Client   *libgm.Client
	Sess     *libgm.PairingSession
	Override *bridgev2.UserLogin
}

var (
	_ bridgev2.LoginProcessDisplayAndWait = (*GoogleLoginProcess)(nil)
	_ bridgev2.LoginProcessCookies        = (*GoogleLoginProcess)(nil)
	_ bridgev2.LoginProcessWithOverride   = (*GoogleLoginProcess)(nil)
)

var loginStepCookies *bridgev2.LoginStep

func init() {
	cookies := map[string]string{
		"OSID":             "messages.google.com",
		"SID":              ".google.com",
		"HSID":             ".google.com",
		"SSID":             ".google.com",
		"APISID":           ".google.com",
		"SAPISID":          ".google.com",
		"__Secure-1PSIDTS": ".google.com",
	}
	requiredCookies := []string{"SID", "HSID", "OSID", "SSID", "APISID", "SAPISID"}
	cookieFields := make([]bridgev2.LoginCookieField, len(cookies))
	i := 0
	for cookie, domain := range cookies {
		cookieFields[i] = bridgev2.LoginCookieField{
			ID:       cookie,
			Required: slices.Contains(requiredCookies, cookie),
			Sources: []bridgev2.LoginCookieFieldSource{{
				Type:         bridgev2.LoginCookieTypeCookie,
				Name:         cookie,
				CookieDomain: domain,
			}},
		}
		i++
	}
	loginStepCookies = &bridgev2.LoginStep{
		Type:         bridgev2.LoginStepTypeCookies,
		StepID:       LoginStepIDGoogle,
		Instructions: "Enter a JSON object with your cookies, or a cURL command copied from browser devtools.",
		CookiesParams: &bridgev2.LoginCookiesParams{
			URL:    "https://accounts.google.com/AccountChooser?continue=https://messages.google.com/web/config",
			Fields: cookieFields,
		},
	}
}

func (gl *GoogleLoginProcess) Cancel() {
	if gl.Client != nil {
		gl.Client.Disconnect()
	}
}

func (gl *GoogleLoginProcess) Start(ctx context.Context) (*bridgev2.LoginStep, error) {
	return loginStepCookies, nil
}

func (gl *GoogleLoginProcess) StartWithOverride(ctx context.Context, override *bridgev2.UserLogin) (*bridgev2.LoginStep, error) {
	meta := override.Metadata.(*UserLoginMetadata)
	// Only allow reauth if the target login has crypto keys and was signed in with Google.
	if meta != nil && meta.Session != nil && meta.Session.TachyonAuthToken != nil && meta.Session.PairingID != uuid.Nil {
		gl.Override = override
	}
	return loginStepCookies, nil
}

func (gl *GoogleLoginProcess) SubmitCookies(ctx context.Context, cookies map[string]string) (*bridgev2.LoginStep, error) {
	if gl.Override != nil {
		meta := gl.Override.Metadata.(*UserLoginMetadata)
		cli := gl.Override.Client.(*GMClient)
		if cli.Client == nil {
			cli.NewClient()
		}
		meta.Session.SetCookies(cookies)
		err := cli.Client.FetchConfig(ctx)
		if err != nil {
			zerolog.Ctx(ctx).Err(err).Msg("Failed to fetch config after Google relogin")
		} else if cli.Client.Config.GetDeviceInfo().GetEmail() != meta.Session.Mobile.GetSourceID() {
			zerolog.Ctx(ctx).Err(err).
				Str("old_login", meta.Session.Mobile.GetSourceID()).
				Str("new_login", cli.Client.Config.GetDeviceInfo().GetEmail()).
				Msg("Reauthenticated with wrong account")
		} else if err = cli.Client.Connect(); err != nil {
			zerolog.Ctx(ctx).Err(err).Msg("Failed to reconnect existing client after Google relogin")
		} else {
			err = gl.Override.Save(ctx)
			if err != nil {
				err = fmt.Errorf("failed to save cookies after relogin: %w", err)
			}
			return &bridgev2.LoginStep{
				Type:         bridgev2.LoginStepTypeComplete,
				StepID:       LoginStepIDComplete,
				Instructions: "Successfully re-authenticated",
				CompleteParams: &bridgev2.LoginCompleteParams{
					UserLoginID: gl.Override.ID,
					UserLogin:   gl.Override,
				},
			}, err
		}
		meta.Session.SetCookies(nil)
	}
	ad := libgm.NewAuthData()
	ad.Cookies = cookies
	gl.Client = libgm.NewClient(ad, gl.User.Log.With().Str("component", "libgm").Str("parent_action", "google pair").Logger())
	gl.Client.SetEventHandler(func(evt any) {
		gl.Client.Logger.Warn().Type("event_type", evt).Msg("Unexpected pre-pairing event")
	})
	err := gl.Client.FetchConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPairStartUnknown, err)
	}
	var emoji string
	emoji, gl.Sess, err = gl.Client.StartGaiaPairing(ctx)
	if err != nil {
		gl.Client.Disconnect()
		switch {
		case errors.Is(err, libgm.ErrNoDevicesFound):
			return nil, ErrPairNoDevices
		case errors.Is(err, libgm.ErrPairingInitTimeout):
			return nil, ErrPairPhoneNotResponding
		default:
			return nil, fmt.Errorf("%w: %w", ErrPairStartUnknown, err)
		}
	}
	return &bridgev2.LoginStep{
		Type:         bridgev2.LoginStepTypeDisplayAndWait,
		StepID:       LoginStepIDEmoji,
		Instructions: "Tap the emoji on the Google Messages app on your phone",
		DisplayAndWaitParams: &bridgev2.LoginDisplayAndWaitParams{
			Type:     bridgev2.LoginDisplayTypeEmoji,
			Data:     emoji,
			ImageURL: libgm.GetEmojiSVG(emoji),
		},
	}, nil
}

func (gl *GoogleLoginProcess) Wait(ctx context.Context) (*bridgev2.LoginStep, error) {
	phoneID, err := gl.Client.FinishGaiaPairing(ctx, gl.Sess)
	if err != nil {
		gl.Client.Disconnect()
		switch {
		case errors.Is(err, libgm.ErrIncorrectEmoji):
			return nil, ErrPairIncorrectEmoji
		case errors.Is(err, libgm.ErrPairingCancelled):
			return nil, ErrPairCancelled
		case errors.Is(err, libgm.ErrPairingTimeout):
			return nil, ErrPairTimeout
		case errors.Is(err, context.Canceled):
			// This should only happen if the client already disconnected, so clients will probably never see this error code.
			return nil, err
		default:
			return nil, fmt.Errorf("%w: %w", ErrPairWaitUnknown, err)
		}
	}
	return gl.Main.finishLogin(ctx, gl.User, gl.Client, false, phoneID, gl.Client.AuthData.Mobile.GetSourceID())
}

func (gc *GMConnector) finishLogin(ctx context.Context, user *bridgev2.User, client *libgm.Client, qr bool, phoneID, remoteName string) (*bridgev2.LoginStep, error) {
	client.Disconnect()
	loginID := networkid.UserLoginID(phoneID)
	idPrefix, err := gc.DB.GetLoginPrefix(ctx, loginID)
	if err != nil {
		return nil, fmt.Errorf("failed to get login prefix: %w", err)
	}
	ul, err := user.NewLogin(ctx, &database.UserLogin{
		ID:         loginID,
		RemoteName: remoteName,
		Metadata: &UserLoginMetadata{
			Session:  client.AuthData,
			IDPrefix: idPrefix,
		},
	}, &bridgev2.NewLoginParams{
		DeleteOnConflict: true,
	})
	if err != nil {
		return nil, err
	}
	if qr {
		// Sleep for a bit to let the phone save the pair data. If we reconnect too quickly,
		// the phone won't recognize the session the bridge will get unpaired.
		time.Sleep(2 * time.Second)
	}
	ul.Client.Connect(ul.Log.WithContext(context.Background()))
	return &bridgev2.LoginStep{
		Type:         bridgev2.LoginStepTypeComplete,
		StepID:       LoginStepIDComplete,
		Instructions: "Successfully logged in",
		CompleteParams: &bridgev2.LoginCompleteParams{
			UserLoginID: ul.ID,
			UserLogin:   ul,
		},
	}, nil
}
