package validation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	entityendpoints "github.com/MalenkiySolovey/solovey-ui/internal/entities/endpoints"
	entityinbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds"
	entityoutbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
	entitytls "github.com/MalenkiySolovey/solovey-ui/internal/entities/tls"
	singboxconfig "github.com/MalenkiySolovey/solovey-ui/internal/singbox/config"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
)

// This test adapter orders the real semantic owners over exact stable captured
// config bytes. It is not a production upgrader or fabricated old fixture.
func replayCatalogue(source []byte) ([]byte, []diagnostics.Finding, error) {
	base, err := singboxconfig.PrepareBaseOptionsUpgrade(source)
	findings := base.Findings
	if err != nil {
		return source, findings, err
	}
	var root map[string]json.RawMessage
	_ = json.Unmarshal(base.Candidate, &root)
	for _, section := range []string{"inbounds", "outbounds"} {
		var rows []json.RawMessage
		if json.Unmarshal(root[section], &rows) != nil {
			continue
		}
		for i, raw := range rows {
			var fields map[string]json.RawMessage
			_ = json.Unmarshal(raw, &fields)
			var kind string
			_ = json.Unmarshal(fields["type"], &kind)
			path := fmt.Sprintf("%s[%d]", section, i)
			var candidate json.RawMessage
			var local []diagnostics.Finding
			if section == "inbounds" {
				projection, stageErr := entityinbounds.PrepareOptionsUpgrade(kind, path, raw)
				candidate, local, err = projection.Candidate, projection.Findings, stageErr
			} else {
				candidate, local, err = entityoutbounds.PrepareOptionsUpgrade(kind, path, raw)
			}
			findings = append(findings, local...)
			if err != nil {
				return source, findings, err
			}
			_ = json.Unmarshal(candidate, &fields)
			if tls, present := fields["tls"]; present {
				projected, local, err := entitytls.PrepareOptionsUpgrade(path+".tls", tls)
				findings = append(findings, local...)
				if err != nil {
					return source, findings, err
				}
				fields["tls"] = projected
			}
			rows[i], _ = json.Marshal(fields)
		}
		root[section], _ = json.Marshal(rows)
	}
	candidate, _ := json.Marshal(root)
	if ValidateConfigShape(candidate) != nil {
		findings = append(findings, diagnostics.Finding{Kind: "config", Path: "config", Code: "UPGRADE_COMPLETE_SHAPE_REJECTED", Severity: diagnostics.Error, Message: "The complete captured candidate is not accepted by its pinned consumer.", MigrationOutcome: diagnostics.ManualRequired, OperatorActionRequired: true})
		return source, findings, diagnostics.FirstError(findings)
	}
	if ValidateConfig(candidate) != nil {
		findings = append(findings, diagnostics.Finding{Kind: "config", Path: "config", Code: "UPGRADE_COMPLETE_BUILD_REJECTED", Severity: diagnostics.Error, Message: "The captured config has no complete accepted offline build. Correct the whole candidate before retrying.", MigrationOutcome: diagnostics.ManualRequired, OperatorActionRequired: true})
		return source, findings, diagnostics.FirstError(findings)
	}
	return candidate, findings, nil
}

