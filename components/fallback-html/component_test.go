//go:build !minimal

package fallbackhtml

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	neutral "github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/components/fallback-html/authority"
	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
	fallbackservice "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/service"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	dbbackup "github.com/MalenkiySolovey/solovey-ui/database/backup"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDropDataRemovesSchemaAndStorage(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", dir)
	_ = dbsqlite.Close()
	if err := dbsqlite.Init(configstorage.GetDBPath()); err != nil {
		t.Fatalf("init sqlite: %v", err)
	}
	t.Cleanup(func() { _ = dbsqlite.Close() })

	ctx := context.Background()
	if err := (component{}).Migrate(ctx, lifecycle.Context{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db := dbsqlite.DB()
	setComponentTestSetting(t, "webPath", "/secret-panel/")
	service := fallbackservice.New(db, fallbackservice.NewRuntime())
	site, err := service.SaveSite(fallbackservice.SiteInput{Name: "Example Portal"}, "tester")
	if err != nil {
		t.Fatalf("SaveSite: %v", err)
	}
	if _, err := service.PublishSite(site.ID, "tester"); err != nil {
		t.Fatalf("PublishSite: %v", err)
	}
	if err := db.Create(&model.InboundDraft{
		Source:      "fallback-html:self-steal",
		SourceRef:   "site/1",
		Status:      "blocked",
		InboundType: "vless",
		Tag:         "fallback-html-site-1",
		Payload:     []byte("{}"),
	}).Error; err != nil {
		t.Fatalf("create component-owned core draft: %v", err)
	}
	if err := db.Create(&model.InboundDraft{
		Source:    "other-component",
		SourceRef: "keep",
		Status:    "blocked",
		Payload:   []byte("{}"),
	}).Error; err != nil {
		t.Fatalf("create foreign core draft: %v", err)
	}
	storageRoot := filepath.Join(configstorage.GetDBFolderPath(), "fallback-html")
	if _, err := os.Stat(storageRoot); err != nil {
		t.Fatalf("storage root should exist before DropData: %v", err)
	}
	siblingRoot := filepath.Join(configstorage.GetDBFolderPath(), "not-fallback-html")
	siblingFile := filepath.Join(siblingRoot, "keep.txt")
	if err := os.MkdirAll(siblingRoot, 0o750); err != nil {
		t.Fatalf("create sibling storage: %v", err)
	}
	if err := os.WriteFile(siblingFile, []byte("keep"), 0o640); err != nil {
		t.Fatalf("write sibling storage: %v", err)
	}

	if err := (component{}).DropData(ctx, lifecycle.Context{}); err != nil {
		t.Fatalf("DropData: %v", err)
	}
	if db.Migrator().HasTable(&fallbackdomain.Site{}) {
		t.Fatalf("fallback_html_sites table still exists after DropData")
	}
	var fallbackDrafts int64
	if err := db.Model(&model.InboundDraft{}).Where("source LIKE ?", "fallback-html:%").Count(&fallbackDrafts).Error; err != nil {
		t.Fatalf("count removed core drafts: %v", err)
	}
	if fallbackDrafts != 0 {
		t.Fatalf("component-owned core drafts remain after DropData: %d", fallbackDrafts)
	}
	var foreignDrafts int64
	if err := db.Model(&model.InboundDraft{}).Where("source = ?", "other-component").Count(&foreignDrafts).Error; err != nil {
		t.Fatalf("count foreign core drafts: %v", err)
	}
	if foreignDrafts != 1 {
		t.Fatalf("DropData removed foreign core drafts: %d", foreignDrafts)
	}
	if _, err := os.Stat(storageRoot); !os.IsNotExist(err) {
		t.Fatalf("storage root after DropData = %v, want not exists", err)
	}
	if _, err := os.Stat(siblingFile); err != nil {
		t.Fatalf("DropData removed non-component storage: %v", err)
	}
}

func TestDropDataRefusesGuardingProviderReservationUntilVerifiedRelease(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", dir)
	_ = dbsqlite.Close()
	if err := dbsqlite.Init(configstorage.GetDBPath()); err != nil {
		t.Fatalf("init sqlite: %v", err)
	}
	t.Cleanup(func() { _ = dbsqlite.Close() })
	ctx := context.Background()
	if err := (component{}).Migrate(ctx, lifecycle.Context{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db := dbsqlite.DB()
	setComponentTestSetting(t, "webPath", "/secret-panel/")
	service := fallbackservice.New(db, fallbackservice.NewRuntime())
	site, err := service.SaveSite(fallbackservice.SiteInput{Name: "Reserved"}, "tester")
	if err != nil {
		t.Fatalf("SaveSite: %v", err)
	}
	if _, err := service.PublishSite(site.ID, "tester"); err != nil {
		t.Fatalf("PublishSite: %v", err)
	}
	now := time.Now().UTC()
	target, err := neutral.FinalizeFallbackTargetV2(neutral.FallbackTargetV2{
		Identity:         neutral.TargetIdentity{ProviderID: id, TargetID: "site:" + strconv.FormatUint(uint64(site.ID), 10)},
		Publish:          neutral.PublishFactsV2{Revision: "publish-drop", ContentDigest: strings.Repeat("a", 64)},
		Endpoint:         neutral.EndpointV2{EndpointID: "endpoint-drop", Network: hostresources.NetworkTCP, AddressFamily: hostresources.AddressFamilyIPv4, Address: "127.0.0.1", Port: 8080, Local: true, TransportSecurity: neutral.TransportSecurityPlaintext, ApplicationProtocols: []neutral.ApplicationProtocol{neutral.ApplicationProtocolHTTP11}, ProxyProtocol: hostresources.CapabilityNo, CanReachManagement: hostresources.CapabilityNo},
		Health:           neutral.HealthV2{Readiness: neutral.ReadinessReady, ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix()},
		Capacity:         neutral.CapacityV2{State: neutral.CapacityReady, ReservationSlotsTotal: authority.SlotsPerTarget, ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix()},
		ProviderRevision: "provider-drop", Source: "fallback-html-runtime", ConfidenceBP: 10000,
	})
	if err != nil {
		t.Fatalf("finalize target: %v", err)
	}
	reference, _ := neutral.ReferenceV2FromTarget(target)
	reservation := neutral.ProviderTargetReservationV1{Schema: neutral.ProviderTargetReservationSchemaV1, ReservationID: "drop-guard", ReservationRevision: "drop-revision", HolderID: "holder", Purpose: neutral.ReservationPurposeNativeFallback, ExactTargetReference: reference, State: neutral.ReservationActive, IssuedAt: now.Unix(), RenewedAt: now.Unix(), FreshnessExpiresAt: now.Add(time.Minute).Unix()}
	row, err := authority.EncodeReservation(reservation, now.Unix(), now.Unix())
	if err != nil {
		t.Fatalf("encode reservation: %v", err)
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("create reservation: %v", err)
	}
	if err := (component{}).DropData(ctx, lifecycle.Context{}); err == nil {
		t.Fatalf("DropData succeeded with active reservation")
	}
	if !db.Migrator().HasTable(&fallbackdomain.Site{}) {
		t.Fatalf("blocked DropData removed component schema")
	}
	reservation.State = neutral.ReservationReleased
	reservation.ReservationRevision = "drop-released"
	reservation.ReleasedAt = now.Add(time.Second).Unix()
	releasedRow, err := authority.EncodeReservation(reservation, row.CreatedAt, now.Add(time.Second).Unix())
	if err != nil {
		t.Fatalf("encode released reservation: %v", err)
	}
	if err := db.Save(&releasedRow).Error; err != nil {
		t.Fatalf("save released reservation: %v", err)
	}
	if err := (component{}).DropData(ctx, lifecycle.Context{}); err != nil {
		t.Fatalf("DropData after release: %v", err)
	}
}

func TestBackupIncludesProviderAuthorityWithoutHostPathsOrSecrets(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", dir)
	installedPath := filepath.Join(dir, "components", "installed.json")
	t.Setenv(installstate.InstalledFileEnv, installedPath)
	if err := installstate.Store(installedPath, installstate.Metadata{Version: 1, Components: []installstate.InstalledComponent{{
		ID: id, Delivery: componentManifest.Delivery, Installed: true,
	}}}); err != nil {
		t.Fatal(err)
	}
	_ = dbsqlite.Close()
	if err := dbsqlite.Init(configstorage.GetDBPath()); err != nil {
		t.Fatalf("init sqlite: %v", err)
	}
	t.Cleanup(func() { _ = dbsqlite.Close() })
	if err := (component{}).Migrate(context.Background(), lifecycle.Context{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().UTC()
	target, err := neutral.FinalizeFallbackTargetV2(neutral.FallbackTargetV2{
		Identity:         neutral.TargetIdentity{ProviderID: id, TargetID: "site:backup"},
		Publish:          neutral.PublishFactsV2{Revision: "publish-backup", ContentDigest: strings.Repeat("b", 64)},
		Endpoint:         neutral.EndpointV2{EndpointID: "endpoint-backup", Network: hostresources.NetworkTCP, AddressFamily: hostresources.AddressFamilyIPv4, Address: "127.0.0.1", Port: 8080, Local: true, TransportSecurity: neutral.TransportSecurityPlaintext, ApplicationProtocols: []neutral.ApplicationProtocol{neutral.ApplicationProtocolHTTP11}, ProxyProtocol: hostresources.CapabilityNo, CanReachManagement: hostresources.CapabilityNo},
		Health:           neutral.HealthV2{Readiness: neutral.ReadinessReady, ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix()},
		Capacity:         neutral.CapacityV2{State: neutral.CapacityReady, ReservationSlotsTotal: authority.SlotsPerTarget, ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix()},
		ProviderRevision: "provider-backup", Source: "fallback-html-runtime", ConfidenceBP: 10000,
	})
	if err != nil {
		t.Fatalf("finalize target: %v", err)
	}
	reference, _ := neutral.ReferenceV2FromTarget(target)
	reservation := neutral.ProviderTargetReservationV1{Schema: neutral.ProviderTargetReservationSchemaV1, ReservationID: "backup-reservation", ReservationRevision: "backup-revision", HolderID: "holder", Purpose: neutral.ReservationPurposeNativeFallback, ExactTargetReference: reference, State: neutral.ReservationReleased, IssuedAt: now.Unix(), RenewedAt: now.Unix(), FreshnessExpiresAt: now.Add(time.Minute).Unix(), ReleasedAt: now.Add(time.Second).Unix()}
	row, err := authority.EncodeReservation(reservation, now.Unix(), now.Unix())
	if err != nil {
		t.Fatalf("encode reservation: %v", err)
	}
	if err := dbsqlite.DB().Create(&row).Error; err != nil {
		t.Fatalf("create reservation: %v", err)
	}
	historicalPayload := json.RawMessage(`{"schema":"fallback-html.self-steal.v1","historical":"retained"}`)
	historical := fallbackdomain.SelfStealDraft{
		SiteID: 99, Status: fallbackservice.LegacySelfStealRetiredStatus,
		Payload: historicalPayload, CreatedAt: now.Unix(),
	}
	if err := dbsqlite.DB().Create(&historical).Error; err != nil {
		t.Fatalf("create historical compatibility row: %v", err)
	}
	if err := registerRuntimeHooks(); err != nil {
		t.Fatalf("register hooks: %v", err)
	}
	t.Cleanup(unregisterRuntimeHooks)
	path, cleanup, err := dbbackup.PrepareExport("")
	if err != nil {
		t.Fatalf("PrepareExport: %v", err)
	}
	defer cleanup()
	backupDB, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}
	backupSQLDB, err := backupDB.DB()
	if err != nil {
		t.Fatalf("open backup SQL handle: %v", err)
	}
	defer backupSQLDB.Close()
	var restored authority.ReservationModel
	if err := backupDB.First(&restored, "reservation_id = ?", reservation.ReservationID).Error; err != nil {
		t.Fatalf("reservation missing from backup: %v", err)
	}
	var restoredHistorical fallbackdomain.SelfStealDraft
	if err := backupDB.First(&restoredHistorical, historical.ID).Error; err != nil {
		t.Fatalf("historical compatibility row missing from backup: %v", err)
	}
	if restoredHistorical.Status != fallbackservice.LegacySelfStealRetiredStatus ||
		string(restoredHistorical.Payload) != string(historicalPayload) {
		t.Fatalf("historical compatibility row changed during backup")
	}
	serialized := restored.TargetReferenceJSON + restored.ReasonCodesJSON
	for _, forbidden := range []string{filepath.ToSlash(dir), "private_key", "certificate", "root_dir"} {
		if strings.Contains(strings.ToLower(serialized), strings.ToLower(forbidden)) {
			t.Fatalf("backup authority leaked %q: %s", forbidden, serialized)
		}
	}
}

func TestProviderV2RegistrationFollowsRuntimeHookLifecycle(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", dir)
	_ = dbsqlite.Close()
	if err := dbsqlite.Init(configstorage.GetDBPath()); err != nil {
		t.Fatalf("init sqlite: %v", err)
	}
	t.Cleanup(func() { _ = dbsqlite.Close() })
	if err := (component{}).Migrate(context.Background(), lifecycle.Context{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := registerRuntimeHooks(); err != nil {
		t.Fatalf("registerRuntimeHooks: %v", err)
	}
	if _, err := neutral.Default.RegisterV2(targetProvider{db: dbsqlite.DB()}); err == nil {
		t.Fatalf("duplicate Provider V2 registration unexpectedly succeeded")
	}
	unregisterRuntimeHooks()
	unregister, err := neutral.Default.RegisterV2(targetProvider{db: dbsqlite.DB()})
	if err != nil {
		t.Fatalf("Provider V2 remained registered after lifecycle stop: %v", err)
	}
	unregister()
}

func TestManifestDeclaresExactFallbackHTMLTables(t *testing.T) {
	var raw struct {
		Database struct {
			Tables []string `json:"tables"`
		} `json:"database"`
	}
	if err := json.Unmarshal(componentJSON, &raw); err != nil {
		t.Fatalf("decode component manifest: %v", err)
	}
	want := []string{
		"fallback_html_sites", "fallback_html_pages", "fallback_html_redirects", "fallback_html_assets",
		"fallback_html_publishes",
		"fallback_html_publish_files", "fallback_html_publish_redirects", "fallback_html_safety_reports",
		"fallback_html_template_sources", "fallback_html_self_steal_drafts", "fallback_html_runtime_targets",
		"fallback_html_external_resources", "fallback_html_events", authority.ReservationTable, authority.ReservationReplayTable,
	}
	slices.Sort(raw.Database.Tables)
	slices.Sort(want)
	if !slices.Equal(raw.Database.Tables, want) {
		t.Fatalf("manifest tables = %v, want %v", raw.Database.Tables, want)
	}
}

func setComponentTestSetting(t *testing.T, key string, value string) {
	t.Helper()
	db := dbsqlite.DB()
	if err := db.Where("key = ?", key).Delete(&model.Setting{}).Error; err != nil {
		t.Fatalf("delete setting %s: %v", key, err)
	}
	if err := db.Create(&model.Setting{Key: key, Value: value}).Error; err != nil {
		t.Fatalf("create setting %s: %v", key, err)
	}
}
