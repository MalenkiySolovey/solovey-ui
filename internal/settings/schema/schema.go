package schema

import (
	"strings"

	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
)

const SecretPresenceSuffix = "HasSecret"

type Schema struct {
	catalog   settingcatalog.Catalog
	encrypted map[string]struct{}
	fields    map[string]Field
}

func New(defaults map[string]string, internal map[string]struct{}, encrypted map[string]struct{}, fields ...Field) Schema {
	return Schema{
		catalog:   settingcatalog.New(defaults, internal),
		encrypted: copyKeySet(encrypted),
		fields:    fieldsByKey(fields),
	}
}

func (s Schema) Keys() []string {
	return s.catalog.Keys()
}

func (s Schema) Defaults() map[string]string {
	defaults := make(map[string]string)
	for _, key := range s.catalog.Keys() {
		value, _ := s.catalog.Default(key)
		defaults[key] = value
	}
	return defaults
}

func (s Schema) Default(key string) (string, bool) {
	return s.catalog.Default(key)
}

func (s Schema) HideInternal(settings map[string]string) {
	s.catalog.HideInternal(settings)
}

func (s Schema) Editable(key string) bool {
	return s.catalog.Editable(key)
}

func (s Schema) Encrypted(key string) bool {
	_, ok := s.encrypted[key]
	return ok
}

func (s Schema) AcceptsSecretPresenceMarker(key string) bool {
	if !IsSecretPresenceMarker(key) {
		return false
	}
	return s.Encrypted(BaseSecretKey(key))
}

func IsSecretPresenceMarker(key string) bool {
	return strings.HasSuffix(key, SecretPresenceSuffix)
}

func BaseSecretKey(key string) string {
	return strings.TrimSuffix(key, SecretPresenceSuffix)
}

func SecretPresenceKey(key string) string {
	return key + SecretPresenceSuffix
}

func copyKeySet(values map[string]struct{}) map[string]struct{} {
	copied := make(map[string]struct{}, len(values))
	for key := range values {
		copied[key] = struct{}{}
	}
	return copied
}