func TestAcceptedOldCatalogueReplay(t *testing.T) {
	var manifest []struct {
		ID     string `json:"fixture_id"`
		Hash   string `json:"config_sha256"`
		Commit string `json:"solovey_commit"`
		Core   string `json:"core_version"`
	}
	data, err := os.ReadFile("testdata/accepted-old-catalogue/manifest.json")
	if err != nil || json.Unmarshal(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}), &manifest) != nil || len(manifest) != 14 {
		t.Fatal("old capture manifest invalid")
	}
	var replayResults []map[string]any
	for _, row := range manifest {
		t.Run(row.ID, func(t *testing.T) {
			source, err := os.ReadFile(filepath.Join("testdata/accepted-old-catalogue", row.ID+".json"))
			if err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256(source)
			if hex.EncodeToString(hash[:]) != row.Hash || row.Commit != "3523eeea9f91c091ab3f0fbdb94c3955e37798b4" || row.Core != "v1.13.18" {
				t.Fatal("exact old capture identity changed")
			}
			expected := diagnostics.AutomaticDiagnostic
			switch row.ID {
			case "clash-cache-active", "tls-ech-noops", "debug-oom-null", "tun-noop-zero", "direct-outbound-zero":
				expected = diagnostics.ManualRequired
			case "debug-oom-false", "geo-active", "old-listen-active":
				expected = diagnostics.UnsupportedLegacy
			case "direct-inbound-supported":
				expected = diagnostics.LosslessAutomatic
			}
			candidate, findings, err := replayCatalogue(source)
			if diagnostics.Outcome(findings) != expected {
				t.Fatalf("outcome got %s want %s", diagnostics.Outcome(findings), expected)
			}
			result := map[string]any{"fixture_id": row.ID, "domain": "COORDINATOR_CATALOGUE", "source_sha256": row.Hash, "outcome": diagnostics.Outcome(findings), "findings": findings, "immutable_capture_preserved": true}
			if expected == diagnostics.ManualRequired || expected == diagnostics.UnsupportedLegacy {
				if err == nil || !bytes.Equal(candidate, source) {
					t.Fatal("failed candidate lost exact source")
				}
				result["source_preserved"] = true
				replayResults = append(replayResults, result)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			twice, _, err := replayCatalogue(candidate)
			if err != nil || !bytes.Equal(twice, candidate) {
				t.Fatal("owner replay not idempotent")
			}
			if ValidateConfig(candidate) != nil {
				t.Fatal("offline complete build rejected automatic candidate")
			}
			result["automatic_candidate_official_validated"] = true
			result["idempotence_verified"] = true
			result["source_preserved"] = bytes.Equal(source, candidate)
			replayResults = append(replayResults, result)
		})
	}
	if output := os.Getenv("SOLOVEY_CATALOGUE_REPLAY_CAPTURE"); output != "" && !t.Failed() {
		data, err := json.MarshalIndent(replayResults, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(output, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNegativeCapabilitiesShareOwnerDiagnostics(t *testing.T) {
	for _, row := range []struct{ source, code string }{
		{`{"network_namespaces":[{"tag":"host"}]}`, "NAMESPACE_HOLDER_UNAVAILABLE"},
		{`{"inbounds":[{"type":"bridge"}]}`, "PRODUCT_CAPABILITY_UNAVAILABLE"},
		{`{"endpoints":[{"type":"tailscale","ssh_server":true}]}`, "TAILSCALE_HOST_FEATURE_UNAVAILABLE"},
		{`{"endpoints":[{"type":"tailscale","taildrop_directory":"fixture"}]}`, "TAILSCALE_HOST_FEATURE_UNAVAILABLE"},
		{`{"outbounds":[{"type":"trojan","tls":{"enabled":false,"spoof":"fixture"}}]}`, "TLS_SPOOF_UNAVAILABLE"},
		{`{"route":{"rules":[{"action":"route","outbound":"direct","tls_spoof_method":"wrong-ack"}]}}`, "TLS_SPOOF_UNAVAILABLE"},
		{`{"outbounds":[{"type":"wireguard"}]}`, "PRODUCT_CAPABILITY_UNAVAILABLE"},
	} {
		if err := ValidateConfigShape([]byte(row.source)); err == nil || !strings.Contains(err.Error(), row.code) {
			t.Fatalf("missing shared reason %s", row.code)
		}
	}
	for _, raw := range []string{`{"ssh_server":false,"taildrop_directory":""}`, `{"ssh_server":{"enabled":false,"disable_sftp":true}}`} {
		if len(entityendpoints.HostCapabilityFindings("tailscale", "endpoint", []byte(raw))) != 0 {
			t.Fatal("disabled host options changed semantics")
		}
	}
	if len(entityendpoints.HostCapabilityFindings("tailscale", "endpoint", []byte(`{"ssh_server":"invalid"}`))) != 1 {
		t.Fatal("invalid host shape bypassed its owner")
	}
}

func TestCatalogueTypedSentinelsAndAliasConflict(t *testing.T) {
	for _, source := range []string{`{"experimental":{"clash_api":{"cache_file":false}}}`, `{"experimental":{"clash_api":{"store_fakeip":""}}}`, `{"route":{"rules":[{"geoip":false,"action":"reject"}]}}`, `{"route":{"rules":[{"rule_set_ipcidr_match_source":true,"rule_set_ip_cidr_match_source":false,"action":"reject"}]}}`} {
		prepared, err := singboxconfig.PrepareBaseOptionsUpgrade([]byte(source))
		if err == nil || !bytes.Equal(prepared.Candidate, []byte(source)) {
			t.Fatal("wrong type or divergent alias silently cleaned up")
		}
	}
}
