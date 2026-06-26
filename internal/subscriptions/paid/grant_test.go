package paid

import (
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

func TestBuildPaidClientUpdatesExtendsFromLaterExpiryAndSnapshotsTraffic(t *testing.T) {
	now := int64(1_700_000_000)
	client := model.Client{
		Expiry:    now + 86400,
		Volume:    1000,
		Up:        200,
		Down:      300,
		TotalUp:   400,
		TotalDown: 500,
	}
	tariff := Tariff{AddDays: 7, AddTrafficBytes: 9000}

	clientUpdates, orderUpdates := BuildPaidClientUpdates(client, tariff, now)
	if clientUpdates["enable"] != true {
		t.Fatalf("enable update missing: %#v", clientUpdates)
	}
	if clientUpdates["expiry"] != now+86400+7*86400 {
		t.Fatalf("expiry update = %#v", clientUpdates["expiry"])
	}
	if clientUpdates["volume"] != int64(10000) || clientUpdates["up"] != int64(0) || clientUpdates["down"] != int64(0) {
		t.Fatalf("traffic reset updates wrong: %#v", clientUpdates)
	}
	if clientUpdates["total_up"] != int64(600) || clientUpdates["total_down"] != int64(800) {
		t.Fatalf("total counters wrong: %#v", clientUpdates)
	}
	if orderUpdates["granted_up"] != int64(200) || orderUpdates["granted_down"] != int64(300) {
		t.Fatalf("grant snapshot wrong: %#v", orderUpdates)
	}
}

func TestBuildRefundClientUpdatesClampsCounters(t *testing.T) {
	now := int64(1_700_000_000)
	client := model.Client{
		Expiry:    now + 2*86400,
		Volume:    500,
		Up:        50,
		Down:      60,
		TotalUp:   100,
		TotalDown: 100,
	}
	order := PaymentOrder{GrantedUp: 200, GrantedDown: 300}
	tariff := Tariff{AddDays: 7, AddTrafficBytes: 9000}

	updates := BuildRefundClientUpdates(client, order, tariff, now, true)
	if updates["expiry"] != now {
		t.Fatalf("expiry should clamp to now: %#v", updates["expiry"])
	}
	if updates["volume"] != int64(0) {
		t.Fatalf("volume should clamp to zero: %#v", updates["volume"])
	}
	if updates["up"] != int64(200) || updates["down"] != int64(300) {
		t.Fatalf("usage restore wrong: %#v", updates)
	}
	if updates["total_up"] != int64(0) || updates["total_down"] != int64(0) {
		t.Fatalf("total counters should clamp: %#v", updates)
	}
}

func TestBuildRefundClientUpdatesPreservesLiveUsageForOlderOrder(t *testing.T) {
	client := model.Client{Volume: 2000, Up: 30, Down: 40, TotalUp: 1150, TotalDown: 2260}
	order := PaymentOrder{GrantedUp: 100, GrantedDown: 200}
	updates := BuildRefundClientUpdates(client, order, Tariff{AddTrafficBytes: 1000}, 1_700_000_000, false)
	if _, exists := updates["up"]; exists {
		t.Fatalf("older refund must not update current up/down: %#v", updates)
	}
	if updates["total_up"] != int64(1050) || updates["total_down"] != int64(2060) {
		t.Fatalf("totals must still roll back relatively: %#v", updates)
	}
}
