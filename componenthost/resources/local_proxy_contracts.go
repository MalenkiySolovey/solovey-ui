package resources

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"sort"
	"strings"
	"time"

	hostsurface "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
)

const (
	LocalProxyFactSchemaV1          = "solovey-ui/local-proxy-fact/v1"
	LocalProxyReferenceSchemaV1     = "solovey-ui/local-proxy-reference/v1"
	LocalProxyGuardLeaseSchemaV1    = "solovey-ui/local-proxy-guard-lease/v1"
	MaxLocalProxyFactFreshnessV1    = 5 * time.Minute
	MaxLocalProxyLeaseFreshnessV1   = 15 * time.Minute
	MaxLocalProxyProtocolsV1        = 4
	MaxLocalProxyReasonCodesV1      = 32
	MaxLocalProxyGuardLeasePageV1   = 256
	LocalProxyGuardPurposeV1        = "LOCAL_PROXY_GUARD"
	LocalProxyProviderRevisionV1    = "core-local-proxy-provider-v1"
	LocalProxyCapabilityRevisionV1  = "local-proxy-socks-http-mixed-v1"
	LocalProxyManagementExclusionV1 = "local-proxy-management-exclusion-v1"
)

var ErrLocalProxyGuardLeaseConflictV1 = errors.New("local_proxy_guard_lease_conflict_v1")

type LocalProxyProtocolV1 string

const (
	LocalProxyProtocolSOCKS4      LocalProxyProtocolV1 = "SOCKS4"
	LocalProxyProtocolSOCKS5      LocalProxyProtocolV1 = "SOCKS5"
	LocalProxyProtocolHTTPForward LocalProxyProtocolV1 = "HTTP_FORWARD"
	LocalProxyProtocolHTTPConnect LocalProxyProtocolV1 = "HTTP_CONNECT"
)

type LocalProxyExposureV1 string

const (
	LocalProxyExposureLoopback    LocalProxyExposureV1 = "LOOPBACK"
	LocalProxyExposurePrivate     LocalProxyExposureV1 = "PRIVATE"
	LocalProxyExposurePublic      LocalProxyExposureV1 = "PUBLIC"
	LocalProxyExposureWildcard    LocalProxyExposureV1 = "WILDCARD"
	LocalProxyExposureUnspecified LocalProxyExposureV1 = "UNSPECIFIED"
	LocalProxyExposureUnknown     LocalProxyExposureV1 = "UNKNOWN"
)

type LocalProxyOwnershipV1 string

const (
	LocalProxyProviderManaged  LocalProxyOwnershipV1 = "PROVIDER_MANAGED"
	LocalProxyExternalManaged  LocalProxyOwnershipV1 = "EXTERNAL_MANAGED"
	LocalProxyOwnershipUnknown LocalProxyOwnershipV1 = "UNKNOWN"
)

type LocalProxyAuthenticationV1 string

const (
	LocalProxyAuthenticationPresent LocalProxyAuthenticationV1 = "PRESENT"
	LocalProxyAuthenticationAbsent  LocalProxyAuthenticationV1 = "ABSENT"
	LocalProxyAuthenticationUnknown LocalProxyAuthenticationV1 = "UNKNOWN"
)

type LocalProxyTLSStateV1 string

const (
	LocalProxyTLSEnabled  LocalProxyTLSStateV1 = "ENABLED"
	LocalProxyTLSDisabled LocalProxyTLSStateV1 = "DISABLED"
	LocalProxyTLSUnknown  LocalProxyTLSStateV1 = "UNKNOWN"
)

type LocalProxySystemProxyStateV1 string

const (
	LocalProxySystemProxyEnabled  LocalProxySystemProxyStateV1 = "ENABLED"
	LocalProxySystemProxyDisabled LocalProxySystemProxyStateV1 = "DISABLED"
	LocalProxySystemProxyUnknown  LocalProxySystemProxyStateV1 = "UNKNOWN"
)

type LocalProxyListenerStateV1 string

const (
	LocalProxyListenerObservedExact LocalProxyListenerStateV1 = "OBSERVED_EXACT"
	LocalProxyListenerUnobserved    LocalProxyListenerStateV1 = "UNOBSERVED"
	LocalProxyListenerStale         LocalProxyListenerStateV1 = "STALE"
	LocalProxyListenerForeign       LocalProxyListenerStateV1 = "FOREIGN"
	LocalProxyListenerUnknown       LocalProxyListenerStateV1 = "UNKNOWN"
)

