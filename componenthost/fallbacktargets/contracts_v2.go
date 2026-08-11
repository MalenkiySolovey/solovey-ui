package fallbacktargets

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

const (
	TargetSchemaV2              = "solovey-ui/fallback-target/v2"
	TargetReferenceSchemaV2     = "solovey-ui/fallback-target-reference/v2"
	MaxProvidersV2              = 128
	MaxTargetsV2                = 4096
	MaxReservationsV2           = 4096
	MaxApplicationProtocolsV2   = 16
	MaxAcceptedServerNamesV2    = 32
	MaxReasonCodesV2            = 32
	MaxReasonCodeLengthV2       = 64
	MaxOpaqueIDLengthV2         = 128
	MaxSourceLengthV2           = 64
	MaxConnectFirstByteP95MSV2  = 300_000
	MaxReservationListPageV2    = 256
	DefaultProviderTimeoutV2    = 5 * time.Second
	MaxProviderRequestTimeoutV2 = 30 * time.Second
	MaxTargetFreshnessV2        = 5 * time.Minute
	MaxTargetClockSkewV2        = 5 * time.Minute
)

type TransportSecurity string

const (
	TransportSecurityUnknown   TransportSecurity = "UNKNOWN"
	TransportSecurityPlaintext TransportSecurity = "PLAINTEXT"
	TransportSecurityTLS       TransportSecurity = "TLS"
)

type ApplicationProtocol string

const (
	ApplicationProtocolUnknown ApplicationProtocol = "UNKNOWN"
	ApplicationProtocolHTTP11  ApplicationProtocol = "HTTP_1_1"
	ApplicationProtocolHTTP2   ApplicationProtocol = "HTTP_2"
)

type CapacityState string

const (
	CapacityUnknown   CapacityState = "UNKNOWN"
	CapacityReady     CapacityState = "READY"
	CapacityPressured CapacityState = "PRESSURED"
	CapacityExhausted CapacityState = "EXHAUSTED"
	CapacityStale     CapacityState = "STALE"
)

type PublishFactsV2 struct {
	Revision      string `json:"revision"`
	ContentDigest string `json:"contentDigest"`
}

type EndpointV2 struct {
	EndpointID           string                        `json:"endpointId"`
	EndpointRevision     string                        `json:"endpointRevision"`
	Network              hostresources.Network         `json:"network"`
	AddressFamily        hostresources.AddressFamily   `json:"addressFamily"`
	Address              string                        `json:"address"`
	Port                 uint16                        `json:"port"`
	Local                bool                          `json:"local"`
	TransportSecurity    TransportSecurity             `json:"transportSecurity"`
	ApplicationProtocols []ApplicationProtocol         `json:"applicationProtocols"`
	AcceptedServerNames  []string                      `json:"acceptedServerNames"`
	ProxyProtocol        hostresources.CapabilityValue `json:"proxyProtocol"`
	CanReachManagement   hostresources.CapabilityValue `json:"canReachManagement"`
}

type HealthV2 struct {
	Readiness             Readiness `json:"readiness"`
	Revision              string    `json:"revision"`
	ObservedAt            int64     `json:"observedAt"`
	ExpiresAt             int64     `json:"expiresAt"`
	ConnectFirstByteP95MS *uint32   `json:"connectFirstByteP95Ms,omitempty"`
	ReasonCodes           []string  `json:"reasonCodes,omitempty"`
}

type CapacityV2 struct {
	Revision              string        `json:"revision"`
	State                 CapacityState `json:"state"`
	ReservationSlotsTotal uint32        `json:"reservationSlotsTotal"`
	ReservationSlotsUsed  uint32        `json:"reservationSlotsUsed"`
	ObservedAt            int64         `json:"observedAt"`
	ExpiresAt             int64         `json:"expiresAt"`
	ReasonCodes           []string      `json:"reasonCodes,omitempty"`
}

type FallbackTargetV2 struct {
	Schema                  string         `json:"schema"`
	Identity                TargetIdentity `json:"identity"`
	Publish                 PublishFactsV2 `json:"publish"`
	Endpoint                EndpointV2     `json:"endpoint"`
	Health                  HealthV2       `json:"health"`
	Capacity                CapacityV2     `json:"capacity"`
	ProviderRevision        string         `json:"providerRevision"`
	Source                  string         `json:"source"`
	ConfidenceBP            int            `json:"confidenceBp"`
	CanonicalTargetRevision string         `json:"canonicalTargetRevision"`
}

type FallbackTargetReferenceV2 struct {
	Schema                 string `json:"schema"`
	ProviderID             string `json:"providerId"`
	TargetID               string `json:"targetId"`
	PublishRevision        string `json:"publishRevision"`
	ContentDigest          string `json:"contentDigest"`
	EndpointID             string `json:"endpointId"`
	EndpointRevision       string `json:"endpointRevision"`
	ProviderHealthRevision string `json:"providerHealthRevision"`
	CapacityRevision       string `json:"capacityRevision"`
	ProviderRevision       string `json:"providerRevision"`
}

