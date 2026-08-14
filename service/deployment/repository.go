package deployment

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
	operationcoordination "github.com/MalenkiySolovey/solovey-ui/internal/ops/operationcoordination"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct{ DB func() *gorm.DB }

const (
	maxDeploymentOperations = 256
	maxDoctorSnapshots      = 64
)

func (r Repository) db() (*gorm.DB, error) {
	if r.DB == nil || r.DB() == nil {
		return nil, errors.New("deployment repository database is unavailable")
	}
	return r.DB(), nil
}

func (r Repository) SavePosture(ctx context.Context, posture domain.Posture, trusted bool) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	now := time.Now().UTC().Unix()
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing model.DeploymentState
		if err := tx.Where("scope = ?", "global").Take(&existing).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		profile, _ := domain.Lookup(posture.Profile)
		desired, generated, generatedRevision := existing.DesiredProfile, existing.GeneratedProfile, existing.GeneratedRevision
		if desired == "" {
			desired = string(posture.Profile)
		}
		if generated == "" {
			generated, generatedRevision = string(posture.Profile), profile.Revision
		}
		installed, active, verified := posture.InstalledProfile, posture.ActiveProfile, posture.VerifiedProfile
		row := model.DeploymentState{Scope: "global", ProfileID: string(posture.Profile), DesiredProfile: desired,
			GeneratedProfile: generated, GeneratedRevision: generatedRevision, InstalledProfile: string(installed),
			ActiveProfile: string(active), VerifiedProfile: string(verified), CompatibilityState: compatibilityState(profile),
			DoctorRevision: existing.DoctorRevision, Runtime: string(posture.Runtime), PostureRevision: posture.Revision,
			Trusted: trusted, ObservedAt: posture.ObservedAt, UpdatedAt: now}
		return tx.Save(&row).Error
	})
}

func (r Repository) State(ctx context.Context) (model.DeploymentState, error) {
	db, err := r.db()
	if err != nil {
		return model.DeploymentState{}, err
	}
	var row model.DeploymentState
	err = db.WithContext(ctx).Where("scope = ?", "global").Take(&row).Error
	return row, err
}

func (r Repository) Admit(ctx context.Context, operation domain.Operation, event string) error {
	return r.create(ctx, operation, event, true)
}

func (r Repository) create(ctx context.Context, operation domain.Operation, event string, updateDesired bool) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	row, err := operationRow(operation)
	if err != nil {
		return err
	}
	journal := journalRow(operation, event, "", time.Now().UTC())
	return operationcoordination.SerializeAdmission(func() error {
		return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var unresolved, total int64
			if err := tx.Model(&model.DeploymentOperation{}).Where("state NOT IN ?", []string{string(domain.StateCommitted), string(domain.StateRolledBack)}).Count(&unresolved).Error; err != nil || unresolved != 0 {
				if err != nil {
					return err
				}
				return ErrOperationConflict
			}
			if err := tx.Model(&model.DeploymentOperation{}).Count(&total).Error; err != nil {
				return err
			}
			if total >= maxDeploymentOperations {
				return ErrOperationConflict
			}
			if operationcoordination.Blocker(ctx, tx, operationcoordination.DomainDeployment) != "" {
				return ErrOperationConflict
			}
			if updateDesired {
				profile, ok := domain.Lookup(operation.TargetProfile)
				if !ok {
					return ErrUnsafeMigration
				}
				desired := tx.Model(&model.DeploymentState{}).Where("scope = ?", "global").Updates(map[string]any{
					"desired_profile": operation.TargetProfile, "generated_profile": operation.TargetProfile,
					"generated_revision": profile.Revision, "updated_at": time.Now().UTC().Unix(),
				})
				if desired.Error != nil {
					return desired.Error
				}
				if desired.RowsAffected != 1 {
					return ErrUnsafeMigration
				}
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			return tx.Create(&journal).Error
		})
	})
}

func (r Repository) ByID(ctx context.Context, id string) (domain.Operation, error) {
	db, err := r.db()
	if err != nil {
		return domain.Operation{}, err
	}
	var row model.DeploymentOperation
	if err := db.WithContext(ctx).Where("operation_id = ?", id).Take(&row).Error; err != nil {
		return domain.Operation{}, err
	}
	return operationFromRow(row)
}

