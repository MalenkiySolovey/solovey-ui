package nativefallback

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	neutralfallback "github.com/MalenkiySolovey/solovey-host/componenthost/fallbacktargets"
	hostresources "github.com/MalenkiySolovey/solovey-host/componenthost/resources"
	corecontract "github.com/MalenkiySolovey/solovey-host/coreruntime/inboundcontrol"
)

func ownerReferenceFixture() neutralfallback.FallbackTargetReferenceV2 {
	digest := strings.Repeat("a", 64)
	return neutralfallback.FallbackTargetReferenceV2{Schema: neutralfallback.TargetReferenceSchemaV2, ProviderID: "provider", TargetID: "target", PublishRevision: "publish", ContentDigest: digest, EndpointID: "endpoint", EndpointRevision: digest, ProviderHealthRevision: digest, CapacityRevision: digest, ProviderRevision: "provider-revision"}
}

func TestMissingOrCanceledTargetInventoryDoesNotUseImplicitOwner(t *testing.T) {
	reference := ownerReferenceFixture()
	if err := reference.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := (RegistryTargetReader{}).ResolveV2(t.Context(), reference); err == nil || err.Error() != "target_inventory_unavailable" {
		t.Fatalf("implicit target owner used: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	// A nonnil inventory with no implementation panics if cancellation is ignored.
	if _, err := (RegistryTargetReader{Registry: struct{ TargetInventory }{}}).ResolveV2(ctx, reference); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled inventory call admitted: %v", err)
	}
}

func TestMissingManagementOwnerDoesNotReadAnotherOwner(t *testing.T) {
	for _, missing := range []string{"endpoints", "resources"} {
		t.Run(missing, func(t *testing.T) {
			reader := InventoryManagementReader{Endpoints: func(context.Context, time.Time) []hostresources.ManagementEndpointV1 {
				t.Fatal("read endpoints before owner admission")
				return nil
			}, Resources: func(context.Context) hostresources.ResourceSnapshot {
				t.Fatal("read resources before owner admission")
				return hostresources.ResourceSnapshot{}
			}}
			if missing == "endpoints" {
				reader.Endpoints = nil
			} else {
				reader.Resources = nil
			}
			result, err := reader.ResolveIsolation(t.Context(), "resource", ManagementEndpointFactsV1{})
			if err == nil || result.State != "UNKNOWN" {
				t.Fatalf("missing inventory was authoritative: %#v %v", result, err)
			}
		})
	}
}

type deniedCoreAdmission struct{ revision string }

func (a deniedCoreAdmission) CapabilityResolverRevision() string { return a.revision }
func (deniedCoreAdmission) VerifyRuntimeIdentity(corecontract.CoreRuntimeIdentityV1) bool {
	return false
}

func TestMissingCoreAdmissionRejectsBeforeAnyCoreOrTargetRead(t *testing.T) {
	for _, admission := range []CoreAdmission{nil, deniedCoreAdmission{}} {
		planner := Planner{Core: struct{ CoreReader }{}, Admission: admission, Targets: struct{ TargetReader }{}, Management: struct{ ManagementReader }{}}
		_, err := planner.Plan(t.Context(), PlanRequestV1{InboundDatabaseID: 1, TargetReference: ownerReferenceFixture()})
		var failure *PlannerError
		if !errors.As(err, &failure) || failure.Code != "planner_input_invalid" {
			t.Fatalf("unbound Core authority admitted: %v", err)
		}
	}
}

func TestVerifiedIdentityLabelDoesNotOverrideCoreAdmission(t *testing.T) {
	identity := corecontract.CoreRuntimeIdentityV1{State: corecontract.RuntimeIdentityVerified, IdentityRevision: strings.Repeat("a", 64), CapabilityResolverRevision: "resolver"}
	snapshot := corecontract.InboundFallbackSnapshotV1{RuntimeIdentityRevision: identity.IdentityRevision, CapabilityResolverRevision: identity.CapabilityResolverRevision}
	planner := Planner{Admission: deniedCoreAdmission{revision: "resolver"}}
	if planner.runtimeIdentityExact(identity, snapshot, "resolver") {
		t.Fatal("identity label bypassed actual Core admission")
	}
}