// LocalProxyFactV1 is a secret-free provider observation. ConfiguredBind and
// ConfiguredPort are intentionally read-only facts; they are never accepted by
// a reference or mutation request.
type LocalProxyFactV1 struct {
	Schema                      string                       `json:"schema"`
	ProviderID                  string                       `json:"providerId"`
	ContributorID               string                       `json:"contributorId"`
	ResourceID                  string                       `json:"resourceId"`
	EndpointID                  string                       `json:"endpointId"`
	InboundDatabaseID           uint                         `json:"inboundDatabaseId"`
	InboundType                 string                       `json:"inboundType"`
	ConfigurationRevision       string                       `json:"configurationRevision"`
	EffectiveRuntimeRevision    string                       `json:"effectiveRuntimeRevision"`
	RuntimeIdentityRevision     string                       `json:"runtimeIdentityRevision"`
	ProviderRevision            string                       `json:"providerRevision"`
	CapabilityRevision          string                       `json:"capabilityRevision"`
	ListenerObservationRevision string                       `json:"listenerObservationRevision,omitempty"`
	OwnerRevision               string                       `json:"ownerRevision"`
	HealthRevision              string                       `json:"healthRevision"`
	CapacityRevision            string                       `json:"capacityRevision"`
	ManagementExclusionRevision string                       `json:"managementExclusionRevision"`
	RecoveryPathRevision        string                       `json:"recoveryPathRevision"`
	FactRevision                string                       `json:"factRevision"`
	ConfiguredBind              string                       `json:"configuredBind"`
	ConfiguredPort              uint16                       `json:"configuredPort"`
	AddressFamily               AddressFamily                `json:"addressFamily"`
	ObservedBind                string                       `json:"observedBind,omitempty"`
	ObservedPort                uint16                       `json:"observedPort,omitempty"`
	ObservedAddressFamily       AddressFamily                `json:"observedAddressFamily,omitempty"`
	Exposure                    LocalProxyExposureV1         `json:"exposure"`
	Ownership                   LocalProxyOwnershipV1        `json:"ownership"`
	ListenerState               LocalProxyListenerStateV1    `json:"listenerState"`
	Protocols                   []LocalProxyProtocolV1       `json:"protocols"`
	Authentication              LocalProxyAuthenticationV1   `json:"authentication"`
	AuthenticationCount         int                          `json:"authenticationCount"`
	AuthenticationRevision      string                       `json:"authenticationRevision"`
	TLS                         LocalProxyTLSStateV1         `json:"tls"`
	TLSRevision                 string                       `json:"tlsRevision"`
	SystemProxy                 LocalProxySystemProxyStateV1 `json:"systemProxy"`
	SystemProxyRevision         string                       `json:"systemProxyRevision"`
	DependentUDPAssociation     bool                         `json:"dependentUdpAssociation"`
	StaticUDPListener           bool                         `json:"staticUdpListener"`
	RuntimeReady                bool                         `json:"runtimeReady"`
	HealthCapabilityReady       bool                         `json:"healthCapabilityReady"`
	CapacityReady               bool                         `json:"capacityReady"`
	ManagementCollision         CapabilityValue              `json:"managementCollision"`
	RecoveryPathCollision       CapabilityValue              `json:"recoveryPathCollision"`
	ObservedAt                  int64                        `json:"observedAt"`
	ExpiresAt                   int64                        `json:"expiresAt"`
	ReasonCodes                 []string                     `json:"reasonCodes,omitempty"`
	configuredAddress           netip.Addr
}

// NewLocalProxyFactV1 binds a core-authored semantic fact to an exact neutral
// resource endpoint and, when available, the exact listener-owner observation.
// There is deliberately no constructor from caller-supplied host/port data.
func NewLocalProxyFactV1(value LocalProxyFactV1, resource ProtectableResource, endpoint PublicEndpoint, observed *hostsurface.HostSurfaceFactV1) (LocalProxyFactV1, error) {
	if resource.ID != endpoint.ResourceID || resource.Owner != endpoint.Owner || resource.Kind != "inbound" ||
		endpoint.Schema != EndpointSchemaV1 || endpoint.Key.Network != NetworkTCP ||
		endpoint.Key.Port == 0 ||
		endpoint.ConfigurationRevision != value.ConfigurationRevision ||
		endpoint.OwnerRevision != value.OwnerRevision || !resourceContainsExactEndpointV1(resource, frontingEndpointRevisionV1(endpoint)) {
		return LocalProxyFactV1{}, errors.New("local_proxy_provider_observation_v1_invalid")
	}
	address, exposure := localProxyAddress(endpoint.Key.BindAddress)
	value.Schema = LocalProxyFactSchemaV1
	value.ResourceID, value.EndpointID = resource.ID, endpoint.ID
	value.ConfiguredBind, value.ConfiguredPort = endpoint.Key.BindAddress, endpoint.Key.Port
	value.AddressFamily, value.Exposure = endpoint.Key.AddressFamily, exposure
	value.configuredAddress = address
	value.Protocols = canonicalLocalProxyProtocols(value.Protocols)
	value.ReasonCodes = canonicalLocalProxyReasons(value.ReasonCodes)
	if observed == nil {
		if value.ListenerState == "" {
			value.ListenerState = LocalProxyListenerUnobserved
		}
	} else {
		value.ObservedBind, value.ObservedPort = observed.Bind, observed.Port
		switch observed.Family {
		case hostsurface.FamilyIPv4:
			value.ObservedAddressFamily = AddressFamilyIPv4
		case hostsurface.FamilyIPv6:
			value.ObservedAddressFamily = AddressFamilyIPv6
		default:
			value.ObservedAddressFamily = AddressFamilyUnknown
		}
		value.ListenerObservationRevision = localProxyObservedRevision(*observed)
		switch {
		case observed.IsStale(time.Unix(value.ObservedAt, 0)):
			value.ListenerState = LocalProxyListenerStale
		case observed.RegisteredResourceID != resource.ID || observed.Network != hostsurface.NetworkTCP ||
			observed.Port != endpoint.Key.Port || observed.Bind != endpoint.Key.BindAddress:
			value.ListenerState = LocalProxyListenerForeign
		case observed.Classification != hostsurface.ClassificationManagedExact || observed.ListenerOwner == nil ||
			!observed.ListenerOwner.Valid(time.Unix(value.ObservedAt, 0)):
			value.ListenerState = LocalProxyListenerForeign
		default:
			value.ListenerState = LocalProxyListenerObservedExact
		}
	}
	value.FactRevision = Revision(localProxyFactRevisionInput(value))
	if err := value.Validate(time.Time{}); err != nil {
		return LocalProxyFactV1{}, err
	}
	return value, nil
}

