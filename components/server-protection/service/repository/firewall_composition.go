package repository

import (
	"context"
	"errors"
	"regexp"
	"time"

	"gorm.io/gorm"
)

var ErrFirewallAuthorityConflict = errors.New("firewall contribution authority conflict")

const FirewallObservationSchemaV1 = "solovey-ui/managed-firewall-observation/v1"

var firewallIdentityPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type FirewallAuthoritySnapshot struct {
	Contributions  []FirewallContributionModel
	Composition    FirewallCompositionModel
	HasComposition bool
	Observation    FirewallObservationModel
	HasObservation bool
}

func (r *Repository) FirewallAuthority(ctx context.Context) (FirewallAuthoritySnapshot, error) {
	if r == nil || r.db == nil {
		return FirewallAuthoritySnapshot{}, errors.New("server-protection repository is not initialized")
	}
	var result FirewallAuthoritySnapshot
	if err := r.db.WithContext(ctx).Order("contribution_id ASC").Find(&result.Contributions).Error; err != nil {
		return FirewallAuthoritySnapshot{}, err
	}
	err := r.db.WithContext(ctx).Where("id = ?", 1).First(&result.Composition).Error
	if err == nil {
		result.HasComposition = true
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return FirewallAuthoritySnapshot{}, err
	}
	err = r.db.WithContext(ctx).Where("id = ?", 1).First(&result.Observation).Error
	if err == nil {
		result.HasObservation = true
		return result, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return result, nil
	}
	return FirewallAuthoritySnapshot{}, err
}

func (r *Repository) FirewallTransition(ctx context.Context, operationID string) (FirewallContributionTransitionModel, error) {
	if r == nil || r.db == nil {
		return FirewallContributionTransitionModel{}, errors.New("server-protection repository is not initialized")
	}
	var value FirewallContributionTransitionModel
	err := r.db.WithContext(ctx).Where("operation_id = ?", operationID).First(&value).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return FirewallContributionTransitionModel{}, ErrRecordNotFound
	}
	return value, err
}

func (r *Repository) FirewallTransitions(ctx context.Context) ([]FirewallContributionTransitionModel, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("server-protection repository is not initialized")
	}
	var values []FirewallContributionTransitionModel
	err := r.db.WithContext(ctx).Order("created_at ASC, operation_id ASC").Find(&values).Error
	return values, err
}

func (r *Repository) CreateFirewallTransition(ctx context.Context, value FirewallContributionTransitionModel) error {
	if r == nil || r.db == nil {
		return errors.New("server-protection repository is not initialized")
	}
	now := time.Now().UTC().UnixNano()
	value.CreatedAt, value.UpdatedAt = now, now
	return r.db.WithContext(ctx).Create(&value).Error
}

func (r *Repository) MarkFirewallTransitionMutation(ctx context.Context, operationID string, markerUnixNano int64) error {
	if r == nil || r.db == nil || markerUnixNano <= 0 {
		return ErrFirewallAuthorityConflict
	}
	update := r.db.WithContext(ctx).Model(&FirewallContributionTransitionModel{}).
		Where("operation_id = ? AND state = ? AND marker_unix_nano = 0", operationID, "PREPARED").
		Updates(map[string]any{"state": "MUTATING", "marker_unix_nano": markerUnixNano, "updated_at": time.Now().UTC().UnixNano()})
	if update.Error != nil {
		return update.Error
	}
	if update.RowsAffected != 1 {
		return ErrFirewallAuthorityConflict
	}
	return nil
}

func (r *Repository) MarkFirewallTransitionMutationCompleted(ctx context.Context, operationID string, completedUnixNano int64) error {
	if r == nil || r.db == nil || completedUnixNano <= 0 {
		return ErrFirewallAuthorityConflict
	}
	update := r.db.WithContext(ctx).Model(&FirewallContributionTransitionModel{}).
		Where("operation_id = ? AND state = ? AND marker_unix_nano > 0 AND mutation_completed_unix_nano = 0 AND marker_unix_nano < ?", operationID, "MUTATING", completedUnixNano).
		Updates(map[string]any{"mutation_completed_unix_nano": completedUnixNano, "updated_at": time.Now().UTC().UnixNano()})
	if update.Error != nil {
		return update.Error
	}
	if update.RowsAffected != 1 {
		return ErrFirewallAuthorityConflict
	}
	return nil
}

