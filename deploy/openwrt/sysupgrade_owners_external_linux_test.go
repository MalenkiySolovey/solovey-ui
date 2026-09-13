//go:build linux && !minimal

package openwrt_test

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	_ "github.com/MalenkiySolovey/solovey-ui/app"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/enabledstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	dbbackup "github.com/MalenkiySolovey/solovey-ui/database/backup"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	openwrt "github.com/MalenkiySolovey/solovey-ui/deploy/openwrt"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/durableowner"
	settingscrypto "github.com/MalenkiySolovey/solovey-ui/internal/settings/crypto"
	"github.com/MalenkiySolovey/solovey-ui/service"
	sptest "github.com/MalenkiySolovey/solovey-ui/testsupport/serverprotection"
	gormsqlite "gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSysupgradeProductionOwnerChild(t *testing.T) {
	root := os.Getenv("SUI_SYSUPGRADE_CHILD_ROOT")
	if root == "" {
		return
	}
	phase, scenario := os.Getenv("SUI_SYSUPGRADE_CHILD_PHASE"), os.Getenv("SUI_SYSUPGRADE_CHILD_SCENARIO")
	if err := syscall.Chroot(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir("/"); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"/tmp", openwrt.DefaultDatabaseFolder, openwrt.DefaultInstallRoot + "/components"} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("TMPDIR", "/tmp")
	helperAction := os.Getenv("SUI_SYSUPGRADE_HELPER_ACTION")
	if helperAction == "" {
		t.Setenv("SUI_DB_FOLDER", openwrt.DefaultDatabaseFolder)
		t.Setenv("SUI_DEPLOYMENT_KIND", "openwrt-package-managed")
		t.Setenv(installstate.InstalledFileEnv, openwrt.DefaultInstallRoot+"/components/installed.json")
	}
	installed := installstate.Metadata{Version: 1, Profile: "full"}
	for _, owner := range registry.Components() {
		installed.Components = append(installed.Components, installstate.InstalledComponent{ID: owner.Manifest.ID, Delivery: owner.Manifest.Delivery, Installed: true})
	}
	if len(installed.Components) != 8 {
		t.Fatal("full package owner inventory incomplete")
	}
	// Models the package's exact installed-owner declaration being available
	// again after reinstall; no runtime component is started in this process.
	if helperAction == "" {
		if err := installstate.Store(installstate.DefaultPath(), installed); err != nil {
			t.Fatal(err)
		}
	}
	environment := openwrt.PreservationEnvironment{
		InspectDurableState: func(_ string, required uint64) (openwrt.DurableStateEvidence, error) {
			return openwrt.DurableStateEvidence{MountPath: "/", Persistent: true, AvailableBytes: 1 << 30, RequiredBytes: required}, nil
		},
		SyncDirectory: func(path string) error {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			return f.Sync()
		},
		RemoveFile: os.Remove, Now: time.Now,
	}
	ctx := context.Background()
	if phase == "verify-live" {
		if err := dbsqlite.Init(configstorage.GetDBPath()); err != nil {
			t.Fatal(err)
		}
		defer dbsqlite.Close()
		var check string
		if err := dbsqlite.DB().Raw("PRAGMA integrity_check").Scan(&check).Error; err != nil || check != "ok" {
			t.Fatal("backup corrupted live SQLite")
		}
		var count int64
		if err := dbsqlite.DB().Model(&model.Client{}).Where("name IN ?", []string{"sysupgrade-client", "after-seal"}).Count(&count).Error; err != nil || count != 2 {
			t.Fatal("backup lost live committed data")
		}
		return
	}
	if helperAction == "complete-backup" || helperAction == "fail-backup" {
		// Exactly the shipping helper's semantic call; never repair its shell
		// environment here. Only physical mount admission is fixture supplied.
		if err := openwrt.FinalizeSysupgradePreservationBackup(ctx, environment, helperAction == "complete-backup"); err != nil {
			t.Fatal(err)
		}
		return
	}
	if helperAction == "prepare" {
		if installstate.DefaultPath() != openwrt.DefaultInstallRoot+"/components/installed.json" {
			t.Fatal("installed owner inventory differs from the OpenWrt package")
		}
		if _, exists, err := installstate.Load(installstate.DefaultPath()); err != nil || !exists {
			t.Fatal("installed owner inventory is unavailable for preservation")
		}
		if err := dbsqlite.Init(configstorage.GetDBPath()); err != nil {
			t.Fatal(err)
		}
		defer dbsqlite.Close()
		if _, err := openwrt.PrepareSysupgradePreservation(ctx, openwrt.DefaultProfile(), environment); err != nil {
			t.Fatal(err)
		}
		if err := dbsqlite.DB().Create(&model.Client{Name: "after-seal", SubSecret: "fixture-after-seal-secret", Inbounds: []byte("[]"), Links: []byte("[]")}).Error; err != nil {
			t.Fatal(err)
		}
		return
	}
	if phase == "prepare" {
		if err := dbsqlite.Init(configstorage.GetDBPath()); err != nil {
			t.Fatal(err)
		}
		defer dbsqlite.Close()
		for _, owner := range installed.Components {
			if err := durableowner.RunStagedRestore(ctx, owner.ID, dbsqlite.DB()); err != nil {
				t.Fatal(err)
			}
		}
		if err := dbsqlite.DB().Exec("PRAGMA journal_mode=WAL").Error; err != nil {
			t.Fatal(err)
		}
		if err := dbsqlite.DB().Exec("PRAGMA wal_autocheckpoint=0").Error; err != nil {
			t.Fatal(err)
		}
		if err := dbsqlite.DB().Create(&model.Client{Name: "sysupgrade-client", Enable: true, SubSecret: "fixture-subscription-secret", Inbounds: []byte("[]"), Links: []byte("[]")}).Error; err != nil {
			t.Fatal(err)
		}
		codec := settingscrypto.Codec{MasterSecret: (&service.SettingService{}).GetSecret}
		box, err := codec.Secretbox()
		if err != nil {
			t.Fatal(err)
		}
		encrypted, err := box.EncryptString("fixture-encrypted-secret", "sysupgrade-secret")
		if err != nil {
			t.Fatal(err)
		}
		for key, value := range map[string]string{"sysupgrade-secret": encrypted, "webPort": "2095", "subPort": "2096", enabledstate.SettingKey("server-protection"): "false"} {
			if err := dbsqlite.DB().Where("key = ?", key).Delete(&model.Setting{}).Error; err != nil {
				t.Fatal(err)
			}
			if err := dbsqlite.DB().Create(&model.Setting{Key: key, Value: value}).Error; err != nil {
				t.Fatal(err)
			}
		}
		sptest.SeedSysupgradeDurableFacts(t, dbsqlite.DB(), scenario)
		if info, err := os.Stat(configstorage.GetDBPath() + "-wal"); err != nil || info.Size() == 0 {
			t.Fatal("live WAL fixture missing")
		}
		if _, err := openwrt.PrepareSysupgradePreservation(ctx, openwrt.DefaultProfile(), environment); err != nil {
			t.Fatal(err)
		}
		probe, err := gorm.Open(gormsqlite.Open(openwrt.DefaultPreservationSnapshot+"?mode=ro"), &gorm.Config{})
		if err != nil {
			t.Fatal(err)
		}
		pool, _ := probe.DB()
		defer pool.Close()
		manifest, err := dbbackup.LoadAndVerifyManifest(ctx, probe)
		if err != nil {
			t.Fatal(err)
		}
		if len(manifest.Owners) != 9 {
			t.Fatal("snapshot omitted installed owners")
		}
		excluded, err := durableowner.NonportableBackupTables("server-protection")
		if err != nil {
			t.Fatal(err)
		}
		for table := range excluded {
			if probe.Migrator().HasTable(table) {
				t.Fatal("host-local table entered snapshot")
			}
		}
		return
	}
	if phase == "reject" {
		if err := openwrt.RestoreSysupgradePreservationIfNeeded(ctx, environment); err == nil {
			t.Fatal("damaged preservation was accepted")
		}
		if dbsqlite.DB() != nil {
			t.Fatal("rejected preservation published a database")
		}
		if _, err := os.Stat(configstorage.GetDBPath()); !os.IsNotExist(err) {
			t.Fatal("rejection created live data")
		}
		return
	}
	if phase != "restore" {
		t.Fatal("unknown fixture phase")
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if _, err := os.Stat(configstorage.GetDBPath() + suffix); !os.IsNotExist(err) {
			t.Fatal("raw database present in fresh root")
		}
	}
	dbbackup.SetSendSighupHook(func() error { return nil })
	if err := openwrt.RestoreSysupgradePreservationIfNeeded(ctx, environment); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.Init(configstorage.GetDBPath()); err != nil {
		t.Fatal(err)
	}
	defer dbsqlite.Close()
	var check string
	if err := dbsqlite.DB().Raw("PRAGMA quick_check").Scan(&check).Error; err != nil || check != "ok" {
		t.Fatal("restored SQLite integrity failed")
	}
	var client model.Client
	if err := dbsqlite.DB().Where("name=?", "sysupgrade-client").First(&client).Error; err != nil || client.SubSecret != "fixture-subscription-secret" {
		t.Fatal("subscription secret lost")
	}
	var late int64
	if err := dbsqlite.DB().Model(&model.Client{}).Where("name=?", "after-seal").Count(&late).Error; err != nil || late != 0 {
		t.Fatal("snapshot was not point-in-time")
	}
	var encrypted model.Setting
	if err := dbsqlite.DB().Where("key=?", "sysupgrade-secret").First(&encrypted).Error; err != nil {
		t.Fatal(err)
	}
	codec := settingscrypto.Codec{MasterSecret: (&service.SettingService{}).GetSecret}
	box, err := codec.Secretbox()
	if err != nil {
		t.Fatal(err)
	}
	clear, err := box.DecryptString(encrypted.Value, "sysupgrade-secret")
	if err != nil || clear != "fixture-encrypted-secret" {
		t.Fatal("restored secret cannot decrypt")
	}
	for key, value := range map[string]string{"webPort": "2095", "subPort": "2096", enabledstate.SettingKey("server-protection"): "false"} {
		var setting model.Setting
		if err := dbsqlite.DB().Where("key=?", key).First(&setting).Error; err != nil || setting.Value != value {
			t.Fatal("configuration lost")
		}
	}
	component, _ := registry.ComponentByID("server-protection")
	if enabled, err := enabledstate.Enabled(component.Manifest); err != nil || enabled {
		t.Fatal("disabled component state was not preserved")
	}
	sptest.AssertSysupgradeDurableFacts(t, dbsqlite.DB(), scenario)
	if _, err := os.Stat(filepath.Join(openwrt.DefaultDatabaseFolder, "initial-admin.txt")); !os.IsNotExist(err) {
		t.Fatal("restoration left bootstrap credentials")
	}
}
