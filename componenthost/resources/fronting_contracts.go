package resources

import (
	"context"
	"errors"
	"net/netip"
	"sort"
	"strings"
	"time"
)

const (
	FrontingBackendFactSchemaV1      = "solovey-ui/fronting-backend-fact/v1"
	FrontingBackendReferenceSchemaV1 = "solovey-ui/fronting-backend-reference/v1"
	EndpointLeaseSchemaV1            = "solovey-ui/endpoint-lease/v1"
	MaxFrontingFactFreshnessV1       = 5 * time.Minute
	MaxEndpointLeaseFreshnessV1      = 15 * time.Minute
	MaxFrontingReasonCodesV1         = 32
	MaxEndpointLeasePageV1           = 256
)

var ErrEndpointLeaseConflictV1 = errors.New("endpoint_lease_conflict_v1")

type ProxyMode string

const (
	ProxyModeOff ProxyMode = "OFF"
	ProxyModeOn  ProxyMode = "ON"
)

type EndpointClassification string

const (
	EndpointClassificationLocal   EndpointClassification = "LOCAL"
	EndpointClassificationPublic  EndpointClassification = "PUBLIC"
	EndpointClassificationUnknown EndpointClassification = "UNKNOWN"
)

type FrontingBackendClass string

const FrontingBackendOrdinaryTCP FrontingBackendClass = "ORDINARY_TCP_BACKEND"

type FrontingBackendResourceKind string

const FrontingBackendInboundResource FrontingBackendResourceKind = "inbound"

type FrontingBackendOwnership string

const (
	FrontingBackendProviderManaged  FrontingBackendOwnership = "PROVIDER_MANAGED"
	FrontingBackendExternalManaged  FrontingBackendOwnership = "EXTERNAL_MANAGED"
	FrontingBackendOwnershipUnknown FrontingBackendOwnership = "UNKNOWN"
)

// FrontingBackendFactV1 is a provider-authored, read-only observation. The
// address and port are deliberately internal to exact resolution and are not
// copied to FrontingBackendReferenceV1.
type FrontingBackendFactV1 struct {
	Schema               string                      `json:"schema"`
	ProviderID           string                      `json:"providerId"`
	ContributorID        string                      `json:"contributorId"`
	ResourceID           string                      `json:"resourceId"`
	EndpointID           string                      `json:"endpointId"`
	EndpointRevision     string                      `json:"endpointRevision"`
	OwnerRevision        string                      `json:"ownerRevision"`
	ProviderRevision     string                      `json:"providerRevision"`
	HealthRevision       string                      `json:"healthRevision"`
	CapacityRevision     string                      `json:"capacityRevision,omitempty"`
	BackendClass         FrontingBackendClass        `json:"backendClass"`
	ResourceKind         FrontingBackendResourceKind `json:"resourceKind"`
	Ownership            FrontingBackendOwnership    `json:"ownership"`
	Network              Network                     `json:"network"`
	AddressFamily        AddressFamily               `json:"addressFamily"`
	Classification       EndpointClassification      `json:"classification"`
	Transport            string                      `json:"transport"`
	AcceptsProxyProtocol CapabilityValue             `json:"acceptsProxyProtocol"`
	CanReachManagement   CapabilityValue             `json:"canReachManagement"`
	HealthReady          bool                        `json:"healthReady"`
	CapacityReady        bool                        `json:"capacityReady"`
	ObservedAt           int64                       `json:"observedAt"`
	ExpiresAt            int64                       `json:"expiresAt"`
	ReasonCodes          []string                    `json:"reasonCodes,omitempty"`
	address              netip.Addr
	port                 uint16
}

