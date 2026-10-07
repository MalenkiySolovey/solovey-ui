//go:build with_quic

package singboxconfig_test

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	singboxconfig "github.com/MalenkiySolovey/solovey-ui/internal/singbox/config"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/validation"
	subformats "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/formats"
)

//go:embed testdata/accepted-1.13-http/*.json
var acceptedHTTPFixtures embed.FS

func TestAcceptedHTTPFixtureReplay(t *testing.T) {
	raw, err := acceptedHTTPFixtures.ReadFile("testdata/accepted-1.13-http/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest []struct {
		ID     string `json:"fixture_id"`
		SHA    string `json:"config_sha256"`
		Commit string `json:"solovey_commit"`
		Core   string `json:"core_version"`
	}
	if json.Unmarshal(raw, &manifest) != nil || len(manifest) != 11 {
		t.Fatal("accepted capture manifest incomplete")
	}
	manual := map[string]bool{"remote-dual-client": true, "multiple-grouped-set": true, "inverted-grouped-set": true, "logical-grouped-set": true, "local-grouped-unpinned": true, "subscription-custom": true}
	var report []map[string]any
	for _, fixture := range manifest {
		t.Run(fixture.ID, func(t *testing.T) {
			if fixture.Commit != "3523eeea9f91c091ab3f0fbdb94c3955e37798b4" || fixture.Core != "v1.13.18" {
				t.Fatal("wrong baseline identity")
			}
			source, err := acceptedHTTPFixtures.ReadFile("testdata/accepted-1.13-http/" + fixture.ID + ".json")
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(source)
			if hex.EncodeToString(digest[:]) != fixture.SHA {
				t.Fatal("captured bytes changed")
			}
			var candidate []byte
			var outcome string
			var findings []diagnostics.Finding
			if fixture.ID == "subscription-custom" {
				prepared, prepareErr := subformats.PrepareJSONExtensionUpgrade(source)
				candidate, outcome, findings, err = prepared.Candidate, prepared.Outcome, prepared.Findings, prepareErr
			} else {
				prepared, prepareErr := singboxconfig.PrepareHTTPUpgrade(source)
				candidate, outcome, findings, err = prepared.Candidate, prepared.Outcome, prepared.Findings, prepareErr
			}
			if (err != nil) != manual[fixture.ID] {
				t.Fatalf("unexpected bounded replay outcome: %v", err)
			}
			if manual[fixture.ID] {
				if outcome != diagnostics.ManualRequired || !bytes.Equal(candidate, source) || len(findings) == 0 {
					t.Fatal("manual source was lost or activated")
				}
			} else {
				if err := validation.ValidateConfig(candidate); err != nil {
					t.Fatalf("pinned complete candidate rejected: %v", err)
				}
				second, err := singboxconfig.PrepareHTTPUpgrade(candidate)
				if err != nil || !bytes.Equal(candidate, second.Candidate) {
					t.Fatal("retry changed candidate")
				}
			}
			report = append(report, map[string]any{"fixture_id": fixture.ID, "outcome": outcome, "source_sha256": fixture.SHA, "source_preserved": manual[fixture.ID], "automatic_candidate_official_validated": !manual[fixture.ID], "idempotence_verified": !manual[fixture.ID], "findings": findings})
		})
	}
	if directory := os.Getenv("SOLOVEY_HTTP_BEHAVIOR_CAPTURE"); directory != "" {
		data, _ := json.MarshalIndent(report, "", "  ")
		if err := os.WriteFile(filepath.Join(directory, "fixture-replay.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
