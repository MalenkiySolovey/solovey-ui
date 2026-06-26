package store

import (
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	paid "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/paid"
	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/paid/provider"
)

func TestApplyPaidOrderGrantAndRefundAreAtomicDomainOperations(t *testing.T) {
	db := newPaidDB(t)
	now := int64(1_700_000_000)
	client := model.Client{
		Name:     `quoted "client"`,
		Enable:   false,
		Expiry:   now,
		Volume:   100,
		Up:       30,
		Down:     20,
		Inbounds: json.RawMessage(`[1,2]`),
	}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	tariff := paid.Tariff{Name: "Month", Price: 10000, Currency: "RUB", AddDays: 30, AddTrafficBytes: 900}
	if err := db.Create(&tariff).Error; err != nil {
		t.Fatal(err)
	}
	order := paid.PaymentOrder{
		ClientId:       client.Id,
		TariffId:       tariff.Id,
		Provider:       string(provider.ProviderStripe),
		Amount:         10000,
		Currency:       "RUB",
		Status:         paid.StatusPending,
		TelegramUserId: 77,
		IdempotencyKey: "grant-order",
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}

	applied, err := ApplyPaidOrderGrant(db, order.Id, "charge-1", []byte(`{"ok":true}`), now, "test")
	if err != nil {
		t.Fatal(err)
	}
	if !applied.Applied || applied.TelegramUserID != 77 || len(applied.InboundIDs) != 2 {
		t.Fatalf("applied = %#v", applied)
	}
	var savedClient model.Client
	if err := db.First(&savedClient, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	if !savedClient.Enable || savedClient.Volume != 1000 || savedClient.Up != 0 || savedClient.Down != 0 {
		t.Fatalf("client grant updates wrong: %#v", savedClient)
	}
	gotOrder, err := GetOrder(db, order.Id)
	if err != nil {
		t.Fatal(err)
	}
	if gotOrder.Status != paid.StatusPaid || gotOrder.ProviderChargeID != "charge-1" || gotOrder.GrantedUp != 30 || gotOrder.GrantedDown != 20 {
		t.Fatalf("order grant updates wrong: %#v", gotOrder)
	}

	refundInbounds, err := FinalizeRefundGrant(db, order.Id, true, now+1, "test")
	if err != nil {
		t.Fatal(err)
	}
	if len(refundInbounds) != 2 {
		t.Fatalf("refund inbounds = %#v", refundInbounds)
	}
	if err := db.First(&savedClient, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	if savedClient.Volume != 100 || savedClient.Up != 30 || savedClient.Down != 20 {
		t.Fatalf("client refund updates wrong: %#v", savedClient)
	}
	gotOrder, _ = GetOrder(db, order.Id)
	if gotOrder.Status != paid.StatusRefunded {
		t.Fatalf("order status after refund = %s", gotOrder.Status)
	}
}

func TestRefundOlderTrafficOrderPreservesCurrentWindow(t *testing.T) {
	db := newPaidDB(t)
	now := int64(1_700_000_000)
	client := model.Client{Name: "stacked", Enable: true, Volume: 5 << 30, Up: 100, Down: 200, TotalUp: 1000, TotalDown: 2000, Inbounds: json.RawMessage(`[]`)}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	tariff := paid.Tariff{Name: "Traffic", Price: 100, Currency: "RUB", AddTrafficBytes: 1 << 30}
	if err := db.Create(&tariff).Error; err != nil {
		t.Fatal(err)
	}
	apply := func(key string) uint {
		order := paid.PaymentOrder{ClientId: client.Id, TariffId: tariff.Id, Provider: string(provider.ProviderStripe), Amount: 100, Currency: "RUB", Status: paid.StatusPending, IdempotencyKey: key}
		if err := db.Create(&order).Error; err != nil {
			t.Fatal(err)
		}
		if result, err := ApplyPaidOrderGrant(db, order.Id, "charge-"+key, nil, now, "test"); err != nil || !result.Applied {
			t.Fatalf("apply %s = %#v, %v", key, result, err)
		}
		return order.Id
	}
	orderA := apply("A")
	if err := db.Model(&model.Client{}).Where("id = ?", client.Id).Updates(map[string]any{"up": 50, "down": 60}).Error; err != nil {
		t.Fatal(err)
	}
	_ = apply("B")
	if err := db.Model(&model.Client{}).Where("id = ?", client.Id).Updates(map[string]any{"up": 30, "down": 40}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := FinalizeRefundGrant(db, orderA, true, now+1, "test"); err != nil {
		t.Fatal(err)
	}
	var got model.Client
	if err := db.First(&got, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	if got.Up != 30 || got.Down != 40 {
		t.Fatalf("current window clobbered: up=%d down=%d", got.Up, got.Down)
	}
	if got.TotalUp != 1050 || got.TotalDown != 2060 || got.Volume != 6<<30 {
		t.Fatalf("relative rollback wrong: %#v", got)
	}
}
