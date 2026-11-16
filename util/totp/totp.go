// Package totp forwards the historical Product import to the canonical Backend
// RFC 6238 implementation.
package totp

import (
	"time"

	backendtotp "github.com/MalenkiySolovey/solovey-ui/backend/security/totp"
)

const (
	Digits      = backendtotp.Digits
	Period      = backendtotp.Period
	Window      = backendtotp.Window
	SecretBytes = backendtotp.SecretBytes
)

var (
	ErrInvalidSecret = backendtotp.ErrInvalidSecret
	ErrInvalidCode   = backendtotp.ErrInvalidCode
	ErrReplay        = backendtotp.ErrReplay
)

func GenerateSecret() (string, error) { return backendtotp.GenerateSecret() }
func ProvisioningURI(secret, account, issuer string) (string, error) {
	return backendtotp.ProvisioningURI(secret, account, issuer)
}
func Verify(secret, code string, now time.Time, lastAcceptedCounter int64) (int64, error) {
	return backendtotp.Verify(secret, code, now, lastAcceptedCounter)
}
func Code(secret []byte, counter uint64, digits int) string {
	return backendtotp.Code(secret, counter, digits)
}
func DecodeSecret(secret string) ([]byte, error) { return backendtotp.DecodeSecret(secret) }
