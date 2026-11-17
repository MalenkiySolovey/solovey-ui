package crypto

import (
	"bytes"
	"encoding/base64"
	"os"
	"strings"
	"sync"

	"errors"
	"fmt"
	backendsecuritycrypto "github.com/MalenkiySolovey/solovey-ui/backend/security/crypto"
	"github.com/MalenkiySolovey/solovey-ui/backend/security/secretbox"
)

var (
	secretboxFallbackWarning sync.Once
	secretboxInvalidWarning  sync.Once
	cookieKeyFallbackWarning sync.Once
	cookieKeyInvalidWarning  sync.Once
)

var (
	CookieKeyHKDFInfo            = []byte("sui:cookie:v1")
	SettingsSecretboxKeyHKDFInfo = []byte("sui:settings-secretbox:v1")
	legacyCookieKeyHKDFSalt      = []byte("s-ui session cookie v1")
	legacyCookieKeyHKDFInfo      = []byte("cookie signing key")
)

type Candidate struct {
	Name string
	Box  *secretbox.Box
}

type Codec struct {
	Warning       func(...any)
	MasterSecret  func() ([]byte, error)
	AuditFallback func(key string, candidate string)
}

func (c Codec) Secretbox() (*secretbox.Box, error) {
	candidates, err := c.SecretboxCandidates()
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, errors.New("no secretbox key candidates")
	}
	return candidates[0].Box, nil
}

func (c Codec) SecretboxCandidates() ([]Candidate, error) {
	if key := strings.TrimSpace(os.Getenv("SUI_SECRETBOX_KEY")); key != "" {
		parsed, err := ParseEnvKeyMaterial(key, 32)
		if err == nil {
			box, err := secretbox.NewRawKey(parsed)
			if err != nil {
				return nil, err
			}
			legacyEnvBox, err := secretbox.New(parsed)
			if err != nil {
				return nil, err
			}
			candidates := []Candidate{
				{Name: "env_raw", Box: box},
				{Name: "legacy_env_hkdf", Box: legacyEnvBox},
			}
			settingsSecretCandidates, err := c.SettingsSecretboxCandidates()
			if err != nil {
				return nil, err
			}
			return append(candidates, settingsSecretCandidates...), nil
		}
		secretboxInvalidWarning.Do(func() {
			c.warning("SUI_SECRETBOX_KEY is invalid:", err, "; encrypted settings use HKDF-derived settings.secret key")
		})
	}
	secretboxFallbackWarning.Do(func() {
		c.warning("SUI_SECRETBOX_KEY is not set or invalid; encrypted settings use HKDF-derived settings.secret key")
	})
	return c.SettingsSecretboxCandidates()
}

func (c Codec) SettingsSecretboxCandidates() ([]Candidate, error) {
	if c.MasterSecret == nil {
		return nil, errors.New("settings crypto master secret provider is not configured")
	}
	secret, err := c.MasterSecret()
	if err != nil {
		return nil, err
	}
	primaryKey, err := DeriveHKDFKey(secret, nil, SettingsSecretboxKeyHKDFInfo)
	if err != nil {
		return nil, err
	}
	primaryBox, err := secretbox.NewRawKey(primaryKey)
	clear(primaryKey)
	if err != nil {
		return nil, err
	}
	legacyBox, err := secretbox.New(secret)
	if err != nil {
		return nil, err
	}
	return []Candidate{
		{Name: "settings_secretbox_v1", Box: primaryBox},
		{Name: "legacy_settings_secret", Box: legacyBox},
	}, nil
}

