package resources

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"
)

const (
	InterceptionFactSchemaV1        = "solovey-ui/interception-inbound-fact/v1"
	InterceptionReferenceSchemaV1   = "solovey-ui/interception-inbound-reference/v1"
	InterceptionLeaseSchemaV1       = "solovey-ui/interception-inbound-lease/v1"
	IngressScopeFactSchemaV1        = "solovey-ui/forwarded-ingress-scope-fact/v1"
	IngressScopeReferenceSchemaV1   = "solovey-ui/forwarded-ingress-scope-reference/v1"
	InterceptionProviderRevisionV1  = "solovey-ui/interception-provider/v1"
	IngressScopeProviderRevisionV1  = "solovey-ui/forwarded-ingress-provider/v1"
	MaxInterceptionFreshnessV1      = 5 * time.Minute
	MaxInterceptionLeaseFreshnessV1 = 10 * time.Minute
	MaxInterceptionReasonCodesV1    = 32
	MaxInterceptionLeasePageV1      = 4096
)

var ErrInterceptionLeaseConflictV1 = errors.New("interception_lease_v1_conflict")

type InterceptionKindV1 string

const (
	InterceptionRedirectV1 InterceptionKindV1 = "REDIRECT"
	InterceptionTProxyV1   InterceptionKindV1 = "TPROXY"
	InterceptionTUNV1      InterceptionKindV1 = "TUN"
)

type InterceptionOwnershipV1 string

const (
	InterceptionProviderManagedV1  InterceptionOwnershipV1 = "PROVIDER_MANAGED"
	InterceptionExternalManagedV1  InterceptionOwnershipV1 = "EXTERNAL_MANAGED"
	InterceptionOwnershipUnknownV1 InterceptionOwnershipV1 = "UNKNOWN"
)

type InterceptionListenerStateV1 string

const (
	InterceptionListenerObservedExactV1 InterceptionListenerStateV1 = "OBSERVED_EXACT"
	InterceptionListenerUnobservedV1    InterceptionListenerStateV1 = "UNOBSERVED"
	InterceptionListenerStaleV1         InterceptionListenerStateV1 = "STALE"
	InterceptionListenerForeignV1       InterceptionListenerStateV1 = "FOREIGN"
	InterceptionListenerUnknownV1       InterceptionListenerStateV1 = "UNKNOWN"
)

type InterceptionInboundFactV1 struct {
	Schema                       string                      `json:"schema"`
	ProviderID                   string                      `json:"providerId"`
	ProviderRevision             string                      `json:"providerRevision"`
	ResourceID                   string                      `json:"resourceId"`
	EndpointID                   string                      `json:"endpointId"`
	InboundDatabaseID            uint                        `json:"inboundDatabaseId"`
	InboundTag                   string                      `json:"inboundTag,omitempty"`
	Kind                         InterceptionKindV1          `json:"kind"`
	Network                      Network                     `json:"network"`
	AddressFamily                AddressFamily               `json:"addressFamily"`
	ConfiguredBind               string                      `json:"configuredBind"`
	ConfiguredPort               uint16                      `json:"configuredPort"`
	ObservedEndpointID           string                      `json:"observedEndpointId,omitempty"`
	Ownership                    InterceptionOwnershipV1     `json:"ownership"`
	ListenerState                InterceptionListenerStateV1 `json:"listenerState"`
	ConfigurationRevision        string                      `json:"configurationRevision"`
	RuntimeRevision              string                      `json:"runtimeRevision"`
	RuntimeGenerationRevision    string                      `json:"runtimeGenerationRevision"`
	ListenerRevision             string                      `json:"listenerRevision"`
	CoreSemanticRevision         string                      `json:"coreSemanticRevision"`
	LinuxOnly                    bool                        `json:"linuxOnly"`
	TransparentSocketRequired    bool                        `json:"transparentSocketRequired"`
	OriginalDestinationMechanism string                      `json:"originalDestinationMechanism"`
	OriginalDestinationPreserved bool                        `json:"originalDestinationPreserved"`
	SourcePreserved              bool                        `json:"sourcePreserved"`
	PolicyRoutingRequired        bool                        `json:"policyRoutingRequired"`
	BoundedUDPFlowState          bool                        `json:"boundedUdpFlowState"`
	HealthCapabilityReady        bool                        `json:"healthCapabilityReady"`
	RuntimeReady                 bool                        `json:"runtimeReady"`
	LocalOutputCapture           bool                        `json:"localOutputCapture"`
	TUNOwned                     bool                        `json:"tunOwned"`
	ObservedAt                   int64                       `json:"observedAt"`
	ExpiresAt                    int64                       `json:"expiresAt"`
	FactRevision                 string                      `json:"factRevision"`
	ReasonCodes                  []string                    `json:"reasonCodes,omitempty"`
}

