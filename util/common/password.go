package common

import (
	"context"

	passwordhash "github.com/MalenkiySolovey/solovey-ui/util/password"
)

// EqualizeLoginTiming performs one bounded current-algorithm derivation so an
// unknown account follows the same expensive-work contour as a known account.
func EqualizeLoginTiming(password string) {
	_ = passwordhash.EqualizeUnknown(context.Background(), password)
}

func HashPassword(password string) (string, error) {
	return passwordhash.Hash(context.Background(), password)
}

func IsPasswordHash(password string) bool {
	return passwordhash.IsEncoded(password)
}

func CheckPassword(storedPassword string, password string) (bool, bool) {
	valid, migrate, _ := passwordhash.Verify(context.Background(), storedPassword, password)
	return valid, migrate
}
