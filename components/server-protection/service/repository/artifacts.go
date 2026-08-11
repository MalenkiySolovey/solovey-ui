package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

var protectedArtifactStates = []string{"prepared", "applying", "health", "applied", "health_failed", "rolling_back", "rollback_failed", "reconcile_required", "lock_suspect"}

func (r *Repository) SaveArtifact(ctx context.Context, item *ArtifactModel) error {
	if r == nil || r.db == nil {
		return errors.New("server-protection repository is not initialized")
	}
	return r.db.WithContext(ctx).Save(item).Error
}

func (r *Repository) ArtifactByOperation(ctx context.Context, operationID string) (ArtifactModel, error) {
	if r == nil || r.db == nil {
		return ArtifactModel{}, errors.New("server-protection repository is not initialized")
	}
	var item ArtifactModel
	err := r.db.WithContext(ctx).Where("operation_id = ?", operationID).Order("created_at DESC, id DESC").First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ArtifactModel{}, ErrRecordNotFound
	}
	return item, err
}

func (r *Repository) ListArtifacts(ctx context.Context) ([]ArtifactModel, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("server-protection repository is not initialized")
	}
	var items []ArtifactModel
	err := r.db.WithContext(ctx).Order("created_at DESC, id DESC").Find(&items).Error
	return items, err
}

func (r *Repository) ProtectedArtifactOperations(ctx context.Context) (map[string]string, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("server-protection repository is not initialized")
	}
	result := make(map[string]string)
	var locks []OperationLockModel
	if err := r.db.WithContext(ctx).Where("state IN ?", protectedArtifactStates).Find(&locks).Error; err != nil {
		return nil, err
	}
	for _, lock := range locks {
		result[lock.OperationID] = lock.State
	}
	var operations []PortOperationModel
	if err := r.db.WithContext(ctx).Where("state IN ?", protectedArtifactStates).Find(&operations).Error; err != nil {
		return nil, err
	}
	for _, operation := range operations {
		result[operation.OperationID] = operation.State
	}
	return result, nil
}

func (r *Repository) DeleteArtifact(ctx context.Context, id uint) error {
	if r == nil || r.db == nil {
		return errors.New("server-protection repository is not initialized")
	}
	return r.db.WithContext(ctx).Delete(&ArtifactModel{}, id).Error
}

func (r *Repository) MarkOperationRecovery(ctx context.Context, operationID string, attempts int, at int64, code string) error {
	if r == nil || r.db == nil {
		return errors.New("server-protection repository is not initialized")
	}
	return r.db.WithContext(ctx).Model(&OperationLockModel{}).Where("operation_id = ?", operationID).
		Updates(map[string]any{"recovery_attempts": attempts, "last_recovery_at": at, "recovery_error_code": code, "updated_at": at}).Error
}