type InterceptionReferenceV1 struct {
	Schema                     string             `json:"schema"`
	ProviderID                 string             `json:"providerId"`
	ResourceID                 string             `json:"resourceId"`
	EndpointID                 string             `json:"endpointId"`
	Kind                       InterceptionKindV1 `json:"kind"`
	Network                    Network            `json:"network"`
	AddressFamily              AddressFamily      `json:"addressFamily"`
	FactRevision               string             `json:"factRevision"`
	ConfigurationRevision      string             `json:"configurationRevision"`
	RuntimeRevision            string             `json:"runtimeRevision"`
	ListenerRevision           string             `json:"listenerRevision"`
	CanonicalReferenceRevision string             `json:"canonicalReferenceRevision"`
}

type InterceptionLeaseV1 struct {
	Schema              string                  `json:"schema"`
	LeaseID             string                  `json:"leaseId"`
	LeaseRevision       string                  `json:"leaseRevision"`
	AuthorityProviderID string                  `json:"authorityProviderId"`
	HolderID            string                  `json:"holderId"`
	ExactReference      InterceptionReferenceV1 `json:"exactReference"`
	State               EndpointLeaseState      `json:"state"`
	IssuedAt            int64                   `json:"issuedAt"`
	RenewedAt           int64                   `json:"renewedAt"`
	ExpiresAt           int64                   `json:"expiresAt"`
	ReleasedAt          int64                   `json:"releasedAt,omitempty"`
	ReasonCodes         []string                `json:"reasonCodes,omitempty"`
}

type AcquireInterceptionLeaseRequestV1 struct {
	RequestID        string                  `json:"requestId"`
	HolderID         string                  `json:"holderId"`
	ExactReference   InterceptionReferenceV1 `json:"exactReference"`
	FreshnessSeconds uint32                  `json:"freshnessSeconds"`
}

type MutateInterceptionLeaseRequestV1 struct {
	RequestID        string `json:"requestId"`
	LeaseID          string `json:"leaseId"`
	ExpectedRevision string `json:"expectedRevision"`
	FreshnessSeconds uint32 `json:"freshnessSeconds,omitempty"`
}

type ReleaseInterceptionLeaseRequestV1 struct {
	RequestID          string `json:"requestId"`
	LeaseID            string `json:"leaseId"`
	ExpectedRevision   string `json:"expectedRevision"`
	DetachmentRevision string `json:"detachmentRevision"`
}

type GetInterceptionLeaseRequestV1 struct {
	LeaseID string `json:"leaseId"`
}
type ListInterceptionLeasesRequestV1 struct {
	HolderID string `json:"holderId"`
	Limit    uint32 `json:"limit"`
}

type InterceptionProviderV1 interface {
	ProviderID() string
	InterceptionFactsV1(context.Context, time.Time) ([]InterceptionInboundFactV1, error)
	AcquireInterceptionLease(context.Context, AcquireInterceptionLeaseRequestV1) (InterceptionLeaseV1, error)
	FenceInterceptionLease(context.Context, MutateInterceptionLeaseRequestV1) (InterceptionLeaseV1, error)
	ActivateInterceptionLease(context.Context, MutateInterceptionLeaseRequestV1) (InterceptionLeaseV1, error)
	RenewInterceptionLease(context.Context, MutateInterceptionLeaseRequestV1) (InterceptionLeaseV1, error)
	ReleaseInterceptionLease(context.Context, ReleaseInterceptionLeaseRequestV1) (InterceptionLeaseV1, error)
	GetInterceptionLease(context.Context, GetInterceptionLeaseRequestV1) (InterceptionLeaseV1, error)
	ListInterceptionLeases(context.Context, ListInterceptionLeasesRequestV1) ([]InterceptionLeaseV1, error)
}

