package credentials

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestNativeCredentialGenerationAndMTProxyMutationAdmission(t *testing.T) {
	for _, kind := range []string{"mieru", "trusttunnel", "ssh", "mtproxy"} {
		value, err := Generate(kind)
		if err != nil || value == "" {
			t.Fatal("native credential generation failed")
		}
		if kind != "mtproxy" {
			if len(value) != 32 {
				t.Fatal("password generation changed")
			}
			continue
		}
		decoded, err := hex.DecodeString(value)
		if err != nil || len(decoded) <= 17 || decoded[0] != 0xee || bytes.Equal(decoded[1:17], make([]byte, 16)) || string(decoded[17:]) != "www.google.com" {
			t.Fatal("MTProxy generation did not include a fake-TLS key and host")
		}
		for _, representation := range []string{value, base64.RawURLEncoding.EncodeToString(decoded)} {
			raw, _ := json.Marshal(map[string]string{"intent": "replace", "value": representation})
			if err := validateReplacement(kind, raw); err != nil {
				t.Fatal("valid MTProxy representation rejected")
			}
		}
	}
	if _, err := Generate("unrecognized"); !errors.Is(err, ErrInvalid) {
		t.Fatal("unknown protocol generated a credential")
	}
	for _, value := range []string{
		"private-invalid-canary", "ee", "dd" + strings.Repeat("12", 16) + "61",
		"ee" + strings.Repeat("00", 16) + "61", "ee" + strings.Repeat("12", 16),
	} {
		raw, _ := json.Marshal(map[string]string{"intent": "replace", "value": value})
		if err := validateReplacement("mtproxy", raw); !errors.Is(err, ErrInvalid) || strings.Contains(err.Error(), value) {
			t.Fatal("invalid MTProxy mutation was admitted or disclosed")
		}
	}
	// Reject malformed replacements before creating a master or audit record,
	// including unassigned credentials which never reach a serving candidate.
	db := credentialDB(t)
	if _, err := ApplyClientField(db, 7, "mtproxy", "", json.RawMessage(`{"intent":"replace","value":"private-invalid-canary"}`)); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid MTProxy credential bypassed owner admission")
	}
}
