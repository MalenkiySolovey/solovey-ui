package coreinboundcontrol

import (
	"context"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/gorm"
)

func (s *Service) ApplyFallbackPatch(ctx context.Context, request ApplyFallbackPatchRequestV1) (FallbackMutationResultV1, error) {
	var result FallbackMutationResultV1
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
		if request.ExpectedBeforeRevision != payload.BeforeConfigurationRevision {
			return adapterFailure(ErrorStaleBeforeRevision)
		}
		validatedEndpoint, err := checkpointEndpointMatches(payload, request.ApprovedEndpoint)
		if err != nil {
			return err
		}
		request.ApprovedEndpoint = validatedEndpoint
		if row.State == checkpointStateReleased || row.State == checkpointStateRestoredCommitted ||
			row.State == checkpointStateRestoreFailed || row.State == checkpointStateRestoredVerified {
			return adapterFailure(ErrorMutationConflict)
		}
		if row.State != checkpointStatePrepared {
			if row.State == checkpointStateRuntimeApplied || row.State == checkpointStateEffectiveVerified {
				if row.AfterRevision != payload.ExpectedAfterRevision || len(row.EffectiveRevision) != 64 || !safeRevision(row.EffectiveRevision) ||
					(row.State == checkpointStateEffectiveVerified && row.ProofDigest != terminalProofDigest(row.ID, checkpointStateEffectiveVerified, row.EffectiveRevision)) {
					return adapterFailure(ErrorCheckpointTampered)
				}
				result = FallbackMutationResultV1{
					Schema: FallbackMutationSchemaV1, CheckpointID: row.ID, InboundDatabaseID: row.InboundID,
					BeforeConfigurationRevision: payload.BeforeConfigurationRevision,
					AfterConfigurationRevision:  row.AfterRevision, ExpectedEffectiveRevision: row.EffectiveRevision,
					AlreadyCommitted: true,
				}
				_ = tx.Rollback().Error
				committed = true
				return nil
			}
			return adapterFailure(ErrorAmbiguousResult)
		}
		if !payload.ExpiresAt.After(s.now()) {
			return adapterFailure(ErrorCheckpointStale)
		}
		inbound, referenceCount, snapshot, err := s.loadInboundState(tx, payload.InboundDatabaseID)
		if err != nil {
			return err
		}
		if snapshot.ConfigurationRevision != payload.BeforeConfigurationRevision ||
			snapshot.RuntimeIdentityRevision != payload.RuntimeIdentityRevision || s.identity.IdentityRevision != payload.RuntimeIdentityRevision {
			return adapterFailure(ErrorStaleBeforeRevision)
		}
		patchRequest := requestFromCheckpoint(payload, request.ApprovedEndpoint, snapshot)
		if err = s.checkPreviewExpectations(snapshot, patchRequest); err != nil {
			if IsAdapterError(err, ErrorStalePreview) {
				return adapterFailure(ErrorStaleBeforeRevision)
			}
			return err
		}
		candidate, changed, _, expectedOptionsDigest, err := s.candidateFor(ctx, tx, inbound, referenceCount, patchRequest)
		if err != nil {
			return err
		}
		after := buildSnapshotWithRuntimeDigest(candidate, referenceCount, s.identity, nil, 0, expectedOptionsDigest)
		if after.ConfigurationRevision != payload.ExpectedAfterRevision {
			return adapterFailure(ErrorInvalidCandidate)
		}
		if err = s.persistCandidate(ctx, tx, payload, &candidate, changed); err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return adapterFailure(ErrorCancelled)
		}
		update := tx.Model(&model.InboundFallbackCheckpoint{}).
			Where("id = ? AND state = ?", row.ID, checkpointStatePrepared).
			Updates(map[string]any{"state": checkpointStateCommitted, "after_revision": after.ConfigurationRevision})
		if update.Error != nil {
			return adapterFailure(ErrorDatabase)
		}
		if update.RowsAffected != 1 {
			return adapterFailure(ErrorMutationConflict)
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
		if err != nil {
			if ctx.Err() != nil {
				return adapterFailure(ErrorAmbiguousResult)
			}
			return adapterFailure(ErrorRuntimeApply)
		}
		if err = validateRuntimeObservation(candidate, observation, expectedOptionsDigest); err != nil {
			if ctx.Err() != nil {
				return adapterFailure(ErrorAmbiguousResult)
			}
			return err
		}
		effectiveRevision := effectiveRevision(payload.RuntimeIdentityRevision, after.ConfigurationRevision, observation)
		stateUpdate := s.db.WithContext(ctx).Model(&model.InboundFallbackCheckpoint{}).
			Where("id = ? AND state = ?", row.ID, checkpointStateCommitted).
			Updates(map[string]any{"state": checkpointStateRuntimeApplied, "effective_revision": effectiveRevision})
		if stateUpdate.Error != nil || stateUpdate.RowsAffected != 1 {
			return adapterFailure(ErrorAmbiguousResult)
		}
		result = FallbackMutationResultV1{
			Schema: FallbackMutationSchemaV1, CheckpointID: row.ID, InboundDatabaseID: payload.InboundDatabaseID,
			BeforeConfigurationRevision: payload.BeforeConfigurationRevision,
			AfterConfigurationRevision:  after.ConfigurationRevision,
			ExpectedEffectiveRevision:   effectiveRevision, Observation: observation,
		}
		return nil
	})
	if err != nil {
		return FallbackMutationResultV1{}, normalizeAdapterError(err, ErrorMutationConflict)
	}
	return result, nil
}

