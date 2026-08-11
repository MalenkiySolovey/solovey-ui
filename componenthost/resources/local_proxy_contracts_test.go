package resources

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func localProxyFactFixture(inboundType string, exposure LocalProxyExposureV1, authentication LocalProxyAuthenticationV1) LocalProxyFactV1 {
	now := time.Unix(1_800_000_000, 0).UTC()
	bind := "127.0.0.1"
	family := AddressFamilyIPv4
	if exposure == LocalProxyExposurePrivate {
		bind = "10.0.0.8"
	}
	if exposure == LocalProxyExposurePublic {
		bind = "203.0.113.8"
	}
	if exposure == LocalProxyExposureWildcard {
		bind = "0.0.0.0"
	}
	protocols := []LocalProxyProtocolV1{LocalProxyProtocolSOCKS5}
	switch inboundType {
	case "http":
		protocols = []LocalProxyProtocolV1{LocalProxyProtocolHTTPConnect, LocalProxyProtocolHTTPForward}
	case "mixed":
		protocols = []LocalProxyProtocolV1{LocalProxyProtocolHTTPConnect, LocalProxyProtocolHTTPForward, LocalProxyProtocolSOCKS5}
	}
	count := 0
	if authentication == LocalProxyAuthenticationPresent {
		count = 1
	}
	digest := Revision("fixture")
	fact := LocalProxyFactV1{
		Schema: LocalProxyFactSchemaV1, ProviderID: "core", ContributorID: "core",
		ResourceID: "core:inbound:17", EndpointID: "tcp:ipv4:1080", InboundDatabaseID: 17,
		InboundType: inboundType, ConfigurationRevision: digest, EffectiveRuntimeRevision: Revision("runtime"),
		RuntimeIdentityRevision: "runtime-owner-v1", ProviderRevision: LocalProxyProviderRevisionV1,
		CapabilityRevision: LocalProxyCapabilityRevisionV1, ListenerObservationRevision: Revision("listener"),
		OwnerRevision: "owner-v1", HealthRevision: Revision("health"), CapacityRevision: Revision("capacity"),
		ManagementExclusionRevision: Revision("management"), RecoveryPathRevision: Revision("recovery"),
		ConfiguredBind: bind, ConfiguredPort: 1080, AddressFamily: family,
		ObservedBind: bind, ObservedPort: 1080, ObservedAddressFamily: family,
		Exposure: exposure, Ownership: LocalProxyProviderManaged, ListenerState: LocalProxyListenerObservedExact,
		Protocols: protocols, Authentication: authentication, AuthenticationCount: count,
		AuthenticationRevision: Revision("authentication"), TLS: LocalProxyTLSDisabled, TLSRevision: Revision("tls"),
		SystemProxy: LocalProxySystemProxyDisabled, SystemProxyRevision: Revision("system-proxy"),
		DependentUDPAssociation: inboundType == "socks" || inboundType == "mixed",
		RuntimeReady:            true, HealthCapabilityReady: true, CapacityReady: true,
		ManagementCollision: CapabilityNo, RecoveryPathCollision: CapabilityNo,
		ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(), ReasonCodes: []string{},
	}
	fact.FactRevision = Revision(localProxyFactRevisionInput(fact))
	return fact
}

func TestLocalProxyEligibilityMatrixAndExactReferenceBoundary(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	for name, fact := range map[string]LocalProxyFactV1{
		"loopback no auth": localProxyFactFixture("socks", LocalProxyExposureLoopback, LocalProxyAuthenticationAbsent),
		"private auth":     localProxyFactFixture("http", LocalProxyExposurePrivate, LocalProxyAuthenticationPresent),
		"mixed auth":       localProxyFactFixture("mixed", LocalProxyExposurePrivate, LocalProxyAuthenticationPresent),
	} {
		t.Run(name, func(t *testing.T) {
			if err := fact.Actionable(now); err != nil {
				t.Fatalf("actionable fact rejected: %v", err)
			}
			reference, err := ReferenceLocalProxyV1(fact, now)
			if err != nil {
				t.Fatal(err)
			}
			payload, err := json.Marshal(reference)
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{"configuredBind", "configuredPort", "observedBind", "observedPort", "127.0.0.1", "10.0.0.8", "username", "password", "destination"} {
				if strings.Contains(string(payload), forbidden) {
					t.Fatalf("reference leaked forbidden authority %q: %s", forbidden, payload)
				}
			}
		})
	}
	for name, fact := range map[string]LocalProxyFactV1{
		"private no auth": localProxyFactFixture("socks", LocalProxyExposurePrivate, LocalProxyAuthenticationAbsent),
		"public":          localProxyFactFixture("http", LocalProxyExposurePublic, LocalProxyAuthenticationPresent),
		"wildcard":        localProxyFactFixture("mixed", LocalProxyExposureWildcard, LocalProxyAuthenticationPresent),
		"unknown auth":    localProxyFactFixture("socks", LocalProxyExposureLoopback, LocalProxyAuthenticationUnknown),
	} {
		t.Run(name, func(t *testing.T) {
			if err := fact.Actionable(now); err == nil {
				t.Fatal("unsafe fact became actionable")
			}
		})
	}
}

