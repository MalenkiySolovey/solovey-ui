package failover

import (
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func WriteMemberStates(db *gorm.DB, states []model.FailoverMemberState) error {
	if db == nil || len(states) == 0 {
		return nil
	}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "group_tag"}, {Name: "member_tag"}},
		UpdateAll: true,
	}).Create(&states).Error
}

func ReadMemberStates(db *gorm.DB, groupTag string) ([]model.FailoverMemberState, error) {
	var rows []model.FailoverMemberState
	if db == nil {
		return rows, nil
	}
	err := db.Where("group_tag = ?", groupTag).Find(&rows).Error
	return rows, err
}
