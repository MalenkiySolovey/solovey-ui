package steps

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/gorm"
)

const ClientDisableReasonStepID = "core-1.13-client-disable-reason"

var ClientDisableReasonChecksum = func() string {
	value := sha256.Sum256([]byte("solovey.core-schema/1.13;clients.access_disable_reason:text:not-null:default-empty;disabled-unclassified=LEGACY_DISABLED;enabled=no-blocker;nonempty-reason-preserved;quota-reset-clears-only-QUOTA_EXHAUSTED/v1"))
	return hex.EncodeToString(value[:])
}()

func addClientDisableReason(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&model.Client{}) {
		return nil
	}
	if !tx.Migrator().HasColumn(&model.Client{}, "AccessDisableReason") {
		if err := tx.Migrator().AddColumn(&model.Client{}, "AccessDisableReason"); err != nil {
			return err
		}
	}
	// Historical enable=false has no durable provenance. Neither traffic
	// counters nor the changes feed may manufacture permission to reactivate.
	return tx.Exec("UPDATE clients SET access_disable_reason = ? WHERE (enable = false OR enable IS NULL) AND (access_disable_reason = '' OR access_disable_reason IS NULL)", model.ClientDisabledLegacy).Error
}
