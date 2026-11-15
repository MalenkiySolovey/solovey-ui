package resources

import (
	hostsurface "github.com/MalenkiySolovey/solovey-host/componenthost/hostsurface"
	canonical "github.com/MalenkiySolovey/solovey-host/componenthost/resources"
	time "time"
)

const LocalProxyFactSchemaV1 = canonical.LocalProxyFactSchemaV1
const LocalProxyReferenceSchemaV1 = canonical.LocalProxyReferenceSchemaV1
const LocalProxyGuardLeaseSchemaV1 = canonical.LocalProxyGuardLeaseSchemaV1
const MaxLocalProxyFactFreshnessV1 = canonical.MaxLocalProxyFactFreshnessV1
const MaxLocalProxyLeaseFreshnessV1 = canonical.MaxLocalProxyLeaseFreshnessV1
const MaxLocalProxyProtocolsV1 = canonical.MaxLocalProxyProtocolsV1
const MaxLocalProxyReasonCodesV1 = canonical.MaxLocalProxyReasonCodesV1
const MaxLocalProxyGuardLeasePageV1 = canonical.MaxLocalProxyGuardLeasePageV1
const LocalProxyGuardPurposeV1 = canonical.LocalProxyGuardPurposeV1
const LocalProxyProviderRevisionV1 = canonical.LocalProxyProviderRevisionV1
const LocalProxyCapabilityRevisionV1 = canonical.LocalProxyCapabilityRevisionV1
const LocalProxyManagementExclusionV1 = canonical.LocalProxyManagementExclusionV1

var ErrLocalProxyGuardLeaseConflictV1 = canonical.ErrLocalProxyGuardLeaseConflictV1

type LocalProxyProtocolV1 = canonical.LocalProxyProtocolV1

const LocalProxyProtocolSOCKS4 = canonical.LocalProxyProtocolSOCKS4
const LocalProxyProtocolSOCKS5 = canonical.LocalProxyProtocolSOCKS5
const LocalProxyProtocolHTTPForward = canonical.LocalProxyProtocolHTTPForward
const LocalProxyProtocolHTTPConnect = canonical.LocalProxyProtocolHTTPConnect

type LocalProxyExposureV1 = canonical.LocalProxyExposureV1

const LocalProxyExposureLoopback = canonical.LocalProxyExposureLoopback
const LocalProxyExposurePrivate = canonical.LocalProxyExposurePrivate
const LocalProxyExposurePublic = canonical.LocalProxyExposurePublic
const LocalProxyExposureWildcard = canonical.LocalProxyExposureWildcard
const LocalProxyExposureUnspecified = canonical.LocalProxyExposureUnspecified
const LocalProxyExposureUnknown = canonical.LocalProxyExposureUnknown

type LocalProxyOwnershipV1 = canonical.LocalProxyOwnershipV1

const LocalProxyProviderManaged = canonical.LocalProxyProviderManaged
const LocalProxyExternalManaged = canonical.LocalProxyExternalManaged
const LocalProxyOwnershipUnknown = canonical.LocalProxyOwnershipUnknown

type LocalProxyAuthenticationV1 = canonical.LocalProxyAuthenticationV1

const LocalProxyAuthenticationPresent = canonical.LocalProxyAuthenticationPresent
const LocalProxyAuthenticationAbsent = canonical.LocalProxyAuthenticationAbsent
const LocalProxyAuthenticationUnknown = canonical.LocalProxyAuthenticationUnknown

type LocalProxyTLSStateV1 = canonical.LocalProxyTLSStateV1

const LocalProxyTLSEnabled = canonical.LocalProxyTLSEnabled
const LocalProxyTLSDisabled = canonical.LocalProxyTLSDisabled
const LocalProxyTLSUnknown = canonical.LocalProxyTLSUnknown

type LocalProxySystemProxyStateV1 = canonical.LocalProxySystemProxyStateV1

const LocalProxySystemProxyEnabled = canonical.LocalProxySystemProxyEnabled
const LocalProxySystemProxyDisabled = canonical.LocalProxySystemProxyDisabled
const LocalProxySystemProxyUnknown = canonical.LocalProxySystemProxyUnknown

type LocalProxyListenerStateV1 = canonical.LocalProxyListenerStateV1

const LocalProxyListenerObservedExact = canonical.LocalProxyListenerObservedExact
const LocalProxyListenerUnobserved = canonical.LocalProxyListenerUnobserved
const LocalProxyListenerStale = canonical.LocalProxyListenerStale
const LocalProxyListenerForeign = canonical.LocalProxyListenerForeign
const LocalProxyListenerUnknown = canonical.LocalProxyListenerUnknown

type LocalProxyFactV1 = canonical.LocalProxyFactV1

// NewLocalProxyFactV1 binds a core-authored semantic fact to an exact neutral
// resource endpoint and, when available, the exact listener-owner observation.
// There is deliberately no constructor from caller-supplied host/port data.
func NewLocalProxyFactV1(value LocalProxyFactV1, resource ProtectableResource, endpoint PublicEndpoint, observed *hostsurface.HostSurfaceFactV1) (LocalProxyFactV1, error) {
	return canonical.NewLocalProxyFactV1(value, resource, endpoint, observed)
}

type LocalProxyReferenceV1 = canonical.LocalProxyReferenceV1

func ReferenceLocalProxyV1(fact LocalProxyFactV1, now time.Time) (LocalProxyReferenceV1, error) {
	return canonical.ReferenceLocalProxyV1(fact, now)
}

func ResolveExactLocalProxyV1(reference LocalProxyReferenceV1, fact LocalProxyFactV1, now time.Time) error {
	return canonical.ResolveExactLocalProxyV1(reference, fact, now)
}

type LocalProxyGuardLeaseV1 = canonical.LocalProxyGuardLeaseV1
type AcquireLocalProxyGuardLeaseRequestV1 = canonical.AcquireLocalProxyGuardLeaseRequestV1
type MutateLocalProxyGuardLeaseRequestV1 = canonical.MutateLocalProxyGuardLeaseRequestV1
type ReleaseLocalProxyGuardLeaseRequestV1 = canonical.ReleaseLocalProxyGuardLeaseRequestV1
type GetLocalProxyGuardLeaseRequestV1 = canonical.GetLocalProxyGuardLeaseRequestV1
type ListLocalProxyGuardLeasesRequestV1 = canonical.ListLocalProxyGuardLeasesRequestV1
type LocalProxyProviderV1 = canonical.LocalProxyProviderV1

func FinalizeLocalProxyGuardLeaseV1(value LocalProxyGuardLeaseV1) (LocalProxyGuardLeaseV1, error) {
	return canonical.FinalizeLocalProxyGuardLeaseV1(value)
}

func ValidateLocalProxyGuardLeaseTransitionV1(current, next LocalProxyGuardLeaseV1, expectedRevision string, mutation EndpointLeaseMutation, now time.Time) error {
	return canonical.ValidateLocalProxyGuardLeaseTransitionV1(current, next, expectedRevision, mutation, now)
}
