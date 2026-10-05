package store

import (
	"encoding/json"
	"errors"

	paid "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid"
	provider "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid/provider"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Cursors record attempted work, never financial completion. Failed remote
// requests rotate fairly; canceled operations leave the cursor unchanged.
func WorkOrders(db *gorm.DB, kind string, limit int) ([]paid.PaymentOrder, error) {
	var cursor paid.ProviderCursor
	if err := db.Where("kind = ?", kind).Find(&cursor).Error; err != nil {
		return nil, err
	}
	query := db.Where("provider = 'cryptobot'")
	if kind == "poll" {
		query = query.Where("status = ? AND provider_ref != ''", paid.StatusPending)
	} else {
		query = query.Where("status IN ? OR (status = ? AND provider_ref = '')", []string{paid.StatusInvoiceCreating, paid.StatusRecoverable}, paid.StatusPending)
	}
	var orders []paid.PaymentOrder
	if err := query.Session(&gorm.Session{}).Where("id > ?", cursor.AfterID).Order("id").Limit(limit).Find(&orders).Error; err != nil {
		return nil, err
	}
	if len(orders) == 0 && cursor.AfterID > 0 {
		if err := query.Session(&gorm.Session{}).Order("id").Limit(limit).Find(&orders).Error; err != nil {
			return nil, err
		}
	}
	return orders, nil
}
func AdvanceProviderCursor(db *gorm.DB, kind string, after uint) error {
	return db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "kind"}}, DoUpdates: clause.AssignmentColumns([]string{"after_id"})}).
		Create(&paid.ProviderCursor{Kind: kind, AfterID: after}).Error
}

func CancellationWork(db *gorm.DB, limit int) ([]paid.InvoiceCancellation, error) {
	var cursor paid.ProviderCursor
	if err := db.Where("kind = 'cancel'").Find(&cursor).Error; err != nil {
		return nil, err
	}
	var work []paid.InvoiceCancellation
	query := db.Where("completed = ?", false)
	if err := query.Session(&gorm.Session{}).Where("id > ?", cursor.AfterID).Order("id").Limit(limit).Find(&work).Error; err != nil {
		return nil, err
	}
	if len(work) == 0 && cursor.AfterID > 0 {
		if err := query.Session(&gorm.Session{}).Order("id").Limit(limit).Find(&work).Error; err != nil {
			return nil, err
		}
	}
	return work, nil
}
func CompleteCancellation(db *gorm.DB, id uint) error {
	return db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&paid.InvoiceCancellation{}).Where("id = ? AND completed = ?", id, false).Update("completed", true)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil
		}
		var work paid.InvoiceCancellation
		if err := tx.First(&work, id).Error; err != nil {
			return err
		}
		var outstanding int64
		if err := tx.Model(&paid.InvoiceCancellation{}).Where("order_id = ? AND completed = ?", work.OrderID, false).Count(&outstanding).Error; err != nil {
			return err
		}
		if outstanding == 0 {
			return tx.Model(&paid.PaymentOrder{}).Where("id = ? AND grant_snapshot = 0 AND status = ? AND review_reason = 'legacy_cancel_pending'", work.OrderID, paid.StatusRecoverable).
				Updates(map[string]any{"status": paid.StatusFailed, "legacy_resolved": true, "review_reason": "legacy_cancelled"}).Error
		}
		return nil
	})
}
func queueCancellations(tx *gorm.DB, orderID uint, candidates []provider.RecoveredInvoice) error {
	for _, candidate := range candidates {
		if candidate.State != provider.InvoiceActive {
			continue
		}
		res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&paid.InvoiceCancellation{OrderID: orderID, Ref: candidate.Ref})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			var existing paid.InvoiceCancellation
			if err := tx.Where("ref = ?", candidate.Ref).First(&existing).Error; err != nil {
				return err
			}
			if existing.OrderID != orderID {
				return errors.New("cancellation identity belongs to another order")
			}
		}
	}
	return nil
}

