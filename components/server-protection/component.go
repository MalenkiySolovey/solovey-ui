//go:build !minimal

package serverprotection

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	neutralfallback "github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionapi "github.com/MalenkiySolovey/solovey-ui/components/server-protection/api"
	protectionruntime "github.com/MalenkiySolovey/solovey-ui/components/server-protection/runtimecontract"
	protectionartifacts "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/artifacts"
	protectionfirewall "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/firewall"
	protectionfronting "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/fronting"
	protectionhealth "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/health"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	protectionhostsurface "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/hostsurface"
	protectioninterception "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/interception"
	protectionlocalproxy "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/localproxy"
	protectionnativefallback "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/nativefallback"
	protectionobservation "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/observation"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	protectionresources "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/resources"
	protectionudpguard "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/udpguard"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	dbbackup "github.com/MalenkiySolovey/solovey-ui/database/backup"
	dbhooks "github.com/MalenkiySolovey/solovey-ui/database/hooks"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	coreservice "github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

//go:embed component.json
var componentJSON []byte

var componentManifest = manifest.MustFromJSON(componentJSON)
var componentID = componentManifest.ID

var hooks = struct {
	sync.Mutex
	unregisterBackup       func()
	unregisterTokenScope   func()
	operationManager       *protectionoperations.Manager
	artifactStorage        *protectionartifacts.Storage
	artifactPruner         *protectionartifacts.Pruner
	firewallWorkflow       *protectionfirewall.Workflow
	frontingWorkflow       *protectionfronting.Workflow
	frontingSemanticSource *productionFrontingSemanticSource
	nativeFallbackWorkflow *protectionnativefallback.Workflow
	localProxyController   *protectionlocalproxy.Controller
	localProxyCancel       context.CancelFunc
	helperClient           *protectionhelper.Client
	artifactCleanupID      cron.EntryID
	artifactScheduler      componenthost.Scheduler
	unregisterHostSurface  func()
	hostSurfaceCancel      context.CancelFunc
	restoreHookRegistered  bool
}{}

func init() {
	registry.Register(registry.Component{Manifest: componentManifest, Lifecycle: &component{}})
	registry.RegisterAPIRoutes(componentID, componenthost.RouteAdapter[protectionapi.Deps]{
		Build:    protectionDeps,
		Register: protectionapi.RegisterRoutes,
	}.RegisterRoutes)
}

type component struct{}

func (component) Migrate(context.Context, lifecycle.Context) error {
	return protectionrepository.Migrate(dbsqlite.DB())
}

func (component) MigrateStaged(_ context.Context, db *gorm.DB) error {
	return protectionrepository.Migrate(db)
}

func (component) RehearseRestore(ctx context.Context, db *gorm.DB) error {
	now := time.Now().UTC()
	if err := protectionrepository.ReconcileRestoredNativeFallbackRecords(ctx, db, now); err != nil {
		return err
	}
	if err := protectionrepository.ReconcileRestoredGraylistStates(ctx, db, now); err != nil {
		return err
	}
	if err := protectionrepository.ReconcileRestoredFrontingRecords(ctx, db, now); err != nil {
		return err
	}
	if err := protectionrepository.ReconcileRestoredUDPGuardRecords(ctx, db, now); err != nil {
		return err
	}
	if err := protectionrepository.ReconcileRestoredLocalProxyRecords(ctx, db, now); err != nil {
		return err
	}
	if err := protectionrepository.ReconcileRestoredFirewallAuthority(ctx, db, now); err != nil {
		return err
	}
	return protectionrepository.ReconcileLegacySelfStealProfiles(ctx, db, now)
}

func (component) InspectDropAuthority(_ context.Context, db *gorm.DB, _ time.Time) lifecycle.DropAuthorityStatus {
	if db == nil {
		return lifecycle.DropAuthorityStatus{State: "UNAVAILABLE", ReasonCodes: []string{"owner_database_unavailable"}}
	}
	if err := protectionrepository.EnsureDropSafe(db); err != nil {
		return lifecycle.DropAuthorityStatus{State: "BLOCKED", ReasonCodes: []string{"server_protection_authority_or_recovery_present"}}
	}
	return lifecycle.DropAuthorityStatus{State: "VERIFIED_SAFE", ReasonCodes: []string{}}
}

