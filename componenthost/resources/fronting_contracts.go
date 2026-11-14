package resources

import (
	canonical "github.com/MalenkiySolovey/solovey-host/componenthost/resources"
	time "time"
)

const FrontingBackendFactSchemaV1 = canonical.FrontingBackendFactSchemaV1
const FrontingBackendReferenceSchemaV1 = canonical.FrontingBackendReferenceSchemaV1
const EndpointLeaseSchemaV1 = canonical.EndpointLeaseSchemaV1
const MaxFrontingFactFreshnessV1 = canonical.MaxFrontingFactFreshnessV1
const MaxEndpointLeaseFreshnessV1 = canonical.MaxEndpointLeaseFreshnessV1
const MaxFrontingReasonCodesV1 = canonical.MaxFrontingReasonCodesV1
const MaxEndpointLeasePageV1 = canonical.MaxEndpointLeasePageV1

var ErrEndpointLeaseConflictV1 = canonical.ErrEndpointLeaseConflictV1

type ProxyMode = canonical.ProxyMode

const ProxyModeOff = canonical.ProxyModeOff
const ProxyModeOn = canonical.ProxyModeOn

type EndpointClassification = canonical.EndpointClassification

const EndpointClassificationLocal = canonical.EndpointClassificationLocal
const EndpointClassificationPublic = canonical.EndpointClassificationPublic
const EndpointClassificationUnknown = canonical.EndpointClassificationUnknown

type FrontingBackendClass = canonical.FrontingBackendClass

const FrontingBackendOrdinaryTCP = canonical.FrontingBackendOrdinaryTCP

type FrontingBackendResourceKind = canonical.FrontingBackendResourceKind

const FrontingBackendInboundResource = canonical.FrontingBackendInboundResource

type FrontingBackendOwnership = canonical.FrontingBackendOwnership

const FrontingBackendProviderManaged = canonical.FrontingBackendProviderManaged
const FrontingBackendExternalManaged = canonical.FrontingBackendExternalManaged
const FrontingBackendOwnershipUnknown = canonical.FrontingBackendOwnershipUnknown

type FrontingBackendFactV1 = canonical.FrontingBackendFactV1

// NewFrontingBackendFactV1 accepts only an exact endpoint already present in a
// neutral resource-provider observation. There is deliberately no constructor
// from a caller-supplied host or port.
func NewFrontingBackendFactV1(value FrontingBackendFactV1, resource ProtectableResource, endpoint PublicEndpoint) (FrontingBackendFactV1, error) {
	return canonical.NewFrontingBackendFactV1(value, resource, endpoint)
}

type FrontingBackendReferenceV1 = canonical.FrontingBackendReferenceV1

func ReferenceFrontingBackendV1(fact FrontingBackendFactV1, mode ProxyMode, now time.Time) (FrontingBackendReferenceV1, error) {
	return canonical.ReferenceFrontingBackendV1(fact, mode, now)
}

func ResolveExactFrontingBackendV1(reference FrontingBackendReferenceV1, fact FrontingBackendFactV1, now time.Time) error {
	return canonical.ResolveExactFrontingBackendV1(reference, fact, now)
}

type FrontingBackendEndpointV1 = canonical.FrontingBackendEndpointV1

func ResolveFrontingBackendEndpointV1(reference FrontingBackendReferenceV1, fact FrontingBackendFactV1, now time.Time) (FrontingBackendEndpointV1, error) {
	return canonical.ResolveFrontingBackendEndpointV1(reference, fact, now)
}

type EndpointLeaseState = canonical.EndpointLeaseState

const EndpointLeaseReserved = canonical.EndpointLeaseReserved
const EndpointLeaseMutationPending = canonical.EndpointLeaseMutationPending
const EndpointLeaseActive = canonical.EndpointLeaseActive
const EndpointLeaseReconcileRequired = canonical.EndpointLeaseReconcileRequired
const EndpointLeaseReleased = canonical.EndpointLeaseReleased

type EndpointLeaseMutation = canonical.EndpointLeaseMutation

const EndpointLeaseFence = canonical.EndpointLeaseFence
const EndpointLeaseActivate = canonical.EndpointLeaseActivate
const EndpointLeaseRenew = canonical.EndpointLeaseRenew
const EndpointLeaseRelease = canonical.EndpointLeaseRelease

type EndpointLeaseV1 = canonical.EndpointLeaseV1
type EndpointLeaseCASV1 = canonical.EndpointLeaseCASV1
type EndpointLeasePurposeV1 = canonical.EndpointLeasePurposeV1

const EndpointLeasePurposeL4FrontingV1 = canonical.EndpointLeasePurposeL4FrontingV1

type AcquireEndpointLeaseRequestV1 = canonical.AcquireEndpointLeaseRequestV1
type MutateEndpointLeaseRequestV1 = canonical.MutateEndpointLeaseRequestV1
type ReleaseEndpointLeaseRequestV1 = canonical.ReleaseEndpointLeaseRequestV1
type GetEndpointLeaseRequestV1 = canonical.GetEndpointLeaseRequestV1
type ListEndpointLeasesRequestV1 = canonical.ListEndpointLeasesRequestV1
type EndpointLeaseProviderV1 = canonical.EndpointLeaseProviderV1

func FinalizeEndpointLeaseV1(value EndpointLeaseV1) (EndpointLeaseV1, error) {
	return canonical.FinalizeEndpointLeaseV1(value)
}

func ValidateEndpointLeaseTransitionV1(current, next EndpointLeaseV1, cas EndpointLeaseCASV1, mutation EndpointLeaseMutation, now time.Time) error {
	return canonical.ValidateEndpointLeaseTransitionV1(current, next, cas, mutation, now)
}

func frontingToken(value string, limit int) bool {
	return canonical.FrontingContractTokenValidV1(value, limit)
}
