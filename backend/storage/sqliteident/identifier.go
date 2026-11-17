// Package sqliteident owns SQLite identifier validation and quoting.
package sqliteident

import "strings"

const maxIdentifierBytes = 96

// Valid accepts the bounded ASCII identifier shape used by dynamic backup and
// schema inventory queries. Quoting remains mandatory even for valid names.
func Valid(value string) bool {
	if value == "" || len(value) > maxIdentifierBytes {
		return false
	}
	for index, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char == '_' || index > 0 && char >= '0' && char <= '9' {
			continue
		}
		return false
	}
	return true
}

// Quote produces a SQLite delimited identifier and escapes embedded quotes.
// Callers accepting dynamic names must validate their domain before quoting.
func Quote(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
