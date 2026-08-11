package sshmanagement

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	operationcoordination "github.com/MalenkiySolovey/solovey-ui/internal/ops/operationcoordination"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct {
	DB func() *gorm.DB
}

func NewRepository(db *gorm.DB) Repository { return Repository{DB: func() *gorm.DB { return db }} }

func (r Repository) db() (*gorm.DB, error) {
	if r.DB == nil || r.DB() == nil {
		return nil, errors.New("ssh management database is unavailable")
	}
	return r.DB(), nil
}

func (r Repository) SavePosture(ctx context.Context, posture domain.SSHPostureV1, now time.Time) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(posture)
	if err != nil {
		return err
	}
	row := model.SSHPostureSnapshot{SemanticRevision: posture.SemanticRevision, PayloadJSON: payload, ObservedAt: posture.ObservedAt, ExpiresAt: posture.ExpiresAt, CreatedAt: now.UTC().Unix()}
	return db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "semantic_revision"}}, DoNothing: true}).Create(&row).Error
}

func (r Repository) LatestPosture(ctx context.Context) (domain.SSHPostureV1, error) {
	db, err := r.db()
	if err != nil {
		return domain.SSHPostureV1{}, err
	}
	var row model.SSHPostureSnapshot
	if err := db.WithContext(ctx).Order("observed_at desc, id desc").First(&row).Error; err != nil {
		return domain.SSHPostureV1{}, err
	}
	var posture domain.SSHPostureV1
	if err := json.Unmarshal(row.PayloadJSON, &posture); err != nil || posture.SemanticRevision != row.SemanticRevision || posture.ObservedAt != row.ObservedAt || posture.ExpiresAt != row.ExpiresAt {
		return domain.SSHPostureV1{}, domain.NewError("posture", domain.ReasonMalformedProviderEvidence)
	}
	return posture, nil
}

func (r Repository) CreateCandidateWithJournal(ctx context.Context, candidate domain.CandidateV1, event string, reason domain.ReasonCode, now time.Time) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	row, err := candidateRow(candidate)
	if err != nil {
		return err
	}
	journal, err := journalRow(candidate, event, reason, now)
	if err != nil {
		return err
	}
	return operationcoordination.SerializeAdmission(func() error {
		return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if operationcoordination.Blocker(ctx, tx, "") != "" {
				return domain.NewError("candidate", domain.ReasonOperationStateConflict)
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			return tx.Create(&journal).Error
		})
	})
}

func (r Repository) Candidate(ctx context.Context, operationID string) (domain.CandidateV1, error) {
	db, err := r.db()
	if err != nil {
		return domain.CandidateV1{}, err
	}
	var row model.SSHManagementCandidate
	if err := db.WithContext(ctx).Where("operation_id = ?", operationID).First(&row).Error; err != nil {
		return domain.CandidateV1{}, err
	}
	return candidateFromRow(row)
}

func (r Repository) CandidateByIdempotency(ctx context.Context, idempotencyKey string) (domain.CandidateV1, error) {
	db, err := r.db()
	if err != nil {
		return domain.CandidateV1{}, err
	}
	var row model.SSHManagementCandidate
	if err := db.WithContext(ctx).Where("idempotency_key = ?", idempotencyKey).First(&row).Error; err != nil {
		return domain.CandidateV1{}, err
	}
	return candidateFromRow(row)
}

func (r Repository) ActiveCandidate(ctx context.Context) (domain.CandidateV1, error) {
	db, err := r.db()
	if err != nil {
		return domain.CandidateV1{}, err
	}
	var row model.SSHManagementCandidate
	if err := db.WithContext(ctx).Where("scope = ? AND state NOT IN ?", "global", terminalStates()).Order("created_at asc").First(&row).Error; err != nil {
		return domain.CandidateV1{}, err
	}
	return candidateFromRow(row)
}

func (r Repository) UpdateCandidateCAS(ctx context.Context, candidate domain.CandidateV1, expectedRevision uint64, expectedState domain.CandidateState) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	row, err := candidateRow(candidate)
	if err != nil {
		return err
	}
	updates := candidateUpdates(row)
	result := db.WithContext(ctx).Model(&model.SSHManagementCandidate{}).
		Where("operation_id = ? AND revision = ? AND state = ?", candidate.OperationID, expectedRevision, string(expectedState)).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return domain.NewError("candidate", domain.ReasonOperationStateConflict)
	}
	return nil
}