func (f LocalProxyFactV1) Validate(now time.Time) error {
	address, exposure := localProxyAddress(f.ConfiguredBind)
	dependentUDP := strings.EqualFold(f.InboundType, "socks") || strings.EqualFold(f.InboundType, "mixed")
	if f.Schema != LocalProxyFactSchemaV1 || !frontingToken(f.ProviderID, 128) || !frontingToken(f.ContributorID, 128) ||
		!frontingToken(f.ResourceID, 256) || !frontingToken(f.EndpointID, 128) || f.InboundDatabaseID == 0 ||
		!frontingToken(f.InboundType, 64) || !frontingDigest(f.ConfigurationRevision) ||
		(f.EffectiveRuntimeRevision != "" && !frontingDigest(f.EffectiveRuntimeRevision)) ||
		!frontingToken(f.RuntimeIdentityRevision, 128) || !frontingToken(f.ProviderRevision, 128) ||
		!frontingToken(f.CapabilityRevision, 128) || f.ListenerObservationRevision != "" && !frontingDigest(f.ListenerObservationRevision) ||
		!frontingToken(f.OwnerRevision, 128) || !frontingDigest(f.HealthRevision) || !frontingDigest(f.CapacityRevision) ||
		!frontingDigest(f.ManagementExclusionRevision) || !frontingDigest(f.RecoveryPathRevision) || !frontingDigest(f.FactRevision) ||
		f.ConfiguredPort == 0 || f.Exposure != exposure || f.configuredAddress.IsValid() && f.configuredAddress != address ||
		!localProxyFamilyMatches(address, f.AddressFamily, exposure) || !validLocalProxyOwnership(f.Ownership) ||
		!validLocalProxyListenerState(f.ListenerState) || !validLocalProxyProtocols(f.Protocols, f.InboundType) ||
		!validLocalProxyAuthentication(f.Authentication, f.AuthenticationCount) || !frontingDigest(f.AuthenticationRevision) ||
		!validLocalProxyTLS(f.TLS) || !frontingDigest(f.TLSRevision) || !validLocalProxySystemProxy(f.SystemProxy) ||
		!frontingDigest(f.SystemProxyRevision) || f.DependentUDPAssociation != dependentUDP || f.StaticUDPListener ||
		(f.ManagementCollision != CapabilityYes && f.ManagementCollision != CapabilityNo && f.ManagementCollision != CapabilityUnknown) ||
		(f.RecoveryPathCollision != CapabilityYes && f.RecoveryPathCollision != CapabilityNo && f.RecoveryPathCollision != CapabilityUnknown) ||
		f.ObservedAt <= 0 || f.ExpiresAt <= f.ObservedAt ||
		f.ExpiresAt-f.ObservedAt > int64(MaxLocalProxyFactFreshnessV1/time.Second) ||
		!now.IsZero() && f.ExpiresAt <= now.UTC().Unix() || !validLocalProxyReasons(f.ReasonCodes) ||
		f.FactRevision != Revision(localProxyFactRevisionInput(f)) {
		return errors.New("local_proxy_fact_v1_invalid")
	}
	if f.ListenerObservationRevision == "" {
		if f.ObservedBind != "" || f.ObservedPort != 0 || f.ObservedAddressFamily != "" {
			return errors.New("local_proxy_fact_v1_observed_listener_invalid")
		}
	} else if f.ObservedBind == "" || f.ObservedPort == 0 ||
		(f.ObservedAddressFamily != AddressFamilyIPv4 && f.ObservedAddressFamily != AddressFamilyIPv6 &&
			f.ObservedAddressFamily != AddressFamilyUnknown) {
		return errors.New("local_proxy_fact_v1_observed_listener_invalid")
	}
	return nil
}

