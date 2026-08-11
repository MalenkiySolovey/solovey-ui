package resources

import (
	"net/netip"
	"path"
	"sort"
	"strings"
)

const (
	ConfiguredListenIntentSchemaV1 = "solovey-ui/configured-listen-intent/v1"
	ExpectedListenerOwnerSchemaV1  = "solovey-ui/expected-listener-owner/v1"
)

type ListenIntentMode string

const (
	ListenIntentExact     ListenIntentMode = "exact"
	ListenIntentWildcard  ListenIntentMode = "wildcard"
	ListenIntentDualStack ListenIntentMode = "dual_stack"
)

// ConfiguredListenIntentV1 is configuration intent only. It deliberately does
// not claim that a wildcard owns either address family. Additive preservation
// may conservatively keep both families; exact listener ownership is resolved
// separately from fresh kernel observations before topology mutation.
type ConfiguredListenIntentV1 struct {
	Schema                string           `json:"schema"`
	Mode                  ListenIntentMode `json:"mode"`
	Network               Network          `json:"network"`
	Address               string           `json:"address"`
	Port                  uint16           `json:"port"`
	RequiredFamilies      []AddressFamily  `json:"requiredFamilies,omitempty"`
	ConfigurationRevision string           `json:"configurationRevision"`
}

// ExpectedListenerOwnerV1 is the non-secret subset of the active deployment
// contract that a resource binds into its inventory revision.
type ExpectedListenerOwnerV1 struct {
	Schema                     string `json:"schema"`
	ContractRevision           string `json:"contractRevision"`
	InstanceID                 string `json:"instanceId"`
	SourceRevision             string `json:"sourceRevision"`
	ArtifactRevision           string `json:"artifactRevision"`
	DeploymentID               string `json:"deploymentId"`
	RuntimeRootBindingRevision string `json:"runtimeRootBindingRevision"`
	ServiceIdentity            string `json:"serviceIdentity"`
	SystemdUnit                string `json:"systemdUnit"`
	ServiceFragmentPath        string `json:"serviceFragmentPath"`
	ServiceUnitSHA256          string `json:"serviceUnitSha256"`
	ServiceControlGroup        string `json:"serviceControlGroup"`
	ExecutablePath             string `json:"executablePath"`
	ExecutableSHA256           string `json:"executableSha256"`
	ProcessUID                 uint32 `json:"processUid"`
	ProcessGID                 uint32 `json:"processGid"`
}

// DeterministicConfiguredEndpointKeys expands configuration intent into the
// exact network/family/port keys that an additive preserve-first firewall must
// keep reachable. It proves no socket or process ownership. A wildcard that
// may accept both families is conservatively represented by both families so
// management access never depends on a listener-owner observation.
func DeterministicConfiguredEndpointKeys(resource ProtectableResource) ([]PublicEndpointKey, bool) {
	_, intents, valid := normalizeConfiguredListenIntents(resource)
	if !valid {
		return nil, false
	}
	keys := make([]PublicEndpointKey, 0, len(intents)*2)
	seen := map[PublicEndpointKey]bool{}
	for _, intent := range intents {
		if !validHex64(intent.ConfigurationRevision) || intent.Port == 0 || (intent.Network != NetworkTCP && intent.Network != NetworkUDP) {
			return nil, false
		}
		add := func(family AddressFamily, bind string) {
			key := PublicEndpointKey{Network: intent.Network, AddressFamily: family, BindAddress: bind, Port: intent.Port}
			if !seen[key] {
				seen[key] = true
				keys = append(keys, key)
			}
		}
		switch intent.Mode {
		case ListenIntentExact:
			if len(intent.RequiredFamilies) != 1 {
				return nil, false
			}
			add(intent.RequiredFamilies[0], intent.Address)
		case ListenIntentWildcard, ListenIntentDualStack:
			normalized := NormalizeListen(intent.Address)
			switch normalized.Class {
			case ListenIPv4Wildcard:
				add(AddressFamilyIPv4, "0.0.0.0")
			case ListenIPv6Wildcard, ListenWildcard:
				add(AddressFamilyIPv4, "0.0.0.0")
				add(AddressFamilyIPv6, "::")
			default:
				return nil, false
			}
		default:
			return nil, false
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		return string(keys[i].Network)+"\x00"+string(keys[i].AddressFamily)+"\x00"+keys[i].BindAddress < string(keys[j].Network)+"\x00"+string(keys[j].AddressFamily)+"\x00"+keys[j].BindAddress
	})
	return keys, len(keys) > 0
}

