package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	paid "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid"
)

type contractTransport func(*http.Request) (*http.Response, error)

func (f contractTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func contractProvider(fn contractTransport) *cryptoBotProvider {
	return &cryptoBotProvider{token: "qualification-sentinel", newHTTPClient: func(timeout time.Duration) (*http.Client, error) {
		return &http.Client{Transport: fn}, nil
	}}
}
func contractResponse(r *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}
}
func contractOrder() paid.PaymentOrder {
	return paid.PaymentOrder{Id: 7, Provider: "cryptobot", ProviderRef: "41", IdempotencyKey: "immutable-key", Amount: 12345, Currency: "RUB", GrantSnapshot: true}
}
func contractInvoice() cryptoInvoice {
	return cryptoInvoice{InvoiceID: "41", Status: "paid", Payload: "immutable-key", Amount: "123.45", Fiat: "RUB", CurrencyType: "fiat"}
}

func TestCryptoBotCreationUsesExactImmutableMinorUnits(t *testing.T) {
	for _, amount := range []int64{1, 101, 9007199254740993, math.MaxInt64} {
		t.Run(fmt.Sprint(amount), func(t *testing.T) {
			order := contractOrder()
			order.Amount = amount
			p := contractProvider(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "pay.crypt.bot" || r.URL.RawQuery != "" || r.Header.Get("Crypto-Pay-API-Token") != "qualification-sentinel" {
					t.Fatal("provider identity/header changed")
				}
				var request map[string]any
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Fatal(err)
				}
				if request["amount"] != fmt.Sprintf("%d.%02d", amount/100, amount%100) || request["payload"] != order.IdempotencyKey || request["fiat"] != order.Currency {
					t.Fatal("request did not use immutable exact purchase")
				}
				if _, ok := r.Context().Deadline(); !ok {
					t.Fatal("no finite request deadline")
				}
				return contractResponse(r, 200, `{"ok":true,"result":{"invoice_id":41,"bot_invoice_url":"https://t.me/CryptoBot?start=41","pay_url":"https://pay.example/41"}}`), nil
			})
			invoice, err := p.CreateInvoice(context.Background(), &order, &paid.Tariff{Price: 1, Name: "purchase"}, nil)
			if err != nil || invoice.ProviderRef != "41" || !strings.HasPrefix(invoice.PayURL, "https://t.me/") {
				t.Fatalf("invoice=%+v err=%v", invoice, err)
			}
		})
	}
	if value, err := parseDecimalMinorUnits("92233720368547758.07"); err != nil || value != math.MaxInt64 {
		t.Fatal(value, err)
	}
	for _, value := range []string{"92233720368547758.08", " 123.45", "123.45 ", "1e2", "1.000", "-1", ""} {
		if _, err := parseDecimalMinorUnits(value); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
}

func TestCryptoBotCreationRejectsUnusableIdentityAndURL(t *testing.T) {
	for _, body := range []string{
		`{"invoice_id":0,"pay_url":"https://pay.example/41"}`, `{"invoice_id":-1,"pay_url":"https://pay.example/41"}`,
		`{"invoice_id":"041","pay_url":"https://pay.example/41"}`, `{"invoice_id":41}`, `{"invoice_id":41,"pay_url":"http://pay.example/41"}`,
		`{"invoice_id":41,"pay_url":"https://user:pass@pay.example/41"}`, `{"invoice_id":41,"pay_url":"/relative"}`,
	} {
		p := contractProvider(func(r *http.Request) (*http.Response, error) {
			return contractResponse(r, 200, `{"ok":true,"result":`+body+`}`), nil
		})
		order := contractOrder()
		if invoice, err := p.CreateInvoice(context.Background(), &order, &paid.Tariff{}, nil); err == nil || invoice != nil {
			t.Fatal("unusable invoice was delivered")
		}
	}
	p := contractProvider(func(r *http.Request) (*http.Response, error) {
		return contractResponse(r, 200, `{"ok":true,"result":{"invoice_id":41,"pay_url":"https://pay.example/41"}}`), nil
	})
	order := contractOrder()
	if invoice, err := p.CreateInvoice(context.Background(), &order, &paid.Tariff{}, nil); err != nil || invoice.PayURL != "https://pay.example/41" {
		t.Fatal("pay_url fallback failed")
	}
	order.Amount = 0
	if _, err := p.CreateInvoice(context.Background(), &order, &paid.Tariff{}, nil); err == nil {
		t.Fatal("zero amount accepted")
	}
}

