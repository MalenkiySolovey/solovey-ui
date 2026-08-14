package security

import (
	"os"
	"strconv"
	"strings"
)

func GetForceCookieSecureEnv() (bool, bool, error) {
	raw := strings.TrimSpace(os.Getenv("SUI_FORCE_COOKIE_SECURE"))
	if raw == "" {
		return false, false, nil
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		return false, true, err
	}
	return enabled, true, nil
}
