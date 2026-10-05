package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	paid "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid"
)

type InvoiceState string

const (
	InvoiceActive   InvoiceState = "active"
	InvoicePaid     InvoiceState = "paid"
	InvoiceTerminal InvoiceState = "terminal"
	InvoiceUnknown  InvoiceState = "unknown"
)

type RecoveredInvoice struct {
	Ref      string
	State    InvoiceState
	Verified bool
	PayURL   string
}
type Reconciliation struct {
	OrderID      uint
	Candidates   []RecoveredInvoice
	ReviewReason string
}
type ReconciliationProvider interface {
	Reconcile(context.Context, []paid.PaymentOrder) ([]Reconciliation, error)
	CancelInvoice(context.Context, string) (bool, error)
}

func (p *cryptoBotProvider) Reconcile(ctx context.Context, orders []paid.PaymentOrder) ([]Reconciliation, error) {
	if len(orders) > 100 {
		return nil, errors.New("cryptobot: reconciliation batch exceeds limit")
	}
	results := make([]Reconciliation, len(orders))
	byKey := make(map[string][]int)
	for index, order := range orders {
		results[index].OrderID = order.Id
		if order.IdempotencyKey == "" {
			results[index].ReviewReason = "provider_payload_missing"
			continue
		}
		byKey[order.IdempotencyKey] = append(byKey[order.IdempotencyKey], index)
	}
	if len(byKey) == 0 {
		return results, nil
	}
	seen := make(map[string]struct{})
	for page := 0; page < 20; page++ {
		items, err := p.invoices(ctx, "/api/getInvoices?count=100&offset="+strconv.Itoa(page*100))
		if err != nil {
			return nil, err
		}
		for _, invoice := range items {
			ref := invoice.InvoiceID.String()
			if !validInvoiceRef(ref) {
				return nil, errors.New("cryptobot: invalid reconciliation identity")
			}
			if _, exists := seen[ref]; exists {
				return nil, errors.New("cryptobot: repeated reconciliation identity")
			}
			seen[ref] = struct{}{}
			for _, index := range byKey[invoice.Payload] {
				if len(byKey[invoice.Payload]) != 1 {
					results[index].ReviewReason = "provider_payload_conflict"
					continue
				}
				state := InvoiceUnknown
				switch invoice.Status {
				case "active":
					state = InvoiceActive
				case "paid":
					state = InvoicePaid
				case "expired", "cancelled":
					state = InvoiceTerminal
				}
				payURL := invoice.BotInvoiceURL
				if !usableInvoiceURL(payURL) {
					payURL = invoice.PayURL
				}
				if !usableInvoiceURL(payURL) {
					payURL = ""
				}
				results[index].Candidates = append(results[index].Candidates, RecoveredInvoice{
					Ref: ref, State: state, Verified: cryptoInvoiceMatches(orders[index], invoice), PayURL: payURL,
				})
			}
		}
		if len(items) < 100 {
			return results, nil
		}
	}
	return nil, errors.New("cryptobot: reconciliation page limit reached")
}

var errInvoiceGone = errors.New("cryptobot: invoice is no longer active")

func (p *cryptoBotProvider) CancelInvoice(ctx context.Context, ref string) (bool, error) {
	if !validInvoiceRef(ref) {
		return false, errors.New("cryptobot: invalid cancellation identity")
	}
	id, _ := strconv.ParseInt(ref, 10, 64)
	var deleted bool
	err := p.call(ctx, http.MethodPost, "/api/deleteInvoice", map[string]any{"invoice_id": id}, &deleted)
	if errors.Is(err, errInvoiceGone) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if !deleted {
		return false, errors.New("cryptobot: cancellation was not confirmed")
	}
	return true, nil
}

// Called only for a rejected, authenticated 2xx envelope. Known exact codes are
// normalized; arbitrary API names never appear in returned errors or audit.
func cryptoAPIError(raw json.RawMessage) error {
	var api struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &api) == nil {
		switch api.Name {
		case "INVOICE_NOT_FOUND", "INVOICE_ALREADY_DELETED", "INVOICE_DELETED":
			return errInvoiceGone
		}
	}
	return errors.New("cryptobot: API rejected request")
}
