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
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/png"
	"net/http"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/event"

	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

const stickerPackURLPrefix = "gmessages://"
const stickerCacheTime = 1 * time.Hour

var _ bridgev2.StickerImportingNetworkAPI = (*GMClient)(nil)

type cachedStickerPack struct {
	pack      *bridgev2.ImportedImagePack
	fetchedAt time.Time
}

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
	gc.stickerLock.Lock()
	cached, ok := gc.stickerPackCache[packID]
	gc.stickerLock.Unlock()
	if ok && time.Since(cached.fetchedAt) < stickerCacheTime {
		return cached.pack, nil
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
		data, err := gc.Client.DownloadSticker(ctx, sticker.GetImage().GetUrl())
		if ctx.Err() != nil {
			return nil, ctx.Err()
		} else if err != nil {
			zerolog.Ctx(ctx).Warn().Err(err).Str("sticker_id", sticker.GetName()).Msg("Failed to download sticker, skipping")
			continue
		}
		config, _, _ := image.DecodeConfig(bytes.NewReader(data))
		mimeType := http.DetectContentType(data)
		mxc, _, err := gc.Main.br.Bot.UploadMedia(ctx, "", data, path.Base(sticker.GetImage().GetUrl()), mimeType)
		if err != nil {
			return nil, fmt.Errorf("failed to upload sticker %s: %w", sticker.GetName(), err)
		}
		if metadata.AvatarURL == "" {
			metadata.AvatarURL = mxc
		}
		imported.Content.Images[path.Base(sticker.GetName())] = &event.ImagePackImage{
			URL:  mxc,
			Body: sticker.GetDescription(),
			Info: &event.FileInfo{
				MimeType: mimeType,
				Size:     len(data),
				Width:    config.Width,
				Height:   config.Height,
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
	if len(imported.Content.Images) == len(pack.GetStickers()) {
		gc.stickerLock.Lock()
		if gc.stickerPackCache == nil {
			gc.stickerPackCache = make(map[string]cachedStickerPack)
		}
		gc.stickerPackCache[packID] = cachedStickerPack{pack: imported, fetchedAt: time.Now()}
		gc.stickerLock.Unlock()
	}
	return imported, nil
}
