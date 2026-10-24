//go:build linux

package updatebroker

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	coreprovider "github.com/MalenkiySolovey/solovey-ui/core/provider"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/targetcheck"
	providerfixture "github.com/MalenkiySolovey/solovey-ui/testsupport/coreprovider"
)

func TestProviderCheckReportBindsInputAndExactObservedSource(t *testing.T) {
	for _, id := range []string{"sing-box", "shtorm-extended"} {
		identity := providerfixture.Identity(t, id)
		releaseIdentity := providerReleaseFixture(t, id)
		report := targetcheck.Report{Schema: targetcheck.Schema, State: "VALID", CoreProvider: identity,
			PanelVersion: releaseIdentity.Version, Capabilities: providerCapabilityFixture(t, id, releaseIdentity.BinaryProfile),
			ConfigurationDigest: updateDigest("unchanged-configuration"), ReasonCodes: []string{}}
		report.CapabilityRevision = report.Capabilities.Revision()
		raw := mustJSON(report)
		if !providerCheckReportMatches(raw, releaseIdentity, report.ConfigurationDigest) {
			t.Fatal("exact report did not match independently bound propositions")
		}
		for name, mutate := range map[string]func(*targetcheck.Report){
			"schema":               func(r *targetcheck.Report) { r.Schema = "unknown" },
			"state":                func(r *targetcheck.Report) { r.State = "REJECTED" },
			"configuration":        func(r *targetcheck.Report) { r.ConfigurationDigest = updateDigest("other") },
			"panel-version":        func(r *targetcheck.Report) { r.PanelVersion = "2026.4.2" },
			"missing-capabilities": func(r *targetcheck.Report) { r.Capabilities.Facts = nil },
			"capability-revision":  func(r *targetcheck.Report) { r.CapabilityRevision = updateDigest("other") },
			"capability-provider":  func(r *targetcheck.Report) { r.Capabilities.Provider.ID = "other" },
			"capability-profile":   func(r *targetcheck.Report) { r.Capabilities.ComponentProfile = "core" },
			"provider":             func(r *targetcheck.Report) { r.CoreProvider.ID = "unverified" },
			"module": func(r *targetcheck.Report) {
				r.CoreProvider.ModuleSum = "h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
			},
			"source-revision":          func(r *targetcheck.Report) { r.CoreProvider.SourceRevision = updateDigest("other")[:40] },
			"source-state":             func(r *targetcheck.Report) { r.CoreProvider.State = coreprovider.Unknown },
			"invented-artifact-trust":  func(r *targetcheck.Report) { r.CoreProvider.ArtifactState = coreprovider.Verified },
			"invented-artifact-digest": func(r *targetcheck.Report) { r.CoreProvider.ArtifactDigest = updateDigest("invented") },
			"identity-revision":        func(r *targetcheck.Report) { r.CoreProvider.Revision = updateDigest("forged") },
			"reason":                   func(r *targetcheck.Report) { r.ReasonCodes = []string{"credential-sentinel"} },
			"source-reason":            func(r *targetcheck.Report) { r.CoreProvider.ReasonCodes = []string{} },
		} {
			t.Run(id+"/"+name, func(t *testing.T) {
				changed := report
				mutate(&changed)
				if providerCheckReportMatches(mustJSON(changed), releaseIdentity, report.ConfigurationDigest) {
					t.Fatal("conflicting candidate report acquired validation authority")
				}
			})
		}
		for _, changed := range [][]byte{
			append(bytes.Clone(raw), []byte(` {}`)...),
			bytes.Replace(raw, []byte(`"state":"VALID"`), []byte(`"state":"REJECTED","state":"VALID"`), 1),
			bytes.Replace(raw, []byte(`"schema":`), []byte(`"unknown":true,"schema":`), 1),
			[]byte(strings.Repeat(" ", targetcheck.MaxReportBytes+1)),
		} {
			if providerCheckReportMatches(changed, releaseIdentity, report.ConfigurationDigest) {
				t.Fatal("ambiguous, unknown or oversized report was accepted")
			}
		}
		// The old process need not contain a future authenticated source pin.
		future := report
		future.CoreProvider.ProviderVersion = "v1.14.4-0.20261024000000-123456789abc"
		future.CoreProvider.SourceRevision = strings.Repeat("a", 40)
		future.CoreProvider.Revision = ""
		future.CoreProvider.Revision = broker.Digest(mustJSON(future.CoreProvider))
		future.Capabilities.Provider = future.CoreProvider
		future.CapabilityRevision = future.Capabilities.Revision()
		releaseIdentity.CoreProvider = future.CoreProvider.Source
		if !providerCheckReportMatches(mustJSON(future), releaseIdentity, report.ConfigurationDigest) ||
			coreprovider.SourceIdentityVerified(future.CoreProvider) {
			t.Fatal("signed future-source observation was confused with the current compiled catalog")
		}
	}
}

