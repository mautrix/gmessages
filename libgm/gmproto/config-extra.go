package gmproto

import (
	"strconv"
)

func (c *Config) ParsedClientVersion() (*ConfigVersion, error) {
	version := c.ClientVersion

	v1 := version[0:4]
	v2 := version[4:6]
	v3 := version[6:8]

	if v2[0] == 48 {
		v2 = string(v2[1])
	}
	if v3[0] == 48 {
		v3 = string(v3[1])
	}

	first, e := strconv.Atoi(v1)
	if e != nil {
		return nil, e
	}

	second, e1 := strconv.Atoi(v2)
	if e1 != nil {
		return nil, e1
	}

	third, e2 := strconv.Atoi(v3)
	if e2 != nil {
		return nil, e2
	}

	return &ConfigVersion{
		Year:  int32(first),
		Month: int32(second),
		Day:   int32(third),
		V1:    4,
		V2:    6,
	}, nil
}
