//go:build !minimal

package serverprotection

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	protectionruntime "github.com/MalenkiySolovey/solovey-ui/components/server-protection/runtimecontract"
	protectionartifacts "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/artifacts"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	"github.com/MalenkiySolovey/solovey-ui/database/backup"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	coreservice "github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/robfig/cron/v3"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type componentMemMultipartFile struct{ *bytes.Reader }

func (componentMemMultipartFile) Close() error { return nil }

type trackingScheduler struct {
	added   []string
	removed []cron.EntryID
}

func (s *trackingScheduler) AddJob(spec string, _ cron.Job) (cron.EntryID, error) {
	s.added = append(s.added, spec)
	return cron.EntryID(len(s.added)), nil
}
func (*trackingScheduler) Schedule(cron.Schedule, cron.Job) cron.EntryID { return 0 }
func (s *trackingScheduler) RemoveJob(id cron.EntryID)                   { s.removed = append(s.removed, id) }
func (s *trackingScheduler) RemoveJobAndWait(_ context.Context, id cron.EntryID) error {
	s.removed = append(s.removed, id)
	return nil
}

func serverProtectionLifecycleContext(scheduler componenthost.Scheduler) lifecycle.Context {
	return lifecycle.Context{Host: componenthost.Deps{
		API:       componenthost.APIDeps{Runtime: coreservice.NewRuntime(nil)},
		Scheduler: scheduler,
	}}
}

func TestComponentManifestIsRegisteredPreviewOnly(t *testing.T) {
	registered, ok := registry.ComponentByID("server-protection")
	if !ok {
		t.Fatalf("server-protection component was not registered")
	}
	if registered.Manifest.DefaultEnabled {
		t.Fatalf("server-protection must remain disabled by default")
	}
	if len(registered.Manifest.TokenScopes) != 3 {
		t.Fatalf("token scopes = %#v", registered.Manifest.TokenScopes)
	}
	for _, scope := range registered.Manifest.TokenScopes {
		if scope == "server-protection" {
			t.Fatal("broad server-protection token scope is forbidden")
		}
	}
}

func TestComponentManifestDeclaresNativeFallbackStateOwnership(t *testing.T) {
	var declaration struct {
		Database struct {
			Tables []string `json:"tables"`
		} `json:"database"`
	}
	if err := json.Unmarshal(componentJSON, &declaration); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, table := range declaration.Database.Tables {
		if table == "server_protection_native_fallback_states" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("native fallback state manifest ownership count=%d", count)
	}
}

func TestComponentManifestDeclaresEveryRepositoryTable(t *testing.T) {
	var declaration struct {
		Database struct {
			Tables []string `json:"tables"`
		} `json:"database"`
	}
	if err := json.Unmarshal(componentJSON, &declaration); err != nil {
		t.Fatal(err)
	}
	got := append([]string(nil), declaration.Database.Tables...)
	want := make([]string, 0, len(protectionrepository.TableModels()))
	for _, table := range protectionrepository.TableModels() {
		want = append(want, table.Name)
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("manifest table ownership mismatch\ngot:  %v\nwant: %v", got, want)
	}
}

func TestDisabledComponentDoesNotStartRecoveryOrCreateArtifactRoot(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", filepath.Join(dir, "db"))
	hooks.Lock()
	hooks.operationManager = nil
	hooks.artifactStorage = nil
	_ = ensureOperationManagerLocked(nil)
	hooks.operationManager = nil
	hooks.Unlock()
	if _, err := os.Stat(filepath.Join(dir, ".runtime", "server-protection")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("disabled component created recovery footprint: %v", err)
	}
}

func TestComponentRuntimeRootProducerUsesVersionedContractShape(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", filepath.Join(dir, "db"))
	if got, want := artifactRootPath(), protectionruntime.RootForDatabaseFolder(filepath.Join(dir, "db")); got != want {
		t.Fatalf("component runtime root = %q, contract = %q", got, want)
	}
	t.Setenv("SUI_DB_FOLDER", "/usr/local/solovey-ui/db")
	if got := filepath.ToSlash(artifactRootPath()); got != protectionruntime.Installed().RuntimeRoot {
		t.Fatalf("installed component runtime root = %q, contract = %q", got, protectionruntime.Installed().RuntimeRoot)
	}
	t.Setenv("SUI_DB_FOLDER", "/var/lib/solovey-ui/db")
	if got := filepath.ToSlash(artifactRootPath()); got != protectionruntime.Installed().RuntimeRoot {
		t.Fatalf("native hardened component runtime root = %q, contract = %q", got, protectionruntime.Installed().RuntimeRoot)
	}
}

