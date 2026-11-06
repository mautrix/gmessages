// mautrix-gmessages - A Matrix-Google Messages puppeting bridge.
// Copyright (C) 2026 Tulir Asokan
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
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/mediaproxy"

	"go.mau.fi/mautrix-gmessages/pkg/libgm"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/events"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

var _ bridgev2.DirectMediableNetwork = (*GMConnector)(nil)

func (gc *GMConnector) SetUseDirectMedia() {
	gc.directMedia = true
}

const (
	mediaHashLength            = 4
	directMediaRefreshTimeout  = 30 * time.Second
	directMediaRefreshCooldown = time.Minute
)

var (
	errDirectMediaNotConnected  = mautrix.MUnknown.WithMessage("Google Messages is not connected").WithStatus(http.StatusServiceUnavailable).WithCanRetry(true)
	errDirectMediaRefreshFailed = mautrix.MUnknown.WithMessage("Failed to request new media ID from phone").WithStatus(http.StatusGatewayTimeout).WithCanRetry(true)
)

type directMediaRefreshFailure struct {
	at  time.Time
	err error
}

type abortOnMediaReadError struct {
	io.ReadCloser
}

func (r abortOnMediaReadError) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if err != nil && err != io.EOF {
		panic(http.ErrAbortHandler)
	}
	return n, err
}

func (m *MessageMetadata) matchesMediaHash(hash []byte) bool {
	return slices.ContainsFunc(m.MediaIDs, func(id string) bool { return bytes.Equal(hashGoogleMediaID(id), hash) })
}

func (m *MessageMetadata) inheritMediaIDs(old *MessageMetadata) {
	for _, id := range old.MediaIDs {
		if !slices.Contains(m.MediaIDs, id) {
			m.MediaIDs = append(m.MediaIDs, id)
		}
	}
}

func (m *MessageMetadata) adoptRefreshedMedia(media *gmproto.MediaContent) bool {
	if len(m.MediaKey) == 0 || media.GetMediaID() == "" || len(media.GetDecryptionKey()) == 0 || slices.Contains(m.MediaIDs, media.GetMediaID()) {
		return false
	}
	m.MediaID, m.MediaKey = media.MediaID, media.DecryptionKey
	m.MediaIDs = append(m.MediaIDs, media.MediaID)
	return true
}

func (m *MessageEvent) adoptRefreshedMedia(dbm DBMessages) (adopted bool) {
	if !m.g.Main.directMedia {
		return false
	}
	for _, info := range m.MessageInfo {
		part := dbm.findMediaPart(info.GetActionMessageID())
		if part == nil || !part.Metadata.(*MessageMetadata).adoptRefreshedMedia(info.GetMediaContent()) {
			continue
		}
		adopted = true
		key := fullMediaRequestKey{MessageID: m.MessageID, ActionMessageID: info.GetActionMessageID()}
		if waiter, ok := m.g.directMediaRefreshWaiters.Pop(key); ok {
			waiter <- info.GetMediaContent()
		}
	}
	return
}

func hashGoogleMediaID(mediaID string) []byte {
	hash := sha256.Sum256([]byte(mediaID))
	return hash[:mediaHashLength]
}

func encodeDirectMediaID(login networkid.UserLoginID, messageID, actionMessageID, mediaID string) networkid.MediaID {
	data := binary.AppendUvarint([]byte{1}, uint64(len(login)))
	data = append(data, login...)
	data = binary.AppendUvarint(data, uint64(len(messageID)))
	data = append(data, messageID...)
	data = append(data, hashGoogleMediaID(mediaID)...)
	return append(data, actionMessageID...)
}

func cutLengthPrefixed(data []byte) (value, rest []byte, ok bool) {
	length, n := binary.Uvarint(data)
	if n <= 0 || length == 0 || length > uint64(len(data)-n) {
		return nil, nil, false
	}
	return data[n : n+int(length)], data[n+int(length):], true
}

func parseDirectMediaID(data networkid.MediaID) (login networkid.UserLoginID, messageID, actionMessageID string, mediaHash []byte, err error) {
	if len(data) == 0 || data[0] != 1 {
		return "", "", "", nil, mediaproxy.ErrInvalidMediaIDSyntax
	}
	loginBytes, rest, ok := cutLengthPrefixed(data[1:])
	if !ok {
		return "", "", "", nil, mediaproxy.ErrInvalidMediaIDSyntax
	}
	messageBytes, rest, ok := cutLengthPrefixed(rest)
	if !ok || len(rest) < mediaHashLength {
		return "", "", "", nil, mediaproxy.ErrInvalidMediaIDSyntax
	}
	return networkid.UserLoginID(loginBytes), string(messageBytes), string(rest[mediaHashLength:]), rest[:mediaHashLength], nil
}

