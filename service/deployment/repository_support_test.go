package deployment

import (
	"context"

	domain "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
	"gorm.io/gorm"
)

func NewRepository(db *gorm.DB) Repository { return Repository{DB: func() *gorm.DB { return db }} }

func (r Repository) Create(ctx context.Context, operation domain.Operation, event string) error {
	return r.create(ctx, operation, event, false)
}
