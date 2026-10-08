package jsonfields

import (
	"reflect"
	"strings"
)

// Names projects public JSON field names from pinned option structs.
// Semantic owners choose consumers and whether to expose deprecated fields.
func Names(t reflect.Type, includeDeprecated bool) []string {
	var result []string
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			result = append(result, Names(field.Type, includeDeprecated)...)
			continue
		}
		if !includeDeprecated && field.Tag.Get("schema") == "omit" {
			continue
		}
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name != "" && name != "-" {
			result = append(result, name)
		}
	}
	return result
}

// NamesOfType selects public fields of an actual pinned consumer type. It
// flattens embedded option structs and treats a pointer as the same value type.
// Validation policy remains with the semantic owner using these field facts.
func NamesOfType(t, wanted reflect.Type) []string {
	var result []string
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		fieldType := field.Type
		if fieldType.Kind() == reflect.Pointer {
			fieldType = fieldType.Elem()
		}
		if field.Anonymous && fieldType.Kind() == reflect.Struct {
			result = append(result, NamesOfType(fieldType, wanted)...)
			continue
		}
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if fieldType == wanted && field.IsExported() && name != "" && name != "-" {
			result = append(result, name)
		}
	}
	return result
}
