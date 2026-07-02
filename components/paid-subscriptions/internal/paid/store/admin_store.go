package store

import (
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/gorm"
)

type BindingRow struct {
	ClientId uint   `json:"clientId"`
	Name     string `json:"name"`
	Enable   bool   `json:"enable"`
	TgUserId int64  `json:"tgUserId"`
	Desc     string `json:"desc"`
	Expiry   int64  `json:"expiry"`
}

func ListBindingRows(db *gorm.DB) ([]BindingRow, error) {
	var rows []BindingRow
	err := db.Table("clients c").
		Select("c.id as client_id, c.name as name, c.enable as enable, c.desc as `desc`, c.expiry as expiry, COALESCE(b.tg_user_id, 0) as tg_user_id").
		Joins("LEFT JOIN paidsub_bindings b ON b.client_id = c.id").
		Order("c.sort_order, c.id").
		Scan(&rows).Error
	return rows, err
}

func ClientExists(db *gorm.DB, clientID uint) (bool, error) {
	if clientID == 0 {
		return false, nil
	}
	var count int64
	if err := db.Model(&model.Client{}).Where("id = ?", clientID).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

type OrderRow struct {
	Id             uint   `json:"id"`
	ClientId       uint   `json:"clientId"`
	Provider       string `json:"provider"`
	Amount         int64  `json:"amount"`
	Currency       string `json:"currency"`
	Status         string `json:"status"`
	TelegramUserId int64  `json:"telegramUserId"`
	CreatedAt      int64  `json:"createdAt"`
	ClientName     string `json:"clientName"`
	ClientDesc     string `json:"clientDesc"`
}

func ListOrderRows(db *gorm.DB, limit int) ([]OrderRow, error) {
	if limit <= 0 {
		limit = 200
	}
	var rows []OrderRow
	err := db.Table("payment_orders o").
		Select("o.id as id, o.client_id as client_id, o.provider as provider, o.amount as amount, o.currency as currency, o.status as status, o.telegram_user_id as telegram_user_id, o.created_at as created_at, c.name as client_name, c.desc as client_desc").
		Joins("LEFT JOIN clients c ON c.id = o.client_id").
		Order("o.id desc").
		Limit(limit).
		Scan(&rows).Error
	return rows, err
}
