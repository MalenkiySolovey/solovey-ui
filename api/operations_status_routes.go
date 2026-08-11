package api

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/enabledstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	componentregistry "github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	pressuredomain "github.com/MalenkiySolovey/solovey-ui/internal/ops/resourcepressure"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
	deploymentservice "github.com/MalenkiySolovey/solovey-ui/service/deployment"
	pressureService "github.com/MalenkiySolovey/solovey-ui/service/resourcepressure"
	updateservice "github.com/MalenkiySolovey/solovey-ui/service/update"
	"github.com/gin-gonic/gin"
)

type ownerStatus struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Installed   bool   `json:"installed"`
	Available   bool   `json:"available"`
	Enabled     string `json:"enabled"`
	DurableData string `json:"durableData"`
	Backup      string `json:"backup"`
	Restore     string `json:"restore"`
	DropData    string `json:"dropData"`
	ReasonCode  string `json:"reasonCode,omitempty"`
}

type operationsStatusProjection struct {
	GeneratedAt    int64
	Pressure       any
	SQLiteRuntime  any
	SQLiteState    string
	MigrationState string
	MigrationRows  int64
	Owners         []ownerStatus
	BackupState    string
	Update         any
	Deployment     any
	DeploymentErr  error
	ReasonCodes    []string
}

func (a *APIHandler) registerOperationsStatusRoutes(g *gin.RouterGroup) {
	group := g.Group("/v1/operations")
	group.GET("/status", a.operationsStatus)
	group.GET("/pressure", a.resourcePressureStatus)
	group.GET("/pressure/history", a.resourcePressureHistory)
	group.GET("/pressure/cleanup", a.resourcePressureCleanup)
	group.GET("/resource-pressure", a.resourcePressureStatus)
	group.GET("/sqlite", a.sqliteRuntimeStatus)
	group.GET("/migrations", a.migrationStatus)
	group.GET("/data/owners", a.dataOwnerStatus)
}

func (a *APIHandler) operationsStatus(c *gin.Context) {
	if _, ok := requireAuthenticatedSecurityContext(c); !ok {
		return
	}
	pressure := pressureService.Shared().Current()
	sqliteStatus, sqliteErr := dbsqlite.InspectRuntime(dbsqlite.DB())
	owners, ownerReasons := installedOwnerStatus()
	updateManager := a.Update
	if updateManager == nil {
		updateManager = updateservice.SharedLifecycle()
	}
	deploymentManager := a.Deployment
	if deploymentManager == nil {
		deploymentManager = deploymentservice.Shared()
	}
	update := updateManager.Status(c.Request.Context(), release.ChannelMain)
	deployment, deploymentErr := deploymentManager.Status(c.Request.Context())
	migrationState := "APPLIED"
	var migrationRows int64
	if db := dbsqlite.DB(); db == nil || db.WithContext(c.Request.Context()).Model(&model.MigrationJournal{}).Count(&migrationRows).Error != nil {
		migrationState = "UNAVAILABLE"
	} else if migrationRows == 0 {
		migrationState = "NOT_OBSERVED"
	}
	sqliteState := "VERIFIED"
	if sqliteErr != nil || !sqliteStatus.RuntimePinned || !sqliteStatus.WALCapable || !sqliteStatus.WALResetSafe {
		sqliteState = "UNAVAILABLE"
	}
	reasons := append([]string{}, ownerReasons...)
	if sqliteErr != nil {
		reasons = append(reasons, "sqlite_runtime_unavailable")
	}
	if deploymentErr != nil {
		reasons = append(reasons, "deployment_posture_unavailable")
	}
	backupState := "AVAILABLE_WITH_BOUNDS"
	if ownerBackupBlocked(ownerReasons) {
		backupState = "OWNER_UNAVAILABLE_FAIL_CLOSED"
	}
	jsonObj(c, projectOperationsStatus(operationsStatusProjection{
		GeneratedAt: time.Now().Unix(), Pressure: pressure, SQLiteRuntime: sqliteStatus, SQLiteState: sqliteState,
		MigrationState: migrationState, MigrationRows: migrationRows, Owners: owners, BackupState: backupState,
		Update: update, Deployment: deployment, DeploymentErr: deploymentErr, ReasonCodes: reasons,
	}), nil)
}

