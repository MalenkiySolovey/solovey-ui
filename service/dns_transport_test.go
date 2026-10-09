package service

import (
	"testing"

	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

func TestSelectedDNSSaveUsesCompletedDBReferencesAndPreservesPreimage(t *testing.T) {
	settings := initSettingTestDB(t)
	store := NewSingBoxBaseConfigStore(settings)
	if err := store.Set(`{"dns":{"servers":[{"type":"local","tag":"local"}]}}`); err != nil {
		t.Fatal(err)
	}
	before, err := store.Get()
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{
		`{"dns":{"servers":[{"type":"resolved","tag":"resolver","service":"missing"}]}}`,
		`{"dns":{"servers":[{"type":"mdns","tag":"multicast","interface":"solovey-phase-owned-missing-interface"}]}}`,
	} {
		projection, err := NewSingBoxConfigBuilder(nil).BuildCandidateProjectionFromDB(dbsqlite.DB(), source, false)
		if err == nil || len(projection.DNSCompatibility) == 0 {
			t.Fatalf("runtime preview lost selected DNS rejection: %v; DNS findings=%d", err, len(projection.DNSCompatibility))
		}
		if err := store.Set(source); err == nil {
			t.Fatal("base save bypassed complete reference/environment preflight")
		}
		after, _ := store.Get()
		if after != before {
			t.Fatal("rejected DNS save changed durable source")
		}
	}
	if err := store.Set(`{"dns":{"servers":[{"type":"local","tag":"corrected"}]}}`); err != nil {
		t.Fatal("explicit correction could not be saved")
	}
}