func (gc *GMConnector) Download(ctx context.Context, mediaID networkid.MediaID, params map[string]string) (mediaproxy.GetMediaResponse, error) {
	loginID, messageID, actionMessageID, mediaHash, err := parseDirectMediaID(mediaID)
	if err != nil {
		return nil, err
	}
	zerolog.Ctx(ctx).Debug().
		Str("login_id", string(loginID)).
		Str("message_id", messageID).
		Str("part_id", actionMessageID).
		Msg("Direct downloading media")
	login, err := gc.br.GetExistingUserLoginByID(ctx, loginID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user login: %w", err)
	} else if login == nil {
		return nil, mautrix.MNotFound.WithMessage("User login not found")
	}
	client := login.Client.(*GMClient)
	cli := client.Client
	if !cli.IsLoggedIn() {
		return nil, errDirectMediaNotConnected
	}
	parts, err := gc.br.DB.Message.GetAllPartsByID(ctx, loginID, client.MakeMessageID(messageID))
	if err != nil {
		return nil, fmt.Errorf("failed to get message: %w", err)
	}
	part := DBMessages(parts).findMediaPart(actionMessageID)
	if part == nil {
		return nil, mautrix.MNotFound.WithMessage("Message not found")
	}
	meta := part.Metadata.(*MessageMetadata)
	if len(meta.MediaKey) == 0 {
		return nil, mautrix.MNotFound.WithMessage("Message doesn't have direct media metadata")
	} else if !meta.matchesMediaHash(mediaHash) {
		return nil, mautrix.MNotFound.WithMessage("Version mismatch, media was likely replaced")
	}
	data, err := cli.DownloadMedia(ctx, meta.MediaID, meta.MediaKey)
	var httpErr events.HTTPError
	if errors.As(err, &httpErr) && (httpErr.Resp.StatusCode == http.StatusNotFound || httpErr.Resp.StatusCode == http.StatusGone) {
		if !client.PhoneResponding {
			return nil, errDirectMediaRefreshFailed
		}
		media, refreshErr := client.refreshDirectMedia(ctx, cli, fullMediaRequestKey{MessageID: messageID, ActionMessageID: actionMessageID}, meta.MediaIDs)
		if refreshErr != nil {
			return nil, refreshErr
		}
		data, err = cli.DownloadMedia(ctx, media.MediaID, media.DecryptionKey)
	}
	if errors.As(err, &httpErr) {
		switch httpErr.Resp.StatusCode {
		case http.StatusNotFound, http.StatusGone:
			return nil, mautrix.MNotFound.WithMessage("Media not found on Google Messages")
		case http.StatusUnauthorized, http.StatusForbidden:
			return nil, mautrix.MForbidden.WithMessage("Google Messages denied access to the media")
		case http.StatusTooManyRequests:
			return nil, mautrix.MLimitExceeded.WithMessage("Rate limited by Google Messages while fetching the media").WithCanRetry(true)
		default:
			return nil, mautrix.MUnknown.WithMessage("Google Messages returned HTTP %d while fetching the media", httpErr.Resp.StatusCode).WithStatus(http.StatusBadGateway).WithCanRetry(true)
		}
	} else if err != nil {
		return nil, err
	}
	return &mediaproxy.GetMediaResponseData{Reader: abortOnMediaReadError{data}, ContentType: meta.MediaMIME}, nil
}

func (gc *GMClient) refreshDirectMedia(ctx context.Context, cli *libgm.Client, key fullMediaRequestKey, knownIDs []string) (*gmproto.MediaContent, error) {
	if failure, failed := gc.directMediaRefreshFailures.Get(key); failed && time.Since(failure.at) < directMediaRefreshCooldown {
		return nil, failure.err
	}
	result := gc.directMediaRefreshes.DoChan(key.MessageID+"\x00"+key.ActionMessageID, func() (any, error) {
		media, err := gc.requestRefreshedMedia(context.WithoutCancel(ctx), cli, key, knownIDs)
		if err != nil {
			zerolog.Ctx(ctx).Debug().Err(err).Msg("Failed to refresh direct media")
			if !errors.Is(err, mautrix.MNotFound) {
				err = errDirectMediaRefreshFailed
			}
			gc.directMediaRefreshFailures.Set(key, directMediaRefreshFailure{at: time.Now(), err: err})
		}
		return media, err
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-result:
		if res.Err != nil {
			return nil, res.Err
		}
		return res.Val.(*gmproto.MediaContent), nil
	}
}

func (gc *GMClient) requestRefreshedMedia(ctx context.Context, cli *libgm.Client, key fullMediaRequestKey, knownIDs []string) (*gmproto.MediaContent, error) {
	ctx, cancel := context.WithTimeout(ctx, directMediaRefreshTimeout)
	defer cancel()
	waiter := make(chan *gmproto.MediaContent, 1)
	gc.directMediaRefreshWaiters.Set(key, waiter)
	defer gc.directMediaRefreshWaiters.Delete(key)
	msg, err := cli.GetFullSizeImage(ctx, key.MessageID, key.ActionMessageID)
	if err != nil {
		return nil, err
	} else if msg == nil {
		return nil, mautrix.MNotFound.WithMessage("Message no longer exists on the phone")
	}
	for _, info := range msg.GetMessageInfo() {
		media := info.GetMediaContent()
		if info.GetActionMessageID() == key.ActionMessageID && media.GetMediaID() != "" && len(media.GetDecryptionKey()) > 0 && !slices.Contains(knownIDs, media.GetMediaID()) {
			return media, nil
		}
	}
	// The response has the phone's current media ID, a re-upload arrives later as a normal message update.
	select {
	case media := <-waiter:
		return media, nil
	case <-ctx.Done():
		return nil, mautrix.MNotFound.WithMessage("Phone didn't re-upload the media")
	}
}
