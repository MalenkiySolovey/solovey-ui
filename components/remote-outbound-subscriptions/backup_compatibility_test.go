//go:build !minimal

package remoteoutboundsubscriptions

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	remote "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/domain"
	remotesettings "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/internal/settings"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	dbbackup "github.com/MalenkiySolovey/solovey-ui/database/backup"
	"github.com/MalenkiySolovey/solovey-ui/database/migration"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

type subscriptionBackupFixture struct {
	SourceCommit string
	BackupSHA256 string
	StateDigests map[string]string
}

// This opt-in generator runs against the previously accepted source. Ordinary
// tests consume its immutable fixture rather than regenerating it with new code.
func TestExportSubscriptionCompatibilityFixture(t *testing.T) {
	output := os.Getenv("SOLOVEY_SUBSCRIPTION_COMPAT_EXPORT")
	if output == "" {
		t.Skip("previous-source fixture generator")
	}
	prepareSubscriptionCompatibilityDB(t)
	seedSubscriptionCompatibilityDB(t)
	if err := migration.EnsureCurrentSchemaJournal(dbsqlite.DB(), true); err != nil {
		t.Fatal(err)
	}
	raw, err := dbbackup.Export("")
	if err != nil {
		t.Fatal(err)
	}
	fixture := subscriptionBackupFixture{SourceCommit: os.Getenv("SOLOVEY_SUBSCRIPTION_COMPAT_SOURCE"), BackupSHA256: fmt.Sprintf("%x", sha256.Sum256(raw)), StateDigests: subscriptionCompatibilityDigests(t)}
	if len(fixture.SourceCommit) != 40 {
		t.Fatal("exact previous Product source commit required")
	}
	writeSubscriptionFixture(t, output, raw, fixture)
}

