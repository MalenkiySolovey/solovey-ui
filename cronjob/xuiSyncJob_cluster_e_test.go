package cronjob

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/deposist/s-ui-x/database"
	"github.com/deposist/s-ui-x/database/importxui"
	"github.com/deposist/s-ui-x/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestIssue3SaveSyncProfileDefaultsAndExplicitFalse(t *testing.T) {
	initCronJobTestDB(t)
	sourcePath := createXUISyncSourceDB(t)

	omitted, err := importxui.SaveSyncProfile(importxui.SyncProfileInput{
		Name: "issue3-defaults",
		Source: importxui.SyncProfileSource{
			Type:          "file",
			URL:           sourcePath,
			SourceTrusted: true, // admin-saved file profiles are trusted (cron sourceFromProfile gate)
		},
		Strategy: importxui.StrategyMerge,
	})
	if err != nil {
		t.Fatal(err)
	}
	var storedDefault model.XUISyncProfile
	if err := database.GetDB().First(&storedDefault, omitted.Id).Error; err != nil {
		t.Fatal(err)
	}
	if !storedDefault.OnlyNew {
		t.Fatalf("omitted onlyNew should default to true: %#v", storedDefault)
	}
	if !storedDefault.Enabled {
		t.Fatalf("omitted enabled should default to true: %#v", storedDefault)
	}

	payload, err := json.Marshal(map[string]any{
		"name":       "issue3-explicit-false",
		"sourceType": "file",
		"source": map[string]any{
			"type": "file",
			"url":  sourcePath,
		},
		"strategy": "merge",
		"onlyNew":  false,
		"enabled":  false,
	})
	if err != nil {
		t.Fatal(err)
	}
	var input importxui.SyncProfileInput
	if err := json.Unmarshal(payload, &input); err != nil {
		t.Fatal(err)
	}
	explicitFalse, err := importxui.SaveSyncProfile(input)
	if err != nil {
		t.Fatal(err)
	}
	var storedFalse model.XUISyncProfile
	if err := database.GetDB().First(&storedFalse, explicitFalse.Id).Error; err != nil {
		t.Fatal(err)
	}
	if storedFalse.OnlyNew {
		t.Fatalf("explicit onlyNew:false should persist false: %#v", storedFalse)
	}
	if storedFalse.Enabled {
		t.Fatalf("explicit enabled:false should persist false: %#v", storedFalse)
	}
}

