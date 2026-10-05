//go:build !minimal

package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	paid "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid"
	paidprovider "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid/provider"
	paidsettings "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/settings"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

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
	previous := http.DefaultTransport
	http.DefaultTransport = lifetimeTransport(func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		var body struct {
			Amount string `json:"amount"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Amount != "1.01" {
			t.Fatal("client-controlled amount reached provider")
		}
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
}
