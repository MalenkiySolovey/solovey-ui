package schema

import (
	"fmt"
	"sort"
	"strconv"
)

type FieldType string

const (
	FieldTypeString  FieldType = "string"
	FieldTypeInt     FieldType = "int"
	FieldTypeBool    FieldType = "bool"
	FieldTypeSecret  FieldType = "secret"
	FieldTypeURL     FieldType = "url"
	FieldTypePath    FieldType = "path"
	FieldTypeCron    FieldType = "cron"
	FieldTypeJSON    FieldType = "json"
	FieldTypeYAML    FieldType = "yaml"
	FieldTypeEnum    FieldType = "enum"
	FieldTypeText    FieldType = "text"
	FieldTypeTagList FieldType = "tag_list"
)

const (
	PageSettings = "settings"
	PageIPCert   = "ip_cert"
	PageInternal = "internal"
)

const (
	GroupInterface         = "interface"
	GroupSession           = "session"
	GroupRuntime           = "runtime"
	GroupSubscription      = "subscription"
	GroupSubscriptionJSON  = "subscription_json"
	GroupSubscriptionClash = "subscription_clash"
	GroupSubscriptionXray  = "subscription_xray"
	GroupIPCertPublic      = "ip_cert_public"
	GroupIPCertInternal    = "ip_cert_internal"
	GroupInternal          = "internal"
)

type Field struct {
	Key               string    `json:"key"`
	LabelKey          string    `json:"labelKey,omitempty"`
	Page              string    `json:"page,omitempty"`
	Group             string    `json:"group,omitempty"`
	Section           string    `json:"section,omitempty"`
	Type              FieldType `json:"type"`
	Default           string    `json:"default"`
	Editable          bool      `json:"editable"`
	Internal          bool      `json:"internal"`
	Encrypted         bool      `json:"encrypted"`
	Advanced          bool      `json:"advanced,omitempty"`
	RestartRequired   bool      `json:"restartRequired,omitempty"`
	SecretPresenceKey string    `json:"secretPresenceKey,omitempty"`
	Options           []string  `json:"options,omitempty"`
	Min               *int      `json:"min,omitempty"`
	Max               *int      `json:"max,omitempty"`
	Order             int       `json:"order,omitempty"`
}

func (s Schema) Field(key string) (Field, bool) {
	defaultValue, ok := s.Default(key)
	if !ok {
		return Field{}, false
	}
	field := s.fields[key]
	field.Key = key
	field.Default = defaultValue
	field.Editable = s.Editable(key)
	field.Internal = !field.Editable
	field.Encrypted = s.Encrypted(key)
	if field.Encrypted {
		field.SecretPresenceKey = SecretPresenceKey(key)
		if field.Type == "" || field.Type == FieldTypeString {
			field.Type = FieldTypeSecret
		}
	}
	if field.Type == "" {
		field.Type = inferFieldType(defaultValue)
	}
	if field.Page == "" {
		if field.Internal {
			field.Page = PageInternal
		} else {
			field.Page = PageSettings
		}
	}
	if field.Group == "" {
		if field.Internal {
			field.Group = GroupInternal
		} else {
			field.Group = GroupRuntime
		}
	}
	if field.LabelKey == "" {
		field.LabelKey = key
	}
	return field, true
}

func (s Schema) Fields() []Field {
	keys := s.Keys()
	fields := make([]Field, 0, len(keys))
	for _, key := range keys {
		field, ok := s.Field(key)
		if ok {
			fields = append(fields, field)
		}
	}
	sortFields(fields)
	return fields
}

func (s Schema) PublicFields() []Field {
	fields := s.Fields()
	public := make([]Field, 0, len(fields))
	for _, field := range fields {
		if !field.Internal {
			public = append(public, field)
		}
	}
	return public
}

// ValidateScalar is the authoritative backend enforcement for scalar field
// metadata. UI consumers receive the same Type/Min/Max/Options contract via
// Field, so bounds and enum membership cannot drift between representations.
func (s Schema) ValidateScalar(key string, value string) error {
	field, ok := s.Field(key)
	if !ok {
		return fmt.Errorf("unknown setting key: %s", key)
	}
	switch field.Type {
	case FieldTypeBool:
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("invalid boolean setting: %s", key)
		}
	case FieldTypeInt:
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid integer setting: %s", key)
		}
		if field.Min != nil && parsed < *field.Min {
			return fmt.Errorf("setting %s must be at least %d", key, *field.Min)
		}
		if field.Max != nil && parsed > *field.Max {
			return fmt.Errorf("setting %s must be at most %d", key, *field.Max)
		}
	case FieldTypeEnum:
		for _, option := range field.Options {
			if value == option {
				return nil
			}
		}
		return fmt.Errorf("invalid enum setting: %s", key)
	}
	return nil
}

func fieldsByKey(fields []Field) map[string]Field {
	byKey := make(map[string]Field, len(fields))
	for _, field := range fields {
		if field.Key == "" {
			panic("settings schema field key is empty")
		}
		if _, exists := byKey[field.Key]; exists {
			panic("duplicate settings schema field key: " + field.Key)
		}
		byKey[field.Key] = field
	}
	return byKey
}

func mergeFields(groups ...[]Field) []Field {
	var fields []Field
	for _, group := range groups {
		fields = append(fields, group...)
	}
	_ = fieldsByKey(fields)
	return fields
}

func intPtr(value int) *int {
	return &value
}

func inferFieldType(defaultValue string) FieldType {
	switch defaultValue {
	case "true", "false":
		return FieldTypeBool
	default:
		return FieldTypeString
	}
}

func sortFields(fields []Field) {
	sort.Slice(fields, func(i, j int) bool {
		if fields[i].Page != fields[j].Page {
			return fields[i].Page < fields[j].Page
		}
		if fields[i].Group != fields[j].Group {
			return fields[i].Group < fields[j].Group
		}
		if fields[i].Order != fields[j].Order {
			return fields[i].Order < fields[j].Order
		}
		return fields[i].Key < fields[j].Key
	})
}
