package management

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

func TestObservedSSHInventoryPreservesAmbiguityAndTopology(t *testing.T) {
	now := time.Unix(10_000, 0).UTC()
	configuration := digestFixture("config")
	ipv6Only := false
	surface := hostfacts.HostSurfaceFactV1{Schema: hostfacts.SchemaV1, ID: "ssh-v6", Network: hostfacts.NetworkTCP,
		Family: hostfacts.FamilyIPv6, Bind: "::", Port: 22, Exposure: hostfacts.ExposurePublic,
		Service: hostfacts.ServiceFact{SystemdUnit: "sshd.service"}, DesiredOwner: "system", LastSeen: now.Unix(),
		ExpiresAt: now.Add(time.Minute).Unix(), Source: "host-surface", ConfidenceBP: 8000,
		ConfigurationRevision: configuration, ReasonCodes: []string{"owner_ambiguous"},
		ListenerOwner: &hostfacts.ListenerOwnerFactV1{Socket: hostfacts.ListenerSocketIdentityV1{Family: hostfacts.FamilyIPv6,
			Wildcard: true, IPv6Only: &ipv6Only, CoverageFamilies: []hostfacts.Family{hostfacts.FamilyIPv6, hostfacts.FamilyIPv4}}}}
	values := Endpoints(nil, hostfacts.Snapshot{Facts: []hostfacts.HostSurfaceFactV1{surface}}, now)
	if len(values) != 1 {
		t.Fatalf("endpoints=%#v", values)
	}
	value := values[0]
	if !value.ObservedListener || value.ConfiguredIntent || !value.Wildcard || !value.DualStack || len(value.ReasonCodes) != 1 || value.ReasonCodes[0] != "owner_ambiguous" {
		t.Fatalf("endpoint=%#v", value)
	}
	if hostresources.ManagementEndpointCurrent(value, now) {
		t.Fatal("ambiguous listener was accepted as current")
	}
}

func TestConfiguredIntentRemainsDistinctFromObservedListener(t *testing.T) {
	now := time.Unix(10_000, 0).UTC()
	revision := digestFixture("panel")
	resource := hostresources.ProtectableResource{ID: "core:panel:web", Kind: "panel_web", Owner: "panel", Protocol: "tcp", Listen: "127.0.0.1", Port: 2053,
		Capabilities: hostresources.ProtectableResourceCapabilities{ConfigRevision: revision, OwnerRevision: digestFixture("owner")}}
	values := Endpoints([]hostresources.ProtectableResource{resource}, hostfacts.Snapshot{}, now)
	if len(values) != 1 || !values[0].ConfiguredIntent || values[0].ObservedListener || values[0].ServiceKind != hostresources.ManagementPanel {
		t.Fatalf("endpoints=%#v", values)
	}
}

type evidenceFixture struct {
	id    string
	paths []hostresources.RecoveryPathV1
	err   error
}

func (f evidenceFixture) ProviderID() string { return f.id }
func (f evidenceFixture) RecoveryPaths(context.Context, time.Time) ([]hostresources.RecoveryPathV1, error) {
	return f.paths, f.err
}

func TestEvidenceRegistryFailsClosedOnDuplicateAndProviderFailure(t *testing.T) {
	registry := NewEvidenceRegistry()
	if _, err := registry.Register(evidenceFixture{id: "duplicate", paths: nil}); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Register(evidenceFixture{id: "duplicate", paths: nil}); err == nil {
		t.Fatal("duplicate evidence authority was accepted")
	}
	if _, err := registry.Register(evidenceFixture{id: "failed", err: errors.New("unavailable")}); err != nil {
		t.Fatal(err)
	}
	snapshot := registry.Snapshot(context.Background(), time.Unix(10_000, 0).UTC())
	if !hasReason(snapshot.ReasonCodes, "evidence_provider_unavailable") {
		t.Fatalf("snapshot=%#v", snapshot)
	}
}

func digestFixture(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func hasReason(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
