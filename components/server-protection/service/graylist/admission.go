package graylist

import (
	"errors"
	"net/netip"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
)

type AdmissionContext struct {
	SelectedAdapterID       string
	ExposesOriginalSubject  bool
	TrustedEndpointSource   bool
	TargetIsHealthTelemetry bool
	Now                     time.Time
}

type AcceptedSignal struct {
	Signal        domain.ProtectionSignalV2
	Delta         int
	Weak          bool
	EvidenceClass domain.GraylistEvidenceClassV2
}

// Admit is the sole V2 scoring admission boundary. Contract-valid unknown,
// compatibility-only, stale, ambiguous, or unscoped facts remain inspectable
// but cannot influence a graylist state.
func Admit(signal domain.ProtectionSignalV2, input AdmissionContext) (AcceptedSignal, error) {
	now := input.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if err := signal.Validate(now); err != nil {
		return AcceptedSignal{}, err
	}
	if !signal.ExpiresAt.After(now) || !signal.KnownKind {
		return AcceptedSignal{}, errors.New("signal is stale or unknown")
	}
	if signal.Scope.Scope != domain.ScopeEndpoint || !domain.ValidContractID(signal.Scope.TargetResourceID, 256) ||
		!domain.ValidContractID(signal.Scope.EndpointID, 256) ||
		(signal.Scope.Transport != "tcp" && signal.Scope.Transport != "udp") ||
		!domain.ValidContractID(signal.Provenance.ObservationWindowID, 128) {
		return AcceptedSignal{}, errors.New("signal lacks exact resource, endpoint, transport, or observation window")
	}
	if !domain.ValidContractID(input.SelectedAdapterID, 128) || signal.Provenance.AdapterID != input.SelectedAdapterID {
		return AcceptedSignal{}, errors.New("signal adapter attribution is ambiguous")
	}
	if failClosedReason(signal.ReasonCodes) || failClosedMetadata(signal.SafeMeta) || forbiddenMetadata(signal.SafeMeta) {
		return AcceptedSignal{}, errors.New("signal provenance or metadata fails closed")
	}
	if input.TargetIsHealthTelemetry || loopbackSubject(signal.Subject) {
		return AcceptedSignal{}, errors.New("loopback target telemetry is not an Internet subject signal")
	}

	kind := domain.SignalKind(signal.Kind)
	switch kind {
	case domain.SignalFallbackHit, domain.SignalMissingSNI, domain.SignalUnexpectedALPN:
		if !input.ExposesOriginalSubject {
			return AcceptedSignal{}, errors.New("selected adapter does not expose the original subject")
		}
	case domain.SignalSYNBurst, domain.SignalConntrackPressure:
		if signal.Source.SourceClass != "native" || !input.TrustedEndpointSource {
			return AcceptedSignal{}, errors.New("connection pressure lacks an exact trusted endpoint source")
		}
	case domain.SignalExternalReputation:
		if signal.Source.SourceClass != "external" {
			return AcceptedSignal{}, errors.New("reputation signal is not externally attributed")
		}
	default:
		if signal.Source.SourceClass != "native" {
			return AcceptedSignal{}, errors.New("non-reputation external signal is not admitted")
		}
	}
	delta := domain.DefaultSignalDelta(kind)
	weak := kind == domain.SignalShortTLSSession || kind == domain.SignalTinyTransfer
	if signal.Source.SourceClass == "external" && delta > 2 {
		delta = 2
	}
	if delta <= 0 {
		return AcceptedSignal{}, errors.New("signal has no bounded policy contribution")
	}
	evidenceClass := domain.GraylistEvidenceStrongTrusted
	if signal.Source.SourceClass == "external" {
		evidenceClass = domain.GraylistEvidenceExternal
	} else if weak {
		evidenceClass = domain.GraylistEvidenceWeak
	} else if !input.ExposesOriginalSubject && !input.TrustedEndpointSource {
		return AcceptedSignal{}, errors.New("signal lacks strong trusted same-scope attribution")
	}
	return AcceptedSignal{Signal: signal, Delta: delta, Weak: weak, EvidenceClass: evidenceClass}, nil
}

func failClosedReason(reasons []string) bool {
	for _, reason := range reasons {
		lower := strings.ToLower(strings.TrimSpace(reason))
		for _, marker := range []string{"unknown", "stale", "truncated", "ambiguous", "legacy", "observe_only", "untrusted"} {
			if strings.Contains(lower, marker) {
				return true
			}
		}
	}
	return false
}

func failClosedMetadata(meta map[string]string) bool {
	for key, value := range meta {
		lowerKey := strings.ToLower(strings.TrimSpace(key))
		lowerValue := strings.ToLower(strings.TrimSpace(value))
		if (lowerKey == "truncated" || lowerKey == "ambiguous" || lowerKey == "unknown_provenance" || lowerKey == "stale") &&
			(lowerValue == "true" || lowerValue == "1" || lowerValue == "yes") {
			return true
		}
	}
	return false
}

func forbiddenMetadata(meta map[string]string) bool {
	return domain.ValidateProtectionSignalSafeMeta(meta) != nil
}

func loopbackSubject(subject domain.SignalSubjectV2) bool {
	switch subject.Type {
	case "ip":
		address, err := netip.ParseAddr(subject.Value)
		return err == nil && address.IsLoopback()
	case "prefix":
		prefix, err := netip.ParsePrefix(subject.Value)
		return err == nil && prefix.Addr().IsLoopback()
	default:
		return false
	}
}