func (s *Service) persistCandidate(ctx context.Context, tx *gorm.DB, payload checkpointPayloadV1, candidate *model.Inbound, changed []ChangedFieldV1) error {
	if candidate == nil {
		return adapterFailure(ErrorInvalidCandidate)
	}
	var update *gorm.DB
	switch payload.Variant {
	case FallbackPatchVLESSRealityHandshakeTCP:
		if candidate.Tls == nil || candidate.Tls.Id != payload.TLSRecordDatabaseID {
			return adapterFailure(ErrorInvalidCandidate)
		}
		update = tx.Model(&model.Tls{}).Where("id = ?", payload.TLSRecordDatabaseID).Update("server", candidate.Tls.Server)
	case FallbackPatchTrojanDefaultTCP, FallbackPatchTrojanALPNTCP:
		update = tx.Model(&model.Inbound{}).Where("id = ?", payload.InboundDatabaseID).Update("options", candidate.Options)
	default:
		return adapterFailure(ErrorUnsupportedConfig)
	}
	if update.Error != nil {
		return adapterFailure(ErrorDatabase)
	}
	if update.RowsAffected != 1 {
		return adapterFailure(ErrorMutationConflict)
	}
	if s.mutation.Hooks != nil {
		if err := s.mutation.Hooks.BeforeCommit(ctx, tx, candidate, payload.Variant, changed); err != nil {
			return adapterFailure(ErrorDatabase)
		}
	}
	return nil
}

func (s *Service) VerifyEffective(ctx context.Context, request VerifyEffectiveRequestV1) (EffectiveVerificationV1, error) {
	var result EffectiveVerificationV1
	err := s.runMutation(ctx, func() error {
		return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			row, payload, err := loadCheckpoint(tx, request.CheckpointID)
			if err != nil {
				return err
			}
			if row.State != checkpointStateRuntimeApplied && row.State != checkpointStateEffectiveVerified {
				return adapterFailure(ErrorEffectiveVerify)
			}
			if request.ExpectedAfterRevision != payload.ExpectedAfterRevision || request.ExpectedEffectiveRevision == "" ||
				request.ExpectedEffectiveRevision != row.EffectiveRevision {
				return adapterFailure(ErrorEffectiveVerify)
			}
			inbound, _, snapshot, err := s.loadInboundState(tx, payload.InboundDatabaseID)
			if err != nil || snapshot.ConfigurationRevision != payload.ExpectedAfterRevision {
				return adapterFailure(ErrorEffectiveVerify)
			}
			expectedOptionsDigest, err := s.currentInboundOptionsDigest(ctx, tx, &inbound)
			if err != nil {
				return adapterFailure(ErrorEffectiveVerify)
			}
			observation, err := s.mutation.Runtime.ObserveInbound(ctx, inbound.Tag)
			if err != nil || validateRuntimeObservation(inbound, observation, expectedOptionsDigest) != nil {
				return adapterFailure(ErrorEffectiveVerify)
			}
			effective := effectiveRevision(payload.RuntimeIdentityRevision, snapshot.ConfigurationRevision, observation)
			if effective != request.ExpectedEffectiveRevision {
				return adapterFailure(ErrorEffectiveVerify)
			}
			proof := terminalProofDigest(row.ID, checkpointStateEffectiveVerified, effective)
			update := tx.Model(&model.InboundFallbackCheckpoint{}).
				Where("id = ? AND state IN ?", row.ID, []string{checkpointStateRuntimeApplied, checkpointStateEffectiveVerified}).
				Updates(map[string]any{"state": checkpointStateEffectiveVerified, "proof_digest": proof})
			if update.Error != nil || update.RowsAffected != 1 {
				return adapterFailure(ErrorDatabase)
			}
			result = EffectiveVerificationV1{
				CheckpointID: row.ID, ConfigurationRevision: snapshot.ConfigurationRevision,
				EffectiveRevision: effective, Verified: true, ProofDigest: proof, Observation: observation,
			}
			return nil
		})
	})
	if err != nil {
		return EffectiveVerificationV1{}, normalizeAdapterError(err, ErrorEffectiveVerify)
	}
	return result, nil
}

