package scoring

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
)

const maxReasons = 8

type Clock interface {
	Now() time.Time
}

type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now() }

type Policy struct {
	Threshold          int
	GraylistTTL        time.Duration
	MaxScore           int
	IPv6PrefixBits     int
	DecayInterval      time.Duration
	DedupeWindow       time.Duration
	ClockSkewTolerance time.Duration
	SafeMetaMaxBytes   int
}

func DefaultPolicy() Policy {
	return Policy{
		Threshold:          domain.DefaultScoreThreshold,
		GraylistTTL:        domain.DefaultGraylistTTLSeconds * time.Second,
		MaxScore:           domain.DefaultMaxScore,
		IPv6PrefixBits:     domain.DefaultIPv6PrefixBits,
		DecayInterval:      10 * time.Minute,
		DedupeWindow:       time.Minute,
		ClockSkewTolerance: 5 * time.Minute,
		SafeMetaMaxBytes:   domain.DefaultSafeMetaMaxBytes,
	}
}

func (value Policy) Validate() error {
	if value.Threshold < 1 || value.MaxScore < value.Threshold {
		return errors.New("score threshold must be positive and not exceed max score")
	}
	if value.GraylistTTL < time.Minute || value.DecayInterval <= 0 || value.DedupeWindow < 0 {
		return errors.New("invalid scoring time policy")
	}
	if value.IPv6PrefixBits < 32 || value.IPv6PrefixBits > 128 {
		return errors.New("IPv6 prefix bits must be between 32 and 128")
	}
	if value.ClockSkewTolerance < 0 || value.SafeMetaMaxBytes < 128 {
		return errors.New("invalid scoring safety limits")
	}
	return nil
}

type ScoreKey struct {
	ResourceID string
	Prefix     netip.Prefix
}

type Signal struct {
	ResourceID string
	Source     netip.Addr
	Kind       domain.SignalKind
	Delta      int
	ObservedAt time.Time
	SafeMeta   domain.SafeMeta
}

type ScoreReason struct {
	Kind      domain.SignalKind `json:"kind"`
	Count     int               `json:"count"`
	LastSeen  time.Time         `json:"lastSeen"`
	SafeLabel string            `json:"safeLabel,omitempty"`
}

type ScoreState struct {
	ResourceID              string                `json:"resourceId"`
	SourcePrefix            netip.Prefix          `json:"sourcePrefix"`
	CurrentScore            int                   `json:"currentScore"`
	RawScore                int                   `json:"rawScore"`
	FirstSeenAt             time.Time             `json:"firstSeenAt"`
	LastSignalAt            time.Time             `json:"lastSignalAt"`
	ExpiresAt               *time.Time            `json:"expiresAt,omitempty"`
	Reasons                 []ScoreReason         `json:"reasons"`
	LastDecision            domain.DecisionAction `json:"lastDecision"`
	LastDedupeKey           string                `json:"lastDedupeKey,omitempty"`
	LastDedupeAt            time.Time             `json:"lastDedupeAt"`
	DedupedCount            int                   `json:"dedupedCount"`
	ClassifierPolicyVersion int                   `json:"classifierPolicyVersion"`
}

type ApplyResult struct {
	State         ScoreState
	EventAccepted bool
	Duplicate     bool
	DedupeKey     string
	Warnings      []string
}

func NormalizeSourcePrefix(addr netip.Addr, ipv6PrefixBits int) (netip.Prefix, error) {
	if !addr.IsValid() {
		return netip.Prefix{}, errors.New("source address is invalid")
	}
	addr = addr.Unmap()
	if addr.Is4() {
		return netip.PrefixFrom(addr, 32), nil
	}
	if ipv6PrefixBits < 32 || ipv6PrefixBits > 128 {
		return netip.Prefix{}, errors.New("IPv6 prefix bits must be between 32 and 128")
	}
	return netip.PrefixFrom(addr, ipv6PrefixBits).Masked(), nil
}

func EffectiveScore(state ScoreState, now time.Time, policy Policy) (int, []string) {
	if state.LastSignalAt.IsZero() || !now.After(state.LastSignalAt) {
		if !state.LastSignalAt.IsZero() && now.Before(state.LastSignalAt.Add(-policy.ClockSkewTolerance)) {
			return clamp(state.CurrentScore, policy.MaxScore), []string{"clock_skew"}
		}
		return clamp(state.CurrentScore, policy.MaxScore), nil
	}
	steps := int(now.Sub(state.LastSignalAt) / policy.DecayInterval)
	return max(0, clamp(state.CurrentScore, policy.MaxScore)-steps), nil
}

