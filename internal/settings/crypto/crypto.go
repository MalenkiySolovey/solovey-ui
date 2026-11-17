package crypto

import (
	canonical "github.com/MalenkiySolovey/solovey-backend/settings/crypto"
	"github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/util/secretbox"
)

type Candidate = canonical.Candidate

var (
	CookieKeyHKDFInfo            = canonical.CookieKeyHKDFInfo
	SettingsSecretboxKeyHKDFInfo = canonical.SettingsSecretboxKeyHKDFInfo
)

type Codec struct {
	MasterSecret  func() ([]byte, error)
	AuditFallback func(key, candidate string)
}

func (c Codec) backend() canonical.Codec {
	return canonical.Codec{MasterSecret: c.MasterSecret, AuditFallback: c.AuditFallback, Warning: logger.Warning}
}
func (c Codec) Secretbox() (*secretbox.Box, error)        { return c.backend().Secretbox() }
func (c Codec) SecretboxCandidates() ([]Candidate, error) { return c.backend().SecretboxCandidates() }
func (c Codec) SettingsSecretboxCandidates() ([]Candidate, error) {
	return c.backend().SettingsSecretboxCandidates()
}
func (c Codec) CookieKeys() ([][]byte, error) { return c.backend().CookieKeys() }
func (c Codec) EncryptString(key, value string) (string, error) {
	return c.backend().EncryptString(key, value)
}
func (c Codec) DecryptString(key, value string) (string, error) {
	return c.backend().DecryptString(key, value)
}
func (c Codec) DecryptBytes(key, value string) ([]byte, error) {
	return c.backend().DecryptBytes(key, value)
}
func ParseEnvKeyList(raw string, min int) ([][]byte, error) {
	return canonical.ParseEnvKeyList(raw, min)
}
func ParseEnvKeyMaterial(value string, min int) ([]byte, error) {
	return canonical.ParseEnvKeyMaterial(value, min)
}
func DecodeKeyMaterial(value string) ([]byte, bool) { return canonical.DecodeKeyMaterial(value) }
func DeriveHKDFKey(master, salt, info []byte) ([]byte, error) {
	return canonical.DeriveHKDFKey(master, salt, info)
}
func AppendUniqueKey(keys [][]byte, key []byte) [][]byte { return canonical.AppendUniqueKey(keys, key) }
func DecryptWithCandidate(candidates []Candidate, key, value string) (int, string, bool) {
	return canonical.DecryptWithCandidate(candidates, key, value)
}
