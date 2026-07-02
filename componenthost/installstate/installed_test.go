package installstate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
)

func TestInstalledIDsEmptyWhenMetadataMissing(t *testing.T) {
	t.Setenv(InstalledFileEnv, filepath.Join(t.TempDir(), "missing.json"))

	ids, err := InstalledIDs([]manifest.Manifest{
		{ID: "telegram", Delivery: manifest.DeliveryInProcess},
		{ID: "remote-outbound-subscriptions", Delivery: manifest.DeliveryInProcess},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Fatalf("missing metadata should not install components implicitly: %#v", ids)
	}
}

func TestInstalledIDsUsesMetadataAsSourceOfTruth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "installed.json")
	t.Setenv(InstalledFileEnv, path)
	if err := os.WriteFile(path, []byte(`{
		"version": 1,
		"binary": "full",
		"components": [
			{"id": "telegram", "delivery": "in-process", "installed": true},
			{"id": "remote-outbound-subscriptions", "delivery": "in-process", "installed": false}
		]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	ids, err := InstalledIDs([]manifest.Manifest{
		{ID: "telegram", Delivery: manifest.DeliveryInProcess},
		{ID: "remote-outbound-subscriptions", Delivery: manifest.DeliveryInProcess},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ids["telegram"]; !ok {
		t.Fatal("telegram should be installed")
	}
	if _, ok := ids["remote-outbound-subscriptions"]; ok {
		t.Fatal("remote-outbound-subscriptions should not be installed")
	}
}

func TestLoadPreservesProfileAndBinary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "installed.json")
	if err := os.WriteFile(path, []byte(`{
		"version": 1,
		"profile": "custom",
		"binary": "full",
		"components": [
			{"id": "telegram", "delivery": "in-process", "installed": true}
		]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	metadata, exists, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("metadata should exist")
	}
	if metadata.Profile != "custom" || metadata.Binary != "full" {
		t.Fatalf("metadata profile/binary = %q/%q, want custom/full", metadata.Profile, metadata.Binary)
	}
}

func TestInstalledIDsIgnoresUnavailableInstalledComponent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "installed.json")
	t.Setenv(InstalledFileEnv, path)
	if err := os.WriteFile(path, []byte(`{
		"version": 1,
		"components": [
			{"id": "missing-component", "delivery": "in-process", "installed": true}
		]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	ids, err := InstalledIDs([]manifest.Manifest{{ID: "telegram", Delivery: manifest.DeliveryInProcess}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ids["missing-component"]; ok {
		t.Fatal("unavailable component must not be active in this binary")
	}
}

func TestSetInstalledCreatesExplicitMetadataFromEmptyState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "components", "installed.json")
	t.Setenv(InstalledFileEnv, path)
	available := []manifest.Manifest{
		{ID: "paid-subscriptions", Name: "Paid subscriptions", Version: "1", Delivery: manifest.DeliveryInProcess},
		{ID: "telegram", Name: "Telegram", Version: "1", Delivery: manifest.DeliveryInProcess},
	}

	metadata, err := SetInstalled(path, available, "telegram", false)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Version != 1 {
		t.Fatalf("version = %d, want 1", metadata.Version)
	}

	ids, err := InstalledIDs(available)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ids["telegram"]; ok {
		t.Fatal("telegram should be explicitly removed")
	}
	if _, ok := ids["paid-subscriptions"]; ok {
		t.Fatal("other components must not be installed implicitly when metadata is created")
	}
}

func TestSetInstalledAddsMissingAvailableComponent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "installed.json")
	t.Setenv(InstalledFileEnv, path)
	if err := os.WriteFile(path, []byte(`{
		"version": 1,
		"components": [
			{"id": "telegram", "delivery": "in-process", "installed": false}
		]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	available := []manifest.Manifest{
		{ID: "paid-subscriptions", Name: "Paid subscriptions", Version: "1", Delivery: manifest.DeliveryInProcess},
		{ID: "telegram", Name: "Telegram", Version: "1", Delivery: manifest.DeliveryInProcess},
	}

	if _, err := SetInstalled(path, available, "paid-subscriptions", true); err != nil {
		t.Fatal(err)
	}
	ids, err := InstalledIDs(available)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ids["paid-subscriptions"]; !ok {
		t.Fatal("paid-subscriptions should be installed")
	}
	if _, ok := ids["telegram"]; ok {
		t.Fatal("telegram should keep its explicit removed state")
	}
}

func TestSetInstalledPreservesUnavailableInstalledComponents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "installed.json")
	t.Setenv(InstalledFileEnv, path)
	if err := os.WriteFile(path, []byte(`{
		"version": 1,
		"components": [
			{"id": "old-component", "delivery": "in-process", "installed": true}
		]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	available := []manifest.Manifest{{ID: "telegram", Name: "Telegram", Version: "1", Delivery: manifest.DeliveryInProcess}}

	metadata, err := SetInstalled(path, available, "telegram", true)
	if err != nil {
		t.Fatal(err)
	}

	if !containsInstalled(metadata.Components, "old-component") {
		t.Fatalf("unavailable component metadata should be preserved: %#v", metadata.Components)
	}
	if !containsInstalled(metadata.Components, "telegram") {
		t.Fatalf("target component should be installed: %#v", metadata.Components)
	}
}

func TestSetInstalledRejectsUnavailableComponent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "installed.json")
	available := []manifest.Manifest{{ID: "telegram", Name: "Telegram", Version: "1", Delivery: manifest.DeliveryInProcess}}

	if _, err := SetInstalled(path, available, "missing-component", true); err == nil {
		t.Fatal("expected unavailable component install to fail")
	}
	if _, exists, err := Load(path); err != nil || exists {
		t.Fatalf("metadata should not be written on failure, exists=%v err=%v", exists, err)
	}
}

func containsInstalled(components []InstalledComponent, id string) bool {
	for _, component := range components {
		if component.ID == id && component.Installed {
			return true
		}
	}
	return false
}
