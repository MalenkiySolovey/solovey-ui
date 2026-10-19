package planner

import (
	"reflect"
	"testing"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

func protectionFixture() (ProtectionEnforcementIntent, Facts) {
	binding, facts := fixture()
	intent := ProtectionEnforcementIntent{Binding: binding, ArtifactRevision: binding.DesiredRevision, Mode: ProtectionEndpointMode,
		Limits: ProtectionLimits{4096, 14400, 86400}, Rates: ProtectionRates{PacketLimit{5, 10}, PacketLimit{20, 40}, PacketLimit{2, 4}}}
	for index, resource := range binding.Resources {
		intent.Endpoints = append(intent.Endpoints, ProtectionEndpoint{Binding: resource,
			Key:           hostresources.PublicEndpointKey{Network: hostresources.NetworkTCP, AddressFamily: hostresources.AddressFamilyIPv4, BindAddress: "0.0.0.0", Port: uint16(443 + index)},
			Contributions: []TimedSource{{Subject: "192.0.2.1/32", Class: BlockSet, ExpiresAt: 2000, TTLSeconds: 600}}})
	}
	return intent, facts
}

func TestProtectionCompileBindsEveryDecisionAndOwnedObject(t *testing.T) {
	intent, facts := protectionFixture()
	first, err := CompileProtection(intent, facts)
	if err != nil {
		t.Fatal(err)
	}
	intent.Endpoints[0], intent.Endpoints[1] = intent.Endpoints[1], intent.Endpoints[0]
	second, err := CompileProtection(intent, facts)
	if err != nil || first.Revision() != second.Revision() || !reflect.DeepEqual(first.Enforcement(), second.Enforcement()) {
		t.Fatalf("non-deterministic protection plan: %v", err)
	}
	if len(first.Resources()) != 6 {
		t.Fatalf("missing table/chain/set ownership: %#v", first.Resources())
	}
	preview := first.Preview()
	if preview.HostRef != first.Intent().HostRef || preview.Owner != first.Intent().Owner || preview.ResourceRef != first.Intent().ResourceRef || preview.DesiredRevision != first.Intent().DesiredRevision || preview.InputRevision != first.Intent().InputRevision || preview.PlanRevision != first.Revision() || !reflect.DeepEqual(preview.Resources, first.Resources()) {
		t.Fatal("preview lost the accepted intent/resource impact identity")
	}
	preview.Resources[0].Owner = "changed-by-preview-reader"
	if first.Resources()[0].Owner != first.Intent().Owner {
		t.Fatal("preview changed an immutable plan")
	}
	for _, resource := range first.Resources() {
		if !resource.HostRef.IsLocal() || resource.Owner != "server-protection" || resource.ResourceRef == "" || resource.PlanRevision != first.Revision() || resource.Kind == "" || resource.Purpose == "" {
			t.Fatalf("incomplete ownership: %#v", resource)
		}
	}
	intent.Endpoints[0].Contributions[0].Subject = "192.0.2.2/32"
	third, err := CompileProtection(intent, facts)
	if err != nil || third.Revision() == first.Revision() {
		t.Fatalf("policy materialization was not bound: %v", err)
	}
	copy := first.Enforcement()
	copy.Endpoints[0].Contributions[0].Subject = "mutated"
	if first.Enforcement().Endpoints[0].Contributions[0].Subject == "mutated" {
		t.Fatal("caller changed compiled intent")
	}
}

func TestProtectionAllocationCollisionIsTyped(t *testing.T) {
	intent, facts := protectionFixture()
	intent.Endpoints[1].Binding.EndpointRevision = intent.Endpoints[0].Binding.EndpointRevision[:12] + hash("collision")[12:]
	facts.Resources[1] = intent.Endpoints[1].Binding
	_, err := CompileProtection(intent, facts)
	requireReason(t, err, AllocationUnavailable)
}
