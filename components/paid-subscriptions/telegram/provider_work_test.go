//go:build !minimal

package telegram

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	paid "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid"
	provider "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid/provider"
	paidsettings "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/settings"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

type workProviderFixture struct {
	poll      func(context.Context, []paid.PaymentOrder) (provider.PollOutcome, error)
	reconcile func(context.Context, []paid.PaymentOrder) ([]provider.Reconciliation, error)
	cancel    func(context.Context, string) (bool, error)
}

func (f workProviderFixture) Poll(ctx context.Context, rows []paid.PaymentOrder) (provider.PollOutcome, error) {
	return f.poll(ctx, rows)
}
func (f workProviderFixture) Reconcile(ctx context.Context, rows []paid.PaymentOrder) ([]provider.Reconciliation, error) {
	if f.reconcile == nil {
		return nil, nil
	}
	return f.reconcile(ctx, rows)
}
func (f workProviderFixture) CancelInvoice(ctx context.Context, ref string) (bool, error) {
	return f.cancel(ctx, ref)
}

func TestPollBoundsFairRotationAndOutageCannotExpirePotentiallyPaidOrders(t *testing.T) {
	db := openTestDB(t)
	if err := ensureTestSchema(db); err != nil {
		t.Fatal(err)
	}
	client := model.Client{Id: 1, Name: "fair", Volume: 100, Inbounds: []byte("[]")}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	var orders []paid.PaymentOrder
	for id := 1; id <= 1205; id++ {
		orders = append(orders, paid.PaymentOrder{ClientId: 1, TariffId: uint(id), Provider: "cryptobot", Amount: 100, Currency: "RUB", Status: paid.StatusPending, ProviderRef: fmt.Sprint(id), IdempotencyKey: fmt.Sprint("fair-", id), GrantSnapshot: true, GrantTraffic: 10, CreatedAt: time.Now().Add(-48 * time.Hour).Unix()})
	}
	if err := db.CreateInBatches(orders, 100).Error; err != nil {
		t.Fatal(err)
	}
	seen := make(map[uint]bool)
	calls := 0
	fake := workProviderFixture{poll: func(ctx context.Context, rows []paid.PaymentOrder) (provider.PollOutcome, error) {
		calls++
		if len(rows) > 100 {
			t.Fatal("unbounded request")
		}
		for _, row := range rows {
			seen[row.Id] = true
		}
		return provider.PollOutcome{}, errors.New("fixture unavailable")
	}}
	runCryptoBotWork(context.Background(), nil, newPaymentCoordinator(), fake, nil)
	if calls != 10 || len(seen) != 1000 {
		t.Fatal("tick not bounded/fair", calls, len(seen))
	}
	var old paid.PaymentOrder
	if err := db.First(&old, 1).Error; err != nil || old.Status != paid.StatusPending {
		t.Fatal("outage expired uncertain invoice")
	}
	calls = 0
	fake.poll = func(ctx context.Context, rows []paid.PaymentOrder) (provider.PollOutcome, error) {
		calls++
		var result provider.PollOutcome
		for _, row := range rows {
			seen[row.Id] = true
			if row.Id == 1205 || row.Id == 1 {
				result.Paid = append(result.Paid, provider.PollResult{OrderID: row.Id, ProviderChargeID: "cryptobot:" + row.ProviderRef})
			}
		}
		return result, nil
	}
	runCryptoBotWork(context.Background(), nil, newPaymentCoordinator(), fake, nil)
	if calls > 10 || len(seen) != 1205 {
		t.Fatal("high IDs starved", calls, len(seen))
	}
	for _, id := range []uint{1, 1205} {
		var row paid.PaymentOrder
		if err := db.First(&row, id).Error; err != nil || row.Status != paid.StatusPaid {
			t.Fatal("late authenticated payment lost", id, err)
		}
	}
	db.First(&client, 1)
	if client.Volume != 120 {
		t.Fatal("late grants not exactly once", client.Volume)
	}
}

func TestCanceledProviderWorkDoesNotAdvanceCursorOrFinancialState(t *testing.T) {
	db := openTestDB(t)
	if err := ensureTestSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Client{Id: 1, Name: "cancelled", Volume: 100}).Error; err != nil {
		t.Fatal(err)
	}
	order := paid.PaymentOrder{ClientId: 1, TariffId: 1, Provider: "cryptobot", Amount: 100, Currency: "RUB", Status: paid.StatusPending, ProviderRef: "41", IdempotencyKey: "cancelled-work", GrantSnapshot: true}
	db.Create(&order)
	ctx, cancel := context.WithCancel(context.Background())
	fake := workProviderFixture{poll: func(context.Context, []paid.PaymentOrder) (provider.PollOutcome, error) {
		cancel()
		return provider.PollOutcome{Paid: []provider.PollResult{{OrderID: order.Id, ProviderChargeID: "cryptobot:41"}}}, nil
	}}
	runCryptoBotWork(ctx, nil, newPaymentCoordinator(), fake, nil)
	var cursors int64
	db.Model(&paid.ProviderCursor{}).Count(&cursors)
	db.First(&order, order.Id)
	if cursors != 0 || order.Status != paid.StatusPending {
		t.Fatal("canceled work marked complete")
	}
}

