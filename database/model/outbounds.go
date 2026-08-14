package model

import "encoding/json"

type Outbound struct {
	Id                  uint            `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	SortOrder           int             `json:"sortOrder" form:"sortOrder" gorm:"column:sort_order;default:0;not null;index"`
	Type                string          `json:"type" form:"type"`
	Tag                 string          `json:"tag" form:"tag" gorm:"unique"`
	RemoteMissing       bool            `json:"remoteMissing" form:"remoteMissing" gorm:"column:remote_missing;default:false;not null"`
	RemoteMissingReason string          `json:"remoteMissingReason,omitempty" form:"remoteMissingReason" gorm:"column:remote_missing_reason"`
	RemoteMissingSince  int64           `json:"remoteMissingSince,omitempty" form:"remoteMissingSince" gorm:"column:remote_missing_since;default:0;not null"`
	RemoteMissingSource string          `json:"remoteMissingSource,omitempty" form:"remoteMissingSource" gorm:"column:remote_missing_source"`
	Options             json.RawMessage `json:"-" form:"-"`
}

func (o *Outbound) UnmarshalJSON(data []byte) error {
	var err error
	raw, err := decodeJSONObject(data)
	if err != nil {
		return err
	}

	// Extract fixed fields and store the rest in Options
	if err := optionalUint(raw, "id", &o.Id); err != nil {
		return err
	}
	delete(raw, "id")
	if err := optionalInt(raw, "sortOrder", &o.SortOrder); err != nil {
		return err
	}
	if err := optionalInt(raw, "sort_order", &o.SortOrder); err != nil {
		return err
	}
	delete(raw, "sortOrder")
	delete(raw, "sort_order")
	o.Type, _ = raw["type"].(string)
	delete(raw, "type")
	o.Tag, _ = raw["tag"].(string)
	delete(raw, "tag")
	if val, exists := raw["remoteMissing"].(bool); exists {
		o.RemoteMissing = val
	}
	delete(raw, "remoteMissing")
	if val, exists := raw["remote_missing"].(bool); exists {
		o.RemoteMissing = val
	}
	delete(raw, "remote_missing")
	o.RemoteMissingReason, _ = raw["remoteMissingReason"].(string)
	delete(raw, "remoteMissingReason")
	o.RemoteMissingSource, _ = raw["remoteMissingSource"].(string)
	delete(raw, "remoteMissingSource")
	if err := optionalInt64(raw, "remoteMissingSince", &o.RemoteMissingSince); err != nil {
		return err
	}
	delete(raw, "remoteMissingSince")
	delete(raw, "remote_missing_reason")
	delete(raw, "remote_missing_source")
	delete(raw, "remote_missing_since")
	// Remaining fields
	o.Options, err = json.MarshalIndent(raw, "", "  ")
	return err
}

// MarshalJSON customizes marshalling
func (o Outbound) MarshalJSON() ([]byte, error) {
	// Combine fixed fields and dynamic fields into one map
	combined := make(map[string]interface{})
	combined["type"] = o.Type
	combined["tag"] = o.Tag

	if o.Options != nil {
		var restFields map[string]json.RawMessage
		if err := json.Unmarshal(o.Options, &restFields); err != nil {
			return nil, err
		}

		for k, v := range restFields {
			combined[k] = v
		}
	}

	return json.Marshal(combined)
}
