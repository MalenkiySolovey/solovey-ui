package app

import (
	"context"
	"time"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	configidentity "github.com/MalenkiySolovey/solovey-ui/config/identity"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/database/schema"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	deploymentdomain "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
	serviceupdate "github.com/MalenkiySolovey/solovey-ui/service/update"
)

func (a *APP) updateHealth(ctx context.Context, operation model.UpdateOperation) serviceupdate.HealthResult {
	reasons := []string{}
	panel := componenthealth.Require(ctx, "core:panel:web", componenthealth.Requirement{Owner: "core", Source: "panel_listener",
		ProofKind: componenthealth.RuntimeReady, DesiredRevision: componenthealth.Revision(operation.Version), MaxAge: 5 * time.Second})
	if panel.Status != componenthealth.StatusOK {
		reasons = append(reasons, "panel_api_health_failed")
	}
	coreReady := a.runtime != nil && a.runtime.Core() != nil && a.runtime.Core().IsRunning()
	if operation.RestartClass == "stack" && !coreReady {
		reasons = append(reasons, "core_runtime_health_failed")
	}
	databaseVersion, coreSchema := "", ""
	runtimeStatus, runtimeErr := dbsqlite.InspectRuntime(dbsqlite.DB())
	if runtimeErr != nil || !runtimeStatus.WALCapable || !runtimeStatus.WALResetSafe {
		reasons = append(reasons, "sqlite_runtime_health_failed")
	}
	if db := dbsqlite.DB(); db == nil {
		reasons = append(reasons, "database_health_unavailable")
	} else {
		var settings []model.Setting
		if err := db.WithContext(ctx).Where("key IN ?", []string{"version", "coreSchemaVersion"}).Find(&settings).Error; err != nil {
			reasons = append(reasons, "database_schema_health_unavailable")
		} else {
			for _, setting := range settings {
				switch setting.Key {
				case "version":
					databaseVersion = setting.Value
				case "coreSchemaVersion":
					coreSchema = setting.Value
				}
			}
		}
		var failedMigrations int64
		if err := db.WithContext(ctx).Model(&model.MigrationJournal{}).Where("state IN ?", []string{"FAILED", "RECOVERY_REQUIRED"}).Count(&failedMigrations).Error; err != nil || failedMigrations != 0 {
			reasons = append(reasons, "owner_migration_health_failed")
		}
	}
	if configidentity.GetVersion() != operation.Version || databaseVersion != operation.Version || coreSchema != schema.CurrentCoreVersion {
		reasons = append(reasons, "release_schema_identity_mismatch")
	}
	evidence := struct {
		OperationID, Version, DatabaseVersion, CoreSchema, SQLiteRevision, PanelStatus string
		CoreReady, Ready                                                               bool
	}{operation.OperationID, operation.Version, databaseVersion, coreSchema, runtimeStatus.Revision, string(panel.Status), coreReady, len(reasons) == 0}
	return serviceupdate.HealthResult{Ready: len(reasons) == 0, ReasonCodes: reasons, Revision: deploymentdomain.Revision(evidence), Evidence: []componenthealth.Result{panel}}
}
