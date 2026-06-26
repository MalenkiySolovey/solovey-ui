package order

import (
	"encoding/json"

	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"gorm.io/gorm"
)

const Clause = "sort_order ASC, id ASC"

type DBTarget struct {
	ModelValue any
	Where      string
	Before     func(*gorm.DB) error
}

func ForSave(tx *gorm.DB, modelValue any, id uint) (int, error) {
	if id == 0 {
		return Next(tx, modelValue)
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

func Next(tx *gorm.DB, modelValue any) (int, error) {
	var maxSortOrder int
	if err := tx.Model(modelValue).Select("COALESCE(MAX(sort_order), 0)").Scan(&maxSortOrder).Error; err != nil {
		return 0, err
	}
	return maxSortOrder + 1, nil
}

func ReorderDBTarget(tx *gorm.DB, target DBTarget, data json.RawMessage) error {
	ids, err := ParseIDs(data)
	if err != nil {
		return err
	}
	if target.Before != nil {
		if err := target.Before(tx); err != nil {
			return err
		}
	}

	var currentIDs []uint
	query := tx.Model(target.ModelValue).Select("id").Order(Clause)
	if target.Where != "" {
		query = query.Where(target.Where)
	}
	if err := query.Scan(&currentIDs).Error; err != nil {
		return err
	}
	if err := ValidateIDs(currentIDs, ids); err != nil {
		return err
	}
	return ApplyIDs(tx, target.ModelValue, ids)
}

func ParseIDs(data json.RawMessage) ([]uint, error) {
	var ids []uint
	if err := json.Unmarshal(data, &ids); err == nil {
		return ids, nil
	}

	var numbers []float64
	if err := json.Unmarshal(data, &numbers); err != nil {
		return nil, err
	}
	ids = make([]uint, 0, len(numbers))
	for _, n := range numbers {
		if n <= 0 || n != float64(uint(n)) {
			return nil, common.NewError("invalid reorder id")
		}
		ids = append(ids, uint(n))
	}
	return ids, nil
}

func ValidateIDs(current []uint, requested []uint) error {
	if len(current) != len(requested) {
		return common.NewErrorf("reorder list length mismatch: got %d, want %d", len(requested), len(current))
	}
	expected := make(map[uint]struct{}, len(current))
	for _, id := range current {
		expected[id] = struct{}{}
	}
	seen := make(map[uint]struct{}, len(requested))
	for _, id := range requested {
		if _, exists := seen[id]; exists {
			return common.NewErrorf("duplicate reorder id: %d", id)
		}
		seen[id] = struct{}{}
		if _, ok := expected[id]; !ok {
			return common.NewErrorf("unknown reorder id: %d", id)
		}
	}
	return nil
}

func ApplyIDs(tx *gorm.DB, modelValue any, ids []uint) error {
	for index, id := range ids {
		update := tx.Model(modelValue).Where("id = ?", id).Update("sort_order", index+1)
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected == 0 {
			return common.NewErrorf("reorder id %d was not updated", id)
		}
	}
	return nil
}
