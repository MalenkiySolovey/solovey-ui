package service

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

func (s *SettingService) validateSubscriptionPathSettings(settings map[string]string) error {
	touched := false
	for _, key := range subscriptionPathSettingKeys {
		if _, ok := settings[key]; ok {
			touched = true
			break
		}
	}
	if !touched {
		return nil
	}

	paths := make(map[string]string, len(subscriptionPathSettingKeys))
	for _, key := range subscriptionPathSettingKeys {
		value, ok := settings[key]
		if !ok {
			var err error
			value, err = s.getString(key)
			if err != nil {
				return err
			}
		}
		normalized, err := normalizeAndValidatePathSetting(key, value)
		if err != nil {
			return err
		}
		paths[key] = normalized
	}

	if paths[settingKeySubJsonPath] == paths[settingKeySubClashPath] {
		return common.NewError("subscription format paths must be unique")
	}
	if paths[settingKeySubJsonPath] == "/" || paths[settingKeySubClashPath] == "/" {
		return common.NewError("subscription format path cannot be root")
	}
	if paths[settingKeySubPath] != "/" {
		if pathHasPrefix(paths[settingKeySubJsonPath], paths[settingKeySubPath]) || pathHasPrefix(paths[settingKeySubClashPath], paths[settingKeySubPath]) {
			return common.NewError("subscription format path conflicts with subscription path")
		}
	}
	return nil
}

func validateSubscriptionSettingInput(key string, value string) error {
	if _, ok := subscriptionBooleanSettingKeys[key]; ok {
		if _, err := strconv.ParseBool(value); err != nil {
			return common.NewError("invalid boolean setting: ", key)
		}
		return nil
	}
	if _, ok := subscriptionURLSettingKeys[key]; ok {
		return validateOptionalHTTPURL(value)
	}
	switch key {
	case settingKeySubRateLimitPerIP:
		limit, err := strconv.Atoi(value)
		if err != nil || limit <= 0 || limit > 10000 {
			return common.NewError("invalid rate-limit setting: ", key)
		}
	case settingKeySubJsonFragment:
		if err := validateOptionalJSONObject(value, key); err != nil {
			return err
		}
	case settingKeySubJsonNoises:
		if err := validateOptionalJSONArray(value, key); err != nil {
			return err
		}
	}
	return nil
}

func validateOptionalJSONObject(value string, key string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(value), &obj); err != nil {
		return common.NewError("invalid JSON setting: ", key)
	}
	if obj == nil {
		return common.NewError("invalid JSON setting: ", key)
	}
	return nil
}

func validateOptionalJSONArray(value string, key string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	var arr []interface{}
	if err := json.Unmarshal([]byte(value), &arr); err != nil {
		return common.NewError("invalid JSON array setting: ", key)
	}
	if arr == nil {
		return common.NewError("invalid JSON array setting: ", key)
	}
	return nil
}
