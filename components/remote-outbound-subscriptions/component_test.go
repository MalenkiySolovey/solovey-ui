//go:build !minimal

package remoteoutboundsubscriptions

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	remotesettings "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/internal/settings"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	outboundentities "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
	localsub "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/local"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"gorm.io/gorm"
)

func TestRuntimeHooksFollowComponentLifecycle(t *testing.T) {
	localsub.ResetClientOutboundContributorsForTest()
	service.ResetOutboundSaveHooksForTest()
	outboundentities.ResetDeleteHooksForTest()
	outboundentities.ResetMetadataAnnotatorsForTest()
	t.Cleanup(localsub.ResetClientOutboundContributorsForTest)
	t.Cleanup(service.ResetOutboundSaveHooksForTest)
	t.Cleanup(outboundentities.ResetDeleteHooksForTest)
	t.Cleanup(outboundentities.ResetMetadataAnnotatorsForTest)

	beforeRegister := localsub.ClientOutboundContributorsVersion()
	registerRuntimeHooks()
	afterRegister := localsub.ClientOutboundContributorsVersion()
	if afterRegister == beforeRegister {
		t.Fatal("remote component start did not register client outbound contributor")
	}

	unregisterRuntimeHooks()
	afterUnregister := localsub.ClientOutboundContributorsVersion()
	if afterUnregister == afterRegister {
		t.Fatal("remote component stop did not unregister client outbound contributor")
	}
}

func TestRemoteSettingsAreContributionOwned(t *testing.T) {
	settingService := initRemoteComponentSettingTestDB(t)

	settings, err := settingService.GetAllSetting()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := (*settings)[remotesettings.GroupAdaptationKey]; ok {
		t.Fatal("remote setting should be absent before the component registers its contribution")
	}

	unregister := registerSettingContribution()
	t.Cleanup(unregister)

	settings, err = settingService.GetAllSetting()
	if err != nil {
		t.Fatal(err)
	}
	if got := (*settings)[remotesettings.GroupAdaptationKey]; got != "urltest" {
		t.Fatalf("remote group adaptation default = %q", got)
	}

	validPayload, err := json.Marshal(map[string]string{
		remotesettings.GroupAdaptationKey: "failover",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Transaction(func(tx *gorm.DB) error {
		return settingService.Save(tx, validPayload)
	}); err != nil {
		t.Fatalf("valid remote setting rejected: %v", err)
	}

	invalidPayload, err := json.Marshal(map[string]string{
		remotesettings.GroupAdaptationKey: "relay",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Transaction(func(tx *gorm.DB) error {
		return settingService.Save(tx, invalidPayload)
	}); err == nil {
		t.Fatal("expected invalid remote setting to be rejected")
	}
}

func initRemoteComponentSettingTestDB(t *testing.T) *service.SettingService {
	t.Helper()
	tempDir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", tempDir)
	if err := dbsqlite.Init(filepath.Join(tempDir, "s-ui.db")); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	testDB := dbsqlite.DB()
	t.Cleanup(func() {
		if testDB != nil {
			if sqlDB, err := testDB.DB(); err == nil {
				_ = sqlDB.Close()
				time.Sleep(25 * time.Millisecond)
			}
		}
	})
	return &service.SettingService{}
}