// NewFrontingBackendFactV1 accepts only an exact endpoint already present in a
// neutral resource-provider observation. There is deliberately no constructor
// from a caller-supplied host or port.
func NewFrontingBackendFactV1(value FrontingBackendFactV1, resource ProtectableResource, endpoint PublicEndpoint) (FrontingBackendFactV1, error) {
	address, err := netip.ParseAddr(endpoint.Key.BindAddress)
	endpointRevision := frontingEndpointRevisionV1(endpoint)
	if err != nil || address.Is4In6() || endpoint.Key.BindAddress != address.String() || endpoint.Schema != EndpointSchemaV1 ||
		resource.ID != endpoint.ResourceID || resource.Owner != endpoint.Owner || resource.Capabilities.OwnerRevision != endpoint.OwnerRevision ||
		!resource.Capabilities.Known || strings.TrimSpace(resource.Kind) != string(FrontingBackendInboundResource) ||
		endpoint.Intent != EndpointIntentLocal || endpoint.Key.Network != NetworkTCP ||
		(endpoint.Key.AddressFamily != AddressFamilyIPv4 && endpoint.Key.AddressFamily != AddressFamilyIPv6) ||
		(endpoint.Key.AddressFamily == AddressFamilyIPv4) != address.Is4() || !address.IsLoopback() || endpoint.Key.Port == 0 ||
		!frontingToken(endpoint.OwnerRevision, 128) || !frontingDigest(endpoint.ConfigurationRevision) ||
		endpoint.ObservedAt <= 0 || value.ObservedAt < endpoint.ObservedAt || value.ObservedAt-endpoint.ObservedAt > int64(MaxFrontingFactFreshnessV1/time.Second) ||
		len(endpoint.ReasonCodes) != 0 || !resourceContainsExactEndpointV1(resource, endpointRevision) {
		return FrontingBackendFactV1{}, errors.New("fronting_backend_provider_observation_v1_invalid")
	}
	value.Schema = FrontingBackendFactSchemaV1
	value.ResourceID, value.EndpointID, value.EndpointRevision = resource.ID, endpoint.ID, endpointRevision
	value.OwnerRevision, value.BackendClass = endpoint.OwnerRevision, FrontingBackendOrdinaryTCP
	value.ResourceKind, value.Network, value.AddressFamily = FrontingBackendInboundResource, endpoint.Key.Network, endpoint.Key.AddressFamily
	value.Classification, value.Transport = EndpointClassificationLocal, "tcp"
	value.AcceptsProxyProtocol = endpoint.ProxyProtocol
	value.address, value.port = address, endpoint.Key.Port
	value.ReasonCodes = canonicalFrontingReasons(value.ReasonCodes)
	if err := value.Validate(); err != nil {
		return FrontingBackendFactV1{}, err
	}
	return value, nil
}

func resourceContainsExactEndpointV1(resource ProtectableResource, endpointRevision string) bool {
	for _, candidate := range resource.Endpoints {
		if frontingEndpointRevisionV1(candidate) == endpointRevision {
			return true
		}
	}
	return false
}

func frontingEndpointRevisionV1(endpoint PublicEndpoint) string {
	endpoint.ObservedAt = 0
	return Revision(endpoint)
}

func (f FrontingBackendFactV1) Validate() error {
	if f.Schema != FrontingBackendFactSchemaV1 || !frontingToken(f.ProviderID, 128) ||
		!frontingToken(f.ContributorID, 128) || !frontingToken(f.ResourceID, 256) ||
		!frontingToken(f.EndpointID, 128) || !frontingDigest(f.EndpointRevision) ||
		!frontingToken(f.OwnerRevision, 128) || !frontingToken(f.ProviderRevision, 128) ||
		!frontingDigest(f.HealthRevision) || f.CapacityRevision != "" && !frontingDigest(f.CapacityRevision) ||
		f.BackendClass != FrontingBackendOrdinaryTCP || f.ResourceKind != FrontingBackendInboundResource || f.Ownership != FrontingBackendProviderManaged ||
		f.Network != NetworkTCP || (f.AddressFamily != AddressFamilyIPv4 && f.AddressFamily != AddressFamilyIPv6) ||
		f.Classification != EndpointClassificationLocal || strings.ToLower(f.Transport) != "tcp" ||
		(f.AcceptsProxyProtocol != CapabilityYes && f.AcceptsProxyProtocol != CapabilityNo && f.AcceptsProxyProtocol != CapabilityUnknown) ||
		(f.CanReachManagement != CapabilityYes && f.CanReachManagement != CapabilityNo && f.CanReachManagement != CapabilityUnknown) ||
		!f.address.IsValid() || !f.address.IsLoopback() || f.port == 0 ||
		(f.AddressFamily == AddressFamilyIPv4) != f.address.Is4() || f.ObservedAt <= 0 ||
		f.ExpiresAt <= f.ObservedAt || f.ExpiresAt-f.ObservedAt > int64(MaxFrontingFactFreshnessV1/time.Second) ||
		!validFrontingReasons(f.ReasonCodes) {
		return errors.New("fronting_backend_fact_v1_invalid")
	}
	return nil
}

