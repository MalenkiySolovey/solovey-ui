package scoring

import (
	"net/netip"
	"slices"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
)

type fixedClock struct{ now time.Time }

func (clock fixedClock) Now() time.Time { return clock.now }

func TestNormalizeSourcePrefix(t *testing.T) {
	mapped := netip.MustParseAddr("::ffff:192.0.2.10")
	prefix, err := NormalizeSourcePrefix(mapped, 64)
	if err != nil || prefix.String() != "192.0.2.10/32" {
		t.Fatalf("mapped IPv4 prefix = %s, err=%v", prefix, err)
	}
	prefix, err = NormalizeSourcePrefix(netip.MustParseAddr("2001:db8:1:2::99"), 64)
	if err != nil || prefix.String() != "2001:db8:1:2::/64" {
		t.Fatalf("IPv6 prefix = %s, err=%v", prefix, err)
	}
}

func TestIPv6DefaultIs64And48IsNotImplicitPolicy(t *testing.T) {
	policy := DefaultPolicy()
	if policy.IPv6PrefixBits != 64 {
		t.Fatalf("default IPv6 prefix = /%d, want /64", policy.IPv6PrefixBits)
	}
	address := netip.MustParseAddr("2001:db8:1:2:3::99")
	defaultPrefix, err := NormalizeSourcePrefix(address, policy.IPv6PrefixBits)
	if err != nil || defaultPrefix.String() != "2001:db8:1:2::/64" {
		t.Fatalf("default IPv6 prefix = %s, err=%v", defaultPrefix, err)
	}
	prefix48, err := NormalizeSourcePrefix(address, 48)
	if err != nil || prefix48.String() != "2001:db8:1::/48" {
		t.Fatalf("explicit /48 normalization = %s, err=%v", prefix48, err)
	}
	if prefix48 == defaultPrefix {
		t.Fatal("/48 must remain an explicit non-default choice")
	}
}

func TestApplySignalThresholdDedupeDecayAndRollingTTL(t *testing.T) {
	policy := DefaultPolicy()
	start := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	meta := domain.SafeMeta{PathClass: "scanner_env", UAClass: "scanner_cli", MethodClass: "get", StatusClass: "4xx", ClassifierPolicyVersion: 1}
	first, err := ApplySignal(ScoreState{}, Signal{
		ResourceID: "panel:web", Source: netip.MustParseAddr("198.51.100.10"),
		Kind: domain.SignalHTTPScannerPath, SafeMeta: meta,
	}, policy, fixedClock{start})
	if err != nil {
		t.Fatalf("first ApplySignal: %v", err)
	}
	if first.State.CurrentScore != 3 || first.State.ExpiresAt != nil || !first.EventAccepted {
		t.Fatalf("first state: %#v", first)
	}
	duplicate, err := ApplySignal(first.State, Signal{
		ResourceID: "panel:web", Source: netip.MustParseAddr("198.51.100.10"),
		Kind: domain.SignalHTTPScannerPath, SafeMeta: meta,
	}, policy, fixedClock{start.Add(30 * time.Second)})
	if err != nil {
		t.Fatalf("duplicate ApplySignal: %v", err)
	}
	if !duplicate.Duplicate || duplicate.EventAccepted || duplicate.State.CurrentScore != 3 || duplicate.State.RawScore != 6 {
		t.Fatalf("duplicate state: %#v", duplicate)
	}
	meta.UAClass = "empty"
	threshold, err := ApplySignal(duplicate.State, Signal{
		ResourceID: "panel:web", Source: netip.MustParseAddr("198.51.100.10"),
		Kind: domain.SignalHTTPScannerUA, SafeMeta: meta,
	}, policy, fixedClock{start.Add(2 * time.Minute)})
	if err != nil {
		t.Fatalf("threshold ApplySignal: %v", err)
	}
	if threshold.State.CurrentScore != 5 || threshold.State.ExpiresAt == nil || !threshold.State.ExpiresAt.Equal(start.Add(62*time.Minute)) {
		t.Fatalf("threshold state: %#v", threshold.State)
	}
	extended, err := ApplySignal(threshold.State, Signal{
		ResourceID: "panel:web", Source: netip.MustParseAddr("198.51.100.10"),
		Kind: domain.SignalRateLimited, SafeMeta: meta,
	}, policy, fixedClock{start.Add(3 * time.Minute)})
	if err != nil {
		t.Fatalf("rolling TTL ApplySignal: %v", err)
	}
	if extended.State.ExpiresAt == nil || !extended.State.ExpiresAt.Equal(start.Add(63*time.Minute)) {
		t.Fatalf("rolling TTL = %v", extended.State.ExpiresAt)
	}
	effective, warnings := EffectiveScore(extended.State, start.Add(34*time.Minute), policy)
	if effective != 4 || len(warnings) != 0 {
		t.Fatalf("effective score = %d warnings=%v, want 4", effective, warnings)
	}
}

func TestApplySignalClampsScoresAndReportsClockSkew(t *testing.T) {
	policy := DefaultPolicy()
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	state := ScoreState{
		ResourceID: "inbound:test", SourcePrefix: netip.MustParsePrefix("203.0.113.1/32"),
		CurrentScore: 99, RawScore: 99, LastSignalAt: now.Add(time.Hour),
	}
	result, err := ApplySignal(state, Signal{
		ResourceID: "inbound:test", Source: netip.MustParseAddr("203.0.113.1"),
		Kind:     domain.SignalConntrackPressure,
		SafeMeta: domain.SafeMeta{StatusClass: "none", ClassifierPolicyVersion: 1},
	}, policy, fixedClock{now})
	if err != nil {
		t.Fatalf("ApplySignal: %v", err)
	}
	if result.State.CurrentScore != policy.MaxScore || result.State.RawScore != policy.MaxScore || !slices.Contains(result.Warnings, "clock_skew") {
		t.Fatalf("clamped/skew state: %#v", result)
	}
}