func (f LocalProxyFactV1) Actionable(now time.Time) error {
	if err := f.Validate(now); err != nil {
		return err
	}
	if !frontingDigest(f.EffectiveRuntimeRevision) {
		return errors.New("local_proxy_runtime_revision_unknown")
	}
	if f.Exposure == LocalProxyExposurePublic || f.Exposure == LocalProxyExposureWildcard ||
		f.Exposure == LocalProxyExposureUnspecified || f.Exposure == LocalProxyExposureUnknown {
		return errors.New("local_proxy_fact_v1_not_shipped")
	}
	if f.Ownership != LocalProxyProviderManaged || f.ListenerState != LocalProxyListenerObservedExact ||
		!f.RuntimeReady || !f.HealthCapabilityReady || !f.CapacityReady ||
		f.ManagementCollision != CapabilityNo || f.RecoveryPathCollision != CapabilityNo ||
		f.Authentication == LocalProxyAuthenticationUnknown || f.TLS == LocalProxyTLSUnknown ||
		f.SystemProxy != LocalProxySystemProxyDisabled || len(f.ReasonCodes) != 0 {
		return errors.New("local_proxy_fact_v1_not_actionable")
	}
	if f.Exposure == LocalProxyExposurePrivate && f.Authentication != LocalProxyAuthenticationPresent {
		return errors.New("local_proxy_private_authentication_required")
	}
	return nil
}

type LocalProxyReferenceV1 struct {
	Schema                      string                       `json:"schema"`
	ProviderID                  string                       `json:"providerId"`
	ContributorID               string                       `json:"contributorId"`
	ResourceID                  string                       `json:"resourceId"`
	EndpointID                  string                       `json:"endpointId"`
	InboundDatabaseID           uint                         `json:"inboundDatabaseId"`
	InboundType                 string                       `json:"inboundType"`
	ConfigurationRevision       string                       `json:"configurationRevision"`
	EffectiveRuntimeRevision    string                       `json:"effectiveRuntimeRevision"`
	RuntimeIdentityRevision     string                       `json:"runtimeIdentityRevision"`
	ProviderRevision            string                       `json:"providerRevision"`
	CapabilityRevision          string                       `json:"capabilityRevision"`
	ListenerObservationRevision string                       `json:"listenerObservationRevision"`
	OwnerRevision               string                       `json:"ownerRevision"`
	HealthRevision              string                       `json:"healthRevision"`
	CapacityRevision            string                       `json:"capacityRevision"`
	ManagementExclusionRevision string                       `json:"managementExclusionRevision"`
	RecoveryPathRevision        string                       `json:"recoveryPathRevision"`
	FactRevision                string                       `json:"factRevision"`
	Exposure                    LocalProxyExposureV1         `json:"exposure"`
	Protocols                   []LocalProxyProtocolV1       `json:"protocols"`
	Authentication              LocalProxyAuthenticationV1   `json:"authentication"`
	AuthenticationRevision      string                       `json:"authenticationRevision"`
	TLS                         LocalProxyTLSStateV1         `json:"tls"`
	TLSRevision                 string                       `json:"tlsRevision"`
	SystemProxy                 LocalProxySystemProxyStateV1 `json:"systemProxy"`
	SystemProxyRevision         string                       `json:"systemProxyRevision"`
	CanonicalReferenceRevision  string                       `json:"canonicalReferenceRevision"`
}

func ReferenceLocalProxyV1(fact LocalProxyFactV1, now time.Time) (LocalProxyReferenceV1, error) {
	if err := fact.Actionable(now); err != nil {
		return LocalProxyReferenceV1{}, err
	}
	value := LocalProxyReferenceV1{
		Schema: LocalProxyReferenceSchemaV1, ProviderID: fact.ProviderID, ContributorID: fact.ContributorID,
		ResourceID: fact.ResourceID, EndpointID: fact.EndpointID, InboundDatabaseID: fact.InboundDatabaseID,
		InboundType: fact.InboundType, ConfigurationRevision: fact.ConfigurationRevision,
		EffectiveRuntimeRevision: fact.EffectiveRuntimeRevision, RuntimeIdentityRevision: fact.RuntimeIdentityRevision,
		ProviderRevision: fact.ProviderRevision, CapabilityRevision: fact.CapabilityRevision,
		ListenerObservationRevision: fact.ListenerObservationRevision, OwnerRevision: fact.OwnerRevision,
		HealthRevision: fact.HealthRevision, CapacityRevision: fact.CapacityRevision,
		ManagementExclusionRevision: fact.ManagementExclusionRevision, RecoveryPathRevision: fact.RecoveryPathRevision,
		FactRevision: fact.FactRevision, Exposure: fact.Exposure, Protocols: append([]LocalProxyProtocolV1(nil), fact.Protocols...),
		Authentication: fact.Authentication, AuthenticationRevision: fact.AuthenticationRevision,
		TLS: fact.TLS, TLSRevision: fact.TLSRevision, SystemProxy: fact.SystemProxy, SystemProxyRevision: fact.SystemProxyRevision,
	}
	value.CanonicalReferenceRevision = Revision(localProxyReferenceRevisionInput(value))
	return value, value.Validate()
}