func (r Repository) ByIdempotency(ctx context.Context, key string) (domain.Operation, error) {
	db, err := r.db()
	if err != nil {
		return domain.Operation{}, err
	}
	var row model.DeploymentOperation
	if err := db.WithContext(ctx).Where("idempotency_key = ?", key).Take(&row).Error; err != nil {
		return domain.Operation{}, err
	}
	return operationFromRow(row)
}

func (r Repository) Active(ctx context.Context) (domain.Operation, error) {
	db, err := r.db()
	if err != nil {
		return domain.Operation{}, err
	}
	var row model.DeploymentOperation
	if err := db.WithContext(ctx).Where("state NOT IN ?", []string{string(domain.StateCommitted), string(domain.StateRolledBack), string(domain.StateManualRecoveryRequired)}).
		Order("updated_at desc").Take(&row).Error; err != nil {
		return domain.Operation{}, err
	}
	return operationFromRow(row)
}

// Recovery returns the newest operation whose durable state still requires
// operator attention. Manual recovery is terminal for serialization purposes,
// so it must not be inferred from Active.
func (r Repository) Recovery(ctx context.Context) (domain.Operation, error) {
	db, err := r.db()
	if err != nil {
		return domain.Operation{}, err
	}
	var row model.DeploymentOperation
	if err := db.WithContext(ctx).
		Where("state = ? OR restored_untrusted = ?", string(domain.StateManualRecoveryRequired), true).
		Order("updated_at desc, operation_id desc").Take(&row).Error; err != nil {
		return domain.Operation{}, err
	}
	return operationFromRow(row)
}

func (r Repository) Update(ctx context.Context, operation domain.Operation, expectedRevision uint64, expectedState domain.OperationState, event, reason string) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	row, err := operationRow(operation)
	if err != nil {
		return err
	}
	journal := journalRow(operation, event, reason, time.Now().UTC())
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		updates := map[string]any{"state": row.State, "checkpoint_ref": row.CheckpointRef, "broker_receipt": row.BrokerReceipt,
			"revision": row.Revision, "restored_untrusted": row.RestoredUntrusted, "reconciled_at": row.ReconciledAt,
			"updated_at": row.UpdatedAt, "reasons_json": row.ReasonsJSON, "binding_revision": row.BindingRevision}
		result := tx.Model(&model.DeploymentOperation{}).Where("operation_id = ? AND revision = ? AND state = ?", operation.OperationID, expectedRevision, string(expectedState)).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrOperationConflict
		}
		return tx.Create(&journal).Error
	})
}

func (r Repository) Timeline(ctx context.Context, id string) ([]model.DeploymentJournal, error) {
	db, err := r.db()
	if err != nil {
		return nil, err
	}
	var rows []model.DeploymentJournal
	err = db.WithContext(ctx).Where("operation_id = ?", id).Order("sequence asc").Find(&rows).Error
	return rows, err
}

func (r Repository) SaveDoctor(ctx context.Context, report domain.DoctorReport) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(report)
	if err != nil || len(payload) > 256<<10 {
		return errors.New("deployment doctor snapshot is too large")
	}
	profile := "unknown"
	if report.Posture != nil {
		profile = string(report.Posture.Profile)
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.DeploymentDoctorSnapshot{Revision: report.Revision, ProfileID: profile,
			Healthy: report.Healthy, PayloadJSON: payload, GeneratedAt: report.GeneratedAt}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.DeploymentState{}).Where("scope = ?", "global").Updates(map[string]any{"doctor_revision": report.Revision, "updated_at": time.Now().UTC().Unix()}).Error; err != nil {
			return err
		}
		return tx.Exec(`DELETE FROM deployment_doctor_snapshots_v1 WHERE id NOT IN (
			SELECT id FROM deployment_doctor_snapshots_v1 ORDER BY generated_at DESC, id DESC LIMIT ?
		)`, maxDoctorSnapshots).Error
	})
}