type IngressScopeOwnershipV1 string

const (
	IngressScopeProviderManagedV1  IngressScopeOwnershipV1 = "PROVIDER_MANAGED"
	IngressScopeExternalManagedV1  IngressScopeOwnershipV1 = "EXTERNAL_MANAGED"
	IngressScopeOwnershipUnknownV1 IngressScopeOwnershipV1 = "UNKNOWN"
)

type ForwardedIngressScopeFactV1 struct {
	Schema            string                  `json:"schema"`
	ProviderID        string                  `json:"providerId"`
	ProviderRevision  string                  `json:"providerRevision"`
	ScopeID           string                  `json:"scopeId"`
	InterfaceName     string                  `json:"interfaceName"`
	InterfaceIndex    int                     `json:"interfaceIndex"`
	InterfaceRevision string                  `json:"interfaceRevision"`
	AddressFamily     AddressFamily           `json:"addressFamily"`
	Ownership         IngressScopeOwnershipV1 `json:"ownership"`
	ForwardedIngress  bool                    `json:"forwardedIngress"`
	Loopback          bool                    `json:"loopback"`
	Virtual           bool                    `json:"virtual"`
	Management        bool                    `json:"management"`
	ExternalManaged   bool                    `json:"externalManaged"`
	ObservedAt        int64                   `json:"observedAt"`
	ExpiresAt         int64                   `json:"expiresAt"`
	ScopeRevision     string                  `json:"scopeRevision"`
	ReasonCodes       []string                `json:"reasonCodes,omitempty"`
}

type ForwardedIngressScopeReferenceV1 struct {
	Schema                     string        `json:"schema"`
	ProviderID                 string        `json:"providerId"`
	ScopeID                    string        `json:"scopeId"`
	InterfaceRevision          string        `json:"interfaceRevision"`
	AddressFamily              AddressFamily `json:"addressFamily"`
	ScopeRevision              string        `json:"scopeRevision"`
	CanonicalReferenceRevision string        `json:"canonicalReferenceRevision"`
}

type ForwardedIngressScopeProviderV1 interface {
	ProviderID() string
	ForwardedIngressScopesV1(context.Context, time.Time) ([]ForwardedIngressScopeFactV1, error)
}

func FinalizeInterceptionFactV1(value InterceptionInboundFactV1) (InterceptionInboundFactV1, error) {
	value.Schema = InterceptionFactSchemaV1
	value.ReasonCodes = interceptionCodes(value.ReasonCodes)
	value.FactRevision = Revision(interceptionFactRevisionInput(value))
	return value, value.Validate(time.Time{})
}