func (r LocalProxyReferenceV1) Validate() error {
	if r.Schema != LocalProxyReferenceSchemaV1 || !frontingToken(r.ProviderID, 128) ||
		!frontingToken(r.ContributorID, 128) || !frontingToken(r.ResourceID, 256) || !frontingToken(r.EndpointID, 128) ||
		r.InboundDatabaseID == 0 || !frontingToken(r.InboundType, 64) || !frontingDigest(r.ConfigurationRevision) ||
		!frontingDigest(r.EffectiveRuntimeRevision) || !frontingToken(r.RuntimeIdentityRevision, 128) ||
		!frontingToken(r.ProviderRevision, 128) || !frontingToken(r.CapabilityRevision, 128) ||
		!frontingDigest(r.ListenerObservationRevision) || !frontingToken(r.OwnerRevision, 128) ||
		!frontingDigest(r.HealthRevision) || !frontingDigest(r.CapacityRevision) ||
		!frontingDigest(r.ManagementExclusionRevision) || !frontingDigest(r.RecoveryPathRevision) ||
		!frontingDigest(r.FactRevision) || (r.Exposure != LocalProxyExposureLoopback && r.Exposure != LocalProxyExposurePrivate) ||
		!validLocalProxyProtocols(r.Protocols, r.InboundType) ||
		(r.Authentication != LocalProxyAuthenticationPresent && r.Authentication != LocalProxyAuthenticationAbsent) ||
		r.Exposure == LocalProxyExposurePrivate && r.Authentication != LocalProxyAuthenticationPresent ||
		!frontingDigest(r.AuthenticationRevision) || !validLocalProxyTLS(r.TLS) || r.TLS == LocalProxyTLSUnknown ||
		!frontingDigest(r.TLSRevision) || !validLocalProxySystemProxy(r.SystemProxy) || r.SystemProxy == LocalProxySystemProxyUnknown ||
		!frontingDigest(r.SystemProxyRevision) || !frontingDigest(r.CanonicalReferenceRevision) ||
		r.CanonicalReferenceRevision != Revision(localProxyReferenceRevisionInput(r)) {
		return errors.New("local_proxy_reference_v1_invalid")
	}
	return nil
}

func ResolveExactLocalProxyV1(reference LocalProxyReferenceV1, fact LocalProxyFactV1, now time.Time) error {
	if err := reference.Validate(); err != nil {
		return err
	}
	want, err := ReferenceLocalProxyV1(fact, now)
	if err != nil {
		return err
	}
	if !localProxyReferencesEqual(want, reference) {
		return errors.New("local_proxy_reference_v1_stale")
	}
	return nil
}

func localProxyReferencesEqual(left, right LocalProxyReferenceV1) bool {
	return left.Schema == right.Schema && left.ProviderID == right.ProviderID && left.ContributorID == right.ContributorID &&
		left.ResourceID == right.ResourceID && left.EndpointID == right.EndpointID && left.InboundDatabaseID == right.InboundDatabaseID &&
		left.InboundType == right.InboundType && left.ConfigurationRevision == right.ConfigurationRevision &&
		left.EffectiveRuntimeRevision == right.EffectiveRuntimeRevision && left.RuntimeIdentityRevision == right.RuntimeIdentityRevision &&
		left.ProviderRevision == right.ProviderRevision && left.CapabilityRevision == right.CapabilityRevision &&
		left.ListenerObservationRevision == right.ListenerObservationRevision && left.OwnerRevision == right.OwnerRevision &&
		left.HealthRevision == right.HealthRevision && left.CapacityRevision == right.CapacityRevision &&
		left.ManagementExclusionRevision == right.ManagementExclusionRevision && left.RecoveryPathRevision == right.RecoveryPathRevision &&
		left.FactRevision == right.FactRevision && left.Exposure == right.Exposure && slices.Equal(left.Protocols, right.Protocols) &&
		left.Authentication == right.Authentication && left.AuthenticationRevision == right.AuthenticationRevision &&
		left.TLS == right.TLS && left.TLSRevision == right.TLSRevision && left.SystemProxy == right.SystemProxy &&
		left.SystemProxyRevision == right.SystemProxyRevision && left.CanonicalReferenceRevision == right.CanonicalReferenceRevision
}

