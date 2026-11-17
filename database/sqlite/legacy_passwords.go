package sqlite

import "gorm.io/gorm"

func rehashLegacyPasswords(tx *gorm.DB) error { return storageInitializer().RehashLegacyPasswords(tx) }
