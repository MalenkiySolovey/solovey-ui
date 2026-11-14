package fallbacktargets

import (
	canonical "github.com/MalenkiySolovey/solovey-host/componenthost/fallbacktargets"
	"time"
)

const (
	TargetSchemaV2              = canonical.TargetSchemaV2
	TargetReferenceSchemaV2     = canonical.TargetReferenceSchemaV2
	MaxProvidersV2              = canonical.MaxProvidersV2
	MaxTargetsV2                = canonical.MaxTargetsV2
	MaxReservationsV2           = canonical.MaxReservationsV2
	MaxApplicationProtocolsV2   = canonical.MaxApplicationProtocolsV2
	MaxAcceptedServerNamesV2    = canonical.MaxAcceptedServerNamesV2
	MaxReasonCodesV2            = canonical.MaxReasonCodesV2
	MaxReasonCodeLengthV2       = canonical.MaxReasonCodeLengthV2
	MaxOpaqueIDLengthV2         = canonical.MaxOpaqueIDLengthV2
	MaxSourceLengthV2           = canonical.MaxSourceLengthV2
	MaxConnectFirstByteP95MSV2  = canonical.MaxConnectFirstByteP95MSV2
	MaxReservationListPageV2    = canonical.MaxReservationListPageV2
	DefaultProviderTimeoutV2    = canonical.DefaultProviderTimeoutV2
	MaxProviderRequestTimeoutV2 = canonical.MaxProviderRequestTimeoutV2
	MaxTargetFreshnessV2        = canonical.MaxTargetFreshnessV2
	MaxTargetClockSkewV2        = canonical.MaxTargetClockSkewV2
)

type TransportSecurity = canonical.TransportSecurity

const (
	TransportSecurityUnknown   = canonical.TransportSecurityUnknown
	TransportSecurityPlaintext = canonical.TransportSecurityPlaintext
	TransportSecurityTLS       = canonical.TransportSecurityTLS
)

type ApplicationProtocol = canonical.ApplicationProtocol

const (
	ApplicationProtocolUnknown = canonical.ApplicationProtocolUnknown
	ApplicationProtocolHTTP11  = canonical.ApplicationProtocolHTTP11
	ApplicationProtocolHTTP2   = canonical.ApplicationProtocolHTTP2
)

type CapacityState = canonical.CapacityState

const (
	CapacityUnknown   = canonical.CapacityUnknown
	CapacityReady     = canonical.CapacityReady
	CapacityPressured = canonical.CapacityPressured
	CapacityExhausted = canonical.CapacityExhausted
	CapacityStale     = canonical.CapacityStale
)

type PublishFactsV2 = canonical.PublishFactsV2

type EndpointV2 = canonical.EndpointV2

type HealthV2 = canonical.HealthV2

type CapacityV2 = canonical.CapacityV2

type FallbackTargetV2 = canonical.FallbackTargetV2

type FallbackTargetReferenceV2 = canonical.FallbackTargetReferenceV2

func FinalizeFallbackTargetV2(target FallbackTargetV2) (FallbackTargetV2, error) {
	return canonical.FinalizeFallbackTargetV2(target)
}

func ReferenceV2FromTarget(target FallbackTargetV2) (FallbackTargetReferenceV2, error) {
	return canonical.ReferenceV2FromTarget(target)
}

func ResolveExactV2(reference FallbackTargetReferenceV2, target FallbackTargetV2, now time.Time) error {
	return canonical.ResolveExactV2(reference, target, now)
}

func EffectiveReadinessV2(health HealthV2, now time.Time) Readiness {
	return canonical.EffectiveReadinessV2(health, now)
}

func EffectiveCapacityStateV2(capacity CapacityV2, now time.Time) CapacityState {
	return canonical.EffectiveCapacityStateV2(capacity, now)
}

func validatedCanonicalTargetV2(target FallbackTargetV2) (FallbackTargetV2, error) {
	return canonical.CanonicalTargetV2(target)
}
func validReasonCodesV2(values []string) bool { return canonical.ReasonCodesValidV2(values) }
