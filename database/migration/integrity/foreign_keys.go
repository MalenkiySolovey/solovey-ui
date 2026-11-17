package integrity

import (
	"fmt"
	canonical "github.com/MalenkiySolovey/solovey-backend/storage/integrity"
	entitytls "github.com/MalenkiySolovey/solovey-ui/internal/entities/tls"
	"gorm.io/gorm"
)

type Options = canonical.Options

func VerifyForeignKeysBeforeMigration(db *gorm.DB, options Options) error {
	return canonical.VerifyForeignKeysBeforeMigration(db, options)
}

func EnsureNoTLSForeignKeyParent(db *gorm.DB) error {
	if err := entitytls.EnsureSentinel(db); err != nil {
		return fmt.Errorf("ensure no-tls foreign key parent: %w", err)
	}
	return nil
}
