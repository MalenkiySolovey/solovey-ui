package store

import (
	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	"gorm.io/gorm"
)

func ClientByTelegramUserID(db *gorm.DB, tgUserID int64) (*model.Client, error) {
	if tgUserID <= 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var client model.Client
	err := db.Model(&model.Client{}).
		Joins("JOIN paidsub_bindings b ON b.client_id = clients.id").
		Where("b.tg_user_id = ?", tgUserID).
		First(&client).Error
	if err != nil {
		return nil, err
	}
	return &client, nil
}
