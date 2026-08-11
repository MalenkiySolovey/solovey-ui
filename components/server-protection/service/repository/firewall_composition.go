package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

var ErrFirewallAuthorityConflict = errors.New("firewall contribution authority conflict")

type FirewallAuthoritySnapshot struct {
	Contributions  []FirewallContributionModel
	Composition    FirewallCompositionModel
	HasComposition bool
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
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
		update := tx.Model(&FirewallContributionTransitionModel{}).
			Where("operation_id = ?", operationID).
			Updates(map[string]any{"state": transitionState, "updated_at": now})
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

// ReconcileRestoredFirewallAuthority never mutates the host firewall. A
// restored aggregate is deliberately distrusted until an operator resolves
// it against fresh managed-table evidence on this host.
func ReconcileRestoredFirewallAuthority(ctx context.Context, db *gorm.DB, now time.Time) error {
	if db == nil || !db.Migrator().HasTable(&FirewallCompositionModel{}) {
		return nil
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		stamp := now.UTC().UnixNano()
		if err := tx.Model(&FirewallCompositionModel{}).Where("state = ?", "ACTIVE").Updates(map[string]any{"state": "RECOVERY_REQUIRED", "updated_at": stamp}).Error; err != nil {
			return err
		}
		if !tx.Migrator().HasTable(&FirewallContributionTransitionModel{}) {
			return nil
		}
		return tx.Model(&FirewallContributionTransitionModel{}).
			Where("state IN ?", []string{"PREPARED", "MUTATING", "APPLIED", "HEALTH_VERIFIED", "ROLLING_BACK"}).
			Updates(map[string]any{"state": "RECOVERY_REQUIRED", "updated_at": stamp}).Error
	})
}
