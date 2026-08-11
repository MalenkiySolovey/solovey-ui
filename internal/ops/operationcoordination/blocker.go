// Package operationcoordination owns the neutral cross-domain mutation fence.
// It stores no authority and does not mutate; each domain still owns its own
// compare-and-swap state machine.
package operationcoordination

import (
	"context"
	"errors"
	"sync"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/gorm"
)

var admissionMu sync.Mutex

// SerializeAdmission closes the in-process check/create race between domain
// repositories. Durable domain rows remain the authority across restarts.
func SerializeAdmission(run func() error) error {
	if run == nil {
		return errors.New("operation admission callback is unavailable")
	}
	admissionMu.Lock()
	defer admissionMu.Unlock()
	return run()
}

type Domain string

const (
	DomainData       Domain = "data"
	DomainUpdate     Domain = "update"
	DomainDeployment Domain = "deployment"
	DomainSSH        Domain = "ssh"
)

func Blocker(ctx context.Context, db *gorm.DB, ignore Domain) string {
	if db == nil {
		return "operation_database_unavailable"
	}
	checks := []struct {
		domain    Domain
		model     any
		terminals []string
		code      string
	}{
		{DomainData, &model.DataLifecycleOperation{}, []string{"APPLIED", "FAILED", "ROLLED_BACK"}, "data_lifecycle_operation_active"},
		{DomainUpdate, &model.UpdateOperation{}, []string{"APPLIED", "FAILED", "ROLLED_BACK"}, "update_operation_active"},
		{DomainDeployment, &model.DeploymentOperation{}, []string{"COMMITTED", "ROLLED_BACK"}, "deployment_operation_active"},
		{DomainSSH, &model.SSHManagementCandidate{}, []string{"COMMITTED", "ROLLED_BACK"}, "ssh_management_operation_active"},
	}
	for _, check := range checks {
		if check.domain == ignore || !db.Migrator().HasTable(check.model) {
			continue
		}
		var count int64
		if err := db.WithContext(ctx).Model(check.model).Where("UPPER(state) NOT IN ?", check.terminals).Count(&count).Error; err != nil || count > 0 {
			return check.code
		}
	}
	return ""
}
