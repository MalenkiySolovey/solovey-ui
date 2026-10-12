package formats

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"strings"

	entitytls "github.com/MalenkiySolovey/solovey-ui/internal/entities/tls"
)

// Mihomo ECHOptions.Parse consumes one base64 ECHConfigList, not PEM. Preserve
// every public entry from single/multiple PEM lists with one vector prefix.
func clashECHConfig(value any) (string, error) {
	text, err := entitytls.PublicECHConfig(value)
	if err != nil || text == "" {
		return text, err
	}
	var lists [][]byte
	if strings.Contains(text, "-----BEGIN ") {
		rest := []byte(strings.TrimSpace(text))
		for len(rest) > 0 {
			if !strings.HasPrefix(string(rest), "-----BEGIN ECH CONFIGS-----") {
				return "", errors.New("invalid Clash public ECH payload")
			}
			block, trailing := pem.Decode(rest)
			if block == nil || block.Type != "ECH CONFIGS" || len(block.Headers) != 0 {
				return "", errors.New("invalid Clash public ECH payload")
			}
			lists = append(lists, block.Bytes)
			rest = []byte(strings.TrimSpace(string(trailing)))
		}
	} else {
		list, err := base64.StdEncoding.DecodeString(text)
		if err != nil {
			return "", errors.New("invalid Clash public ECH base64")
		}
		lists = append(lists, list)
	}
	var entries []byte
	for _, list := range lists {
		if len(list) < 2 || int(binary.BigEndian.Uint16(list)) != len(list)-2 {
			return "", errors.New("invalid Clash ECHConfigList length")
		}
		for offset := 2; offset < len(list); {
			if len(list)-offset < 4 {
				return "", errors.New("invalid Clash ECHConfig entry")
			}
			size := int(binary.BigEndian.Uint16(list[offset+2:]))
			if size > len(list)-offset-4 {
				return "", errors.New("invalid Clash ECHConfig entry")
			}
			offset += size + 4
		}
		if len(entries)+len(list)-2 > 65535 {
			return "", errors.New("Clash ECHConfigList exceeds its wire limit")
		}
		entries = append(entries, list[2:]...)
	}
	list := make([]byte, 2, len(entries)+2)
	binary.BigEndian.PutUint16(list, uint16(len(entries)))
	list = append(list, entries...)
	return base64.StdEncoding.EncodeToString(list), nil
}