// CommitFirewallAuthority atomically changes only the operation-owned
// contribution and publishes the freshly composed aggregate. The exact
// composition fence protects the helper candidate/DB commit interval, while
// expectedContributionRevision prevents an old operation from reverting a
// newer update of the same contribution.
func (r *Repository) CommitFirewallAuthority(ctx context.Context, operationID, expectedCompositionRevision, expectedContributionRevision string, replacement *FirewallContributionModel, composition FirewallCompositionModel, transitionState string) error {
	if r == nil || r.db == nil {
		return errors.New("server-protection repository is not initialized")
	}
	return r.durableTransition(ctx, "firewall_composition_commit", func(tx *gorm.DB) error {
		var transition FirewallContributionTransitionModel
		if err := tx.Where("operation_id = ?", operationID).First(&transition).Error; err != nil {
			return err
		}
		switch transitionState {
		case "APPLIED":
			if transition.State != "MUTATING" || transition.MarkerUnixNano <= 0 || transition.MutationCompletedUnixNano <= transition.MarkerUnixNano || replacement == nil ||
				replacement.ContributionID != transition.ContributionID || replacement.SemanticRevision != transition.DesiredSemanticRevision || composition.Revision != transition.AfterCompositionRevision {
				return ErrFirewallAuthorityConflict
			}
		case "ROLLED_BACK":
			if transition.State != "MUTATING" && transition.State != "APPLIED" && transition.State != "HEALTH_VERIFIED" {
				return ErrFirewallAuthorityConflict
			}
			if transition.PreviousPresent {
				if replacement == nil || replacement.ContributionID != transition.ContributionID || replacement.SemanticRevision != transition.PreviousSemanticRevision {
					return ErrFirewallAuthorityConflict
				}
			} else if replacement != nil {
				return ErrFirewallAuthorityConflict
			}
		default:
			return ErrFirewallAuthorityConflict
		}
		var currentComposition FirewallCompositionModel
		err := tx.Where("id = ?", 1).First(&currentComposition).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if expectedCompositionRevision != "" {
				return ErrFirewallAuthorityConflict
			}
		} else if err != nil {
			return err
		} else if currentComposition.Revision != expectedCompositionRevision {
			return ErrFirewallAuthorityConflict
		}

		var current FirewallContributionModel
		err = tx.Where("contribution_id = ?", transition.ContributionID).First(&current).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if expectedContributionRevision != "" {
				return ErrFirewallAuthorityConflict
			}
		} else if err != nil {
			return err
		} else if current.SemanticRevision != expectedContributionRevision {
			return ErrFirewallAuthorityConflict
		}

		now := time.Now().UTC().UnixNano()
		if replacement == nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				if deleteResult := tx.Where("contribution_id = ?", transition.ContributionID).Delete(&FirewallContributionModel{}); deleteResult.Error != nil || deleteResult.RowsAffected != 1 {
					if deleteResult.Error != nil {
						return deleteResult.Error
					}
					return ErrFirewallAuthorityConflict
				}
			}
		} else {
			value := *replacement
			value.AppliedOperationID = operationID
			value.UpdatedAt = now
			if value.CreatedAt == 0 {
				if current.CreatedAt != 0 {
					value.CreatedAt = current.CreatedAt
				} else {
					value.CreatedAt = now
				}
			}
			if err := tx.Save(&value).Error; err != nil {
				return err
			}
		}

		if composition.Schema == "" {
			if err := tx.Where("id = ?", 1).Delete(&FirewallCompositionModel{}).Error; err != nil {
				return err
			}
		} else {
			composition.ID = 1
			composition.State = "ACTIVE"
			composition.AppliedOperationID = operationID
			composition.UpdatedAt = now
			if err := tx.Save(&composition).Error; err != nil {
				return err
			}
		}
		observation := FirewallObservationModel{ID: 1, Schema: FirewallObservationSchemaV1, State: "ABSENT", ObservedAt: now, UpdatedAt: now}
		if composition.Schema != "" {
			observation.State = "MATCHING"
			observation.HasCommittedAuthority = true
			observation.CommittedCompositionRevision = composition.Revision
			observation.ManagedTablePresent = true
			observation.CurrentRevision = composition.ManagedPlanRevision
			observation.CurrentSemanticSHA256 = composition.CandidateSemanticSHA256
			observation.CurrentTimedMembershipSHA256 = composition.CandidateTimedMembershipSHA256
			observation.ExpectedTimedMembershipSHA256 = composition.CandidateTimedMembershipSHA256
		}
		if err := validateFirewallObservation(observation); err != nil {
			return err
		}
		if err := tx.Save(&observation).Error; err != nil {
			return err
		}
		transitionValues := map[string]any{"state": transitionState, "updated_at": now}
		if transitionState == "ROLLED_BACK" && currentComposition.Runtime.State == "RESTORE_FAILED" {
			transitionValues["runtime_retirement_reason"] = currentComposition.Runtime.Reason
		}
		update := tx.Model(&FirewallContributionTransitionModel{}).
			Where("operation_id = ?", operationID).
			Updates(transitionValues)
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return ErrFirewallAuthorityConflict
		}
		return nil
	})
}

