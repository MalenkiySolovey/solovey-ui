package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"testing"

	paid "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid"
)

func TestCryptoBotRecoveryCompletesBoundedScanBeforeReturningCandidates(t *testing.T) {
	for _, mode := range []string{"complete", "repeat", "limit", "outage"} {
		t.Run(mode, func(t *testing.T) {
			requests := 0
			p := contractProvider(func(r *http.Request) (*http.Response, error) {
				page := requests
				requests++
				if r.URL.Query().Get("count") != "100" || r.URL.Query().Get("offset") != strconv.Itoa(page*100) {
					t.Fatal("unbounded or nondeterministic listing")
				}
				if mode == "outage" && page == 1 {
					return contractResponse(r, 503, "unavailable"), nil
				}
				var items []cryptoInvoice
				if page == 0 || mode == "limit" || mode == "repeat" {
					for index := 1; index <= 100; index++ {
						id := page*100 + index
						if mode == "repeat" {
							id = index
						}
						invoice := contractInvoice()
						invoice.InvoiceID = json.Number(strconv.Itoa(id))
						invoice.Payload = fmt.Sprint("unrelated-", id)
						if page == 0 && index == 41 {
							invoice = contractInvoice()
						}
						items = append(items, invoice)
					}
				} else {
					items = []cryptoInvoice{}
				}
				body, _ := json.Marshal(map[string]any{"ok": true, "result": map[string]any{"items": items}})
				return contractResponse(r, 200, string(body)), nil
			})
			out, err := p.Reconcile(context.Background(), []paid.PaymentOrder{contractOrder()})
			if mode == "complete" {
				if err != nil || requests != 2 || len(out) != 1 || len(out[0].Candidates) != 1 || !out[0].Candidates[0].Verified || out[0].Candidates[0].State != InvoicePaid {
					t.Fatal(out, err, requests)
				}
			} else if err == nil || out != nil || requests > 20 {
				t.Fatal("partial/ambiguous scan escaped", out, err, requests)
			}
			if mode == "limit" && requests != 20 {
				t.Fatal("page bound not enforced")
			}
		})
	}
}

func TestCryptoBotRecoveryNormalizesMissingAndMismatchedEvidence(t *testing.T) {
	invoice := contractInvoice()
	invoice.Amount = "99.99"
	body, _ := json.Marshal(map[string]any{"ok": true, "result": map[string]any{"items": []cryptoInvoice{invoice}}})
	p := contractProvider(func(r *http.Request) (*http.Response, error) { return contractResponse(r, 200, string(body)), nil })
	order, missing := contractOrder(), contractOrder()
	missing.Id = 8
	missing.IdempotencyKey = "no-match"
	out, err := p.Reconcile(context.Background(), []paid.PaymentOrder{order, missing})
	if err != nil || len(out[0].Candidates) != 1 || out[0].Candidates[0].Verified || len(out[1].Candidates) != 0 {
		t.Fatal(out, err)
	}
	missing.IdempotencyKey = ""
	out, err = p.Reconcile(context.Background(), []paid.PaymentOrder{missing})
	if err != nil || out[0].ReviewReason != "provider_payload_missing" {
		t.Fatal(out, err)
	}
}

func TestCryptoBotCancellationRequiresConfirmedTypedOrExactTerminalOutcome(t *testing.T) {
	for _, fixture := range []struct {
		status   int
		body     string
		resolved bool
	}{
		{200, `{"ok":true,"result":true}`, true}, {200, `{"ok":true,"result":false}`, false},
		{200, `{"ok":true}`, false}, {200, `{"ok":true,"result":null}`, false},
		{200, `{"ok":false,"error":{"name":"INVOICE_NOT_FOUND"}}`, true},
		{200, `{"ok":false,"error":{"name":"INVOICE_ALREADY_DELETED"}}`, true},
		{200, `{"ok":false,"error":{"name":"prefix_INVOICE_NOT_FOUND"}}`, false},
		{500, `{"ok":false,"error":{"name":"INVOICE_NOT_FOUND"}}`, false},
		{200, `{"ok":false,"error":{"name":"INVOICE_PAID"}}`, false},
	} {
		p := contractProvider(func(r *http.Request) (*http.Response, error) {
			var request struct {
				InvoiceID int64 `json:"invoice_id"`
			}
			if r.Method != "POST" || r.URL.Path != "/api/deleteInvoice" || json.NewDecoder(r.Body).Decode(&request) != nil || request.InvoiceID != 41 {
				t.Fatal("wrong cancellation identity")
			}
			return contractResponse(r, fixture.status, fixture.body), nil
		})
		resolved, err := p.CancelInvoice(context.Background(), "41")
		if resolved != fixture.resolved || (err == nil) != fixture.resolved {
			t.Fatal("cancellation boundary", resolved, err)
		}
	}
}
