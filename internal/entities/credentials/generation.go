package credentials

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"

	"github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds/clientfacts"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

// Generate belongs to the existing Users assignment/rotation operation. The
// MTProto wire credential includes its fake-TLS hostname, not a bare password.
func Generate(protocol string) (string, error) {
	fact, known := clientfacts.Resolve(protocol)
	if !known || fact.SecretField == "" {
		return "", ErrInvalid
	}
	if protocol != "mtproxy" {
		return common.SecureRandom(32)
	}
	key := make([]byte, 16)
	defer common.WipeBytes(key)
	if _, err := rand.Read(key); err != nil {
		return "", ErrInvalid
	}
	return "ee" + hex.EncodeToString(key) + hex.EncodeToString([]byte("www.google.com")), nil
}

func validateReplacement(protocol string, mutation json.RawMessage) error {
	if protocol != "mtproxy" {
		return nil
	}
	var intent struct{ Intent, Value string }
	if json.Unmarshal(mutation, &intent) != nil || intent.Intent != "replace" {
		// Common SecretRef performs strict mutation/reference validation.
		return nil
	}
	decoded, err := hex.DecodeString(intent.Value)
	if err != nil {
		decoded, err = base64.RawURLEncoding.DecodeString(intent.Value)
	}
	defer common.WipeBytes(decoded)
	if err != nil || len(decoded) < 18 || decoded[0] != 0xee || bytes.Equal(decoded[1:17], make([]byte, 16)) {
		return ErrInvalid
	}
	return nil
}
