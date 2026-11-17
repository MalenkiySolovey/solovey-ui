// Package saveidentity owns the invariant that create operations cannot select
// an existing primary key and edit operations must target an existing row.
package saveidentity

import (
	"errors"

	"gorm.io/gorm"
)

func Validate(tx *gorm.DB, action string, id uint, model any) error {
	if tx == nil || model == nil {
		return errors.New("entity persistence is unavailable")
	}
	switch action {
	case "new":
		if id != 0 {
			return errors.New("new entity must not specify an id")
		}
		return nil
	case "edit":
		if id == 0 {
			return errors.New("entity id is required for edit")
		}
		var count int64
		if err := tx.Model(model).Where("id = ?", id).Limit(1).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return gorm.ErrRecordNotFound
		}
		return nil
	default:
		return errors.New("unsupported entity save action")
	}
}
