package decision

import (
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/scoring"
)

func TestDecideKeepsDangerousActionsBehindCapabilities(t *testing.T) {
	state := scoring.ScoreState{ResourceID: "inbound:test", SourcePrefix: netip.MustParsePrefix("192.0.2.5/32"), CurrentScore: 8}
	blocked := Decide(state, Context{Enabled: true, Threshold: 5, RequestedAction: domain.DecisionBlock})
	if blocked.Action != domain.DecisionRecordOnly || blocked.Support != domain.SupportMissingCapability {
		t.Fatalf("block without capability: %#v", blocked)
	}
	allowed := Decide(state, Context{
		Enabled: true, Threshold: 5, RequestedAction: domain.DecisionBlock,
		Capabilities: Capabilities{HardBlock: true, AdvancedAccepted: true},
	})
	if allowed.Action != domain.DecisionBlock || allowed.Support != domain.SupportSupported {
		t.Fatalf("block with capability: %#v", allowed)
	}
	stale := Decide(state, Context{Enabled: true, ResourceStale: true, Threshold: 5, RequestedAction: domain.DecisionRateLimit, Capabilities: Capabilities{RateLimit: true}})
	if stale.Action != domain.DecisionRecordOnly || stale.Support != domain.SupportDegraded {
		t.Fatalf("stale resource decision: %#v", stale)
	}
}

func TestLegacyDecisionNormalizerHashesUnsafeResourceAndNeverResolvesAction(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	value := NormalizeScoreState(scoring.ScoreState{ResourceID: `/var/lib/private/token`, SourcePrefix: netip.Prefix{}, LastSignalAt: now}, string(domain.DecisionBlock), now)
	if strings.Contains(value.Scope.TargetResourceID, "private") || strings.ContainsAny(value.Scope.TargetResourceID, `/\\`) {
		t.Fatalf("legacy decision leaked resource path: %#v", value)
	}
	if value.CapabilityResolution.Implemented || value.CapabilityResolution.ResolvedIntent != domain.IntentObserve || value.State == domain.DecisionApplied {
		t.Fatalf("legacy enum became actionable: %#v", value)
	}
	if err := value.Validate(now); err != nil {
		t.Fatalf("normalized decision is invalid: %v", err)
	}
}
