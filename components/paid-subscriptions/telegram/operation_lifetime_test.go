//go:build !minimal

package telegram

import (
	"context"
	"errors"
	"io"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	paid "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid"
	paidprovider "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid/provider"
	paidsettings "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/settings"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

type lifetimeTransport func(*http.Request) (*http.Response, error)

func (f lifetimeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestProviderNetworkIterationsRetainOriginalDatabaseAdmission(t *testing.T) {
	for _, kind := range []string{"payment_poll", "payment_reconcile", "bot_iteration", "invoice_create", "stars_refund"} {
		t.Run(kind, func(t *testing.T) {
			database := openTestDB(t)
			if err := ensureTestSchema(database); err != nil {
				t.Fatal(err)
			}
			unregister := service.RegisterSettingContribution("test-paid-lifetime", service.SettingContribution{Defaults: paidsettings.Defaults(), Internal: paidsettings.InternalKeys(), Encrypted: paidsettings.EncryptedKeys()})
			t.Cleanup(unregister)
			entered, proceed := make(chan struct{}), make(chan struct{})
			var enterOnce, resumeOnce sync.Once
			var requests atomic.Int32
			resume := func() { resumeOnce.Do(func() { close(proceed) }) }
			previousTransport := http.DefaultTransport
			http.DefaultTransport = lifetimeTransport(func(r *http.Request) (*http.Response, error) {
				requests.Add(1)
				enterOnce.Do(func() { close(entered) })
				select {
				case <-proceed:
				case <-r.Context().Done():
					return nil, r.Context().Err()
				}
				body := `{"ok":true,"result":[{"update_id":4}]}`
				if kind == "payment_poll" || kind == "payment_reconcile" {
					body = `{"ok":true,"result":{"items":[{"invoice_id":41,"status":"paid","amount":"1.00","fiat":"RUB","currency_type":"fiat","payload":"network-poll-fixture"}]}}`
				}
				if kind == "invoice_create" {
					body = `{"ok":true,"result":{"invoice_id":42,"pay_url":"https://pay.example/42"}}`
				}
				if kind == "stars_refund" {
					body = `{"ok":true,"result":true}`
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			t.Cleanup(func() { http.DefaultTransport = previousTransport })
			t.Cleanup(resume)
			settings := &service.SettingService{}
			for key, value := range map[string]string{
				paidsettings.EnabledKey: "true", paidsettings.CryptoBotEnabledKey: "true", paidsettings.CryptoBotTokenKey: "qualification-fixture",
				paidsettings.BotTokenKey: "qualification-fixture", paidsettings.BotPollSecondsKey: "1", paidsettings.UpdateOffsetKey: "0",
				paidsettings.TransportModeKey: "proxy", paidsettings.ProxyURLKey: "", paidsettings.ProxyUsernameKey: "", paidsettings.ProxyPasswordKey: "",
				paidsettings.OrderTTLMinutesKey: "30",
			} {
				if err := settings.SetComponentSettingString(key, value); err != nil {
					t.Fatal(err)
				}
			}
			if err := database.Create(&model.Client{Id: 1, Name: "network-lifetime", Inbounds: []byte("[]")}).Error; err != nil {
				t.Fatal(err)
			}
			order := paid.PaymentOrder{ClientId: 1, TariffId: 1, Provider: "cryptobot", Amount: 100, Currency: "RUB", Status: paid.StatusPending, IdempotencyKey: "network-poll-fixture", ProviderPayload: []byte(`{"ref":"41"}`), CreatedAt: time.Now().Unix(), GrantSnapshot: true, GrantAddDays: 1, GrantTraffic: 1024}
			if kind == "payment_poll" {
				order.ProviderRef = "41"
			}
			if kind == "stars_refund" {
				order.Provider = "stars"
				order.Currency = "XTR"
				order.Status = paid.StatusPaid
				order.TelegramUserId = 7
				order.ProviderChargeID = "tg:original"
			}
			tariff := paid.Tariff{Id: 2, Name: "create-lifetime", Enabled: true, Price: 100, Currency: "RUB", AddDays: 1}
			if err := database.Create(&tariff).Error; err != nil {
				t.Fatal(err)
			}
			expiring := paid.PaymentOrder{ClientId: 1, TariffId: 1, Provider: "stars", Amount: 100, Currency: "XTR", Status: paid.StatusPending, IdempotencyKey: "expiry-after-network", ExpiresAt: time.Now().Add(-time.Minute).Unix()}
			if err := database.Create(&order).Error; err != nil {
				t.Fatal(err)
			}
			if err := database.Create(&expiring).Error; err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			bot := newBotWithRuntime(nil)
			defer bot.closeIdleConnections()
			finished := make(chan struct{})
			go func() {
				if kind == "payment_poll" || kind == "payment_reconcile" {
					PollOnce(ctx, nil)
				} else if kind == "invoice_create" {
					if _, _, err := newPaymentCoordinator().CreateOrder(ctx, &model.Client{Id: 1}, &tariff, paidprovider.ProviderCryptoBot, 7); err != nil {
						t.Error(err)
					}
				} else if kind == "stars_refund" {
					if _, err := newPaymentCoordinator().refundOrder(ctx, order.Id, false); err != nil {
						t.Error(err)
					}
				} else {
					bot.pollIteration(ctx, time.Second)
				}
				close(finished)
			}()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("provider network barrier was not reached")
			}
			pool, err := database.DB()
			if err != nil {
				t.Fatal(err)
			}
			if pool.Stats().InUse != 0 {
				t.Fatal("network barrier unexpectedly held SQL resources")
			}
			type maintenanceResult struct {
				owner *dbsqlite.Maintenance
				err   error
			}
			maintained := make(chan maintenanceResult, 1)
			go func() { owner, err := dbsqlite.BeginMaintenance(ctx); maintained <- maintenanceResult{owner, err} }()
			for {
				_, release, err := dbsqlite.AcquireOperation(ctx)
				if errors.Is(err, dbsqlite.ErrMaintenance) {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				release()
				runtime.Gosched()
			}
			select {
			case got := <-maintained:
				if got.owner != nil {
					got.owner.End()
				}
				t.Fatalf("maintenance crossed network-only gap: %v", got.err)
			default:
			}
			PollOnce(ctx, nil)
			bot.pollIteration(ctx, time.Second)
			if _, _, err := newPaymentCoordinator().CreateOrder(ctx, &model.Client{Id: 1}, &tariff, paidprovider.ProviderCryptoBot, 7); !errors.Is(err, dbsqlite.ErrMaintenance) {
				t.Fatal("late create crossed maintenance", err)
			}
			if _, err := newPaymentCoordinator().refundOrder(ctx, order.Id, false); !errors.Is(err, dbsqlite.ErrMaintenance) {
				t.Fatal("late refund crossed maintenance", err)
			}
			if requests.Load() != 1 {
				t.Fatal("late provider operation entered network I/O")
			}
			resume()
			select {
			case <-finished:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			var got maintenanceResult
			select {
			case got = <-maintained:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if got.err != nil {
				t.Fatal(got.err)
			}
			defer got.owner.End()
			if dbsqlite.DB() != database {
				t.Fatal("provider operation changed database generation")
			}
			if kind == "payment_poll" || kind == "payment_reconcile" {
				if err := database.First(&order, order.Id).Error; err != nil || order.Status != paid.StatusPaid {
					t.Fatalf("post-network payment did not finish on original generation: %v", err)
				}
				if err := database.First(&expiring, expiring.Id).Error; err != nil || expiring.Status != paid.StatusExpired {
					t.Fatalf("post-network expiry did not finish on original generation: %v", err)
				}
			} else if kind == "stars_refund" {
				if err := database.First(&order, order.Id).Error; err != nil || order.Status != paid.StatusRefunded {
					t.Fatal("refund did not finish on original generation", err)
				}
			} else if kind == "invoice_create" {
				var created paid.PaymentOrder
				if err := database.Where("tariff_id = ?", tariff.Id).First(&created).Error; err != nil || created.Status != paid.StatusPending || created.ProviderRef != "42" {
					t.Fatal("create did not finish on original generation", err)
				}
			} else {
				if offset, err := bot.setting.GetPaidSubUpdateOffset(); err != nil || offset != 5 {
					t.Fatalf("post-network offset did not finish on original generation: offset=%d err=%v", offset, err)
				}
			}
			got.owner.End()
		})
	}
}
