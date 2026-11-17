package validation

import (
	"errors"
	"github.com/MalenkiySolovey/solovey-host/subscriptions/canonical"
	settingcatalog "github.com/MalenkiySolovey/solovey-ui/backend/settings/catalog"
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
			return errors.New("subscription format path cannot be root")
		}
		if _, exists := seen[path]; exists {
			return errors.New("subscription format paths must be unique")
		}
		seen[path] = struct{}{}
	}
	if paths.Base != "/" {
		for _, path := range formatPaths {
			if urlPathHasPrefix(path, paths.Base) {
				return errors.New("subscription format path conflicts with subscription path")
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
	case settingcatalog.SubClashUDPKey:
		_, err := canonical.ParseClashUDPPolicy(value)
		return err
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