func TestLifecycleKeepsInstalledOwnerBackupWhileRuntimeScopesFollowStartStop(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", dir)
	markServerProtectionInstalled(t, dir)
	_ = dbsqlite.Close()
	if err := dbsqlite.Init(filepath.Join(dir, "s-ui.db")); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbsqlite.Close() })
	if err := dbsqlite.DB().Create(&model.User{Username: "server-protection-test"}).Error; err != nil {
		t.Fatal(err)
	}

	c := component{}
	scheduler := &trackingScheduler{}
	lifecycleCtx := serverProtectionLifecycleContext(scheduler)
	if err := c.Migrate(context.Background(), lifecycle.Context{}); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := c.Start(context.Background(), lifecycleCtx); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if err := c.Start(context.Background(), lifecycleCtx); err != nil {
		t.Fatalf("idempotent Start: %v", err)
	}
	if _, err := (&coreservice.UserService{}).AddToken("server-protection-test", 0, "component scope", "server-protection:read"); err != nil {
		t.Fatalf("component scope missing after Start: %v", err)
	}
	assertLifecycleBackupTable(t, true)
	if len(scheduler.added) != 1 || scheduler.added[0] != "@every 1h" {
		t.Fatalf("artifact cleanup schedule = %#v", scheduler.added)
	}

	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("idempotent Stop: %v", err)
	}
	if _, err := (&coreservice.UserService{}).AddToken("server-protection-test", 0, "removed component scope", "server-protection:read"); err == nil {
		t.Fatal("component scope remained available after Stop")
	}
	assertLifecycleBackupTable(t, true)
	if len(scheduler.removed) != 1 || scheduler.removed[0] != 1 {
		t.Fatalf("artifact cleanup was not stopped: %#v", scheduler.removed)
	}
}

