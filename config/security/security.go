package security

import (
	"os"
	"strconv"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/config/identity"
	"github.com/MalenkiySolovey/solovey-ui/config/storage"
)

func GetSecret() string {
	if secret := os.Getenv("SUI_SECRET"); secret != "" {
		return secret
	}
	return identity.GetName() + ":" + storage.GetDBFolderPath()
}

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
