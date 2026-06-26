package validation

import (
	"net/mail"
	"net/netip"
	"strconv"
	"strings"

	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"github.com/MalenkiySolovey/solovey-ui/util/ssrf"
)

func ValidateIPCertSettingInput(key string, value string) error {
	switch key {
	case settingcatalog.IPCertEnabledKey:
		if _, err := strconv.ParseBool(value); err != nil {
			return common.NewError("invalid boolean setting: ", key)
		}
	case settingcatalog.IPCertTargetIPKey:
		if strings.TrimSpace(value) == "" {
			return nil
		}
		if err := ValidateIssuableIP(value); err != nil {
			return err
		}
	case settingcatalog.IPCertEmailKey:
		if err := ValidateIPCertEmail(value, false); err != nil {
			return err
		}
	case settingcatalog.IPCertChallengePortKey:
		if err := ValidateIntRange(key, value, 1, 65535); err != nil {
			return err
		}
	case settingcatalog.IPCertApplyTargetKey:
		if err := ValidateIPCertApplyTarget(value); err != nil {
			return err
		}
	}
	return nil
}

// ValidateIssuableIP rejects anything that is not a public IP literal Let's
// Encrypt could plausibly issue for. No DNS resolution is performed.
func ValidateIssuableIP(raw string) error {
	addr, err := netip.ParseAddr(strings.TrimSpace(raw))
	if err != nil {
		return common.NewError("ip cert: target must be a valid IP literal")
	}
	if ssrf.IsBlockedAddr(addr) {
		return common.NewError("ip cert: IP is private/loopback/reserved and not issuable")
	}
	return nil
}

// ValidateIPCertEmail rejects malformed ACME-account emails. An empty value is
// allowed unless required because ACME permits registration without a contact.
func ValidateIPCertEmail(email string, required bool) error {
	email = strings.TrimSpace(email)
	if email == "" {
		if required {
			return common.NewError("ip cert: email is required when auto-renew is enabled")
		}
		return nil
	}
	if len(email) > 254 {
		return common.NewError("ip cert: email is too long")
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return common.NewError("ip cert: email is not a valid address")
	}
	return nil
}

// ValidateIPCertPort bounds the HTTP-01 challenge port.
func ValidateIPCertPort(port int) error {
	if port < 1 || port > 65535 {
		return common.NewError("ip cert: challenge port must be between 1 and 65535")
	}
	return nil
}

// ValidateIPCertApplyTarget accepts "panel" or "inbound:<numericTlsId>".
func ValidateIPCertApplyTarget(value string) error {
	if value == "" || value == "panel" {
		return nil
	}
	rest, ok := strings.CutPrefix(value, "inbound:")
	if !ok {
		return common.NewError("ipCertApplyTarget must be 'panel' or 'inbound:<id>'")
	}
	if id, err := strconv.Atoi(rest); err != nil || id <= 0 {
		return common.NewError("ipCertApplyTarget inbound id must be a positive integer")
	}
	return nil
}
