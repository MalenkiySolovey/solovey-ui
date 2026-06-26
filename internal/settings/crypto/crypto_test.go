package crypto

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func TestCodecEncryptDecryptRoundTrip(t *testing.T) {
	codec := Codec{
		MasterSecret: func() ([]byte, error) {
			return []byte("test-master-key-material-32-bytes!!"), nil
		},
	}
	encrypted, err := codec.EncryptString("token", "secret-value")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if encrypted == "secret-value" {
		t.Fatal("value was not encrypted")
	}
	decrypted, err := codec.DecryptString("token", encrypted)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if decrypted != "secret-value" {
		t.Fatalf("decrypted = %q, want secret-value", decrypted)
	}
}

func TestParseEnvKeyListSupportsRolloverSeparators(t *testing.T) {
	key1 := []byte("0123456789abcdef0123456789abcdef")
	key2 := []byte("abcdef0123456789abcdef0123456789")
	raw := base64.RawURLEncoding.EncodeToString(key1) + ";" + base64.StdEncoding.EncodeToString(key2)

	keys, err := ParseEnvKeyList(raw, 32)
	if err != nil {
		t.Fatalf("parse key list: %v", err)
	}
	if len(keys) != 2 || !bytes.Equal(keys[0], key1) || !bytes.Equal(keys[1], key2) {
		t.Fatalf("unexpected parsed keys: %#v", keys)
	}
}

func TestDeriveHKDFKeyIsDomainSeparated(t *testing.T) {
	master := []byte("test-master-key-material-32-bytes!!")
	cookie, err := DeriveHKDFKey(master, nil, CookieKeyHKDFInfo)
	if err != nil {
		t.Fatalf("derive cookie: %v", err)
	}
	settings, err := DeriveHKDFKey(master, nil, SettingsSecretboxKeyHKDFInfo)
	if err != nil {
		t.Fatalf("derive settings: %v", err)
	}
	if bytes.Equal(cookie, settings) {
		t.Fatal("cookie and settings keys should be domain-separated")
	}
}
