package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"gorm.io/gorm"
)

func TestProviderConfigurationSnapshotPreservesInactiveDesiredRowsWithoutWriting(t *testing.T) {
	s, core := maintenanceFixture(t)
	db := dbsqlite.DB()
	const secret = "provider-snapshot-credential-sentinel"
	endpoint := model.Endpoint{Type: "unregistered-provider-fixture", Tag: "preserved-endpoint", Options: json.RawMessage(`{"credential":"` + secret + `"}`)}
	if err := db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	var before []model.Setting
	if err := db.Order("id").Find(&before).Error; err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.CaptureProviderConfiguration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Clear()
	if core.IsRunning() || snapshot.Maintenance || len(snapshot.ConfigurationDigest) != 64 || len(snapshot.ConfigurationRevision) != 64 || len(snapshot.CapabilityRevision) != 64 {
		t.Fatal("capture changed runtime or omitted checkpoint evidence")
	}
	if !json.Valid(snapshot.Configuration) || bytes.Contains(snapshot.Configuration, []byte(secret)) || len(snapshot.Preserved) != 1 || snapshot.Preserved[0].ID != endpoint.Id || snapshot.Preserved[0].Reason == "" {
		t.Fatal("inactive source was silently projected or lost its preservation diagnostic")
	}
	projection, err := json.Marshal(snapshot)
	if err != nil || bytes.Contains(projection, []byte(secret)) || bytes.Contains(projection, []byte(`"configuration":`)) {
		t.Fatal("snapshot projection exposed configuration bytes")
	}
	var after []model.Setting
	if err := db.Order("id").Find(&after).Error; err != nil {
		t.Fatal(err)
	}
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(after)
	if !bytes.Equal(beforeJSON, afterJSON) {
		t.Fatal("read-only capture initialized or changed settings")
	}
	if err := db.Model(&endpoint).Update("options", json.RawMessage(`{"credential":"changed-preserved-value"}`)).Error; err != nil {
		t.Fatal(err)
	}
	changed, err := s.CaptureProviderConfiguration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer changed.Clear()
	if changed.ConfigurationRevision == snapshot.ConfigurationRevision || changed.ConfigurationDigest != snapshot.ConfigurationDigest {
		t.Fatal("inactive Options were not independently bound to desired-state revision")
	}
	var retained model.Endpoint
	if err := db.First(&retained, endpoint.Id).Error; err != nil || !bytes.Contains(retained.Options, []byte("changed-preserved-value")) {
		t.Fatal("capture changed the unsupported source row")
	}
	owned := snapshot.Configuration
	snapshot.Clear()
	if len(snapshot.Configuration) != 0 || strings.Trim(string(owned), "\x00") != "" {
		t.Fatal("configuration clear retained credential-bearing bytes")
	}
}

func TestProviderDesiredRevisionBindsHiddenColumnsAndDisabledClients(t *testing.T) {
	maintenanceFixture(t)
	db := dbsqlite.DB()
	client := model.Client{Enable: false, Name: "disabled", Config: json.RawMessage(`{"password":"first-hidden-value"}`)}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	initial, err := providerDesiredRevision(db.WithContext(t.Context()))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&client).Updates(map[string]any{"up": 123, "down": 456, "last_online": 789, "total_up": 1024}).Error; err != nil {
		t.Fatal(err)
	}
	observed, err := providerDesiredRevision(db.WithContext(t.Context()))
	if err != nil || observed != initial {
		t.Fatal("traffic observation changed desired configuration revision", err)
	}
	if err := db.Model(&client).Update("config", json.RawMessage(`{"password":"second-hidden-value"}`)).Error; err != nil {
		t.Fatal(err)
	}
	changed, err := providerDesiredRevision(db.WithContext(t.Context()))
	if err != nil || changed == initial {
		t.Fatal("disabled credential edit escaped desired revision", err)
	}
	if err := db.Model(&client).Update("enable", true).Error; err != nil {
		t.Fatal(err)
	}
	enabled, err := providerDesiredRevision(db.WithContext(t.Context()))
	if err != nil || enabled == changed {
		t.Fatal("access intent escaped desired revision", err)
	}
}

