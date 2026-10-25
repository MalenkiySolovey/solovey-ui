package store

import (
	"errors"

	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
	settingscrypto "github.com/MalenkiySolovey/solovey-ui/internal/settings/crypto"
	"gorm.io/gorm"
)

// Codec reads the approved key lifecycle from this exact candidate database.
// It never creates a master on read or falls through to a published global DB.
func Codec(db *gorm.DB) settingscrypto.Codec {
	return settingscrypto.Codec{MasterSecret: func() ([]byte, error) {
		if db == nil {
			return nil, errors.New("master-key database is unavailable")
		}
		row, err := Find(db, settingcatalog.SecretKey)
		if err != nil || row.Value == "" {
			return nil, errors.New("master-key database is unavailable")
		}
		return []byte(row.Value), nil
	}}
}
