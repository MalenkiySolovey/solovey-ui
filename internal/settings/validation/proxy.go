package validation

import "github.com/MalenkiySolovey/solovey-ui/util/ssrf"

func ValidateProxyURLValue(value string, storedSecretMarker string) error {
	if value == "" || value == storedSecretMarker {
		return nil
	}
	return ssrf.ValidateProxyURL(value)
}
