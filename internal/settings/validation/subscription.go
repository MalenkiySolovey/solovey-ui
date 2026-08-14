package validation

import (
	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
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
	if _, ok := settingcatalog.SubscriptionURLKeys()[key]; ok {
		return ValidateOptionalHTTPURL(value)
	}
	switch key {
	case settingcatalog.SubJsonFragmentKey:
		if err := ValidateOptionalJSONObject(value, key); err != nil {
			return err
		}
	case settingcatalog.SubJsonNoisesKey:
		if err := ValidateOptionalJSONArray(value, key); err != nil {
			return err
		}
	}
	return nil
}
