package sshmanagement

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	operationcoordination "github.com/MalenkiySolovey/solovey-ui/internal/ops/operationcoordination"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
	"gorm.io/gorm"
)

const (
	maxTerminalSSHOperations    = 128
	maxTerminalSSHBytes         = 8 << 20
	maxProtectedSSHClosures     = 32
	maxSSHPostureSnapshots      = 16
	maxSSHPostureBytes          = 2 << 20
	maxTerminalSSHRecoveryRows  = 256
	maxTerminalSSHRecoveryBytes = 2 << 20
	maxSSHTimelinePage          = 128
	sshTerminalRetentionAge     = 30 * 24 * time.Hour
	sshObservationRetentionAge  = 30 * 24 * time.Hour
)

var errSSHProtectedClosureCapacity = errors.New("SSH protected terminal closure capacity exceeded")

// HistoryRetentionV1 is the operator-visible replay horizon. Authority that
// is active, recoverable, restored-untrusted, broker-referenced, or referenced
// by live recovery evidence is outside these pruning budgets and is retained.
type HistoryRetentionV1 struct {
	TerminalOperations int   `json:"terminalOperations"`
	TerminalAgeSeconds int64 `json:"terminalAgeSeconds"`
	TerminalBytes      int   `json:"terminalBytes"`
	TimelinePage       int   `json:"timelinePage"`
	PostureSnapshots   int   `json:"postureSnapshots"`
	PostureBytes       int   `json:"postureBytes"`
	RecoveryRows       int   `json:"recoveryRows"`
	RecoveryBytes      int   `json:"recoveryBytes"`
}

func HistoryRetentionPolicy() HistoryRetentionV1 {
	return HistoryRetentionV1{TerminalOperations: maxTerminalSSHOperations, TerminalAgeSeconds: int64(sshTerminalRetentionAge / time.Second),
		TerminalBytes: maxTerminalSSHBytes, TimelinePage: maxSSHTimelinePage, PostureSnapshots: maxSSHPostureSnapshots,
		PostureBytes: maxSSHPostureBytes, RecoveryRows: maxTerminalSSHRecoveryRows, RecoveryBytes: maxTerminalSSHRecoveryBytes}
}

func (r Repository) PruneHistory(ctx context.Context, now time.Time) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	return operationcoordination.SerializeAdmission(func() error {
		return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return pruneSSHHistoryTx(tx, now.UTC()) })
	})
}

func pruneSSHHistoryTx(tx *gorm.DB, now time.Time) error {
	if err := pruneTerminalCandidatesTx(tx, now); err != nil {
		return err
	}
	if err := pruneOrphanOperationRowsTx(tx); err != nil {
		return err
	}
	if err := prunePostureTx(tx, now); err != nil {
		return err
	}
	return pruneRecoveryEvidenceTx(tx, now)
}

func pruneOrphanOperationRowsTx(tx *gorm.DB) error {
	operations := tx.Model(&model.SSHManagementCandidate{}).Select("operation_id")
	for _, row := range []any{&model.SSHReconnectChallenge{}, &model.SSHManagedArtifactCheckpoint{}, &model.SSHManagementJournal{}} {
		if err := tx.Where("operation_id NOT IN (?)", operations).Delete(row).Error; err != nil {
			return err
		}
	}
	return nil
}

func enforceProtectedClosureCapacity(tx *gorm.DB, now time.Time) error {
	var terminal []model.SSHManagementCandidate
	if err := tx.Where("state IN ?", terminalStates()).Find(&terminal).Error; err != nil {
		return err
	}
	liveTargets, err := liveRecoveryTargets(tx, now)
	if err != nil {
		return err
	}
	protected := 0
	for _, row := range terminal {
		if !safeTerminalCandidate(row, liveTargets) {
			protected++
		}
	}
	if protected >= maxProtectedSSHClosures {
		return errSSHProtectedClosureCapacity
	}
	return nil
}

func pruneTerminalCandidatesTx(tx *gorm.DB, now time.Time) error {
	var rows []model.SSHManagementCandidate
	if err := tx.Where("state IN ?", terminalStates()).Order("updated_at desc, operation_id desc").Find(&rows).Error; err != nil {
		return err
	}
	liveTargets, err := liveRecoveryTargets(tx, now)
	if err != nil {
		return err
	}
	cutoff := now.Add(-sshTerminalRetentionAge).Unix()
	kept, keptBytes := 0, 0
	for _, row := range rows {
		if !safeTerminalCandidate(row, liveTargets) {
			continue
		}
		size, err := terminalCandidateBytes(tx, row)
		if err != nil {
			return err
		}
		if kept < maxTerminalSSHOperations && row.UpdatedAt >= cutoff && keptBytes+size <= maxTerminalSSHBytes {
			kept++
			keptBytes += size
			continue
		}
		if err := deleteTerminalCandidateTx(tx, row.OperationID); err != nil {
			return err
		}
	}
	return nil
}

