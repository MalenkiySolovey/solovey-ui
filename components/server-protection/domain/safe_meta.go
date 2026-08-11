package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	maxPathClassBytes = 128
	maxUAClassBytes   = 128
	maxMethodBytes    = 32
	maxStatusBytes    = 16
	maxALPNBytes      = 64
	maxSNIBytes       = 64
	maxBucketBytes    = 32
)

type SafeMeta struct {
	PathClass               string `json:"path_class,omitempty"`
	UAClass                 string `json:"ua_class,omitempty"`
	MethodClass             string `json:"method_class,omitempty"`
	StatusClass             string `json:"status_class,omitempty"`
	ALPNClass               string `json:"alpn_class,omitempty"`
	SNIClass                string `json:"sni_class,omitempty"`
	BytesClass              string `json:"bytes_class,omitempty"`
	DurationClass           string `json:"duration_class,omitempty"`
	Truncated               bool   `json:"truncated,omitempty"`
	ClassifierPolicyVersion int    `json:"classifier_policy_version"`
}

func (value SafeMeta) Validate() error {
	if value.ClassifierPolicyVersion <= 0 {
		return errors.New("classifier policy version must be positive")
	}
	for _, field := range []struct {
		name  string
		value string
		limit int
	}{
		{"path_class", value.PathClass, maxPathClassBytes},
		{"ua_class", value.UAClass, maxUAClassBytes},
		{"method_class", value.MethodClass, maxMethodBytes},
		{"status_class", value.StatusClass, maxStatusBytes},
		{"alpn_class", value.ALPNClass, maxALPNBytes},
		{"sni_class", value.SNIClass, maxSNIBytes},
		{"bytes_class", value.BytesClass, maxBucketBytes},
		{"duration_class", value.DurationClass, maxBucketBytes},
	} {
		if !utf8.ValidString(field.value) {
			return fmt.Errorf("%s must be valid UTF-8", field.name)
		}
		if len(field.value) > field.limit {
			return fmt.Errorf("%s exceeds %d bytes", field.name, field.limit)
		}
		if containsSensitiveDelimiter(field.value) {
			return fmt.Errorf("%s must be a classifier constant", field.name)
		}
	}
	return nil
}

func (value SafeMeta) Bounded(maxBytes int) SafeMeta {
	if maxBytes <= 0 {
		maxBytes = DefaultSafeMetaMaxBytes
	}
	if encoded, err := json.Marshal(value); err == nil && len(encoded) <= maxBytes {
		return value
	}
	bounded := SafeMeta{
		PathClass:               truncateClass(value.PathClass, maxPathClassBytes),
		UAClass:                 truncateClass(value.UAClass, maxUAClassBytes),
		Truncated:               true,
		ClassifierPolicyVersion: normalizedPolicyVersion(value.ClassifierPolicyVersion),
	}
	if encoded, err := json.Marshal(bounded); err == nil && len(encoded) <= maxBytes {
		return bounded
	}
	bounded.UAClass = ""
	if encoded, err := json.Marshal(bounded); err == nil && len(encoded) <= maxBytes {
		return bounded
	}
	bounded.PathClass = ""
	return bounded
}

func (value SafeMeta) DedupeParts() []string {
	return []string{value.PathClass, value.UAClass, value.MethodClass, value.StatusClass,
		value.ALPNClass, value.SNIClass, value.BytesClass, value.DurationClass,
		strconv.Itoa(normalizedPolicyVersion(value.ClassifierPolicyVersion))}
}

func containsSensitiveDelimiter(value string) bool {
	lower := strings.ToLower(value)
	return strings.ContainsAny(value, "?&#=/\\\r\n\t ") ||
		strings.Contains(lower, "bearer") || strings.Contains(lower, "token") ||
		strings.Contains(lower, "password") || strings.Contains(lower, "uuid")
}

func truncateClass(value string, limit int) string {
	if !utf8.ValidString(value) {
		return "invalid_utf8"
	}
	if len(value) <= limit {
		return value
	}
	return "overlong"
}

func normalizedPolicyVersion(value int) int {
	if value <= 0 {
		return ClassifierPolicyVersion
	}
	return value
}