type FrontingBackendReferenceV1 struct {
	Schema                     string                      `json:"schema"`
	ProviderID                 string                      `json:"providerId"`
	ContributorID              string                      `json:"contributorId"`
	ResourceID                 string                      `json:"resourceId"`
	EndpointID                 string                      `json:"endpointId"`
	EndpointRevision           string                      `json:"endpointRevision"`
	OwnerRevision              string                      `json:"ownerRevision"`
	ProviderRevision           string                      `json:"providerRevision"`
	HealthRevision             string                      `json:"healthRevision"`
	CapacityRevision           string                      `json:"capacityRevision,omitempty"`
	BackendClass               FrontingBackendClass        `json:"backendClass"`
	ResourceKind               FrontingBackendResourceKind `json:"resourceKind"`
	Ownership                  FrontingBackendOwnership    `json:"ownership"`
	Network                    Network                     `json:"network"`
	AddressFamily              AddressFamily               `json:"addressFamily"`
	Classification             EndpointClassification      `json:"classification"`
	SelectedTransport          string                      `json:"selectedTransport"`
	SelectedProxyMode          ProxyMode                   `json:"selectedProxyMode"`
	ManagementReachability     CapabilityValue             `json:"managementReachability"`
	CanonicalReferenceRevision string                      `json:"canonicalReferenceRevision"`
}

func ReferenceFrontingBackendV1(fact FrontingBackendFactV1, mode ProxyMode, now time.Time) (FrontingBackendReferenceV1, error) {
	if err := fact.Validate(); err != nil || fact.ExpiresAt <= now.UTC().Unix() || !fact.HealthReady || !fact.CapacityReady || len(fact.ReasonCodes) != 0 {
		return FrontingBackendReferenceV1{}, errors.New("fronting_backend_fact_v1_not_actionable")
	}
	if fact.CanReachManagement != CapabilityNo {
		return FrontingBackendReferenceV1{}, errors.New("fronting_backend_management_reachability_unproven")
	}
	if mode != ProxyModeOff && mode != ProxyModeOn || mode == ProxyModeOn && fact.AcceptsProxyProtocol != CapabilityYes {
		return FrontingBackendReferenceV1{}, errors.New("fronting_backend_proxy_mode_unproven")
	}
	value := FrontingBackendReferenceV1{
		Schema: FrontingBackendReferenceSchemaV1, ProviderID: fact.ProviderID, ContributorID: fact.ContributorID,
		ResourceID: fact.ResourceID, EndpointID: fact.EndpointID, EndpointRevision: fact.EndpointRevision,
		OwnerRevision: fact.OwnerRevision, ProviderRevision: fact.ProviderRevision, HealthRevision: fact.HealthRevision,
		CapacityRevision: fact.CapacityRevision, BackendClass: fact.BackendClass, ResourceKind: fact.ResourceKind, Ownership: fact.Ownership,
		Network: fact.Network, AddressFamily: fact.AddressFamily,
		Classification: fact.Classification, SelectedTransport: "tcp", SelectedProxyMode: mode,
		ManagementReachability: fact.CanReachManagement,
	}
	value.CanonicalReferenceRevision = Revision(frontingBackendReferenceRevisionInput(value))
	return value, nil
}

func (r FrontingBackendReferenceV1) Validate() error {
	if r.Schema != FrontingBackendReferenceSchemaV1 || !frontingToken(r.ProviderID, 128) ||
		!frontingToken(r.ContributorID, 128) || !frontingToken(r.ResourceID, 256) || !frontingToken(r.EndpointID, 128) ||
		!frontingDigest(r.EndpointRevision) || !frontingToken(r.OwnerRevision, 128) || !frontingToken(r.ProviderRevision, 128) ||
		!frontingDigest(r.HealthRevision) || r.CapacityRevision != "" && !frontingDigest(r.CapacityRevision) ||
		r.BackendClass != FrontingBackendOrdinaryTCP || r.ResourceKind != FrontingBackendInboundResource || r.Ownership != FrontingBackendProviderManaged ||
		r.Network != NetworkTCP || (r.AddressFamily != AddressFamilyIPv4 && r.AddressFamily != AddressFamilyIPv6) ||
		r.Classification != EndpointClassificationLocal || r.SelectedTransport != "tcp" ||
		(r.SelectedProxyMode != ProxyModeOff && r.SelectedProxyMode != ProxyModeOn) || r.ManagementReachability != CapabilityNo ||
		!frontingDigest(r.CanonicalReferenceRevision) || r.CanonicalReferenceRevision != Revision(frontingBackendReferenceRevisionInput(r)) {
		return errors.New("fronting_backend_reference_v1_invalid")
	}
	return nil
}

