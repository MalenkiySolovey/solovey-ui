//go:build with_quic

package singboxconfig_test

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"testing"

	entityinbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds"
	singboxconfig "github.com/MalenkiySolovey/solovey-ui/internal/singbox/config"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/validation"
)

// These exact bytes were captured by the accepted-source storage owner, not by
// current 1.14 code. The manifest binds capture commit/core and byte identities.
//
//go:embed testdata/accepted-1.13-dns/*.json
var acceptedDNSFixtures embed.FS

func TestAcceptedDNSFixtureReplay(t *testing.T) {
	manifestRaw, err := acceptedDNSFixtures.ReadFile("testdata/accepted-1.13-dns/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest []struct {
		ID     string `json:"fixture_id"`
		SHA    string `json:"config_sha256"`
		Commit string `json:"solovey_commit"`
		Core   string `json:"core_version"`
	}
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		t.Fatal(err)
	}
	manual := map[string]bool{"legacy-strategy": true, "legacy-local": true, "rcode-final": true, "local-constrained": true, "tun-default": true, "fakeip-active": true}
	if len(manifest) != 28 {
		t.Fatal("capture manifest incomplete")
	}
	for _, f := range manifest {
		t.Run(f.ID, func(t *testing.T) {
			if f.Commit != "3523eeea9f91c091ab3f0fbdb94c3955e37798b4" || f.Core != "v1.13.18" {
				t.Fatal("incorrect baseline")
			}
			raw, err := acceptedDNSFixtures.ReadFile("testdata/accepted-1.13-dns/" + f.ID + ".json")
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(raw)
			if hex.EncodeToString(sum[:]) != f.SHA {
				t.Fatal("captured bytes changed")
			}
			p, err := singboxconfig.PrepareDNSUpgrade(raw)
			p.Findings = append(p.Findings, entityinbounds.TUNDNSFindings(raw)...)
			if err == nil {
				err = diagnostics.FirstError(p.Findings)
			}
			blocked := manual[f.ID] || f.ID == "unknown-scheme"
			if (err != nil) != blocked {
				t.Fatalf("unexpected replay outcome: %v", err)
			}
			if blocked {
				if !bytes.Equal(raw, p.Candidate) {
					t.Fatal("failed replay destroyed preimage")
				}
				return
			}
			if err := validation.ValidateConfig(p.Candidate); err != nil {
				t.Fatalf("official candidate rejected: %v", err)
			}
			second, err := singboxconfig.PrepareDNSUpgrade(p.Candidate)
			if err != nil || !bytes.Equal(p.Candidate, second.Candidate) {
				t.Fatal("retry changed candidate")
			}
		})
	}
}