func (component) Start(ctx context.Context, lifecycleCtx lifecycle.Context) error {
	hooks.Lock()
	defer hooks.Unlock()
	if hooks.unregisterBackup == nil {
		tables := protectionrepository.BackupTableModels()
		contributions := make([]dbbackup.TableContribution, 0, len(tables))
		for _, table := range tables {
			contributions = append(contributions, dbbackup.TableContribution{Name: table.Name, Model: table.Model})
		}
		hooks.unregisterBackup = dbbackup.RegisterTables(componentID, contributions)
	}
	if !hooks.restoreHookRegistered {
		dbhooks.RegisterImportPostOpenHook(componentID+":native-fallback-state", func(ctx context.Context) error {
			now := time.Now().UTC()
			if err := protectionrepository.ReconcileRestoredNativeFallbackRecords(ctx, dbsqlite.DB(), now); err != nil {
				return err
			}
			if err := protectionrepository.ReconcileRestoredGraylistStates(ctx, dbsqlite.DB(), now); err != nil {
				return err
			}
			if err := protectionrepository.ReconcileRestoredFrontingRecords(ctx, dbsqlite.DB(), now); err != nil {
				return err
			}
			if err := protectionrepository.ReconcileRestoredUDPGuardRecords(ctx, dbsqlite.DB(), now); err != nil {
				return err
			}
			if err := protectionrepository.ReconcileRestoredLocalProxyRecords(ctx, dbsqlite.DB(), now); err != nil {
				return err
			}
			if err := protectionrepository.ReconcileRestoredFirewallAuthority(ctx, dbsqlite.DB(), now); err != nil {
				return err
			}
			return protectionrepository.ReconcileLegacySelfStealProfiles(ctx, dbsqlite.DB(), now)
		})
		hooks.restoreHookRegistered = true
	}
	if hooks.unregisterTokenScope == nil {
		hooks.unregisterTokenScope = coreservice.RegisterAPITokenScopeProvider(func() []string {
			return append([]string(nil), componentManifest.TokenScopes...)
		})
	}
	storage, err := ensureArtifactStorageLocked()
	if err != nil {
		cleanupHooksLocked()
		return err
	}
	repository := protectionrepository.New(dbsqlite.DB())
	if hooks.unregisterHostSurface == nil {
		var provider *protectionhostsurface.Provider
		if client, helperErr := ensureHelperClientLocked(); helperErr == nil {
			provider = protectionhostsurface.NewProvider(protectionhostsurface.HelperOwnerObserver{Helper: client})
		} else {
			provider = protectionhostsurface.NewProvider(protectionhostsurface.UnavailableOwnerObserver{Availability: ownerAvailabilityForHelperError(helperErr)})
		}
		hooks.unregisterHostSurface = hostfacts.Register(provider)
		reconcileCtx, cancel := context.WithCancel(ctx)
		hooks.hostSurfaceCancel = cancel
		go protectionhostsurface.RunReconciler(reconcileCtx, nil)
	}
	hooks.artifactPruner = protectionartifacts.NewPruner(storage, repository, nil)
	pruner := hooks.artifactPruner
	manager := ensureOperationManagerLocked()
	recovery := protectionoperations.Recovery(protectionartifacts.OperationRecovery{Storage: storage, Repository: repository})
	if workflow, workflowErr := ensureFirewallWorkflowLocked(); workflowErr == nil {
		recovery = protectionfirewall.BackendRecovery{Helper: workflow.Helper, Manager: manager, Storage: storage, Repository: repository, Health: workflow.RollbackHealth, Workflow: workflow}
	}
	if err := manager.SetRecovery(recovery); err != nil {
		cleanupHooksLocked()
		return err
	}
	frontingRecovery := protectionfronting.BackendRecovery{Manager: manager, Storage: storage, Repository: repository}
	if workflow, workflowErr := ensureFrontingWorkflowLocked(); workflowErr == nil {
		frontingRecovery.Helper, frontingRecovery.Health = workflow.Helper, workflow.RollbackHealth
	}
	// Always register a kind-specific backend. If the restricted nginx helper
	// is unavailable, recovery fails closed and creates a fronting bundle rather
	// than ever falling through to the firewall backend.
	if err := manager.SetRecoveryForKind(protectionoperations.KindFronting, frontingRecovery); err != nil {
		cleanupHooksLocked()
		return err
	}
	nativeWorkflow, err := ensureNativeFallbackWorkflowLocked(lifecycleCtx.Host.API.Runtime)
	if err != nil {
		cleanupHooksLocked()
		return err
	}
	if err := manager.SetReconcilerForKind(protectionoperations.KindNativeFallback, nativeWorkflow); err != nil {
		cleanupHooksLocked()
		return err
	}
	if workflow, workflowErr := ensureFrontingWorkflowLocked(); workflowErr == nil {
		if err := manager.SetReconcilerForKind(protectionoperations.KindFronting, protectionfronting.V2Reconciler{Workflow: workflow}); err != nil {
			cleanupHooksLocked()
			return err
		}
	}
	localProxyController := ensureLocalProxyControllerLocked()
	if err := manager.SetReconcilerForKind(protectionoperations.KindLocalProxy, protectionlocalproxy.Reconciler{Controller: localProxyController}); err != nil {
		cleanupHooksLocked()
		return err
	}
	if err := manager.Start(ctx); err != nil {
		cleanupHooksLocked()
		return err
	}
	if hooks.localProxyCancel == nil {
		renewCtx, cancel := context.WithCancel(ctx)
		hooks.localProxyCancel = cancel
		go protectionlocalproxy.RunLeaseRenewer(renewCtx, localProxyController)
	}
	if err := protectionobservation.DefaultWorker.Start(protectionrepository.New(dbsqlite.DB())); err != nil {
		_ = manager.Stop(context.Background())
		hooks.operationManager = nil
		cleanupHooksLocked()
		return err
	}
	if lifecycleCtx.Host.Scheduler != nil && hooks.artifactCleanupID == 0 {
		entryID, scheduleErr := lifecycleCtx.Host.Scheduler.AddJob("@every 1h", cron.FuncJob(func() {
			settings, _, loadErr := repository.LoadSettings(context.Background())
			if loadErr == nil {
				_, _ = pruner.Prune(context.Background(), settings.ArtifactRetentionCount, settings.ArtifactRetentionDays)
			}
		}))
		if scheduleErr != nil {
			_ = protectionobservation.DefaultWorker.Stop(context.Background())
			_ = manager.Stop(context.Background())
			hooks.operationManager = nil
			cleanupHooksLocked()
			return scheduleErr
		}
		hooks.artifactCleanupID = entryID
		hooks.artifactScheduler = lifecycleCtx.Host.Scheduler
	}
	return nil
}

