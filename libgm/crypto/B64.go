package crypto

import (
	"encoding/base64"
	"fmt"
	"strings"
)

func EncodeBase64Standard(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

func EncodeBase64(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

func Base64Decode(input string) ([]byte, error) {
    padding := len(input) % 4
    if padding > 0 {
        input += strings.Repeat("=", 4-padding)
    }

    data, err := base64.URLEncoding.DecodeString(input)
    if err != nil {
        return nil, err
    }
    return data, nil
}

func Base64DecodeStandard(input string) ([]byte, error) {
    decoded, err := base64.StdEncoding.DecodeString(input)
    if err != nil {
        fmt.Println("decode error:", err)
        return nil, err
    }
    return decoded, nil
}

func Base64Encode(input []byte) string {
	return base64.StdEncoding.EncodeToString(input)
}