func TestIssue3XUISyncHonorsOnlyNewFalse(t *testing.T) {
	initCronJobTestDB(t)
	sourcePath := createXUISyncSourceDBWithInbound(t)
	if err := database.GetDB().Create(&model.Inbound{
		Type:    "http",
		Tag:     "sync-inbound",
		Addrs:   json.RawMessage(`[]`),
		OutJson: json.RawMessage(`{}`),
		Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":8080}`),
	}).Error; err != nil {
		t.Fatal(err)
	}
	profile, err := importxui.SaveSyncProfile(importxui.SyncProfileInput{
		Name:       "issue3-only-new-false",
		SourceType: "file",
		Source: importxui.SyncProfileSource{
			Type:          "file",
			URL:           sourcePath,
			SourceTrusted: true, // admin-saved file profiles are trusted (cron sourceFromProfile gate)
		},
		Strategy:        importxui.StrategyReplace,
		OnlyNew:         false,
		OnlyNewProvided: true,
		Enabled:         true,
		EnabledProvided: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	job := &XUISyncJob{now: func() time.Time { return time.Unix(1700000500, 0) }}
	if err := job.RunProfile(context.Background(), profile); err != nil {
		t.Fatal(err)
	}

	var stored model.Inbound
	if err := database.GetDB().Where("tag = ?", "sync-inbound").First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Type != "trojan" {
		t.Fatalf("OnlyNew:false should allow replacing conflicts, got inbound type %q", stored.Type)
	}
}

func TestIssue7SaveSyncProfilePersistsImportPolicy(t *testing.T) {
	initCronJobTestDB(t)
	sourcePath := createXUISyncSourceDB(t)

	profile, err := importxui.SaveSyncProfile(importxui.SyncProfileInput{
		Name:       "issue7-policy",
		SourceType: "file",
		Source: importxui.SyncProfileSource{
			Type:          "file",
			URL:           sourcePath,
			SourceTrusted: true, // admin-saved file profiles are trusted (cron sourceFromProfile gate)
		},
		Strategy:        importxui.StrategyMerge,
		OnlyNew:         true,
		OnlyNewProvided: true,
		IncludeSettings: true,
		IncludeHistory:  true,
		IncludeRouting:  true,
		AdminMode:       string(importxui.AdminModeResetRequired),
		Enabled:         true,
		EnabledProvided: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	var stored model.XUISyncProfile
	if err := database.GetDB().First(&stored, profile.Id).Error; err != nil {
		t.Fatal(err)
	}
	if !stored.IncludeSettings || !stored.IncludeHistory || !stored.IncludeRouting {
		t.Fatalf("include policy fields were not persisted: %#v", stored)
	}
	if stored.AdminMode != string(importxui.AdminModeResetRequired) {
		t.Fatalf("adminMode=%q, want %q", stored.AdminMode, importxui.AdminModeResetRequired)
	}
}

func TestIssue7XUISyncPassesProfileImportPolicy(t *testing.T) {
	initCronJobTestDB(t)
	sourcePath := createXUISyncSourceDBWithPolicy(t)
	profile, err := importxui.SaveSyncProfile(importxui.SyncProfileInput{
		Name:       "issue7-cron-policy",
		SourceType: "file",
		Source: importxui.SyncProfileSource{
			Type:          "file",
			URL:           sourcePath,
			SourceTrusted: true, // admin-saved file profiles are trusted (cron sourceFromProfile gate)
		},
		Strategy:        importxui.StrategyMerge,
		OnlyNew:         false,
		OnlyNewProvided: true,
		IncludeSettings: true,
		IncludeHistory:  true,
		IncludeRouting:  true,
		AdminMode:       string(importxui.AdminModeResetRequired),
		Enabled:         true,
		EnabledProvided: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	job := &XUISyncJob{now: func() time.Time { return time.Unix(1700000600, 0) }}
	if err := job.RunProfile(context.Background(), profile); err != nil {
		t.Fatal(err)
	}

	var webPort model.Setting
	if err := database.GetDB().Where("key = ?", "webPort").First(&webPort).Error; err != nil {
		t.Fatal(err)
	}
	if webPort.Value != "2095" {
		t.Fatalf("includeSettings did not import webPort: %#v", webPort)
	}
	var statsCount int64
	if err := database.GetDB().Model(&model.Stats{}).Count(&statsCount).Error; err != nil {
		t.Fatal(err)
	}
	if statsCount == 0 {
		t.Fatal("includeHistory did not import any stats")
	}
	var liveConfig model.Setting
	if err := database.GetDB().Where("key = ?", "config").First(&liveConfig).Error; err != nil {
		t.Fatal(err)
	}
	// The source routing rule (domain geosite:google -> direct) must be merged
	// into the live config as a geosite-google rule set.
	if !strings.Contains(liveConfig.Value, "geosite-google") {
		t.Fatalf("includeRouting did not merge the routing rule into the live config: %s", liveConfig.Value)
	}
	var admin model.User
	if err := database.GetDB().Where("username = ?", "xui-admin").First(&admin).Error; err != nil {
		t.Fatal(err)
	}
	if !admin.ForcePasswordReset {
		t.Fatalf("profile adminMode reset_required was not applied: %#v", admin)
	}
}

func createXUISyncSourceDBWithInbound(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "x-ui.db")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err == nil {
		defer sqlDB.Close()
	}
	if err := db.Exec(`CREATE TABLE inbounds (
		id integer primary key,
		user_id integer,
		up integer,
		down integer,
		total integer,
		all_time integer,
		remark text,
		enable integer,
		expiry_time integer,
		traffic_reset text,
		last_traffic_reset_time integer,
		listen text,
		port integer,
		protocol text,
		settings text,
		stream_settings text,
		tag text,
		sniffing text
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE client_traffics (
		id integer primary key,
		inbound_id integer,
		enable integer,
		email text,
		up integer,
		down integer,
		all_time integer,
		expiry_time integer,
		total integer,
		reset integer,
		last_online integer
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO inbounds (
		id, user_id, up, down, total, all_time, remark, enable,
		expiry_time, traffic_reset, last_traffic_reset_time, listen,
		port, protocol, settings, stream_settings, tag, sniffing
	) VALUES (
		1, 0, 0, 0, 0, 0, 'sync-inbound', 1,
		0, '', 0, '0.0.0.0',
		443, 'trojan', '{}', '{}', 'sync-inbound', '{}'
	)`).Error; err != nil {
		t.Fatal(err)
	}
	return path
}

func createXUISyncSourceDBWithPolicy(t *testing.T) string {
	t.Helper()
	path := createXUISyncSourceDBWithInbound(t)
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err == nil {
		defer sqlDB.Close()
	}
	if err := db.Exec(`CREATE TABLE settings (
		id integer primary key,
		key text,
		value text
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO settings(id, key, value) VALUES
		(1, 'webPort', '2095'),
		(2, 'xrayConfig', '{"routing":{"rules":[{"outboundTag":"direct","domain":["geosite:google"]}]}}')
	`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE users (
		id integer primary key,
		username text,
		password text
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO users(id, username, password) VALUES(1, 'xui-admin', 'source-secret')").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE outbound_traffics (
		id integer primary key,
		tag text,
		up integer,
		down integer
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO outbound_traffics(id, tag, up, down) VALUES(1, 'direct', 10, 20)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO client_traffics(id, inbound_id, enable, email, up, down, all_time, expiry_time, total, reset, last_online) VALUES(1, 1, 1, 'alice', 30, 40, 0, 0, 0, 0, 0)").Error; err != nil {
		t.Fatal(err)
	}
	return path
}
