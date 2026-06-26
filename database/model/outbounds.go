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
	var raw map[string]interface{}
	if err = json.Unmarshal(data, &raw); err != nil {
		return err
	}

	// Extract fixed fields and store the rest in Options
	if val, exists := raw["id"].(float64); exists {
		o.Id = uint(val)
	}
	delete(raw, "id")
	if val, exists := raw["sortOrder"].(float64); exists {
		o.SortOrder = int(val)
	}
	if val, exists := raw["sort_order"].(float64); exists {
		o.SortOrder = int(val)
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
	if val, exists := raw["remoteMissingSince"].(float64); exists {
		o.RemoteMissingSince = int64(val)
	}
	delete(raw, "remoteMissingSince")
	delete(raw, "remote_missing_reason")
	delete(raw, "remote_missing_source")
	delete(raw, "remote_missing_since")
	delete(raw, "remoteOutboundManaged")
	delete(raw, "remoteOutboundConnection")
	delete(raw, "remoteOutboundSubscription")
	delete(raw, "remoteOutboundGroups")

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
