//go:build !minimal

package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	paid "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid"
	paidprovider "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid/provider"
	paidstore "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid/store"
	paidsettings "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/settings"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

func TestConcurrentCreateRetriesDuringProviderWaitReuseOneInvoice(t *testing.T) {
	db := openTestDB(t)
	if err := ensureTestSchema(db); err != nil {
		t.Fatal(err)
	}
	unregister := service.RegisterSettingContribution("test-paid-create-race", service.SettingContribution{Defaults: paidsettings.Defaults(), Internal: paidsettings.InternalKeys(), Encrypted: paidsettings.EncryptedKeys()})
	t.Cleanup(unregister)
	settings := &service.SettingService{}
	for key, value := range map[string]string{paidsettings.CryptoBotEnabledKey: "true", paidsettings.CryptoBotTokenKey: "fixture", paidsettings.OrderTTLMinutesKey: "30", paidsettings.TransportModeKey: "proxy"} {
		if err := settings.SetComponentSettingString(key, value); err != nil {
			t.Fatal(err)
		}
	}
	client := model.Client{Name: "create-race", Inbounds: json.RawMessage(`[]`)}
	tariff := paid.Tariff{Name: "create-race", Price: 101, Currency: "RUB", AddDays: 2, Enabled: true}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&tariff).Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	entered, proceed := make(chan struct{}), make(chan struct{})
	var enteredOnce, proceedOnce sync.Once
	defer proceedOnce.Do(func() { close(proceed) })
	var requests atomic.Int32
	previous := http.DefaultTransport
	http.DefaultTransport = lifetimeTransport(func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		enteredOnce.Do(func() { close(entered) })
		select {
		case <-proceed:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":{"invoice_id":41,"pay_url":"https://pay.example/41"}}`)), Request: r}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	type outcome struct {
		order   *paid.PaymentOrder
		invoice *paidprovider.Invoice
		err     error
	}
	first := make(chan outcome, 1)
	go func() {
		order, invoice, err := newPaymentCoordinator().CreateOrder(ctx, &client, &tariff, paidprovider.ProviderCryptoBot, 7)
		first <- outcome{order, invoice, err}
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("invoice barrier not reached")
	}
	start, retries := make(chan struct{}), make(chan outcome, 8)
	var ready sync.WaitGroup
	ready.Add(8)
	for range 8 {
		go func() {
			ready.Done()
			<-start
			order, invoice, err := newPaymentCoordinator().CreateOrder(ctx, &client, &tariff, paidprovider.ProviderCryptoBot, 7)
			retries <- outcome{order, invoice, err}
		}()
	}
	ready.Wait()
	close(start)
	var id uint
	for range 8 {
		select {
		case got := <-retries:
			if got.err == nil || got.invoice != nil || got.order == nil {
				t.Fatal("uncertain remote create did not fail closed", got.err)
			}
			if id == 0 {
				id = got.order.Id
			}
			if got.order.Id != id {
				t.Fatal("retry created a different local intent")
			}
		case <-ctx.Done():
			t.Fatal("retry reached a second blocked provider operation")
		}
	}
	if requests.Load() != 1 {
		t.Fatal("duplicate non-idempotent create", requests.Load())
	}
	proceedOnce.Do(func() { close(proceed) })
	select {
	case got := <-first:
		if got.err != nil || got.order == nil || got.order.Id != id || got.invoice == nil || got.invoice.ProviderRef != "41" {
			t.Fatal("original creation did not persist", got.err)
		}
	case <-ctx.Done():
		t.Fatal("original creation did not finish")
	}
	order, invoice, err := newPaymentCoordinator().CreateOrder(ctx, &client, &tariff, paidprovider.ProviderCryptoBot, 7)
	if err != nil || order.Id != id || invoice.ProviderRef != "41" || requests.Load() != 1 {
		t.Fatal("persisted invoice not reused", err)
	}
	var count int64
	if err := db.Model(&paid.PaymentOrder{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatal("duplicate durable intent", count, err)
	}
}

func TestRemoteInvoicePersistenceFailureReusesOriginalIntent(t *testing.T) {
	db := openTestDB(t)
	if err := ensureTestSchema(db); err != nil {
		t.Fatal(err)
	}
	unregister := service.RegisterSettingContribution("test-paid-intent", service.SettingContribution{Defaults: paidsettings.Defaults(), Internal: paidsettings.InternalKeys(), Encrypted: paidsettings.EncryptedKeys()})
	t.Cleanup(unregister)
	settings := &service.SettingService{}
	for key, value := range map[string]string{paidsettings.CryptoBotEnabledKey: "true", paidsettings.CryptoBotTokenKey: "fixture", paidsettings.OrderTTLMinutesKey: "30", paidsettings.TransportModeKey: "proxy"} {
		if err := settings.SetComponentSettingString(key, value); err != nil {
			t.Fatal(err)
		}
	}
	client := model.Client{Name: "intent", Inbounds: json.RawMessage(`[]`)}
	tariff := paid.Tariff{Name: "authoritative", Price: 101, Currency: "RUB", AddDays: 2, Enabled: true}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&tariff).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER reject_ref BEFORE UPDATE OF provider_ref ON payment_orders BEGIN SELECT RAISE(FAIL,'reference fixture'); END`).Error; err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	var remotePayload string
	previous := http.DefaultTransport
	http.DefaultTransport = lifetimeTransport(func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		if r.Method == "GET" {
			data, _ := json.Marshal(map[string]any{"ok": true, "result": map[string]any{"items": []map[string]any{{"invoice_id": 41, "status": "paid", "amount": "1.01", "fiat": "RUB", "currency_type": "fiat", "payload": remotePayload}}}})
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(data))), Request: r}, nil
		}
		var body struct {
			Amount  string `json:"amount"`
			Payload string `json:"payload"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Amount != "1.01" {
			t.Fatal("client-controlled amount reached provider")
		}
		remotePayload = body.Payload
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":{"invoice_id":41,"pay_url":"https://pay.example/41"}}`)), Request: r}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	forged := tariff
	forged.Price = 1
	forged.AddDays = 99
	p := newPaymentCoordinator()
	order, inv, err := p.CreateOrder(context.Background(), &client, &forged, paidprovider.ProviderCryptoBot, 7)
	if err == nil || inv != nil || order == nil {
		t.Fatal("unpersisted URL was delivered")
	}
	if err := db.Exec(`DROP TRIGGER reject_ref`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&order).Update("expires_at", 1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&tariff).Error; err != nil {
		t.Fatal(err)
	}
	second, inv, err := p.CreateOrder(context.Background(), &client, &forged, paidprovider.ProviderCryptoBot, 7)
	if err == nil || inv != nil || second.Id != order.Id || requests.Load() != 1 {
		t.Fatal("retry created a second remote invoice")
	}
	if err := db.First(&order, order.Id).Error; err != nil {
		t.Fatal(err)
	}
	if order.Status != paid.StatusRecoverable || order.GrantAddDays != 2 || order.Amount != 101 {
		t.Fatal("immutable intent lost")
	}
	reconciler := p.providerByKind(paidprovider.ProviderCryptoBot).(paidprovider.ReconciliationProvider)
	recovered, err := reconciler.Reconcile(context.Background(), []paid.PaymentOrder{*order})
	if err != nil || len(recovered) != 1 {
		t.Fatal("remote success could not be reconciled", err)
	}
	result, err := paidstore.RecoverProviderOrder(db, recovered[0], nowUnix(), cryptoBotPollGraceSeconds)
	if err != nil || !result.Applied {
		t.Fatal("paid recovery did not commit", result, err)
	}
	if err := db.First(&order, order.Id).Error; err != nil || order.Status != paid.StatusPaid || order.ProviderRef != "41" || order.GrantAddDays != 2 {
		t.Fatal("recovery fabricated current tariff", err)
	}
}
