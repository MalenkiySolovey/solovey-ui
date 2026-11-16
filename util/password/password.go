// Package password forwards the historical Product import to the canonical
// Backend password policy and hash implementation.
package password

import (
	"context"

	backendpassword "github.com/MalenkiySolovey/solovey-ui/backend/security/password"
)

const (
	PolicyVersion   = backendpassword.PolicyVersion
	BlocklistID     = backendpassword.BlocklistID
	MinCharacters   = backendpassword.MinCharacters
	MaxUTF8Bytes    = backendpassword.MaxUTF8Bytes
	ArgonVersion    = backendpassword.ArgonVersion
	ArgonMemoryKiB  = backendpassword.ArgonMemoryKiB
	ArgonTime       = backendpassword.ArgonTime
	ArgonParallelism = backendpassword.ArgonParallelism
	ArgonSaltBytes  = backendpassword.ArgonSaltBytes
	ArgonKeyBytes   = backendpassword.ArgonKeyBytes
)

var (
	ErrBusy              = backendpassword.ErrBusy
	ErrInvalidHash       = backendpassword.ErrInvalidHash
	ErrUnsupportedHash   = backendpassword.ErrUnsupportedHash
	ErrPasswordTooShort  = backendpassword.ErrPasswordTooShort
	ErrPasswordTooLong   = backendpassword.ErrPasswordTooLong
	ErrPasswordUTF8      = backendpassword.ErrPasswordUTF8
	ErrPasswordBlocklist = backendpassword.ErrPasswordBlocklist
)

func ValidateNew(value string) error { return backendpassword.ValidateNew(value) }
func Hash(ctx context.Context, plaintext string) (string, error) {
	return backendpassword.Hash(ctx, plaintext)
}
func Verify(ctx context.Context, encoded, plaintext string) (bool, bool, error) {
	return backendpassword.Verify(ctx, encoded, plaintext)
}
func EqualizeUnknown(ctx context.Context, plaintext string) error {
	return backendpassword.EqualizeUnknown(ctx, plaintext)
}
func IsEncoded(value string) bool { return backendpassword.IsEncoded(value) }
func IsCurrent(value string) bool { return backendpassword.IsCurrent(value) }