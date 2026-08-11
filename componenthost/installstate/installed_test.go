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
		{ID: "fixture-beta", Delivery: manifest.DeliveryInProcess},
		{ID: "fixture-remote", Delivery: manifest.DeliveryInProcess},
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
			{"id": "fixture-beta", "delivery": "in-process", "installed": true},
			{"id": "fixture-remote", "delivery": "in-process", "installed": false}
		]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	ids, err := InstalledIDs([]manifest.Manifest{
		{ID: "fixture-beta", Delivery: manifest.DeliveryInProcess},
		{ID: "fixture-remote", Delivery: manifest.DeliveryInProcess},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ids["fixture-beta"]; !ok {
		t.Fatal("fixture-beta should be installed")
	}
	if _, ok := ids["fixture-remote"]; ok {
		t.Fatal("fixture-remote should not be installed")
	}
}

func TestLoadPreservesProfileAndBinary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "installed.json")
	if err := os.WriteFile(path, []byte(`{
		"version": 1,
		"profile": "custom",
		"binary": "full",
		"components": [
			{"id": "fixture-beta", "delivery": "in-process", "installed": true}
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

	ids, err := InstalledIDs([]manifest.Manifest{{ID: "fixture-beta", Delivery: manifest.DeliveryInProcess}})
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
		{ID: "fixture-alpha", Name: "Fixture alpha", Version: "1", Delivery: manifest.DeliveryInProcess},
		{ID: "fixture-beta", Name: "Fixture Beta", Version: "1", Delivery: manifest.DeliveryInProcess},
	}

	metadata, err := SetInstalled(path, available, "fixture-beta", false)
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
	if _, ok := ids["fixture-beta"]; ok {
		t.Fatal("fixture-beta should be explicitly removed")
	}
	if _, ok := ids["fixture-alpha"]; ok {
		t.Fatal("other components must not be installed implicitly when metadata is created")
	}
}

func TestSetInstalledAddsMissingAvailableComponent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "installed.json")
	t.Setenv(InstalledFileEnv, path)
	if err := os.WriteFile(path, []byte(`{
		"version": 1,
		"components": [
			{"id": "fixture-beta", "delivery": "in-process", "installed": false}
		]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	available := []manifest.Manifest{
		{ID: "fixture-alpha", Name: "Fixture alpha", Version: "1", Delivery: manifest.DeliveryInProcess},
		{ID: "fixture-beta", Name: "Fixture Beta", Version: "1", Delivery: manifest.DeliveryInProcess},
	}

	if _, err := SetInstalled(path, available, "fixture-alpha", true); err != nil {
		t.Fatal(err)
	}
	ids, err := InstalledIDs(available)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ids["fixture-alpha"]; !ok {
		t.Fatal("fixture-alpha should be installed")
	}
	if _, ok := ids["fixture-beta"]; ok {
		t.Fatal("fixture-beta should keep its explicit removed state")
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
	available := []manifest.Manifest{{ID: "fixture-beta", Name: "Fixture Beta", Version: "1", Delivery: manifest.DeliveryInProcess}}

	metadata, err := SetInstalled(path, available, "fixture-beta", true)
	if err != nil {
		t.Fatal(err)
	}

	if !containsInstalled(metadata.Components, "old-component") {
		t.Fatalf("unavailable component metadata should be preserved: %#v", metadata.Components)
	}
	if !containsInstalled(metadata.Components, "fixture-beta") {
		t.Fatalf("target component should be installed: %#v", metadata.Components)
	}
}

func TestSetInstalledRejectsUnavailableComponent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "installed.json")
	available := []manifest.Manifest{{ID: "fixture-beta", Name: "Fixture Beta", Version: "1", Delivery: manifest.DeliveryInProcess}}

	if _, err := SetInstalled(path, available, "missing-component", true); err == nil {
		t.Fatal("expected unavailable component install to fail")
	}
	if _, exists, err := Load(path); err != nil || exists {
		t.Fatalf("metadata should not be written on failure, exists=%v err=%v", exists, err)
	}
}

func TestLoadRejectsUnknownFutureAndOversizedInstalledAuthority(t *testing.T) {
	for name, payload := range map[string][]byte{
		"unknown field":  []byte(`{"version":1,"components":[],"rawPath":"C:/unsafe"}`),
		"future version": []byte(`{"version":2,"components":[]}`),
		"duplicate":      []byte(`{"version":1,"components":[{"id":"fixture-beta","installed":true},{"id":"fixture-beta","installed":true}]}`),
		"delivery":       []byte(`{"version":1,"components":[{"id":"fixture-beta","delivery":"external","installed":true}]}`),
		"oversized":      append([]byte(`{"version":1,"components":[],"padding":"`), append(make([]byte, MaxInstalledMetadataBytes), []byte(`"}`)...)...),
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "installed.json")
			if err := os.WriteFile(path, payload, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, exists, err := Load(path); !exists || err == nil {
				t.Fatalf("malformed installed authority exists=%v err=%v", exists, err)
			}
		})
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
