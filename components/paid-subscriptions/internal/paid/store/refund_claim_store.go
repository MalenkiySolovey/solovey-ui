package store

import (
	"errors"
	"strings"

	paid "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid"
	"gorm.io/gorm"
)

var ErrRefundInProgress = errors.New("refund is already in progress")

const refundClaimSeconds int64 = 120

// The durable intent survives uncertain remote outcomes and restart. A bounded
// claim arbitrates network calls; retries retain the original charge and policy.
func ClaimStarsRefund(db *gorm.DB, id uint, revoke bool, token string, now int64) (*paid.PaymentOrder, error) {
	if token == "" {
		return nil, errors.New("refund claim identity missing")
	}
	var order paid.PaymentOrder
	err := db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&paid.PaymentOrder{}).Where("id = ? AND status = ? AND provider = 'stars'", id, paid.StatusPaid).
			Updates(map[string]any{"status": paid.StatusRefundPending, "refund_revoke": revoke, "refund_claim": token, "refund_claim_until": now + refundClaimSeconds})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			res = tx.Model(&paid.PaymentOrder{}).Where("id = ? AND status = ? AND provider = 'stars' AND refund_claim_until <= ?", id, paid.StatusRefundPending, now).
				Updates(map[string]any{"refund_claim": token, "refund_claim_until": now + refundClaimSeconds})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return ErrRefundInProgress
			}
		}
		if err := tx.First(&order, id).Error; err != nil {
			return err
		}
		if err := ValidateOrderGrant(order); err != nil {
			return err
		}
		if order.Currency != "XTR" || order.TelegramUserId <= 0 || !strings.HasPrefix(order.ProviderChargeID, "tg:") || strings.TrimPrefix(order.ProviderChargeID, "tg:") == "" {
			return errors.New("refund charge identity invalid")
		}
		return nil
	})
	return &order, err
}
func ReleaseRefundClaim(db *gorm.DB, id uint, token string) error {
	return db.Model(&paid.PaymentOrder{}).Where("id = ? AND status = ? AND refund_claim = ?", id, paid.StatusRefundPending, token).
		Updates(map[string]any{"refund_claim": "", "refund_claim_until": 0}).Error
}
