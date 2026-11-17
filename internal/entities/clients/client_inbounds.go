package entityclients

import (
	"encoding/json"
	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	"github.com/MalenkiySolovey/solovey-backend/users"
	"github.com/MalenkiySolovey/solovey-ui/logger"
	"gorm.io/gorm"
)

func InboundsByID(tx *gorm.DB, id uint) ([]uint, error)      { return users.InboundsByID(tx, id) }
func FillOmittedContents(tx *gorm.DB, c *model.Client) error { return users.FillOmittedContents(tx, c) }
func FindInboundChanges(tx *gorm.DB, c *model.Client) ([]uint, error) {
	return users.FindInboundChanges(tx, c)
}
func DecodeInbounds(id uint, raw json.RawMessage, op string) ([]uint, bool) {
	return users.DecodeInbounds(id, raw, op, logger.Warningf)
}
func AppendInbound(id uint, raw json.RawMessage, inbound uint, op string) (json.RawMessage, bool, error) {
	return users.AppendInbound(id, raw, inbound, op, logger.Warningf)
}
func RemoveInbound(id uint, raw json.RawMessage, inbound uint, op string) (json.RawMessage, bool, error) {
	return users.RemoveInbound(id, raw, inbound, op, logger.Warningf)
}
func IDsByInbound(tx *gorm.DB, id uint) ([]uint, error)      { return users.IDsByInbound(tx, id) }
func ByInbound(tx *gorm.DB, id uint) ([]model.Client, error) { return users.ByInbound(tx, id) }

type InboundNameRow = users.InboundNameRow

func NamesByInboundIDs(tx *gorm.DB, ids []uint) (map[uint][]string, error) {
	return users.NamesByInboundIDs(tx, ids)
}
