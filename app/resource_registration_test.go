package app

import (
	"testing"

	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

func TestCoreResourcesRequireInitializedConfigService(t *testing.T) {
	application := NewApp()
	if err := application.registerResources(); err == nil {
		t.Fatal("resource registration accepted an uninitialized config service")
	}
	if application.stopResources != nil {
		t.Fatal("failed registration installed a cleanup handle")
	}
}

func TestApplicationStartRequiresInitializationAndStopIsSafe(t *testing.T) {
	application := NewApp()
	if err := application.Start(); err == nil {
		t.Fatal("Start accepted an uninitialized application")
	}
	application.Stop()
	application.Stop()
}

func TestCoreResourcesUseApplicationConfigServiceAndRegisterOnce(t *testing.T) {
	t.Setenv("SUI_DB_FOLDER", t.TempDir())
	if err := dbsqlite.Init(configstorage.GetDBPath()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbsqlite.Close() })
	application := NewApp()
	application.configService = service.NewConfigServiceWithRuntime(nil)
	control := application.configService.CoreInboundControl()

	if err := application.registerResources(); err != nil {
		t.Fatalf("register resources: %v", err)
	}
	t.Cleanup(func() {
		if application.stopResources != nil {
			application.stopResources()
			application.stopResources = nil
		}
	})
	if application.stopResources == nil {
		t.Fatal("resource registration did not install cleanup")
	}
	firstCleanup := application.stopResources
	if err := application.registerResources(); err != nil {
		t.Fatalf("repeat registration: %v", err)
	}
	if application.configService.CoreInboundControl() != control {
		t.Fatal("resource registration replaced the application core control")
	}
	if application.stopResources == nil {
		t.Fatal("repeat registration removed cleanup")
	}

	firstCleanup()
	application.stopResources = nil
}
