package store

import (
	"encoding/json"
	"errors"

	paid "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid"
	paidprovider "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid/provider"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
		GrantAddDays:   tariff.AddDays,
		GrantTraffic:   tariff.AddTrafficBytes,
		GrantSnapshot:  true,
	}
}

// CreateInvoiceIntent arbitrates an active purchase durably, across coordinator
// instances and restarts. The insert is the first write; its unique constraint
// decides who may perform the non-idempotent remote create.
func CreateInvoiceIntent(db *gorm.DB, order *paid.PaymentOrder) (*paid.PaymentOrder, bool, error) {
	order.Status = paid.StatusInvoiceCreating
	res := db.Clauses(clause.OnConflict{DoNothing: true}).Create(order)
	if res.Error != nil {
		return nil, false, res.Error
	}
	if res.RowsAffected == 1 {
		return order, true, nil
	}
	var existing paid.PaymentOrder
	err := db.Where("client_id = ? AND tariff_id = ? AND telegram_user_id = ? AND provider = ? AND status IN ?",
		order.ClientId, order.TariffId, order.TelegramUserId, order.Provider,
		[]string{paid.StatusInvoiceCreating, paid.StatusRecoverable, paid.StatusPending}).First(&existing).Error
	return &existing, false, err
}

func ActiveCryptoBotOrder(db *gorm.DB, clientID, tariffID uint, tgID int64) (*paid.PaymentOrder, error) {
	var order paid.PaymentOrder
	err := db.Where("client_id = ? AND tariff_id = ? AND telegram_user_id = ? AND provider = 'cryptobot' AND status IN ?",
		clientID, tariffID, tgID, []string{paid.StatusPending, paid.StatusInvoiceCreating, paid.StatusRecoverable, paid.StatusManualReview}).Order("id").First(&order).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &order, err
}

func MarkInvoiceRecoverable(db *gorm.DB, id uint) error {
	return db.Model(&paid.PaymentOrder{}).Where("id = ? AND status = ?", id, paid.StatusInvoiceCreating).
		Updates(map[string]any{"status": paid.StatusRecoverable, "review_reason": "invoice_creation_uncertain"}).Error
}

func SaveInvoiceResult(db *gorm.DB, orderID uint, invoice *paidprovider.Invoice) error {
	if invoice == nil {
		return errors.New("provider returned an empty invoice")
	}
	updates := map[string]any{"status": paid.StatusPending, "review_reason": ""}
	if invoice.PayURL != "" {
		updates["external_url"] = invoice.PayURL
	}
	if invoice.ProviderRef != "" {
		updates["provider_ref"] = invoice.ProviderRef
		ref, _ := json.Marshal(map[string]string{"ref": invoice.ProviderRef})
		updates["provider_payload"] = ref
	}
	if len(updates) == 0 {
		return nil
	}
	result := db.Model(&paid.PaymentOrder{}).Where("id = ? AND status IN ? AND (provider_ref = '' OR provider_ref = ?)", orderID, []string{paid.StatusPending, paid.StatusInvoiceCreating}, invoice.ProviderRef).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrOrderAlreadyFinalized
	}
	return nil
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

func MarkOrderFailed(db *gorm.DB, id uint) error {
	result := db.Model(&paid.PaymentOrder{}).Where("id = ? AND status = ?", id, paid.StatusPending).
		Update("status", paid.StatusFailed)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrOrderAlreadyFinalized
	}
	return nil
}

func ReviewProviderOrders(db *gorm.DB, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	return db.Model(&paid.PaymentOrder{}).Where("id IN ? AND status IN ?", ids,
		[]string{paid.StatusPending, paid.StatusInvoiceCreating, paid.StatusRecoverable}).
		Updates(map[string]any{"status": paid.StatusManualReview, "review_reason": "provider_metadata_or_identity_conflict"}).Error
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