func (f InterceptionInboundFactV1) Validate(now time.Time) error {
	if f.Schema != InterceptionFactSchemaV1 || !interceptionToken(f.ProviderID, 128) ||
		f.ProviderRevision != InterceptionProviderRevisionV1 || !interceptionToken(f.ResourceID, 256) ||
		!interceptionToken(f.EndpointID, 256) ||
		f.InboundDatabaseID == 0 || !validInterceptionKind(f.Kind) || (f.Network != NetworkTCP && f.Network != NetworkUDP) ||
		(f.AddressFamily != AddressFamilyIPv4 && f.AddressFamily != AddressFamilyIPv6) ||
		f.ConfiguredPort == 0 || !interceptionDigest(f.ConfigurationRevision) || !interceptionDigest(f.RuntimeRevision) ||
		!interceptionToken(f.RuntimeGenerationRevision, 128) || !interceptionDigest(f.ListenerRevision) ||
		!interceptionDigest(f.CoreSemanticRevision) || !validInterceptionOwnership(f.Ownership) ||
		!validInterceptionListenerState(f.ListenerState) || !interceptionToken(f.OriginalDestinationMechanism, 64) ||
		f.ObservedAt <= 0 || f.ExpiresAt <= f.ObservedAt ||
		f.ExpiresAt-f.ObservedAt > int64(MaxInterceptionFreshnessV1/time.Second) ||
		!now.IsZero() && f.ExpiresAt <= now.UTC().Unix() || !interceptionDigest(f.FactRevision) ||
		f.FactRevision != Revision(interceptionFactRevisionInput(f)) || !slices.Equal(f.ReasonCodes, interceptionCodes(f.ReasonCodes)) {
		return errors.New("interception_fact_v1_invalid")
	}
	if f.Kind == InterceptionRedirectV1 && (f.Network != NetworkTCP || f.TransparentSocketRequired || f.PolicyRoutingRequired || f.BoundedUDPFlowState) {
		return errors.New("redirect_interception_fact_v1_contradictory")
	}
	if f.Kind == InterceptionTProxyV1 && (!f.TransparentSocketRequired || !f.PolicyRoutingRequired) {
		return errors.New("tproxy_interception_fact_v1_contradictory")
	}
	if f.Network == NetworkUDP && (f.Kind != InterceptionTProxyV1 || !f.BoundedUDPFlowState) {
		return errors.New("interception_fact_v1_contradictory")
	}
	return nil
}

func ReferenceInterceptionV1(fact InterceptionInboundFactV1, now time.Time) (InterceptionReferenceV1, error) {
	if err := fact.Validate(now); err != nil {
		return InterceptionReferenceV1{}, err
	}
	value := InterceptionReferenceV1{
		Schema: InterceptionReferenceSchemaV1, ProviderID: fact.ProviderID, ResourceID: fact.ResourceID, EndpointID: fact.EndpointID,
		Kind: fact.Kind, Network: fact.Network, AddressFamily: fact.AddressFamily, FactRevision: fact.FactRevision,
		ConfigurationRevision: fact.ConfigurationRevision, RuntimeRevision: fact.RuntimeRevision, ListenerRevision: fact.ListenerRevision,
	}
	value.CanonicalReferenceRevision = Revision(interceptionReferenceRevisionInput(value))
	return value, value.Validate()
}

func (r InterceptionReferenceV1) Validate() error {
	if r.Schema != InterceptionReferenceSchemaV1 || !interceptionToken(r.ProviderID, 128) ||
		!interceptionToken(r.ResourceID, 256) || !interceptionToken(r.EndpointID, 256) || !validInterceptionKind(r.Kind) ||
		(r.Network != NetworkTCP && r.Network != NetworkUDP) ||
		(r.AddressFamily != AddressFamilyIPv4 && r.AddressFamily != AddressFamilyIPv6) ||
		!interceptionDigest(r.FactRevision) || !interceptionDigest(r.ConfigurationRevision) ||
		!interceptionDigest(r.RuntimeRevision) || !interceptionDigest(r.ListenerRevision) ||
		!interceptionDigest(r.CanonicalReferenceRevision) ||
		r.CanonicalReferenceRevision != Revision(interceptionReferenceRevisionInput(r)) {
		return errors.New("interception_reference_v1_invalid")
	}
	return nil
}

func ResolveExactInterceptionV1(reference InterceptionReferenceV1, fact InterceptionInboundFactV1, now time.Time) error {
	if reference.Validate() != nil || fact.Validate(now) != nil ||
		reference.ProviderID != fact.ProviderID || reference.ResourceID != fact.ResourceID || reference.EndpointID != fact.EndpointID ||
		reference.Kind != fact.Kind || reference.Network != fact.Network || reference.AddressFamily != fact.AddressFamily ||
		reference.FactRevision != fact.FactRevision || reference.ConfigurationRevision != fact.ConfigurationRevision ||
		reference.RuntimeRevision != fact.RuntimeRevision || reference.ListenerRevision != fact.ListenerRevision {
		return errors.New("interception_reference_v1_stale")
	}
	return nil
}