func TestCancellationFailureRotatesDurablyWithoutCompletingWork(t *testing.T) {
	db := openTestDB(t)
	if err := ensureTestSchema(db); err != nil {
		t.Fatal(err)
	}
	var queue []paid.InvoiceCancellation
	for id := 1; id <= 205; id++ {
		queue = append(queue, paid.InvoiceCancellation{OrderID: uint(id), Ref: fmt.Sprint(5000 + id)})
	}
	if err := db.CreateInBatches(queue, 100).Error; err != nil {
		t.Fatal(err)
	}
	requests := 0
	fake := workProviderFixture{poll: func(context.Context, []paid.PaymentOrder) (provider.PollOutcome, error) {
		return provider.PollOutcome{}, nil
	}, cancel: func(context.Context, string) (bool, error) {
		requests++
		return false, errors.New("fixture unavailable")
	}}
	runCryptoBotWork(context.Background(), nil, newPaymentCoordinator(), fake, fake)
	if requests != 100 {
		t.Fatal("cancellation tick unbounded", requests)
	}
	var complete int64
	db.Model(&paid.InvoiceCancellation{}).Where("completed = ?", true).Count(&complete)
	if complete != 0 {
		t.Fatal("failed cancellation discarded")
	}
	fake.cancel = func(context.Context, string) (bool, error) { return true, nil }
	runCryptoBotWork(context.Background(), nil, newPaymentCoordinator(), fake, fake)
	db.Model(&paid.InvoiceCancellation{}).Where("completed = ?", true).Count(&complete)
	if complete != 100 {
		t.Fatal("higher cancellation work starved", complete)
	}
	runCryptoBotWork(context.Background(), nil, newPaymentCoordinator(), fake, fake)
	runCryptoBotWork(context.Background(), nil, newPaymentCoordinator(), fake, fake)
	db.Model(&paid.InvoiceCancellation{}).Where("completed = ?", true).Count(&complete)
	if complete != 205 {
		t.Fatal("cancellation wrap/retry failed", complete)
	}
}

func TestFailedPaymentNotificationCannotUndoOrRepeatCommittedGrant(t *testing.T) {
	db := openTestDB(t)
	if err := ensureTestSchema(db); err != nil {
		t.Fatal(err)
	}
	unregister := service.RegisterSettingContribution("test-payment-delivery", service.SettingContribution{Defaults: paidsettings.Defaults(), Internal: paidsettings.InternalKeys(), Encrypted: paidsettings.EncryptedKeys()})
	t.Cleanup(unregister)
	if err := (&service.SettingService{}).SetComponentSettingString(paidsettings.BotTokenKey, "fixture"); err != nil {
		t.Fatal(err)
	}
	client := model.Client{Id: 1, Name: "delivery", Volume: 100, Inbounds: []byte("[]")}
	db.Create(&client)
	order := paid.PaymentOrder{ClientId: 1, TariffId: 1, Provider: "cryptobot", Amount: 100, Currency: "RUB", Status: paid.StatusPending, ProviderRef: "41", IdempotencyKey: "delivery", TelegramUserId: 7, GrantSnapshot: true, GrantTraffic: 10}
	db.Create(&order)
	previous := http.DefaultTransport
	requests := 0
	http.DefaultTransport = lifetimeTransport(func(*http.Request) (*http.Response, error) {
		requests++
		var saved paid.PaymentOrder
		if err := db.First(&saved, order.Id).Error; err != nil || saved.Status != paid.StatusPaid {
			t.Fatal("delivery preceded commit")
		}
		return nil, errors.New("fixture delivery failure")
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	fake := workProviderFixture{poll: func(context.Context, []paid.PaymentOrder) (provider.PollOutcome, error) {
		return provider.PollOutcome{Paid: []provider.PollResult{{OrderID: order.Id, ProviderChargeID: "cryptobot:41"}}}, nil
	}}
	for range 2 {
		runCryptoBotWork(context.Background(), nil, newPaymentCoordinator(), fake, nil)
	}
	db.First(&client, 1)
	db.First(&order, order.Id)
	if requests != 1 || client.Volume != 110 || order.Status != paid.StatusPaid {
		t.Fatal("delivery failure changed financial commit", requests, client.Volume, order.Status)
	}
}
