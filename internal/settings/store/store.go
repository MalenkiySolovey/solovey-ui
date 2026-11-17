package store

import (
	canonical "github.com/MalenkiySolovey/solovey-ui/backend/settings/store"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	settingscrypto "github.com/MalenkiySolovey/solovey-ui/internal/settings/crypto"
	"gorm.io/gorm"
)

type ResealCodec = canonical.ResealCodec

func Codec(db *gorm.DB) settingscrypto.Codec {
	return settingscrypto.Codec{MasterSecret: canonical.Codec(db).MasterSecret}
}
func EnsureMasterSecret(db *gorm.DB) ([]byte, error) { return canonical.EnsureMasterSecret(db) }
func ResealSecretSettings(db *gorm.DB, codec ResealCodec, keys map[string]struct{}) (int, error) {
	return canonical.ResealSecretSettings(db, codec, keys)
}
func Find(db *gorm.DB, key string) (*model.Setting, error) { return canonical.Find(db, key) }
func List(db *gorm.DB) ([]*model.Setting, error)           { return canonical.List(db) }
func EnsureDefaults(db *gorm.DB, keys []string, value func(string) (string, bool)) error {
	return canonical.EnsureDefaults(db, keys, value)
}
func DeleteAll(db *gorm.DB) error { return canonical.DeleteAll(db) }
func InsertIfMissing(db *gorm.DB, key, value string) error {
	return canonical.InsertIfMissing(db, key, value)
}
func UpsertValue(db *gorm.DB, key, value string) error { return canonical.UpsertValue(db, key, value) }
