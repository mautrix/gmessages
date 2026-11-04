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
	"os"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/mediaproxy"

	"go.mau.fi/mautrix-gmessages/pkg/libgm/events"
)

var _ bridgev2.DirectMediableNetwork = (*GMConnector)(nil)

func (gc *GMConnector) SetUseDirectMedia() {
	gc.directMedia = true
}

const mediaHashLength = 4

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
	} else if !bytes.Equal(hashGoogleMediaID(meta.MediaID), mediaHash) {
		return nil, mautrix.MNotFound.WithMessage("Version mismatch, media was likely replaced")
	}
	return &mediaproxy.GetMediaResponseFile{
		Callback: func(f *os.File) (*mediaproxy.FileMeta, error) {
			data, err := client.Client.DownloadMedia(ctx, meta.MediaID, meta.MediaKey)
			var httpErr events.HTTPError
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
			defer data.Close()
			if _, err = io.Copy(f, data); err != nil {
				return nil, fmt.Errorf("failed to download media: %w", err)
			}
			return &mediaproxy.FileMeta{ContentType: meta.MediaMIME}, nil
		},
	}, nil
}
