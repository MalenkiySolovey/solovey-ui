package udpguard

import (
	"testing"

	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

func TestReceiptExpiredReceiptHasExplicitUDPContractCode(t *testing.T) {
	if code := ErrorCode(udpReceiptError(protectionrepository.ErrIdempotencyKeyExpired)); code != CodeIdempotencyExpired {
		t.Fatalf("expired receipt code=%q", code)
	}
}
