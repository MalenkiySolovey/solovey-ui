package service

import "gorm.io/gorm"

// ReadWebTLSFiles is Backend composition for a publication already using an
// owner database operation. It uses the existing Settings owner and the same
// transaction, including one-connection SQLite. It is not a public Host port;
// feature-facing HTTP ports expose only bounded TLS facts.
func ReadWebTLSFiles(database *gorm.DB) (string, string, error) {
	settings := SettingService{database: database}
	certificate, err := settings.GetCertFile()
	if err != nil {
		return "", "", err
	}
	key, err := settings.GetKeyFile()
	return certificate, key, err
}