func projectOperationsStatus(input operationsStatusProjection) gin.H {
	return gin.H{
		"schema":      "solovey.operations-status/v1",
		"generatedAt": input.GeneratedAt,
		"security":    gin.H{"state": "OBSERVED_BY_SECURITY_DOMAIN", "accepted": false, "live": false},
		"deployment":  gin.H{"state": typedAvailability(input.DeploymentErr), "posture": input.Deployment},
		"update":      input.Update,
		"pressure":    input.Pressure,
		"sqlite":      gin.H{"state": input.SQLiteState, "runtime": input.SQLiteRuntime},
		"migrations":  gin.H{"state": input.MigrationState, "journalRows": input.MigrationRows, "targetCoreSchema": "1.11"},
		"owners":      input.Owners,
		"backup":      gin.H{"state": input.BackupState, "restoreExecution": "STEP_UP_REQUIRED"},
		"restore":     gin.H{"rehearsal": "AVAILABLE", "execution": "GLOBAL_ADMISSION_REQUIRED"},
		"dropData":    gin.H{"state": "PREVIEW_REQUIRED", "force": false},
		"evidence":    gin.H{"normalCI": "IN_PROGRESS", "live": "NOT_RUN", "accepted": false},
		"reasonCodes": append([]string(nil), input.ReasonCodes...),
	}
}

func typedAvailability(err error) string {
	if err != nil {
		return "UNAVAILABLE"
	}
	return "OBSERVED"
}

func (a *APIHandler) resourcePressureStatus(c *gin.Context) {
	if _, ok := requireAuthenticatedSecurityContext(c); !ok {
		return
	}
	manager := pressureService.Shared()
	snapshot := manager.Current()
	freshUntil := int64(0)
	limitations := []string{}
	for _, signal := range snapshot.Signals {
		if signal.ExpiresAt > 0 && (freshUntil == 0 || signal.ExpiresAt < freshUntil) {
			freshUntil = signal.ExpiresAt
		}
		if signal.Status != pressuredomain.ProviderSupported && len(limitations) < pressuredomain.MaxReasonCodes {
			reason := signal.ReasonCode
			if reason == "" {
				reason = "provider_" + string(signal.Status)
			}
			limitations = append(limitations, signal.ID+":"+reason)
		}
	}
	admission := gin.H{}
	for _, class := range []string{"status", "interactive", "heavy_mutation", "optional", "recovery_essential"} {
		admission[class] = manager.Admission(class)
	}
	jsonObj(c, gin.H{
		"schema": "solovey.resource-pressure-posture/v1",
		"desired": gin.H{"thresholds": pressuredomain.DefaultThresholds(),
			"sampleIntervalSeconds": int(pressuredomain.SampleInterval / time.Second),
			"recoveryWindowSeconds": int(pressuredomain.RecoveryWindow / time.Second)},
		"selected":         gin.H{"signals": snapshot.Signals},
		"actual":           snapshot,
		"state":            snapshot.State,
		"previousState":    snapshot.PreviousState,
		"reasonCodes":      snapshot.ReasonCodes,
		"revision":         snapshot.Revision,
		"observedAt":       snapshot.ObservedAt,
		"freshUntil":       freshUntil,
		"admissionEffects": admission,
		"limitations":      limitations,
	}, nil)
}

