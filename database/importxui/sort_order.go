package importxui

import "gorm.io/gorm"

func nextImportSortOrder(tx *gorm.DB, modelValue any) (int, error) {
	var maxSortOrder int
	if err := tx.Model(modelValue).Select("COALESCE(MAX(sort_order), 0)").Scan(&maxSortOrder).Error; err != nil {
		return 0, err
	}
	return maxSortOrder + 1, nil
}