func (s *Service) currentInboundOptionsDigest(ctx context.Context, tx *gorm.DB, inbound *model.Inbound) (string, error) {
	if inbound == nil {
		return "", adapterFailure(ErrorInvalidCandidate)
	}
	content, err := inbound.MarshalJSON()
	if err != nil {
		return "", err
	}
	if s.mutation.Hydrator != nil {
		content, err = s.mutation.Hydrator.HydrateInbound(ctx, tx, inbound, content)
		if err != nil {
			return "", err
		}
	}
	return canonicalInboundOptionsDigest(ctx, content)
}

func (s *Service) runMutation(ctx context.Context, operation func() error) error {
	if s == nil || s.db == nil {
		return adapterFailure(ErrorDatabase)
	}
	if s.mutation.Coordinator == nil || s.mutation.Runtime == nil {
		return adapterFailure(ErrorUnsupportedRuntime)
	}
	err := s.mutation.Coordinator.RunBlockingContext(ctx, operation)
	if err != nil {
		if _, ok := err.(*AdapterError); ok {
			return err
		}
		if ctx.Err() != nil {
			return adapterFailure(ErrorCancelled)
		}
		return normalizeAdapterError(err, ErrorMutationConflict)
	}
	return nil
}

func checkpointEndpointMatches(payload checkpointPayloadV1, endpoint ApprovedEndpointV1) (ApprovedEndpointV1, error) {
	validated, err := validateApprovedEndpoint(payload.Variant, endpoint)
	if err != nil {
		return ApprovedEndpointV1{}, err
	}
	if validated.ProviderID != payload.EndpointProviderID || validated.EndpointID != payload.EndpointID ||
		validated.EndpointRevision != payload.EndpointRevision || endpointBindingDigest(validated) != payload.EndpointBindingDigest {
		return ApprovedEndpointV1{}, adapterFailure(ErrorInvalidEndpoint)
	}
	return validated, nil
}

func requestFromCheckpoint(payload checkpointPayloadV1, endpoint ApprovedEndpointV1, snapshot InboundFallbackSnapshotV1) PreviewFallbackPatchRequestV1 {
	return PreviewFallbackPatchRequestV1{
		Expected: FallbackPatchExpectationsV1{
			InboundDatabaseID: payload.InboundDatabaseID, ResourceID: snapshot.ResourceID,
			ConfigurationRevision:      payload.BeforeConfigurationRevision,
			RuntimeIdentityRevision:    payload.RuntimeIdentityRevision,
			CapabilityResolverRevision: CapabilityResolverRevisionV1,
			EndpointRevision:           payload.EndpointRevision,
		},
		Variant: payload.Variant, ApprovedEndpoint: endpoint, ReplaceDefaultToo: payload.ReplaceDefaultToo,
	}
}

func validateRuntimeObservation(inbound model.Inbound, observation RuntimeInboundObservationV1, expectedOptionsDigest string) error {
	if !observation.RuntimeAvailable || observation.MatchingInboundCount != 1 || observation.Tag != inbound.Tag ||
		normalizeType(observation.Type) != normalizeType(inbound.Type) || len(expectedOptionsDigest) != 64 ||
		!safeRevision(expectedOptionsDigest) || observation.OptionsDigest != expectedOptionsDigest || observation.ManagerGeneration == 0 {
		return adapterFailure(ErrorEffectiveVerify)
	}
	return nil
}

func effectiveRevision(runtimeIdentity, configuration string, observation RuntimeInboundObservationV1) string {
	return digestValue(struct {
		Schema, RuntimeIdentity, Configuration, Tag, Type, OptionsDigest string
		ManagerGeneration                                                uint64
	}{"solovey-ui/effective-inbound/v1", runtimeIdentity, configuration, observation.Tag,
		observation.Type, observation.OptionsDigest, observation.ManagerGeneration})
}

func terminalProofDigest(checkpointID, state, revision string) string {
	return digestValue(struct{ Schema, CheckpointID, State, Revision string }{
		"solovey-ui/inbound-fallback-terminal-proof/v1", checkpointID, state, revision,
	})
}
