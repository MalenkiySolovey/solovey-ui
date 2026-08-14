package app

import (
	"context"
	"errors"
	"log"
	"os"
	"sync"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	managementregistry "github.com/MalenkiySolovey/solovey-ui/componenthost/management"
	componentsupervisor "github.com/MalenkiySolovey/solovey-ui/componenthost/supervisor"
	configidentity "github.com/MalenkiySolovey/solovey-ui/config/identity"
	configlogging "github.com/MalenkiySolovey/solovey-ui/config/logging"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	versionpolicy "github.com/MalenkiySolovey/solovey-ui/config/versionpolicy"
	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	"github.com/MalenkiySolovey/solovey-ui/cronjob/scheduler"
	"github.com/MalenkiySolovey/solovey-ui/database/migration"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/database/restorestate"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	deploymentdomain "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
	subserver "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/server"
	ipmonitor "github.com/MalenkiySolovey/solovey-ui/ipmonitor"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"
	datalifecycleservice "github.com/MalenkiySolovey/solovey-ui/service/datalifecycle"
	deploymentservice "github.com/MalenkiySolovey/solovey-ui/service/deployment"
	"github.com/MalenkiySolovey/solovey-ui/service/resourceinventory"
	pressureService "github.com/MalenkiySolovey/solovey-ui/service/resourcepressure"
	sshmanagementservice "github.com/MalenkiySolovey/solovey-ui/service/sshmanagement"
	serviceupdate "github.com/MalenkiySolovey/solovey-ui/service/update"
	"github.com/MalenkiySolovey/solovey-ui/sub"
	"github.com/MalenkiySolovey/solovey-ui/web"
)

type APP struct {
	lifecycle   sync.Mutex
	initialized bool
	started     bool
	service.SettingService
	configService *service.ConfigService
	webServer     *web.Server
	subServer     *sub.Server
	cronScheduler *scheduler.Scheduler
	core          *coreruntime.Core
	runtime       *service.Runtime
	components    *componentsupervisor.Supervisor
	stopResources func()
	stopCoreHooks func()
}

func NewApp() *APP {
	return &APP{}
}

func (a *APP) Init() (err error) {
	a.lifecycle.Lock()
	defer a.lifecycle.Unlock()
	if a.initialized {
		return nil
	}
	defer func() {
		if err != nil {
			a.cleanupInitialization()
		}
	}()
	if err := versionpolicy.ValidateReleaseVersion(configidentity.GetVersion()); err != nil {
		return errors.New("invalid embedded release version: " + err.Error())
	}
	log.Printf("%v %v", configidentity.GetName(), configidentity.GetVersion())

	a.initLog()

	databasePath := configstorage.GetDBPath()
	if err := restorestate.Recover(databasePath); err != nil {
		return err
	}
	_, databaseStatErr := os.Stat(databasePath)
	freshDatabase := errors.Is(databaseStatErr, os.ErrNotExist)
	if databaseStatErr != nil && !freshDatabase {
		return databaseStatErr
	}

	err = dbsqlite.Init(configstorage.GetDBPath())
	if err != nil {
		return err
	}
	if err := migration.EnsureCurrentSchemaJournal(dbsqlite.DB(), freshDatabase); err != nil {
		return err
	}
	dataReconcileCtx, cancelDataReconcile := context.WithTimeout(context.Background(), 10*time.Second)
	if reconcileErr := datalifecycleservice.Shared().ReconcileStartup(dataReconcileCtx); reconcileErr != nil {
		logger.Warning("data lifecycle startup reconciliation requires attention")
	}
	cancelDataReconcile()

	// Init Setting
	if _, err := a.SettingService.GetAllSetting(); err != nil {
		return errors.New("initialize settings: " + err.Error())
	}
	// Re-seal any secret settings still encrypted under a DB-derived key once an
	// out-of-database SUI_SECRETBOX_KEY is configured. No-op without the env key,
	// idempotent, and transactional across the secret set. A failure leaves the
	// existing ciphertext intact and is reported without blocking startup.
	if n, err := a.SettingService.ResealSecretSettings(); err != nil {
		logger.Warning("failed to re-seal secret settings: ", err)
	} else if n > 0 {
		logger.Info("re-sealed ", n, " secret setting(s) under SUI_SECRETBOX_KEY")
	}
	if err := ipmonitor.WarmUp(); err != nil {
		return err
	}

	a.core = coreruntime.NewCore(ipmonitor.Observer{})
	a.runtime = service.NewRuntime(a.core)
	service.SetDefaultRuntime(a.runtime)

	a.cronScheduler = scheduler.New()
	a.components = componentsupervisor.New(componenthost.Deps{
		API: componenthost.APIDeps{
			Runtime: a.runtime,
		},
		Scheduler: a.cronScheduler,
	})
	service.RegisterComponentSettingsReconciler(a.components.Reconcile)
	service.RegisterComponentMigrator(a.components.Migrate)
	service.RegisterComponentDataDropper(a.components.DropData)
	a.webServer, err = web.NewServer(web.WithRuntime(a.runtime))
	if err != nil {
		return err
	}
	a.subServer = sub.NewServer()

	a.configService = service.NewConfigServiceWithRuntime(a.runtime)
	if err := a.registerResources(); err != nil {
		return err
	}
	if err := a.registerCoreHooks(); err != nil {
		a.stopResources()
		a.stopResources = nil
		return err
	}

	if err := a.components.Migrate(context.Background()); err != nil {
		logger.Warning("failed to migrate components: ", err)
	}

	a.initialized = true
	return nil
}

