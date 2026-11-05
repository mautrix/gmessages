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
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/mediaproxy"

	"go.mau.fi/mautrix-gmessages/pkg/libgm/events"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

var _ bridgev2.DirectMediableNetwork = (*GMConnector)(nil)

func (gc *GMConnector) SetUseDirectMedia() {
	gc.directMedia = true
}

const mediaHashLength = 4

type directMediaPartKey struct {
	MessageID networkid.MessageID
	PartID    networkid.PartID
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
	return bytes.Equal(hashGoogleMediaID(m.MediaID), hash) ||
		(m.RefreshedFromMediaID != "" && bytes.Equal(hashGoogleMediaID(m.RefreshedFromMediaID), hash)) ||
		slices.ContainsFunc(m.PriorMediaIDs, func(id string) bool { return bytes.Equal(hashGoogleMediaID(id), hash) })
}

func (m *MessageMetadata) inheritMediaIDs(old *MessageMetadata) {
	for _, id := range append(slices.Clone(old.PriorMediaIDs), old.MediaID, old.RefreshedFromMediaID) {
		if id != "" && !slices.Contains(m.PriorMediaIDs, id) {
			m.PriorMediaIDs = append(m.PriorMediaIDs, id)
		}
	}
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
	} else if login == nil || !login.Client.IsLoggedIn() {
		return nil, bridgev2.ErrNotLoggedIn
	}
	client := login.Client.(*GMClient)
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
	data, err := client.Client.DownloadMedia(ctx, meta.MediaID, meta.MediaKey)
	var httpErr events.HTTPError
	if errors.As(err, &httpErr) && (httpErr.Resp.StatusCode == http.StatusNotFound || httpErr.Resp.StatusCode == http.StatusGone) {
		result := client.directMediaRefreshes.DoChan(string(part.ID)+"\x00"+string(part.PartID), func() (any, error) {
			return client.refreshDirectMedia(context.WithoutCancel(ctx), part, messageID, mediaHash)
		})
		var refreshed *MessageMetadata
		var refreshErr error
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case res := <-result:
			refreshErr = res.Err
			refreshed, _ = res.Val.(*MessageMetadata)
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		} else if refreshErr != nil {
			zerolog.Ctx(ctx).Debug().Err(refreshErr).Msg("Failed to refresh direct media")
		} else if refreshed != nil && refreshed.matchesMediaHash(mediaHash) {
			meta = refreshed
			data, err = client.Client.DownloadMedia(ctx, meta.MediaID, meta.MediaKey)
		}
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

func (gc *GMClient) refreshDirectMedia(ctx context.Context, part *database.Message, messageID string, mediaHash []byte) (*MessageMetadata, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	meta := part.Metadata.(*MessageMetadata)
	if meta.MediaPartID == "" {
		return nil, nil
	}
	if updated, err := gc.saveRefreshedMedia(ctx, part, nil, mediaHash); updated != nil || err != nil {
		return updated, err
	}
	conversationID, err := gc.ParsePortalID(part.Room.ID)
	if err != nil {
		return nil, err
	}
	message, err := gc.Client.GetFullSizeImage(ctx, messageID, meta.MediaPartID)
	if err != nil {
		return nil, err
	}
	findMedia := func(message *gmproto.Message) *gmproto.MediaContent {
		if message.GetMessageID() == messageID {
			for _, info := range message.GetMessageInfo() {
				media := info.GetMediaContent()
				if info.GetActionMessageID() == meta.MediaPartID && media.GetMediaID() != "" && media.GetMediaID() != meta.MediaID && len(media.GetDecryptionKey()) == 32 {
					return media
				}
			}
		}
		return nil
	}
	if media := findMedia(message); media != nil {
		return gc.saveRefreshedMedia(ctx, part, media, mediaHash)
	}
	for {
		if updated, err := gc.saveRefreshedMedia(ctx, part, nil, mediaHash); updated != nil || err != nil {
			return updated, err
		}
		var cursor *gmproto.Cursor
		type cursorKey struct {
			id        string
			timestamp int64
		}
		seen := make(map[cursorKey]bool)
		for {
			response, err := gc.Client.FetchMessages(ctx, conversationID, 100, cursor)
			if err != nil {
				return nil, err
			}
			found := false
			for _, message := range response.GetMessages() {
				if media := findMedia(message); media != nil {
					return gc.saveRefreshedMedia(ctx, part, media, mediaHash)
				}
				found = found || message.GetMessageID() == messageID
			}
			cursor = response.GetCursor()
			if found || cursor == nil || len(response.GetMessages()) == 0 {
				break
			}
			key := cursorKey{cursor.LastItemID, cursor.LastItemTimestamp}
			if seen[key] {
				break
			}
			seen[key] = true
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func (gc *GMClient) saveRefreshedMedia(ctx context.Context, part *database.Message, media *gmproto.MediaContent, mediaHash []byte) (*MessageMetadata, error) {
	key := directMediaPartKey{part.ID, part.PartID}
	if err := gc.lockDirectMediaPart(ctx, key); err != nil {
		return nil, err
	}
	defer gc.directMediaLocks.Unlock(key)
	updated, err := gc.Main.br.DB.Message.GetPartByID(ctx, gc.UserLogin.ID, part.ID, part.PartID)
	if err != nil || updated == nil {
		return nil, err
	}
	meta := updated.Metadata.(*MessageMetadata)
	if !meta.matchesMediaHash(mediaHash) {
		return nil, mautrix.MNotFound.WithMessage("Version mismatch, media was likely replaced")
	}
	if meta.MediaID != part.Metadata.(*MessageMetadata).MediaID {
		return meta, nil
	} else if media == nil {
		return nil, nil
	}
	if meta.RefreshedFromMediaID == "" {
		meta.RefreshedFromMediaID = meta.MediaID
	}
	meta.inheritMediaIDs(meta)
	meta.MediaID, meta.MediaKey = media.MediaID, media.DecryptionKey
	if err = gc.Main.br.DB.Message.Update(ctx, updated); err != nil {
		return nil, err
	}
	return meta, nil
}

func (gc *GMClient) lockDirectMediaPart(ctx context.Context, key directMediaPartKey) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		} else if gc.directMediaLocks.TryLock(key) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}
