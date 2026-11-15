package protection_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	protection "github.com/MalenkiySolovey/solovey-component-server-protection"
	"github.com/MalenkiySolovey/solovey-component-server-protection/domain"
	"github.com/MalenkiySolovey/solovey-component-server-protection/service/enforcement"
	"github.com/MalenkiySolovey/solovey-component-server-protection/service/helpercontract"
	"github.com/MalenkiySolovey/solovey-component-server-protection/service/observation"
	"github.com/MalenkiySolovey/solovey-component-server-protection/service/observationio"
	"github.com/MalenkiySolovey/solovey-component-server-protection/service/policy"
	hostresources "github.com/MalenkiySolovey/solovey-host/componenthost/resources"
	networkintent "github.com/MalenkiySolovey/solovey-host/network/intent"
)

// This standalone consumer intentionally has no Product dependency or executor.
func TestHeadlessConsumerUsesCanonicalDescriptor(t *testing.T) {
	var manifest struct {
		ID, Delivery   string
		DefaultEnabled bool
	}
	if err := json.Unmarshal(protection.ManifestJSON(), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.ID != "server-protection" || manifest.Delivery != "in-process" || manifest.DefaultEnabled {
		t.Fatalf("unexpected descriptor: %+v", manifest)
	}
}

func TestHeadlessPolicyProducesTypedHostIntentWithIndependentInput(t *testing.T) {
	input := enforcement.IntentInput{
		Revision: strings.Repeat("a", 64), InputRevision: strings.Repeat("b", 64), Mode: networkintent.ProtectionEndpointMode,
		Limits:        enforcement.DynamicSetLimits{MaxElements: 4096, DefaultTTLSeconds: 14400, MaxTTLSeconds: 86400},
		AllowTCPPorts: []int{22},
		Endpoints: []enforcement.Endpoint{{ResourceID: "core:inbound:one", Owner: "inbound", ConfigurationRevision: strings.Repeat("c", 64), EndpointRevision: strings.Repeat("d", 64),
			Key: hostresources.PublicEndpointKey{Network: hostresources.NetworkTCP, AddressFamily: hostresources.AddressFamilyIPv4, BindAddress: "192.0.2.1", Port: 443}}},
	}
	var intent networkintent.ProtectionEnforcementIntent = enforcement.NetworkIntent(input)
	if !intent.Binding.HostRef.IsLocal() || intent.Binding.Owner != "server-protection" || intent.Binding.ResourceRef != networkintent.ProtectionResourceRef || len(intent.Binding.Resources) != 1 || intent.Binding.Resources[0].DesiredRevision != input.Endpoints[0].ConfigurationRevision {
		t.Fatalf("typed owner binding lost: %+v", intent)
	}
	input.AllowTCPPorts[0] = 8443
	input.Endpoints[0].ResourceID = "foreign"
	if intent.AllowTCPPorts[0] != 22 || intent.Endpoints[0].Binding.ResourceRef != "core:inbound:one" {
		t.Fatal("mutable embedding data changed the published request")
	}
}

func TestHeadlessConsumerCannotGrantUnknownPrivilegedAuthority(t *testing.T) {
	unknown := helpercontract.DefaultCapabilities()
	if err := helpercontract.ValidateCapabilities(unknown); err != nil {
		t.Fatal(err)
	}
	for _, capability := range unknown.Capabilities {
		if capability.Operation != helpercontract.OperationCapabilities && helpercontract.CapabilityAvailable(unknown, capability.Operation) {
			t.Fatalf("unknown capability admitted %s", capability.Operation)
		}
	}
	request := helpercontract.Request{ProtocolVersion: helpercontract.ProtocolVersion, Correlation: helpercontract.Correlation{OperationID: "operation", InstanceID: "instance"}, Operation: "shell.execute", Capabilities: &helpercontract.CapabilitiesRequest{}}
	if request.ValidateShape() == nil {
		t.Fatal("headless embedding granted arbitrary execution")
	}
	guard := policy.EvaluateManagementGuard(policy.ManagementGuardInput{Subject: domain.SignalSubjectV2{Type: "prefix", Value: "192.0.2.1/24"}, MayRestrictTraffic: true, Now: time.Unix(1000, 0).UTC()})
	if guard.ActionAllowed || guard.State != policy.ManagementGuardUnknown {
		t.Fatalf("unqualified management data allowed restriction: %+v", guard)
	}
}

type dormantStore struct{ buffer, cycles int }

func (s *dormantStore) LoadSettings(context.Context) (domain.Settings, bool, error) {
	return domain.Settings{ObservationBufferSize: s.buffer}, false, nil
}
func (s *dormantStore) BeginObservation(context.Context) (observationio.Cycle, error) {
	s.cycles++
	return nil, errors.New("unexpected durable effect")
}

func TestHeadlessMissingAndDisabledPortsStayInactive(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		store := &dormantStore{}
		if enabled {
			store.buffer = 1
		}
		worker := observation.NewWorker()
		err := worker.Start(store)
		if enabled && err == nil || !enabled && err != nil {
			t.Fatalf("unexpected admission enabled=%v: %v", enabled, err)
		}
		if worker.Status().Running || store.cycles != 0 {
			t.Fatal("unqualified or disabled embedding started work")
		}
		if err := worker.Stop(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
}