// UpdateCandidateCASWithJournal persists the state/revision and its audit-safe
// journal entry in one transaction. A transition is never reported durable
// when its journal could not be recorded.
func (r Repository) UpdateCandidateCASWithJournal(ctx context.Context, candidate domain.CandidateV1, expectedRevision uint64, expectedState domain.CandidateState, event string, reason domain.ReasonCode, now time.Time) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	row, err := candidateRow(candidate)
	if err != nil {
		return err
	}
	journal, err := journalRow(candidate, event, reason, now)
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.SSHManagementCandidate{}).
			Where("operation_id = ? AND revision = ? AND state = ?", candidate.OperationID, expectedRevision, string(expectedState)).
			Updates(candidateUpdates(row))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return domain.NewError("candidate", domain.ReasonOperationStateConflict)
		}
		return tx.Create(&journal).Error
	})
}

// CommitCandidateCASWithJournalAndEvidence makes reconnect-proof consumption,
// evidence publication, the terminal candidate transition, and its journal
// entry one durability boundary. A caller can therefore never observe a
// committed candidate without its operation-bound recovery evidence (or the
// inverse).
func (r Repository) CommitCandidateCASWithJournalAndEvidence(ctx context.Context, candidate domain.CandidateV1, expectedRevision uint64, expectedState domain.CandidateState, challengeRevision uint64, path hostresources.RecoveryPathV1, now time.Time) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	row, err := candidateRow(candidate)
	if err != nil {
		return err
	}
	journal, err := journalRow(candidate, "candidate_committed", "", now)
	if err != nil {
		return err
	}
	evidence, err := recoveryEvidenceRow(path, now)
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		challenge := tx.Model(&model.SSHReconnectChallenge{}).
			Where("operation_id = ? AND revision = ? AND consumed_at = 0 AND expires_at > ?", candidate.OperationID, challengeRevision, now.UTC().Unix()).
			Updates(map[string]any{"consumed_at": now.UTC().Unix(), "revision": challengeRevision + 1})
		if challenge.Error != nil {
			return challenge.Error
		}
		if challenge.RowsAffected != 1 {
			return domain.NewError("challenge", domain.ReasonReconnectProofInvalid)
		}
		updated := tx.Model(&model.SSHManagementCandidate{}).
			Where("operation_id = ? AND revision = ? AND state = ?", candidate.OperationID, expectedRevision, string(expectedState)).
			Updates(candidateUpdates(row))
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return domain.NewError("candidate", domain.ReasonOperationStateConflict)
		}
		if err := tx.Clauses(recoveryEvidenceUpsert()).Create(&evidence).Error; err != nil {
			return err
		}
		return tx.Create(&journal).Error
	})
}

func (r Repository) SaveCheckpoint(ctx context.Context, operationID string, prior PriorArtifactV1, stagedDigest, stagedConfigurationRevision string, now time.Time) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	row := model.SSHManagedArtifactCheckpoint{OperationID: operationID, PriorPresent: prior.Present, PriorContent: append([]byte(nil), prior.Content...), PriorOwner: prior.Owner, PriorGroup: prior.Group, PriorModeClass: prior.ModeClass, PriorMode: prior.Mode, PriorDigest: prior.Digest, StagedArtifactDigest: stagedDigest, StagedConfigurationRevision: stagedConfigurationRevision, CreatedAt: now.UTC().Unix()}
	return db.WithContext(ctx).Create(&row).Error
}

func (r Repository) Checkpoint(ctx context.Context, operationID string) (model.SSHManagedArtifactCheckpoint, error) {
	db, err := r.db()
	if err != nil {
		return model.SSHManagedArtifactCheckpoint{}, err
	}
	var row model.SSHManagedArtifactCheckpoint
	err = db.WithContext(ctx).Where("operation_id = ?", operationID).First(&row).Error
	return row, err
}

