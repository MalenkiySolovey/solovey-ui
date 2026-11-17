//go:build !minimal

package settings

import (
	settingsvalidation "github.com/MalenkiySolovey/solovey-backend/settings/validation"
	canonical "github.com/MalenkiySolovey/solovey-component-telegram/settings"
)

func Validate(key, value, storedSecretMarker string) error {
	return canonical.Validate(key, value, storedSecretMarker, settingsvalidation.ValidateProxyURLValue)
}
