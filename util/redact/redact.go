// Package redact preserves the public sink-redaction import path.
package redact

import canonical "github.com/MalenkiySolovey/solovey-host/redact"

const Marker = canonical.Marker

func StringLimit(value string, maxBytes int) string { return canonical.StringLimit(value, maxBytes) }
func Value(value any) any                           { return canonical.Value(value) }
func String(value string) string                    { return canonical.String(value) }
func IsSensitiveKey(key string) bool                { return canonical.IsSensitiveKey(key) }
