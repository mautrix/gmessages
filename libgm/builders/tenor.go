package builders

import (
	"fmt"
	"net/url"

	"go.mau.fi/mautrix-gmessages/libgm/util"
)

type TenorSearch struct {
	query          string
	locale         string
	content_filter string
	media_filter   string
	limit          string // limit results
}

func NewTenorSearchBuilder() *TenorSearch {
	return &TenorSearch{}
}

func (t *TenorSearch) SetQuery(query string) *TenorSearch {
	t.query = query
	return t
}
func (t *TenorSearch) SetLocale(locale string) *TenorSearch {
	t.locale = locale
	return t
}
func (t *TenorSearch) SetContentFilter(content_filter string) *TenorSearch {
	t.content_filter = content_filter
	return t
}
func (t *TenorSearch) SetMediaFilter(media_filter string) *TenorSearch {
	t.media_filter = media_filter
	return t
}
func (t *TenorSearch) SetLimit(limit string) *TenorSearch {
	t.limit = limit
	return t
}
func (t *TenorSearch) Build() (string, error) {
	if t.query == "" {
		return "", fmt.Errorf("failed to build TenorSearch: query is empty")
	}
	params := url.Values{}
	params.Add("key", util.TENOR_API_KEY)
	params.Add("q", t.query)

	if t.locale == "" {
		t.locale = "en-US"
	}
	params.Add("locale", t.locale)

	if t.content_filter == "" {
		t.content_filter = "medium"
	}
	params.Add("contentfilter", t.content_filter)

	if t.media_filter == "" {
		t.media_filter = "minimal"
	}
	params.Add("media_filter", t.media_filter)

	if t.limit == "" {
		t.limit = "16"
	}
	params.Add("limit", t.limit)

	return "?" + params.Encode(), nil
}
