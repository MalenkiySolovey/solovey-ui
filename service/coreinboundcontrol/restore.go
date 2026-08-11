package coreinboundcontrol

import (
	"context"
	"encoding/json"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/gorm"
)

func (s *Service) RestoreCheckpoint(ctx context.Context, request RestoreCheckpointRequestV1) (RestoreCheckpointResultV1, error) {
	var result RestoreCheckpointResultV1
	err := s.runMutation(ctx, func() error {
		if err := ctx.Err(); err != nil {
			return adapterFailure(ErrorCancelled)
		}
		tx := s.db.WithContext(ctx).Begin()
		if tx.Error != nil {
			return adapterFailure(ErrorDatabase)
		}
		committed := false
		defer func() {
			if !committed {
				_ = tx.Rollback().Error
			}
		}()
		row, payload, err := loadCheckpoint(tx, request.CheckpointID)
		if err != nil {
			return err
		}
		if row.State == checkpointStateRestoreFailed || row.State == checkpointStateRestoredCommitted {
			return adapterFailure(ErrorRestoreFailure)
		}
		if row.State != checkpointStateCommitted && row.State != checkpointStateRuntimeApplied && row.State != checkpointStateEffectiveVerified {
			return adapterFailure(ErrorMutationConflict)
		}
		if request.ExpectedCurrentRevision != payload.ExpectedAfterRevision {
			return adapterFailure(ErrorRestoreDrift)
		}
		inbound, referenceCount, snapshot, err := s.loadInboundState(tx, payload.InboundDatabaseID)
		if err != nil {
			return err
		}
		if snapshot.ConfigurationRevision != payload.ExpectedAfterRevision {
			return adapterFailure(ErrorReconcileRequired)
		}
		candidate, changed, expectedOptionsDigest, err := s.restoreCandidate(ctx, tx, inbound, referenceCount, payload)
		if err != nil {
			return err
		}
		restored := buildSnapshotWithRuntimeDigest(candidate, referenceCount, s.identity, nil, 0, expectedOptionsDigest)
		if restored.ConfigurationRevision != payload.BeforeConfigurationRevision {
			return adapterFailure(ErrorRestoreFailure)
		}
		if err = s.persistCandidate(ctx, tx, payload, &candidate, changed); err != nil {
			return adapterFailure(ErrorRestoreFailure)
		}
		if err = ctx.Err(); err != nil {
			return adapterFailure(ErrorCancelled)
		}
		update := tx.Model(&model.InboundFallbackCheckpoint{}).
			Where("id = ? AND state = ?", row.ID, row.State).
			Updates(map[string]any{"state": checkpointStateRestoredCommitted, "after_revision": restored.ConfigurationRevision})
		if update.Error != nil || update.RowsAffected != 1 {
			return adapterFailure(ErrorRestoreFailure)
		}
		if err = tx.Commit().Error; err != nil {
			return adapterFailure(ErrorAmbiguousResult)
		}
		committed = true
		if err = ctx.Err(); err != nil {
			return adapterFailure(ErrorAmbiguousResult)
		}
		if s.mutation.Hooks != nil {
			s.mutation.Hooks.AfterCommit(payload.Variant, payload.InboundDatabaseID)
		}
		observation, err := s.mutation.Runtime.ApplyInbound(ctx, payload.InboundDatabaseID)
		if err != nil || validateRuntimeObservation(candidate, observation, expectedOptionsDigest) != nil {
			s.markRestoreFailed(row.ID)
			if ctx.Err() != nil {
				return adapterFailure(ErrorAmbiguousResult)
			}
			return adapterFailure(ErrorRestoreFailure)
		}
		effective := effectiveRevision(payload.RuntimeIdentityRevision, restored.ConfigurationRevision, observation)
		proof := terminalProofDigest(row.ID, checkpointStateRestoredVerified, effective)
		stateUpdate := s.db.WithContext(ctx).Model(&model.InboundFallbackCheckpoint{}).
			Where("id = ? AND state = ?", row.ID, checkpointStateRestoredCommitted).
			Updates(map[string]any{
				"state": checkpointStateRestoredVerified, "effective_revision": effective, "proof_digest": proof,
			})
		if stateUpdate.Error != nil || stateUpdate.RowsAffected != 1 {
			return adapterFailure(ErrorAmbiguousResult)
		}
		result = RestoreCheckpointResultV1{
			CheckpointID: row.ID, RestoredConfigurationRevision: restored.ConfigurationRevision,
			RestoredEffectiveRevision: effective, ProofDigest: proof,
		}
		return nil
	})
	if err != nil {
		return RestoreCheckpointResultV1{}, normalizeAdapterError(err, ErrorRestoreFailure)
	}
	return result, nil
}

