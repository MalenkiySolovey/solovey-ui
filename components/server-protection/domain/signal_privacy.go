package domain

import (
	"errors"
	"strconv"
	"strings"
)

var privacySafeSignalMetaKeys = map[string]struct{}{
	"alpn_class":                {},
	"bytes_class":               {},
	"classifier_policy_version": {},
	"duration_class":            {},
	"method_class":              {},
	"path_class":                {},
	"rate_limited":              {},
	"sni_class":                 {},
	"status_class":              {},
	"truncated":                 {},
	"ua_class":                  {},
}

// ValidateProtectionSignalSafeMeta is the persistence-grade privacy boundary
// for signal metadata. Only pre-classified scalar facts are accepted.
func ValidateProtectionSignalSafeMeta(meta map[string]string) error {
	for key, value := range meta {
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.ToLower(strings.TrimSpace(value))
		if _, ok := privacySafeSignalMetaKeys[key]; !ok {
			return errors.New("signal metadata key is not privacy-safe")
		}
		switch key {
		case "classifier_policy_version":
			version, err := strconv.Atoi(value)
			if err != nil || version < 1 || version > 1_000_000 {
				return errors.New("signal classifier policy version is invalid")
			}
		case "rate_limited", "truncated":
			if value != "true" && value != "false" {
				return errors.New("signal boolean metadata is invalid")
			}
		default:
			if !ValidContractID(value, 64) {
				return errors.New("signal classified metadata is invalid")
			}
		}
	}
	return nil
}