type LocalProxyGuardLeaseV1 struct {
	Schema              string                `json:"schema"`
	LeaseID             string                `json:"leaseId"`
	LeaseRevision       string                `json:"leaseRevision"`
	AuthorityProviderID string                `json:"authorityProviderId"`
	HolderID            string                `json:"holderId"`
	Purpose             string                `json:"purpose"`
	ExactReference      LocalProxyReferenceV1 `json:"exactReference"`
	State               EndpointLeaseState    `json:"state"`
	IssuedAt            int64                 `json:"issuedAt"`
	RenewedAt           int64                 `json:"renewedAt"`
	ExpiresAt           int64                 `json:"expiresAt"`
	ReleasedAt          int64                 `json:"releasedAt,omitempty"`
	ReasonCodes         []string              `json:"reasonCodes,omitempty"`
}

type AcquireLocalProxyGuardLeaseRequestV1 struct {
	RequestID        string                `json:"requestId"`
	HolderID         string                `json:"holderId"`
	Purpose          string                `json:"purpose"`
	ExactReference   LocalProxyReferenceV1 `json:"exactReference"`
	FreshnessSeconds uint32                `json:"freshnessSeconds"`
}

type MutateLocalProxyGuardLeaseRequestV1 struct {
	RequestID        string `json:"requestId"`
	LeaseID          string `json:"leaseId"`
	ExpectedRevision string `json:"expectedRevision"`
	FreshnessSeconds uint32 `json:"freshnessSeconds,omitempty"`
}

type ReleaseLocalProxyGuardLeaseRequestV1 struct {
	RequestID        string `json:"requestId"`
	LeaseID          string `json:"leaseId"`
	ExpectedRevision string `json:"expectedRevision"`
}

type GetLocalProxyGuardLeaseRequestV1 struct {
	LeaseID string `json:"leaseId"`
}

type ListLocalProxyGuardLeasesRequestV1 struct {
	HolderID string `json:"holderId"`
	Limit    uint16 `json:"limit"`
}

type LocalProxyProviderV1 interface {
	ProviderID() string
	LocalProxyFactsV1(context.Context, time.Time) ([]LocalProxyFactV1, error)
	AcquireLocalProxyGuardLease(context.Context, AcquireLocalProxyGuardLeaseRequestV1) (LocalProxyGuardLeaseV1, error)
	FenceLocalProxyGuardLease(context.Context, MutateLocalProxyGuardLeaseRequestV1) (LocalProxyGuardLeaseV1, error)
	ActivateLocalProxyGuardLease(context.Context, MutateLocalProxyGuardLeaseRequestV1) (LocalProxyGuardLeaseV1, error)
	RenewLocalProxyGuardLease(context.Context, MutateLocalProxyGuardLeaseRequestV1) (LocalProxyGuardLeaseV1, error)
	ReleaseLocalProxyGuardLease(context.Context, ReleaseLocalProxyGuardLeaseRequestV1) (LocalProxyGuardLeaseV1, error)
	GetLocalProxyGuardLease(context.Context, GetLocalProxyGuardLeaseRequestV1) (LocalProxyGuardLeaseV1, error)
	ListLocalProxyGuardLeases(context.Context, ListLocalProxyGuardLeasesRequestV1) ([]LocalProxyGuardLeaseV1, error)
}

func (r AcquireLocalProxyGuardLeaseRequestV1) Validate() error {
	if !frontingToken(r.RequestID, 128) || !frontingToken(r.HolderID, 128) || r.Purpose != LocalProxyGuardPurposeV1 ||
		r.FreshnessSeconds == 0 || time.Duration(r.FreshnessSeconds)*time.Second > MaxLocalProxyLeaseFreshnessV1 ||
		r.ExactReference.Validate() != nil {
		return errors.New("local_proxy_guard_lease_acquire_request_v1_invalid")
	}
	return nil
}

func (r MutateLocalProxyGuardLeaseRequestV1) Validate(requireFreshness bool) error {
	if !frontingToken(r.RequestID, 128) || !frontingToken(r.LeaseID, 128) || !frontingDigest(r.ExpectedRevision) ||
		requireFreshness && (r.FreshnessSeconds == 0 || time.Duration(r.FreshnessSeconds)*time.Second > MaxLocalProxyLeaseFreshnessV1) {
		return errors.New("local_proxy_guard_lease_mutation_request_v1_invalid")
	}
	return nil
}

func (r ReleaseLocalProxyGuardLeaseRequestV1) Validate() error {
	if !frontingToken(r.RequestID, 128) || !frontingToken(r.LeaseID, 128) || !frontingDigest(r.ExpectedRevision) {
		return errors.New("local_proxy_guard_lease_release_request_v1_invalid")
	}
	return nil
}

func (r GetLocalProxyGuardLeaseRequestV1) Validate() error {
	if !frontingToken(r.LeaseID, 128) {
		return errors.New("local_proxy_guard_lease_get_request_v1_invalid")
	}
	return nil
}

