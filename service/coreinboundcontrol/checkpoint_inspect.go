package coreinboundcontrol

import (
	"context"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/gorm"
)

// InspectCheckpoint performs the read-only integrity and current-revision
// check required before restart recovery chooses resume, rollback, or manual
// reconciliation. It intentionally does not acquire the mutation coordinator.
func (s *Service) InspectCheckpoint(ctx context.Context, request InspectCheckpointRequestV1) (CheckpointStatusV1, error) {
	if err := ctx.Err(); err != nil {
		return CheckpointStatusV1{}, adapterFailure(ErrorCancelled)
	}
	if s == nil || s.db == nil {
		return CheckpointStatusV1{}, adapterFailure(ErrorDatabase)
	}
	var status CheckpointStatusV1
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var loadErr error
		status, loadErr = s.inspectCheckpointTx(tx, request.CheckpointID)
		return loadErr
	})
	if err != nil {
		return CheckpointStatusV1{}, normalizeAdapterError(err, ErrorCheckpointMissing)
	}
	return status, nil
}

// FindCheckpoint performs a bounded, read-only lookup by the exact
// core-generated preview digest. Multiple matches are treated as ambiguous;
// callers must fail closed instead of guessing recovery authority.
func (s *Service) FindCheckpoint(ctx context.Context, request FindCheckpointRequestV1) (CheckpointStatusV1, error) {
	if err := ctx.Err(); err != nil {
		return CheckpointStatusV1{}, adapterFailure(ErrorCancelled)
	}
	if s == nil || s.db == nil {
		return CheckpointStatusV1{}, adapterFailure(ErrorDatabase)
	}
	if !safeOpaque(request.PreviewDigest) || len(request.PreviewDigest) != 64 {
		return CheckpointStatusV1{}, adapterFailure(ErrorCheckpointMissing)
	}
	var status CheckpointStatusV1
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []model.InboundFallbackCheckpoint
		if err := tx.Where("state <> ?", checkpointStateReleased).
			Order("created_at_unix DESC, id DESC").Limit(maxRecoverableCheckpoints + 1).Find(&rows).Error; err != nil {
			return adapterFailure(ErrorDatabase)
		}
		if len(rows) > maxRecoverableCheckpoints {
			return adapterFailure(ErrorMutationConflict)
		}
		found := 0
		for _, row := range rows {
			loaded, payload, err := loadCheckpoint(tx, row.ID)
			if err != nil {
				return err
			}
			if payload.PreviewDigest != request.PreviewDigest {
				continue
			}
			found++
			if found > 1 {
				return adapterFailure(ErrorMutationConflict)
			}
			status, err = s.checkpointStatusTx(tx, loaded, payload)
			if err != nil {
				return err
			}
		}
		if found == 0 {
			return adapterFailure(ErrorCheckpointMissing)
		}
		return nil
	})
	if err != nil {
		return CheckpointStatusV1{}, normalizeAdapterError(err, ErrorCheckpointMissing)
	}
	return status, nil
}

func (s *Service) inspectCheckpointTx(tx *gorm.DB, checkpointID string) (CheckpointStatusV1, error) {
	row, payload, err := loadCheckpoint(tx, checkpointID)
	if err != nil {
		return CheckpointStatusV1{}, err
	}
	return s.checkpointStatusTx(tx, row, payload)
}

func (s *Service) checkpointStatusTx(tx *gorm.DB, row model.InboundFallbackCheckpoint, payload checkpointPayloadV1) (CheckpointStatusV1, error) {
	_, _, snapshot, err := s.loadInboundState(tx, payload.InboundDatabaseID)
	if err != nil {
		return CheckpointStatusV1{}, err
	}
	state, ok := publicCheckpointState(row.State)
	if !ok {
		return CheckpointStatusV1{}, adapterFailure(ErrorCheckpointTampered)
	}
	status := CheckpointStatusV1{
		CheckpointID: row.ID, State: state, PreviewDigest: payload.PreviewDigest,
		IntegrityDigest: row.IntegrityDigest, BeforeConfigurationRevision: payload.BeforeConfigurationRevision,
		ExpectedAfterRevision: payload.ExpectedAfterRevision, CurrentConfigurationRevision: snapshot.ConfigurationRevision,
		CurrentEffectiveRevision: snapshot.Effective.Revision, ProofDigest: row.ProofDigest,
	}
	if row.State == checkpointStatePrepared {
		status.UncommittedReleaseProof = terminalProofDigest(row.ID, checkpointStatePrepared, payload.BeforeConfigurationRevision)
	}
	if snapshot.ConfigurationRevision == payload.BeforeConfigurationRevision && row.State != checkpointStateReleased {
		status.DetachedReleaseProof = terminalProofDigest(row.ID, string(CheckpointProofDurablyAdopted), snapshot.ConfigurationRevision)
	}
	return status, nil
}

func publicCheckpointState(value string) (CheckpointStateV1, bool) {
	switch value {
	case checkpointStatePrepared:
		return CheckpointStatePrepared, true
	case checkpointStateCommitted:
		return CheckpointStateCommitted, true
	case checkpointStateRuntimeApplied:
		return CheckpointStateRuntimeApplied, true
	case checkpointStateEffectiveVerified:
		return CheckpointStateEffectiveVerified, true
	case checkpointStateRestoredCommitted:
		return CheckpointStateRestoredCommitted, true
	case checkpointStateRestoreFailed:
		return CheckpointStateRestoreFailed, true
	case checkpointStateRestoredVerified:
		return CheckpointStateRestoredVerified, true
	case checkpointStateReleased:
		return CheckpointStateReleased, true
	default:
		return "", false
	}
}
