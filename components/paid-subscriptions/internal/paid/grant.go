package paid

import "github.com/MalenkiySolovey/solovey-ui/database/model"

func BuildPaidClientUpdates(client model.Client, tariff Tariff, now int64) (clientUpdates map[string]any, orderUpdates map[string]any) {
	clientUpdates = map[string]any{"enable": true}
	orderUpdates = map[string]any{}
	if tariff.AddDays > 0 {
		base := client.Expiry
		if base < now {
			base = now
		}
		clientUpdates["expiry"] = base + int64(tariff.AddDays)*86400
	}
	if tariff.AddTrafficBytes > 0 {
		clientUpdates["volume"] = client.Volume + tariff.AddTrafficBytes
		clientUpdates["total_up"] = client.TotalUp + client.Up
		clientUpdates["total_down"] = client.TotalDown + client.Down
		clientUpdates["up"] = int64(0)
		clientUpdates["down"] = int64(0)
		orderUpdates["granted_up"] = client.Up
		orderUpdates["granted_down"] = client.Down
	}
	return clientUpdates, orderUpdates
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