func (a *APP) Start() error {
	a.lifecycle.Lock()
	defer a.lifecycle.Unlock()
	if a.started {
		return nil
	}
	if !a.initialized {
		return errors.New("application is not initialized")
	}
	if err := a.start(); err != nil {
		return err
	}
	a.started = true
	return nil
}

func (a *APP) start() (err error) {
	if err := a.registerResources(); err != nil {
		return err
	}
	if err := a.registerCoreHooks(); err != nil {
		return err
	}
	loc, err := a.SettingService.GetTimeLocation()
	if err != nil {
		return err
	}

	trafficAge, err := a.SettingService.GetTrafficAge()
	if err != nil {
		return err
	}

	err = a.cronScheduler.Start(loc, trafficAge)
	if err != nil {
		return err
	}
	schedulerStarted := true
	webStarted := false
	subStarted := false
	defer func() {
		if err == nil {
			return
		}
		if subStarted {
			_ = a.subServer.Stop()
		}
		if webStarted {
			_ = a.webServer.Stop()
		}
		if schedulerStarted {
			stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			_ = a.cronScheduler.Stop(stopCtx)
			cancel()
		}
	}()

	err = a.webServer.Start()
	if err != nil {
		return err
	}
	webStarted = true

	err = a.subServer.Start()
	if err != nil {
		return err
	}
	subStarted = true

	// Optional in-process components self-register behind the component
	// supervisor. This keeps app free of direct optional-domain imports while
	// preserving the existing full-profile behavior.
	if err := a.components.Start(context.Background()); err != nil {
		logger.Warning("start components err:", err)
	}

	// A core start failure is intentionally non-fatal: the web/sub panel must
	// stay up so the admin can fix a bad sing-box config through the UI. The
	// failure is surfaced loudly here and reflected in the panel's core status.
	if err = a.configService.StartCore(); err != nil {
		logger.Error("sing-box core failed to start; panel stays up so you can fix the config: ", err)
	}
	reconcileCtx, cancelReconcile := context.WithTimeout(context.Background(), 30*time.Second)
	if reconcileErr := serviceupdate.SharedLifecycle().ReconcileStartup(reconcileCtx); reconcileErr != nil {
		logger.Warning("signed update startup reconciliation requires attention")
	}
	cancelReconcile()
	return nil
}

func (a *APP) Stop() {
	a.lifecycle.Lock()
	defer a.lifecycle.Unlock()
	if !a.initialized && a.runtime == nil {
		return
	}
	if a.initialized || a.runtime != nil {
		a.stop()
	}
	if a.initialized {
		if err := dbsqlite.Close(); err != nil {
			logger.Warning("close database err:", err)
		}
		a.initialized = false
	}
	service.SetDefaultRuntime(nil)
	a.configService = nil
	a.webServer = nil
	a.subServer = nil
	a.cronScheduler = nil
	a.core = nil
	a.runtime = nil
	a.components = nil
}

func (a *APP) stop() {
	service.StopRestartManager()
	if a.cronScheduler != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := a.cronScheduler.Stop(stopCtx); err != nil {
			logger.Warning("stop cron scheduler err:", err)
		}
		cancel()
	}
	if a.subServer != nil {
		if err := a.subServer.Stop(); err != nil {
			logger.Warning("stop Sub Server err:", err)
		}
	}
	if a.webServer != nil {
		if err := a.webServer.Stop(); err != nil {
			logger.Warning("stop Web Server err:", err)
		}
	}
	a.stopHookRegistrations()
	componentsCtx, componentsCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if a.components != nil {
		if err := a.components.Stop(componentsCtx); err != nil {
			logger.Warning("stop components err:", err)
		}
	}
	componentsCancel()
	if a.configService != nil {
		if err := a.configService.StopCore(); err != nil {
			logger.Warning("stop Core err:", err)
		}
	}
	tokenCtx, tokenCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := service.StopTokenUseDebouncer(tokenCtx); err != nil {
		logger.Warning("stop token use debouncer err:", err)
	}
	tokenCancel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := service.StopAuditWriter(ctx); err != nil {
		logger.Warning("stop audit writer err:", err)
	}
	cancel()
	a.stopResourceRegistrations()
	a.started = false
}

