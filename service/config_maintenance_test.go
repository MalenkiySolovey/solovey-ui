package service

import (
	"encoding/json"
	"errors"
	"net"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	corebox "github.com/MalenkiySolovey/solovey-ui/core/box"
	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
	"github.com/sagernet/sing-box/log"
)

func maintenanceFixture(t *testing.T) (*ConfigService, *coreruntime.Core) {
	t.Helper()
	initSettingTestDB(t)
	core := coreruntime.NewCore()
	t.Cleanup(func() { _ = core.Stop() })
	return NewConfigServiceWithRuntime(NewRuntime(core)), core
}

func TestMaintenancePersistsAcrossLifecycleSaveResetAndRestoreInitialization(t *testing.T) {
	s, core := maintenanceFixture(t)
	if held, err := s.CoreMaintenance(); err != nil || held {
		t.Fatalf("missing key must default off: held=%v err=%v", held, err)
	}
	if err := s.StartCore(); err != nil {
		t.Fatal(err)
	}
	oldGeneration := core.RuntimeStatus(t.Context()).Generation
	if err := s.SetCoreMaintenance(t.Context(), oldGeneration, true); err != nil {
		t.Fatal(err)
	}
	if core.IsRunning() {
		t.Fatal("maintenance did not stop core")
	}
	// A new service represents boot/restore/cron consumers of the same durable
	// settings. None of them can silently reopen the core during a hold.
	reloaded := NewConfigServiceWithRuntime(s.Runtime)
	for name, call := range map[string]func() error{"boot/cron": reloaded.StartCore, "operator restart": reloaded.RestartCore} {
		if err := call(); err != nil || core.IsRunning() {
			t.Fatalf("%s defeated hold: %v", name, err)
		}
	}
	plan := newConfigSavePlan("config")
	plan.RequireCoreRestart("maintenance save")
	if err := reloaded.applyCoreSaveEffect(plan); err != nil || core.IsRunning() {
		t.Fatal("config save defeated hold", err)
	}
	if err := s.ResetSettings(); err != nil {
		t.Fatal(err)
	}
	if err := initializeRestoreImportSettings(t.Context(), &s.SettingService, &restoreImportPostOpenResult{}); err != nil {
		t.Fatal(err)
	}
	if held, err := s.CoreMaintenance(); err != nil || !held {
		t.Fatal("reset/restored settings lost deliberate hold", err)
	}
	public, err := s.GetAllSetting()
	if err != nil {
		t.Fatal(err)
	}
	if _, leaked := (*public)[settingcatalog.CoreMaintenanceKey]; leaked {
		t.Fatal("generic settings exposed lifecycle flag")
	}
	if err := s.SettingService.Save(dbsqlite.DB(), []byte(`{"coreMaintenance":"false"}`)); err == nil {
		t.Fatal("generic settings bypassed lifecycle authority")
	}
	if err := reloaded.SetCoreMaintenance(t.Context(), "", false); err != nil {
		t.Fatal(err)
	}
	if !core.IsRunning() || core.RuntimeStatus(t.Context()).Generation == oldGeneration {
		t.Fatal("resume did not validate/publish a fresh generation")
	}
}