func (component) Stop(ctx context.Context) error {
	workerErr := protectionobservation.DefaultWorker.Stop(ctx)
	hooks.Lock()
	defer hooks.Unlock()
	var operationErr error
	if hooks.operationManager != nil {
		operationErr = hooks.operationManager.Stop(ctx)
		hooks.operationManager = nil
	}
	if hooks.artifactScheduler != nil && hooks.artifactCleanupID != 0 {
		hooks.artifactScheduler.RemoveJob(hooks.artifactCleanupID)
	}
	hooks.artifactCleanupID = 0
	hooks.artifactScheduler = nil
	hooks.artifactPruner = nil
	hooks.artifactStorage = nil
	hooks.firewallWorkflow = nil
	hooks.frontingWorkflow = nil
	hooks.frontingSemanticSource = nil
	hooks.nativeFallbackWorkflow = nil
	hooks.localProxyController = nil
	if hooks.localProxyCancel != nil {
		hooks.localProxyCancel()
		hooks.localProxyCancel = nil
	}
	hooks.helperClient = nil
	cleanupHooksLocked()
	return errors.Join(workerErr, operationErr)
}

func (component) DropData(ctx context.Context, _ lifecycle.Context) error {
	if err := protectionobservation.DefaultWorker.Stop(ctx); err != nil {
		return err
	}
	if err := protectionrepository.EnsureDropSafe(dbsqlite.DB()); err != nil {
		return err
	}
	storage, err := protectionartifacts.New(artifactRootPath())
	if err != nil {
		return err
	}
	if err := storage.DropAll(); err != nil {
		return err
	}
	return protectionrepository.DropSchema(dbsqlite.DB())
}

