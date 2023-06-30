package textgapi

import (
	"log"
	"os"

	"go.mau.fi/mautrix-gmessages/libgm/builders"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

type Misc struct {
	client *Client
}

func (m *Misc) TenorSearch(searchOpts *builders.TenorSearch) (interface{}, error) {
	searchQuery, buildErr := searchOpts.Build()
	if buildErr != nil {
		return nil, buildErr
	}

	uri := util.TENOR_SEARCH_GIF + searchQuery
	log.Println(uri)
	os.Exit(1)
	return nil, nil
}
