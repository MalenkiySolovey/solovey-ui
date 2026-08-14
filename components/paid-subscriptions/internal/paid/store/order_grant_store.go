package store

import (
	"encoding/json"
	"errors"
	"fmt"

	paid "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/gorm"
)

var ErrOrderAlreadyFinalized = errors.New("order already finalized")

type AppliedOrderResult struct {
	Applied        bool
	TelegramUserID int64
	InboundIDs     []uint
}

func ApplyPaidOrderGrant(db *gorm.DB, orderID uint, chargeID string, raw []byte, now int64, actor string) (AppliedOrderResult, error) {
	var result AppliedOrderResult
	err := db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&paid.PaymentOrder{}).
			Where("id = ? AND status = ?", orderID, paid.StatusPending).
			Updates(map[string]any{
				"status":             paid.StatusPaid,
				"paid_at":            now,
				"provider_charge_id": chargeID,
				"provider_payload":   raw,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return ErrOrderAlreadyFinalized
		}
		var order paid.PaymentOrder
		if err := tx.Where("id = ?", orderID).First(&order).Error; err != nil {
			return err
		}
		grant, err := grantForOrder(tx, order)
		if err != nil {
			return err
		}
		var client model.Client
		if err := tx.Where("id = ?", order.ClientId).First(&client).Error; err != nil {
			return err
		}

		updates, orderUpdates, err := paid.BuildPaidClientUpdates(client, grant, now)
		if err != nil {
			return err
		}
		if len(orderUpdates) > 0 {
			if err := tx.Model(&paid.PaymentOrder{}).Where("id = ?", orderID).
				Updates(orderUpdates).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&model.Client{}).Where("id = ?", client.Id).Updates(updates).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.Changes{
			DateTime: now,
			Actor:    actor,
			Key:      "clients",
			Action:   "renew",
			Obj:      JSONString(client.Name),
		}).Error; err != nil {
			return err
		}
		result.Applied = true
		result.TelegramUserID = order.TelegramUserId
		if len(client.Inbounds) > 0 {
			_ = json.Unmarshal(client.Inbounds, &result.InboundIDs)
		}
		return nil
	})
	return result, err
}

func FinalizeRefundGrant(db *gorm.DB, orderID uint, revoke bool, now int64, actor string) ([]uint, error) {
	var inboundIDs []uint
	err := db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&paid.PaymentOrder{}).
			Where("id = ? AND status = ?", orderID, paid.StatusPaid).
			Update("status", paid.StatusRefunded)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return ErrOrderAlreadyFinalized
		}
		if !revoke {
			return nil
		}
		var order paid.PaymentOrder
		if err := tx.Where("id = ?", orderID).First(&order).Error; err != nil {
			return err
		}
		grant, err := grantForOrder(tx, order)
		if err != nil {
			return err
		}
		var client model.Client
		if err := tx.Where("id = ?", order.ClientId).First(&client).Error; err != nil {
			return err
		}
		restoreLiveUsage := true
		if grant.AddTrafficBytes > 0 {
			var newerTrafficOrders int64
			if err := tx.Model(&paid.PaymentOrder{}).
				Where("client_id = ? AND id > ? AND status = ?", order.ClientId, order.Id, paid.StatusPaid).
				Where("(grant_snapshot = ? AND grant_traffic_bytes > 0) OR (grant_snapshot = ? AND tariff_id IN (?))",
					true, false, tx.Model(&paid.Tariff{}).Select("id").Where("add_traffic_bytes > 0")).
				Count(&newerTrafficOrders).Error; err != nil {
				return err
			}
			restoreLiveUsage = newerTrafficOrders == 0
		}
		updates := paid.BuildRefundClientUpdates(client, order, grant, now, restoreLiveUsage)
		if len(updates) == 0 {
			return nil
		}
		if err := tx.Model(&model.Client{}).Where("id = ?", client.Id).Updates(updates).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.Changes{
			DateTime: now,
			Actor:    actor,
			Key:      "clients",
			Action:   "refund",
			Obj:      JSONString(client.Name),
		}).Error; err != nil {
			return err
		}
		if len(client.Inbounds) > 0 {
			_ = json.Unmarshal(client.Inbounds, &inboundIDs)
		}
		return nil
	})
	return inboundIDs, err
}

func grantForOrder(tx *gorm.DB, order paid.PaymentOrder) (paid.Tariff, error) {
	if order.GrantSnapshot {
		if order.Amount <= 0 || order.GrantAddDays < 0 || order.GrantTraffic < 0 {
			return paid.Tariff{}, fmt.Errorf("payment order grant snapshot is invalid")
		}
		return paid.Tariff{AddDays: order.GrantAddDays, AddTrafficBytes: order.GrantTraffic}, nil
	}
	var tariff paid.Tariff
	if err := tx.Where("id = ?", order.TariffId).First(&tariff).Error; err != nil {
		return paid.Tariff{}, err
	}
	if tariff.Price <= 0 && tariff.StarsAmount <= 0 {
		return paid.Tariff{}, fmt.Errorf("tariff has no price")
	}
	return tariff, nil
}

func JSONString(s string) json.RawMessage {
	b, err := json.Marshal(s)
	if err != nil {
		return json.RawMessage(`""`)
	}
	return b
}