func TestProviderDesiredRevisionDistinguishesSQLiteValueKinds(t *testing.T) {
	maintenanceFixture(t)
	db := dbsqlite.DB().WithContext(t.Context())
	endpoint := model.Endpoint{Type: "unregistered-provider-fixture", Tag: "preserved-value-kinds", Options: json.RawMessage(`{}`)}
	if err := db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	blobRevision, err := providerDesiredRevision(db)
	if err != nil {
		t.Fatal(err)
	}
	// Plain JSON encoding represents a []byte as base64 text. A text column
	// holding that base64 must not acquire the same revision as the raw BLOB.
	if err := db.Model(&endpoint).Update("options", gorm.Expr("CAST(? AS TEXT)", base64.StdEncoding.EncodeToString(endpoint.Options))).Error; err != nil {
		t.Fatal(err)
	}
	textRevision, err := providerDesiredRevision(db)
	if err != nil || textRevision == blobRevision {
		t.Fatal("different persisted SQLite values shared one desired revision", err)
	}
}

func TestProviderConfigurationSnapshotUsesExistingDatabaseMaintenanceAuthority(t *testing.T) {
	s, _ := maintenanceFixture(t)
	owner, err := dbsqlite.BeginMaintenance(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer owner.End()
	if _, err := s.CaptureProviderConfiguration(t.Context()); !errors.Is(err, ErrProviderConfigurationUnavailable) {
		t.Fatal("ordinary capture bypassed active database maintenance", err)
	}
	snapshot, err := s.CaptureProviderConfiguration(owner.Context(t.Context()))
	if err != nil {
		t.Fatal("existing maintenance owner could not capture its view", err)
	}
	snapshot.Clear()
	canceled, cancel := context.WithCancel(owner.Context(t.Context()))
	cancel()
	if snapshot, err := s.CaptureProviderConfiguration(canceled); !errors.Is(err, ErrProviderConfigurationUnavailable) || len(snapshot.Configuration) != 0 {
		t.Fatal("canceled capture published configuration")
	}
}

func TestProviderConfigurationSnapshotDoesNotReleaseDeliberateCoreHold(t *testing.T) {
	s, core := maintenanceFixture(t)
	if err := s.SetCoreMaintenance(t.Context(), "", true); err != nil {
		t.Fatal(err)
	}
	held, err := s.CaptureProviderConfiguration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer held.Clear()
	if !held.Maintenance || held.RuntimeState == "RUNNING" || core.IsRunning() {
		t.Fatal("capture defeated deliberate hold")
	}
}

func TestProviderConfigurationHoldAndResumeKeepExistingDatabaseMaintenanceAdmission(t *testing.T) {
	s, core := maintenanceFixture(t)
	owner, err := dbsqlite.BeginMaintenance(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer owner.End()
	if err := s.SetCoreMaintenance(t.Context(), "", true); !errors.Is(err, dbsqlite.ErrMaintenance) {
		t.Fatal("ordinary maintenance mutation bypassed the database owner", err)
	}
	ctx := owner.Context(t.Context())
	if err := s.SetCoreMaintenance(ctx, "", true); err != nil {
		t.Fatal("owner could not establish its durable hold", err)
	}
	if err := s.SetCoreMaintenance(ctx, "", false); err != nil {
		t.Fatal("owner could not restore prior runtime intent", err)
	}
	if !core.IsRunning() {
		t.Fatal("owner resume did not start the validated core")
	}
	snapshot, err := s.CaptureProviderConfiguration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Clear()
	if snapshot.Maintenance || snapshot.RuntimeState != "RUNNING" || snapshot.RuntimeGeneration == "" {
		t.Fatal("resumed runtime did not publish generation-bound configuration evidence")
	}
	if _, release, err := dbsqlite.AcquireOperation(t.Context()); err == nil {
		release()
		t.Fatal("core resume released someone else's database maintenance authority")
	}
}