func (a *APIHandler) resourcePressureHistory(c *gin.Context) {
	if _, ok := requireAuthenticatedSecurityContext(c); !ok {
		return
	}
	after, limit, ok := boundedPressureHistoryQuery(c)
	if !ok {
		return
	}
	db := dbsqlite.DB()
	if db == nil {
		c.JSON(http.StatusOK, Msg{Success: false, Msg: "resource_pressure_history_unavailable", Obj: gin.H{
			"state": "UNAVAILABLE", "reasonCode": "resource_pressure_history_unavailable",
		}})
		return
	}
	items := make([]model.ResourcePressureTransition, 0, limit)
	query := db.WithContext(c.Request.Context()).Where("sequence > ?", after).Order("sequence ASC").Limit(limit + 1)
	if err := query.Find(&items).Error; err != nil {
		c.JSON(http.StatusOK, Msg{Success: false, Msg: "resource_pressure_history_unavailable", Obj: gin.H{
			"state": "UNAVAILABLE", "reasonCode": "resource_pressure_history_unavailable",
		}})
		return
	}
	truncated := len(items) > limit
	if truncated {
		items = items[:limit]
	}
	nextAfter := after
	if len(items) > 0 {
		nextAfter = items[len(items)-1].Sequence
	}
	jsonObj(c, gin.H{
		"schema":         "solovey.resource-pressure-history/v1",
		"state":          "OBSERVED",
		"items":          items,
		"after":          after,
		"nextAfter":      nextAfter,
		"limit":          limit,
		"truncated":      truncated,
		"retentionLimit": pressureService.MaxPersistedTransitions,
	}, nil)
}

func (a *APIHandler) resourcePressureCleanup(c *gin.Context) {
	if _, ok := requireAuthenticatedSecurityContext(c); !ok {
		return
	}
	jsonObj(c, pressureCleanupProjection(pressureService.Shared().Current()), nil)
}

func pressureCleanupProjection(snapshot pressuredomain.Snapshot) gin.H {
	state := "NOT_REQUIRED"
	switch snapshot.State {
	case pressuredomain.StateWarning, pressuredomain.StateRecovering:
		state = "SAFE_MITIGATIONS_ACTIVE"
	case pressuredomain.StateConstrained, pressuredomain.StateCritical:
		state = "OPERATOR_ACTION_REQUIRED"
	case pressuredomain.StateUnknown, "":
		state = "OBSERVATION_UNAVAILABLE"
	}
	return gin.H{
		"schema":           "solovey.resource-pressure-cleanup-posture/v1",
		"state":            state,
		"pressureState":    snapshot.State,
		"pressureRevision": snapshot.Revision,
		"previewOnly":      true,
		"automaticActions": []gin.H{
			{"id": "defer_optional_work", "mode": "ADMISSION_POLICY", "destructive": false},
			{"id": "defer_heavy_mutation", "mode": "ADMISSION_POLICY", "destructive": false},
			{"id": "bound_pressure_history", "mode": "RETENTION", "destructive": false},
		},
		"forbiddenActions": []string{
			"delete_durable_owner_data",
			"delete_operator_backups",
			"vacuum_without_global_admission",
			"run_raw_operator_commands",
		},
		"reasonCodes": append([]string(nil), snapshot.ReasonCodes...),
		"limitations": []string{
			"no_automatic_durable_data_cleanup",
			"no_cleanup_execution_endpoint",
		},
	}
}

func boundedPressureHistoryQuery(c *gin.Context) (uint64, int, bool) {
	after := uint64(0)
	limit := 100
	if !strictQueryKeys(c, "invalid resource pressure history query", "after", "limit") {
		return 0, 0, false
	}
	if value := strings.TrimSpace(c.Query("after")); value != "" {
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			securityBadRequest(c, "invalid resource pressure history query")
			return 0, 0, false
		}
		after = parsed
	}
	if value := strings.TrimSpace(c.Query("limit")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 200 {
			securityBadRequest(c, "invalid resource pressure history query")
			return 0, 0, false
		}
		limit = parsed
	}
	return after, limit, true
}

func (a *APIHandler) sqliteRuntimeStatus(c *gin.Context) {
	if _, ok := requireAuthenticatedSecurityContext(c); !ok {
		return
	}
	status, err := dbsqlite.InspectRuntime(dbsqlite.DB())
	if err != nil {
		c.JSON(http.StatusOK, Msg{Success: false, Msg: "sqlite_runtime_unavailable", Obj: gin.H{
			"state": "UNAVAILABLE", "reasonCode": "sqlite_runtime_unavailable", "runtime": status}})
		return
	}
	jsonObj(c, status, nil)
}