func (r ListLocalProxyGuardLeasesRequestV1) Validate() error {
	if !frontingToken(r.HolderID, 128) || r.Limit == 0 || r.Limit > MaxLocalProxyGuardLeasePageV1 {
		return errors.New("local_proxy_guard_lease_list_request_v1_invalid")
	}
	return nil
}

func (l LocalProxyGuardLeaseV1) Validate() error {
	if l.Schema != LocalProxyGuardLeaseSchemaV1 || !frontingToken(l.LeaseID, 128) || !frontingDigest(l.LeaseRevision) ||
		!frontingToken(l.AuthorityProviderID, 128) || !frontingToken(l.HolderID, 128) || l.Purpose != LocalProxyGuardPurposeV1 ||
		l.ExactReference.Validate() != nil || l.IssuedAt <= 0 || l.RenewedAt < l.IssuedAt ||
		l.ExpiresAt <= l.RenewedAt || l.ExpiresAt-l.RenewedAt > int64(MaxLocalProxyLeaseFreshnessV1/time.Second) ||
		!validLocalProxyReasons(l.ReasonCodes) || l.LeaseRevision != Revision(localProxyLeaseRevisionInput(l)) {
		return errors.New("local_proxy_guard_lease_v1_invalid")
	}
	switch l.State {
	case EndpointLeaseReserved, EndpointLeaseMutationPending, EndpointLeaseActive, EndpointLeaseReconcileRequired:
		if l.ReleasedAt != 0 {
			return errors.New("local_proxy_guard_lease_release_time_invalid")
		}
	case EndpointLeaseReleased:
		if l.ReleasedAt <= 0 {
			return errors.New("local_proxy_guard_lease_release_time_invalid")
		}
	default:
		return errors.New("local_proxy_guard_lease_state_invalid")
	}
	return nil
}

func FinalizeLocalProxyGuardLeaseV1(value LocalProxyGuardLeaseV1) (LocalProxyGuardLeaseV1, error) {
	value.Schema = LocalProxyGuardLeaseSchemaV1
	value.Purpose = LocalProxyGuardPurposeV1
	value.ReasonCodes = canonicalLocalProxyReasons(value.ReasonCodes)
	value.LeaseRevision = Revision(localProxyLeaseRevisionInput(value))
	return value, value.Validate()
}

func ValidateLocalProxyGuardLeaseTransitionV1(current, next LocalProxyGuardLeaseV1, expectedRevision string, mutation EndpointLeaseMutation, now time.Time) error {
	if current.Validate() != nil || next.Validate() != nil || current.LeaseRevision != expectedRevision ||
		current.LeaseID != next.LeaseID || current.AuthorityProviderID != next.AuthorityProviderID ||
		current.HolderID != next.HolderID || !localProxyReferencesEqual(current.ExactReference, next.ExactReference) ||
		current.IssuedAt != next.IssuedAt || current.ReleasedAt != 0 ||
		mutation != EndpointLeaseRelease && now.UTC().Unix() > current.ExpiresAt {
		return ErrLocalProxyGuardLeaseConflictV1
	}
	want, ok := localProxyLeaseTransition(current.State, mutation)
	if !ok || next.State != want || (mutation == EndpointLeaseRelease) != (next.ReleasedAt > 0) {
		return ErrLocalProxyGuardLeaseConflictV1
	}
	if mutation == EndpointLeaseRenew && (next.RenewedAt <= current.RenewedAt || next.ExpiresAt <= current.ExpiresAt) {
		return ErrLocalProxyGuardLeaseConflictV1
	}
	return nil
}

