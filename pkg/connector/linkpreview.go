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
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/event"

	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/util"
)

const (
	maxLinkPreviewImageSize   = 5 * 1024 * 1024
	maxLinkPreviewRedirects   = 3
	linkPreviewImageTimeout   = 10 * time.Second
	linkPreviewConnectTimeout = 5 * time.Second
)

var (
	errLinkPreviewNonPublicIP     = errors.New("address is not public")
	errLinkPreviewBadScheme       = errors.New("unsupported URL scheme")
	errLinkPreviewTooManyRedirect = errors.New("too many redirects")
	errLinkPreviewTooLarge        = errors.New("image is too large")
	errLinkPreviewNotImage        = errors.New("response is not a supported image")
)

var linkPreviewImageTypes = map[string]struct{}{
	"image/jpeg": {},
	"image/png":  {},
	"image/gif":  {},
	"image/webp": {},
}

var nonPublicPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
}

func isPublicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return false
	}
	for _, prefix := range nonPublicPrefixes {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

func allowOnlyPublicAddresses(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return err
	} else if !isPublicIP(ip) {
		return errLinkPreviewNonPublicIP
	}
	return nil
}

func checkLinkPreviewURL(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return errLinkPreviewBadScheme
	} else if u.Hostname() == "" {
		return fmt.Errorf("missing host")
	}
	return nil
}

var linkPreviewHTTPClient = &http.Client{
	Timeout: linkPreviewImageTimeout,
	Transport: &http.Transport{
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout: linkPreviewConnectTimeout,
			Control: allowOnlyPublicAddresses,
		}).DialContext,
		TLSHandshakeTimeout:   linkPreviewConnectTimeout,
		ResponseHeaderTimeout: linkPreviewConnectTimeout,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		ForceAttemptHTTP2:     true,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxLinkPreviewRedirects {
			return errLinkPreviewTooManyRedirect
		}
		return checkLinkPreviewURL(req.URL)
	},
}

func fetchLinkPreviewImage(ctx context.Context, rawURL string) ([]byte, string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, "", err
	} else if err = checkLinkPreviewURL(parsed); err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", util.UserAgent)
	req.Header.Set("Accept", "image/webp,image/png,image/jpeg,image/gif")
	resp, err := linkPreviewHTTPClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("unexpected status code %d", resp.StatusCode)
	} else if resp.ContentLength > maxLinkPreviewImageSize {
		return nil, "", errLinkPreviewTooLarge
	} else if mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); mediaType != "" && !strings.HasPrefix(mediaType, "image/") && mediaType != "application/octet-stream" {
		return nil, "", errLinkPreviewNotImage
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxLinkPreviewImageSize+1))
	if err != nil {
		return nil, "", err
	} else if len(data) > maxLinkPreviewImageSize {
		return nil, "", errLinkPreviewTooLarge
	}
	mimeType := http.DetectContentType(data)
	if _, ok := linkPreviewImageTypes[mimeType]; !ok {
		return nil, "", errLinkPreviewNotImage
	}
	return data, mimeType, nil
}

func (gc *GMClient) addLinkPreviewImage(ctx context.Context, portal *bridgev2.Portal, intent bridgev2.MatrixAPI, msg *gmproto.Message, preview *event.BeeperLinkPreview) {
	imageURL := msg.GetLinkPreview().GetImageURL()
	if imageURL == "" {
		return
	}
	log := zerolog.Ctx(ctx)
	data, mimeType, err := fetchLinkPreviewImage(ctx, imageURL)
	if err != nil {
		log.Debug().Err(err).Msg("Failed to fetch link preview image")
		return
	}
	mxc, file, err := intent.UploadMedia(ctx, portal.MXID, data, "", mimeType)
	if err != nil {
		log.Err(err).Msg("Failed to upload link preview image")
		return
	}
	preview.ImageURL = mxc
	preview.ImageEncryption = file
	preview.ImageType = mimeType
	preview.ImageSize = event.IntOrString(len(data))
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		preview.ImageWidth = event.IntOrString(cfg.Width)
		preview.ImageHeight = event.IntOrString(cfg.Height)
	}
}
