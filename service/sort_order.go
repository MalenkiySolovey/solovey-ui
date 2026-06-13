package service

import (
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"gorm.io/gorm"
)

const sortOrderClause = "sort_order ASC, id ASC"

func sortOrderForSave(tx *gorm.DB, modelValue any, id uint) (int, error) {
	if id == 0 {
		return nextSortOrder(tx, modelValue)
	}

	var existing int
	result := tx.Model(modelValue).Select("sort_order").Where("id = ?", id).Scan(&existing)
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected == 0 {
		return 0, common.NewError("record not found")
	}
	return existing, nil
}

func nextSortOrder(tx *gorm.DB, modelValue any) (int, error) {
	var maxSortOrder int
	if err := tx.Model(modelValue).Select("COALESCE(MAX(sort_order), 0)").Scan(&maxSortOrder).Error; err != nil {
		return 0, err
	}
	return maxSortOrder + 1, nil
}
