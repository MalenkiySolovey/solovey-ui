package model

import "encoding/json"

type Service struct {
	Id        uint   `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	SortOrder int    `json:"sortOrder" form:"sortOrder" gorm:"column:sort_order;default:0;not null;index"`
	Type      string `json:"type" form:"type"`
	Tag       string `json:"tag" form:"tag" gorm:"unique"`

	// Foreign key to tls table
	TlsId uint `json:"tls_id" form:"tls_id"`
	Tls   *Tls `json:"tls" form:"tls" gorm:"foreignKey:TlsId;references:Id"`

	Options json.RawMessage `json:"-" form:"-"`
}

func (i *Service) UnmarshalJSON(data []byte) error {
	var err error
	var raw map[string]interface{}
	if err = json.Unmarshal(data, &raw); err != nil {
		return err
	}

	// Extract fixed fields and store the rest in Options
	if val, exists := raw["id"].(float64); exists {
		i.Id = uint(val)
	}
	delete(raw, "id")
	if val, exists := raw["sortOrder"].(float64); exists {
		i.SortOrder = int(val)
	}
	if val, exists := raw["sort_order"].(float64); exists {
		i.SortOrder = int(val)
	}
	delete(raw, "sortOrder")
	delete(raw, "sort_order")
	i.Type, _ = raw["type"].(string)
	delete(raw, "type")
	i.Tag, _ = raw["tag"].(string)
	delete(raw, "tag")

	// TlsId
	if val, exists := raw["tls_id"].(float64); exists {
		i.TlsId = uint(val)
	}
	delete(raw, "tls_id")
	delete(raw, "tls")

	// Remaining fields
	i.Options, err = json.MarshalIndent(raw, "", "  ")
	return err
}

// MarshalJSON customizes marshalling
func (i Service) MarshalJSON() ([]byte, error) {
	// Combine fixed fields and dynamic fields into one map
	combined := make(map[string]interface{})
	combined["type"] = i.Type
	combined["tag"] = i.Tag
	if i.Tls != nil {
		combined["tls"] = i.Tls.Server
	}

	if i.Options != nil {
		var restFields map[string]json.RawMessage
		if err := json.Unmarshal(i.Options, &restFields); err != nil {
			return nil, err
		}

		for k, v := range restFields {
			combined[k] = v
		}
	}

	return json.Marshal(combined)
}

func (i Service) MarshalFull() (*map[string]interface{}, error) {
	combined := make(map[string]interface{})
	combined["id"] = i.Id
	combined["sortOrder"] = i.SortOrder
	combined["type"] = i.Type
	combined["tag"] = i.Tag
	combined["tls_id"] = i.TlsId

	if i.Options != nil {
		var restFields map[string]interface{}
		if err := json.Unmarshal(i.Options, &restFields); err != nil {
			return nil, err
		}

		for k, v := range restFields {
			combined[k] = v
		}
	}
	return &combined, nil
}
