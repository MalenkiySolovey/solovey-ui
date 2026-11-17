//go:build !minimal

package settings

import (
	canonical "github.com/MalenkiySolovey/solovey-component-telegram/settings"
	settingsvalidation "github.com/MalenkiySolovey/solovey-backend/settings/validation"
)

func Validate(key, value, storedSecretMarker string) error {
	return canonical.Validate(key, value, storedSecretMarker, settingsvalidation.ValidateProxyURLValue)
}
