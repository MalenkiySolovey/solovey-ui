package order

import (
	"encoding/json"
	owner "github.com/MalenkiySolovey/solovey-backend/storage/entityorder"
	"gorm.io/gorm"
)

const Clause = owner.Clause

type DBTarget = owner.DBTarget

func ForSave(tx *gorm.DB, value any, id uint) (int, error) { return owner.ForSave(tx, value, id) }
func Next(tx *gorm.DB, value any) (int, error)             { return owner.Next(tx, value) }
func ReorderDBTarget(tx *gorm.DB, target DBTarget, data json.RawMessage) error {
	return owner.ReorderDBTarget(tx, target, data)
}
func ParseIDs(data json.RawMessage) ([]uint, error)     { return owner.ParseIDs(data) }
func ValidateIDs(current, requested []uint) error       { return owner.ValidateIDs(current, requested) }
func ApplyIDs(tx *gorm.DB, value any, ids []uint) error { return owner.ApplyIDs(tx, value, ids) }
