package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	paid "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

const cryptoBotBase = "https://pay.crypt.bot"
const maxProviderResponseBytes = 1 << 20
const maxCryptoBotPollBatch = 100
const providerTimeout = 15 * time.Second

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

type cryptoInvoice struct {
	InvoiceID     json.Number `json:"invoice_id"`
	Status        string      `json:"status"`
	Amount        string      `json:"amount"`
	CurrencyType  string      `json:"currency_type"`
	Fiat          string      `json:"fiat"`
	Payload       string      `json:"payload"`
	BotInvoiceURL string      `json:"bot_invoice_url"`
	PayURL        string      `json:"pay_url"`
}

func validInvoiceRef(ref string) bool {
	n, err := strconv.ParseInt(ref, 10, 64)
	return err == nil && n > 0 && strconv.FormatInt(n, 10) == ref
}
func usableInvoiceURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil
}
func (p *cryptoBotProvider) CreateInvoice(ctx context.Context, order *paid.PaymentOrder, tariff *paid.Tariff, client *model.Client) (*Invoice, error) {
	if order == nil || tariff == nil || order.Amount <= 0 || order.Currency == "" || order.IdempotencyKey == "" {
		return nil, errors.New("cryptobot: invalid purchase identity")
	}
	body := map[string]any{"currency_type": "fiat", "fiat": order.Currency,
		"amount":  fmt.Sprintf("%d.%02d", order.Amount/100, order.Amount%100),
		"payload": order.IdempotencyKey, "description": tariff.Name}
	var out cryptoInvoice
	if err := p.call(ctx, http.MethodPost, "/api/createInvoice", body, &out); err != nil {
		return nil, err
	}
	if !validInvoiceRef(out.InvoiceID.String()) {
		return nil, errors.New("cryptobot: invalid invoice identity")
	}
	payURL := out.BotInvoiceURL
	if !usableInvoiceURL(payURL) {
		payURL = out.PayURL
	}
	if !usableInvoiceURL(payURL) {
		return nil, errors.New("cryptobot: invalid invoice URL")
	}
	return &Invoice{Method: InvoiceURL, Title: tariff.Name, PayURL: payURL,
		ProviderRef: out.InvoiceID.String(), Payload: order.IdempotencyKey}, nil
}
func orderInvoiceRef(order paid.PaymentOrder) string {
	if order.ProviderRef != "" {
		return order.ProviderRef
	}
	return ExtractProviderRef(order.ProviderPayload)
}

