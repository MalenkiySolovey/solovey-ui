package domain

import (
	"errors"
	"fmt"
)

const (
	DefaultRetentionGlobalLimit       = 1000
	DefaultRetentionPerResourceLimit  = 200
	DefaultDiagnosticsCacheTTLSeconds = 10
	DefaultObservationBufferSize      = 1024
	DefaultObservationFlushIntervalMS = 1000
	DefaultClockSkewToleranceSeconds  = 300
	DefaultArtifactRetentionCount     = 10
	DefaultArtifactRetentionDays      = 30
)

var KnownFeatureFlags = map[string]struct{}{
	"enable_apply_beta":            {},
	"enable_fronting_beta":         {},
	"enable_node_beta":             {},
	"enable_hard_block":            {},
	"enable_external_integrations": {},
	"enable_desync_links":          {},
}

type Settings struct {
	Enabled                    bool            `json:"enabled"`
	RetentionGlobalLimit       int             `json:"retentionGlobalLimit"`
	RetentionPerResourceLimit  int             `json:"retentionPerResourceLimit"`
	DefaultScoreThreshold      int             `json:"defaultScoreThreshold"`
	DefaultGraylistTTLSeconds  int             `json:"defaultGraylistTtlSeconds"`
	DiagnosticsCacheTTLSeconds int             `json:"diagnosticsCacheTtlSeconds"`
	ObservationBufferSize      int             `json:"observationBufferSize"`
	ObservationFlushIntervalMS int             `json:"observationFlushIntervalMs"`
	IPv6GraylistPrefixBits     int             `json:"ipv6GraylistPrefixBits"`
	MaxScore                   int             `json:"maxScore"`
	SafeMetaMaxBytes           int             `json:"safeMetaMaxBytes"`
	ClockSkewToleranceSeconds  int             `json:"clockSkewToleranceSeconds"`
	ArtifactRetentionCount     int             `json:"artifactRetentionCount"`
	ArtifactRetentionDays      int             `json:"artifactRetentionDays"`
	AdvancedAcknowledgedAt     int64           `json:"advancedAcknowledgedAt,omitempty"`
	FeatureFlags               map[string]bool `json:"featureFlags"`
}

func DefaultSettings() Settings {
	return Settings{
		Enabled:                    true,
		RetentionGlobalLimit:       DefaultRetentionGlobalLimit,
		RetentionPerResourceLimit:  DefaultRetentionPerResourceLimit,
		DefaultScoreThreshold:      DefaultScoreThreshold,
		DefaultGraylistTTLSeconds:  DefaultGraylistTTLSeconds,
		DiagnosticsCacheTTLSeconds: DefaultDiagnosticsCacheTTLSeconds,
		ObservationBufferSize:      DefaultObservationBufferSize,
		ObservationFlushIntervalMS: DefaultObservationFlushIntervalMS,
		IPv6GraylistPrefixBits:     DefaultIPv6PrefixBits,
		MaxScore:                   DefaultMaxScore,
		SafeMetaMaxBytes:           DefaultSafeMetaMaxBytes,
		ClockSkewToleranceSeconds:  DefaultClockSkewToleranceSeconds,
		ArtifactRetentionCount:     DefaultArtifactRetentionCount,
		ArtifactRetentionDays:      DefaultArtifactRetentionDays,
		FeatureFlags:               map[string]bool{},
	}
}

func (value Settings) Validate() error {
	for _, field := range []struct {
		name       string
		value, min int
		max        int
	}{
		{"retention global limit", value.RetentionGlobalLimit, 100, 100000},
		{"retention per-resource limit", value.RetentionPerResourceLimit, 20, 10000},
		{"default score threshold", value.DefaultScoreThreshold, 1, 100},
		{"default graylist TTL", value.DefaultGraylistTTLSeconds, 60, 604800},
		{"diagnostics cache TTL", value.DiagnosticsCacheTTLSeconds, 1, 300},
		{"observation buffer size", value.ObservationBufferSize, 0, 65536},
		{"observation flush interval", value.ObservationFlushIntervalMS, 100, 10000},
		{"IPv6 graylist prefix bits", value.IPv6GraylistPrefixBits, 32, 128},
		{"max score", value.MaxScore, 5, 10000},
		{"safe meta max bytes", value.SafeMetaMaxBytes, 128, 4096},
		{"clock skew tolerance", value.ClockSkewToleranceSeconds, 30, 3600},
		{"artifact retention count", value.ArtifactRetentionCount, 1, 100},
		{"artifact retention days", value.ArtifactRetentionDays, 1, 365},
	} {
		if field.value < field.min || field.value > field.max {
			return fmt.Errorf("%s must be between %d and %d", field.name, field.min, field.max)
		}
	}
	if value.RetentionPerResourceLimit > value.RetentionGlobalLimit {
		return errors.New("per-resource retention must not exceed global retention")
	}
	if value.DefaultScoreThreshold > value.MaxScore {
		return errors.New("default score threshold must not exceed max score")
	}
	for key, enabled := range value.FeatureFlags {
		if _, ok := KnownFeatureFlags[key]; !ok {
			return fmt.Errorf("unknown feature flag %q", key)
		}
		if enabled && value.AdvancedAcknowledgedAt == 0 {
			return fmt.Errorf("feature flag %q requires advanced acknowledgement", key)
		}
	}
	return nil
}
