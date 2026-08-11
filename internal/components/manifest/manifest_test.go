package manifest

import (
	"strings"
	"testing"
)

func TestManifestValidate(t *testing.T) {
	valid := Manifest{
		ID:          "remote-subscriptions",
		Name:        "Remote Subscriptions",
		Delivery:    DeliveryInProcess,
		TokenScopes: []string{"server-protection:read", "flat-scope"},
		Frontend: Frontend{
			Entries: []string{
				"frontend/views/RemoteOutboundSubscriptions.vue",
				"frontend/locales/en/telegram.ts",
			},
		},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid manifest failed: %v", err)
	}

	for _, row := range []Manifest{
		{ID: "Bad_ID", Name: "Bad", Delivery: DeliveryInProcess},
		{ID: "bad", Delivery: DeliveryInProcess},
		{ID: "bad", Name: "Bad"},
		{ID: "bad", Name: "Bad", Delivery: Delivery("sidecar")},
		{ID: "bad", Name: "Bad", Delivery: DeliveryInProcess, Frontend: Frontend{Entries: []string{""}}},
		{ID: "bad", Name: "Bad", Delivery: DeliveryInProcess, Frontend: Frontend{Entries: []string{"../views/Bad.vue"}}},
		{ID: "bad", Name: "Bad", Delivery: DeliveryInProcess, Frontend: Frontend{Entries: []string{"src/views/Bad.js"}}},
		{ID: "bad", Name: "Bad", Delivery: DeliveryInProcess, Frontend: Frontend{Entries: []string{"assets/Bad.vue"}}},
		{ID: "bad", Name: "Bad", Delivery: DeliveryInProcess, TokenScopes: []string{"bad::scope"}},
		{ID: "bad", Name: "Bad", Delivery: DeliveryInProcess, TokenScopes: []string{"Bad:scope"}},
	} {
		if err := row.Validate(); err == nil {
			t.Fatalf("manifest %#v unexpectedly validated", row)
		}
	}
}

func TestDurableResourceManifestV1NormalizesChecksumsAndRejectsDrift(t *testing.T) {
	item := Manifest{ID: "owner-one", Name: "Owner One", Version: "1", Delivery: DeliveryInProcess,
		Database: Database{Tables: []string{"owner_rows"}, Settings: []string{"owner.setting"}, Secrets: []string{"owner.secret"},
			Files: []DurableFileResource{{Path: ".runtime/owner-one", BackupClass: FileBackupExcluded,
				Redaction: RedactionSensitive, Portability: PortabilityHostBound}}}}
	item = item.Normalized()
	if err := item.Validate(); err != nil {
		t.Fatal(err)
	}
	if item.Database.Schema != DurableResourceSchemaV1 || len(item.Database.SchemaChecksum) != 64 || len(item.Database.MigrationChecksum) != 64 ||
		item.Database.SchemaChecksum == item.Database.MigrationChecksum {
		t.Fatalf("normalized durable manifest is incomplete: %#v", item.Database)
	}
	first := item.Database.SchemaChecksum
	reordered := item
	reordered.Database.Tables = []string{"owner_rows"}
	if got := reordered.Normalized().Database.SchemaChecksum; got != first {
		t.Fatalf("canonical checksum changed: got %s want %s", got, first)
	}
	drifted := item
	drifted.Database.Settings = append(drifted.Database.Settings, "owner.other")
	if err := drifted.Validate(); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("resource drift was accepted: %v", err)
	}
	unsafe := item
	unsafe.Database.SchemaChecksum, unsafe.Database.MigrationChecksum = "", ""
	unsafe.Database.Files[0].Path = "../escape"
	if err := unsafe.Validate(); err == nil {
		t.Fatal("unsafe durable path was accepted")
	}
}