func (a *APP) cleanupRegistrations() {
	a.stopHookRegistrations()
	a.stopResourceRegistrations()
}

func (a *APP) stopHookRegistrations() {
	if a.stopCoreHooks != nil {
		a.stopCoreHooks()
		a.stopCoreHooks = nil
	}
}

func (a *APP) stopResourceRegistrations() {
	if a.stopResources != nil {
		a.stopResources()
		a.stopResources = nil
	}
}

func (a *APP) cleanupInitialization() {
	if a.runtime != nil {
		a.stop()
	} else {
		a.cleanupRegistrations()
	}
	if err := dbsqlite.Close(); err != nil {
		logger.Warning("close database after initialization failure:", err)
	}
	service.SetDefaultRuntime(nil)
	a.configService = nil
	a.webServer = nil
	a.subServer = nil
	a.cronScheduler = nil
	a.core = nil
	a.runtime = nil
	a.components = nil
	a.initialized = false
	a.started = false
}

func (a *APP) initLog() {
	switch configlogging.GetLogLevel() {
	case configlogging.Debug:
		logger.Init(logger.LevelDebug)
	case configlogging.Info:
		logger.Init(logger.LevelInfo)
	case configlogging.Warn:
		logger.Init(logger.LevelWarning)
	case configlogging.Error:
		logger.Init(logger.LevelError)
	default:
		logger.Init(logger.LevelInfo)
	}
}

func (a *APP) registerResources() error {
	if a.stopResources != nil {
		return nil
	}
	if a.configService == nil {
		return errors.New("core resource registration requires initialized config service")
	}
	stop, err := resourceinventory.RegisterCoreContributors(&a.SettingService, dbsqlite.DB(), a.configService.CoreInboundControl())
	if err != nil {
		return err
	}
	a.stopResources = stop
	return nil
}

