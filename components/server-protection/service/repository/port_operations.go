package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

// CreatePortOperation persists immutable ownership snapshots before the
// mutation boundary. The operation lock owns idempotency; this unique key is a
// second durable guard against a partially retried request.
func (r *Repository) CreatePortOperation(ctx context.Context, item PortOperationModel) (PortOperationModel, bool, error) {
	if r == nil || r.db == nil {
		return PortOperationModel{}, false, errors.New("server-protection repository is not initialized")
	}
	var result PortOperationModel
	joined := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if item.IdempotencyKey != "" {
			err := tx.Where("idempotency_key = ?", item.IdempotencyKey).First(&result).Error
			if err == nil {
				joined = true
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		if item.Revision == 0 {
			item.Revision = 1
		}
		if err := tx.Create(&item).Error; err != nil {
			return err
		}
		result = item
		return nil
	})
	return result, joined, err
}

func (r *Repository) PortOperation(ctx context.Context, operationID string) (PortOperationModel, error) {
	if r == nil || r.db == nil {
		return PortOperationModel{}, errors.New("server-protection repository is not initialized")
	}
	var item PortOperationModel
	err := r.db.WithContext(ctx).Where("operation_id = ?", operationID).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return PortOperationModel{}, ErrRecordNotFound
	}
	return item, err
}

type FencedPortOperationUpdate struct {
	OperationID string
	Revision    int
	FromStates  []string
	ToState     string
	HealthJSON  []byte
	UpdatedAt   int64
}

func (r *Repository) UpdatePortOperationFenced(ctx context.Context, update FencedPortOperationUpdate) (PortOperationModel, error) {
	if r == nil || r.db == nil {
		return PortOperationModel{}, errors.New("server-protection repository is not initialized")
	}
	var item PortOperationModel
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		values := map[string]any{"state": update.ToState, "revision": update.Revision + 1, "updated_at": update.UpdatedAt}
		if update.HealthJSON != nil {
			values["health_result_json"] = update.HealthJSON
		}
		if update.ToState == "applied" {
			values["applied_at"] = update.UpdatedAt
		}
		if update.ToState == "rolled_back" {
			values["rolled_back_at"] = update.UpdatedAt
		}
		result := tx.Model(&PortOperationModel{}).Where("operation_id = ? AND revision = ? AND state IN ?", update.OperationID, update.Revision, update.FromStates).Updates(values)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrOperationFenced
		}
		return tx.Where("operation_id = ?", update.OperationID).First(&item).Error
	})
	return item, err
}

func (r *Repository) ListPortOperations(ctx context.Context, states []string) ([]PortOperationModel, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("server-protection repository is not initialized")
	}
	query := r.db.WithContext(ctx).Model(&PortOperationModel{})
	if len(states) > 0 {
		query = query.Where("state IN ?", states)
	}
	var items []PortOperationModel
	return items, query.Order("created_at ASC, id ASC").Find(&items).Error
}