func FinalizeIngressScopeFactV1(value ForwardedIngressScopeFactV1) (ForwardedIngressScopeFactV1, error) {
	value.Schema = IngressScopeFactSchemaV1
	value.ReasonCodes = interceptionCodes(value.ReasonCodes)
	value.ScopeRevision = Revision(ingressScopeRevisionInput(value))
	return value, value.Validate(time.Time{})
}

func (f ForwardedIngressScopeFactV1) Validate(now time.Time) error {
	if f.Schema != IngressScopeFactSchemaV1 || !interceptionToken(f.ProviderID, 128) ||
		f.ProviderRevision != IngressScopeProviderRevisionV1 || !interceptionToken(f.ScopeID, 256) ||
		!interceptionInterfaceName(f.InterfaceName) || f.InterfaceIndex <= 0 || !interceptionDigest(f.InterfaceRevision) ||
		(f.AddressFamily != AddressFamilyIPv4 && f.AddressFamily != AddressFamilyIPv6) ||
		!validIngressOwnership(f.Ownership) || f.ObservedAt <= 0 || f.ExpiresAt <= f.ObservedAt ||
		f.ExpiresAt-f.ObservedAt > int64(MaxInterceptionFreshnessV1/time.Second) ||
		!now.IsZero() && f.ExpiresAt <= now.UTC().Unix() || !interceptionDigest(f.ScopeRevision) ||
		f.ScopeRevision != Revision(ingressScopeRevisionInput(f)) || !slices.Equal(f.ReasonCodes, interceptionCodes(f.ReasonCodes)) {
		return errors.New("forwarded_ingress_scope_fact_v1_invalid")
	}
	if f.ForwardedIngress && (f.Loopback || f.Virtual || f.Management || f.ExternalManaged || f.Ownership != IngressScopeProviderManagedV1) {
		return errors.New("forwarded_ingress_scope_fact_v1_contradictory")
	}
	return nil
}

func ReferenceIngressScopeV1(fact ForwardedIngressScopeFactV1, now time.Time) (ForwardedIngressScopeReferenceV1, error) {
	if fact.Validate(now) != nil || !fact.ForwardedIngress {
		return ForwardedIngressScopeReferenceV1{}, errors.New("forwarded_ingress_scope_not_actionable")
	}
	value := ForwardedIngressScopeReferenceV1{
		Schema: IngressScopeReferenceSchemaV1, ProviderID: fact.ProviderID, ScopeID: fact.ScopeID,
		InterfaceRevision: fact.InterfaceRevision, AddressFamily: fact.AddressFamily, ScopeRevision: fact.ScopeRevision,
	}
	value.CanonicalReferenceRevision = Revision(ingressScopeReferenceRevisionInput(value))
	return value, value.Validate()
}

func (r ForwardedIngressScopeReferenceV1) Validate() error {
	if r.Schema != IngressScopeReferenceSchemaV1 || !interceptionToken(r.ProviderID, 128) ||
		!interceptionToken(r.ScopeID, 256) || !interceptionDigest(r.InterfaceRevision) ||
		(r.AddressFamily != AddressFamilyIPv4 && r.AddressFamily != AddressFamilyIPv6) ||
		!interceptionDigest(r.ScopeRevision) || !interceptionDigest(r.CanonicalReferenceRevision) ||
		r.CanonicalReferenceRevision != Revision(ingressScopeReferenceRevisionInput(r)) {
		return errors.New("forwarded_ingress_scope_reference_v1_invalid")
	}
	return nil
}

func ResolveExactIngressScopeV1(reference ForwardedIngressScopeReferenceV1, fact ForwardedIngressScopeFactV1, now time.Time) error {
	if reference.Validate() != nil || fact.Validate(now) != nil || !fact.ForwardedIngress ||
		reference.ProviderID != fact.ProviderID || reference.ScopeID != fact.ScopeID ||
		reference.InterfaceRevision != fact.InterfaceRevision || reference.AddressFamily != fact.AddressFamily ||
		reference.ScopeRevision != fact.ScopeRevision {
		return errors.New("forwarded_ingress_scope_reference_v1_stale")
	}
	return nil
}

