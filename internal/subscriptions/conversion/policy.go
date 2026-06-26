package conversion

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	ModeOriginal = "original"

	ModeURLTest  = "urltest"
	ModeSelector = "selector"
	ModeFailover = "failover"

	ModeXrayBalancer = "balancer"

	ModeMihomoSelect      = "select"
	ModeMihomoURLTest     = "url-test"
	ModeMihomoFallback    = "fallback"
	ModeMihomoLoadBalance = "load-balance"
)

const (
	TargetOutbound = "outbound"
	TargetSingBox  = "sing-box"
	TargetXray     = "xray"
	TargetMihomo   = "mihomo"
)

const (
	FeatureXrayBalancer      = "xrayBalancer"
	FeatureMihomoFallback    = "mihomoFallback"
	FeatureMihomoLoadBalance = "mihomoLoadBalance"
	FeatureMihomoSmart       = "mihomoSmart"
	FeatureMihomoRelay       = "mihomoRelay"
	FeatureMihomoSSID        = "mihomoSsid"
)

type RuleSet map[string]string

type ClientPolicy struct {
	SingBox RuleSet `json:"singBox"`
	Xray    RuleSet `json:"xray"`
	Mihomo  RuleSet `json:"mihomo"`
}

type Policy struct {
	Outbound RuleSet      `json:"outbound"`
	Client   ClientPolicy `json:"client"`
}

func DefaultPolicy() Policy {
	return Policy{
		Outbound: RuleSet{
			FeatureXrayBalancer:      ModeURLTest,
			FeatureMihomoFallback:    ModeURLTest,
			FeatureMihomoLoadBalance: ModeURLTest,
			FeatureMihomoSmart:       ModeURLTest,
			FeatureMihomoRelay:       ModeSelector,
			FeatureMihomoSSID:        ModeSelector,
		},
		Client: ClientPolicy{
			SingBox: RuleSet{
				FeatureXrayBalancer:      ModeURLTest,
				FeatureMihomoFallback:    ModeURLTest,
				FeatureMihomoLoadBalance: ModeURLTest,
				FeatureMihomoSmart:       ModeURLTest,
				FeatureMihomoRelay:       ModeSelector,
				FeatureMihomoSSID:        ModeSelector,
			},
			Xray: RuleSet{
				FeatureXrayBalancer:      ModeOriginal,
				FeatureMihomoFallback:    ModeXrayBalancer,
				FeatureMihomoLoadBalance: ModeXrayBalancer,
				FeatureMihomoSmart:       ModeXrayBalancer,
				FeatureMihomoRelay:       ModeXrayBalancer,
				FeatureMihomoSSID:        ModeXrayBalancer,
			},
			Mihomo: RuleSet{
				FeatureXrayBalancer:      ModeMihomoURLTest,
				FeatureMihomoFallback:    ModeOriginal,
				FeatureMihomoLoadBalance: ModeOriginal,
				FeatureMihomoSmart:       ModeOriginal,
				FeatureMihomoRelay:       ModeOriginal,
				FeatureMihomoSSID:        ModeOriginal,
			},
		},
	}
}

func DefaultPolicyJSON() string {
	data, _ := json.Marshal(DefaultPolicy())
	return string(data)
}

func ParsePolicy(raw string, legacyGroupAdaptation string) Policy {
	policy := DefaultPolicy()
	legacy := NormalizeRuntimeMode(legacyGroupAdaptation)
	if legacy != "" {
		for feature := range policy.Outbound {
			if feature == FeatureMihomoRelay || feature == FeatureMihomoSSID {
				continue
			}
			policy.Outbound[feature] = legacy
			policy.Client.SingBox[feature] = legacy
		}
	}
	if strings.TrimSpace(raw) == "" {
		return policy
	}
	var parsed Policy
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return policy
	}
	mergeRules(policy.Outbound, parsed.Outbound, TargetOutbound)
	mergeRules(policy.Client.SingBox, parsed.Client.SingBox, TargetSingBox)
	mergeRules(policy.Client.Xray, parsed.Client.Xray, TargetXray)
	mergeRules(policy.Client.Mihomo, parsed.Client.Mihomo, TargetMihomo)
	return policy
}

func ValidatePolicyJSON(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var parsed Policy
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return err
	}
	if err := validateRuleSet("outbound", parsed.Outbound, TargetOutbound); err != nil {
		return err
	}
	if err := validateRuleSet("client.singBox", parsed.Client.SingBox, TargetSingBox); err != nil {
		return err
	}
	if err := validateRuleSet("client.xray", parsed.Client.Xray, TargetXray); err != nil {
		return err
	}
	return validateRuleSet("client.mihomo", parsed.Client.Mihomo, TargetMihomo)
}