func protectionDeps(host componenthost.APIDeps) protectionapi.Deps {
	hooks.Lock()
	defer hooks.Unlock()
	manager := ensureOperationManagerLocked()
	workflow, _ := ensureFirewallWorkflowLocked()
	frontingWorkflow, _ := ensureFrontingWorkflowLocked()
	if hooks.frontingSemanticSource == nil {
		hooks.frontingSemanticSource = newProductionFrontingSemanticSource(frontingWorkflow)
	}
	nativeFallbackWorkflow, _ := ensureNativeFallbackWorkflowLocked(host.Runtime)
	localProxyController := ensureLocalProxyControllerLocked()
	frontingAdapter := protectionfronting.NewNginxAdapter()
	if frontingWorkflow != nil {
		frontingAdapter = protectionfronting.NewManagedNginxAdapter(frontingWorkflow)
	}
	repository := protectionrepository.New(dbsqlite.DB())
	baseline := protectionfirewall.NewBaselineService(repository)
	return protectionapi.Deps{
		Repository:        repository,
		RequireScope:      host.Auth.RequireScope,
		Actor:             host.Request.Actor,
		Audit:             host.Audit.Audit,
		JSONObj:           host.HTTP.JSONObj,
		JSONMsg:           host.HTTP.JSONMsg,
		ObservationStatus: protectionobservation.DefaultWorker.Status,
		Operations:        manager,
		Firewall:          workflow,
		Baseline:          baseline,
		UDPGuard: &protectionudpguard.Controller{
			Repository: repository, Operations: manager, Firewall: workflow, Baseline: baseline,
		},
		Fronting:       frontingAdapter,
		FrontingV2:     &protectionfronting.SemanticServiceV2{Workflow: frontingWorkflow, Repository: repository, Source: hooks.frontingSemanticSource},
		NativeFallback: nativeFallbackWorkflow,
		LocalProxy:     localProxyController,
		Interception:   protectioninterception.New(),
	}
}

func ensureOperationManagerLocked() *protectionoperations.Manager {
	if hooks.operationManager != nil {
		return hooks.operationManager
	}
	repository := protectionrepository.New(dbsqlite.DB())
	options := protectionoperations.Options{
		Audit: func(_ context.Context, event protectionoperations.AuditEvent) error {
			return (&coreservice.AuditService{}).Record(coreservice.AuditEvent{
				Actor: event.Actor, Event: event.Event, Resource: "server-protection",
				Severity: coreservice.AuditSeverityWarn, Details: event.Details,
			})
		},
	}
	if hooks.artifactStorage != nil {
		options.Recovery = protectionartifacts.OperationRecovery{Storage: hooks.artifactStorage, Repository: repository}
	}
	hooks.operationManager = protectionoperations.NewManager(repository, options)
	return hooks.operationManager
}

func ensureArtifactStorageLocked() (*protectionartifacts.Storage, error) {
	if hooks.artifactStorage != nil {
		return hooks.artifactStorage, nil
	}
	storage, err := protectionartifacts.New(artifactRootPath())
	if err != nil {
		return nil, err
	}
	hooks.artifactStorage = storage
	return storage, nil
}