func FinalizeInterceptionLeaseV1(value InterceptionLeaseV1) (InterceptionLeaseV1, error) {
	value.Schema = InterceptionLeaseSchemaV1
	value.ReasonCodes = interceptionCodes(value.ReasonCodes)
	value.LeaseRevision = Revision(interceptionLeaseRevisionInput(value))
	return value, value.Validate()
}

func (l InterceptionLeaseV1) Validate() error {
	if l.Schema != InterceptionLeaseSchemaV1 || !interceptionToken(l.LeaseID, 128) ||
		!interceptionDigest(l.LeaseRevision) || !interceptionToken(l.AuthorityProviderID, 128) ||
		!interceptionToken(l.HolderID, 128) || l.AuthorityProviderID != l.ExactReference.ProviderID ||
		l.ExactReference.Validate() != nil || l.IssuedAt <= 0 || l.RenewedAt < l.IssuedAt ||
		l.ExpiresAt <= l.RenewedAt || l.ExpiresAt-l.RenewedAt > int64(MaxInterceptionLeaseFreshnessV1/time.Second) ||
		!slices.Equal(l.ReasonCodes, interceptionCodes(l.ReasonCodes)) ||
		l.LeaseRevision != Revision(interceptionLeaseRevisionInput(l)) {
		return errors.New("interception_lease_v1_invalid")
	}
	switch l.State {
	case EndpointLeaseReserved, EndpointLeaseMutationPending, EndpointLeaseActive, EndpointLeaseReconcileRequired:
		if l.ReleasedAt != 0 {
			return errors.New("interception_lease_v1_release_invalid")
		}
	case EndpointLeaseReleased:
		if l.ReleasedAt < l.RenewedAt {
			return errors.New("interception_lease_v1_release_invalid")
		}
	default:
		return errors.New("interception_lease_v1_state_invalid")
	}
	return nil
}

func ValidateInterceptionLeaseTransitionV1(current, next InterceptionLeaseV1, expected string, mutation EndpointLeaseMutation, now time.Time) error {
	if current.Validate() != nil || expected != current.LeaseRevision ||
		next.Validate() != nil || next.LeaseID != current.LeaseID || next.AuthorityProviderID != current.AuthorityProviderID ||
		next.HolderID != current.HolderID || next.ExactReference != current.ExactReference || next.IssuedAt != current.IssuedAt ||
		next.LeaseRevision == current.LeaseRevision || next.RenewedAt < current.RenewedAt || next.ExpiresAt < current.ExpiresAt ||
		mutation != EndpointLeaseRelease && current.ExpiresAt <= now.UTC().Unix() {
		return errors.New("interception_lease_v1_cas_stale")
	}
	want, ok := interceptionLeaseTransition(current.State, mutation)
	if !ok || next.State != want || (mutation == EndpointLeaseRelease) != (next.ReleasedAt > 0) {
		return errors.New("interception_lease_v1_transition_invalid")
	}
	if mutation == EndpointLeaseRenew && (next.RenewedAt <= current.RenewedAt || next.ExpiresAt <= current.ExpiresAt) {
		return errors.New("interception_lease_v1_transition_invalid")
	}
	return nil
}

