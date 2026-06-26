package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	paid "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/paid"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
)

const cryptoBotBase = "https://pay.crypt.bot"
const maxProviderResponseBytes = 1 << 20

type cryptoBotProvider struct {
	token         string
	newHTTPClient func(time.Duration) (*http.Client, error)
	notify        func(string, map[string]string)
}

type CryptoBotDeps struct {
	NewHTTPClient func(time.Duration) (*http.Client, error)
	Notify        func(string, map[string]string)
}

func NewCryptoBotProvider(token string, deps CryptoBotDeps) PaymentProvider {
	return &cryptoBotProvider{token: token, newHTTPClient: deps.NewHTTPClient, notify: deps.Notify}
}

func (p *cryptoBotProvider) Kind() ProviderKind { return ProviderCryptoBot }
func (p *cryptoBotProvider) Title(language string) string {
	return ProviderTitle(ProviderCryptoBot, language)
}

func (p *cryptoBotProvider) CreateInvoice(ctx context.Context, order *paid.PaymentOrder, tariff *paid.Tariff, client *model.Client) (*Invoice, error) {
	amount := fmt.Sprintf("%.2f", float64(order.Amount)/100.0)
	body := map[string]any{
		"currency_type": "fiat",
		"fiat":          order.Currency,
		"amount":        amount,
		"payload":       order.IdempotencyKey,
		"description":   tariff.Name,
	}
	var out struct {
		InvoiceID json.Number `json:"invoice_id"`
		PayURL    string      `json:"pay_url"`
	}
	if err := p.call(ctx, http.MethodPost, "/api/createInvoice", body, &out); err != nil {
		return nil, err
	}
	return &Invoice{
		Method:      InvoiceURL,
		Title:       tariff.Name,
		PayURL:      out.PayURL,
		ProviderRef: out.InvoiceID.String(),
		Payload:     order.IdempotencyKey,
	}, nil
}

func (p *cryptoBotProvider) Poll(ctx context.Context, pending []paid.PaymentOrder) ([]PollResult, error) {
	idToOrder := map[string]paid.PaymentOrder{}
	var ids []string
	for _, order := range pending {
		ref := ExtractProviderRef(order.ProviderPayload)
		if ref == "" {
			continue
		}
		idToOrder[ref] = order
		ids = append(ids, ref)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	var out struct {
		Items []struct {
			InvoiceID json.Number `json:"invoice_id"`
			Status    string      `json:"status"`
			Amount    string      `json:"amount"`
			Fiat      string      `json:"fiat"`
		} `json:"items"`
	}
	path := "/api/getInvoices?invoice_ids=" + url.QueryEscape(strings.Join(ids, ","))
	if err := p.call(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	var results []PollResult
	for _, item := range out.Items {
		if item.Status != "paid" {
			continue
		}
		invoiceID := item.InvoiceID.String()
		order, ok := idToOrder[invoiceID]
		if !ok {
			continue
		}
		if item.Amount != "" {
			want := fmt.Sprintf("%.2f", float64(order.Amount)/100.0)
			got := item.Amount
			if paid, err := strconv.ParseFloat(item.Amount, 64); err == nil {
				got = fmt.Sprintf("%.2f", paid)
			}
			currencyMismatch := item.Fiat != "" && !strings.EqualFold(item.Fiat, order.Currency)
			if got != want || currencyMismatch {
				logger.Warning("paidsub: cryptobot paid amount/currency mismatch; refusing order ", order.Id)
				if p.notify != nil {
					p.notify("paidsub_payment_mismatch", map[string]string{
						"orderId": fmt.Sprintf("%d", order.Id),
					})
				}
				continue
			}
		}
		results = append(results, PollResult{
			OrderID:          order.Id,
			ProviderChargeID: "cryptobot:" + invoiceID,
		})
	}
	return results, nil
}

func (p *cryptoBotProvider) call(ctx context.Context, method, path string, body any, out any) error {
	if p.newHTTPClient == nil {
		return fmt.Errorf("cryptobot: HTTP client is not configured")
	}
	client, err := p.newHTTPClient(15 * time.Second)
	if err != nil {
		return err
	}
	var reader io.Reader
	if body != nil {
		bb, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(bb)
	}
	req, err := http.NewRequestWithContext(ctx, method, cryptoBotBase+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Crypto-Pay-API-Token", p.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("cryptobot: network error")
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxProviderResponseBytes))
	var env struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("cryptobot: malformed response")
	}
	if !env.OK {
		return fmt.Errorf("cryptobot: api returned not-ok")
	}
	if out != nil && len(env.Result) > 0 {
		return json.Unmarshal(env.Result, out)
	}
	return nil
}

func ExtractProviderRef(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}
	var m struct {
		Ref string `json:"ref"`
	}
	if err := json.Unmarshal(payload, &m); err != nil {
		return ""
	}
	return m.Ref
}
