package canonical

import (
	"encoding/json"
	"math"
	"strconv"
)

var criticalTextFields = map[string][]string{
	"tls": {"server_name"}, "transport": {"type", "path", "service_name"},
	"multiplex": {"protocol"}, "obfs": {"type", "password"},
}

// These checks guard carriers that foreign parsers or target serializers can
// otherwise coerce to absent/default values. Protocol eligibility and required
// credentials remain with their existing protocol and destination owners.
func criticalShapeDiagnostics(raw map[string]any) []ProjectionDiagnostic {
	diagnostics := make([]ProjectionDiagnostic, 0, 5)
	invalid := func(field string) {
		diagnostics = append(diagnostics, ProjectionDiagnostic{State: ProjectionManual, Code: "critical_field_shape_unmapped", Field: field})
	}
	if value, present := raw["server"]; present {
		if _, ok := value.(string); !ok {
			invalid("endpoint")
		}
	}
	if value, present := raw["server_port"]; present && !nonnegativeInteger(value) {
		invalid("endpoint")
	}
	if value, present := raw["version"]; present && stringValue(raw["type"]) == "socks" {
		if _, ok := value.(string); !ok {
			invalid("protocol")
		}
	}
	for _, key := range authenticationFields {
		value, present := raw[key]
		if !present {
			continue
		}
		if key == "alter_id" {
			if !nonnegativeInteger(value) {
				invalid("authentication")
				break
			}
		} else if _, ok := value.(string); !ok {
			invalid("authentication")
			break
		}
	}
	for _, key := range []string{"tls", "transport", "multiplex", "obfs"} {
		value, present := raw[key]
		if !present || value == nil {
			continue
		}
		if key == "obfs" && stringValue(raw["type"]) == "hysteria" {
			if _, ok := value.(string); ok {
				continue
			}
		}
		object, ok := value.(map[string]any)
		if !ok {
			invalid(key)
			continue
		}
		valid := true
		for _, field := range criticalTextFields[key] {
			if value, present := object[field]; present {
				if _, ok := value.(string); !ok {
					valid = false
				}
			}
		}
		for _, flag := range []string{"enabled", "insecure", "disable_sni", "padding"} {
			if flagValue, present := object[flag]; present {
				if _, ok := flagValue.(bool); !ok {
					valid = false
				}
			}
		}
		if key == "tls" {
			for _, nested := range []string{"utls", "reality", "ech"} {
				if nestedValue, present := object[nested]; present && nestedValue != nil {
					nestedObject, ok := nestedValue.(map[string]any)
					if !ok {
						valid = false
						continue
					}
					if enabled, present := nestedObject["enabled"]; present {
						if _, ok := enabled.(bool); !ok {
							valid = false
						}
					}
					for _, field := range []string{"fingerprint", "public_key", "short_id"} {
						if value, present := nestedObject[field]; present {
							if _, ok := value.(string); !ok {
								valid = false
							}
						}
					}
					if value, present := nestedObject["config"]; present && value != nil && !stringListCarrier(value) {
						valid = false
					}
				}
			}
			for _, list := range []string{"alpn", "certificate", "certificate_public_key_sha256"} {
				if value, present := object[list]; present && !stringListCarrier(value) {
					valid = false
				}
			}
		}
		if key == "transport" {
			if object["type"] == "xhttp" {
				for _, field := range []string{"host", "mode"} {
					if value, present := object[field]; present {
						if _, ok := value.(string); !ok {
							valid = false
						}
					}
				}
			}
			if value, present := object["host"]; present && !stringListCarrier(value) {
				valid = false
			}
			if value, present := object["headers"]; present {
				headers, ok := value.(map[string]any)
				if !ok {
					valid = false
				} else {
					for _, value := range headers {
						if object["type"] == "xhttp" {
							if _, ok := value.(string); !ok {
								valid = false
							}
						}
						if !stringListCarrier(value) {
							valid = false
						}
					}
				}
			}
		}
		if !valid {
			invalid(key)
		}
	}
	return diagnostics
}

func stringListCarrier(value any) bool {
	switch values := value.(type) {
	case string:
		return true // Existing Listable string carrier.
	case []string:
		return true
	case []any:
		for _, value := range values {
			if _, ok := value.(string); !ok {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func nonnegativeInteger(value any) bool {
	switch number := value.(type) {
	case int:
		return number >= 0
	case int64:
		return number >= 0
	case uint, uint64:
		return true
	case float64:
		return number >= 0 && !math.IsInf(number, 0) && number == math.Trunc(number)
	case json.Number:
		integer, err := strconv.ParseUint(string(number), 10, 64)
		return err == nil && integer <= math.MaxInt64
	case string:
		_, err := strconv.ParseUint(number, 10, 64)
		return err == nil
	default:
		return false
	}
}