func (r AcquireInterceptionLeaseRequestV1) Validate() error {
	if !interceptionToken(r.RequestID, 128) || !interceptionToken(r.HolderID, 128) ||
		r.ExactReference.Validate() != nil || r.FreshnessSeconds == 0 ||
		time.Duration(r.FreshnessSeconds)*time.Second > MaxInterceptionLeaseFreshnessV1 {
		return errors.New("interception_lease_acquire_request_v1_invalid")
	}
	return nil
}
func (r MutateInterceptionLeaseRequestV1) Validate(requireFreshness bool) error {
	if !interceptionToken(r.RequestID, 128) || !interceptionToken(r.LeaseID, 128) || !interceptionDigest(r.ExpectedRevision) ||
		requireFreshness && (r.FreshnessSeconds == 0 || time.Duration(r.FreshnessSeconds)*time.Second > MaxInterceptionLeaseFreshnessV1) ||
		!requireFreshness && r.FreshnessSeconds != 0 {
		return errors.New("interception_lease_mutation_request_v1_invalid")
	}
	return nil
}
func (r ReleaseInterceptionLeaseRequestV1) Validate() error {
	if !interceptionToken(r.RequestID, 128) || !interceptionToken(r.LeaseID, 128) ||
		!interceptionDigest(r.ExpectedRevision) || !interceptionDigest(r.DetachmentRevision) {
		return errors.New("interception_lease_release_request_v1_invalid")
	}
	return nil
}
func (r GetInterceptionLeaseRequestV1) Validate() error {
	if !interceptionToken(r.LeaseID, 128) {
		return errors.New("interception_lease_get_request_v1_invalid")
	}
	return nil
}
func (r ListInterceptionLeasesRequestV1) Validate() error {
	if !interceptionToken(r.HolderID, 128) || r.Limit == 0 || r.Limit > MaxInterceptionLeasePageV1 {
		return errors.New("interception_lease_list_request_v1_invalid")
	}
	return nil
}

func interceptionLeaseTransition(current EndpointLeaseState, mutation EndpointLeaseMutation) (EndpointLeaseState, bool) {
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

func validInterceptionKind(value InterceptionKindV1) bool {
	return value == InterceptionRedirectV1 || value == InterceptionTProxyV1 || value == InterceptionTUNV1
}
func validInterceptionOwnership(value InterceptionOwnershipV1) bool {
	return value == InterceptionProviderManagedV1 || value == InterceptionExternalManagedV1 || value == InterceptionOwnershipUnknownV1
}
func validInterceptionListenerState(value InterceptionListenerStateV1) bool {
	return value == InterceptionListenerObservedExactV1 || value == InterceptionListenerUnobservedV1 ||
		value == InterceptionListenerStaleV1 || value == InterceptionListenerForeignV1 || value == InterceptionListenerUnknownV1
}
func validIngressOwnership(value IngressScopeOwnershipV1) bool {
	return value == IngressScopeProviderManagedV1 || value == IngressScopeExternalManagedV1 || value == IngressScopeOwnershipUnknownV1
}

func interceptionFactRevisionInput(value InterceptionInboundFactV1) InterceptionInboundFactV1 {
	value.ObservedAt, value.ExpiresAt, value.FactRevision = 0, 0, ""
	return value
}
func interceptionReferenceRevisionInput(value InterceptionReferenceV1) InterceptionReferenceV1 {
	value.CanonicalReferenceRevision = ""
	return value
}
func ingressScopeRevisionInput(value ForwardedIngressScopeFactV1) ForwardedIngressScopeFactV1 {
	value.ObservedAt, value.ExpiresAt, value.ScopeRevision = 0, 0, ""
	return value
}
func ingressScopeReferenceRevisionInput(value ForwardedIngressScopeReferenceV1) ForwardedIngressScopeReferenceV1 {
	value.CanonicalReferenceRevision = ""
	return value
}
func interceptionLeaseRevisionInput(value InterceptionLeaseV1) InterceptionLeaseV1 {
	value.LeaseRevision = ""
	return value
}

func interceptionCodes(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, min(len(values), MaxInterceptionReasonCodesV1))
	for _, value := range values {
		value = strings.ToUpper(strings.TrimSpace(value))
		if interceptionToken(value, 96) && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	if len(result) > MaxInterceptionReasonCodesV1 {
		result = result[:MaxInterceptionReasonCodesV1]
	}
	return result
}

func interceptionToken(value string, limit int) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > limit || strings.ContainsAny(value, "/\\?#&={}[]<>\"'\r\n\t ") {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._:@+-", r) {
			continue
		}
		return false
	}
	return true
}

func interceptionInterfaceName(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 64 || strings.ContainsAny(value, "/\\?#&={}[]<>\"'\r\n\t ") {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._:@+-", r) {
			continue
		}
		return false
	}
	return true
}

func interceptionDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' && r < 'a' || r > 'f' {
			return false
		}
	}
	return true
}