func safeTerminalCandidate(row model.SSHManagementCandidate, liveTargets map[string]bool) bool {
	if row.RestoredUntrusted || liveTargets[row.OperationID] || !row.BrokerStageReleased {
		return false
	}
	return row.State == string(domain.StateCommitted) || row.State == string(domain.StateRolledBack)
}

func liveRecoveryTargets(tx *gorm.DB, now time.Time) (map[string]bool, error) {
	var targets []string
	if err := liveRecoveryEvidence(tx, now).Where("target_operation != ''").Distinct().Pluck("target_operation", &targets).Error; err != nil {
		return nil, err
	}
	result := make(map[string]bool, len(targets))
	for _, target := range targets {
		result[target] = true
	}
	return result, nil
}

func terminalCandidateBytes(tx *gorm.DB, row model.SSHManagementCandidate) (int, error) {
	data, _ := json.Marshal(row)
	total := len(data) + 128
	var checkpoint model.SSHManagedArtifactCheckpoint
	if err := tx.Where("operation_id = ?", row.OperationID).Take(&checkpoint).Error; err == nil {
		total += len(checkpoint.PriorContent) + len(checkpoint.PriorOwner) + len(checkpoint.PriorGroup) + len(checkpoint.PriorModeClass) +
			len(checkpoint.PriorDigest) + len(checkpoint.StagedArtifactDigest) + len(checkpoint.StagedConfigurationRevision) + 128
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}
	var challenge model.SSHReconnectChallenge
	if err := tx.Where("operation_id = ?", row.OperationID).Take(&challenge).Error; err == nil {
		challengeData, _ := json.Marshal(challenge)
		total += len(challengeData) + len(challenge.VerifierDigest) + len(challenge.EndpointID) + len(challenge.PrincipalID) + 128
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}
	var journals []model.SSHManagementJournal
	if err := tx.Where("operation_id = ?", row.OperationID).Find(&journals).Error; err != nil {
		return 0, err
	}
	for _, journal := range journals {
		journalData, _ := json.Marshal(journal)
		total += len(journalData) + 64
	}
	return total, nil
}

func deleteTerminalCandidateTx(tx *gorm.DB, operationID string) error {
	for _, query := range []any{&model.SSHReconnectChallenge{}, &model.SSHManagedArtifactCheckpoint{}, &model.SSHManagementJournal{}} {
		if err := tx.Where("operation_id = ?", operationID).Delete(query).Error; err != nil {
			return err
		}
	}
	return tx.Where("operation_id = ?", operationID).Delete(&model.SSHManagementCandidate{}).Error
}

func prunePostureTx(tx *gorm.DB, now time.Time) error {
	var rows []model.SSHPostureSnapshot
	if err := tx.Order("observed_at desc, id desc").Find(&rows).Error; err != nil {
		return err
	}
	cutoff := now.Add(-sshObservationRetentionAge).Unix()
	kept, keptBytes := 0, 0
	for _, row := range rows {
		size := len(row.PayloadJSON) + len(row.SemanticRevision) + 96
		keepNewest := kept == 0
		if keepNewest || kept < maxSSHPostureSnapshots && row.ObservedAt >= cutoff && keptBytes+size <= maxSSHPostureBytes {
			kept++
			keptBytes += size
			continue
		}
		if err := tx.Delete(&model.SSHPostureSnapshot{}, row.ID).Error; err != nil {
			return err
		}
	}
	return nil
}

func pruneRecoveryEvidenceTx(tx *gorm.DB, now time.Time) error {
	var rows []model.SSHRecoveryEvidence
	if err := tx.Order("updated_at desc, id desc").Find(&rows).Error; err != nil {
		return err
	}
	cutoff := now.Add(-sshObservationRetentionAge).Unix()
	kept, keptBytes := 0, 0
	for _, row := range rows {
		if row.VerificationState == "verified" && row.ConsumedAt == 0 && row.ExpiresAt > now.Unix() {
			continue
		}
		data, _ := json.Marshal(row)
		size := len(data) + 96
		if kept < maxTerminalSSHRecoveryRows && row.UpdatedAt >= cutoff && keptBytes+size <= maxTerminalSSHRecoveryBytes {
			kept++
			keptBytes += size
			continue
		}
		if err := tx.Delete(&model.SSHRecoveryEvidence{}, "id = ?", row.ID).Error; err != nil {
			return err
		}
	}
	return nil
}
