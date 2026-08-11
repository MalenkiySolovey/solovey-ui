package coreinboundcontrol

import (
	"reflect"
	"testing"
)

func exactBuildInput(withUTLS bool) RuntimeBuildInputV1 {
	return RuntimeBuildInputV1{
		Available: true,
		Modules: []RuntimeModuleV1{
			{Path: PinnedSingBoxModule, Version: PinnedSingBoxVersion, Sum: PinnedSingBoxModuleSum},
			{Path: PinnedUTLSModule, Version: PinnedUTLSVersion, Sum: PinnedUTLSModuleSum},
		},
		WithUTLS: withUTLS, BuildProfileRevision: expectedBuildProfileRevision(withUTLS),
		CapabilityResolverRevision: CapabilityResolverRevisionV1,
	}
}

func exactIdentity(withUTLS bool) CoreRuntimeIdentityV1 {
	return ResolveRuntimeIdentityV1(exactBuildInput(withUTLS))
}

func TestRuntimeIdentityExactPinnedTuple(t *testing.T) {
	identity := exactIdentity(true)
	if identity.State != RuntimeIdentityVerified || len(identity.ReasonCodes) != 0 {
		t.Fatalf("identity = %#v", identity)
	}
	if identity.IdentityRevision != PinnedRuntimeIdentityWithUTLSRevisionV1 {
		t.Fatalf("identity revision = %s", identity.IdentityRevision)
	}
	if identity.SingBoxVersion != PinnedSingBoxVersion || identity.SingBoxModuleSum != PinnedSingBoxModuleSum ||
		identity.SingBoxSourceRevision != PinnedSingBoxSourceRevision || identity.UTLSVersion != PinnedUTLSVersion ||
		identity.UTLSModuleSum != PinnedUTLSModuleSum || identity.UTLSSourceRevision != PinnedUTLSSourceRevision {
		t.Fatalf("pinned tuple changed: %#v", identity)
	}
}

func TestRuntimeIdentityRejectsMissingBuildInfo(t *testing.T) {
	identity := ResolveRuntimeIdentityV1(RuntimeBuildInputV1{})
	if identity.State != RuntimeIdentityUnknown || !containsReason(identity.ReasonCodes, ReasonBuildInfoMissing) {
		t.Fatalf("identity = %#v", identity)
	}
}

func TestRuntimeIdentityRejectsReplacedSingBox(t *testing.T) {
	input := exactBuildInput(true)
	input.Modules[0].Replaced = true
	identity := ResolveRuntimeIdentityV1(input)
	if !containsReason(identity.ReasonCodes, ReasonSingBoxModuleReplaced) {
		t.Fatalf("identity = %#v", identity)
	}
}

func TestRuntimeIdentityRejectsMismatchedSingBoxVersionAndSum(t *testing.T) {
	input := exactBuildInput(true)
	input.Modules[0].Version = "v0.0.0"
	input.Modules[0].Sum = "h1:wrong"
	identity := ResolveRuntimeIdentityV1(input)
	if !containsReason(identity.ReasonCodes, ReasonSingBoxVersionMismatch) || !containsReason(identity.ReasonCodes, ReasonSingBoxSumMismatch) {
		t.Fatalf("identity = %#v", identity)
	}
}

func TestRuntimeIdentityRejectsMismatchedUTLSTuple(t *testing.T) {
	input := exactBuildInput(true)
	input.Modules[1].Version = "v0.0.0"
	input.Modules[1].Sum = "h1:wrong"
	identity := ResolveRuntimeIdentityV1(input)
	if !containsReason(identity.ReasonCodes, ReasonUTLSVersionMismatch) || !containsReason(identity.ReasonCodes, ReasonUTLSSumMismatch) {
		t.Fatalf("identity = %#v", identity)
	}
}

func TestRuntimeIdentityAcceptsWithUTLSTrueAndFalseProfiles(t *testing.T) {
	for _, withUTLS := range []bool{true, false} {
		identity := exactIdentity(withUTLS)
		if identity.State != RuntimeIdentityVerified || identity.WithUTLS != withUTLS || identity.BuildProfileRevision != expectedBuildProfileRevision(withUTLS) {
			t.Fatalf("with_utls=%t identity = %#v", withUTLS, identity)
		}
	}
}

func TestRuntimeIdentityRevisionIsDeterministic(t *testing.T) {
	left := exactIdentity(true)
	right := exactIdentity(true)
	if left.IdentityRevision == "" || left.IdentityRevision != right.IdentityRevision || !reflect.DeepEqual(left, right) {
		t.Fatalf("identity revisions differ: %#v %#v", left, right)
	}
}

func TestRuntimeIdentityRejectsResolverRevisionDrift(t *testing.T) {
	input := exactBuildInput(true)
	input.CapabilityResolverRevision = "unexpected"
	identity := ResolveRuntimeIdentityV1(input)
	if identity.State != RuntimeIdentityUnknown || !containsReason(identity.ReasonCodes, ReasonResolverRevisionMismatch) {
		t.Fatalf("identity = %#v", identity)
	}
}

func TestRuntimeIdentityRejectsInconsistentBuildProfile(t *testing.T) {
	input := exactBuildInput(true)
	input.BuildProfileRevision = BuildProfileWithoutUTLSRevision
	identity := ResolveRuntimeIdentityV1(input)
	if !containsReason(identity.ReasonCodes, ReasonWithUTLSInconsistent) {
		t.Fatalf("identity = %#v", identity)
	}
}
