package repository

import (
	"context"
	"reflect"
	"time"

	"gorm.io/gorm"
)

// FirewallRuntimeBinding is lifecycle evidence on the canonical composition,
// never an alternative policy or an authorization to create a new operation.
type FirewallRuntimeBinding struct {
	Schema                   string `gorm:"size:96"`
	VerifiedBoot             string `gorm:"size:64"`
	AttemptBoot              string `gorm:"size:64"`
	State                    string `gorm:"size:32"`
	Reason                   string `gorm:"size:96"`
	StartedAt                int64  `gorm:"not null;default:0"`
	Deadline                 int64  `gorm:"not null;default:0"`
	MutationAt               int64  `gorm:"not null;default:0"`
	HealthAt                 int64  `gorm:"not null;default:0"`
	HealthRevision           string `gorm:"size:64"`
	ArtifactRevision         string `gorm:"size:128"`
	ArtifactSHA256           string `gorm:"size:64"`
	ArtifactMembershipSHA256 string `gorm:"size:64"`
	CleanupAttempted         bool   `gorm:"not null;default:false"`
}

const FirewallRuntimeSchema = "solovey-ui/firewall-runtime-continuation/v1"

// UpdateFirewallRuntime fences lifecycle progress with the already claimed
// operation and exact composition. The semantic owner supplies the decision.
func (r *Repository) UpdateFirewallRuntime(ctx context.Context, operationID string, operationRevision int, compositionRevision string, before, after FirewallRuntimeBinding) error {
	if r == nil || r.db == nil || after.Schema != FirewallRuntimeSchema || !firewallIdentityPattern.MatchString(after.VerifiedBoot) ||
		(after.AttemptBoot != "" && !firewallIdentityPattern.MatchString(after.AttemptBoot)) || len(after.Reason) > 96 {
		return ErrFirewallAuthorityConflict
	}
	switch after.State {
	case "HEALTH_VERIFIED", "RESTORE_REQUIRED", "RESTORING_RUNTIME", "RESTORE_FAILED":
	default:
		return ErrFirewallAuthorityConflict
	}
	if !validFirewallRuntimeTransition(before, after) {
		return ErrFirewallAuthorityConflict
	}
	return r.durableTransition(ctx, "firewall_runtime", func(tx *gorm.DB) error {
		var operation OperationLockModel
		if err := tx.Where("operation_id = ? AND revision = ? AND state IN ?", operationID, operationRevision, []string{"applying", "applied", "reconcile_required", "restoring_runtime"}).First(&operation).Error; err != nil {
			return runtimeAuthorityReadError(err)
		}
		if operation.Kind != "firewall" {
			return ErrFirewallAuthorityConflict
		}
		var composition FirewallCompositionModel
		if err := tx.Where("id = ? AND revision = ? AND state = ? AND applied_operation_id = ?", 1, compositionRevision, "ACTIVE", operationID).First(&composition).Error; err != nil {
			return runtimeAuthorityReadError(err)
		}
		if !reflect.DeepEqual(composition.Runtime, before) {
			return ErrFirewallAuthorityConflict
		}
		var transition FirewallContributionTransitionModel
		if err := tx.Where("operation_id = ? AND state = ?", operationID, "HEALTH_VERIFIED").First(&transition).Error; err != nil {
			return runtimeAuthorityReadError(err)
		}
		if transition.AfterCompositionRevision != composition.Revision || transition.ManagedPlanRevision != composition.ManagedPlanRevision || transition.CandidateSHA256 != composition.CandidateSHA256 || transition.CandidateSemanticSHA256 != composition.CandidateSemanticSHA256 || transition.CandidateTimedMembershipSHA256 != composition.CandidateTimedMembershipSHA256 {
			return ErrFirewallAuthorityConflict
		}
		composition.Runtime, composition.UpdatedAt = after, time.Now().UTC().UnixNano()
		return tx.Save(&composition).Error
	})
}

func validFirewallRuntimeTransition(before, after FirewallRuntimeBinding) bool {
	if after.State == "HEALTH_VERIFIED" {
		if after.HealthAt <= 0 || !firewallIdentityPattern.MatchString(after.HealthRevision) {
			return false
		}
		if before.Schema == "" {
			return before == (FirewallRuntimeBinding{}) && after.AttemptBoot == "" && after.MutationAt == 0
		}
		return before.State == "RESTORING_RUNTIME" && after.VerifiedBoot == before.AttemptBoot && after.AttemptBoot == before.AttemptBoot &&
			after.MutationAt == before.MutationAt && after.HealthAt > before.MutationAt && after.ArtifactRevision == before.ArtifactRevision && !after.CleanupAttempted
	}
	if before.Schema != FirewallRuntimeSchema || after.VerifiedBoot != before.VerifiedBoot || after.AttemptBoot == "" || after.AttemptBoot == after.VerifiedBoot ||
		after.StartedAt <= 0 || after.Deadline <= after.StartedAt || after.Deadline-after.StartedAt > 300 {
		return false
	}
	if before.State == "HEALTH_VERIFIED" {
		return after.State == "RESTORE_REQUIRED" && after.MutationAt == 0 && after.HealthAt == 0 && after.HealthRevision == "" && !after.CleanupAttempted
	}
	if after.AttemptBoot != before.AttemptBoot || after.StartedAt != before.StartedAt || after.Deadline != before.Deadline {
		return false
	}
	switch after.State {
	case "RESTORE_REQUIRED":
		return before.State == "RESTORE_REQUIRED" && after.MutationAt == 0 && !after.CleanupAttempted
	case "RESTORING_RUNTIME":
		return before.State == "RESTORE_REQUIRED" && after.MutationAt > 0 && after.ArtifactRevision != "" && len(after.ArtifactRevision) <= 128 && firewallIdentityPattern.MatchString(after.ArtifactSHA256) && firewallIdentityPattern.MatchString(after.ArtifactMembershipSHA256) && !after.CleanupAttempted
	case "RESTORE_FAILED":
		return (before.State == "RESTORE_REQUIRED" || before.State == "RESTORING_RUNTIME" || before.State == "RESTORE_FAILED") && after.MutationAt == before.MutationAt && (!before.CleanupAttempted || after.CleanupAttempted)
	}
	return false
}
