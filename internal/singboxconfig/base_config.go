package singboxconfig

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

const DefaultBaseConfig = `{
  "log": {
    "level": "info"
  },
  "dns": {
    "servers": [],
    "rules": []
  },
  "route": {
    "rules": [
		  {
        "action": "sniff"
      },
      {
        "protocol": [
          "dns"
        ],
        "action": "hijack-dns"
      }
    ]
  },
  "experimental": {}
}`

type Document struct {
	raw      json.RawMessage
	sections map[string]json.RawMessage
}

func ParseBaseConfig(config json.RawMessage) (Document, error) {
	trimmed := bytes.TrimSpace(config)
	if len(trimmed) == 0 {
		return Document{}, fmt.Errorf("config must be a JSON object")
	}

	sections := map[string]json.RawMessage{}
	if err := json.Unmarshal(trimmed, &sections); err != nil {
		return Document{}, fmt.Errorf("config must be a JSON object: %w", err)
	}
	if sections == nil {
		return Document{}, fmt.Errorf("config must be a JSON object")
	}

	return Document{
		raw:      append(json.RawMessage(nil), trimmed...),
		sections: sections,
	}, nil
}

func NormalizeBaseConfig(config json.RawMessage) (string, error) {
	doc, err := ParseBaseConfig(config)
	if err != nil {
		return "", err
	}
	if err := doc.ValidateEditableSections(); err != nil {
		return "", err
	}
	return doc.MarshalIndented()
}

func (d Document) DNS() (json.RawMessage, bool) {
	return d.section("dns")
}

func (d Document) Route() (json.RawMessage, bool) {
	return d.section("route")
}

func (d Document) section(name string) (json.RawMessage, bool) {
	raw, ok := d.sections[name]
	if !ok {
		return nil, false
	}
	return append(json.RawMessage(nil), raw...), true
}

func (d Document) ValidateEditableSections() error {
	if err := d.validateObjectSectionFields("dns", []string{"servers", "rules"}); err != nil {
		return err
	}
	if err := d.validateObjectSectionFields("route", []string{"rules", "rule_set"}); err != nil {
		return err
	}
	if err := d.validateUniqueTaggedObjects("dns", "servers"); err != nil {
		return err
	}
	if err := d.validateUniqueTaggedObjects("route", "rule_set"); err != nil {
		return err
	}
	return nil
}

func (d Document) MarshalIndented() (string, error) {
	configs, err := json.MarshalIndent(d.raw, "", "  ")
	if err != nil {
		return "", err
	}
	return string(configs), nil
}

func (d Document) validateObjectSectionFields(section string, arrayFields []string) error {
	obj, ok, err := d.objectSection(section)
	if err != nil || !ok {
		return err
	}
	for _, field := range arrayFields {
		raw, ok := obj[field]
		if !ok {
			continue
		}
		if err := validateJSONRawArray(raw); err != nil {
			return fmt.Errorf("config.%s.%s must be a JSON array", section, field)
		}
	}
	return nil
}

func (d Document) validateUniqueTaggedObjects(section string, field string) error {
	obj, ok, err := d.objectSection(section)
	if err != nil || !ok {
		return err
	}
	raw, ok := obj[field]
	if !ok {
		return nil
	}

	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil {
		return fmt.Errorf("config.%s.%s must contain JSON objects", section, field)
	}

	seen := map[string]struct{}{}
	for i, row := range rows {
		rawTag, ok := row["tag"]
		if !ok {
			continue
		}
		var tag string
		if err := json.Unmarshal(rawTag, &tag); err != nil {
			return fmt.Errorf("config.%s.%s[%d].tag must be a string", section, field, i)
		}
		if strings.TrimSpace(tag) == "" {
			continue
		}
		if _, exists := seen[tag]; exists {
			return fmt.Errorf("config.%s.%s has duplicate tag %q", section, field, tag)
		}
		seen[tag] = struct{}{}
	}
	return nil
}

func (d Document) objectSection(section string) (map[string]json.RawMessage, bool, error) {
	raw, ok := d.sections[section]
	if !ok {
		return nil, false, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, false, fmt.Errorf("config.%s must be a JSON object", section)
	}
	if obj == nil {
		return nil, false, fmt.Errorf("config.%s must be a JSON object", section)
	}
	return obj, true, nil
}

func validateJSONRawArray(raw json.RawMessage) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return fmt.Errorf("not an array")
	}
	var rows []json.RawMessage
	return json.Unmarshal(trimmed, &rows)
}