func TestCryptoBotPollAuthenticatesEveryProviderFact(t *testing.T) {
	mutations := map[string]func(*cryptoInvoice){
		"valid": func(*cryptoInvoice) {}, "payload": func(i *cryptoInvoice) { i.Payload = "other" },
		"missing_payload": func(i *cryptoInvoice) { i.Payload = "" }, "amount": func(i *cryptoInvoice) { i.Amount = "123.46" },
		"currency": func(i *cryptoInvoice) { i.Fiat = "USD" }, "currency_case": func(i *cryptoInvoice) { i.Fiat = "rub" },
		"currency_type": func(i *cryptoInvoice) { i.CurrencyType = "crypto" }, "unknown_status": func(i *cryptoInvoice) { i.Status = "unknown" },
		"wrong_ref": func(i *cryptoInvoice) { i.InvoiceID = "42" }, "active": func(i *cryptoInvoice) { i.Status = "active" },
		"expired": func(i *cryptoInvoice) { i.Status = "expired" }, "cancelled": func(i *cryptoInvoice) { i.Status = "cancelled" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			invoice := contractInvoice()
			mutate(&invoice)
			data, _ := json.Marshal(map[string]any{"ok": true, "result": map[string]any{"items": []cryptoInvoice{invoice}}})
			p := contractProvider(func(r *http.Request) (*http.Response, error) { return contractResponse(r, 200, string(data)), nil })
			out, err := p.Poll(context.Background(), []paid.PaymentOrder{contractOrder()})
			if err != nil {
				t.Fatal(err)
			}
			if (len(out.Paid) == 1) != (name == "valid") {
				t.Fatal("unauthenticated paid outcome", out)
			}
			if name == "valid" && out.Paid[0].ProviderChargeID != "cryptobot:41" {
				t.Fatal("charge identity lost")
			}
			if (len(out.TerminalOrderIDs) == 1) != (name == "expired" || name == "cancelled") {
				t.Fatal("unverified terminal outcome", out)
			}
			if name != "valid" && name != "wrong_ref" && name != "active" && name != "expired" && name != "cancelled" && len(out.ReviewOrderIDs) != 1 {
				t.Fatal("conflict not normalized", out)
			}
		})
	}
}

func TestCryptoBotPollRejectsDuplicateInputAndResponseIdentities(t *testing.T) {
	for _, duplicateResponse := range []bool{false, true} {
		items := []cryptoInvoice{contractInvoice()}
		orders := []paid.PaymentOrder{contractOrder()}
		if duplicateResponse {
			items = append(items, contractInvoice())
		} else {
			other := contractOrder()
			other.Id = 8
			orders = append(orders, other)
		}
		data, _ := json.Marshal(map[string]any{"ok": true, "result": map[string]any{"items": items}})
		p := contractProvider(func(r *http.Request) (*http.Response, error) { return contractResponse(r, 200, string(data)), nil })
		out, err := p.Poll(context.Background(), orders)
		if err != nil || len(out.Paid) != 0 || len(out.TerminalOrderIDs) != 0 || len(out.ReviewOrderIDs) != len(orders) {
			t.Fatal("ambiguous identity accepted", out, err)
		}
	}
}