// ConfiguredListenIntents returns the validated, canonical per-network
// configuration intents. Consumers must still obtain listener ownership from
// fresh observations; these values are configuration truth only.
func ConfiguredListenIntents(resource ProtectableResource) ([]ConfiguredListenIntentV1, bool) {
	_, intents, valid := normalizeConfiguredListenIntents(resource)
	if !valid {
		return nil, false
	}
	result := append([]ConfiguredListenIntentV1(nil), intents...)
	for index := range result {
		result[index].RequiredFamilies = append([]AddressFamily(nil), intents[index].RequiredFamilies...)
	}
	return result, true
}

func normalizeConfiguredListenIntents(resource ProtectableResource) (ConfiguredListenIntentV1, []ConfiguredListenIntentV1, bool) {
	primary, valid := normalizeConfiguredListenIntent(resource)
	if len(resource.ListenIntents) == 0 {
		return primary, []ConfiguredListenIntentV1{primary}, valid
	}
	if len(resource.ListenIntents) > 2 {
		return primary, nil, false
	}
	values := append([]ConfiguredListenIntentV1(nil), resource.ListenIntents...)
	for index := range values {
		values[index].RequiredFamilies = normalizedFamilies(values[index].RequiredFamilies)
		if !validAdditionalListenIntent(resource, values[index]) {
			return primary, nil, false
		}
	}
	sort.Slice(values, func(i, j int) bool { return values[i].Network < values[j].Network })
	for index := 1; index < len(values); index++ {
		if values[index-1].Network == values[index].Network {
			return primary, nil, false
		}
	}
	return values[0], values, true
}

func validAdditionalListenIntent(resource ProtectableResource, value ConfiguredListenIntentV1) bool {
	derived := BuildConfiguredListenIntent(resource)
	if value.Schema != ConfiguredListenIntentSchemaV1 || (value.Network != NetworkTCP && value.Network != NetworkUDP) || value.Port != derived.Port || value.ConfigurationRevision != derived.ConfigurationRevision || NormalizeListen(value.Address).Value != derived.Address {
		return false
	}
	switch value.Mode {
	case ListenIntentExact:
		address, err := netip.ParseAddr(value.Address)
		return err == nil && !address.IsUnspecified() && len(value.RequiredFamilies) == 1 && ((value.RequiredFamilies[0] == AddressFamilyIPv4) == address.Unmap().Is4())
	case ListenIntentWildcard:
		normalized := NormalizeListen(value.Address)
		return normalized.Wildcard() && (normalized.Class != ListenWildcard || len(value.RequiredFamilies) == 0) && (normalized.Class != ListenIPv4Wildcard || len(value.RequiredFamilies) == 1 && value.RequiredFamilies[0] == AddressFamilyIPv4) && (normalized.Class != ListenIPv6Wildcard || len(value.RequiredFamilies) == 1 && value.RequiredFamilies[0] == AddressFamilyIPv6)
	case ListenIntentDualStack:
		return value.Address == "*" && len(value.RequiredFamilies) == 2 && value.RequiredFamilies[0] == AddressFamilyIPv4 && value.RequiredFamilies[1] == AddressFamilyIPv6
	default:
		return false
	}
}

func BuildConfiguredListenIntent(resource ProtectableResource) ConfiguredListenIntentV1 {
	normalized := NormalizeListen(resource.Listen)
	intent := ConfiguredListenIntentV1{
		Schema: ConfiguredListenIntentSchemaV1, Network: NetworkForProtocol(resource.Protocol),
		Address: normalized.Value, ConfigurationRevision: resource.Capabilities.ConfigRevision,
	}
	if resource.Port > 0 && resource.Port <= 65535 {
		intent.Port = uint16(resource.Port)
	}
	switch normalized.Class {
	case ListenWildcard:
		intent.Mode = ListenIntentWildcard
	case ListenIPv4Wildcard:
		intent.Mode, intent.RequiredFamilies = ListenIntentWildcard, []AddressFamily{AddressFamilyIPv4}
	case ListenIPv6Wildcard:
		intent.Mode, intent.RequiredFamilies = ListenIntentWildcard, []AddressFamily{AddressFamilyIPv6}
	default:
		switch normalized.Family {
		case 4:
			intent.Mode, intent.RequiredFamilies = ListenIntentExact, []AddressFamily{AddressFamilyIPv4}
		case 6:
			intent.Mode, intent.RequiredFamilies = ListenIntentExact, []AddressFamily{AddressFamilyIPv6}
		default:
			intent.Mode = ListenIntentWildcard
		}
	}
	return intent
}