func (s *Service) restoreCandidate(ctx context.Context, tx *gorm.DB, inbound model.Inbound, referenceCount int64, payload checkpointPayloadV1) (model.Inbound, []ChangedFieldV1, string, error) {
	candidate := inbound
	candidate.Options = append(json.RawMessage(nil), inbound.Options...)
	if inbound.Tls != nil {
		tlsCopy := *inbound.Tls
		tlsCopy.Server = append(json.RawMessage(nil), inbound.Tls.Server...)
		candidate.Tls = &tlsCopy
	}
	var changed []ChangedFieldV1
	var err error
	switch payload.Variant {
	case FallbackPatchVLESSRealityHandshakeTCP:
		if candidate.Tls == nil || referenceCount != 1 || payload.PreviousTarget == nil {
			return model.Inbound{}, nil, "", adapterFailure(ErrorRestoreFailure)
		}
		candidate.Tls.Server, _, err = patchRealityHandshake(candidate.Tls.Server, *payload.PreviousTarget)
		changed = []ChangedFieldV1{{Path: "tls.reality.handshake.server"}, {Path: "tls.reality.handshake.server_port"}}
	case FallbackPatchTrojanDefaultTCP:
		if payload.PreviousTarget == nil {
			return model.Inbound{}, nil, "", adapterFailure(ErrorRestoreFailure)
		}
		candidate.Options, _, err = patchTrojanFallback(candidate.Options, *payload.PreviousTarget)
		changed = []ChangedFieldV1{{Path: "fallback"}}
	case FallbackPatchTrojanALPNTCP:
		candidate.Options, err = restoreTrojanALPN(candidate.Options, payload.PreviousALPN, payload.PreviousDefault)
		changed = []ChangedFieldV1{{Path: "fallback_for_alpn"}}
		if payload.PreviousDefault != nil {
			changed = append(changed, ChangedFieldV1{Path: "fallback"})
		}
	default:
		return model.Inbound{}, nil, "", adapterFailure(ErrorRestoreFailure)
	}
	if err != nil {
		return model.Inbound{}, nil, "", adapterFailure(ErrorRestoreFailure)
	}
	content, err := candidate.MarshalJSON()
	if err != nil {
		return model.Inbound{}, nil, "", adapterFailure(ErrorRestoreFailure)
	}
	if s.mutation.Hydrator != nil {
		content, err = s.mutation.Hydrator.HydrateInbound(ctx, tx, &candidate, content)
		if err != nil {
			return model.Inbound{}, nil, "", adapterFailure(ErrorRestoreFailure)
		}
	}
	validator := s.mutation.validator
	if validator == nil {
		validator = defaultCandidateValidator{}
	}
	if err = validator.ValidateInbound(ctx, content); err != nil {
		return model.Inbound{}, nil, "", adapterFailure(ErrorRestoreFailure)
	}
	expectedOptionsDigest, err := canonicalInboundOptionsDigest(ctx, content)
	if err != nil {
		return model.Inbound{}, nil, "", adapterFailure(ErrorRestoreFailure)
	}
	return candidate, changed, expectedOptionsDigest, nil
}

func (s *Service) markRestoreFailed(checkpointID string) {
	_ = s.db.Model(&model.InboundFallbackCheckpoint{}).
		Where("id = ? AND state = ?", checkpointID, checkpointStateRestoredCommitted).
		Update("state", checkpointStateRestoreFailed).Error
}

func (s *Service) ReleaseCheckpoint(ctx context.Context, request ReleaseCheckpointRequestV1) (CheckpointReleaseV1, error) {
	var result CheckpointReleaseV1
	err := s.runMutation(ctx, func() error {
		now := s.now()
		return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			row, payload, err := loadCheckpoint(tx, request.CheckpointID)
			if err != nil {
				return err
			}
			if row.State == checkpointStateReleased {
				return adapterFailure(ErrorCheckpointRelease)
			}
			switch request.Kind {
			case CheckpointProofApplyNeverCommitted:
				if row.State != checkpointStatePrepared ||
					request.ProofDigest != terminalProofDigest(row.ID, checkpointStatePrepared, payload.BeforeConfigurationRevision) {
					return adapterFailure(ErrorCheckpointRelease)
				}
				_, _, snapshot, loadErr := s.loadInboundState(tx, payload.InboundDatabaseID)
				if loadErr != nil || snapshot.ConfigurationRevision != payload.BeforeConfigurationRevision {
					return adapterFailure(ErrorCheckpointRelease)
				}
			case CheckpointProofRestoreVerified:
				if row.State != checkpointStateRestoredVerified || request.ProofDigest == "" || request.ProofDigest != row.ProofDigest {
					return adapterFailure(ErrorCheckpointRelease)
				}
			case CheckpointProofDurablyAdopted:
				if row.State != checkpointStateCommitted && row.State != checkpointStateRuntimeApplied && row.State != checkpointStateEffectiveVerified {
					return adapterFailure(ErrorCheckpointRelease)
				}
				_, _, snapshot, loadErr := s.loadInboundState(tx, payload.InboundDatabaseID)
				if loadErr != nil || snapshot.ConfigurationRevision != payload.BeforeConfigurationRevision ||
					request.ProofDigest != terminalProofDigest(row.ID, string(CheckpointProofDurablyAdopted), snapshot.ConfigurationRevision) {
					return adapterFailure(ErrorCheckpointRelease)
				}
			default:
				return adapterFailure(ErrorCheckpointRelease)
			}
			update := tx.Model(&model.InboundFallbackCheckpoint{}).Where("id = ? AND state = ?", row.ID, row.State).
				Updates(map[string]any{"state": checkpointStateReleased, "released_at_unix": now.Unix()})
			if update.Error != nil || update.RowsAffected != 1 {
				return adapterFailure(ErrorDatabase)
			}
			result = CheckpointReleaseV1{CheckpointID: row.ID, ReleasedAt: now}
			return nil
		})
	})
	if err != nil {
		return CheckpointReleaseV1{}, normalizeAdapterError(err, ErrorCheckpointRelease)
	}
	return result, nil
}