func TestCryptoBotHTTPContractSanitizesAndBoundsFailure(t *testing.T) {
	for _, fixture := range []struct {
		status int
		body   string
	}{
		{500, `{"ok":true,"result":{}}`}, {200, `{"ok":false,"error":{"name":"qualification-sentinel"}}`},
		{200, `{"ok":true}`}, {200, `{"ok":true,"result":null}`}, {200, `{"ok":true,"result":"qualification-sentinel"}`},
		{200, `qualification-sentinel`}, {200, strings.Repeat("x", maxProviderResponseBytes+1)},
	} {
		p := contractProvider(func(r *http.Request) (*http.Response, error) {
			return contractResponse(r, fixture.status, fixture.body), nil
		})
		var out cryptoInvoice
		if err := p.call(context.Background(), "GET", "/api/getInvoices", nil, &out); err == nil || strings.Contains(err.Error(), "qualification-sentinel") {
			t.Fatal("failure leaked or accepted", err)
		}
	}
	p := &cryptoBotProvider{newHTTPClient: func(time.Duration) (*http.Client, error) { return nil, errors.New("qualification-sentinel") }}
	if err := p.call(context.Background(), "GET", "/api/getInvoices", nil, &cryptoInvoice{}); err == nil || strings.Contains(err.Error(), "qualification-sentinel") {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	p = contractProvider(func(r *http.Request) (*http.Response, error) {
		cancel()
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	if err := p.call(ctx, "GET", "/api/getInvoices", nil, &cryptoInvoice{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation identity lost", err)
	}
	p = contractProvider(func(r *http.Request) (*http.Response, error) { return nil, errors.New("qualification-sentinel") })
	if err := p.call(context.Background(), "GET", "/api/getInvoices", nil, &cryptoInvoice{}); err == nil || strings.Contains(err.Error(), "qualification-sentinel") {
		t.Fatal(err)
	}
}

func TestCryptoBotRedirectCannotForwardTokenOrMutateInjectedClient(t *testing.T) {
	requests := 0
	client := &http.Client{Transport: contractTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		response := contractResponse(r, 302, "")
		response.Header.Set("Location", "https://other.example/secret")
		return response, nil
	})}
	p := &cryptoBotProvider{token: "qualification-sentinel", newHTTPClient: func(time.Duration) (*http.Client, error) { return client, nil }}
	if err := p.call(context.Background(), "GET", "/api/getInvoices", nil, &cryptoInvoice{}); err == nil || requests != 1 {
		t.Fatal("redirect followed", err, requests)
	}
	if client.CheckRedirect != nil || client.Timeout != 0 {
		t.Fatal("injected client was mutated")
	}
}

type failedContractBody struct {
	read   func() (int, error)
	closed bool
}

func (b *failedContractBody) Read([]byte) (int, error) { return b.read() }
func (b *failedContractBody) Close() error             { b.closed = true; return nil }

func TestCryptoBotBodyFailurePreservesCancellationAndCloses(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	body := &failedContractBody{read: func() (int, error) { cancel(); return 0, errors.New("qualification-sentinel") }}
	p := contractProvider(func(r *http.Request) (*http.Response, error) {
		response := contractResponse(r, 200, "")
		response.Body = body
		return response, nil
	})
	err := p.call(ctx, "GET", "/api/getInvoices", nil, &cryptoInvoice{})
	if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "qualification-sentinel") || !body.closed {
		t.Fatal("read failure contract", err, body.closed)
	}
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	requests := 0
	p = contractProvider(func(r *http.Request) (*http.Response, error) { requests++; return nil, errors.New("unexpected") })
	if err := p.call(ctx, "GET", "/api/getInvoices", nil, nil); !errors.Is(err, context.Canceled) || requests != 0 {
		t.Fatal("canceled request entered transport", err)
	}
}

func TestCryptoBotPollRequiresBoundedPresentItems(t *testing.T) {
	for _, result := range []string{`{}`, `{"items":null}`, `{"items":"bad"}`} {
		p := contractProvider(func(r *http.Request) (*http.Response, error) {
			return contractResponse(r, 200, `{"ok":true,"result":`+result+`}`), nil
		})
		if _, err := p.Poll(context.Background(), []paid.PaymentOrder{contractOrder()}); err == nil {
			t.Fatal("missing typed list accepted")
		}
	}
	p := contractProvider(func(r *http.Request) (*http.Response, error) {
		t.Fatal("oversize work entered transport")
		return nil, nil
	})
	if _, err := p.Poll(context.Background(), make([]paid.PaymentOrder, 101)); err == nil {
		t.Fatal("unbounded poll accepted")
	}
}