func (r *Repository) RecordFirewallTransitionHealth(ctx context.Context, operationID, providerInstance string, generation uint64, observationRevision string, startedUnixNano, completedUnixNano, expiresUnixNano int64) error {
	if r == nil || r.db == nil || providerInstance == "" || generation == 0 || observationRevision == "" || startedUnixNano <= 0 || completedUnixNano < startedUnixNano || expiresUnixNano <= completedUnixNano {
		return ErrFirewallAuthorityConflict
	}
	update := r.db.WithContext(ctx).Model(&FirewallContributionTransitionModel{}).
		Where("operation_id = ? AND state = ? AND marker_unix_nano > 0 AND mutation_completed_unix_nano > marker_unix_nano", operationID, "APPLIED").
		Where("mutation_completed_unix_nano <= ?", startedUnixNano).
		Updates(map[string]any{"health_provider_instance": providerInstance, "health_generation": generation, "health_observation_revision": observationRevision, "health_started_unix_nano": startedUnixNano, "health_completed_unix_nano": completedUnixNano, "health_expires_unix_nano": expiresUnixNano, "state": "HEALTH_VERIFIED", "updated_at": time.Now().UTC().UnixNano()})
	if update.Error != nil {
		return update.Error
	}
	if update.RowsAffected != 1 {
		return ErrFirewallAuthorityConflict
	}
	return nil
}

func (r *Repository) SetFirewallTransitionState(ctx context.Context, operationID, from, to string) error {
	if r == nil || r.db == nil {
		return errors.New("server-protection repository is not initialized")
	}
	update := r.db.WithContext(ctx).Model(&FirewallContributionTransitionModel{}).
		Where("operation_id = ? AND state = ?", operationID, from).
		Updates(map[string]any{"state": to, "updated_at": time.Now().UTC().UnixNano()})
	if update.Error != nil {
		return update.Error
	}
	if update.RowsAffected != 1 {
		return ErrFirewallAuthorityConflict
	}
	return nil
}

