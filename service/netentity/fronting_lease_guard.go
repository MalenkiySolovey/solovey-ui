package netentity

import (
	"encoding/json"
	"errors"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entityinbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds"
	entitytls "github.com/MalenkiySolovey/solovey-ui/internal/entities/tls"

	"gorm.io/gorm"
)

var (
	ErrInboundEndpointLeased = errors.New("inbound_endpoint_lease_active")
	// ErrFrontingEndpointLeased is retained for callers that matched the Slice
	// 1 symbol. The durable guard now also covers local-proxy leases.
	ErrFrontingEndpointLeased = ErrInboundEndpointLeased
)

func guardInboundFrontingLease(tx *gorm.DB, action string, data json.RawMessage) error {
	if tx == nil || action == string(entityinbounds.ActionNew) {
		return nil
	}
	var inboundID uint
	switch action {
	case string(entityinbounds.ActionEdit):
		var identity struct {
			ID uint `json:"id"`
		}
		if err := json.Unmarshal(data, &identity); err != nil {
			return err
		}
		inboundID = identity.ID
	case string(entityinbounds.ActionDel):
		var tag string
		if err := json.Unmarshal(data, &tag); err != nil {
			return err
		}
		if err := tx.Model(&model.Inbound{}).Select("id").Where("tag = ?", tag).Scan(&inboundID).Error; err != nil {
			return err
		}
	default:
		return nil
	}
	return rejectLeasedInbounds(tx, "inbound_id = ?", inboundID)
}

func guardTLSFrontingLease(tx *gorm.DB, action string, data json.RawMessage) error {
	if tx == nil || action == string(entitytls.ActionNew) {
		return nil
	}
	var tlsID uint
	switch action {
	case string(entitytls.ActionEdit):
		var identity struct {
			ID uint `json:"id"`
		}
		if err := json.Unmarshal(data, &identity); err != nil {
			return err
		}
		tlsID = identity.ID
	case string(entitytls.ActionDel):
		if err := json.Unmarshal(data, &tlsID); err != nil {
			return err
		}
	default:
		return nil
	}
	return rejectLeasedInbounds(tx, "inbound_id IN (?)", tx.Model(&model.Inbound{}).Select("id").Where("tls_id = ?", tlsID))
}

func rejectLeasedInbounds(tx *gorm.DB, predicate string, value any) error {
	var count int64
	err := tx.Model(&model.InboundEndpointLease{}).
		Where(predicate, value).
		Where("state <> ?", "RELEASED").
		Count(&count).Error
	if err != nil {
		return err
	}
	if count != 0 {
		return ErrInboundEndpointLeased
	}
	return nil
}