func localProxyLeaseTransition(current EndpointLeaseState, mutation EndpointLeaseMutation) (EndpointLeaseState, bool) {
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

func localProxyAddress(bind string) (netip.Addr, LocalProxyExposureV1) {
	bind = strings.TrimSpace(bind)
	if bind == "" {
		return netip.Addr{}, LocalProxyExposureUnspecified
	}
	if bind == "*" {
		return netip.Addr{}, LocalProxyExposureWildcard
	}
	address, err := netip.ParseAddr(bind)
	if err != nil || address.Is4In6() || address.String() != bind {
		return netip.Addr{}, LocalProxyExposureUnknown
	}
	switch {
	case address.IsUnspecified():
		return address, LocalProxyExposureWildcard
	case address.IsLoopback():
		return address, LocalProxyExposureLoopback
	case address.IsPrivate():
		return address, LocalProxyExposurePrivate
	default:
		return address, LocalProxyExposurePublic
	}
}

func localProxyFamilyMatches(address netip.Addr, family AddressFamily, exposure LocalProxyExposureV1) bool {
	if !address.IsValid() {
		return family == AddressFamilyUnknown &&
			(exposure == LocalProxyExposureWildcard || exposure == LocalProxyExposureUnspecified || exposure == LocalProxyExposureUnknown)
	}
	return family == AddressFamilyIPv4 && address.Is4() || family == AddressFamilyIPv6 && address.Is6()
}

func validLocalProxyProtocols(values []LocalProxyProtocolV1, inboundType string) bool {
	canonical := canonicalLocalProxyProtocols(values)
	if len(canonical) == 0 || len(canonical) != len(values) || !slices.Equal(canonical, values) {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(inboundType)) {
	case "socks":
		required := map[LocalProxyProtocolV1]bool{LocalProxyProtocolSOCKS5: true}
		for _, value := range values {
			if value != LocalProxyProtocolSOCKS4 && value != LocalProxyProtocolSOCKS5 {
				return false
			}
			delete(required, value)
		}
		return len(required) == 0
	case "http":
		required := map[LocalProxyProtocolV1]bool{
			LocalProxyProtocolHTTPForward: true, LocalProxyProtocolHTTPConnect: true,
		}
		for _, value := range values {
			if value != LocalProxyProtocolHTTPForward && value != LocalProxyProtocolHTTPConnect {
				return false
			}
			delete(required, value)
		}
		return len(required) == 0
	case "mixed":
		required := map[LocalProxyProtocolV1]bool{
			LocalProxyProtocolSOCKS5: true, LocalProxyProtocolHTTPForward: true, LocalProxyProtocolHTTPConnect: true,
		}
		for _, value := range values {
			delete(required, value)
		}
		if len(required) != 0 {
			return false
		}
	default:
		return false
	}
	return true
}

func canonicalLocalProxyProtocols(values []LocalProxyProtocolV1) []LocalProxyProtocolV1 {
	seen := map[LocalProxyProtocolV1]bool{}
	result := make([]LocalProxyProtocolV1, 0, min(len(values), MaxLocalProxyProtocolsV1))
	for _, value := range values {
		switch value {
		case LocalProxyProtocolSOCKS4, LocalProxyProtocolSOCKS5, LocalProxyProtocolHTTPForward, LocalProxyProtocolHTTPConnect:
			if !seen[value] {
				seen[value] = true
				result = append(result, value)
			}
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func validLocalProxyAuthentication(value LocalProxyAuthenticationV1, count int) bool {
	if count < 0 || count > 65535 {
		return false
	}
	switch value {
	case LocalProxyAuthenticationPresent:
		return count > 0
	case LocalProxyAuthenticationAbsent:
		return count == 0
	case LocalProxyAuthenticationUnknown:
		return count == 0
	default:
		return false
	}
}

func validLocalProxyOwnership(value LocalProxyOwnershipV1) bool {
	return value == LocalProxyProviderManaged || value == LocalProxyExternalManaged || value == LocalProxyOwnershipUnknown
}

func validLocalProxyListenerState(value LocalProxyListenerStateV1) bool {
	switch value {
	case LocalProxyListenerObservedExact, LocalProxyListenerUnobserved, LocalProxyListenerStale, LocalProxyListenerForeign, LocalProxyListenerUnknown:
		return true
	default:
		return false
	}
}

func validLocalProxyTLS(value LocalProxyTLSStateV1) bool {
	return value == LocalProxyTLSEnabled || value == LocalProxyTLSDisabled || value == LocalProxyTLSUnknown
}

func validLocalProxySystemProxy(value LocalProxySystemProxyStateV1) bool {
	return value == LocalProxySystemProxyEnabled || value == LocalProxySystemProxyDisabled || value == LocalProxySystemProxyUnknown
}

func localProxyObservedRevision(value hostsurface.HostSurfaceFactV1) string {
	copyValue := value
	copyValue.FirstSeen, copyValue.LastSeen, copyValue.ExpiresAt = 0, 0, 0
	return Revision(copyValue)
}

func localProxyFactRevisionInput(value LocalProxyFactV1) LocalProxyFactV1 {
	value.ObservedAt, value.ExpiresAt, value.FactRevision = 0, 0, ""
	value.configuredAddress = netip.Addr{}
	return value
}

func localProxyReferenceRevisionInput(value LocalProxyReferenceV1) LocalProxyReferenceV1 {
	value.CanonicalReferenceRevision = ""
	return value
}

func localProxyLeaseRevisionInput(value LocalProxyGuardLeaseV1) LocalProxyGuardLeaseV1 {
	value.LeaseRevision = ""
	return value
}

func validLocalProxyReasons(values []string) bool {
	return slices.Equal(values, canonicalLocalProxyReasons(values))
}

func canonicalLocalProxyReasons(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, min(len(values), MaxLocalProxyReasonCodesV1))
	for _, value := range values {
		value = strings.ToUpper(strings.TrimSpace(value))
		if value == "" || len(value) > 96 || seen[value] {
			continue
		}
		safe := true
		for _, character := range value {
			if character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '_' || character == '-' {
				continue
			}
			safe = false
			break
		}
		if !safe {
			value = "LOCAL_PROXY_REASON_INVALID"
		}
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
		if len(result) == MaxLocalProxyReasonCodesV1 {
			break
		}
	}
	sort.Strings(result)
	return result
}