func TestProviderCheckOutputIsBoundedThroughReaderCopy(t *testing.T) {
	output := boundedProviderCheckOutput{limit: 8}
	if n, err := io.Copy(&output, strings.NewReader("12345678")); err != nil || n != 8 {
		t.Fatal("exact bounded output failed")
	}
	if n, err := io.Copy(&output, strings.NewReader("credential-sentinel")); err == nil || n != 0 || string(output.Bytes()) != "12345678" || strings.Contains(err.Error(), "credential-sentinel") {
		t.Fatal("copy bypassed the output bound or echoed candidate output")
	}
}

func TestPreparedProviderActualConfigurationCheck(t *testing.T) {
	binary := os.Getenv("SOLOVEY_PROVIDER_TEST_BINARY")
	if binary == "" {
		t.Skip("actual executable configuration probe is supplied during provider qualification")
	}
	requireRootUpdateBrokerTest(t)
	id, profile := os.Getenv("SOLOVEY_PROVIDER_TEST_ID"), os.Getenv("SOLOVEY_PROVIDER_TEST_PROFILE")
	identity := providerReleaseFixture(t, id)
	if profile != "full" && profile != "core" {
		t.Fatal("actual target check requires an explicit profile")
	}
	identity.BinaryProfile, identity.Artifacts[0].Role = profile, "panel-"+profile
	identity.ArtifactSetDigest = artifactSetDigest(identity.Artifacts)
	root := t.TempDir()
	panel := filepath.Join(root, "solovey-ui")
	input, err := os.Open(binary)
	if err != nil {
		t.Fatal(err)
	}
	output, err := os.OpenFile(panel, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		_ = input.Close()
		t.Fatal(err)
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	inputErr := input.Close()
	if copyErr != nil || closeErr != nil || inputErr != nil {
		t.Fatal("actual candidate copy failed")
	}
	if err := os.Chmod(panel, 0o755); err != nil {
		t.Fatal(err)
	}
	// The fixed descriptor works under an unprivileged principal even though
	// the private staging ancestry cannot be traversed by that principal.
	peer := broker.PeerIdentity{PID: os.Getpid(), UID: 65534, GID: 65534}
	raw := []byte(" {\n\"log\":{\"disabled\":true}\n} ")
	before := bytes.Clone(raw)
	check, err := checkPreparedProvider(t.Context(), root, identity, raw, peer)
	if err != nil || check.ConfigurationDigest != broker.Digest(raw) || !validDigest(check.ExecutableDigest) ||
		!validDigest(check.ReportDigest) || !bytes.Equal(raw, before) {
		t.Fatal("actual descriptor-bound target check failed", err)
	}
	t.Logf("provider=%s profile=%s executable_sha256=%s configuration_sha256=%s report_sha256=%s", id, profile, check.ExecutableDigest, check.ConfigurationDigest, check.ReportDigest)
	if validateProviderCapabilities(check.TargetCapabilities, check.CapabilityRevision, identity.CoreProvider) != nil {
		t.Fatal("actual target configuration check did not bind build capabilities")
	}
	wrongVersion := identity
	wrongVersion.Version = "2026.4.999"
	if result, err := checkPreparedProvider(t.Context(), root, wrongVersion, raw, peer); err == nil || result != (preparedProviderCheck{}) {
		t.Fatal("signed release version did not match the candidate's compiled Panel version")
	}
	for _, invalid := range [][]byte{
		[]byte(`{"credential-sentinel-unknown":true}`),
		[]byte(`{"outbounds":[{"type":"direct","tag":"d","credential-sentinel":true}]}`),
		[]byte(`{"log":{},"log":{}}`),
	} {
		before := bytes.Clone(invalid)
		if result, err := checkPreparedProvider(t.Context(), root, identity, invalid, peer); err == nil || result != (preparedProviderCheck{}) ||
			strings.Contains(err.Error(), "credential-sentinel") || !bytes.Equal(invalid, before) {
			t.Fatal("unsupported/ambiguous configuration was accepted, mutated or echoed")
		}
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := checkPreparedProvider(cancelled, root, identity, raw, peer); err == nil {
		t.Fatal("cancelled validation acquired evidence")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 || entries[0].Name() != "solovey-ui" {
		t.Fatal("target check wrote a runtime artifact")
	}
	encoded, err := json.Marshal(check)
	if err != nil || bytes.Contains(encoded, []byte("credential-sentinel")) {
		t.Fatal("target check evidence contains configuration content")
	}
}
