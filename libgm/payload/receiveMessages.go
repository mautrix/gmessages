package payload

import (
	"encoding/json"

	"go.mau.fi/mautrix-gmessages/libgm/util"
)

func ReceiveMessages(rpcKey string) ([]byte, string, error) {
	id := util.RandomUUIDv4()
	data := []interface{}{
		[]interface{}{
			id,
			nil,
			nil,
			nil,
			nil,
			rpcKey,
			[]interface{}{
				nil,
				nil,
				2023,
				6,
				8,
				nil,
				4,
				nil,
				6,
			},
		},
		nil,
		nil,
		[]interface{}{
			nil,
			[]interface{}{},
		},
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, "", err
	}
	return jsonData, id, nil
}