func TestDisabledComponentStopsRecoveryRunnerAcrossEnable(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", dir)
	_ = dbsqlite.Close()
	if err := dbsqlite.Init(filepath.Join(dir, "s-ui.db")); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbsqlite.Close() })
	c := component{}
	if err := c.Migrate(context.Background(), lifecycle.Context{}); err != nil {
		t.Fatal(err)
	}
	if err := c.Start(context.Background(), serverProtectionLifecycleContext(nil)); err != nil {
		t.Fatal(err)
	}
	hooks.Lock()
	before := hooks.operationManager
	hooks.Unlock()
	port := 443
	acquired, err := before.Acquire(context.Background(), protectionoperations.AcquireRequest{
		Kind: protectionoperations.KindFirewall, ResourceID: "panel:https", Protocol: "tcp",
		Listen: "0.0.0.0", Port: &port, IdempotencyKey: "lifecycle", Actor: "admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := before.Acquire(context.Background(), protectionoperations.AcquireRequest{
		Kind: protectionoperations.KindFirewall, IdempotencyKey: "disabled-runner", Actor: "admin",
	}); !errors.Is(err, protectionoperations.ErrFenced) {
		t.Fatalf("disabled component left recovery manager active: %v", err)
	}
	hooks.Lock()
	if hooks.operationManager != nil {
		hooks.Unlock()
		t.Fatal("operation runner remained installed after component Stop")
	}
	hooks.Unlock()

	if err := c.Start(context.Background(), serverProtectionLifecycleContext(nil)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Stop(context.Background()) })
	hooks.Lock()
	after := hooks.operationManager
	hooks.Unlock()
	if after == nil || before.InstanceID() == after.InstanceID() {
		t.Fatalf("component enable did not create a new fenced instance: before=%v after=%v", before, after)
	}
	items, err := protectionrepository.New(dbsqlite.DB()).ListOperationLocks(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].OperationID != acquired.Operation.OperationID || items[0].State != protectionoperations.StateLockSuspect {
		t.Fatalf("recovered lifecycle journal = %#v", items)
	}
	if _, err := after.ForceUnlock(context.Background(), protectionoperations.ForceUnlockRequest{
		OperationID: items[0].OperationID, Revision: items[0].Revision, Actor: "admin", Confirmed: true,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestDropDataRemovesOnlySafeTerminalArtifactStorage(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", filepath.Join(dir, "db"))
	if err := os.MkdirAll(filepath.Join(dir, "db"), 0o700); err != nil {
		t.Fatal(err)
	}
	_ = dbsqlite.Close()
	if err := dbsqlite.Init(filepath.Join(dir, "db", "s-ui.db")); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbsqlite.Close() })
	c := component{}
	if err := c.Migrate(context.Background(), lifecycle.Context{}); err != nil {
		t.Fatal(err)
	}
	storage, err := protectionartifacts.New(artifactRootPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (protectionartifacts.Service{Storage: storage, Store: protectionrepository.New(dbsqlite.DB())}).WriteRevision(
		context.Background(), "operation-000000000000000000000000000000ff", "revision-terminal", map[string][]byte{"resource-before.json": []byte("safe")},
	); err != nil {
		t.Fatal(err)
	}
	if err := c.DropData(context.Background(), lifecycle.Context{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(artifactRootPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("component artifact root remains after DropData: %v", err)
	}
	for _, table := range protectionrepository.TableModels() {
		if dbsqlite.DB().Migrator().HasTable(table.Model) {
			t.Fatalf("component schema remains after DropData: %s", table.Name)
		}
	}
}

func TestContractV2RecordsRoundTripThroughComponentBackup(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", dir)
	markServerProtectionInstalled(t, dir)
	_ = dbsqlite.Close()
	if err := dbsqlite.Init(configstorage.GetDBPath()); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbsqlite.Close() })

	c := component{}
	if err := c.Migrate(context.Background(), lifecycle.Context{}); err != nil {
		t.Fatal(err)
	}
	if err := c.Start(context.Background(), serverProtectionLifecycleContext(nil)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Stop(context.Background()) })

	rows := []any{
		&protectionrepository.ProtectionSignalV2Model{
			SignalID: "signal-contract-v2-backup", Schema: "ProtectionSignalV2", SourceID: "test", SourceClass: "neutral",
			Category: "connection", Kind: "observed", KnownKind: true, SubjectType: "endpoint", SubjectValue: "endpoint:test",
			Scope: "endpoint", TargetResourceID: "resource:test", ObservedAt: 1, ExpiresAt: 2, ConfidenceBP: 9000,
			PolicyRevision: "policy:test", ContractJSON: []byte(`{"schema":"ProtectionSignalV2"}`),
		},
		&protectionrepository.ProtectionDecisionV2Model{
			DecisionID: "decision-contract-v2-backup", Schema: "ProtectionDecisionV2", PolicyRevision: "policy:test",
			SubjectType: "endpoint", SubjectValue: "endpoint:test", Scope: "endpoint", RequestedIntent: "OBSERVE",
			ResolvedIntent: "OBSERVE", State: "CURRENT", CreatedAt: 1, ExpiresAt: 2,
			ContractJSON: []byte(`{"schema":"ProtectionDecisionV2"}`),
		},
		&protectionrepository.FallbackTargetLeaseModel{
			LeaseID: "lease-contract-v2-backup", HolderID: "server-protection", ProviderID: "fixture-provider", TargetID: "target:test",
			PublishRevision: "publish:test", ContentDigest: "digest:test", ApprovedLocalEndpointID: "endpoint:test",
			ProviderHealthRevision: "health:test", IssuedAt: 1, RenewedAt: 1, ExpiresAt: 2, State: "ACTIVE",
			ReasonCodesJSON: []byte(`[]`),
		},
		&protectionrepository.RecoveryPathModel{
			RecoveryPathID: "recovery-contract-v2-backup", Kind: "PANEL", EndpointID: "endpoint:test", PrincipalID: "principal:test",
			VerificationMethod: "test", VerifiedAt: 1, ExpiresAt: 2, IndependenceClass: "independent",
			VerificationState: "VERIFIED", ReasonCodesJSON: []byte(`[]`),
		},
		&protectionrepository.NativeFallbackOperationModel{
			Schema: protectionrepository.NativeFallbackOperationSchemaV1, OperationID: "native-backup-operation", Revision: 4,
			ResourceID: "core:inbound:backup", InboundDatabaseID: 77, PlanID: strings.Repeat("1", 64), PlanDigest: strings.Repeat("1", 64), PlanJSON: []byte(`{}`),
			RuntimeIdentityRevision: strings.Repeat("2", 64), CapabilityResolverRevision: strings.Repeat("3", 64),
			BeforeConfigurationRevision: strings.Repeat("4", 64), ExpectedAfterRevision: strings.Repeat("5", 64), BeforeEffectiveRevision: strings.Repeat("6", 64),
			TargetReferenceJSON: []byte(`{}`), TargetRevision: strings.Repeat("7", 64), ProviderRevision: "provider-backup",
			EndpointRevision: strings.Repeat("8", 64), PublishRevision: "publish-backup", HealthRevision: strings.Repeat("9", 64), CapacityRevision: strings.Repeat("a", 64),
			WorkflowState: protectionrepository.NativeWorkflowRolledBack, HealthFactsJSON: []byte(`{}`), ReasonCodesJSON: []byte(`[]`), RecoveryBundleJSON: []byte(`{}`), CreatedAt: 1, UpdatedAt: 2,
		},
		&protectionrepository.NativeFallbackStateModel{
			ResourceID: "core:inbound:backup", Schema: "solovey-ui/native-fallback-state/v1", InboundDatabaseID: 77,
			DesiredState: "NATIVE_FALLBACK", SelectedVariant: "VLESS_REALITY_HANDSHAKE_TCP", ActualState: "ROLLED_BACK",
			OperationID: "native-backup-operation", OperationRevision: "4", ReasonCodesJSON: []byte(`[]`), CreatedAt: 1, UpdatedAt: 2,
		},
	}
	for _, row := range rows {
		if err := dbsqlite.DB().Create(row).Error; err != nil {
			t.Fatalf("seed %T: %v", row, err)
		}
	}

	data, err := backup.Export("")
	if err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(t.TempDir(), "contract-v2-backup.db")
	if err := os.WriteFile(backupPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	backupProbe, err := gorm.Open(sqlite.Open(backupPath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	backupSQLDB, err := backupProbe.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer backupSQLDB.Close()
	if _, err := backup.LoadAndVerifyManifest(context.Background(), backupProbe); err != nil {
		t.Fatalf("generated backup manifest is invalid: %v", err)
	}
	for _, table := range []string{
		"server_protection_signals_v2",
		"server_protection_decisions_v2",
		"server_protection_fallback_target_leases",
		"server_protection_recovery_paths_v1",
		"server_protection_native_fallback_operations",
		"server_protection_native_fallback_states",
	} {
		if err := dbsqlite.DB().Exec("DELETE FROM " + table).Error; err != nil {
			t.Fatal(err)
		}
	}
	backup.SetSendSighupHook(func() error { return nil })
	t.Cleanup(func() { backup.SetSendSighupHook(nil) })
	if err := backup.Restore(componentMemMultipartFile{Reader: bytes.NewReader(data)}); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{
		"server_protection_signals_v2",
		"server_protection_decisions_v2",
		"server_protection_fallback_target_leases",
		"server_protection_recovery_paths_v1",
		"server_protection_native_fallback_operations",
		"server_protection_native_fallback_states",
	} {
		var count int64
		if err := dbsqlite.DB().Table(table).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("restored %s rows=%d, want 1", table, count)
		}
	}
}

func markServerProtectionInstalled(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, "components", "installed.json")
	t.Setenv(installstate.InstalledFileEnv, path)
	if err := installstate.Store(path, installstate.Metadata{Version: 1, Profile: "full", Binary: "full",
		Components: []installstate.InstalledComponent{{
			ID: componentID, Delivery: componentManifest.Delivery, Installed: true,
		}},
	}); err != nil {
		t.Fatal(err)
	}
}

func assertLifecycleBackupTable(t *testing.T, want bool) {
	t.Helper()
	data, err := backup.Export("")
	if err != nil {
		t.Fatalf("export backup: %v", err)
	}
	path := filepath.Join(t.TempDir(), "backup.db")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	for _, table := range protectionrepository.BackupTableModels() {
		if got := db.Migrator().HasTable(table.Name); got != want {
			t.Fatalf("backup %s present=%v, want %v", table.Name, got, want)
		}
	}
}