func ResolveExactFrontingBackendV1(reference FrontingBackendReferenceV1, fact FrontingBackendFactV1, now time.Time) error {
	if err := reference.Validate(); err != nil {
		return err
	}
	want, err := ReferenceFrontingBackendV1(fact, reference.SelectedProxyMode, now)
	if err != nil {
		return err
	}
	if want != reference {
		return errors.New("fronting_backend_reference_v1_stale")
	}
	return nil
}

// FrontingBackendEndpointV1 is an execution-only projection of an exact,
// provider-authored backend fact. It cannot be constructed from a reference
// alone, so callers cannot turn an API/UI host or port into a destination.
type FrontingBackendEndpointV1 struct {
	Address       netip.Addr
	Port          uint16
	AddressFamily AddressFamily
	Network       Network
}

func ResolveFrontingBackendEndpointV1(reference FrontingBackendReferenceV1, fact FrontingBackendFactV1, now time.Time) (FrontingBackendEndpointV1, error) {
	if err := ResolveExactFrontingBackendV1(reference, fact, now); err != nil {
		return FrontingBackendEndpointV1{}, err
	}
	return FrontingBackendEndpointV1{Address: fact.address, Port: fact.port, AddressFamily: fact.AddressFamily, Network: fact.Network}, nil
}

func frontingBackendReferenceRevisionInput(value FrontingBackendReferenceV1) FrontingBackendReferenceV1 {
	value.CanonicalReferenceRevision = ""
	return value
}

type EndpointLeaseState string

const (
	EndpointLeaseReserved          EndpointLeaseState = "RESERVED"
	EndpointLeaseMutationPending   EndpointLeaseState = "MUTATION_PENDING"
	EndpointLeaseActive            EndpointLeaseState = "ACTIVE"
	EndpointLeaseReconcileRequired EndpointLeaseState = "RECONCILE_REQUIRED"
	EndpointLeaseReleased          EndpointLeaseState = "RELEASED"
)

type EndpointLeaseMutation string

const (
	EndpointLeaseFence    EndpointLeaseMutation = "FENCE_FOR_MUTATION"
	EndpointLeaseActivate EndpointLeaseMutation = "ACTIVATE"
	EndpointLeaseRenew    EndpointLeaseMutation = "RENEW"
	EndpointLeaseRelease  EndpointLeaseMutation = "RELEASE"
)

// EndpointLeaseV1 is a pure provider-authority/CAS value. Mutations remain on
// the separate provider interface; changing or deleting a mirror cannot
// release provider authority.
type EndpointLeaseV1 struct {
	Schema              string                     `json:"schema"`
	LeaseID             string                     `json:"leaseId"`
	LeaseRevision       string                     `json:"leaseRevision"`
	AuthorityProviderID string                     `json:"authorityProviderId"`
	HolderID            string                     `json:"holderId"`
	ExactReference      FrontingBackendReferenceV1 `json:"exactReference"`
	State               EndpointLeaseState         `json:"state"`
	IssuedAt            int64                      `json:"issuedAt"`
	RenewedAt           int64                      `json:"renewedAt"`
	ExpiresAt           int64                      `json:"expiresAt"`
	ReleasedAt          int64                      `json:"releasedAt,omitempty"`
	ReasonCodes         []string                   `json:"reasonCodes,omitempty"`
}

type EndpointLeaseCASV1 struct {
	RequestID        string `json:"requestId"`
	LeaseID          string `json:"leaseId"`
	ExpectedRevision string `json:"expectedRevision"`
}

type EndpointLeasePurposeV1 string

const EndpointLeasePurposeL4FrontingV1 EndpointLeasePurposeV1 = "L4_FRONTING"

type AcquireEndpointLeaseRequestV1 struct {
	RequestID        string                     `json:"requestId"`
	HolderID         string                     `json:"holderId"`
	Purpose          EndpointLeasePurposeV1     `json:"purpose"`
	ExactReference   FrontingBackendReferenceV1 `json:"exactReference"`
	FreshnessSeconds uint32                     `json:"freshnessSeconds"`
}