func (t FallbackTargetV2) Validate() error {
	_, err := validatedCanonicalTargetV2(t)
	return err
}

func FinalizeFallbackTargetV2(target FallbackTargetV2) (FallbackTargetV2, error) {
	target.Schema = TargetSchemaV2
	canonical, err := canonicalTargetV2(target, false)
	if err != nil {
		return FallbackTargetV2{}, err
	}
	canonical.Endpoint.EndpointRevision = revisionOf(endpointRevisionInput(canonical.Endpoint))
	canonical.Health.Revision = revisionOf(healthRevisionInput(canonical.Health))
	canonical.Capacity.Revision = revisionOf(capacityRevisionInput(canonical.Capacity))
	canonical.CanonicalTargetRevision = revisionOf(targetRevisionInput(canonical))
	return canonical, nil
}

func ReferenceV2FromTarget(target FallbackTargetV2) (FallbackTargetReferenceV2, error) {
	if err := target.Validate(); err != nil {
		return FallbackTargetReferenceV2{}, err
	}
	return FallbackTargetReferenceV2{
		Schema: TargetReferenceSchemaV2, ProviderID: target.Identity.ProviderID,
		TargetID: target.Identity.TargetID, PublishRevision: target.Publish.Revision,
		ContentDigest: target.Publish.ContentDigest, EndpointID: target.Endpoint.EndpointID,
		EndpointRevision: target.Endpoint.EndpointRevision, ProviderHealthRevision: target.Health.Revision,
		CapacityRevision: target.Capacity.Revision, ProviderRevision: target.ProviderRevision,
	}, nil
}

func (r FallbackTargetReferenceV2) Validate() error {
	if r.Schema != TargetReferenceSchemaV2 || !validOpaqueID(r.ProviderID, MaxOpaqueIDLengthV2) ||
		!validOpaqueID(r.TargetID, MaxOpaqueIDLengthV2) || !validOpaqueID(r.PublishRevision, MaxOpaqueIDLengthV2) ||
		!isSHA256(r.ContentDigest) || !validOpaqueID(r.EndpointID, MaxOpaqueIDLengthV2) ||
		!isSHA256(r.EndpointRevision) || !isSHA256(r.ProviderHealthRevision) ||
		!isSHA256(r.CapacityRevision) || !validOpaqueID(r.ProviderRevision, MaxOpaqueIDLengthV2) {
		return errors.New("fallback_target_reference_v2_invalid")
	}
	return nil
}

func ResolveExactV2(reference FallbackTargetReferenceV2, target FallbackTargetV2, now time.Time) error {
	if err := reference.Validate(); err != nil {
		return err
	}
	if err := target.Validate(); err != nil {
		return errors.New("fallback_target_v2_invalid")
	}
	if reference.ProviderID != target.Identity.ProviderID || reference.TargetID != target.Identity.TargetID {
		return errors.New("fallback_target_v2_missing")
	}
	if reference.PublishRevision != target.Publish.Revision || reference.ContentDigest != target.Publish.ContentDigest ||
		reference.EndpointID != target.Endpoint.EndpointID || reference.EndpointRevision != target.Endpoint.EndpointRevision ||
		reference.ProviderHealthRevision != target.Health.Revision || reference.CapacityRevision != target.Capacity.Revision ||
		reference.ProviderRevision != target.ProviderRevision {
		return errors.New("fallback_target_reference_v2_stale")
	}
	if EffectiveReadinessV2(target.Health, now) != ReadinessReady {
		return errors.New("fallback_target_health_not_actionable")
	}
	if EffectiveCapacityStateV2(target.Capacity, now) != CapacityReady {
		return errors.New("fallback_target_capacity_not_actionable")
	}
	if len(target.Health.ReasonCodes) != 0 || len(target.Capacity.ReasonCodes) != 0 {
		return errors.New("fallback_target_v2_has_unresolved_reasons")
	}
	return nil
}

func EffectiveReadinessV2(health HealthV2, now time.Time) Readiness {
	if health.Readiness == ReadinessUnknown {
		return ReadinessUnknown
	}
	if health.ObservedAt > now.UTC().Add(MaxTargetClockSkewV2).Unix() {
		return ReadinessUnknown
	}
	if health.ExpiresAt <= now.UTC().Unix() {
		return ReadinessStale
	}
	return health.Readiness
}

func EffectiveCapacityStateV2(capacity CapacityV2, now time.Time) CapacityState {
	if capacity.State == CapacityUnknown {
		return CapacityUnknown
	}
	if capacity.State == CapacityStale {
		return CapacityStale
	}
	if capacity.ObservedAt > now.UTC().Add(MaxTargetClockSkewV2).Unix() {
		return CapacityUnknown
	}
	if capacity.ExpiresAt <= now.UTC().Unix() {
		return CapacityStale
	}
	return capacity.State
}

