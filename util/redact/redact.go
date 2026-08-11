package redact

import (
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
	"unicode/utf8"
)

const Marker = "[REDACTED]"

// StringLimit applies sink redaction and then enforces a UTF-8-safe byte
// ceiling. Callers should choose the smallest limit appropriate for the sink.
func StringLimit(value string, maxBytes int) string {
	value = String(value)
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value
	}
	value = value[:maxBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

var sensitiveKeyFragments = []string{
	"authorization",
	"cookie",
	"credential",
	"csrf",
	"passphrase",
	"password",
	"private",
	"proxy",
	"recovery",
	"secret",
	"session_id",
	"session_token",
	"token",
	"access_key",
	"client_secret",
	"subscription",
}

var sensitiveExactKeys = []string{
	"otp",
	"totp",
	"mfa",
	"2fa",
}

var sensitiveValuePatterns = []struct {
	pattern     *regexp.Regexp
	replacement string
}{
	{
		pattern:     regexp.MustCompile(`\b\d{8,10}:[A-Za-z0-9_-]{35}\b`),
		replacement: Marker,
	},
	{
		// Strip inline URL credentials: scheme://user:pass@host -> scheme://[REDACTED]@host
		pattern:     regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://)[^/?#\s:@]+:[^/?#\s@]+@`),
		replacement: `${1}` + Marker + `@`,
	},
	{
		pattern:     regexp.MustCompile(`(?i)(\bAuthorization\s*:\s*Bearer\s+)[^\s,;]+`),
		replacement: `${1}` + Marker,
	},
	{
		pattern:     regexp.MustCompile(`(?i)(\bProxy-Authorization\s*:\s*(?:Basic|Bearer)\s+)[^\s,;]+`),
		replacement: `${1}` + Marker,
	},
	{
		// sing-box prefixes authenticated inbound activity with the principal in
		// brackets. Keep the connection diagnostic while removing the credential.
		pattern:     regexp.MustCompile(`\[[^\]\r\n]{1,256}\](\s+inbound\s+(?:connection|packet(?:\s+addr)?\s+connection)\b)`),
		replacement: Marker + `${1}`,
	},
	{
		pattern:     regexp.MustCompile(`(?i)(\b(?:Cookie|Set-Cookie)\s*:\s*)[^\r\n]+`),
		replacement: `${1}` + Marker,
	},
	{
		// Keep the key for diagnostics and remove the entire scalar value.
		pattern:     regexp.MustCompile(`(?i)(\b(?:password|passwd|passphrase|proxy_password|csrf(?:[_-]?token)?|recovery[_-]?code|private[_-]?key|client[_-]?secret|access[_-]?token|refresh[_-]?token|session[_-]?(?:id|token)|cookie|secret)\b["']?\s*[:=]\s*["']?)[^"'\s,;}\]]+(["']?)`),
		replacement: `${1}` + Marker + `${2}`,
	},
	{
		pattern:     regexp.MustCompile(`(?i)(\bToken\s*:\s*)[^\s,;]+`),
		replacement: `${1}` + Marker,
	},
	{
		pattern:     regexp.MustCompile(`(?i)(\b(?:totp|otp|mfa|2fa|secret|otp[_-]?secret|totp[_-]?secret|two[_-]?factor(?:[_-]?secret)?)\b["']?\s*[:=]\s*["']?)\b[A-Z2-7]{32}\b(["']?)`),
		replacement: `${1}` + Marker + `${2}`,
	},
	{
		pattern:     regexp.MustCompile(`(?is)-----BEGIN ([A-Z0-9 ]*PRIVATE KEY)-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`),
		replacement: "-----BEGIN ${1}-----" + Marker + "-----END ${1}-----",
	},
}

func Value(value any) any {
	switch v := value.(type) {
	case map[string]any:
		redacted := make(map[string]any, len(v))
		for key, item := range v {
			if IsSensitiveKey(key) {
				redacted[key] = Marker
				continue
			}
			redacted[key] = Value(item)
		}
		return redacted
	case map[string]string:
		redacted := make(map[string]string, len(v))
		for key, item := range v {
			if IsSensitiveKey(key) {
				redacted[key] = Marker
				continue
			}
			redacted[key] = String(item)
		}
		return redacted
	case []any:
		redacted := make([]any, len(v))
		for i, item := range v {
			redacted[i] = Value(item)
		}
		return redacted
	case []string:
		redacted := make([]string, len(v))
		for i, item := range v {
			redacted[i] = String(item)
		}
		return redacted
	case json.RawMessage:
		var decoded any
		if err := json.Unmarshal(v, &decoded); err == nil {
			return Value(decoded)
		}
		return String(string(v))
	case string:
		return String(v)
	default:
		if v == nil {
			return nil
		}
		if _, customJSON := v.(json.Marshaler); customJSON {
			encoded, err := json.Marshal(v)
			if err == nil {
				var decoded any
				if json.Unmarshal(encoded, &decoded) == nil {
					return Value(decoded)
				}
			}
		}
		if redacted, ok := reflectValue(reflect.ValueOf(v)); ok {
			return redacted.Interface()
		}
		return value
	}
}

var rawMessageType = reflect.TypeOf(json.RawMessage{})

func reflectValue(value reflect.Value) (reflect.Value, bool) {
	if !value.IsValid() {
		return value, true
	}
	if value.Type() == rawMessageType {
		var decoded any
		if json.Unmarshal(value.Bytes(), &decoded) != nil {
			return reflect.ValueOf(json.RawMessage(String(string(value.Bytes())))), true
		}
		encoded, err := json.Marshal(Value(decoded))
		if err != nil {
			return reflect.Value{}, false
		}
		return reflect.ValueOf(json.RawMessage(encoded)), true
	}
	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type()), true
		}
		item, ok := reflectValue(value.Elem())
		if !ok {
			return reflect.Value{}, false
		}
		result := reflect.New(value.Type()).Elem()
		result.Set(item)
		return result, true
	case reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type()), true
		}
		item, ok := reflectValue(value.Elem())
		if !ok {
			return reflect.Value{}, false
		}
		result := reflect.New(value.Type().Elem())
		result.Elem().Set(item)
		return result, true
	case reflect.Struct:
		result := reflect.New(value.Type()).Elem()
		for index := 0; index < value.NumField(); index++ {
			fieldType := value.Type().Field(index)
			target := result.Field(index)
			if !fieldType.IsExported() || !target.CanSet() {
				continue
			}
			jsonName := strings.Split(fieldType.Tag.Get("json"), ",")[0]
			if jsonName == "-" {
				continue
			}
			if jsonName == "" {
				jsonName = fieldType.Name
			}
			if IsSensitiveKey(jsonName) {
				target.Set(redactedMarkerValue(target.Type()))
				continue
			}
			item, ok := reflectValue(value.Field(index))
			if !ok {
				return reflect.Value{}, false
			}
			target.Set(item)
		}
		return result, true
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type()), true
		}
		result := reflect.MakeMapWithSize(value.Type(), value.Len())
		iterator := value.MapRange()
		for iterator.Next() {
			mapKey := iterator.Key()
			item := iterator.Value()
			if mapKey.Kind() == reflect.String && IsSensitiveKey(mapKey.String()) {
				result.SetMapIndex(mapKey, redactedMarkerValue(item.Type()))
				continue
			}
			redacted, ok := reflectValue(item)
			if !ok {
				return reflect.Value{}, false
			}
			result.SetMapIndex(mapKey, redacted)
		}
		return result, true
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type()), true
		}
		result := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for index := 0; index < value.Len(); index++ {
			item, ok := reflectValue(value.Index(index))
			if !ok {
				return reflect.Value{}, false
			}
			result.Index(index).Set(item)
		}
		return result, true
	case reflect.Array:
		result := reflect.New(value.Type()).Elem()
		for index := 0; index < value.Len(); index++ {
			item, ok := reflectValue(value.Index(index))
			if !ok {
				return reflect.Value{}, false
			}
			result.Index(index).Set(item)
		}
		return result, true
	case reflect.String:
		return reflect.ValueOf(String(value.String())).Convert(value.Type()), true
	default:
		return value, true
	}
}

func redactedMarkerValue(target reflect.Type) reflect.Value {
	marker := reflect.ValueOf(Marker)
	if marker.Type().AssignableTo(target) {
		return marker
	}
	if marker.Type().ConvertibleTo(target) {
		return marker.Convert(target)
	}
	if target.Kind() == reflect.Interface {
		return marker
	}
	if target.Kind() == reflect.Slice && target.Elem().Kind() == reflect.Uint8 {
		return reflect.ValueOf([]byte(Marker)).Convert(target)
	}
	return reflect.Zero(target)
}

func String(value string) string {
	for _, item := range sensitiveValuePatterns {
		value = item.pattern.ReplaceAllString(value, item.replacement)
	}
	return value
}

func IsSensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	for _, exact := range sensitiveExactKeys {
		if normalized == exact {
			return true
		}
	}
	for _, fragment := range sensitiveKeyFragments {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	return false
}
