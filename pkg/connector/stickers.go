// mautrix-gmessages - A Matrix-Google Messages puppeting bridge.
// Copyright (C) 2026 Killian Lelong
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
	"mime"
	"net/http"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

const stickerPackURLPrefix = "gmessages://"
const stickerCacheTime = 1 * time.Hour

var _ bridgev2.StickerImportingNetworkAPI = (*GMClient)(nil)

func stickerPackMetadata(pack *gmproto.StickerPack) event.ImagePackMetadata {
	return event.ImagePackMetadata{
		DisplayName: pack.GetTitle(),
		Attribution: pack.GetArtist(),
		Usage:       []event.ImagePackUsage{event.ImagePackUsageSticker},
		BridgedPack: &event.BridgedStickerPack{Network: "gmessages", URL: stickerPackURLPrefix + pack.GetName()},
	}
}

func (gc *GMClient) getStickerPacks(ctx context.Context) ([]*gmproto.StickerPack, error) {
	if gc.Client == nil {
		return nil, bridgev2.ErrNotLoggedIn
	}
	gc.stickerLock.Lock()
	if time.Since(gc.stickerPacksFetchedAt) < stickerCacheTime {
		packs := gc.stickerPacks
		gc.stickerLock.Unlock()
		return packs, nil
	}
	gc.stickerLock.Unlock()
	packs, err := gc.Client.ListStickerPacks(ctx)
	if err != nil {
		return nil, err
	}
	gc.stickerLock.Lock()
	gc.stickerPacks, gc.stickerPacksFetchedAt = packs, time.Now()
	gc.stickerLock.Unlock()
	return packs, nil
}

func (gc *GMClient) ListImagePacks(ctx context.Context) ([]*event.ImagePackMetadata, error) {
	packs, err := gc.getStickerPacks(ctx)
	if err != nil {
		return nil, err
	}
	metadata := make([]*event.ImagePackMetadata, 0, len(packs))
	for _, pack := range packs {
		meta := stickerPackMetadata(pack)
		metadata = append(metadata, &meta)
	}
	return metadata, nil
}

func (gc *GMClient) DownloadImagePack(ctx context.Context, packURL string) (*bridgev2.ImportedImagePack, error) {
	packID, ok := strings.CutPrefix(packURL, stickerPackURLPrefix)
	if !ok || packID == "" {
		return nil, mautrix.MNotFound.WithMessage("Unsupported sticker pack")
	}
	packs, err := gc.getStickerPacks(ctx)
	if err != nil {
		return nil, err
	}
	index := slices.IndexFunc(packs, func(pack *gmproto.StickerPack) bool { return pack.GetName() == packID })
	if index < 0 {
		return nil, mautrix.MNotFound.WithMessage("Sticker pack not found")
	}
	pack := packs[index]
	metadata := stickerPackMetadata(pack)
	imported := &bridgev2.ImportedImagePack{
		Shortcode: "gmessages_" + path.Base(pack.GetName()),
		Content:   &event.ImagePackEventContent{Images: make(map[string]*event.ImagePackImage, len(pack.GetStickers()))},
	}
	for _, sticker := range pack.GetStickers() {
		url := sticker.GetImage().GetUrl()
		dbKey := database.Key("stickercache:" + url)
		mxc := id.ContentURIString(gc.Main.br.DB.KV.Get(ctx, dbKey))
		if mxc == "" {
			data, err := gc.Client.DownloadSticker(ctx, url)
			if ctx.Err() != nil {
				return nil, ctx.Err()
			} else if err != nil {
				zerolog.Ctx(ctx).Warn().Err(err).Str("sticker_id", sticker.GetName()).Msg("Failed to download sticker, skipping")
				continue
			}
			mxc, _, err = gc.Main.br.Bot.UploadMedia(ctx, "", data, path.Base(url), http.DetectContentType(data))
			if err != nil {
				return nil, fmt.Errorf("failed to upload sticker %s: %w", sticker.GetName(), err)
			}
			gc.Main.br.DB.KV.Set(ctx, dbKey, string(mxc))
		}
		if metadata.AvatarURL == "" {
			metadata.AvatarURL = mxc
		}
		imported.Content.Images[path.Base(sticker.GetName())] = &event.ImagePackImage{
			URL:  mxc,
			Body: sticker.GetDescription(),
			Info: &event.FileInfo{
				MimeType: mime.TypeByExtension(path.Ext(url)),
				Width:    int(sticker.GetImage().GetDimensions().GetWidth()),
				Height:   int(sticker.GetImage().GetDimensions().GetHeight()),
				BridgedSticker: &event.BridgedSticker{
					Network: "gmessages",
					ID:      sticker.GetName(),
					PackURL: metadata.BridgedPack.URL,
				},
			},
		}
	}
	if len(imported.Content.Images) == 0 {
		return nil, mautrix.MNotFound.WithMessage("Sticker pack is empty")
	}
	imported.Content.Metadata = metadata
	return imported, nil
}
