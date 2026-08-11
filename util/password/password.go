// Package password owns the bounded password policy and password-hash format.
package password

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

const (
	PolicyVersion = 1
	BlocklistID   = "common-passwords-v1"

	MinCharacters = 15
	MaxUTF8Bytes  = 256

	ArgonVersion     = argon2.Version
	ArgonMemoryKiB   = 64 * 1024
	ArgonTime        = 3
	ArgonParallelism = 1
	ArgonSaltBytes   = 16
	ArgonKeyBytes    = 32

	bcryptPrefix = "bcrypt:"
)

var (
	ErrBusy              = errors.New("password derivation capacity exhausted")
	ErrInvalidHash       = errors.New("invalid password hash")
	ErrUnsupportedHash   = errors.New("unsupported password hash")
	ErrPasswordTooShort  = errors.New("password must contain at least 15 Unicode characters")
	ErrPasswordTooLong   = errors.New("password exceeds 256 UTF-8 bytes")
	ErrPasswordUTF8      = errors.New("password must be valid UTF-8")
	ErrPasswordBlocklist = errors.New("password appears in the local common-password blocklist")
)

//go:embed common-passwords-v1.txt
var commonPasswordsText string

var commonPasswords = loadCommonPasswords(commonPasswordsText)

// Slots is deliberately process-wide: password verification and generation
// share the same two Argon2id memory reservations and never build a wait queue.
var slots = make(chan struct{}, 2)

type Parameters struct {
	MemoryKiB   uint32
	Time        uint32
	Parallelism uint8
	SaltBytes   uint32
	KeyBytes    uint32
}

func CurrentParameters() Parameters {
	return Parameters{
		MemoryKiB:   ArgonMemoryKiB,
		Time:        ArgonTime,
		Parallelism: ArgonParallelism,
		SaltBytes:   ArgonSaltBytes,
		KeyBytes:    ArgonKeyBytes,
	}
}

func ValidateNew(value string) error {
	if !utf8.ValidString(value) {
		return ErrPasswordUTF8
	}
	if utf8.RuneCountInString(value) < MinCharacters {
		return ErrPasswordTooShort
	}
	if len([]byte(value)) > MaxUTF8Bytes {
		return ErrPasswordTooLong
	}
	if _, blocked := commonPasswords[normalizeForBlocklist(value)]; blocked {
		return ErrPasswordBlocklist
	}
	return nil
}

func Hash(ctx context.Context, plaintext string) (string, error) {
	if len([]byte(plaintext)) > MaxUTF8Bytes {
		return "", ErrPasswordTooLong
	}
	release, err := acquire(ctx)
	if err != nil {
		return "", err
	}
	defer release()

	salt := make([]byte, ArgonSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(plaintext), salt, ArgonTime, ArgonMemoryKiB, ArgonParallelism, ArgonKeyBytes)
	defer wipe(key)
	return encodePHC(salt, key), nil
}

func Verify(ctx context.Context, encoded, plaintext string) (valid bool, needsRehash bool, err error) {
	if len([]byte(plaintext)) > MaxUTF8Bytes {
		return false, false, nil
	}
	switch {
	case strings.HasPrefix(encoded, "$argon2"):
		salt, expected, err := parsePHC(encoded)
		if err != nil {
			return false, false, err
		}
		release, err := acquire(ctx)
		if err != nil {
			return false, false, err
		}
		defer release()
		actual := argon2.IDKey([]byte(plaintext), salt, ArgonTime, ArgonMemoryKiB, ArgonParallelism, uint32(len(expected)))
		defer wipe(actual)
		return subtle.ConstantTimeCompare(actual, expected) == 1, false, nil
	case strings.HasPrefix(encoded, bcryptPrefix):
		hash := strings.TrimPrefix(encoded, bcryptPrefix)
		return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext)) == nil, true, nil
	case isRawBcrypt(encoded):
		return bcrypt.CompareHashAndPassword([]byte(encoded), []byte(plaintext)) == nil, true, nil
	default:
		// Compatibility for the oldest supported backups. Startup adaptation
		// rewrites this form, but comparison remains constant-time until then.
		return subtle.ConstantTimeCompare([]byte(encoded), []byte(plaintext)) == 1, true, nil
	}
}