func (r Repository) MarkRestoredUntrusted(ctx context.Context) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.DeploymentState{}).Where("1 = 1").UpdateColumn("trusted", false).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.DeploymentOperation{}).Where("1 = 1").UpdateColumns(map[string]any{"checkpoint_ref": "", "broker_receipt": ""}).Error; err != nil {
			return err
		}
		var active []model.DeploymentOperation
		if err := tx.Where("state NOT IN ?", []string{string(domain.StateCommitted), string(domain.StateRolledBack), string(domain.StateManualRecoveryRequired)}).Find(&active).Error; err != nil {
			return err
		}
		now := time.Now().UTC()
		for _, row := range active {
			updatedAt := now.Unix()
			if updatedAt <= row.UpdatedAt {
				updatedAt = row.UpdatedAt + 1
			}
			if updatedAt < row.CreatedAt {
				updatedAt = row.CreatedAt
			}
			nextRevision := row.Revision + 1
			reasons := []string{}
			_ = json.Unmarshal(row.ReasonsJSON, &reasons)
			reasons = unique(append(reasons, "restored_state_requires_fresh_doctor"))
			encoded, _ := json.Marshal(reasons)
			updates := map[string]any{"restored_untrusted": true, "state": string(domain.StateManualRecoveryRequired),
				"checkpoint_ref": "", "broker_receipt": "", "revision": nextRevision, "updated_at": updatedAt, "reasons_json": encoded}
			result := tx.Model(&model.DeploymentOperation{}).Where("operation_id = ? AND revision = ?", row.OperationID, row.Revision).Updates(updates)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrOperationConflict
			}
			operation := domain.Operation{OperationID: row.OperationID, Revision: nextRevision, State: domain.StateManualRecoveryRequired}
			journal := journalRow(operation, "restore_invalidated_live_authority", "restored_state_requires_fresh_doctor", time.Unix(updatedAt, 0).UTC())
			if err := tx.Create(&journal).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func operationRow(operation domain.Operation) (model.DeploymentOperation, error) {
	if err := operation.Validate(); err != nil {
		return model.DeploymentOperation{}, err
	}
	reasons, _ := json.Marshal(operation.Reasons)
	return model.DeploymentOperation{OperationID: operation.OperationID, IdempotencyKey: operation.IdempotencyKey,
		State: string(operation.State), FromProfile: string(operation.FromProfile), TargetProfile: string(operation.TargetProfile),
		ExpectedPosture: operation.ExpectedPosture, ExpectedManagement: operation.ExpectedManagement,
		CheckpointRef: operation.CheckpointRef, BrokerReceipt: operation.BrokerReceipt,
		Revision: operation.Revision, RestoredUntrusted: operation.RestoredUntrusted, ReconciledAt: operation.ReconciledAt,
		CreatedAt: operation.CreatedAt, UpdatedAt: operation.UpdatedAt, ReasonsJSON: reasons, BindingRevision: operation.BindingRevision}, nil
}

func operationFromRow(row model.DeploymentOperation) (domain.Operation, error) {
	operation := domain.Operation{Schema: domain.SchemaV1, OperationID: row.OperationID, IdempotencyKey: row.IdempotencyKey,
		State: domain.OperationState(row.State), FromProfile: domain.ProfileID(row.FromProfile), TargetProfile: domain.ProfileID(row.TargetProfile),
		ExpectedPosture: row.ExpectedPosture, ExpectedManagement: row.ExpectedManagement,
		CheckpointRef: row.CheckpointRef, BrokerReceipt: row.BrokerReceipt,
		Revision: row.Revision, RestoredUntrusted: row.RestoredUntrusted, ReconciledAt: row.ReconciledAt,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, BindingRevision: row.BindingRevision}
	if len(row.ReasonsJSON) != 0 && json.Unmarshal(row.ReasonsJSON, &operation.Reasons) != nil {
		return domain.Operation{}, errors.New("deployment operation reasons are malformed")
	}
	if err := operation.Validate(); err != nil {
		return domain.Operation{}, err
	}
	return operation, nil
}

func journalRow(operation domain.Operation, event, reason string, now time.Time) model.DeploymentJournal {
	return model.DeploymentJournal{OperationID: operation.OperationID, Sequence: operation.Revision, State: string(operation.State),
		Event: event, Reason: reason, Revision: domain.Revision(struct {
			Operation string
			Sequence  uint64
			State     domain.OperationState
			Event     string
			Reason    string
		}{operation.OperationID, operation.Revision, operation.State, event, reason}), CreatedAt: now.Unix()}
}

func compatibilityState(profile domain.Profile) string {
	switch profile.Support {
	case domain.TierCompatibility:
		return "COMPATIBILITY"
	case domain.TierExperimental:
		return "EXPERIMENTAL"
	default:
		return "HARDENED"
	}
}
