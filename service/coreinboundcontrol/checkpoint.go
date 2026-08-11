package coreinboundcontrol

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/gorm"
)

const (
	maxCheckpointPayloadSize    = 8 << 10
	maxRecoverableCheckpoints   = 256
	releasedCheckpointRetention = 7 * 24 * time.Hour
)

const (
	checkpointStatePrepared          = "prepared"
	checkpointStateCommitted         = "committed"
	checkpointStateRuntimeApplied    = "runtime_applied"
	checkpointStateEffectiveVerified = "effective_verified"
	checkpointStateRestoredCommitted = "restored_committed"
	checkpointStateRestoreFailed     = "restore_failed"
	checkpointStateRestoredVerified  = "restored_verified"
	checkpointStateReleased          = "released"
)

type checkpointPayloadV1 struct {
	Schema                      string                   `json:"schema"`
	CheckpointID                string                   `json:"checkpointId"`
	PreviewDigest               string                   `json:"previewDigest"`
	InboundDatabaseID           uint                     `json:"inboundDatabaseId"`
	TLSRecordDatabaseID         uint                     `json:"tlsRecordDatabaseId,omitempty"`
	Variant                     FallbackPatchVariantV1   `json:"variant"`
	ReplaceDefaultToo           bool                     `json:"replaceDefaultToo,omitempty"`
	BeforeConfigurationRevision string                   `json:"beforeConfigurationRevision"`
	ExpectedAfterRevision       string                   `json:"expectedAfterRevision"`
	RuntimeIdentityRevision     string                   `json:"runtimeIdentityRevision"`
	EndpointProviderID          string                   `json:"endpointProviderId"`
	EndpointID                  string                   `json:"endpointId"`
	EndpointRevision            string                   `json:"endpointRevision"`
	EndpointBindingDigest       string                   `json:"endpointBindingDigest"`
	PreviousTarget              *checkpointTargetV1      `json:"previousTarget,omitempty"`
	PreviousALPN                []checkpointALPNTargetV1 `json:"previousAlpn,omitempty"`
	PreviousDefault             *checkpointTargetV1      `json:"previousDefault,omitempty"`
	CreatedAt                   time.Time                `json:"createdAt"`
	ExpiresAt                   time.Time                `json:"expiresAt"`
}