// RecordFirewallObservation atomically fences the committed composition seen
// by the caller and stores only the latest host-local live-kernel fact. It
// never changes contribution or committed composition authority.
func (r *Repository) RecordFirewallObservation(ctx context.Context, value FirewallObservationModel, expectedCompositionRevision string) error {
	if r == nil || r.db == nil {
		return ErrFirewallAuthorityConflict
	}
	return r.durableTransition(ctx, "firewall_observation", func(tx *gorm.DB) error {
		var count int64
		query := tx.Model(&FirewallCompositionModel{}).Where("id = ?", 1)
		if expectedCompositionRevision != "" {
			query = query.Where("revision = ?", expectedCompositionRevision)
		}
		if err := query.Count(&count).Error; err != nil {
			return err
		}
		if expectedCompositionRevision == "" && count != 0 || expectedCompositionRevision != "" && count != 1 {
			return ErrFirewallAuthorityConflict
		}
		now := time.Now().UTC().UnixNano()
		value.ID = 1
		value.Schema = FirewallObservationSchemaV1
		value.HasCommittedAuthority = expectedCompositionRevision != ""
		value.CommittedCompositionRevision = expectedCompositionRevision
		value.ObservedAt = now
		value.UpdatedAt = now
		if err := validateFirewallObservation(value); err != nil {
			return err
		}
		return tx.Save(&value).Error
	})
}

// RetireFirewallAuthorityAfterRuntimeLoss atomically closes the durable
// aggregate after a fresh ABSENT observation proves that the volatile kernel
// table is gone. The operation fence is supplied by the startup reconciler.
// Contributions are desired-state inputs only while their aggregate has live
// authority, so retiring the aggregate also retires those inputs and every
// transition that claimed the lost table. The operation manager terminalizes
// each APPLIED operation separately under its own revision fence.
func (r *Repository) RetireFirewallAuthorityAfterRuntimeLoss(ctx context.Context, operationID string, operationRevision int, expectedCompositionRevision string) error {
	if r == nil || r.db == nil || operationID == "" || operationRevision <= 0 || expectedCompositionRevision != "" && !firewallIdentityPattern.MatchString(expectedCompositionRevision) {
		return ErrFirewallAuthorityConflict
	}
	return r.durableTransition(ctx, "firewall_retirement", func(tx *gorm.DB) error {
		var operation OperationLockModel
		if err := tx.Where("operation_id = ? AND revision = ? AND kind = ? AND state IN ?", operationID, operationRevision, "firewall", []string{"applied", "reconcile_required", "restoring_runtime"}).First(&operation).Error; err != nil {
			return ErrFirewallAuthorityConflict
		}
		var observation FirewallObservationModel
		if err := tx.Where("id = ?", 1).First(&observation).Error; err != nil || observation.State != "ABSENT" || observation.ManagedTablePresent {
			return ErrFirewallAuthorityConflict
		}
		var composition FirewallCompositionModel
		err := tx.Where("id = ?", 1).First(&composition).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			var count int64
			if expectedCompositionRevision != "" || observation.HasCommittedAuthority || observation.CommittedCompositionRevision != "" || tx.Model(&FirewallContributionModel{}).Count(&count).Error != nil || count != 0 {
				return ErrFirewallAuthorityConflict
			}
			return nil
		}
		if err != nil || composition.State != "ACTIVE" || composition.Revision != expectedCompositionRevision || !observation.HasCommittedAuthority || observation.CommittedCompositionRevision != expectedCompositionRevision {
			return ErrFirewallAuthorityConflict
		}
		now := time.Now().UTC().UnixNano()
		if err := tx.Where("1 = 1").Delete(&FirewallContributionModel{}).Error; err != nil {
			return err
		}
		deleted := tx.Where("id = ? AND revision = ? AND state = ?", 1, expectedCompositionRevision, "ACTIVE").Delete(&FirewallCompositionModel{})
		if deleted.Error != nil || deleted.RowsAffected != 1 {
			if deleted.Error != nil {
				return deleted.Error
			}
			return ErrFirewallAuthorityConflict
		}
		if err := tx.Model(&FirewallContributionTransitionModel{}).
			Where("state IN ?", []string{"APPLIED", "HEALTH_VERIFIED"}).
			Updates(map[string]any{"state": "RETIRED_RUNTIME_LOSS", "runtime_retirement_reason": composition.Runtime.Reason, "updated_at": now}).Error; err != nil {
			return err
		}
		retiredObservation := FirewallObservationModel{ID: 1, Schema: FirewallObservationSchemaV1, State: "ABSENT", ObservedAt: now, UpdatedAt: now}
		if err := validateFirewallObservation(retiredObservation); err != nil {
			return err
		}
		return tx.Save(&retiredObservation).Error
	})
}

