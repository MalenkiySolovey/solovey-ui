package store

import (
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	paid "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/paid"
	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/paid/provider"
)

func TestOrderStoreHistoryAndExpiry(t *testing.T) {
	db := newPaidDB(t)
	now := int64(10_000)
	client := model.Client{Name: "alice"}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	tariff := paid.Tariff{Name: "Month", Price: 100, Currency: "RUB"}
	if err := db.Create(&tariff).Error; err != nil {
		t.Fatal(err)
	}

	stale := NewPendingOrder(&client, &tariff, provider.ProviderStripe, 100, "RUB", 7, "stale", now-100, 1)
	fresh := NewPendingOrder(&client, &tariff, provider.ProviderStripe, 100, "RUB", 7, "fresh", now, 30)
	crypto := NewPendingOrder(&client, &tariff, provider.ProviderCryptoBot, 100, "RUB", 7, "crypto", now-100, 1)
	if err := db.Create([]*paid.PaymentOrder{stale, fresh, crypto}).Error; err != nil {
		t.Fatal(err)
	}
	if err := ExpireStaleOrders(db, now); err != nil {
		t.Fatal(err)
	}

	gotStale, _ := GetOrder(db, stale.Id)
	gotFresh, _ := GetOrder(db, fresh.Id)
	gotCrypto, _ := GetOrder(db, crypto.Id)
	if gotStale.Status != paid.StatusExpired || gotFresh.Status != paid.StatusPending || gotCrypto.Status != paid.StatusPending {
		t.Fatalf("unexpected statuses: stale=%s fresh=%s crypto=%s", gotStale.Status, gotFresh.Status, gotCrypto.Status)
	}

	orders, err := OrdersForTelegramUser(db, 7, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 3 {
		t.Fatalf("history len=%d", len(orders))
	}

	MarkOrderFailed(db, fresh.Id)
	gotFresh, _ = GetOrder(db, fresh.Id)
	if gotFresh.Status != paid.StatusFailed {
		t.Fatalf("MarkOrderFailed status=%s", gotFresh.Status)
	}
}

func TestSaveInvoiceResultStoresExternalURLAndProviderRef(t *testing.T) {
	db := newPaidDB(t)
	order := paid.PaymentOrder{Status: paid.StatusPending, IdempotencyKey: "k"}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	if err := SaveInvoiceResult(db, order.Id, &provider.Invoice{PayURL: "https://pay.example", ProviderRef: "inv-1"}); err != nil {
		t.Fatal(err)
	}
	got, err := GetOrder(db, order.Id)
	if err != nil {
		t.Fatal(err)
	}
	if got.ExternalURL != "https://pay.example" || provider.ExtractProviderRef(got.ProviderPayload) != "inv-1" {
		t.Fatalf("invoice fields not stored: %#v", got)
	}
}
