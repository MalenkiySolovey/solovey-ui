package model

import "encoding/json"

type Inbound struct {
	Id        uint   `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	SortOrder int    `json:"sortOrder" form:"sortOrder" gorm:"column:sort_order;default:0;not null;index"`
	Type      string `json:"type" form:"type"`
	Tag       string `json:"tag" form:"tag" gorm:"unique"`

	// Foreign key to tls table
	TlsId uint `json:"tls_id" form:"tls_id"`
	Tls   *Tls `json:"tls" form:"tls" gorm:"foreignKey:TlsId;references:Id"`

	Addrs   json.RawMessage `json:"addrs" form:"addrs"`
	OutJson json.RawMessage `json:"out_json" form:"out_json"`
	Options json.RawMessage `json:"-" form:"-"`
}

func (i *Inbound) UnmarshalJSON(data []byte) error {
	var err error
	raw, err := decodeJSONObject(data)
	if err != nil {
		return err
	}

	// Extract fixed fields and store the rest in Options
	if err := optionalUint(raw, "id", &i.Id); err != nil {
		return err
	}
	delete(raw, "id")
	if err := optionalInt(raw, "sortOrder", &i.SortOrder); err != nil {
		return err
	}
	if err := optionalInt(raw, "sort_order", &i.SortOrder); err != nil {
		return err
	}
	delete(raw, "sortOrder")
	delete(raw, "sort_order")
	i.Type, _ = raw["type"].(string)
	delete(raw, "type")
	i.Tag, _ = raw["tag"].(string)
	delete(raw, "tag")

	// TlsId
	if err := optionalUint(raw, "tls_id", &i.TlsId); err != nil {
		return err
	}
	delete(raw, "tls_id")
	delete(raw, "tls")
	delete(raw, "users")

	// Addrs
	i.Addrs, err = json.MarshalIndent(raw["addrs"], "", "  ")
	if err != nil {
		return err
	}
	delete(raw, "addrs")

	// OutJson
	i.OutJson, err = json.MarshalIndent(raw["out_json"], "", "  ")
	if err != nil {
		return err
	}
	delete(raw, "out_json")

	// Remaining fields
	i.Options, err = json.MarshalIndent(raw, "", "  ")
	return err
}

// MarshalJSON customizes marshalling
func (i Inbound) MarshalJSON() ([]byte, error) {
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

func (i Inbound) MarshalFull() (*map[string]interface{}, error) {
	combined := make(map[string]interface{})
	combined["id"] = i.Id
	combined["sortOrder"] = i.SortOrder
	combined["type"] = i.Type
	combined["tag"] = i.Tag
	combined["tls_id"] = i.TlsId
	combined["addrs"] = i.Addrs
	combined["out_json"] = i.OutJson

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