func (r Repository) SaveChallenge(ctx context.Context, challenge domain.ReconnectChallengeV1) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	row := model.SSHReconnectChallenge{
		OperationID: challenge.OperationID, CandidateDigest: challenge.CandidateDigest, MarkerDigest: challenge.MarkerDigest,
		EndpointID: challenge.EndpointID, PrincipalID: challenge.PrincipalID, AuthenticationClass: challenge.AuthenticationClass,
		ServiceRevision: challenge.ServiceRevision, BinaryRevision: challenge.BinaryRevision, ConfigurationRevision: challenge.ConfigurationRevision,
		VerifierDigest: challenge.VerifierDigest, IssuedAt: challenge.IssuedAt, ExpiresAt: challenge.ExpiresAt, ConsumedAt: challenge.ConsumedAt, Revision: challenge.Revision,
	}
	return db.WithContext(ctx).Create(&row).Error
}

func (r Repository) Challenge(ctx context.Context, operationID string) (domain.ReconnectChallengeV1, error) {
	db, err := r.db()
	if err != nil {
		return domain.ReconnectChallengeV1{}, err
	}
	var row model.SSHReconnectChallenge
	if err := db.WithContext(ctx).Where("operation_id = ?", operationID).First(&row).Error; err != nil {
		return domain.ReconnectChallengeV1{}, err
	}
	return challengeFromRow(row), nil
}

func (r Repository) ConsumeChallengeCAS(ctx context.Context, operationID string, revision uint64, now time.Time) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	result := db.WithContext(ctx).Model(&model.SSHReconnectChallenge{}).
		Where("operation_id = ? AND revision = ? AND consumed_at = 0 AND expires_at > ?", operationID, revision, now.UTC().Unix()).
		Updates(map[string]any{"consumed_at": now.UTC().Unix(), "revision": revision + 1})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return domain.NewError("challenge", domain.ReasonReconnectProofInvalid)
	}
	return nil
}

func (r Repository) UpsertRecoveryEvidence(ctx context.Context, path hostresources.RecoveryPathV1, now time.Time) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	row, err := recoveryEvidenceRow(path, now)
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Clauses(recoveryEvidenceUpsert()).Create(&row).Error
}

func recoveryEvidenceRow(path hostresources.RecoveryPathV1, now time.Time) (model.SSHRecoveryEvidence, error) {
	reasons, _ := json.Marshal(path.ReasonCodes)
	row := model.SSHRecoveryEvidence{ID: path.ID, Kind: path.Kind, EndpointID: path.EndpointID, PrincipalID: path.PrincipalID, SourcePrefix: path.SourcePrefix,
		VerificationMethod: path.VerificationMethod, EvidenceProvider: path.EvidenceProvider, TargetOperation: path.TargetOperation,
		VerifiedAt: path.VerifiedAt, ExpiresAt: path.ExpiresAt, IndependenceClass: path.IndependenceClass, VerificationState: path.VerificationState,
		OperationBound: path.OperationBound, SingleUse: path.SingleUse, ConsumedAt: path.ConsumedAt, Revision: path.Revision,
		ReasonCodesJSON: reasons, SourceRevision: path.SourceRevision, ConfigurationRevision: path.ConfigurationRevision,
		ServiceRevision: path.ServiceRevision, BinaryRevision: path.BinaryRevision, ProducerRevision: path.ProducerRevision, UpdatedAt: now.UTC().Unix()}
	return row, nil
}

func recoveryEvidenceUpsert() clause.OnConflict {
	return clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns([]string{
		"verification_state", "expires_at", "consumed_at", "revision", "reason_codes_json", "updated_at",
	})}
}

func (r Repository) RecoveryRows(ctx context.Context, now time.Time) ([]model.SSHRecoveryEvidence, error) {
	db, err := r.db()
	if err != nil {
		return nil, err
	}
	var rows []model.SSHRecoveryEvidence
	err = db.WithContext(ctx).Where("expires_at > ?", now.UTC().Unix()).Order("id asc").Limit(4096).Find(&rows).Error
	return rows, err
}

func (r Repository) InvalidateRecoveryEvidence(ctx context.Context, kind, principal, reason string, now time.Time) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	if !safeEvent(reason) {
		return domain.NewError("recovery", domain.ReasonMalformedProviderEvidence)
	}
	reasons, _ := json.Marshal([]string{reason})
	query := db.WithContext(ctx).Model(&model.SSHRecoveryEvidence{}).
		Where("kind = ? AND verification_state = ?", kind, "verified")
	if principal != "" {
		query = query.Where("principal_id = ?", principal)
	}
	return query.Updates(map[string]any{"verification_state": "invalidated", "reason_codes_json": reasons, "updated_at": now.UTC().Unix(), "revision": gorm.Expr("revision + 1")}).Error
}