func validatedCanonicalTargetV2(target FallbackTargetV2) (FallbackTargetV2, error) {
	canonical, err := canonicalTargetV2(target, true)
	if err != nil {
		return FallbackTargetV2{}, err
	}
	if canonical.CanonicalTargetRevision != revisionOf(targetRevisionInput(canonical)) {
		return FallbackTargetV2{}, errors.New("fallback_target_revision_mismatch")
	}
	return canonical, nil
}

func canonicalTargetV2(target FallbackTargetV2, requireRevisions bool) (FallbackTargetV2, error) {
	if target.Schema != TargetSchemaV2 || !validOpaqueID(target.Identity.ProviderID, MaxOpaqueIDLengthV2) ||
		!validOpaqueID(target.Identity.TargetID, MaxOpaqueIDLengthV2) || !validOpaqueID(target.Publish.Revision, MaxOpaqueIDLengthV2) ||
		!isSHA256(target.Publish.ContentDigest) || !validOpaqueID(target.ProviderRevision, MaxOpaqueIDLengthV2) ||
		!validSafeToken(target.Source, MaxSourceLengthV2) || target.ConfidenceBP < 0 || target.ConfidenceBP > 10_000 {
		return FallbackTargetV2{}, errors.New("fallback_target_v2_identity_or_provider_invalid")
	}
	target.Endpoint.ApplicationProtocols = canonicalProtocols(target.Endpoint.ApplicationProtocols)
	target.Endpoint.AcceptedServerNames = canonicalServerNames(target.Endpoint.AcceptedServerNames)
	target.Health.ReasonCodes = canonicalReasonCodesV2(target.Health.ReasonCodes)
	target.Capacity.ReasonCodes = canonicalReasonCodesV2(target.Capacity.ReasonCodes)
	if err := validateEndpointV2(target.Endpoint, requireRevisions); err != nil {
		return FallbackTargetV2{}, err
	}
	if err := validateHealthV2(target.Health, requireRevisions); err != nil {
		return FallbackTargetV2{}, err
	}
	if err := validateCapacityV2(target.Capacity, requireRevisions); err != nil {
		return FallbackTargetV2{}, err
	}
	if requireRevisions {
		if !isSHA256(target.CanonicalTargetRevision) || target.Endpoint.EndpointRevision != revisionOf(endpointRevisionInput(target.Endpoint)) ||
			target.Health.Revision != revisionOf(healthRevisionInput(target.Health)) || target.Capacity.Revision != revisionOf(capacityRevisionInput(target.Capacity)) {
			return FallbackTargetV2{}, errors.New("fallback_target_v2_revision_invalid")
		}
	}
	return target, nil
}

func validateEndpointV2(endpoint EndpointV2, requireRevision bool) error {
	if !validOpaqueID(endpoint.EndpointID, MaxOpaqueIDLengthV2) || (requireRevision && !isSHA256(endpoint.EndpointRevision)) ||
		endpoint.Network != hostresources.NetworkTCP || endpoint.Port == 0 || !endpoint.Local ||
		endpoint.ProxyProtocol != hostresources.CapabilityNo || endpoint.CanReachManagement != hostresources.CapabilityNo {
		return errors.New("fallback_target_v2_endpoint_invalid")
	}
	ip := net.ParseIP(endpoint.Address)
	if ip == nil || (!ip.IsLoopback()) {
		return errors.New("fallback_target_v2_endpoint_address_invalid")
	}
	if endpoint.AddressFamily == hostresources.AddressFamilyIPv4 {
		if ip.To4() == nil || endpoint.Address != ip.To4().String() {
			return errors.New("fallback_target_v2_endpoint_family_invalid")
		}
	} else if endpoint.AddressFamily == hostresources.AddressFamilyIPv6 {
		if ip.To4() != nil || endpoint.Address != ip.String() {
			return errors.New("fallback_target_v2_endpoint_family_invalid")
		}
	} else {
		return errors.New("fallback_target_v2_endpoint_family_invalid")
	}
	switch endpoint.TransportSecurity {
	case TransportSecurityUnknown, TransportSecurityPlaintext, TransportSecurityTLS:
	default:
		return errors.New("fallback_target_v2_transport_security_invalid")
	}
	if len(endpoint.ApplicationProtocols) == 0 || len(endpoint.ApplicationProtocols) > MaxApplicationProtocolsV2 || len(endpoint.AcceptedServerNames) > MaxAcceptedServerNamesV2 {
		return errors.New("fallback_target_v2_endpoint_bounds_invalid")
	}
	for _, protocol := range endpoint.ApplicationProtocols {
		switch protocol {
		case ApplicationProtocolUnknown, ApplicationProtocolHTTP11, ApplicationProtocolHTTP2:
		default:
			return errors.New("fallback_target_v2_application_protocol_invalid")
		}
	}
	for _, name := range endpoint.AcceptedServerNames {
		if !validServerName(name) {
			return errors.New("fallback_target_v2_server_name_invalid")
		}
	}
	return nil
}

