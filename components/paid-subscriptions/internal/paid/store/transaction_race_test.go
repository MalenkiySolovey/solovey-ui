package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	paid "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid"
	"github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid/provider"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/gorm"
)

func TestRefundRacingUncommittedGrantWaitsForCompleteApplication(t *testing.T) {
	db := concurrentPaidDB(t)
	client := model.Client{Name: "grant-refund", Volume: 100, Inbounds: []byte("[]")}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	order := paid.PaymentOrder{ClientId: client.Id, Provider: "stars", Amount: 10, Currency: "XTR", TelegramUserId: 7, Status: paid.StatusPending, IdempotencyKey: "grant-refund", GrantSnapshot: true, GrantTraffic: 50}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	// A refund arriving before application cannot claim or mutate this order.
	if _, err := ClaimStarsRefund(db, order.Id, true, "too-early", 1000); !errors.Is(err, ErrRefundInProgress) {
		t.Fatal("unapplied refund accepted", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	grantWritten, resume, refundStarted := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var grantOnce, refundOnce, resumeOnce sync.Once
	defer resumeOnce.Do(func() { close(resume) })
	if err := db.Callback().Update().After("gorm:update").Register("fixture:grant_wait", func(tx *gorm.DB) {
		updates, ok := tx.Statement.Dest.(map[string]any)
		if ok && tx.Statement.Table == "payment_orders" && updates["status"] == paid.StatusPaid && tx.Error == nil {
			grantOnce.Do(func() { close(grantWritten) })
			select {
			case <-resume:
			case <-ctx.Done():
				tx.AddError(ctx.Err())
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Update().Before("gorm:update").Register("fixture:refund_started", func(tx *gorm.DB) {
		updates, ok := tx.Statement.Dest.(map[string]any)
		if ok && tx.Statement.Table == "payment_orders" && updates["status"] == paid.StatusRefundPending {
			refundOnce.Do(func() { close(refundStarted) })
		}
	}); err != nil {
		t.Fatal(err)
	}
	grantDone, refundDone := make(chan error, 1), make(chan error, 1)
	go func() {
		_, err := ApplyPaidOrderGrant(db.WithContext(ctx), order.Id, "tg:charge", nil, 1000, "test")
		grantDone <- err
	}()
	select {
	case <-grantWritten:
	case <-ctx.Done():
		t.Fatal("grant barrier not reached")
	}
	go func() {
		_, err := ClaimStarsRefund(db.WithContext(ctx), order.Id, true, "refund-claim", 1000)
		refundDone <- err
	}()
	select {
	case <-refundStarted:
	case <-ctx.Done():
		t.Fatal("refund did not race grant")
	}
	// Other connections still see the complete pre-grant state, never the
	// intermediate paid flag or a refundable charge without its client grant.
	before, err := GetOrder(db.WithContext(ctx), order.Id)
	if err != nil || before.Status != paid.StatusPending || before.ProviderChargeID != "" {
		t.Fatal("uncommitted payment leaked", before, err)
	}
	resumeOnce.Do(func() { close(resume) })
	for _, done := range []chan error{grantDone, refundDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("transaction race did not finish")
		}
	}
	if _, err := FinalizeClaimedRefundGrant(db, order.Id, "refund-claim", 1000, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyPaidOrderGrant(db, order.Id, "tg:charge", nil, 1000, "test"); !errors.Is(err, ErrOrderAlreadyFinalized) {
		t.Fatal("grant repeated", err)
	}
	if _, err := FinalizeClaimedRefundGrant(db, order.Id, "refund-claim", 1000, "test"); !errors.Is(err, ErrOrderAlreadyFinalized) {
		t.Fatal("refund repeated", err)
	}
	got, err := GetOrder(db, order.Id)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.First(&client, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	var renewals, refunds int64
	if err := db.Model(&model.Changes{}).Where("action = 'renew'").Count(&renewals).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Changes{}).Where("action = 'refund'").Count(&refunds).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != paid.StatusRefunded || got.ProviderChargeID != "tg:charge" || client.Volume != 100 || renewals != 1 || refunds != 1 {
		t.Fatal("financial race produced partial or repeated state", got.Status, client.Volume, renewals, refunds)
	}
}

func TestPollConfirmationAndRecoveryRaceApplyOnce(t *testing.T) {
	db := concurrentPaidDB(t)
	client := model.Client{Name: "poll-recovery", Volume: 100}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	order := paid.PaymentOrder{ClientId: client.Id, Provider: "cryptobot", Amount: 100, Currency: "RUB", Status: paid.StatusPending, IdempotencyKey: "poll-recovery", GrantSnapshot: true, GrantTraffic: 50}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	type outcome struct {
		applied bool
		err     error
	}
	done := make(chan outcome, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	go func() {
		ready.Done()
		<-start
		got, err := ApplyPaidOrderGrant(db, order.Id, "cryptobot:41", nil, 1000, "poll")
		done <- outcome{got.Applied && err == nil, err}
	}()
	go func() {
		ready.Done()
		<-start
		got, err := RecoverProviderOrder(db, provider.Reconciliation{OrderID: order.Id, Candidates: []provider.RecoveredInvoice{{Ref: "41", State: provider.InvoicePaid, Verified: true}}}, 1000, 86400)
		done <- outcome{got.Applied && err == nil, err}
	}()
	ready.Wait()
	close(start)
	applications := 0
	for range 2 {
		got := <-done
		if got.err != nil && !errors.Is(got.err, ErrOrderAlreadyFinalized) {
			t.Fatal(got.err)
		}
		if got.applied {
			applications++
		}
	}
	if err := db.First(&client, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	got, err := GetOrder(db, order.Id)
	if err != nil || applications != 1 || got.Status != paid.StatusPaid || got.ProviderChargeID != "cryptobot:41" || client.Volume != 150 {
		t.Fatal("poll/recovery double or partial grant", applications, got, client.Volume, err)
	}
}
