package events

import (
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
)

func TestLegacyRouteAndBlockValuesNormalizeToObserveOnlySignals(t *testing.T) {
	for _, action := range []domain.DecisionAction{domain.DecisionRouteToDecoy, domain.DecisionBlock} {
		event := ProbeEvent{ResourceID: "core:inbound:1", ResourceKind: domain.ResourceInbound, SourcePrefix: "192.0.2.1/32", SignalKind: domain.SignalFallbackHit, Action: action, SafeMeta: domain.SafeMeta{ClassifierPolicyVersion: 1}, ObservedAt: time.Unix(100, 0)}
		signal := NormalizeProbeEvent(event, string(action))
		if err := signal.Validate(time.Unix(100, 0)); err != nil {
			t.Fatal(err)
		}
		if signal.ReasonCodes[0] != domain.ReasonCompatibilityObserveOnly {
			t.Fatalf("reasons=%#v", signal.ReasonCodes)
		}
	}
}

func TestLegacyNormalizerHashesPathShapedIdentifiers(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	signal := NormalizeProbeEvent(ProbeEvent{ResourceID: `/var/lib/private/token`, SignalKind: domain.SignalFallbackHit, SafeMeta: domain.SafeMeta{ClassifierPolicyVersion: 1}, ObservedAt: now}, `C:\\secret\\event`)
	payload := signal.Subject.Value + strings.Join(signal.Provenance.EvidenceRefIDs, ",") + signal.Scope.TargetResourceID
	if strings.Contains(payload, "private") || strings.Contains(payload, "secret") || strings.ContainsAny(payload, `/\\`) {
		t.Fatalf("compatibility signal leaked a path: %#v", signal)
	}
	if err := signal.Validate(now); err != nil {
		t.Fatalf("sanitized compatibility signal is invalid: %v", err)
	}
}
