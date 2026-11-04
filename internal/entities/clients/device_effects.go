package entityclients

import (
	"encoding/json"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	clientdevices "github.com/MalenkiySolovey/solovey-ui/internal/entities/clients/devices"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"gorm.io/gorm"
)

// Capture dependent endpoints before a User edit/delete. Device state cannot
// change through an ordinary edit, but enable, expiry, quota and deletion must
// remove its effective peers in the same config change.
func DeviceEndpointIDsForSave(tx *gorm.DB, action string, data json.RawMessage) ([]uint, error) {
	var ids []uint
	switch action {
	case "edit":
		var row struct {
			ID uint `json:"id"`
		}
		if json.Unmarshal(data, &row) != nil {
			return nil, clientdevices.ErrInvalid
		}
		ids = []uint{row.ID}
	case "editbulk":
		var rows []struct {
			ID uint `json:"id"`
		}
		if json.Unmarshal(data, &rows) != nil {
			return nil, clientdevices.ErrInvalid
		}
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
	case "del":
		var id uint
		if json.Unmarshal(data, &id) != nil {
			return nil, clientdevices.ErrInvalid
		}
		ids = []uint{id}
	case "delbulk":
		if json.Unmarshal(data, &ids) != nil {
			return nil, clientdevices.ErrInvalid
		}
	default:
		return nil, nil
	}
	var rows []model.Client
	if err := tx.Select("config").Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	var endpoints []uint
	for _, row := range rows {
		endpoints = common.UnionUintArray(endpoints, clientdevices.EndpointIDs(row.Config))
	}
	return endpoints, nil
}
