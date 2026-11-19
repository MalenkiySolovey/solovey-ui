package store

import (
	"github.com/MalenkiySolovey/solovey-component-paid-subscriptions/provider"
	commerce "github.com/MalenkiySolovey/solovey-component-paid-subscriptions/store"
	"gorm.io/gorm"
)

// RecoverProviderOrder binds historical state repair to the same transaction
// as the Client entitlement; used by direct cross-owner recovery fixtures.
func RecoverProviderOrder(db *gorm.DB, recovered provider.Reconciliation, now, grace int64) (AppliedOrderResult, error) {
	var result AppliedOrderResult
	err := commerce.RecoverProviderOrder(db, recovered, now, grace, func(tx *gorm.DB, orderID uint, chargeID string) error {
		var err error
		result, err = ApplyPaidOrderGrant(tx, orderID, chargeID, nil, now, "PaidSubBot")
		return err
	})
	if err != nil {
		return AppliedOrderResult{}, err
	}
	return result, nil
}