// EqualizeUnknown performs exactly one bounded current-algorithm derivation.
func EqualizeUnknown(ctx context.Context, plaintext string) error {
	if len([]byte(plaintext)) > MaxUTF8Bytes {
		plaintext = plaintext[:MaxUTF8Bytes]
	}
	release, err := acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	salt := []byte("sui-dummy-salt-v1")
	key := argon2.IDKey([]byte(plaintext), salt, ArgonTime, ArgonMemoryKiB, ArgonParallelism, ArgonKeyBytes)
	wipe(key)
	return nil
}

func IsEncoded(value string) bool {
	return strings.HasPrefix(value, "$argon2id$") || strings.HasPrefix(value, bcryptPrefix) || isRawBcrypt(value)
}

func IsCurrent(value string) bool {
	_, _, err := parsePHC(value)
	return err == nil
}

func encodePHC(salt, key []byte) string {
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		ArgonVersion,
		ArgonMemoryKiB,
		ArgonTime,
		ArgonParallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
}

func parsePHC(encoded string) ([]byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return nil, nil, ErrInvalidHash
	}
	version, ok := strings.CutPrefix(parts[2], "v=")
	if !ok {
		return nil, nil, ErrInvalidHash
	}
	parsedVersion, err := strconv.Atoi(version)
	if err != nil || parsedVersion != ArgonVersion {
		return nil, nil, ErrUnsupportedHash
	}
	memory, iterations, parallelism, err := parseParameterTuple(parts[3])
	if err != nil {
		return nil, nil, err
	}
	// Refuse attacker-selected resource costs before any derivation. The policy
	// has one accepted profile; future profiles require an explicit parser update.
	if memory != ArgonMemoryKiB || iterations != ArgonTime || parallelism != ArgonParallelism {
		return nil, nil, ErrUnsupportedHash
	}
	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil || len(salt) != ArgonSaltBytes {
		return nil, nil, ErrInvalidHash
	}
	key, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil || len(key) != ArgonKeyBytes {
		return nil, nil, ErrInvalidHash
	}
	return salt, key, nil
}

func parseParameterTuple(value string) (uint32, uint32, uint8, error) {
	parts := strings.Split(value, ",")
	if len(parts) != 3 {
		return 0, 0, 0, ErrInvalidHash
	}
	values := make(map[string]uint64, 3)
	for _, part := range parts {
		key, raw, ok := strings.Cut(part, "=")
		if !ok || raw == "" {
			return 0, 0, 0, ErrInvalidHash
		}
		parsed, err := strconv.ParseUint(raw, 10, 32)
		if err != nil {
			return 0, 0, 0, ErrInvalidHash
		}
		if _, duplicate := values[key]; duplicate {
			return 0, 0, 0, ErrInvalidHash
		}
		values[key] = parsed
	}
	memory, memoryOK := values["m"]
	iterations, timeOK := values["t"]
	parallelism, parallelismOK := values["p"]
	if !memoryOK || !timeOK || !parallelismOK || len(values) != 3 ||
		memory > ArgonMemoryKiB || iterations > ArgonTime || parallelism > ArgonParallelism ||
		memory == 0 || iterations == 0 || parallelism == 0 {
		return 0, 0, 0, ErrUnsupportedHash
	}
	return uint32(memory), uint32(iterations), uint8(parallelism), nil
}

func acquire(ctx context.Context) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case slots <- struct{}{}:
		return func() { <-slots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return nil, ErrBusy
	}
}

func isRawBcrypt(value string) bool {
	return strings.HasPrefix(value, "$2a$") || strings.HasPrefix(value, "$2b$") || strings.HasPrefix(value, "$2y$")
}

func loadCommonPasswords(text string) map[string]struct{} {
	loaded := make(map[string]struct{})
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		loaded[normalizeForBlocklist(line)] = struct{}{}
	}
	return loaded
}

func normalizeForBlocklist(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func wipe(value []byte) {
	for i := range value {
		value[i] = 0
	}
}
