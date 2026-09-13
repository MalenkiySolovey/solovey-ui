//go:build !minimal

package api

import (
	"net/http"
	"testing"

	protectionfronting "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/fronting"
)

func TestReceiptExpiredFrontingReceiptIsAnExplicitConflict(t *testing.T) {
	code, status := frontingErrorCode(&protectionfronting.SemanticErrorV2{Code: "idempotency_key_expired"})
	if code != "idempotency_key_expired" || status != http.StatusConflict {
		t.Fatalf("expired receipt API contract code=%q status=%d", code, status)
	}
}
