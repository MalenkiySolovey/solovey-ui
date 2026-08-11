package resources

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

type fingerprintInput struct {
	Kind             string                     `json:"kind"`
	Owner            string                     `json:"owner"`
	Protocol         string                     `json:"protocol"`
	Listen           string                     `json:"listen"`
	Port             int                        `json:"port"`
	TLSEnabled       bool                       `json:"tls_enabled"`
	TLSMode          string                     `json:"tls_mode,omitempty"`
	PublicHostnames  []string                   `json:"public_hostnames,omitempty"`
	FallbackTargetID string                     `json:"fallback_target,omitempty"`
	OwnerRevision    string                     `json:"owner_revision,omitempty"`
	ConfigRevision   string                     `json:"config_revision,omitempty"`
	ListenIntent     ConfiguredListenIntentV1   `json:"listen_intent"`
	ListenIntents    []ConfiguredListenIntentV1 `json:"listen_intents,omitempty"`
	ExpectedOwner    ExpectedListenerOwnerV1    `json:"expected_owner,omitempty"`
}

func Fingerprint(resource ProtectableResource) string {
	input := fingerprintInput{
		Kind:             normalizeToken(resource.Kind),
		Owner:            normalizeToken(resource.Owner),
		Protocol:         normalizeToken(resource.Protocol),
		Listen:           NormalizeListen(resource.Listen).Value,
		Port:             resource.Port,
		TLSEnabled:       resource.TLS,
		TLSMode:          normalizeToken(resource.Capabilities.TLSMode),
		PublicHostnames:  normalizedStrings(resource.Capabilities.PublicHostnames, true),
		FallbackTargetID: strings.TrimSpace(resource.Capabilities.FallbackTargetID),
		OwnerRevision:    strings.TrimSpace(resource.Capabilities.OwnerRevision),
		ConfigRevision:   strings.TrimSpace(resource.Capabilities.ConfigRevision),
		ListenIntent:     resource.ListenIntent,
		ListenIntents:    resource.ListenIntents,
		ExpectedOwner:    resource.Capabilities.ExpectedListenerOwner,
	}
	payload, _ := json.Marshal(input)
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func Revision(value any) string {
	payload, _ := json.Marshal(value)
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func normalizeToken(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizedStrings(values []string, hostnames bool) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if hostnames {
			value = strings.ToLower(strings.TrimSuffix(value, "."))
		}
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