func validateHealthV2(health HealthV2, requireRevision bool) error {
	if requireRevision && !isSHA256(health.Revision) {
		return errors.New("fallback_target_v2_health_revision_invalid")
	}
	switch health.Readiness {
	case ReadinessReady, ReadinessNotReady, ReadinessStale, ReadinessUnknown:
	default:
		return errors.New("fallback_target_v2_health_state_invalid")
	}
	if health.ObservedAt <= 0 || health.ExpiresAt <= health.ObservedAt ||
		health.ExpiresAt-health.ObservedAt > int64(MaxTargetFreshnessV2/time.Second) || !validReasonCodesV2(health.ReasonCodes) {
		return errors.New("fallback_target_v2_health_invalid")
	}
	if health.ConnectFirstByteP95MS != nil && *health.ConnectFirstByteP95MS > MaxConnectFirstByteP95MSV2 {
		return errors.New("fallback_target_v2_latency_invalid")
	}
	return nil
}

func validateCapacityV2(capacity CapacityV2, requireRevision bool) error {
	if requireRevision && !isSHA256(capacity.Revision) {
		return errors.New("fallback_target_v2_capacity_revision_invalid")
	}
	switch capacity.State {
	case CapacityUnknown, CapacityReady, CapacityPressured, CapacityExhausted, CapacityStale:
	default:
		return errors.New("fallback_target_v2_capacity_state_invalid")
	}
	if capacity.ObservedAt <= 0 || capacity.ExpiresAt <= capacity.ObservedAt ||
		capacity.ExpiresAt-capacity.ObservedAt > int64(MaxTargetFreshnessV2/time.Second) ||
		capacity.ReservationSlotsTotal > MaxReservationsV2 || capacity.ReservationSlotsUsed > capacity.ReservationSlotsTotal ||
		!validReasonCodesV2(capacity.ReasonCodes) {
		return errors.New("fallback_target_v2_capacity_invalid")
	}
	if capacity.State == CapacityReady || capacity.State == CapacityPressured {
		if capacity.ReservationSlotsTotal == 0 || capacity.ReservationSlotsUsed >= capacity.ReservationSlotsTotal {
			return errors.New("fallback_target_v2_capacity_contradictory")
		}
	}
	if capacity.State == CapacityExhausted && capacity.ReservationSlotsUsed < capacity.ReservationSlotsTotal {
		return errors.New("fallback_target_v2_capacity_contradictory")
	}
	return nil
}

func validReasonCodesV2(values []string) bool {
	if len(values) > MaxReasonCodesV2 {
		return false
	}
	for _, value := range values {
		if !validSafeToken(value, MaxReasonCodeLengthV2) {
			return false
		}
	}
	return true
}

func canonicalReasonCodesV2(values []string) []string {
	out := append([]string{}, values...)
	sort.Strings(out)
	return out
}

func canonicalProtocols(values []ApplicationProtocol) []ApplicationProtocol {
	out := append([]ApplicationProtocol(nil), values...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func canonicalServerNames(values []string) []string {
	out := append([]string(nil), values...)
	for index := range out {
		out[index] = strings.ToLower(out[index])
	}
	sort.Strings(out)
	return out
}

func validSafeToken(value string, limit int) bool {
	if !utf8.ValidString(value) || value == "" || value != strings.TrimSpace(value) || len(value) > limit || strings.ContainsAny(value, "\\/?:#&={}[]<>\"'\r\n\t ") {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character == 0x7f {
			return false
		}
	}
	return true
}

func validServerName(value string) bool {
	if !utf8.ValidString(value) || value == "" || len(value) > 253 || value != strings.ToLower(value) || strings.ContainsAny(value, "/\\:@?#&={}[]<>\"'\r\n\t ") {
		return false
	}
	labels := strings.Split(value, ".")
	for _, label := range labels {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '-' {
				continue
			}
			return false
		}
	}
	return true
}

func revisionOf(value any) string {
	payload, _ := json.Marshal(value)
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func endpointRevisionInput(endpoint EndpointV2) any {
	endpoint.EndpointRevision = ""
	return endpoint
}

func healthRevisionInput(health HealthV2) any {
	health.Revision = ""
	return health
}

func capacityRevisionInput(capacity CapacityV2) any {
	capacity.Revision = ""
	return capacity
}

func targetRevisionInput(target FallbackTargetV2) any {
	target.CanonicalTargetRevision = ""
	return target
}