func ApplySignal(state ScoreState, signal Signal, policy Policy, clock Clock) (ApplyResult, error) {
	if clock == nil {
		return ApplyResult{}, errors.New("clock is required")
	}
	if err := policy.Validate(); err != nil {
		return ApplyResult{}, err
	}
	if err := signal.Kind.Validate(); err != nil {
		return ApplyResult{}, err
	}
	if strings.TrimSpace(signal.ResourceID) == "" {
		return ApplyResult{}, errors.New("resource id is required")
	}
	prefix, err := NormalizeSourcePrefix(signal.Source, policy.IPv6PrefixBits)
	if err != nil {
		return ApplyResult{}, err
	}
	meta := signal.SafeMeta.Bounded(policy.SafeMetaMaxBytes)
	if err := meta.Validate(); err != nil {
		return ApplyResult{}, fmt.Errorf("safe meta: %w", err)
	}
	now := clock.Now()
	warnings := make([]string, 0, 1)
	if !signal.ObservedAt.IsZero() && signal.ObservedAt.After(now.Add(policy.ClockSkewTolerance)) {
		warnings = append(warnings, "clock_skew")
	}
	if state.ResourceID == "" {
		state.ResourceID = signal.ResourceID
		state.SourcePrefix = prefix
		state.FirstSeenAt = now
		state.LastDecision = domain.DecisionRecordOnly
		state.ClassifierPolicyVersion = meta.ClassifierPolicyVersion
	} else if state.ResourceID != signal.ResourceID || state.SourcePrefix != prefix {
		return ApplyResult{}, errors.New("signal does not match score state key")
	}
	decayed, decayWarnings := EffectiveScore(state, now, policy)
	state.CurrentScore = decayed
	warnings = append(warnings, decayWarnings...)
	if state.ExpiresAt != nil && !state.ExpiresAt.After(now) {
		state.ExpiresAt = nil
	}
	delta := signal.Delta
	if delta == 0 {
		delta = domain.DefaultSignalDelta(signal.Kind)
	}
	if delta < 0 {
		return ApplyResult{}, errors.New("signal delta must not be negative")
	}
	dedupeKey := buildDedupeKey(signal.ResourceID, prefix, signal.Kind, meta)
	duplicate := dedupeKey == state.LastDedupeKey && !state.LastDedupeAt.IsZero() &&
		!now.Before(state.LastDedupeAt) && now.Sub(state.LastDedupeAt) <= policy.DedupeWindow
	state.RawScore = clamp(state.RawScore+delta, policy.MaxScore)
	state.LastDedupeKey = dedupeKey
	state.LastDedupeAt = now
	state.LastSignalAt = now
	state.ClassifierPolicyVersion = meta.ClassifierPolicyVersion
	if duplicate {
		state.DedupedCount++
	} else {
		state.CurrentScore = clamp(state.CurrentScore+delta, policy.MaxScore)
		state.Reasons = updateReasons(state.Reasons, signal.Kind, safeReasonLabel(meta), now)
	}
	if state.CurrentScore >= policy.Threshold {
		expires := now.Add(policy.GraylistTTL)
		state.ExpiresAt = &expires
	}
	return ApplyResult{
		State:         state,
		EventAccepted: !duplicate,
		Duplicate:     duplicate,
		DedupeKey:     dedupeKey,
		Warnings:      uniqueStrings(warnings),
	}, nil
}

func buildDedupeKey(resourceID string, prefix netip.Prefix, kind domain.SignalKind, meta domain.SafeMeta) string {
	parts := []string{resourceID, prefix.String(), string(kind)}
	parts = append(parts, meta.DedupeParts()...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:16])
}

func updateReasons(reasons []ScoreReason, kind domain.SignalKind, label string, now time.Time) []ScoreReason {
	next := ScoreReason{Kind: kind, Count: 1, LastSeen: now, SafeLabel: label}
	out := make([]ScoreReason, 0, min(maxReasons, len(reasons)+1))
	for _, reason := range reasons {
		if reason.Kind == kind && reason.SafeLabel == label {
			next.Count = reason.Count + 1
			continue
		}
		out = append(out, reason)
	}
	out = append([]ScoreReason{next}, out...)
	if len(out) > maxReasons {
		out = out[:maxReasons]
	}
	return out
}

func safeReasonLabel(meta domain.SafeMeta) string {
	if meta.PathClass != "" {
		return meta.PathClass
	}
	if meta.UAClass != "" {
		return meta.UAClass
	}
	if meta.SNIClass != "" {
		return meta.SNIClass
	}
	return "classified"
}

func clamp(value, limit int) int {
	if value < 0 {
		return 0
	}
	if value > limit {
		return limit
	}
	return value
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok || value == "" {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