func (c Codec) CookieKeys() ([][]byte, error) {
	if raw := strings.TrimSpace(os.Getenv("SUI_COOKIE_KEY")); raw != "" {
		keys, err := ParseEnvKeyList(raw, 32)
		if err == nil {
			return keys, nil
		}
		cookieKeyInvalidWarning.Do(func() {
			c.warning("SUI_COOKIE_KEY is invalid:", err, "; using HKDF-derived compatibility key from settings.secret")
		})
	} else {
		cookieKeyFallbackWarning.Do(func() {
			c.warning("SUI_COOKIE_KEY is not set; using HKDF-derived compatibility key from settings.secret")
		})
	}

	if c.MasterSecret == nil {
		return nil, errors.New("settings crypto master secret provider is not configured")
	}
	secret, err := c.MasterSecret()
	if err != nil {
		return nil, err
	}
	cookieKey, err := DeriveHKDFKey(secret, nil, CookieKeyHKDFInfo)
	if err != nil {
		return nil, err
	}
	legacyCookieKey, err := DeriveHKDFKey(secret, legacyCookieKeyHKDFSalt, legacyCookieKeyHKDFInfo)
	if err != nil {
		clear(cookieKey)
		return nil, err
	}
	keys := AppendUniqueKey(nil, cookieKey)
	keys = AppendUniqueKey(keys, legacyCookieKey)
	keys = AppendUniqueKey(keys, secret)
	return keys, nil
}

func (c Codec) EncryptString(key string, value string) (string, error) {
	if value == "" || secretbox.IsEncrypted(value) {
		return value, nil
	}
	box, err := c.Secretbox()
	if err != nil {
		return "", err
	}
	return box.EncryptString(value, key)
}

func (c Codec) DecryptString(key string, value string) (string, error) {
	if value == "" || !secretbox.IsEncrypted(value) {
		return value, nil
	}
	candidates, err := c.SecretboxCandidates()
	if err != nil {
		return "", err
	}
	for i, candidate := range candidates {
		plaintext, err := candidate.Box.DecryptString(value, key)
		if err == nil {
			if i > 0 && c.AuditFallback != nil {
				c.AuditFallback(key, candidate.Name)
			}
			return plaintext, nil
		}
	}
	return "", errors.New("secret setting decrypt failed")
}

func (c Codec) DecryptBytes(key string, value string) ([]byte, error) {
	if value == "" {
		return nil, nil
	}
	if !secretbox.IsEncrypted(value) {
		return []byte(value), nil
	}
	candidates, err := c.SecretboxCandidates()
	if err != nil {
		return nil, err
	}
	for i, candidate := range candidates {
		plaintext, err := candidate.Box.DecryptBytes(value, key)
		if err == nil {
			if i > 0 && c.AuditFallback != nil {
				c.AuditFallback(key, candidate.Name)
			}
			return plaintext, nil
		}
	}
	return nil, errors.New("secret setting decrypt failed")
}

func ParseEnvKeyList(raw string, minLen int) ([][]byte, error) {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r'
	})
	keys := make([][]byte, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, err := ParseEnvKeyMaterial(part, minLen)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return nil, errors.New("empty key list")
	}
	return keys, nil
}

func ParseEnvKeyMaterial(value string, minLen int) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, errors.New("empty key material")
	}
	if decoded, ok := DecodeKeyMaterial(value); ok {
		if len(decoded) < minLen {
			return nil, fmt.Errorf("decoded key length %d is smaller than %d bytes", len(decoded), minLen)
		}
		return decoded, nil
	}
	return nil, errors.New("key material must be base64-encoded")
}

func DecodeKeyMaterial(value string) ([]byte, bool) {
	if decoded, err := base64.StdEncoding.DecodeString(value); err == nil && len(decoded) > 0 {
		return decoded, true
	}
	if decoded, err := base64.RawStdEncoding.DecodeString(value); err == nil && len(decoded) > 0 {
		return decoded, true
	}
	if decoded, err := base64.RawURLEncoding.DecodeString(value); err == nil && len(decoded) > 0 {
		return decoded, true
	}
	return nil, false
}

func DeriveHKDFKey(masterKey []byte, salt []byte, info []byte) ([]byte, error) {
	return backendsecuritycrypto.DeriveHKDFKey(masterKey, salt, info)
}

func AppendUniqueKey(keys [][]byte, key []byte) [][]byte {
	if len(key) == 0 {
		return keys
	}
	for _, existing := range keys {
		if bytes.Equal(existing, key) {
			return keys
		}
	}
	return append(keys, key)
}

func DecryptWithCandidate(candidates []Candidate, key, value string) (int, string, bool) {
	for i, candidate := range candidates {
		if plaintext, err := candidate.Box.DecryptString(value, key); err == nil {
			return i, plaintext, true
		}
	}
	return -1, "", false
}

func (c Codec) warning(args ...any) {
	if c.Warning != nil {
		c.Warning(args...)
	}
}