func (s *Service) PrepareCheckpoint(ctx context.Context, request PrepareCheckpointRequestV1) (CheckpointPreparationV1, error) {
	if err := ctx.Err(); err != nil {
		return CheckpointPreparationV1{}, adapterFailure(ErrorCancelled)
	}
	if s == nil || s.db == nil {
		return CheckpointPreparationV1{}, adapterFailure(ErrorDatabase)
	}
	endpoint, err := validateApprovedEndpoint(request.Preview.Variant, request.ApprovedEndpoint)
	if err != nil {
		return CheckpointPreparationV1{}, err
	}
	preview := request.Preview
	if preview.Schema != FallbackPatchPreviewSchemaV1 || preview.Digest == "" || preview.PreviewID != preview.Digest ||
		preview.EndpointProviderID != endpoint.ProviderID || preview.EndpointID != endpoint.EndpointID ||
		preview.EndpointRevision != endpoint.EndpointRevision || preview.Digest != previewDigestWithEndpoint(preview, endpoint) {
		return CheckpointPreparationV1{}, adapterFailure(ErrorStalePreview)
	}
	now := s.now()
	if !preview.ExpiresAt.After(now) {
		return CheckpointPreparationV1{}, adapterFailure(ErrorStalePreview)
	}
	patchRequest := PreviewFallbackPatchRequestV1{
		Expected: FallbackPatchExpectationsV1{
			InboundDatabaseID: preview.InboundDatabaseID, ResourceID: preview.ResourceID,
			ConfigurationRevision:      preview.BeforeConfigurationRevision,
			RuntimeIdentityRevision:    preview.RuntimeIdentityRevision,
			CapabilityResolverRevision: preview.CapabilityResolverRevision,
			EndpointRevision:           preview.EndpointRevision,
		},
		Variant: preview.Variant, ApprovedEndpoint: endpoint, ReplaceDefaultToo: request.ReplaceDefaultToo,
	}
	var prepared CheckpointPreparationV1
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := pruneReleasedCheckpoints(tx, now); err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&model.InboundFallbackCheckpoint{}).Where("state <> ?", checkpointStateReleased).Count(&count).Error; err != nil {
			return adapterFailure(ErrorDatabase)
		}
		if count >= maxRecoverableCheckpoints {
			return adapterFailure(ErrorMutationConflict)
		}
		inbound, referenceCount, snapshot, loadErr := s.loadInboundState(tx, patchRequest.Expected.InboundDatabaseID)
		if loadErr != nil {
			return loadErr
		}
		if checkErr := s.checkPreviewExpectations(snapshot, patchRequest); checkErr != nil {
			return checkErr
		}
		candidate, changed, checkpointData, expectedOptionsDigest, candidateErr := s.candidateFor(ctx, tx, inbound, referenceCount, patchRequest)
		if candidateErr != nil {
			return candidateErr
		}
		after := buildSnapshotWithRuntimeDigest(candidate, referenceCount, s.identity, nil, 0, expectedOptionsDigest)
		if after.ConfigurationRevision != preview.ExpectedAfterRevision || digestValue(changed) != digestValue(preview.ChangedFields) {
			return adapterFailure(ErrorStalePreview)
		}
		checkpointID, idErr := newCheckpointID()
		if idErr != nil {
			return adapterFailure(ErrorDatabase)
		}
		payload := checkpointPayloadV1{
			Schema: FallbackCheckpointSchemaV1, CheckpointID: checkpointID,
			PreviewDigest: preview.Digest, InboundDatabaseID: snapshot.InboundDatabaseID,
			TLSRecordDatabaseID: snapshot.TLSRecordDatabaseID, Variant: patchRequest.Variant,
			ReplaceDefaultToo:           patchRequest.ReplaceDefaultToo,
			BeforeConfigurationRevision: snapshot.ConfigurationRevision,
			ExpectedAfterRevision:       after.ConfigurationRevision,
			RuntimeIdentityRevision:     snapshot.RuntimeIdentityRevision,
			EndpointProviderID:          endpoint.ProviderID,
			EndpointID:                  endpoint.EndpointID,
			EndpointRevision:            endpoint.EndpointRevision,
			EndpointBindingDigest:       endpointBindingDigest(endpoint),
			PreviousTarget:              checkpointData.PreviousTarget, PreviousALPN: checkpointData.PreviousALPN,
			PreviousDefault: checkpointData.PreviousDefault, CreatedAt: now, ExpiresAt: preview.ExpiresAt,
		}
		content, marshalErr := json.Marshal(payload)
		if marshalErr != nil || len(content) == 0 || len(content) > maxCheckpointPayloadSize {
			return adapterFailure(ErrorInvalidCandidate)
		}
		integrity := digestBytes(content)
		row := model.InboundFallbackCheckpoint{
			ID: checkpointID, Schema: FallbackCheckpointSchemaV1, InboundID: snapshot.InboundDatabaseID,
			Payload: content, IntegrityDigest: integrity, State: checkpointStatePrepared,
			CreatedAtUnix: now.Unix(), ExpiresAtUnix: preview.ExpiresAt.Unix(),
		}
		if err := tx.Create(&row).Error; err != nil {
			return adapterFailure(ErrorDatabase)
		}
		prepared = CheckpointPreparationV1{
			Schema: FallbackCheckpointSchemaV1, CheckpointID: checkpointID,
			PreviewDigest: preview.Digest, IntegrityDigest: integrity,
			UncommittedReleaseProof: terminalProofDigest(checkpointID, checkpointStatePrepared, snapshot.ConfigurationRevision),
			ExpiresAt:               preview.ExpiresAt,
		}
		return nil
	})
	if err != nil {
		return CheckpointPreparationV1{}, normalizeAdapterError(err, ErrorDatabase)
	}
	return prepared, nil
}

func loadCheckpoint(tx *gorm.DB, checkpointID string) (model.InboundFallbackCheckpoint, checkpointPayloadV1, error) {
	if !safeOpaque(checkpointID) {
		return model.InboundFallbackCheckpoint{}, checkpointPayloadV1{}, adapterFailure(ErrorCheckpointMissing)
	}
	var row model.InboundFallbackCheckpoint
	if err := tx.First(&row, "id = ?", checkpointID).Error; err != nil {
		return row, checkpointPayloadV1{}, adapterFailure(ErrorCheckpointMissing)
	}
	if row.Schema != FallbackCheckpointSchemaV1 || len(row.Payload) == 0 || len(row.Payload) > maxCheckpointPayloadSize ||
		row.IntegrityDigest != digestBytes(row.Payload) {
		return row, checkpointPayloadV1{}, adapterFailure(ErrorCheckpointTampered)
	}
	var payload checkpointPayloadV1
	if err := json.Unmarshal(row.Payload, &payload); err != nil || payload.Schema != row.Schema ||
		payload.CheckpointID != row.ID || payload.InboundDatabaseID != row.InboundID ||
		payload.BeforeConfigurationRevision == "" || payload.ExpectedAfterRevision == "" ||
		payload.RuntimeIdentityRevision == "" || payload.EndpointRevision == "" || payload.EndpointBindingDigest == "" {
		return row, checkpointPayloadV1{}, adapterFailure(ErrorCheckpointTampered)
	}
	return row, payload, nil
}

func pruneReleasedCheckpoints(tx *gorm.DB, now time.Time) error {
	cutoff := now.Add(-releasedCheckpointRetention).Unix()
	if err := tx.Where("state = ? AND released_at_unix > 0 AND released_at_unix < ?", checkpointStateReleased, cutoff).
		Delete(&model.InboundFallbackCheckpoint{}).Error; err != nil {
		return adapterFailure(ErrorDatabase)
	}
	return nil
}

func newCheckpointID() (string, error) {
	content := make([]byte, 24)
	if _, err := rand.Read(content); err != nil {
		return "", err
	}
	return hex.EncodeToString(content), nil
}
