package libgm

import (
	"bytes"
	"context"
	"net/http"

	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/util"
)

func (c *Client) ListStickerPacks(ctx context.Context) ([]*gmproto.StickerPack, error) {
	if c == nil {
		return nil, ErrClientIsNil
	}
	// Same user context as the web app, minus the market config that it fetches from the phone.
	body, err := proto.Marshal(&gmproto.ListStickerPacksRequest{UserContext: &gmproto.StickerUserContext{
		Locale: &gmproto.StickerUserContext_Locale{
			Client:   &gmproto.StickerUserContext_ClientInfo{Type: 2, Version: "4"},
			Language: "en",
			Region:   "US",
		},
		Version:    "20210823",
		ClientType: 3,
	}})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, util.ListStickerPacksURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	util.BuildRelayHeaders(req, ContentTypeProtobuf, "*/*")
	req.Header.Set("x-goog-api-key", util.StickerAPIKey)
	resp, err := typedHTTPResponse[*gmproto.ListStickerPacksResponse](c.http.Do(req))
	if err != nil {
		return nil, err
	}
	packs := make([]*gmproto.StickerPack, 0, len(resp.GetItems()))
	for _, item := range resp.GetItems() {
		packs = append(packs, item.GetPack())
	}
	return packs, nil
}

func (c *Client) DownloadSticker(ctx context.Context, url string) ([]byte, error) {
	if c == nil {
		return nil, ErrClientIsNil
	}
	return c.downloadPublicFile(ctx, url, "downloading sticker")
}
