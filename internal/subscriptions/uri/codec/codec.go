// Package codec owns the base64 wire encoding used by subscription URIs.
package codec

import (
	"encoding/base64"
	"errors"
)

// Function to return decoded bytes if a string is Base64 encoded
func DecodeOrOriginal(str string) string {
	decoded, err := Decode(str)
	if err == nil {
		return string(decoded)
	}
	return str
}

func Decode(str string) ([]byte, error) {
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if decoded, err := encoding.DecodeString(str); err == nil {
			return decoded, nil
		}
	}
	return nil, errors.New("invalid subscription base64")
}

func Encode(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}
