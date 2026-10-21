//go:build !minimal

package importxui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/operationcoordination"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

func NewOperationID() (string, error) {
	id, err := common.RandomUUID()
	if err != nil {
		return "", err
	}
	return "import:" + id, nil
}

func PlanRevision(plan MigrationPlan) (string, error) {
	plan.Source.Path = "" // an upload's temporary path is not semantic input
	raw, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func (r *Report) startOperation(id string) error {
	reference := operationcoordination.NewReference("import-xui", "database", id)
	if reference.Owner != "import-xui" {
		return errors.New("invalid import operation identity")
	}
	change := operationcoordination.Change{Reference: reference, Revision: 1,
		Phase: "APPLY", OwnerPhase: "applying", Outcome: operationcoordination.OutcomePending}
	r.Change = &change
	r.projectOperation(operationcoordination.OutcomePending, "APPLY", "applying")
	return nil
}

func (r *Report) projectOperation(outcome operationcoordination.Outcome, phase, ownerPhase string) {
	if r.Change == nil {
		return
	}
	r.Change.Outcome, r.Change.Phase, r.Change.OwnerPhase = outcome, phase, ownerPhase
	r.Change.CheckpointRef = r.BackupPath
	if outcome == operationcoordination.OutcomeFailed {
		changed := r.Change.WithProblem("import_apply_failed")
		r.Change = &changed
	}
	// Item progress remains the import owner's transient count. The shared
	// envelope must not relabel items as bytes or invent cancellation support.
	job := operationcoordination.ProjectJob(*r.Change, nil, outcome == operationcoordination.OutcomePending)
	r.Job = &job
}

type HistoryEntry struct {
	model.AuditEvent
	Change *operationcoordination.Change `json:"change,omitempty"`
	Job    *operationcoordination.Job    `json:"job,omitempty"`
}

// ProjectHistory reconstructs only immutable outcomes proven by owner history.
// Legacy events have no invented operation, pending job or recovery receipt.
func ProjectHistory(events []model.AuditEvent) []HistoryEntry {
	entries := make([]HistoryEntry, 0, len(events))
	for _, event := range events {
		entry := HistoryEntry{AuditEvent: event}
		var details struct {
			OperationID  string `json:"operationId"`
			PlanRevision string `json:"planRevision"`
			Backup       string `json:"backup"`
		}
		if json.Unmarshal(event.Details, &details) == nil && details.OperationID != "" {
			reference := operationcoordination.NewReference("import-xui", "database", details.OperationID)
			if reference.Owner == "import-xui" {
				change := operationcoordination.Change{Reference: reference, Revision: 1, ExpectedRevision: details.PlanRevision}
				if details.Backup != "" && filepath.Base(details.Backup) == details.Backup {
					change.CheckpointRef = details.Backup
				}
				switch event.Event {
				case "panel_import":
					change.Phase, change.OwnerPhase, change.Outcome = "COMMIT", "committed", operationcoordination.OutcomeCommitted
				case "panel_import_failed":
					change.Phase, change.OwnerPhase, change.Outcome = "FAILED", "failed", operationcoordination.OutcomeFailed
					change = change.WithProblem("import_apply_failed")
				case "panel_import_rollback":
					change.Phase, change.OwnerPhase, change.Outcome = "ROLLBACK", "rolled_back", operationcoordination.OutcomeRolledBack
					change.RollbackVerified = true
				}
				if change.Outcome != "" {
					entry.Change = &change
					job := operationcoordination.ProjectJob(change, nil, false)
					entry.Job = &job
				}
			}
		}
		entries = append(entries, entry)
	}
	return entries
}
