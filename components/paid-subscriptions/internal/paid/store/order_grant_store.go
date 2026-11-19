package store

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/MalenkiySolovey/solovey-backend/users/entitlement"
	paid "github.com/MalenkiySolovey/solovey-component-paid-subscriptions"
	commerce "github.com/MalenkiySolovey/solovey-component-paid-subscriptions/store"
	"gorm.io/gorm"
)

type AppliedOrderResult struct {
	Applied        bool
	TelegramUserID int64
	InboundIDs     []uint
}

func ApplyPaidOrderGrant(db *gorm.DB, orderID uint, chargeID string, raw []byte, now int64, actor string) (AppliedOrderResult, error) {
	var result AppliedOrderResult
	if chargeID == "" {
		return result, fmt.Errorf("payment charge identity is empty")
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&paid.PaymentOrder{}).
			Where("id = ? AND status = ?", orderID, paid.StatusPending).
			Updates(map[string]any{
				"status":             paid.StatusPaid,
				"paid_at":            now,
				"provider_charge_id": chargeID,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return commerce.ErrOrderAlreadyFinalized
		}
		var order paid.PaymentOrder
		if err := tx.Where("id = ?", orderID).First(&order).Error; err != nil {
			return err
		}
		// Keep the durable invoice reference; arbitrary confirmation payloads
		// are neither financial authority nor necessary durable audit evidence.
		grant, err := grantForOrder(tx, order)
		if err != nil {
			return err
		}
		renewal, err := entitlement.Renew(tx, order.ClientId, grant, now, actor)
		if err != nil {
			return err
		}
		if grant.TrafficBytes > 0 {
			orderUpdates := map[string]any{"granted_up": renewal.Usage.Up, "granted_down": renewal.Usage.Down}
			if err := tx.Model(&paid.PaymentOrder{}).Where("id = ?", orderID).
				Updates(orderUpdates).Error; err != nil {
				return err
			}
		}
		result = AppliedOrderResult{Applied: true, TelegramUserID: order.TelegramUserId, InboundIDs: renewal.InboundIDs}
		return nil
	})
	if err != nil {
		return AppliedOrderResult{}, err
	}
	return result, nil
}

func FinalizeRefundGrant(db *gorm.DB, orderID uint, revoke bool, now int64, actor string) ([]uint, error) {
	return finalizeRefundGrant(db, orderID, paid.StatusPaid, "", revoke, now, actor)
}

func FinalizeClaimedRefundGrant(db *gorm.DB, orderID uint, token string, now int64, actor string) ([]uint, error) {
	if token == "" {
		return nil, ErrRefundInProgress
	}
	return finalizeRefundGrant(db, orderID, paid.StatusRefundPending, token, false, now, actor)
}

func finalizeRefundGrant(db *gorm.DB, orderID uint, status, token string, revoke bool, now int64, actor string) ([]uint, error) {
	var inboundIDs []uint
	err := db.Transaction(func(tx *gorm.DB) error {
		query := tx.Model(&paid.PaymentOrder{}).Where("id = ? AND status = ?", orderID, status)
		if token != "" {
			query = query.Where("refund_claim = ?", token)
		}
		res := query.Updates(map[string]any{"status": paid.StatusRefunded, "refund_claim": "", "refund_claim_until": 0})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return commerce.ErrOrderAlreadyFinalized
		}
		var order paid.PaymentOrder
		if err := tx.Where("id = ?", orderID).First(&order).Error; err != nil {
			return err
		}
		if token != "" {
			revoke = order.RefundRevoke
		}
		if !revoke {
			_, err := grantForOrder(tx, order)
			return err
		}
		grant, err := grantForOrder(tx, order)
		if err != nil {
			return err
		}
		restoreLiveUsage := true
		if grant.TrafficBytes > 0 {
			var newerTrafficOrders int64
			if err := tx.Model(&paid.PaymentOrder{}).
				Where("client_id = ? AND id > ? AND status IN ?", order.ClientId, order.Id, []string{paid.StatusPaid, paid.StatusRefundPending}).
				Where("grant_snapshot = ? AND grant_traffic_bytes > 0", true).
				Count(&newerTrafficOrders).Error; err != nil {
				return err
			}
			restoreLiveUsage = newerTrafficOrders == 0
		}
		inboundIDs, err = entitlement.Revoke(tx, order.ClientId, grant,
			entitlement.Usage{Up: order.GrantedUp, Down: order.GrantedDown}, restoreLiveUsage, now, actor)
		return err
	})
	if err != nil {
		return nil, err
	}
	return inboundIDs, nil
}

func ValidateOrderGrant(order paid.PaymentOrder) error {
	_, err := grantForOrder(nil, order)
	return err
}

func grantForOrder(_ *gorm.DB, order paid.PaymentOrder) (entitlement.Terms, error) {
	if order.GrantSnapshot {
		if order.Amount <= 0 || order.GrantAddDays < 0 || int64(order.GrantAddDays) > math.MaxInt64/86400 || order.GrantTraffic < 0 {
			return entitlement.Terms{}, fmt.Errorf("payment order grant snapshot is invalid")
		}
		return entitlement.Terms{Days: order.GrantAddDays, TrafficBytes: order.GrantTraffic}, nil
	}
	return entitlement.Terms{}, fmt.Errorf("payment order requires manual review: purchase snapshot missing")
}

func JSONString(s string) json.RawMessage {
	b, err := json.Marshal(s)
	if err != nil {
		return json.RawMessage(`""`)
	}
	return b
}