func normalizeConfiguredListenIntent(resource ProtectableResource) (ConfiguredListenIntentV1, bool) {
	derived := BuildConfiguredListenIntent(resource)
	value := resource.ListenIntent
	if value.Schema == "" {
		return derived, true
	}
	value.RequiredFamilies = normalizedFamilies(value.RequiredFamilies)
	if value.Schema != ConfiguredListenIntentSchemaV1 || value.Network != derived.Network || value.Port != derived.Port ||
		value.ConfigurationRevision != derived.ConfigurationRevision || NormalizeListen(value.Address).Value != derived.Address {
		return derived, false
	}
	switch value.Mode {
	case ListenIntentExact:
		address, err := netip.ParseAddr(value.Address)
		if err != nil || address.IsUnspecified() || len(value.RequiredFamilies) != 1 ||
			(value.RequiredFamilies[0] == AddressFamilyIPv4) != address.Unmap().Is4() {
			return derived, false
		}
	case ListenIntentWildcard:
		normalized := NormalizeListen(value.Address)
		if !normalized.Wildcard() || normalized.Class == ListenWildcard && len(value.RequiredFamilies) != 0 ||
			normalized.Class == ListenIPv4Wildcard && (len(value.RequiredFamilies) != 1 || value.RequiredFamilies[0] != AddressFamilyIPv4) ||
			normalized.Class == ListenIPv6Wildcard && (len(value.RequiredFamilies) != 1 || value.RequiredFamilies[0] != AddressFamilyIPv6) {
			return derived, false
		}
	case ListenIntentDualStack:
		if value.Address != "*" || len(value.RequiredFamilies) != 2 || value.RequiredFamilies[0] != AddressFamilyIPv4 || value.RequiredFamilies[1] != AddressFamilyIPv6 {
			return derived, false
		}
	default:
		return derived, false
	}
	return value, true
}

func normalizedFamilies(values []AddressFamily) []AddressFamily {
	seen := map[AddressFamily]bool{}
	result := make([]AddressFamily, 0, 2)
	for _, value := range values {
		if (value == AddressFamilyIPv4 || value == AddressFamilyIPv6) && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func (e ExpectedListenerOwnerV1) Valid() bool {
	return e.Schema == ExpectedListenerOwnerSchemaV1 && validHex64(e.ContractRevision) &&
		validHex64(e.RuntimeRootBindingRevision) && validHex64(e.ServiceUnitSHA256) && validHex64(e.ExecutableSHA256) &&
		strings.HasPrefix(e.SourceRevision, "src-") && len(e.SourceRevision) == 68 && validHex64(strings.TrimPrefix(e.SourceRevision, "src-")) &&
		strings.HasPrefix(e.ArtifactRevision, "art-") && len(e.ArtifactRevision) == 68 && validHex64(strings.TrimPrefix(e.ArtifactRevision, "art-")) &&
		strings.HasPrefix(e.DeploymentID, "dep-") && len(e.DeploymentID) == 68 && validHex64(strings.TrimPrefix(e.DeploymentID, "dep-")) &&
		safeEndpointToken(e.InstanceID) != "" && safeEndpointToken(e.ServiceIdentity) != "" && safeEndpointToken(e.SystemdUnit) != "" &&
		canonicalExpectedPath(e.ServiceFragmentPath) && canonicalExpectedPath(e.ServiceControlGroup) && canonicalExpectedPath(e.ExecutablePath)
}

func canonicalExpectedPath(value string) bool {
	return value != "" && value != "/" && len(value) <= 512 && strings.HasPrefix(value, "/") && path.Clean(value) == value && !strings.ContainsAny(value, "\x00\r\n\t")
}

func validHex64(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	for _, r := range value {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
