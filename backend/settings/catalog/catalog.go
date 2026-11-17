package catalog

import "sort"

type Catalog struct {
	defaults map[string]string
	internal map[string]struct{}
}

func New(defaults map[string]string, internal map[string]struct{}) Catalog {
	return Catalog{
		defaults: copyStringMap(defaults),
		internal: copyKeySet(internal),
	}
}

func MergeDefaultMaps(groups ...map[string]string) map[string]string {
	merged := make(map[string]string)
	for _, group := range groups {
		for key, value := range group {
			if _, exists := merged[key]; exists {
				panic("duplicate default setting key: " + key)
			}
			merged[key] = value
		}
	}
	return merged
}

func KeySet(keys ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		set[key] = struct{}{}
	}
	return set
}

func MergeKeySets(groups ...map[string]struct{}) map[string]struct{} {
	merged := make(map[string]struct{})
	for _, group := range groups {
		for key := range group {
			merged[key] = struct{}{}
		}
	}
	return merged
}

func (c Catalog) Keys() []string {
	keys := make([]string, 0, len(c.defaults))
	for key := range c.defaults {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (c Catalog) Default(key string) (string, bool) {
	value, ok := c.defaults[key]
	return value, ok
}

func (c Catalog) HideInternal(settings map[string]string) {
	for key := range c.internal {
		delete(settings, key)
	}
}

func (c Catalog) Editable(key string) bool {
	if _, ok := c.defaults[key]; !ok {
		return false
	}
	_, internal := c.internal[key]
	return !internal
}

func SortedKeys(settings map[string]string) []string {
	keys := make([]string, 0, len(settings))
	for key := range settings {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func copyStringMap(values map[string]string) map[string]string {
	copied := make(map[string]string, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}

func copyKeySet(values map[string]struct{}) map[string]struct{} {
	copied := make(map[string]struct{}, len(values))
	for key := range values {
		copied[key] = struct{}{}
	}
	return copied
}