func (p Policy) Mode(target string, feature string) string {
	switch strings.TrimSpace(target) {
	case TargetOutbound:
		return runtimeMode(p.Outbound[feature], p.defaultOutbound(feature))
	case TargetSingBox:
		return runtimeMode(p.Client.SingBox[feature], p.defaultClient(TargetSingBox, feature))
	case TargetXray:
		return targetMode(TargetXray, feature, p.Client.Xray[feature], p.defaultClient(TargetXray, feature))
	case TargetMihomo:
		return targetMode(TargetMihomo, feature, p.Client.Mihomo[feature], p.defaultClient(TargetMihomo, feature))
	default:
		return runtimeMode(p.Outbound[feature], p.defaultOutbound(feature))
	}
}

func NormalizeRuntimeMode(value string) string {
	return runtimeMode(value, "")
}

func FeatureForXrayBalancer() string {
	return FeatureXrayBalancer
}

func FeatureForMihomoGroup(sourceType string) string {
	switch strings.ToLower(strings.TrimSpace(sourceType)) {
	case "fallback":
		return FeatureMihomoFallback
	case "load-balance":
		return FeatureMihomoLoadBalance
	case "smart":
		return FeatureMihomoSmart
	case "relay":
		return FeatureMihomoRelay
	case "ssid":
		return FeatureMihomoSSID
	default:
		return ""
	}
}

func mergeRules(dst RuleSet, src RuleSet, target string) {
	if dst == nil {
		return
	}
	for key, value := range src {
		if _, ok := dst[key]; !ok {
			continue
		}
		if normalized := targetMode(target, key, value, ""); normalized != "" {
			dst[key] = normalized
		}
	}
}

func validateRuleSet(name string, rules RuleSet, target string) error {
	defaults := DefaultPolicy().Outbound
	for feature, mode := range rules {
		if _, ok := defaults[feature]; !ok {
			return fmt.Errorf("unknown conversion feature %s.%s", name, feature)
		}
		if normalized := targetMode(target, feature, mode, ""); normalized != "" {
			continue
		}
		return fmt.Errorf("invalid conversion mode %s.%s=%s", name, feature, mode)
	}
	return nil
}

func (p Policy) defaultOutbound(feature string) string {
	if value := DefaultPolicy().Outbound[feature]; value != "" {
		return value
	}
	return ModeURLTest
}

func (p Policy) defaultClient(target string, feature string) string {
	defaults := DefaultPolicy()
	switch target {
	case TargetXray:
		if value := defaults.Client.Xray[feature]; value != "" {
			return value
		}
	case TargetMihomo:
		if value := defaults.Client.Mihomo[feature]; value != "" {
			return value
		}
	default:
		if value := defaults.Client.SingBox[feature]; value != "" {
			return value
		}
	}
	return ModeURLTest
}

func runtimeMode(value string, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case ModeURLTest:
		return ModeURLTest
	case ModeSelector:
		return ModeSelector
	case ModeFailover:
		return ModeFailover
	default:
		return fallback
	}
}

func targetMode(target string, feature string, value string, fallback string) string {
	target = strings.TrimSpace(target)
	switch strings.ToLower(strings.TrimSpace(value)) {
	case ModeOriginal:
		if nativeTarget(feature) == target {
			return ModeOriginal
		}
		return fallback
	}
	switch target {
	case TargetXray:
		return xrayMode(value, fallback)
	case TargetMihomo:
		return mihomoMode(value, fallback)
	default:
		return runtimeMode(value, fallback)
	}
}

func xrayMode(value string, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case ModeXrayBalancer, ModeURLTest, ModeSelector, ModeFailover:
		return ModeXrayBalancer
	default:
		return fallback
	}
}

func mihomoMode(value string, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case ModeMihomoSelect:
		return ModeMihomoSelect
	case ModeMihomoURLTest:
		return ModeMihomoURLTest
	case ModeMihomoFallback:
		return ModeMihomoFallback
	case ModeMihomoLoadBalance:
		return ModeMihomoLoadBalance
	case ModeSelector:
		return ModeMihomoSelect
	case ModeURLTest:
		return ModeMihomoURLTest
	case ModeFailover:
		return ModeMihomoFallback
	default:
		return fallback
	}
}

func nativeTarget(feature string) string {
	switch feature {
	case FeatureXrayBalancer:
		return TargetXray
	case FeatureMihomoFallback, FeatureMihomoLoadBalance, FeatureMihomoSmart, FeatureMihomoRelay, FeatureMihomoSSID:
		return TargetMihomo
	default:
		return ""
	}
}
