package store

import (
	"encoding/json"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	paid "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/paid"
	paidprovider "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/paid/provider"
	"gorm.io/gorm"
)

func NewPendingOrder(client *model.Client, tariff *paid.Tariff, kind paidprovider.ProviderKind, amount int64, currency string, tgUserID int64, idempotencyKey string, now int64, ttlMinutes int) *paid.PaymentOrder {
	return &paid.PaymentOrder{
		ClientId:       client.Id,
		TariffId:       tariff.Id,
		Provider:       string(kind),
		Amount:         amount,
		Currency:       currency,
		Status:         paid.StatusPending,
		TelegramUserId: tgUserID,
		IdempotencyKey: idempotencyKey,
		CreatedAt:      now,
		ExpiresAt:      now + int64(ttlMinutes)*60,
	}
}

func SaveInvoiceResult(db *gorm.DB, orderID uint, invoice *paidprovider.Invoice) error {
	updates := map[string]any{}
	if invoice.PayURL != "" {
		updates["external_url"] = invoice.PayURL
	}
	if invoice.ProviderRef != "" {
		ref, _ := json.Marshal(map[string]string{"ref": invoice.ProviderRef})
		updates["provider_payload"] = ref
	}
	if len(updates) == 0 {
		return nil
	}
	return db.Model(&paid.PaymentOrder{}).Where("id = ?", orderID).Updates(updates).Error
}

func GetOrder(db *gorm.DB, id uint) (*paid.PaymentOrder, error) {
	var order paid.PaymentOrder
	if err := db.Where("id = ?", id).First(&order).Error; err != nil {
		return nil, err
	}
	return &order, nil
}

func FindOrderByPayload(db *gorm.DB, payload string) (*paid.PaymentOrder, error) {
	if payload == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var order paid.PaymentOrder
	if err := db.Where("idempotency_key = ?", payload).First(&order).Error; err != nil {
		return nil, err
	}
	return &order, nil
}

func MarkOrderFailed(db *gorm.DB, id uint) {
	_ = db.Model(&paid.PaymentOrder{}).Where("id = ? AND status = ?", id, paid.StatusPending).
		Update("status", paid.StatusFailed).Error
}

func ExpireStaleOrders(db *gorm.DB, now int64) error {
	return db.Model(&paid.PaymentOrder{}).
		Where("status = ? AND provider <> ? AND expires_at > 0 AND expires_at < ?",
			paid.StatusPending, string(paidprovider.ProviderCryptoBot), now).
		Update("status", paid.StatusExpired).Error
}

func ExpireStalePolledOrders(db *gorm.DB, now int64, graceSeconds int64) error {
	cutoff := now - graceSeconds
	return db.Model(&paid.PaymentOrder{}).
		Where("status = ? AND provider = ? AND created_at > 0 AND created_at < ?",
			paid.StatusPending, string(paidprovider.ProviderCryptoBot), cutoff).
		Update("status", paid.StatusExpired).Error
}

func OrdersForTelegramUser(db *gorm.DB, tgUserID int64, limit int) ([]paid.PaymentOrder, error) {
	if tgUserID <= 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	var orders []paid.PaymentOrder
	if err := db.Where("telegram_user_id = ?", tgUserID).Order("id desc").Limit(limit).Find(&orders).Error; err != nil {
		return nil, err
	}
	return orders, nil
}

func RefundableOrdersForTelegramUser(db *gorm.DB, tgUserID int64, limit int) ([]paid.PaymentOrder, error) {
	if tgUserID <= 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	var orders []paid.PaymentOrder
	if err := db.Where("telegram_user_id = ? AND status = ?", tgUserID, paid.StatusPaid).
		Order("id desc").Limit(limit).Find(&orders).Error; err != nil {
		return nil, err
	}
	return orders, nil
}

func PendingOrdersByProvider(db *gorm.DB, kind paidprovider.ProviderKind) ([]paid.PaymentOrder, error) {
	var pending []paid.PaymentOrder
	err := db.Where("provider = ? AND status = ?", string(kind), paid.StatusPending).
		Find(&pending).Error
	return pending, err
}
