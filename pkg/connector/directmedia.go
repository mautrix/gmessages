package connector

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"mime"
	"strings"

	"github.com/rs/zerolog"
	"go.mau.fi/mautrix-gmessages/pkg/libgm"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
	"go.mau.fi/util/exmime"
	"go.mau.fi/util/ffmpeg"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/mediaproxy"
)

var _ bridgev2.DirectMediableNetwork = (*GMConnector)(nil)

func (gc *GMConnector) SetUseDirectMedia() { gc.directMedia = true }

func mediaMIME(msg *gmproto.MediaContent) string {
	typ, _, err := mime.ParseMediaType(msg.MimeType)
	if err == nil && strings.ContainsRune(typ, '/') && typ != "application/octet-stream" && !strings.ContainsRune(typ, '*') {
		return typ
	}
	return libgm.FormatToMediaType[msg.GetFormat()].Format
}

const mediaHashLength = 4

var errDirectMediaNotFound = errors.New("media not found")

func hashGoogleMediaID(mediaID string) []byte {
	hash := sha256.Sum256([]byte(mediaID))
	return hash[:mediaHashLength]
}

func encodeDirectMediaID(login networkid.UserLoginID, messageID, actionMessageID, mediaID string) networkid.MediaID {
	data := binary.AppendUvarint(nil, uint64(len(login)))
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
	loginBytes, rest, ok := cutLengthPrefixed(data)
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
	login, err := gc.br.GetExistingUserLoginByID(ctx, loginID)
	if err != nil {
		return nil, err
	}
	if login == nil || !login.Client.IsLoggedIn() {
		return nil, bridgev2.ErrNotLoggedIn
	}
	client := login.Client.(*GMClient)
	parts, err := gc.br.DB.Message.GetAllPartsByID(ctx, loginID, client.MakeMessageID(messageID))
	if err != nil {
		return nil, err
	}
	part := DBMessages(parts).findMediaPart(actionMessageID)
	if part == nil {
		return nil, errDirectMediaNotFound
	}
	meta := part.Metadata.(*MessageMetadata)
	if len(meta.MediaKey) == 0 || !bytes.Equal(hashGoogleMediaID(meta.MediaID), mediaHash) {
		return nil, errDirectMediaNotFound
	}
	data, err := client.Client.DownloadMedia(ctx, meta.MediaID, meta.MediaKey)
	if err != nil {
		return nil, err
	}
	return &mediaproxy.GetMediaResponseData{Reader: data, ContentType: meta.MediaMIME, ContentLength: -1}, nil
}

func (gc *GMClient) convertDirectMedia(ctx context.Context, portal *bridgev2.Portal, messageID string, msg *gmproto.MediaContent, dbMeta *MessageMetadata) *event.MessageEventContent {
	mimeType := mediaMIME(msg)
	if mimeType == "" || len(msg.DecryptionKey) != 32 {
		return nil
	}
	content := &event.MessageEventContent{MsgType: event.MsgFile, Body: msg.MediaName, Info: &event.FileInfo{MimeType: mimeType, Size: int(msg.Size)}}
	mediaClass, _, _ := strings.Cut(mimeType, "/")
	switch mediaClass {
	case "image":
		content.MsgType = event.MsgImage
	case "video":
		content.MsgType = event.MsgVideo
	case "audio":
		if mimeType != "audio/ogg" && ffmpeg.Supported() {
			return nil
		}
		content.MsgType = event.MsgAudio
		content.MSC3245Voice = &event.MSC3245Voice{}
	}
	directID := encodeDirectMediaID(gc.UserLogin.ID, messageID, dbMeta.MediaPartID, msg.MediaID)
	var err error
	content.URL, content.File, err = portal.Bridge.GenerateContentURI(ctx, portal.MXID, directID)
	if err != nil {
		zerolog.Ctx(ctx).Debug().Err(err).Msg("Failed to generate direct media URI, falling back to upload")
		return nil
	}
	dbMeta.MediaKey, dbMeta.MediaMIME = msg.DecryptionKey, mimeType
	if !strings.ContainsRune(content.Body, '.') {
		content.Body += exmime.ExtensionFromMimetype(mimeType)
	}
	return content
}
