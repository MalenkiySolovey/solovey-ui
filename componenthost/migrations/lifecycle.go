package migrations

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const ScopeComponent = "component"

var journalAdmission = struct {
	sync.Mutex
	active map[string]struct{}
}{active: map[string]struct{}{}}

type Step struct {
	OwnerID  string
	StepID   string
	Checksum string
	Version  string
}

func StepFor(item manifest.Manifest) (Step, error) {
	item = item.Normalized()
	if err := item.Validate(); err != nil {
		return Step{}, err
	}
	checksum := item.Database.MigrationChecksum
	version := item.Database.MigrationVersion
	if checksum == "" {
		data, _ := json.Marshal(struct{ ID, Version, Delivery string }{item.ID, item.Version, string(item.Delivery)})
		sum := sha256.Sum256(data)
		checksum, version = hex.EncodeToString(sum[:]), item.Version
	}
	return Step{OwnerID: item.ID, StepID: "component-schema:" + version,
		Checksum: checksum, Version: version}, nil
}

func RecordUnavailableOwner(db *gorm.DB, item installstate.InstalledComponent) error {
	if db == nil {
		return errors.New("component migration journal database is not initialized")
	}
	if err := manifest.ValidateID(item.ID); err != nil {
		return err
	}
	now := time.Now().Unix()
	digest := sha256.Sum256([]byte(item.ID + ":" + string(item.Delivery) + ":unavailable"))
	row := model.MigrationJournal{Scope: ScopeComponent, OwnerID: item.ID, StepID: "installed-owner-availability",
		Checksum: hex.EncodeToString(digest[:]), State: "RECOVERY_REQUIRED", CompatibilityState: "OWNER_UNAVAILABLE",
		ErrorCode: "installed_owner_unavailable", DropState: "BLOCKED", StartedAt: now, FinishedAt: now, UpdatedAt: now}
	return db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "scope"}, {Name: "owner_id"}, {Name: "step_id"}},
		DoUpdates: clause.Assignments(map[string]any{"checksum": row.Checksum, "state": row.State,
			"compatibility_state": row.CompatibilityState, "error_code": row.ErrorCode, "drop_state": row.DropState,
			"finished_at": row.FinishedAt, "updated_at": row.UpdatedAt})}).Create(&row).Error
}

func BeginStep(db *gorm.DB, step Step) (bool, error) {
	if db == nil || step.OwnerID == "" || step.StepID == "" || len(step.Checksum) != 64 {
		return false, errors.New("component migration step is invalid")
	}
	journalAdmission.Lock()
	defer journalAdmission.Unlock()
	activeKey := step.OwnerID + "\x00" + step.StepID
	if _, active := journalAdmission.active[activeKey]; active {
		return false, errors.New("component migration step is already running in this process")
	}
	run := false
	var admissionErr error
	err := db.Transaction(func(tx *gorm.DB) error {
		var existing model.MigrationJournal
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&existing, "scope = ? AND owner_id = ? AND step_id = ?", ScopeComponent, step.OwnerID, step.StepID).Error
		now := time.Now().Unix()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			row := model.MigrationJournal{Scope: ScopeComponent, OwnerID: step.OwnerID, StepID: step.StepID, Checksum: step.Checksum,
				State: "RUNNING", CompatibilityState: "COMPATIBLE", ErrorCode: "", DropState: "NOT_REQUESTED",
				StartedAt: now, UpdatedAt: now}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			run = true
			return nil
		}
		if err != nil {
			return err
		}
		if existing.Checksum != step.Checksum {
			if err := tx.Model(&existing).Updates(map[string]any{"state": "RECOVERY_REQUIRED", "compatibility_state": "CHECKSUM_MISMATCH",
				"error_code": "migration_checksum_mismatch", "finished_at": now, "updated_at": now}).Error; err != nil {
				return err
			}
			admissionErr = errors.New("component migration checksum changed")
			return nil
		}
		if existing.State == "APPLIED" {
			return nil
		}
		switch existing.State {
		case "PENDING", "RUNNING", "FAILED", "RECOVERY_REQUIRED":
			result := tx.Model(&model.MigrationJournal{}).
				Where("scope = ? AND owner_id = ? AND step_id = ? AND checksum = ? AND state = ?",
					ScopeComponent, step.OwnerID, step.StepID, step.Checksum, existing.State).
				Updates(map[string]any{"state": "RUNNING", "compatibility_state": "COMPATIBLE", "error_code": "",
					"started_at": now, "finished_at": 0, "updated_at": now, "retry_count": gorm.Expr("retry_count + 1")})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errors.New("component migration admission changed concurrently")
			}
			run = true
			return nil
		default:
			if err := tx.Model(&existing).Updates(map[string]any{"state": "RECOVERY_REQUIRED", "compatibility_state": "STATE_INVALID",
				"error_code": "migration_state_invalid", "finished_at": now, "updated_at": now}).Error; err != nil {
				return err
			}
			admissionErr = errors.New("component migration journal state is invalid")
			return nil
		}
	})
	if err == nil && admissionErr == nil && run {
		journalAdmission.active[activeKey] = struct{}{}
	}
	return run, errors.Join(err, admissionErr)
}

func FinishStep(db *gorm.DB, step Step, migrationErr error) error {
	activeKey := step.OwnerID + "\x00" + step.StepID
	defer func() {
		journalAdmission.Lock()
		delete(journalAdmission.active, activeKey)
		journalAdmission.Unlock()
	}()
	if db == nil {
		return errors.New("component migration journal database is not initialized")
	}
	state, errorCode := "APPLIED", ""
	if migrationErr != nil {
		state, errorCode = "FAILED", "component_migration_failed"
	}
	now := time.Now().Unix()
	result := db.Model(&model.MigrationJournal{}).Where("scope = ? AND owner_id = ? AND step_id = ? AND checksum = ? AND state = ?",
		ScopeComponent, step.OwnerID, step.StepID, step.Checksum, "RUNNING").Updates(map[string]any{
		"state": state, "error_code": errorCode, "finished_at": now, "updated_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("component migration %s state changed", step.StepID)
	}
	return nil
}