func TestLocalProxyFactSeparatesFreshnessFromSemanticRevisionAndFailsClosedOnDrift(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	first := localProxyFactFixture("mixed", LocalProxyExposureLoopback, LocalProxyAuthenticationAbsent)
	refreshed := first
	refreshed.ObservedAt, refreshed.ExpiresAt = first.ObservedAt+10, first.ExpiresAt+10
	if err := refreshed.Validate(now.Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if first.FactRevision != refreshed.FactRevision {
		t.Fatal("freshness clock changed semantic fact revision")
	}
	drifted := first
	drifted.TLS = LocalProxyTLSEnabled
	if err := drifted.Validate(now); err == nil {
		t.Fatal("TLS drift with stale fact revision was accepted")
	}
	offline := first
	offline.EffectiveRuntimeRevision = ""
	offline.RuntimeReady = false
	offline.HealthCapabilityReady = false
	offline.ReasonCodes = []string{"RUNTIME_CONFIGURATION_UNPROVEN"}
	offline.FactRevision = Revision(localProxyFactRevisionInput(offline))
	if err := offline.Validate(now); err != nil {
		t.Fatalf("diagnostic offline fact rejected: %v", err)
	}
	if err := offline.Actionable(now); err == nil {
		t.Fatal("offline fact became actionable")
	}
}

func TestLocalProxyFactRejectsIncompleteProtocolAndUDPOrSystemProxyClaims(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	tests := []struct {
		name       string
		fact       LocalProxyFactV1
		diagnostic bool
	}{
		{"socks without socks5", localProxyFactFixture("socks", LocalProxyExposureLoopback, LocalProxyAuthenticationAbsent), false},
		{"http without connect", localProxyFactFixture("http", LocalProxyExposureLoopback, LocalProxyAuthenticationAbsent), false},
		{"wrong dependent udp", localProxyFactFixture("mixed", LocalProxyExposureLoopback, LocalProxyAuthenticationAbsent), false},
		{"system proxy enabled", localProxyFactFixture("http", LocalProxyExposureLoopback, LocalProxyAuthenticationAbsent), true},
	}
	tests[0].fact.Protocols = []LocalProxyProtocolV1{LocalProxyProtocolSOCKS4}
	tests[1].fact.Protocols = []LocalProxyProtocolV1{LocalProxyProtocolHTTPForward}
	tests[2].fact.DependentUDPAssociation = false
	tests[3].fact.SystemProxy = LocalProxySystemProxyEnabled
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.fact.FactRevision = Revision(localProxyFactRevisionInput(test.fact))
			if test.diagnostic {
				if err := test.fact.Validate(now); err != nil {
					t.Fatalf("diagnostic fact must remain valid: %v", err)
				}
				if _, err := ReferenceLocalProxyV1(test.fact, now); err == nil {
					t.Fatal("enabled system proxy produced an actionable reference")
				}
				return
			}
			if err := test.fact.Validate(now); err == nil {
				t.Fatal("invalid protocol or UDP semantics were accepted")
			}
		})
	}
}

func TestLocalProxyLeaseTransitionsRequireCASAndPreserveExactReference(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	reference, err := ReferenceLocalProxyV1(localProxyFactFixture("socks", LocalProxyExposureLoopback, LocalProxyAuthenticationAbsent), now)
	if err != nil {
		t.Fatal(err)
	}
	current, err := FinalizeLocalProxyGuardLeaseV1(LocalProxyGuardLeaseV1{
		LeaseID: "local-proxy-lease-1", AuthorityProviderID: "core", HolderID: "operation-1",
		ExactReference: reference, State: EndpointLeaseReserved, IssuedAt: now.Unix(),
		RenewedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(), ReasonCodes: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	next := current
	next.State = EndpointLeaseMutationPending
	next, err = FinalizeLocalProxyGuardLeaseV1(next)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateLocalProxyGuardLeaseTransitionV1(current, next, current.LeaseRevision, EndpointLeaseFence, now); err != nil {
		t.Fatal(err)
	}
	if err := ValidateLocalProxyGuardLeaseTransitionV1(current, next, Revision("stale"), EndpointLeaseFence, now); err == nil {
		t.Fatal("stale CAS revision was accepted")
	}
	next.ExactReference.Protocols = []LocalProxyProtocolV1{LocalProxyProtocolSOCKS4}
	next, _ = FinalizeLocalProxyGuardLeaseV1(next)
	if err := ValidateLocalProxyGuardLeaseTransitionV1(current, next, current.LeaseRevision, EndpointLeaseFence, now); err == nil {
		t.Fatal("lease authority reference changed during transition")
	}

	expiredAt := now.Add(2 * time.Minute)
	expired := current
	released := expired
	released.State, released.ReleasedAt = EndpointLeaseReleased, expiredAt.Unix()
	released, err = FinalizeLocalProxyGuardLeaseV1(released)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateLocalProxyGuardLeaseTransitionV1(expired, released, expired.LeaseRevision, EndpointLeaseRelease, expiredAt); err != nil {
		t.Fatalf("exact release of expired authority must remain recoverable: %v", err)
	}
	expiredFence := expired
	expiredFence.State = EndpointLeaseMutationPending
	expiredFence, err = FinalizeLocalProxyGuardLeaseV1(expiredFence)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateLocalProxyGuardLeaseTransitionV1(expired, expiredFence, expired.LeaseRevision, EndpointLeaseFence, expiredAt); err == nil {
		t.Fatal("expired authority crossed the mutation boundary")
	}
}
