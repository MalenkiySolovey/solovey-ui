package telegram

import (
	"testing"

	paidcore "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/paid"
	paidtest "github.com/MalenkiySolovey/solovey-ui/paidsub/internal/testutil"
	"gorm.io/gorm"
)

func openTestDB(t *testing.T) *gorm.DB {
	return paidtest.OpenDatabase(t)
}

func ensureTestSchema(db *gorm.DB) error {
	return paidcore.EnsureSchema(db)
}