func (a *APIHandler) migrationStatus(c *gin.Context) {
	if _, ok := requireAuthenticatedSecurityContext(c); !ok {
		return
	}
	if dbsqlite.DB() == nil {
		c.JSON(http.StatusOK, Msg{Success: false, Msg: "migration_journal_unavailable", Obj: gin.H{"state": "UNAVAILABLE"}})
		return
	}
	var rows []model.MigrationJournal
	if err := dbsqlite.DB().WithContext(c.Request.Context()).Order("scope, owner_id, step_id").Limit(200).Find(&rows).Error; err != nil {
		c.JSON(http.StatusOK, Msg{Success: false, Msg: "migration_journal_unavailable", Obj: gin.H{"state": "UNAVAILABLE"}})
		return
	}
	jsonObj(c, gin.H{"state": migrationAggregateState(rows), "items": rows, "limit": 200, "truncated": len(rows) == 200}, nil)
}

func (a *APIHandler) dataOwnerStatus(c *gin.Context) {
	if _, ok := requireAuthenticatedSecurityContext(c); !ok {
		return
	}
	owners, reasons := installedOwnerStatus()
	jsonObj(c, gin.H{"state": ownerAggregateState(owners), "items": owners, "reasonCodes": reasons}, nil)
}

func installedOwnerStatus() ([]ownerStatus, []string) {
	result := []ownerStatus{{ID: "core", Kind: "core", Installed: true, Available: true, Enabled: "ENABLED",
		DurableData: "PRESENT", Backup: "SUPPORTED", Restore: "SUPPORTED", DropData: "NOT_APPLICABLE"}}
	installed, err := installstate.InstalledComponents()
	if err != nil {
		return result, []string{"installed_owner_manifest_unavailable"}
	}
	available := map[string]componentregistry.Component{}
	for _, component := range componentregistry.Components() {
		available[component.Manifest.ID] = component
	}
	reasons := []string{}
	for _, component := range installed {
		availableComponent, isAvailable := available[component.ID]
		status := ownerStatus{ID: component.ID, Kind: "component", Installed: true, Available: isAvailable,
			Enabled: "UNKNOWN", DurableData: "PRESERVED", Backup: "OPAQUE_PRESERVATION_REQUIRED", Restore: "FAIL_CLOSED_IF_UNAVAILABLE", DropData: "PREVIEW_REQUIRED"}
		if status.Available {
			status.Backup, status.Restore = "SUPPORTED", "SUPPORTED"
			enabled, enabledErr := enabledstate.Enabled(availableComponent.Manifest)
			if enabledErr != nil {
				status.ReasonCode = "owner_enabled_state_unavailable"
				reasons = append(reasons, status.ReasonCode+":"+component.ID)
			} else if enabled {
				status.Enabled = "ENABLED"
			} else {
				status.Enabled = "DISABLED"
			}
		}
		result = append(result, status)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	for _, owner := range result {
		if owner.Installed && !owner.Available {
			reasons = append(reasons, "installed_owner_unavailable:"+owner.ID)
		}
	}
	return result, reasons
}

func ownerBackupBlocked(reasonCodes []string) bool {
	for _, reasonCode := range reasonCodes {
		if reasonCode == "installed_owner_manifest_unavailable" ||
			strings.HasPrefix(reasonCode, "installed_owner_unavailable:") {
			return true
		}
	}
	return false
}

func migrationAggregateState(rows []model.MigrationJournal) string {
	if len(rows) == 0 {
		return "NOT_OBSERVED"
	}
	result := "APPLIED"
	for _, row := range rows {
		if row.State == "RECOVERY_REQUIRED" {
			return "RECOVERY_REQUIRED"
		}
		if row.State == "FAILED" {
			result = "FAILED"
		} else if row.State == "RUNNING" && result == "APPLIED" {
			result = "RUNNING"
		}
	}
	return result
}

func ownerAggregateState(owners []ownerStatus) string {
	for _, owner := range owners {
		if owner.Installed && !owner.Available {
			return "OWNER_UNAVAILABLE"
		}
	}
	return "READY"
}