type MutateEndpointLeaseRequestV1 struct {
	RequestID        string `json:"requestId"`
	LeaseID          string `json:"leaseId"`
	ExpectedRevision string `json:"expectedRevision"`
	FreshnessSeconds uint32 `json:"freshnessSeconds,omitempty"`
}

type ReleaseEndpointLeaseRequestV1 struct {
	RequestID          string `json:"requestId"`
	LeaseID            string `json:"leaseId"`
	ExpectedRevision   string `json:"expectedRevision"`
	DetachmentRevision string `json:"detachmentRevision"`
}

type GetEndpointLeaseRequestV1 struct {
	LeaseID string `json:"leaseId"`
}

type ListEndpointLeasesRequestV1 struct {
	HolderID string `json:"holderId"`
	Limit    uint32 `json:"limit"`
}

// EndpointLeaseProviderV1 is the provider-owned endpoint lease authority.
// Consumers persist only returned mirrors; they have no
// release-by-delete or force transition surface.
type EndpointLeaseProviderV1 interface {
	ProviderID() string
	AcquireEndpointLease(context.Context, AcquireEndpointLeaseRequestV1) (EndpointLeaseV1, error)
	FenceEndpointLease(context.Context, MutateEndpointLeaseRequestV1) (EndpointLeaseV1, error)
	ActivateEndpointLease(context.Context, MutateEndpointLeaseRequestV1) (EndpointLeaseV1, error)
	ReleaseEndpointLease(context.Context, ReleaseEndpointLeaseRequestV1) (EndpointLeaseV1, error)
	GetEndpointLease(context.Context, GetEndpointLeaseRequestV1) (EndpointLeaseV1, error)
	ListEndpointLeases(context.Context, ListEndpointLeasesRequestV1) ([]EndpointLeaseV1, error)
}

func (r AcquireEndpointLeaseRequestV1) Validate() error {
	if !frontingToken(r.RequestID, 128) || !frontingToken(r.HolderID, 128) || r.Purpose != EndpointLeasePurposeL4FrontingV1 ||
		r.FreshnessSeconds == 0 || time.Duration(r.FreshnessSeconds)*time.Second > MaxEndpointLeaseFreshnessV1 || r.ExactReference.Validate() != nil {
		return errors.New("endpoint_lease_acquire_request_v1_invalid")
	}
	return nil
}

func (r MutateEndpointLeaseRequestV1) Validate(requireFreshness bool) error {
	if !frontingToken(r.RequestID, 128) || !frontingToken(r.LeaseID, 128) || !frontingDigest(r.ExpectedRevision) ||
		requireFreshness && (r.FreshnessSeconds == 0 || time.Duration(r.FreshnessSeconds)*time.Second > MaxEndpointLeaseFreshnessV1) ||
		!requireFreshness && r.FreshnessSeconds != 0 {
		return errors.New("endpoint_lease_mutation_request_v1_invalid")
	}
	return nil
}

func (r ReleaseEndpointLeaseRequestV1) Validate() error {
	if !frontingToken(r.RequestID, 128) || !frontingToken(r.LeaseID, 128) || !frontingDigest(r.ExpectedRevision) || !frontingDigest(r.DetachmentRevision) {
		return errors.New("endpoint_lease_release_request_v1_invalid")
	}
	return nil
}

func (r GetEndpointLeaseRequestV1) Validate() error {
	if !frontingToken(r.LeaseID, 128) {
		return errors.New("endpoint_lease_get_request_v1_invalid")
	}
	return nil
}

func (r ListEndpointLeasesRequestV1) Validate() error {
	if !frontingToken(r.HolderID, 128) || r.Limit == 0 || r.Limit > MaxEndpointLeasePageV1 {
		return errors.New("endpoint_lease_list_request_v1_invalid")
	}
	return nil
}

