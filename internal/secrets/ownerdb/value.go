// Package ownerdb composes Product warning logging for Backend-owned rows.
package ownerdb

import (
	"context"
	"encoding/json"
	canonical "github.com/MalenkiySolovey/solovey-backend/secrets/ownerdb"
	"github.com/MalenkiySolovey/solovey-host/secretref"
	"github.com/MalenkiySolovey/solovey-ui/logger"
	"gorm.io/gorm"
)

func Reference(owner string, row uint, field string) (secretref.Reference, error) {
	return canonical.Reference(owner, row, field)
}
func View(ref secretref.Reference, captured string) secretref.View {
	return canonical.View(ref, captured)
}
func Apply(db *gorm.DB, ref secretref.Reference, captured string, mutation json.RawMessage) (string, error) {
	return canonical.Apply(db, ref, captured, mutation, logger.Warning)
}
func Access(ctx context.Context, db *gorm.DB, request secretref.Access, captured string, use func([]byte) error) (secretref.View, error) {
	return canonical.Access(ctx, db, request, captured, use, logger.Warning)
}
