package deploymentbroker

import "testing"

func TestTransitionAuthorityProjectionAllowsOnlyOwnedPartialStates(t *testing.T) {
	for _, unit := range []transitionPart{transitionOriginal, transitionTarget} {
		for _, marker := range []transitionPart{transitionOriginal, transitionTarget} {
			if !transitionAuthoritySafe(unit, marker) {
				t.Fatalf("owned partial transition rejected: unit=%d marker=%d", unit, marker)
			}
		}
	}
	for _, values := range [][2]transitionPart{{transitionForeign, transitionOriginal}, {transitionOriginal, transitionForeign}, {transitionForeign, transitionForeign}} {
		if transitionAuthoritySafe(values[0], values[1]) {
			t.Fatalf("foreign transition accepted: %v", values)
		}
	}
}

func TestStagedDataProjectionRejectsForeignAndTamperedAuthority(t *testing.T) {
	marker := ".solovey-migration-" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	valid := []stagedDataFact{
		{Name: "solovey-ui.db", UID: 1001, GID: 1001, Mode: 0o600, Regular: true},
		{Name: marker, UID: 0, GID: 0, Mode: 0o400, Regular: true, MarkerContentMatches: true},
	}
	if err := validateStagedDataFacts(valid, marker, 1001, 1001, true); err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func([]stagedDataFact) []stagedDataFact{
		"foreign name":   func(v []stagedDataFact) []stagedDataFact { v[0].Name = "foreign.db"; return v },
		"wrong owner":    func(v []stagedDataFact) []stagedDataFact { v[0].UID = 0; return v },
		"broad mode":     func(v []stagedDataFact) []stagedDataFact { v[0].Mode = 0o640; return v },
		"symlink":        func(v []stagedDataFact) []stagedDataFact { v[0].Regular = false; return v },
		"marker content": func(v []stagedDataFact) []stagedDataFact { v[1].MarkerContentMatches = false; return v },
		"marker mode":    func(v []stagedDataFact) []stagedDataFact { v[1].Mode = 0o600; return v },
		"missing marker": func(v []stagedDataFact) []stagedDataFact { return v[:1] },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			copy := append([]stagedDataFact(nil), valid...)
			if err := validateStagedDataFacts(mutate(copy), marker, 1001, 1001, true); err == nil {
				t.Fatal("unsafe staged authority accepted")
			}
		})
	}
	partial := valid[:1]
	if err := validateStagedDataFacts(partial, marker, 1001, 1001, false); err != nil {
		t.Fatalf("pre-switch partial staging must remain exactly cleanable: %v", err)
	}
}

func BenchmarkMigrationPlanningProjection(b *testing.B) {
	marker := ".solovey-migration-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	facts := []stagedDataFact{
		{Name: "solovey-ui.db", UID: 1001, GID: 1001, Mode: 0o600, Regular: true},
		{Name: "solovey-ui.db-wal", UID: 1001, GID: 1001, Mode: 0o600, Regular: true},
		{Name: marker, UID: 0, GID: 0, Mode: 0o400, Regular: true, MarkerContentMatches: true},
	}
	b.ReportAllocs()
	for b.Loop() {
		if !transitionAuthoritySafe(transitionOriginal, transitionTarget) {
			b.Fatal("owned transition changed")
		}
		if err := validateStagedDataFacts(facts, marker, 1001, 1001, true); err != nil {
			b.Fatal(err)
		}
	}
}
