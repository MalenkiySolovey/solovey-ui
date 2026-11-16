// Package crypto owns pure cryptographic derivations used by Panel security.
package crypto

import (
	"crypto/sha256"
	"errors"
	"io"

	"golang.org/x/crypto/hkdf"
)

const derivedKeyLength = 32

// DeriveHKDFKey derives one domain-separated 32-byte key. The caller owns the
// returned bytes and must wipe them after use when they contain secret material.
func DeriveHKDFKey(masterKey []byte, salt []byte, info []byte) ([]byte, error) {
	if len(masterKey) == 0 {
		return nil, errors.New("empty master key")
	}
	key := make([]byte, derivedKeyLength)
	reader := hkdf.New(sha256.New, masterKey, salt, info)
	if _, err := io.ReadFull(reader, key); err != nil {
		return nil, err
	}
	return key, nil
}
