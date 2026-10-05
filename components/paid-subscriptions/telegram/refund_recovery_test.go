//go:build !minimal

package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	integrationtelegram "github.com/MalenkiySolovey/solovey-ui/componentkit/telegram"
	paid "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid"
	paidstore "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid/store"
	paidsettings "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/settings"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

func TestStarsRefundRemoteSuccessLocalFailureRetriesSameIntentOnce(t *testing.T) {
	db := openTestDB(t)
	if err := ensureTestSchema(db); err != nil {
		t.Fatal(err)
	}
	unregister := service.RegisterSettingContribution("test-refund-recovery", service.SettingContribution{Defaults: paidsettings.Defaults(), Internal: paidsettings.InternalKeys(), Encrypted: paidsettings.EncryptedKeys()})
	t.Cleanup(unregister)
	settings := &service.SettingService{}
	for key, value := range map[string]string{paidsettings.BotTokenKey: "fixture", paidsettings.TransportModeKey: "proxy", paidsettings.BotPollSecondsKey: "1"} {
		if err := settings.SetComponentSettingString(key, value); err != nil {
			t.Fatal(err)
		}
	}
	client := model.Client{Id: 1, Name: "refund", Volume: 200, Inbounds: []byte("[]")}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	order := paid.PaymentOrder{ClientId: 1, TariffId: 1, Provider: "stars", Amount: 10, Currency: "XTR", Status: paid.StatusPaid, TelegramUserId: 7, ProviderChargeID: "tg:original", IdempotencyKey: "refund", GrantSnapshot: true, GrantTraffic: 100}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_refund BEFORE UPDATE ON clients BEGIN SELECT RAISE(ABORT,'fixture local refund failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	entered, proceed := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var requests atomic.Int32
	previous := http.DefaultTransport
	http.DefaultTransport = lifetimeTransport(func(r *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(r.URL.Path, "/refundStarPayment") {
			t.Fatal("unexpected provider operation")
		}
		var body struct {
			Charge string `json:"telegram_payment_charge_id"`
			User   int64  `json:"user_id"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Charge != "original" || body.User != 7 {
			t.Fatal("wrong refund identity")
		}
		count := requests.Add(1)
		response := `{"ok":true,"result":true}`
		if count == 1 {
			once.Do(func() { close(entered) })
			select {
			case <-proceed:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
		} else {
			response = `{"ok":false,"error_code":400,"description":"Bad Request: CHARGE_ALREADY_REFUNDED"}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response)), Request: r}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() { _, err := newPaymentCoordinator().refundOrder(ctx, order.Id, true); finished <- err }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("network barrier not reached")
	}
	if _, err := newPaymentCoordinator().refundOrder(ctx, order.Id, false); !errors.Is(err, paidstore.ErrRefundInProgress) {
		t.Fatal("second refund reached provider", err)
	}
	if requests.Load() != 1 {
		t.Fatal("duplicate concurrent provider refund")
	}
	close(proceed)
	if err := <-finished; err == nil {
		t.Fatal("local commit failure accepted")
	}
	if err := db.First(&order, order.Id).Error; err != nil || order.Status != paid.StatusRefundPending || !order.RefundRevoke || order.RefundClaim != "" {
		t.Fatal("intent not recoverable", err)
	}
	if err := db.Exec(`DROP TRIGGER fail_refund`).Error; err != nil {
		t.Fatal(err)
	}
	if status, err := newPaymentCoordinator().refundOrder(ctx, order.Id, false); err != nil || status != "refunded" {
		t.Fatal("equivalent remote success did not recover", status, err)
	}
	db.First(&client, 1)
	db.First(&order, order.Id)
	if client.Volume != 100 || order.Status != paid.StatusRefunded || requests.Load() != 2 {
		t.Fatal("refund intent/reversal wrong", client.Volume, order.Status, requests.Load())
	}
	if _, err := newPaymentCoordinator().refundOrder(ctx, order.Id, true); !errors.Is(err, errRefundNotApplicable) || requests.Load() != 2 {
		t.Fatal("terminal refund repeated")
	}
}

func TestAlreadyRefundedRequiresExactStructuredFinancialOutcome(t *testing.T) {
	for _, description := range []string{"NOT_ALREADY_REFUNDED", "CHARGE_ALREADY_REFUNDED_extra", "network ALREADY_REFUNDED"} {
		if isAlreadyRefunded(&integrationtelegram.APIError{Code: 400, Description: description}) {
			t.Fatal("unrelated error accepted as money returned")
		}
	}
	if isAlreadyRefunded(&integrationtelegram.APIError{Code: 500, Description: "CHARGE_ALREADY_REFUNDED"}) {
		t.Fatal("nonfinancial status accepted")
	}
}
