package sqlite

import (
	canonical "github.com/MalenkiySolovey/solovey-backend/storage/bootstrap"
	"gorm.io/gorm"
)

func ensureIndexes(db *gorm.DB) error    { return canonical.EnsureIndexes(db) }
func ensureSortOrders(db *gorm.DB) error { return canonical.EnsureSortOrders(db) }