func (l EndpointLeaseV1) Validate() error {
	if l.Schema != EndpointLeaseSchemaV1 || !frontingToken(l.LeaseID, 128) || !frontingDigest(l.LeaseRevision) ||
		!frontingToken(l.AuthorityProviderID, 128) || !frontingToken(l.HolderID, 128) ||
		l.AuthorityProviderID != l.ExactReference.ProviderID || l.IssuedAt <= 0 || l.RenewedAt < l.IssuedAt ||
		l.ExpiresAt <= l.RenewedAt || l.ExpiresAt-l.RenewedAt > int64(MaxEndpointLeaseFreshnessV1/time.Second) ||
		!validFrontingReasons(l.ReasonCodes) || l.LeaseRevision != Revision(endpointLeaseRevisionInput(l)) {
		return errors.New("endpoint_lease_v1_invalid")
	}
	if err := l.ExactReference.Validate(); err != nil {
		return errors.New("endpoint_lease_reference_v1_invalid")
	}
	switch l.State {
	case EndpointLeaseReserved, EndpointLeaseMutationPending, EndpointLeaseActive, EndpointLeaseReconcileRequired:
		if l.ReleasedAt != 0 {
			return errors.New("endpoint_lease_release_time_invalid")
		}
	case EndpointLeaseReleased:
		if l.ReleasedAt < l.RenewedAt {
			return errors.New("endpoint_lease_release_time_invalid")
		}
	default:
		return errors.New("endpoint_lease_state_invalid")
	}
	return nil
}

func FinalizeEndpointLeaseV1(value EndpointLeaseV1) (EndpointLeaseV1, error) {
	value.Schema = EndpointLeaseSchemaV1
	value.ReasonCodes = canonicalFrontingReasons(value.ReasonCodes)
	value.LeaseRevision = Revision(endpointLeaseRevisionInput(value))
	return value, value.Validate()
}

func ValidateEndpointLeaseTransitionV1(current, next EndpointLeaseV1, cas EndpointLeaseCASV1, mutation EndpointLeaseMutation, now time.Time) error {
	if err := current.Validate(); err != nil || current.ExpiresAt <= now.UTC().Unix() || !frontingToken(cas.RequestID, 128) ||
		cas.LeaseID != current.LeaseID || cas.ExpectedRevision != current.LeaseRevision {
		return errors.New("endpoint_lease_cas_stale")
	}
	if err := next.Validate(); err != nil {
		return err
	}
	if next.LeaseID != current.LeaseID || next.AuthorityProviderID != current.AuthorityProviderID || next.HolderID != current.HolderID ||
		next.ExactReference != current.ExactReference || next.IssuedAt != current.IssuedAt || next.LeaseRevision == current.LeaseRevision ||
		next.RenewedAt < current.RenewedAt || next.ExpiresAt < current.ExpiresAt {
		return errors.New("endpoint_lease_transition_conflict")
	}
	want, ok := endpointLeaseTransition(current.State, mutation)
	if !ok || next.State != want || (mutation == EndpointLeaseRelease) != (next.ReleasedAt > 0) {
		return errors.New("endpoint_lease_transition_illegal")
	}
	if mutation == EndpointLeaseRenew && (next.RenewedAt <= current.RenewedAt || next.ExpiresAt <= current.ExpiresAt) {
		return errors.New("endpoint_lease_renewal_not_advanced")
	}
	return nil
}

func endpointLeaseTransition(current EndpointLeaseState, mutation EndpointLeaseMutation) (EndpointLeaseState, bool) {
	switch mutation {
	case EndpointLeaseFence:
		return EndpointLeaseMutationPending, current == EndpointLeaseReserved
	case EndpointLeaseActivate:
		return EndpointLeaseActive, current == EndpointLeaseMutationPending
	case EndpointLeaseRenew:
		return EndpointLeaseActive, current == EndpointLeaseActive
	case EndpointLeaseRelease:
		return EndpointLeaseReleased, current != EndpointLeaseReleased
	default:
		return "", false
	}
}

func endpointLeaseRevisionInput(value EndpointLeaseV1) EndpointLeaseV1 {
	value.LeaseRevision = ""
	return value
}

func frontingToken(value string, limit int) bool {
	if value == "" || value != strings.TrimSpace(value) || len(value) > limit || strings.ContainsAny(value, "/\\?#&={}[]<>\"'\r\n\t ") {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._:@+-", r) {
			continue
		}
		return false
	}
	return true
}

func frontingDigest(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	for _, r := range value {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func validFrontingReasons(values []string) bool {
	if len(values) > MaxFrontingReasonCodesV1 {
		return false
	}
	for i, value := range values {
		if !frontingToken(value, 64) || i > 0 && values[i-1] >= value {
			return false
		}
	}
	return true
}

func canonicalFrontingReasons(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, min(len(values), MaxFrontingReasonCodesV1))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if !frontingToken(value, 64) {
			value = "fronting_reason_invalid"
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
		if len(result) == MaxFrontingReasonCodesV1 {
			break
		}
	}
	sort.Strings(result)
	return result
}
