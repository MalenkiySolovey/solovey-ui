package store

import (
	"errors"

	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"gorm.io/gorm"
)

// EnsureMasterSecret uses the same settings master lifecycle for live settings
// and admitted candidate transactions. Read-only previews must not call it.
// Insert-if-missing prevents concurrent creators from replacing an existing key.
func EnsureMasterSecret(db *gorm.DB) ([]byte, error) {
	if db == nil {
		return nil, errors.New("settings master database is unavailable")
	}
	row, err := Find(db, settingcatalog.SecretKey)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		seed, err := common.SecureRandom(32)
		if err != nil {
			return nil, err
		}
		if err := InsertIfMissing(db, settingcatalog.SecretKey, seed); err != nil {
			return nil, err
		}
		row, err = Find(db, settingcatalog.SecretKey)
		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	return []byte(row.Value), nil
}