func TestMaintenanceReadAndWriteFailuresDoNotMutateHealthyCore(t *testing.T) {
	s, core := maintenanceFixture(t)
	if err := s.StartCore(); err != nil {
		t.Fatal(err)
	}
	generation := core.RuntimeStatus(t.Context()).Generation
	if err := s.setCoreMaintenanceValue("true"); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Model(&model.Setting{}).Where("key = ?", settingcatalog.CoreMaintenanceKey).Update("value", "invalid").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.RestartCore(); !errors.Is(err, ErrMaintenanceUnavailable) || !core.IsRunning() || core.RuntimeStatus(t.Context()).Generation != generation {
		t.Fatal("unknown desired state disturbed accepted runtime", err)
	}
	if err := core.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := s.StartCore(); !errors.Is(err, ErrMaintenanceUnavailable) || core.IsRunning() {
		t.Fatal("invalid desired state permitted startup", err)
	}
	if err := s.setCoreMaintenanceValue("false"); err != nil {
		t.Fatal(err)
	}
	if err := s.StartCore(); err != nil {
		t.Fatal(err)
	}
	generation = core.RuntimeStatus(t.Context()).Generation
	if err := dbsqlite.DB().Migrator().DropTable(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	if err := s.RestartCore(); !errors.Is(err, ErrMaintenanceUnavailable) || !core.IsRunning() {
		t.Fatal("DB read error was interpreted as maintenance off", err)
	}
	if err := s.SetCoreMaintenance(t.Context(), generation, true); !errors.Is(err, ErrMaintenanceUnavailable) || !core.IsRunning() {
		t.Fatal("failed flag write stopped core", err)
	}
}

func TestMaintenanceRejectsLateGenerationAndReportsFailedResume(t *testing.T) {
	s, core := maintenanceFixture(t)
	if err := s.StartCore(); err != nil {
		t.Fatal(err)
	}
	generation := core.RuntimeStatus(t.Context()).Generation
	if err := s.SetCoreMaintenance(t.Context(), "00000000-0000-4000-8000-000000000001", true); !errors.Is(err, coreruntime.ErrStaleGeneration) {
		t.Fatal("stale hold accepted", err)
	}
	if held, _ := s.CoreMaintenance(); held || !core.IsRunning() {
		t.Fatal("late request changed desired state")
	}
	if err := s.SetCoreMaintenance(t.Context(), generation, true); err != nil {
		t.Fatal(err)
	}
	// Occupied desired inbound port reaches the real listener startup failure.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	options, err := json.Marshal(map[string]any{"listen": "127.0.0.1", "listen_port": listener.Addr().(*net.TCPAddr).Port})
	if err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Create(&model.Inbound{Type: "socks", Tag: "occupied", Options: options}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.SetCoreMaintenance(t.Context(), "", false); err == nil {
		t.Fatal("failed resume reported success")
	}
	if held, err := s.CoreMaintenance(); err != nil || held || core.IsRunning() {
		t.Fatal("desired off and failed actual state were conflated", err)
	}
	if core.RuntimeStatus(t.Context()).State != "stopped_by_error" {
		t.Fatal("failed start was reported as maintenance")
	}
}

func TestMaintenanceSurvivesDatabaseCloseAndReopen(t *testing.T) {
	s, core := maintenanceFixture(t)
	if err := s.StartCore(); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCoreMaintenance(t.Context(), core.RuntimeStatus(t.Context()).Generation, true); err != nil {
		t.Fatal(err)
	}
	var databases []struct{ Name, File string }
	if err := dbsqlite.DB().Raw("PRAGMA database_list").Scan(&databases).Error; err != nil {
		t.Fatal(err)
	}
	var path string
	for _, database := range databases {
		if database.Name == "main" {
			path = database.File
		}
	}
	if path == "" {
		t.Fatal("fixture has no persistent database")
	}
	if err := dbsqlite.Close(); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.Init(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbsqlite.Close() })
	boot := NewConfigServiceWithRuntime(NewRuntime(coreruntime.NewCore()))
	t.Cleanup(func() { _ = boot.StopCore() })
	if held, err := boot.CoreMaintenance(); err != nil || !held {
		t.Fatalf("durable hold was lost: held=%v err=%v", held, err)
	}
	if err := boot.StartCore(); err != nil || boot.IsCoreRunning() {
		t.Fatal("boot after database reopen defeated hold", err)
	}
}

type maintenanceStopFailureFactory struct {
	log.Factory
	err    error
	closes int
}

func (i *maintenanceStopFailureFactory) Close() error {
	i.closes++
	return errors.Join(i.Factory.Close(), i.err)
}

func TestMaintenancePreservesIntentAndReportsFailedStop(t *testing.T) {
	s, core := maintenanceFixture(t)
	if err := s.StartCore(); err != nil {
		t.Fatal(err)
	}
	generation := core.RuntimeStatus(t.Context()).Generation
	// Inject a returned close failure at an existing owner in the real Box
	// teardown, without a production injection seam. The pinned inbound manager
	// discards child Close errors, so it cannot witness this returned-error path.
	field := reflect.ValueOf(core).Elem().FieldByName("instance")
	box := reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Interface().(*corebox.Box)
	factoryField := reflect.ValueOf(box).Elem().FieldByName("logFactory")
	factory := reflect.NewAt(factoryField.Type(), unsafe.Pointer(factoryField.UnsafeAddr())).Elem().Interface().(log.Factory)
	want := errors.New("fixture close failure")
	candidate := &maintenanceStopFailureFactory{Factory: factory, err: want}
	setUnexportedFieldForConfigSaveTest(factoryField, reflect.ValueOf(candidate))
	if err := s.SetCoreMaintenance(t.Context(), generation, true); err == nil || !strings.Contains(err.Error(), want.Error()) {
		t.Fatal("failed close was reported as success", err)
	}
	held, err := s.CoreMaintenance()
	status := core.RuntimeStatus(t.Context())
	if err != nil || !held || core.IsRunning() || status.State != "stopped_by_error" || status.Generation != "" || candidate.closes != 1 {
		t.Fatalf("failed stop conflated desired/actual: held=%v err=%v status=%+v closes=%d", held, err, status, candidate.closes)
	}
	if err := s.StartCore(); err != nil || core.IsRunning() {
		t.Fatal("automatic recovery defeated hold after failed stop", err)
	}
}
