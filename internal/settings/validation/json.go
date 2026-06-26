package validation

import (
	"encoding/json"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

func ValidateOptionalJSONObject(value string, key string) error {
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

func ValidateOptionalJSONArray(value string, key string) error {
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