func validateFirewallObservation(value FirewallObservationModel) error {
	allowed := map[string]bool{"ABSENT": true, "MATCHING": true, "FOREIGN": true, "DRIFTED": true, "UNAVAILABLE": true}
	if value.Schema != FirewallObservationSchemaV1 || !allowed[value.State] || len(value.Reason) > 96 || value.ObservedAt <= 0 ||
		value.HasCommittedAuthority != (value.CommittedCompositionRevision != "") || value.CommittedCompositionRevision != "" && !firewallIdentityPattern.MatchString(value.CommittedCompositionRevision) {
		return ErrFirewallAuthorityConflict
	}
	if value.CurrentRevision != "" && !firewallIdentityPattern.MatchString(value.CurrentRevision) || value.CurrentSemanticSHA256 != "" && !firewallIdentityPattern.MatchString(value.CurrentSemanticSHA256) ||
		value.CurrentTimedMembershipSHA256 != "" && !firewallIdentityPattern.MatchString(value.CurrentTimedMembershipSHA256) || value.ExpectedTimedMembershipSHA256 != "" && !firewallIdentityPattern.MatchString(value.ExpectedTimedMembershipSHA256) {
		return ErrFirewallAuthorityConflict
	}
	if !value.ManagedTablePresent && (value.CurrentRevision != "" || value.CurrentSemanticSHA256 != "" || value.CurrentTimedMembershipSHA256 != "" || value.ExpectedTimedMembershipSHA256 != "") || value.State == "ABSENT" && value.ManagedTablePresent ||
		value.State == "MATCHING" && (!value.HasCommittedAuthority || !value.ManagedTablePresent || value.CurrentRevision == "" || value.CurrentSemanticSHA256 == "" || (value.CurrentTimedMembershipSHA256 == "") != (value.ExpectedTimedMembershipSHA256 == "") || value.CurrentTimedMembershipSHA256 != value.ExpectedTimedMembershipSHA256) {
		return ErrFirewallAuthorityConflict
	}
	return nil
}

// ReconcileRestoredFirewallAuthority never mutates the host firewall. A
// restored aggregate is deliberately distrusted until an operator resolves
// it against fresh managed-table evidence on this host.
func ReconcileRestoredFirewallAuthority(ctx context.Context, db *gorm.DB, now time.Time) error {
	if db == nil || !db.Migrator().HasTable(&FirewallCompositionModel{}) {
		return nil
	}
	// Import owns schema replacement; resolve optional tables before the short
	// write-first transaction so schema reads cannot create a stale snapshot.
	hasObservation := db.Migrator().HasTable(&FirewallObservationModel{})
	hasTransition := db.Migrator().HasTable(&FirewallContributionTransitionModel{})
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		stamp := now.UTC().UnixNano()
		if hasObservation {
			if err := tx.Where("id = ?", 1).Delete(&FirewallObservationModel{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&FirewallCompositionModel{}).Where("state = ?", "ACTIVE").Updates(map[string]any{"state": "RECOVERY_REQUIRED", "updated_at": stamp}).Error; err != nil {
			return err
		}
		if !hasTransition {
			return nil
		}
		return tx.Model(&FirewallContributionTransitionModel{}).
			Where("state IN ?", []string{"PREPARED", "MUTATING", "APPLIED", "HEALTH_VERIFIED", "ROLLING_BACK"}).
			Updates(map[string]any{"state": "RECOVERY_REQUIRED", "updated_at": stamp}).Error
	})
}
