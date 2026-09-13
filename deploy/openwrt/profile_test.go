package openwrt

import (
	"errors"
	"testing"
)

func TestDefaultProfileUsesDeploymentInjectedDatabaseAuthority(t *testing.T) {
	profile := DefaultProfile()
	if err := profile.Validate(); err != nil {
		t.Fatal(err)
	}
	environment, err := profile.DatabaseEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if len(environment) != 1 || environment["SUI_DB_FOLDER"] != DefaultDatabaseFolder {
		t.Fatalf("database environment = %#v", environment)
	}
}

func TestProfileRejectsKnownVolatileDurableDatabaseRoots(t *testing.T) {
	for _, databaseFolder := range []string{"/tmp/solovey-ui/db", "/var/lib/solovey-ui/db"} {
		profile := DefaultProfile()
		profile.DatabaseFolder = databaseFolder
		if err := profile.Validate(); !errors.Is(err, ErrVolatileDurableState) {
			t.Fatalf("Validate(%q) = %v, want volatile-state error", databaseFolder, err)
		}
	}
}

func TestProfileAcceptsExplicitPersistentAlternateDatabaseMount(t *testing.T) {
	profile := DefaultProfile()
	profile.DatabaseFolder = "/mnt/extroot/solovey-ui/db"
	evidence := DurableStateEvidence{MountPath: "/mnt/extroot", Persistent: true, AvailableBytes: 64 << 20, RequiredBytes: 64 << 20}
	if err := profile.ValidateDurableState(evidence); err != nil {
		t.Fatal(err)
	}
	evidence.Persistent = false
	if err := profile.ValidateDurableState(evidence); !errors.Is(err, ErrUnprovenDurableState) {
		t.Fatalf("non-persistent alternate mount = %v", err)
	}
	evidence.Persistent, evidence.AvailableBytes = true, evidence.RequiredBytes-1
	if err := profile.ValidateDurableState(evidence); !errors.Is(err, ErrInsufficientDurableSpace) {
		t.Fatalf("insufficient alternate mount capacity = %v", err)
	}
}

func TestProfileAcceptsProvenPersistentRootOverlayMount(t *testing.T) {
	profile := DefaultProfile()
	evidence := DurableStateEvidence{MountPath: "/", Persistent: true, AvailableBytes: 64 << 20, RequiredBytes: 32 << 20}
	if err := profile.ValidateDurableState(evidence); err != nil {
		t.Fatal(err)
	}
	evidence.Persistent = false
	if err := profile.ValidateDurableState(evidence); !errors.Is(err, ErrUnprovenDurableState) {
		t.Fatalf("unproven root overlay = %v", err)
	}
}