func TestPreviousSubscriptionBackupRestoresExactOwnedState(t *testing.T) {
	prepareSubscriptionCompatibilityDB(t)
	input := os.Getenv("SOLOVEY_SUBSCRIPTION_COMPAT_IMPORT")
	if input == "" {
		input = filepath.Join("testdata", "previous-subscription-backup")
	}
	compressed, err := os.ReadFile(input + ".db.gz")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(io.LimitReader(reader, 4<<20))
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	metadata, err := os.ReadFile(input + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture subscriptionBackupFixture
	if err := json.Unmarshal(metadata, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.SourceCommit) != 40 || fmt.Sprintf("%x", sha256.Sum256(raw)) != fixture.BackupSHA256 {
		t.Fatal("previous-source backup identity mismatch")
	}
	dbbackup.SetSendSighupHook(func() error { return nil })
	t.Cleanup(func() { dbbackup.SetSendSighupHook(nil) })
	if err := dbbackup.Restore(subscriptionCompatibilityFile{bytes.NewReader(raw)}); err != nil {
		t.Fatal(err)
	}
	actual := subscriptionCompatibilityDigests(t)
	if !maps.Equal(actual, fixture.StateDigests) {
		t.Fatalf("restored source, relationship, settings or schema changed: got %v want %v", actual, fixture.StateDigests)
	}
	// Restore reopens the database. Its current handle must remain usable, and a
	// new export must retain all source state for a source-only Product rollback.
	sqlDB, err := dbsqlite.DB().DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Ping(); err != nil {
		t.Fatal(err)
	}
	if output := os.Getenv("SOLOVEY_SUBSCRIPTION_COMPAT_REEXPORT"); output != "" {
		reexport, err := dbbackup.Export("")
		if err != nil {
			t.Fatal(err)
		}
		fixture.BackupSHA256 = fmt.Sprintf("%x", sha256.Sum256(reexport))
		fixture.SourceCommit = os.Getenv("SOLOVEY_SUBSCRIPTION_COMPAT_SOURCE")
		if len(fixture.SourceCommit) != 40 {
			t.Fatal("exact integrated source commit required")
		}
		writeSubscriptionFixture(t, output, reexport, fixture)
	}
	t.Logf("accepted backup source=%s state=%v", fixture.SourceCommit, actual)
}

type subscriptionCompatibilityFile struct{ *bytes.Reader }

func (subscriptionCompatibilityFile) Close() error { return nil }

func prepareSubscriptionCompatibilityDB(t *testing.T) {
	t.Helper()
	t.Setenv("SUI_DB_FOLDER", t.TempDir())
	if err := dbsqlite.Init(configstorage.GetDBPath()); err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(t.TempDir(), "installed.json")
	t.Setenv(installstate.InstalledFileEnv, installed)
	if err := installstate.Store(installed, installstate.Metadata{Version: 1, Components: []installstate.InstalledComponent{{ID: id, Delivery: componentManifest.Delivery, Installed: true}}}); err != nil {
		t.Fatal(err)
	}
	unregisterRuntimeHooks()
	registerRuntimeHooks()
	t.Cleanup(unregisterRuntimeHooks)
	if err := (component{}).Migrate(context.Background(), lifecycle.Context{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if current := dbsqlite.DB(); current != nil {
			if handle, err := current.DB(); err == nil {
				_ = handle.Close()
			}
		}
	})
}

func seedSubscriptionCompatibilityDB(t *testing.T) {
	t.Helper()
	db := dbsqlite.DB()
	const stamp int64 = 1700000000
	sources := []remote.RemoteOutboundSubscription{
		{Id: 41, SortOrder: 1, Name: "Accepted remote source", Url: "https://provider.example/subscription", SourceType: "remote", SourceHeaders: "{\"Authorization\":\"Bearer fixture-only\"}", Enabled: true, AutoUpdate: true, UpdateInterval: 3600, TagPrefix: "accepted-", LastUpdated: stamp, LastError: "redacted fetch failure", CanonicalSnapshot: json.RawMessage("{\"accepted\":true}"), CollectionSnapshot: json.RawMessage("{\"representations\":[\"xray\",\"mihomo\"]}"), CreatedAt: stamp, UpdatedAt: stamp},
		{Id: 42, SortOrder: 2, Name: "Accepted inline source", Url: "inline://source", SourceType: "inline", SourceContent: "vless://fixture-only@edge.example:443#inline", Enabled: false, TagPrefix: "inline-", LastUpdated: stamp, CreatedAt: stamp, UpdatedAt: stamp},
		{Id: 43, SortOrder: 3, Name: "Accepted local file source", Url: "file:///approved/subscription.txt", SourceType: "file", SourcePath: "/approved/subscription.txt", Enabled: false, TagPrefix: "file-", LastUpdated: stamp, CreatedAt: stamp, UpdatedAt: stamp},
	}
	if err := db.Create(&sources).Error; err != nil {
		t.Fatal(err)
	}
	outbound := model.Outbound{Id: 81, SortOrder: 2, Type: "socks", Tag: "accepted-node", Options: json.RawMessage("{\"server\":\"edge.example\",\"server_port\":1080,\"username\":\"fixture-only\",\"password\":\"fixture-only\",\"bind_interface\":\"local-user-choice\"}")}
	if err := db.Create(&outbound).Error; err != nil {
		t.Fatal(err)
	}
	groups := []remote.RemoteOutboundGroup{{Id: 51, SubscriptionId: 41, SortOrder: 1, Name: "Primary", Enabled: true, OutboundEnabled: true, CreatedAt: stamp, UpdatedAt: stamp}, {Id: 52, SubscriptionId: 41, SortOrder: 2, Name: "Client membership", Enabled: false, CreatedAt: stamp, UpdatedAt: stamp}}
	if err := db.Create(&groups).Error; err != nil {
		t.Fatal(err)
	}
	connection := remote.RemoteOutboundConnection{Id: 61, SubscriptionId: 41, GroupId: 51, SortOrder: 1, Name: "Accepted endpoint", SourceKey: "stable-source-key", Type: "socks", OutboundTag: outbound.Tag, Enabled: true, Synced: true, OutboundId: &outbound.Id, Options: json.RawMessage("{\"server\":\"edge.example\",\"server_port\":1080}"), Canonical: json.RawMessage("{\"source_identity\":\"stable-endpoint\"}"), LastSeen: stamp, CreatedAt: stamp, UpdatedAt: stamp}
	if err := db.Create(&connection).Error; err != nil {
		t.Fatal(err)
	}
	memberships := []remote.RemoteOutboundGroupConnection{{Id: 71, GroupId: 51, ConnectionId: 61, CreatedAt: stamp}, {Id: 72, GroupId: 52, ConnectionId: 61, CreatedAt: stamp}}
	if err := db.Create(&memberships).Error; err != nil {
		t.Fatal(err)
	}
	settings := remotesettings.Defaults()
	settings[remotesettings.GroupAdaptationKey] = "failover"
	for key, value := range settings {
		if err := db.Create(&model.Setting{Key: key, Value: value}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&model.ComponentMigration{Id: 91, ComponentID: id, Name: componentManifest.Name, Version: "3", Delivery: string(componentManifest.Delivery), AppliedAt: stamp}).Error; err != nil {
		t.Fatal(err)
	}
}

func subscriptionCompatibilityDigests(t *testing.T) map[string]string {
	t.Helper()
	queries := map[string]string{
		"remote_outbound_subscriptions":     "SELECT * FROM remote_outbound_subscriptions ORDER BY id",
		"remote_outbound_groups":            "SELECT * FROM remote_outbound_groups ORDER BY id",
		"remote_outbound_connections":       "SELECT * FROM remote_outbound_connections ORDER BY id",
		"remote_outbound_group_connections": "SELECT * FROM remote_outbound_group_connections ORDER BY id",
		"linked_outbounds":                  "SELECT * FROM outbounds WHERE id = 81",
		"owned_settings":                    "SELECT key,value FROM settings WHERE key IN ('subRemoteConversionPolicy','subRemoteGroupAdaptation') ORDER BY key",
		"migration":                         "SELECT * FROM component_migrations WHERE component_id = 'remote-outbound-subscriptions' ORDER BY id",
		"schema":                            "SELECT name,sql FROM sqlite_master WHERE name LIKE '%remote_outbound%' ORDER BY name",
	}
	result := map[string]string{}
	for label, query := range queries {
		var rows []map[string]any
		if err := dbsqlite.DB().Raw(query).Scan(&rows).Error; err != nil {
			t.Fatal(err)
		}
		if len(rows) == 0 {
			t.Fatalf("compatibility fixture omitted %s", label)
		}
		data, err := json.Marshal(rows)
		if err != nil {
			t.Fatal(err)
		}
		result[label] = fmt.Sprintf("%x", sha256.Sum256(data))
	}
	return result
}

func writeSubscriptionFixture(t *testing.T, output string, raw []byte, fixture subscriptionBackupFixture) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
		t.Fatal(err)
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output+".db.gz", compressed.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(fixture, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output+".json", append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}
