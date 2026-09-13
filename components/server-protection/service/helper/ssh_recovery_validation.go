package helper

import (
	"net/netip"
	"strings"
	"time"
)

// ValidSSHRecoveryResult is the untrusted transport-side validation of the
// typed owner projection. It enforces the same request, cardinality,
// freshness, canonical-network, and revision bounds before policy can consume
// a helper response.
func ValidSSHRecoveryResult(request SSHRecoveryObserveRequest, result *SSHRecoveryResult, now time.Time) bool {
	if result == nil || !validSHA256(result.VerifierRevision) || !validSHA256(result.ObserverRevision) ||
		request.MaxEvents < 1 || request.MaxEvents > 64 || len(result.Observations) > request.MaxEvents {
		return false
	}
	seen := make(map[string]bool, len(result.Observations))
	previousMicros, nowMicros := int64(0), now.UTC().UnixMicro()
	for _, observation := range result.Observations {
		prefix, err := netip.ParsePrefix(observation.SourcePrefix)
		if err != nil || prefix.String() != observation.SourcePrefix || prefix.Addr().IsUnspecified() || prefix.Addr().IsMulticast() ||
			prefix.Bits() != 32 && prefix.Bits() != 128 || observation.AuthenticationClass != "publickey" ||
			!prefixedRecoveryDigest(observation.ObservationID, "recovery:") || !prefixedRecoveryDigest(observation.PrincipalID, "principal:") ||
			observation.ObservedAtMicros <= request.SinceUnixMicros || observation.ObservedAtMicros > nowMicros+int64((5*time.Minute)/time.Microsecond) ||
			observation.ObservedAt != observation.ObservedAtMicros/1_000_000 || observation.ObservedAtMicros < previousMicros || seen[observation.ObservationID] {
			return false
		}
		seen[observation.ObservationID] = true
		previousMicros = observation.ObservedAtMicros
	}
	return true
}

func prefixedRecoveryDigest(value, prefix string) bool {
	return strings.HasPrefix(value, prefix) && validSHA256(strings.TrimPrefix(value, prefix))
}
