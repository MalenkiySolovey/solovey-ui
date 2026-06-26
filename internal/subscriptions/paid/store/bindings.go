package store

import (
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	paid "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/paid"
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

func BindingForClient(db *gorm.DB, clientID uint) (*paid.Binding, error) {
	var binding paid.Binding
	if err := db.Where("client_id = ?", clientID).First(&binding).Error; err != nil {
		return nil, err
	}
	return &binding, nil
}

func SetBinding(db *gorm.DB, clientID uint, tgUserID int64, now int64) error {
	if clientID == 0 || tgUserID <= 0 {
		return gorm.ErrInvalidData
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("tg_user_id = ? OR client_id = ?", tgUserID, clientID).
			Delete(&paid.Binding{}).Error; err != nil {
			return err
		}
		return tx.Create(&paid.Binding{
			ClientId:  clientID,
			TgUserId:  tgUserID,
			CreatedAt: now,
			UpdatedAt: now,
		}).Error
	})
}

func UnbindClient(db *gorm.DB, clientID uint) error {
	return db.Where("client_id = ?", clientID).Delete(&paid.Binding{}).Error
}

func ListTelegramUserIDs(db *gorm.DB) ([]int64, error) {
	var rows []paid.Binding
	if err := db.Where("tg_user_id > 0").Order("id asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	users := make([]int64, 0, len(rows))
	for _, row := range rows {
		users = append(users, row.TgUserId)
	}
	return users, nil
}
