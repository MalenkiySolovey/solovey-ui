//go:build !minimal

package paidsubscriptions

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	paid "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid"
	"github.com/MalenkiySolovey/solovey-ui/database/backup"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPaidOrderBackupStagedRestoreAndDisabledLifecyclePreserveIntent(t *testing.T) {
	initPaidComponentTestDB(t)
	installedPath := filepath.Join(t.TempDir(), "installed.json")
	t.Setenv(installstate.InstalledFileEnv, installedPath)
	if err := installstate.Store(installedPath, installstate.Metadata{Version: 1, Components: []installstate.InstalledComponent{{ID: id, Delivery: componentManifest.Delivery, Installed: true}}}); err != nil {
		t.Fatal(err)
	}
	c := &component{}
	if err := c.Migrate(context.Background(), lifecycle.Context{}); err != nil {
		t.Fatal(err)
	}
	order := paid.PaymentOrder{ClientId: 1, TariffId: 1, Provider: "cryptobot", Amount: 101, Currency: "RUB", Status: paid.StatusRecoverable, IdempotencyKey: "backup-intent", GrantSnapshot: true, GrantAddDays: 2, ProviderRef: "41", ReviewReason: "invoice_creation_uncertain"}
	if err := dbsqlite.DB().Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	queue := paid.InvoiceCancellation{OrderID: order.Id, Ref: "42"}
	if err := dbsqlite.DB().Create(&queue).Error; err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Create(&paid.ProviderCursor{Kind: "cancel", AfterID: queue.Id}).Error; err != nil {
		t.Fatal(err)
	}
	refund := paid.PaymentOrder{ClientId: 2, TariffId: 1, Provider: "stars", Amount: 10, Currency: "XTR", Status: paid.StatusRefundPending, IdempotencyKey: "backup-refund", GrantSnapshot: true, RefundRevoke: true, RefundClaim: "prior-process", RefundClaimUntil: 100, ProviderChargeID: "tg:original"}
	if err := dbsqlite.DB().Create(&refund).Error; err != nil {
		t.Fatal(err)
	}
	if err := c.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	path, cleanup, err := backup.PrepareExport("")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	staged, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := staged.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for range 2 {
		if err := c.MigrateStaged(context.Background(), staged); err != nil {
			t.Fatal(err)
		}
	}
	var got paid.PaymentOrder
	if err := staged.First(&got, order.Id).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != order.Status || got.ProviderRef != "41" || got.IdempotencyKey != order.IdempotencyKey || got.GrantAddDays != 2 || !got.GrantSnapshot {
		t.Fatal("backup/staged migration changed financial authority")
	}
	var restoredQueue paid.InvoiceCancellation
	var restoredCursor paid.ProviderCursor
	var restoredRefund paid.PaymentOrder
	if err := staged.First(&restoredQueue, queue.Id).Error; err != nil || restoredQueue.Ref != "42" || restoredQueue.Completed {
		t.Fatal("cancellation work lost", err)
	}
	if err := staged.Where("kind = 'cancel'").First(&restoredCursor).Error; err != nil || restoredCursor.AfterID != queue.Id {
		t.Fatal("cursor lost", err)
	}
	if err := staged.First(&restoredRefund, refund.Id).Error; err != nil || restoredRefund.Status != paid.StatusRefundPending || !restoredRefund.RefundRevoke || restoredRefund.ProviderChargeID != "tg:original" {
		t.Fatal("refund intent lost", err)
	}
}
