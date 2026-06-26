package ipcert

import (
	"time"

	settingsvalidation "github.com/MalenkiySolovey/solovey-ui/internal/settings/validation"
)

// RenewThreshold is the remaining-validity window below which a managed IP
// certificate is re-issued. Let's Encrypt shortlived certs live ~160h
// (~6.7 days); renewing at <72h leaves several 12h cron passes of margin.
const RenewThreshold = 72 * time.Hour

func ShouldRenew(notAfter, now time.Time) bool {
	if notAfter.IsZero() {
		return true
	}
	return notAfter.Sub(now) < RenewThreshold
}

func ValidateIssuableIP(raw string) error {
	return settingsvalidation.ValidateIssuableIP(raw)
}

func ValidateEmail(email string, required bool) error {
	return settingsvalidation.ValidateIPCertEmail(email, required)
}

func ValidatePort(port int) error {
	return settingsvalidation.ValidateIPCertPort(port)
}

func ValidateApplyTarget(value string) error {
	return settingsvalidation.ValidateIPCertApplyTarget(value)
}