func (r Repository) AppendJournal(ctx context.Context, candidate domain.CandidateV1, event string, reason domain.ReasonCode, now time.Time) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	row, err := journalRow(candidate, event, reason, now)
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Create(&row).Error
}

func (r Repository) Journal(ctx context.Context, operationID string) ([]model.SSHManagementJournal, error) {
	db, err := r.db()
	if err != nil {
		return nil, err
	}
	var rows []model.SSHManagementJournal
	err = db.WithContext(ctx).Where("operation_id = ?", operationID).Order("sequence asc").Limit(512).Find(&rows).Error
	return rows, err
}

func (r Repository) MarkRestoredUntrusted(ctx context.Context, now time.Time) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	reasons, _ := json.Marshal([]domain.ReasonCode{domain.ReasonRestoredStateUntrusted})
	return db.WithContext(ctx).Model(&model.SSHManagementCandidate{}).Where("state != ?", string(domain.StateRolledBack)).Updates(map[string]any{
		"restored_untrusted": true, "reconciled_at": 0, "reason_codes_json": reasons, "updated_at": now.UTC().Unix(),
	}).Error
}

func (r Repository) DropData(ctx context.Context) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	var count int64
	if err := db.WithContext(ctx).Model(&model.SSHManagementCandidate{}).
		Where("state NOT IN ? OR state = ? OR restored_untrusted = ? OR reconciled_at = 0", terminalStates(), string(domain.StateManualRecoveryRequired), true).
		Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return domain.NewError("drop_data", domain.ReasonOperationStateConflict)
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, table := range []any{&model.SSHReconnectChallenge{}, &model.SSHManagedArtifactCheckpoint{}, &model.SSHManagementJournal{}, &model.SSHRecoveryEvidence{}, &model.SSHManagementCandidate{}, &model.SSHPostureSnapshot{}} {
			if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(table).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func candidateRow(candidate domain.CandidateV1) (model.SSHManagementCandidate, error) {
	policy, err := json.Marshal(candidate.Policy)
	if err != nil {
		return model.SSHManagementCandidate{}, err
	}
	preservation, err := json.Marshal(candidate.Preservation)
	if err != nil {
		return model.SSHManagementCandidate{}, err
	}
	reasons, err := json.Marshal(candidate.ReasonCodes)
	if err != nil {
		return model.SSHManagementCandidate{}, err
	}
	return model.SSHManagementCandidate{OperationID: candidate.OperationID, Scope: "global", IdempotencyKey: candidate.IdempotencyKey,
		State: string(candidate.State), Revision: candidate.Revision, PolicyJSON: policy, PreservationJSON: preservation,
		CandidateDigest: candidate.CandidateDigest, BindingDigest: candidate.BindingDigest, BeforeArtifactDigest: candidate.BeforeArtifactDigest,
		AfterArtifactDigest: candidate.AfterArtifactDigest, PostureRevision: candidate.PostureRevision, EndpointRevision: candidate.EndpointRevision,
		RecoveryRevision: candidate.RecoveryRevision, ProviderRevision: candidate.ProviderRevision, BinaryRevision: candidate.BinaryRevision,
		ServiceRevision: candidate.ServiceRevision, ConfigurationRevision: candidate.ConfigurationRevision, EarliestSafetyExpiry: candidate.EarliestSafetyExpiry,
		ReconnectExpiresAt: candidate.ReconnectExpiresAt, RollbackAttempts: candidate.RollbackAttempts, RestoredUntrusted: candidate.RestoredUntrusted,
		ReconciledAt:    candidate.ReconciledAt,
		ReasonCodesJSON: reasons, CreatedAt: candidate.CreatedAt, UpdatedAt: candidate.UpdatedAt}, nil
}

func candidateFromRow(row model.SSHManagementCandidate) (domain.CandidateV1, error) {
	candidate := domain.CandidateV1{Schema: domain.CandidateSchemaV1, OperationID: row.OperationID, IdempotencyKey: row.IdempotencyKey,
		State: domain.CandidateState(row.State), Revision: row.Revision, CandidateDigest: row.CandidateDigest, BindingDigest: row.BindingDigest,
		BeforeArtifactDigest: row.BeforeArtifactDigest, AfterArtifactDigest: row.AfterArtifactDigest, PostureRevision: row.PostureRevision,
		EndpointRevision: row.EndpointRevision, RecoveryRevision: row.RecoveryRevision, ProviderRevision: row.ProviderRevision,
		BinaryRevision: row.BinaryRevision, ServiceRevision: row.ServiceRevision, ConfigurationRevision: row.ConfigurationRevision,
		EarliestSafetyExpiry: row.EarliestSafetyExpiry, ReconnectExpiresAt: row.ReconnectExpiresAt, RollbackAttempts: row.RollbackAttempts,
		RestoredUntrusted: row.RestoredUntrusted, ReconciledAt: row.ReconciledAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
	if json.Unmarshal(row.PolicyJSON, &candidate.Policy) != nil || json.Unmarshal(row.PreservationJSON, &candidate.Preservation) != nil || json.Unmarshal(row.ReasonCodesJSON, &candidate.ReasonCodes) != nil {
		return domain.CandidateV1{}, domain.NewError("candidate", domain.ReasonMalformedProviderEvidence)
	}
	if candidate.BindingDigest != domain.BindingDigest(candidate) {
		return domain.CandidateV1{}, domain.NewError("candidate", domain.ReasonRevisionMismatch)
	}
	return candidate, nil
}

func challengeFromRow(row model.SSHReconnectChallenge) domain.ReconnectChallengeV1 {
	return domain.ReconnectChallengeV1{Schema: domain.ChallengeSchemaV1, OperationID: row.OperationID, CandidateDigest: row.CandidateDigest,
		MarkerDigest: row.MarkerDigest, EndpointID: row.EndpointID, PrincipalID: row.PrincipalID, AuthenticationClass: row.AuthenticationClass,
		ServiceRevision: row.ServiceRevision, BinaryRevision: row.BinaryRevision, ConfigurationRevision: row.ConfigurationRevision,
		VerifierDigest: row.VerifierDigest, IssuedAt: row.IssuedAt, ExpiresAt: row.ExpiresAt, ConsumedAt: row.ConsumedAt, Revision: row.Revision}
}

func terminalStates() []string {
	return []string{string(domain.StateCommitted), string(domain.StateRolledBack), string(domain.StateManualRecoveryRequired)}
}

func candidateUpdates(row model.SSHManagementCandidate) map[string]any {
	return map[string]any{
		"state": row.State, "revision": row.Revision, "policy_json": row.PolicyJSON, "preservation_json": row.PreservationJSON,
		"candidate_digest": row.CandidateDigest, "binding_digest": row.BindingDigest, "before_artifact_digest": row.BeforeArtifactDigest,
		"after_artifact_digest": row.AfterArtifactDigest, "posture_revision": row.PostureRevision, "endpoint_revision": row.EndpointRevision,
		"recovery_revision": row.RecoveryRevision, "provider_revision": row.ProviderRevision, "binary_revision": row.BinaryRevision,
		"service_revision": row.ServiceRevision, "configuration_revision": row.ConfigurationRevision,
		"earliest_safety_expiry": row.EarliestSafetyExpiry, "reconnect_expires_at": row.ReconnectExpiresAt,
		"rollback_attempts": row.RollbackAttempts, "restored_untrusted": row.RestoredUntrusted, "reconciled_at": row.ReconciledAt,
		"reason_codes_json": row.ReasonCodesJSON, "updated_at": row.UpdatedAt,
	}
}

func journalRow(candidate domain.CandidateV1, event string, reason domain.ReasonCode, now time.Time) (model.SSHManagementJournal, error) {
	if !safeEvent(event) {
		return model.SSHManagementJournal{}, domain.NewError("journal", domain.ReasonMalformedProviderEvidence)
	}
	return model.SSHManagementJournal{OperationID: candidate.OperationID, Sequence: candidate.Revision, State: string(candidate.State), Event: event, ReasonCode: string(reason), Revision: domain.Revision(struct {
		Operation            string
		Sequence             uint64
		State, Event, Reason string
	}{candidate.OperationID, candidate.Revision, string(candidate.State), event, string(reason)}), CreatedAt: now.UTC().Unix()}, nil
}

func safeEvent(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func repositoryConflict(err error) error {
	if err == nil {
		return nil
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "unique") || strings.Contains(lower, "constraint") {
		return domain.NewError("candidate", domain.ReasonCandidateAlreadyActive)
	}
	return fmt.Errorf("ssh management repository: %w", err)
}
