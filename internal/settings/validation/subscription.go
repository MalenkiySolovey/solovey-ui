package validation

import (
	"strconv"

	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
	subconversion "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/conversion"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

type SubscriptionPaths struct {
	Base  string
	JSON  string
	Clash string
	Xray  string
}

func ValidateSubscriptionPaths(paths SubscriptionPaths) error {
	formatPaths := []string{paths.JSON, paths.Clash, paths.Xray}
	seen := make(map[string]struct{}, len(formatPaths))
	for _, path := range formatPaths {
		if path == "/" {
			return common.NewError("subscription format path cannot be root")
		}
		if _, exists := seen[path]; exists {
			return common.NewError("subscription format paths must be unique")
		}
		seen[path] = struct{}{}
	}
	if paths.Base != "/" {
		for _, path := range formatPaths {
			if urlPathHasPrefix(path, paths.Base) {
				return common.NewError("subscription format path conflicts with subscription path")
			}
		}
	}
	return nil
}

func ValidateSubscriptionSettingInput(key string, value string) error {
	if _, ok := settingcatalog.SubscriptionBooleanKeys()[key]; ok {
		if _, err := strconv.ParseBool(value); err != nil {
			return common.NewError("invalid boolean setting: ", key)
		}
		return nil
	}
	if _, ok := settingcatalog.SubscriptionURLKeys()[key]; ok {
		return ValidateOptionalHTTPURL(value)
	}
	switch key {
	case settingcatalog.SubRateLimitPerIPKey:
		limit, err := strconv.Atoi(value)
		if err != nil || limit <= 0 || limit > 10000 {
			return common.NewError("invalid rate-limit setting: ", key)
		}
	case settingcatalog.SubJsonFragmentKey:
		if err := ValidateOptionalJSONObject(value, key); err != nil {
			return err
		}
	case settingcatalog.SubJsonNoisesKey:
		if err := ValidateOptionalJSONArray(value, key); err != nil {
			return err
		}
	case settingcatalog.SubRemoteGroupAdaptationKey:
		if !validSubscriptionGroupAdaptation(value) {
			return common.NewError("invalid subscription group adaptation setting: ", key)
		}
	case settingcatalog.SubRemoteConversionPolicyKey:
		if err := subconversion.ValidatePolicyJSON(value); err != nil {
			return common.NewError("invalid remote conversion policy setting: ", err)
		}
	}
	return nil
}

func validSubscriptionGroupAdaptation(value string) bool {
	switch value {
	case "urltest", "selector", "failover":
		return true
	default:
		return false
	}
}
