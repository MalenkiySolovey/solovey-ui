package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	paid "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/paid"
)

func TestRenderExternalURLReplacesSafeServerSidePlaceholders(t *testing.T) {
	order := &paid.PaymentOrder{Id: 10, Amount: 12345, Currency: "RUB", IdempotencyKey: "idem"}
	tariff := &paid.Tariff{Id: 20}
	client := &model.Client{Id: 30}

	got := RenderExternalURL("https://pay.example/o/{orderId}/c/{clientId}/t/{tariffId}?a={amount}&c={currency}&k={key}", order, tariff, client)
	for _, want := range []string{"/o/10/", "/c/30/", "/t/20", "a=12345", "c=RUB", "k=idem"} {
		if !strings.Contains(got, want) {
			t.Fatalf("RenderExternalURL missing %q in %q", want, got)
		}
	}
}

func TestTelegramProviderInvoiceShape(t *testing.T) {
	provider := NewTelegramProvider(ProviderStars, "")
	invoice, err := provider.CreateInvoice(context.Background(), &paid.PaymentOrder{IdempotencyKey: "payload"}, &paid.Tariff{Name: "Month", StarsAmount: 50}, &model.Client{})
	if err != nil {
		t.Fatal(err)
	}
	if invoice.Method != InvoiceTelegramNative || invoice.Currency != "XTR" || invoice.Payload != "payload" {
		t.Fatalf("unexpected invoice: %#v", invoice)
	}
	if len(invoice.Prices) != 1 || invoice.Prices[0].Amount != 50 {
		t.Fatalf("unexpected invoice prices: %#v", invoice.Prices)
	}
}
