// Package uuid owns cryptographically random UUID identifiers.
package uuid

import canonical "github.com/gofrs/uuid/v5"

// RandomUUID returns a cryptographically random UUIDv4 string.
func RandomUUID() (string, error) {
	id, err := canonical.NewV4()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}
