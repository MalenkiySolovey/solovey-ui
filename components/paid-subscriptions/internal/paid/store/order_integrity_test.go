package store

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	paid "github.com/MalenkiySolovey/solovey-component-paid-subscriptions"
	commerce "github.com/MalenkiySolovey/solovey-component-paid-subscriptions/store"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func concurrentPaidDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "payment.db")+"?_busy_timeout=5000&_journal_mode=WAL"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	if err := db.AutoMigrate(&model.Client{}, &model.Changes{}); err != nil {
		t.Fatal(err)
	}
	if err := paid.EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestLegacySnapshotNeverUsesCurrentTariffForGrantOrRefund(t *testing.T) {
	db := newPaidDB(t)
	client := model.Client{Name: "legacy", Volume: 100, Expiry: 1000}
	tariff := paid.Tariff{Name: "edited", Price: 100, AddDays: 90, AddTrafficBytes: 9999}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&tariff).Error; err != nil {
		t.Fatal(err)
	}
	order := paid.PaymentOrder{ClientId: client.Id, TariffId: tariff.Id, Provider: "stars", Amount: 100, Currency: "XTR", Status: paid.StatusPending, IdempotencyKey: "legacy"}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyPaidOrderGrant(db, order.Id, "tg:charge", nil, 1000, "test"); err == nil {
		t.Fatal("legacy grant accepted")
	}
	if err := db.Model(&order).Update("status", paid.StatusPaid).Error; err != nil {
		t.Fatal(err)
	}
	for _, revoke := range []bool{true, false} {
		if _, err := FinalizeRefundGrant(db, order.Id, revoke, 1000, "test"); err == nil {
			t.Fatal("legacy refund accepted")
		}
	}
	if err := paid.EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := paid.EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	got, err := commerce.GetOrder(db, order.Id)
	if err != nil || got.Status != paid.StatusManualReview || got.ReviewReason != "legacy_snapshot_missing" || got.GrantSnapshot {
		t.Fatalf("legacy classification: %#v, %v", got, err)
	}
	var unchanged model.Client
	if err := db.First(&unchanged, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	if unchanged.Volume != 100 || unchanged.Expiry != 1000 {
		t.Fatal("legacy changed client")
	}
}

func TestConcurrentConfirmationsAndFailedCommitGrantOnce(t *testing.T) {
	db := concurrentPaidDB(t)
	client := model.Client{Name: "once", Volume: 10}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	order := paid.PaymentOrder{ClientId: client.Id, Provider: "cryptobot", Amount: 100, Currency: "RUB", Status: paid.StatusPending, IdempotencyKey: "once", GrantSnapshot: true, GrantTraffic: 50, ProviderRef: "41", ProviderPayload: []byte(`{"ref":"41"}`)}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER reject_grant BEFORE UPDATE OF volume ON clients BEGIN SELECT RAISE(FAIL, 'grant commit fixture'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyPaidOrderGrant(db, order.Id, "cryptobot:41", nil, 1000, "test"); err == nil {
		t.Fatal("failed commit accepted")
	}
	got, _ := commerce.GetOrder(db, order.Id)
	if got.Status != paid.StatusPending || got.ProviderChargeID != "" {
		t.Fatal("partial financial state committed")
	}
	if err := db.Exec(`DROP TRIGGER reject_grant`).Error; err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	done := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, err := ApplyPaidOrderGrant(db, order.Id, "cryptobot:41", nil, 1000, "test")
			done <- err
		}()
	}
	close(start)
	success := 0
	for range 2 {
		err := <-done
		if err == nil {
			success++
		} else if !errors.Is(err, commerce.ErrOrderAlreadyFinalized) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatalf("applications=%d", success)
	}
	if err := db.First(&client, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	got, _ = commerce.GetOrder(db, order.Id)
	if client.Volume != 60 || got.ProviderRef != "41" || string(got.ProviderPayload) != `{"ref":"41"}` {
		t.Fatal("grant or original invoice identity changed")
	}
}
