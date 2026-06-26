// Package codec owns the base64 wire encoding used by subscription URIs.
package codec

import "encoding/base64"

// Function to return decoded bytes if a string is Base64 encoded
func DecodeOrOriginal(str string) string {
	decoded, err := base64.StdEncoding.DecodeString(str)
	if err == nil {
		return string(decoded)
	}
	return str
}

func Decode(str string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(str)
}

func Encode(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}