// Recovery selects only a sole authenticated paid invoice. Recovery state,
// sibling cancellation and local application compose in the same transaction.
func RecoverProviderOrder(db *gorm.DB, recovered provider.Reconciliation, now, grace int64) (AppliedOrderResult, error) {
	var applied AppliedOrderResult
	err := db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&paid.PaymentOrder{}).Where("id = ? AND provider = 'cryptobot' AND (status IN ? OR (status = ? AND provider_ref = ''))", recovered.OrderID, []string{paid.StatusInvoiceCreating, paid.StatusRecoverable}, paid.StatusPending).Update("status", paid.StatusRecoverable)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return ErrOrderAlreadyFinalized
		}
		var order paid.PaymentOrder
		if err := tx.First(&order, recovered.OrderID).Error; err != nil {
			return err
		}
		review := func(reason string) error {
			return tx.Model(&paid.PaymentOrder{}).Where("id = ?", order.Id).Updates(map[string]any{"status": paid.StatusManualReview, "review_reason": reason}).Error
		}
		if recovered.ReviewReason != "" {
			return review(recovered.ReviewReason)
		}
		if len(recovered.Candidates) == 0 {
			return nil
		}
		var active, paidInvoices []provider.RecoveredInvoice
		seen := make(map[string]bool)
		for _, candidate := range recovered.Candidates {
			if !candidate.Verified || candidate.Ref == "" || seen[candidate.Ref] || candidate.State == provider.InvoiceUnknown {
				return review("provider_metadata_or_identity_conflict")
			}
			seen[candidate.Ref] = true
			if candidate.State == provider.InvoicePaid {
				paidInvoices = append(paidInvoices, candidate)
			}
			if candidate.State == provider.InvoiceActive {
				active = append(active, candidate)
			}
		}
		if len(paidInvoices) > 1 {
			return review("multiple_paid_invoices")
		}
		if !order.GrantSnapshot {
			if len(paidInvoices) > 0 {
				if err := queueCancellations(tx, order.Id, active); err != nil {
					return err
				}
				updates := map[string]any{"status": paid.StatusManualReview, "review_reason": "legacy_snapshot_missing", "provider_ref": paidInvoices[0].Ref, "provider_charge_id": "cryptobot:" + paidInvoices[0].Ref}
				if len(order.ProviderPayload) == 0 {
					updates["provider_payload"] = invoiceEvidence(order.ProviderRef, paidInvoices[0].Ref)
				}
				return tx.Model(&order).Updates(updates).Error
			}
			if err := queueCancellations(tx, order.Id, active); err != nil {
				return err
			}
			if len(active) > 0 {
				return tx.Model(&order).Updates(map[string]any{"review_reason": "legacy_cancel_pending"}).Error
			}
			return tx.Model(&order).Updates(map[string]any{"status": paid.StatusFailed, "legacy_resolved": true, "review_reason": "legacy_terminal"}).Error
		}
		if err := ValidateOrderGrant(order); err != nil {
			return review("purchase_snapshot_invalid")
		}
		var canonical provider.RecoveredInvoice
		if len(paidInvoices) == 1 {
			canonical = paidInvoices[0]
		} else if len(active) == 1 {
			canonical = active[0]
		} else if len(active) > 1 {
			return review("multiple_active_invoices")
		} else {
			if order.CreatedAt > 0 && order.CreatedAt < now-grace {
				return tx.Model(&order).Updates(map[string]any{"status": paid.StatusExpired, "review_reason": "provider_terminal"}).Error
			}
			return nil
		}
		if canonical.State == provider.InvoiceActive && canonical.PayURL == "" {
			return review("provider_invoice_url_missing")
		}
		updates := map[string]any{"status": paid.StatusPending, "provider_ref": canonical.Ref, "external_url": canonical.PayURL, "review_reason": ""}
		// The canonical ref is authoritative; existing raw historical evidence
		// remains intact even when a verified paid sibling becomes canonical.
		if len(order.ProviderPayload) == 0 {
			updates["provider_payload"] = invoiceEvidence(order.ProviderRef, canonical.Ref)
		}
		if err := tx.Model(&order).Updates(updates).Error; err != nil {
			return err
		}
		if canonical.State != provider.InvoicePaid {
			return nil
		}
		if err := queueCancellations(tx, order.Id, active); err != nil {
			return err
		}
		var err error
		applied, err = ApplyPaidOrderGrant(tx, order.Id, "cryptobot:"+canonical.Ref, nil, now, "PaidSubBot")
		return err
	})
	if errors.Is(err, ErrOrderAlreadyFinalized) {
		return AppliedOrderResult{}, nil
	}
	return applied, err
}

func invoiceEvidence(originalRef, canonicalRef string) []byte {
	if originalRef == "" {
		originalRef = canonicalRef
	}
	payload, _ := json.Marshal(map[string]string{"ref": originalRef})
	return payload
}

func ExpireVerifiedProviderOrders(db *gorm.DB, ids []uint, now, grace int64) error {
	if len(ids) == 0 {
		return nil
	}
	return db.Model(&paid.PaymentOrder{}).Where("id IN ? AND provider = 'cryptobot' AND status = ? AND created_at > 0 AND created_at < ?", ids, paid.StatusPending, now-grace).
		Updates(map[string]any{"status": paid.StatusExpired, "review_reason": "provider_terminal"}).Error
}
