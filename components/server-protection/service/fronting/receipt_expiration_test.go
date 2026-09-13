package fronting

import (
	"errors"
	"testing"

	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

func TestReceiptExpiredReceiptHasExplicitFrontingSemanticCode(t *testing.T) {
	err := normalizeSemanticErrorV2(protectionrepository.ErrIdempotencyKeyExpired)
	var semantic *SemanticErrorV2
	if !errors.As(err, &semantic) || semantic.Code != "idempotency_key_expired" || semantic.Ambiguous {
		t.Fatalf("expired receipt semantic error=%#v", err)
	}
}
