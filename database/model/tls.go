package model

import "encoding/json"

type Tls struct {
	Id        uint            `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	SortOrder int             `json:"sortOrder" form:"sortOrder" gorm:"column:sort_order;default:0;not null;index"`
	Name      string          `json:"name" form:"name"`
	Server    json.RawMessage `json:"server" form:"server"`
	Client    json.RawMessage `json:"client" form:"client"`
}
