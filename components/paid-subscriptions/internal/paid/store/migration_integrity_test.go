package store

import (
	"fmt"
	"testing"

	paid "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid"
)

func TestExistingSchemaUpgradePreservesFinancialEvidence(t *testing.T) {
	db := newPaidDB(t)
	for _, index := range []string{"idx_payment_orders_ref", "idx_payment_orders_active_purchase"} {
		if err := db.Exec("DROP INDEX IF EXISTS " + index).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, column := range []string{"provider_ref", "review_reason", "legacy_resolved"} {
		if err := db.Migrator().DropColumn(&paid.PaymentOrder{}, column); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec(`INSERT INTO payment_orders (client_id,tariff_id,telegram_user_id,provider,amount,currency,status,idempotency_key,provider_payload,grant_snapshot,grant_add_days)
		VALUES (1,1,7,'cryptobot',100,'RUB','pending','legacy','{"ref":"41"}',0,0),
		(2,1,8,'stars',100,'XTR','paid','paid-legacy',NULL,0,0),
		(3,1,9,'stars',100,'XTR','refunded','refunded-legacy',NULL,0,0),
		(4,1,10,'cryptobot',100,'RUB','pending','snapshot','{"ref":"42"}',1,30)`).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := paid.EnsureSchema(db); err != nil {
			t.Fatal(err)
		}
	}
	var rows []paid.PaymentOrder
	if err := db.Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 || rows[0].Status != paid.StatusRecoverable || rows[0].ProviderRef != "41" || string(rows[0].ProviderPayload) != `{"ref":"41"}` || rows[0].GrantSnapshot {
		t.Fatalf("legacy evidence: %#v", rows)
	}
	if rows[1].Status != paid.StatusManualReview || rows[2].Status != paid.StatusRefunded || rows[3].Status != paid.StatusPending || rows[3].GrantAddDays != 30 {
		t.Fatal("migration changed proven or terminal history")
	}
	if err := db.Model(&rows[0]).Updates(map[string]any{"status": paid.StatusFailed, "legacy_resolved": true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := paid.EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	got, _ := GetOrder(db, rows[0].Id)
	if got.Status != paid.StatusFailed {
		t.Fatal("migration resurrected resolved legacy")
	}
}

func TestLegacyDuplicateInvoiceReferencesRemainReviewEvidence(t *testing.T) {
	db := newPaidDB(t)
	if err := db.Exec(`DROP INDEX IF EXISTS idx_payment_orders_ref`).Error; err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 2; i++ {
		order := paid.PaymentOrder{ClientId: uint(i), TariffId: 1, Provider: "cryptobot", Amount: 100, Currency: "RUB", Status: paid.StatusPending, IdempotencyKey: fmt.Sprint("duplicate-", i), ProviderPayload: []byte(`{"ref":"41"}`)}
		if err := db.Create(&order).Error; err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := paid.EnsureSchema(db); err != nil {
			t.Fatal(err)
		}
	}
	var rows []paid.PaymentOrder
	if err := db.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Status != paid.StatusManualReview || row.ReviewReason != "provider_identity_conflict" || row.ProviderRef != "" || string(row.ProviderPayload) != `{"ref":"41"}` {
			t.Fatal("duplicate identity automatically resolved")
		}
	}
}
