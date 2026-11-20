package store

import (
	"github.com/MalenkiySolovey/solovey-backend/storage/sqlite"
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

func ApplyPaidOrderGrant(db *gorm.DB, orderID uint, chargeID string, _ []byte, now int64, actor string) (AppliedOrderResult, error) {
	var result AppliedOrderResult
	err := commerce.SettlePayment(db, sqlite.RunTransaction, orderID, chargeID, now, func(tx *gorm.DB, order paid.PaymentOrder) (commerce.GrantedUsage, error) {
		renewal, err := entitlement.Renew(tx, order.ClientId, entitlementTerms(order), now, actor)
		if err != nil {
			return commerce.GrantedUsage{}, err
		}
		result = AppliedOrderResult{Applied: true, TelegramUserID: order.TelegramUserId, InboundIDs: renewal.InboundIDs}
		return commerce.GrantedUsage{Up: renewal.Usage.Up, Down: renewal.Usage.Down}, nil
	})
	if err != nil {
		return AppliedOrderResult{}, err
	}
	return result, nil
}

func FinalizeRefundGrant(db *gorm.DB, orderID uint, revoke bool, now int64, actor string) ([]uint, error) {
	return finalizeRefundGrant(db, orderID, "", revoke, now, actor)
}

func FinalizeClaimedRefundGrant(db *gorm.DB, orderID uint, token string, now int64, actor string) ([]uint, error) {
	if token == "" {
		return nil, commerce.ErrRefundInProgress
	}
	return finalizeRefundGrant(db, orderID, token, false, now, actor)
}

func finalizeRefundGrant(db *gorm.DB, orderID uint, claim string, revoke bool, now int64, actor string) ([]uint, error) {
	var inboundIDs []uint
	err := commerce.SettleRefund(db, sqlite.RunTransaction, orderID, claim, revoke, func(tx *gorm.DB, order paid.PaymentOrder, restoreUsage bool) error {
		var err error
		inboundIDs, err = entitlement.Revoke(tx, order.ClientId, entitlementTerms(order),
			entitlement.Usage{Up: order.GrantedUp, Down: order.GrantedDown}, restoreUsage, now, actor)
		return err
	})
	if err != nil {
		return nil, err
	}
	return inboundIDs, nil
}

func entitlementTerms(order paid.PaymentOrder) entitlement.Terms {
	return entitlement.Terms{Days: order.GrantAddDays, TrafficBytes: order.GrantTraffic}
}
