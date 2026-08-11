// Package totp implements the product's bounded RFC 6238 profile.
package totp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" // #nosec G505 -- RFC 6238 interoperability profile, not collision resistance.
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	Digits      = 6
	Period      = 30 * time.Second
	Window      = 1
	SecretBytes = 20
)

var (
	ErrInvalidSecret = errors.New("invalid TOTP secret")
	ErrInvalidCode   = errors.New("invalid TOTP code")
	ErrReplay        = errors.New("TOTP counter was already accepted")
)

var base32NoPadding = base32.StdEncoding.WithPadding(base32.NoPadding)

func GenerateSecret() (string, error) {
	raw := make([]byte, SecretBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base32NoPadding.EncodeToString(raw), nil
}

func ProvisioningURI(secret, account, issuer string) (string, error) {
	if _, err := DecodeSecret(secret); err != nil {
		return "", err
	}
	issuer = strings.TrimSpace(issuer)
	account = strings.TrimSpace(account)
	if issuer == "" || account == "" {
		return "", errors.New("TOTP issuer and account are required")
	}
	values := url.Values{}
	values.Set("secret", secret)
	values.Set("issuer", issuer)
	values.Set("algorithm", "SHA1")
	values.Set("digits", strconv.Itoa(Digits))
	values.Set("period", strconv.Itoa(int(Period/time.Second)))
	return (&url.URL{
		Scheme:   "otpauth",
		Host:     "totp",
		Path:     "/" + issuer + ":" + account,
		RawQuery: values.Encode(),
	}).String(), nil
}

func Verify(secret, code string, now time.Time, lastAcceptedCounter int64) (int64, error) {
	if len(code) != Digits {
		return 0, ErrInvalidCode
	}
	for _, char := range code {
		if char < '0' || char > '9' {
			return 0, ErrInvalidCode
		}
	}
	raw, err := DecodeSecret(secret)
	if err != nil {
		return 0, err
	}
	current := now.Unix() / int64(Period/time.Second)
	matched := int64(-1)
	for offset := -Window; offset <= Window; offset++ {
		counter := current + int64(offset)
		if counter < 0 {
			continue
		}
		expected := Code(raw, uint64(counter), Digits)
		equal := subtle.ConstantTimeCompare([]byte(expected), []byte(code))
		matched = selectInt64(equal, counter, matched)
	}
	if matched < 0 {
		return 0, ErrInvalidCode
	}
	if matched <= lastAcceptedCounter {
		return 0, ErrReplay
	}
	return matched, nil
}

func Code(secret []byte, counter uint64, digits int) string {
	var message [8]byte
	binary.BigEndian.PutUint64(message[:], counter)
	mac := hmac.New(sha1.New, secret) // #nosec G401 -- mandated RFC 6238 profile.
	_, _ = mac.Write(message[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	binaryCode := (uint32(sum[offset])&0x7f)<<24 |
		(uint32(sum[offset+1])&0xff)<<16 |
		(uint32(sum[offset+2])&0xff)<<8 |
		(uint32(sum[offset+3]) & 0xff)
	modulus := uint32(math.Pow10(digits))
	return fmt.Sprintf("%0*d", digits, binaryCode%modulus)
}

func DecodeSecret(secret string) ([]byte, error) {
	secret = strings.ToUpper(strings.TrimSpace(secret))
	if secret == "" || len(secret) > 256 {
		return nil, ErrInvalidSecret
	}
	raw, err := base32NoPadding.DecodeString(secret)
	if err != nil || len(raw) < 16 || len(raw) > 64 {
		return nil, ErrInvalidSecret
	}
	return raw, nil
}

func selectInt64(choice int, whenTrue, whenFalse int64) int64 {
	mask := int64(-choice)
	return (whenTrue & mask) | (whenFalse &^ mask)
}
