// mautrix-gmessages - A Matrix-Google Messages puppeting bridge.
// Copyright (C) 2023 Tulir Asokan, Sumner Evans
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
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/id"
)

type AnalyticsClient struct {
	url    string
	key    string
	userID string
	log    zerolog.Logger
	client http.Client
}

var Analytics AnalyticsClient

func (ac *AnalyticsClient) trackSync(userID id.UserID, event string, properties map[string]interface{}) error {
	var buf bytes.Buffer
	var analyticsUserId string
	if Analytics.userID != "" {
		analyticsUserId = Analytics.userID
	} else {
		analyticsUserId = userID.String()
	}
	err := json.NewEncoder(&buf).Encode(map[string]interface{}{
		"userId":     analyticsUserId,
		"event":      event,
		"properties": properties,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, ac.url, &buf)
	if err != nil {
		return err
	}
	req.SetBasicAuth(ac.key, "")
	resp, err := ac.client.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("unexpected status code %d", resp.StatusCode)
	}
	return nil
}

func (ac *AnalyticsClient) IsEnabled() bool {
	return len(ac.key) > 0
}

func (ac *AnalyticsClient) Track(userID id.UserID, event string, properties ...map[string]interface{}) {
	if !ac.IsEnabled() {
		return
	} else if len(properties) > 1 {
		panic("Track should be called with at most one property map")
	}

	go func() {
		props := map[string]interface{}{}
		if len(properties) > 0 {
			props = properties[0]
		}
		props["bridge"] = "gmessages"
		err := ac.trackSync(userID, event, props)
		if err != nil {
			ac.log.Err(err).Str("event", event).Msg("Error tracking event")
		} else {
			ac.log.Debug().Str("event", event).Msg("Tracked event")
		}
	}()
}