// Missing invoices and unavailable providers are uncertainty, never nonpayment.
func (p *cryptoBotProvider) Poll(ctx context.Context, pending []paid.PaymentOrder) (PollOutcome, error) {
	var result PollOutcome
	if len(pending) > maxCryptoBotPollBatch {
		return result, errors.New("cryptobot: poll batch exceeds limit")
	}
	byRef := make(map[string][]paid.PaymentOrder)
	var ids []string
	for _, order := range pending {
		ref := orderInvoiceRef(order)
		if !validInvoiceRef(ref) {
			result.ReviewOrderIDs = append(result.ReviewOrderIDs, order.Id)
			continue
		}
		if len(byRef[ref]) == 0 {
			ids = append(ids, ref)
		}
		byRef[ref] = append(byRef[ref], order)
	}
	if len(ids) == 0 {
		return result, nil
	}
	items, err := p.invoices(ctx, "/api/getInvoices?invoice_ids="+url.QueryEscape(strings.Join(ids, ",")))
	if err != nil {
		return PollOutcome{}, err
	}
	byInvoice := make(map[string][]cryptoInvoice)
	for _, item := range items {
		ref := item.InvoiceID.String()
		if !validInvoiceRef(ref) {
			return PollOutcome{}, errors.New("cryptobot: invalid response identity")
		}
		byInvoice[ref] = append(byInvoice[ref], item)
	}
	for _, ref := range ids {
		orders, found := byRef[ref], byInvoice[ref]
		if len(orders) != 1 || len(found) > 1 {
			for _, order := range orders {
				result.ReviewOrderIDs = append(result.ReviewOrderIDs, order.Id)
			}
			continue
		}
		if len(found) == 0 {
			continue
		}
		order, invoice := orders[0], found[0]
		if !cryptoInvoiceMatches(order, invoice) {
			result.ReviewOrderIDs = append(result.ReviewOrderIDs, order.Id)
			if p.notify != nil {
				p.notify("paidsub_payment_mismatch", map[string]string{"orderId": strconv.FormatUint(uint64(order.Id), 10)})
			}
			continue
		}
		switch invoice.Status {
		case "paid":
			result.Paid = append(result.Paid, PollResult{OrderID: order.Id, ProviderChargeID: "cryptobot:" + ref})
		case "expired", "cancelled":
			result.TerminalOrderIDs = append(result.TerminalOrderIDs, order.Id)
		case "active":
		default:
			result.ReviewOrderIDs = append(result.ReviewOrderIDs, order.Id)
		}
	}
	return result, nil
}
func (p *cryptoBotProvider) invoices(ctx context.Context, path string) ([]cryptoInvoice, error) {
	var out struct {
		Items []cryptoInvoice `json:"items"`
	}
	if err := p.call(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	if out.Items == nil || len(out.Items) > maxCryptoBotPollBatch {
		return nil, errors.New("cryptobot: invalid invoice list")
	}
	return out.Items, nil
}
func cryptoInvoiceMatches(order paid.PaymentOrder, invoice cryptoInvoice) bool {
	return order.IdempotencyKey != "" && invoice.Payload == order.IdempotencyKey &&
		invoice.CurrencyType == "fiat" && cryptoBotPaymentMatches(order, invoice.Amount, invoice.Fiat)
}
func cryptoBotPaymentMatches(order paid.PaymentOrder, amount, currency string) bool {
	minor, err := parseDecimalMinorUnits(amount)
	return err == nil && minor > 0 && minor == order.Amount && currency != "" && currency == order.Currency
}
func parseDecimalMinorUnits(value string) (int64, error) {
	parts := strings.Split(value, ".")
	if value == "" || len(parts) > 2 || parts[0] == "" {
		return 0, errors.New("invalid payment amount")
	}
	for _, part := range parts {
		if part == "" {
			return 0, errors.New("invalid payment amount")
		}
		for _, char := range part {
			if char < '0' || char > '9' {
				return 0, errors.New("invalid payment amount")
			}
		}
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
	}
	if len(fraction) > 2 {
		return 0, errors.New("payment amount has too many fractional digits")
	}
	fraction += strings.Repeat("0", 2-len(fraction))
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || whole > math.MaxInt64/100 {
		return 0, errors.New("payment amount is out of range")
	}
	cents, err := strconv.ParseInt(fraction, 10, 64)
	if err != nil || cents > math.MaxInt64-whole*100 {
		return 0, errors.New("payment amount is out of range")
	}
	return whole*100 + cents, nil
}

// Exclude provider bodies, URLs and transport details; retain cancellation identity.
func providerIOError(ctx context.Context, message string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("cryptobot: %s: %w", message, err)
	}
	return fmt.Errorf("cryptobot: %s", message)
}
func (p *cryptoBotProvider) call(ctx context.Context, method, path string, body any, out any) error {
	ctx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return providerIOError(ctx, "request canceled")
	}
	if p.newHTTPClient == nil {
		return errors.New("cryptobot: HTTP client is not configured")
	}
	client, err := p.newHTTPClient(providerTimeout)
	if err != nil || client == nil {
		return providerIOError(ctx, "HTTP client is unavailable")
	}
	defer client.CloseIdleConnections()
	bounded := *client
	if bounded.Timeout <= 0 || bounded.Timeout > providerTimeout {
		bounded.Timeout = providerTimeout
	}
	bounded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	var reader io.Reader
	if body != nil {
		bb, err := json.Marshal(body)
		if err != nil {
			return errors.New("cryptobot: invalid request")
		}
		reader = bytes.NewReader(bb)
	}
	req, err := http.NewRequestWithContext(ctx, method, cryptoBotBase+path, reader)
	if err != nil {
		return errors.New("cryptobot: invalid request")
	}
	req.Header.Set("Crypto-Pay-API-Token", p.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := bounded.Do(req)
	if err != nil {
		return providerIOError(ctx, "network error")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New("cryptobot: HTTP status rejected")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxProviderResponseBytes+1))
	if err != nil {
		return providerIOError(ctx, "response read failed")
	}
	if len(data) > maxProviderResponseBytes {
		return errors.New("cryptobot: response is too large")
	}
	var env struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
	}
	if json.Unmarshal(data, &env) != nil {
		return errors.New("cryptobot: malformed response")
	}
	if !env.OK {
		return errors.New("cryptobot: API rejected request")
	}
	if len(env.Result) == 0 || bytes.Equal(bytes.TrimSpace(env.Result), []byte("null")) {
		return errors.New("cryptobot: result is missing")
	}
	if out != nil && json.Unmarshal(env.Result, out) != nil {
		return errors.New("cryptobot: malformed result")
	}
	return nil
}
func ExtractProviderRef(payload []byte) string {
	var m struct {
		Ref string `json:"ref"`
	}
	if json.Unmarshal(payload, &m) != nil {
		return ""
	}
	return m.Ref
}
