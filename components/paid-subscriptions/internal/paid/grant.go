package paid

import (
	"fmt"
	"math"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

func BuildPaidClientUpdates(client model.Client, tariff Tariff, now int64) (clientUpdates map[string]any, orderUpdates map[string]any, err error) {
	clientUpdates = map[string]any{"enable": true}
	orderUpdates = map[string]any{}
	if tariff.AddDays > 0 {
		base := client.Expiry
		if base < now {
			base = now
		}
		extension := int64(tariff.AddDays) * 86400
		if base < 0 || base > math.MaxInt64-extension {
			return nil, nil, fmt.Errorf("paid subscription expiry would overflow")
		}
		clientUpdates["expiry"] = base + extension
	}
	if tariff.AddTrafficBytes > 0 {
		volume, err := addNonNegative(client.Volume, tariff.AddTrafficBytes)
		if err != nil {
			return nil, nil, fmt.Errorf("paid subscription volume: %w", err)
		}
		totalUp, err := addNonNegative(client.TotalUp, client.Up)
		if err != nil {
			return nil, nil, fmt.Errorf("paid subscription upload total: %w", err)
		}
		totalDown, err := addNonNegative(client.TotalDown, client.Down)
		if err != nil {
			return nil, nil, fmt.Errorf("paid subscription download total: %w", err)
		}
		clientUpdates["volume"] = volume
		clientUpdates["total_up"] = totalUp
		clientUpdates["total_down"] = totalDown
		clientUpdates["up"] = int64(0)
		clientUpdates["down"] = int64(0)
		orderUpdates["granted_up"] = client.Up
		orderUpdates["granted_down"] = client.Down
	}
	return clientUpdates, orderUpdates, nil
}

func addNonNegative(left, right int64) (int64, error) {
	if left < 0 || right < 0 || left > math.MaxInt64-right {
		return 0, fmt.Errorf("value would overflow")
	}
	return left + right, nil
}

func BuildRefundClientUpdates(client model.Client, order PaymentOrder, tariff Tariff, now int64, restoreLiveUsage bool) map[string]any {
	updates := map[string]any{}
	if tariff.AddDays > 0 && client.Expiry > 0 {
		newExpiry := client.Expiry - int64(tariff.AddDays)*86400
		if newExpiry < now {
			newExpiry = now
		}
		updates["expiry"] = newExpiry
	}
	if tariff.AddTrafficBytes > 0 {
		newVolume := client.Volume - tariff.AddTrafficBytes
		if newVolume < 0 {
			newVolume = 0
		}
		updates["volume"] = newVolume
		newTotalUp := client.TotalUp - order.GrantedUp
		if newTotalUp < 0 {
			newTotalUp = 0
		}
		newTotalDown := client.TotalDown - order.GrantedDown
		if newTotalDown < 0 {
			newTotalDown = 0
		}
		if restoreLiveUsage {
			updates["up"] = order.GrantedUp
			updates["down"] = order.GrantedDown
		}
		updates["total_up"] = newTotalUp
		updates["total_down"] = newTotalDown
	}
	return updates
}