func ensureFirewallWorkflowLocked() (*protectionfirewall.Workflow, error) {
	if hooks.firewallWorkflow != nil {
		return hooks.firewallWorkflow, nil
	}
	storage, err := ensureArtifactStorageLocked()
	if err != nil {
		return nil, err
	}
	manager := ensureOperationManagerLocked()
	client, err := ensureHelperClientLocked()
	if err != nil {
		return nil, errors.Join(protectionfirewall.ErrMissingCapability, err)
	}
	workflow := &protectionfirewall.Workflow{
		Manager: manager, Helper: client,
		Artifacts: protectionartifacts.Service{Storage: storage, Store: protectionrepository.New(dbsqlite.DB())}, Marker: storage, State: storage,
		Recovery: protectionartifacts.OperationRecovery{Storage: storage, Repository: protectionrepository.New(dbsqlite.DB())},
		Health: func(ctx context.Context, resources []hostresources.ProtectableResource) []componenthealth.Result {
			return protectionhealth.Evaluate(ctx, resources, nil)
		},
		RollbackHealth: func(ctx context.Context, _ []hostresources.ProtectableResource) []componenthealth.Result {
			return protectionhealth.Evaluate(ctx, protectionresources.Snapshot(ctx, true).Resources, nil)
		},
		Contributions: protectionrepository.New(dbsqlite.DB()),
	}
	hooks.firewallWorkflow = workflow
	return workflow, nil
}

func ensureFrontingWorkflowLocked() (*protectionfronting.Workflow, error) {
	if hooks.frontingWorkflow != nil {
		return hooks.frontingWorkflow, nil
	}
	storage, err := ensureArtifactStorageLocked()
	if err != nil {
		return nil, err
	}
	manager := ensureOperationManagerLocked()
	client, err := ensureHelperClientLocked()
	if err != nil {
		return nil, errors.Join(protectionfronting.ErrMissingCapability, err)
	}
	repository := protectionrepository.New(dbsqlite.DB())
	workflow := &protectionfronting.Workflow{
		Manager: manager, Helper: client,
		Artifacts: protectionartifacts.Service{Storage: storage, Store: repository}, Marker: storage, State: storage,
		Recovery: protectionartifacts.OperationRecovery{Storage: storage, Repository: repository},
		Health: func(ctx context.Context, resources []hostresources.ProtectableResource) []componenthealth.Result {
			return protectionhealth.Evaluate(ctx, resources, nil)
		},
		RollbackHealth: func(ctx context.Context, _ []hostresources.ProtectableResource) []componenthealth.Result {
			return protectionhealth.Evaluate(ctx, protectionresources.Snapshot(ctx, true).Resources, nil)
		},
	}
	source := wireProductionFrontingV2(workflow, storage)
	hooks.frontingWorkflow = workflow
	hooks.frontingSemanticSource = source
	return workflow, nil
}

func wireProductionFrontingV2(workflow *protectionfronting.Workflow, storage *protectionartifacts.Storage) *productionFrontingSemanticSource {
	source := newProductionFrontingSemanticSource(workflow)
	if workflow == nil {
		return source
	}
	workflow.V2Plans = source
	workflow.V2Leases = hostresources.DefaultFrontingBackendsV1
	workflow.V2Fallbacks = neutralfallback.Default
	workflow.V2Artifacts = storage
	workflow.V2Health = protectionfronting.DefaultExactHealthRegistryV2.FixedL4Check()
	workflow.V2SNIHealth = protectionfronting.DefaultExactHealthRegistryV2.SNIPrereadCheck()
	return source
}

func ensureNativeFallbackWorkflowLocked(runtime *coreservice.Runtime) (*protectionnativefallback.Workflow, error) {
	if hooks.nativeFallbackWorkflow != nil {
		return hooks.nativeFallbackWorkflow, nil
	}
	if runtime == nil {
		return nil, errors.New("native fallback core runtime unavailable")
	}
	storage, err := ensureArtifactStorageLocked()
	if err != nil {
		return nil, err
	}
	repository := protectionrepository.New(dbsqlite.DB())
	coreControl := coreservice.NewConfigServiceWithRuntime(runtime).CoreInboundControl()
	if coreControl == nil {
		return nil, errors.New("native fallback core inbound control unavailable")
	}
	planner := protectionnativefallback.Planner{
		Core: coreControl, Targets: protectionnativefallback.RegistryTargetReader{Registry: neutralfallback.Default},
		Management: protectionnativefallback.InventoryManagementReader{},
	}
	workflow := &protectionnativefallback.Workflow{
		Operations: ensureOperationManagerLocked(), Journal: repository, Planner: planner, Core: coreControl,
		Providers: neutralfallback.Default, Artifacts: protectionartifacts.Service{Storage: storage, Store: repository}, Marker: storage,
	}
	hooks.nativeFallbackWorkflow = workflow
	return workflow, nil
}

