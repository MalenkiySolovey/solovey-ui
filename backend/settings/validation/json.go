package validation

import (
	"encoding/json"
	"fmt"
	"strings"
)

func ValidateOptionalJSONObject(value string, key string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(value), &obj); err != nil {
		return fmt.Errorf("invalid JSON setting:  %s", key)
	}
	if obj == nil {
		return fmt.Errorf("invalid JSON setting:  %s", key)
	}
	return nil
}

func ValidateOptionalJSONArray(value string, key string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	var arr []interface{}
	if err := json.Unmarshal([]byte(value), &arr); err != nil {
		return fmt.Errorf("invalid JSON array setting:  %s", key)
	}
	if arr == nil {
		return fmt.Errorf("invalid JSON array setting:  %s", key)
	}
	return nil
}