func (a *APP) registerCoreHooks() error {
	if a.stopCoreHooks != nil {
		return nil
	}
	stopIPMonitor, err := ipmonitor.RegisterSecurityEventAuditHook(func(_ string, _ string, payload map[string]any) {
		_ = (&service.AuditService{}).Record(service.AuditEvent{
			Actor:    "system",
			Event:    "ip_limit_enforced",
			Resource: "ipmonitor",
			Severity: service.AuditSeverityWarn,
			Details:  payload,
		})
	})
	if err != nil {
		return err
	}
	stopSubscriptions, err := subserver.RegisterHooks(subserver.Hooks{
		ListenFallbackAudit: func(component, requestedAddr, fallbackAddr string, bindErr error) {
			_ = (&service.AuditService{}).RecordListenFallback(component, requestedAddr, fallbackAddr, bindErr)
		},
		EnumerationAudit: func(ip string, invalidLookups, windowMinutes int) {
			_ = (&service.AuditService{}).Record(service.AuditEvent{
				Actor:    "anonymous",
				Event:    "sub_enumeration",
				Resource: "sub",
				Severity: service.AuditSeverityWarn,
				IP:       ip,
				Details:  map[string]any{"invalidLookups": invalidLookups, "windowMinutes": windowMinutes},
			})
		},
		RateLimitProvider: func() (int, error) {
			return (&service.SettingService{}).GetSubRateLimitPerIP()
		},
	})
	if err != nil {
		stopIPMonitor()
		return err
	}
	stopClientIPCache := service.RegisterConfigSaveObserver("core.ipmonitor.client_identity", func(ctx service.ConfigSaveObserverContext) (service.ConfigSaveAfterCommit, error) {
		if ctx.Object != "clients" {
			return nil, nil
		}
		return ipmonitor.ResetCaches, nil
	})
	sshManager := sshmanagementservice.Shared()
	sshManager.Audit = func(_ context.Context, event sshmanagementservice.AuditEventV1) {
		_ = (&service.AuditService{}).Record(service.AuditEvent{Actor: "system", Event: "ssh_management_" + event.Event,
			Resource: "ssh-management", Severity: service.AuditSeverityWarn, Details: map[string]any{
				"operationId": event.OperationID, "state": event.State, "reasonCode": event.ReasonCode, "revision": event.Revision,
			}})
	}
	stopSSHEvidence, err := managementregistry.RegisterEvidenceProvider(sshManager)
	if err != nil {
		stopClientIPCache()
		stopSubscriptions()
		stopIPMonitor()
		return err
	}
	stopSSHPanelEvents := service.RegisterPanelEventNotifier("core:ssh-management-recovery", func(event string, fields map[string]string) {
		if err := sshManager.HandlePanelEvent(event, fields); err != nil {
			logger.Warning("SSH management panel recovery observation failed")
		}
	})
	watchdogCtx, stopSSHWatchdog := context.WithCancel(context.Background())
	pressureService.Shared().Start(watchdogCtx)
	updateManager := serviceupdate.SharedLifecycle()
	updateManager.SetAdmissionCheck(func(class string) bool { return pressureService.Shared().Admission(class).Allowed })
	updateManager.SetHealthCheck(func(ctx context.Context, operation model.UpdateOperation) serviceupdate.HealthResult {
		reasons := []string{}
		panel := componenthealth.Check(ctx, "core:panel:web")
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
			if err := db.WithContext(ctx).Model(&model.MigrationJournal{}).
				Where("state IN ?", []string{"FAILED", "RECOVERY_REQUIRED"}).Count(&failedMigrations).Error; err != nil || failedMigrations != 0 {
				reasons = append(reasons, "owner_migration_health_failed")
			}
		}
		if configidentity.GetVersion() != operation.Version || databaseVersion != operation.Version || coreSchema != "1.11" {
			reasons = append(reasons, "release_schema_identity_mismatch")
		}
		evidence := struct {
			OperationID, Version, DatabaseVersion, CoreSchema, SQLiteRevision, PanelStatus string
			CoreReady, Ready                                                               bool
		}{operation.OperationID, operation.Version, databaseVersion, coreSchema, runtimeStatus.Revision, string(panel.Status), coreReady, len(reasons) == 0}
		return serviceupdate.HealthResult{Ready: len(reasons) == 0, ReasonCodes: reasons, Revision: deploymentdomain.Revision(evidence)}
	})
	if err := sshManager.ReconcileStartup(watchdogCtx); err != nil {
		logger.Warning("SSH management startup reconciliation requires attention")
	}
	sshWatchdogDone := make(chan struct{})
	go func() {
		defer close(sshWatchdogDone)
		sshManager.StartWatchdog(watchdogCtx)
	}()
	deploymentManager := deploymentservice.Shared()
	deploymentManager.Health = func(ctx context.Context, _ time.Time) deploymentservice.RuntimeHealth {
		panel := componenthealth.Check(ctx, "core:panel:web")
		coreReady := a.runtime != nil && a.runtime.Core() != nil && a.runtime.Core().IsRunning()
		reasons := []string{}
		if panel.Status != componenthealth.StatusOK {
			reasons = append(reasons, "panel_api_health_failed")
		}
		if !coreReady {
			reasons = append(reasons, "core_runtime_health_failed")
		}
		result := deploymentservice.RuntimeHealth{Ready: len(reasons) == 0, Reasons: reasons,
			EvidenceRevision: deploymentdomain.Revision(struct {
				Panel componenthealth.Result
				Core  bool
			}{panel, coreReady})}
		result.Revision = deploymentdomain.Revision(result)
		return result
	}
	deploymentManager.Audit = func(_ context.Context, event deploymentservice.AuditEvent) {
		_ = (&service.AuditService{}).Record(service.AuditEvent{Actor: "system", Event: "deployment_" + event.Event,
			Resource: "deployment", Severity: service.AuditSeverityWarn, Details: map[string]any{
				"operationId": event.OperationID, "state": event.State, "reasonCode": event.Reason, "revision": event.Revision,
			}})
	}
	if err := deploymentManager.ReconcileStartup(watchdogCtx); err != nil {
		logger.Warning("deployment startup reconciliation requires attention")
	}
	var once sync.Once
	a.stopCoreHooks = func() {
		once.Do(func() {
			stopSSHWatchdog()
			stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := pressureService.Shared().Stop(stopCtx); err != nil {
				logger.Warning("resource pressure watcher did not stop cleanly:", err)
			}
			select {
			case <-sshWatchdogDone:
			case <-stopCtx.Done():
				logger.Warning("SSH management watchdog did not stop cleanly:", stopCtx.Err())
			}
			cancel()
			sshManager.Audit = nil
			updateManager.SetAdmissionCheck(nil)
			updateManager.SetHealthCheck(nil)
			deploymentManager.Health = nil
			deploymentManager.Audit = nil
			stopSSHPanelEvents()
			stopSSHEvidence()
			stopClientIPCache()
			stopSubscriptions()
			stopIPMonitor()
		})
	}
	return nil
}

func (a *APP) RestartApp() {
	a.lifecycle.Lock()
	defer a.lifecycle.Unlock()
	if !a.initialized || !a.started {
		return
	}
	a.stop()
	if err := a.start(); err != nil {
		logger.Warning("failed to restart app: ", err)
		return
	}
	a.started = true
}