func ensureLocalProxyControllerLocked() *protectionlocalproxy.Controller {
	if hooks.localProxyController != nil {
		return hooks.localProxyController
	}
	hooks.localProxyController = &protectionlocalproxy.Controller{
		Repository: protectionrepository.New(dbsqlite.DB()), Operations: ensureOperationManagerLocked(),
		Providers: hostresources.DefaultLocalProxiesV1, Probes: componenthealth.DefaultLocalProxyProbesV1,
	}
	return hooks.localProxyController
}

func ensureHelperClientLocked() (*protectionhelper.Client, error) {
	if hooks.helperClient != nil {
		return hooks.helperClient, nil
	}
	storage, err := ensureArtifactStorageLocked()
	if err != nil {
		return nil, err
	}
	root, err := protectionhelper.NewManagedRoot(storage.Root())
	if err != nil {
		return nil, err
	}
	invoker, reason := protectionhelper.DiscoverInstalledBrokerInvoker()
	if invoker == nil {
		availability := protectionhostsurface.OwnerHelperIdentityMismatch
		if reason == "broker_not_installed" {
			availability = protectionhostsurface.OwnerHelperNotInstalled
		}
		return nil, helperClientAvailabilityError{Availability: availability, Cause: fmt.Errorf("privileged broker unavailable: %s", reason)}
	}
	client, err := protectionhelper.NewClient(root, ensureOperationManagerLocked(), invoker, helperAuditRecorder{})
	if err != nil {
		return nil, helperClientAvailabilityError{Availability: protectionhostsurface.OwnerObserverNotRegistered, Cause: err}
	}
	hooks.helperClient = client
	return client, nil
}

type helperClientAvailabilityError struct {
	Availability protectionhostsurface.OwnerAvailability
	Cause        error
}

func (e helperClientAvailabilityError) Error() string { return "restricted helper client unavailable" }
func (e helperClientAvailabilityError) Unwrap() error { return e.Cause }

func ownerAvailabilityForHelperError(err error) protectionhostsurface.OwnerAvailability {
	var availabilityError helperClientAvailabilityError
	if errors.As(err, &availabilityError) && availabilityError.Availability != "" {
		return availabilityError.Availability
	}
	return protectionhostsurface.OwnerObserverNotRegistered
}

type helperAuditRecorder struct{}

func (helperAuditRecorder) RecordHelperAudit(_ context.Context, event protectionhelper.AuditEvent) error {
	return (&coreservice.AuditService{}).Record(coreservice.AuditEvent{Actor: "system", Event: "server_protection.helper", Resource: "server-protection", Severity: coreservice.AuditSeverityInfo, Details: map[string]any{
		"phase": event.Phase, "operation": event.Operation, "operation_id": event.OperationID, "lock_revision": event.LockRevision,
		"ok": event.OK, "code": event.Code, "exit_class": event.ExitClass,
	}})
}

func artifactRootPath() string {
	return protectionruntime.RootForDatabaseFolder(configstorage.GetDBFolderPath())
}

func cleanupHooksLocked() {
	if hooks.hostSurfaceCancel != nil {
		hooks.hostSurfaceCancel()
		hooks.hostSurfaceCancel = nil
	}
	if hooks.unregisterHostSurface != nil {
		hooks.unregisterHostSurface()
		hooks.unregisterHostSurface = nil
	}
	if hooks.localProxyCancel != nil {
		hooks.localProxyCancel()
		hooks.localProxyCancel = nil
	}
	if hooks.unregisterBackup != nil {
		hooks.unregisterBackup()
		hooks.unregisterBackup = nil
	}
	if hooks.restoreHookRegistered {
		dbhooks.RegisterImportPostOpenHook(componentID+":native-fallback-state", nil)
		hooks.restoreHookRegistered = false
	}
	if hooks.unregisterTokenScope != nil {
		hooks.unregisterTokenScope()
		hooks.unregisterTokenScope = nil
	}
}
