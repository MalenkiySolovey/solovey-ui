package operationcoordination

import (
	"context"
	"errors"
	"math"

	"gorm.io/gorm"
)

// Fence is the existing owner's durable operation generation. It is not a
// resource lock or permission to apply, resume or roll back an operation.
type Fence struct {
	OperationID string
	Revision    uint64
	State       string
}

// CommitTransition extracts the CAS/journal plumbing shared by owner
// repositories. Owners supply their row, exact columns, journal and conflict
// error. They retain state validation, locking, checkpoints and recovery.
// A failed journal write rolls back the state write in the same transaction.
func CommitTransition(ctx context.Context, db *gorm.DB, row any, fence Fence, updates map[string]any, journal any, conflict error) error {
	if ctx == nil || db == nil || row == nil || journal == nil || conflict == nil ||
		fence.OperationID == "" || fence.State == "" || fence.Revision == 0 || fence.Revision == math.MaxUint64 {
		return errors.New("operation transition contract is unavailable")
	}
	if revision, ok := updates["revision"].(uint64); !ok || revision != fence.Revision+1 {
		return errors.New("operation transition revision is invalid")
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(row).Where("operation_id = ? AND revision = ? AND state = ?", fence.OperationID, fence.Revision, fence.State).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return conflict
		}
		return tx.Create(journal).Error
	})
}
