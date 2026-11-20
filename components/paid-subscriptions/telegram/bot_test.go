//go:build !minimal

package telegram

import (
	"errors"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-backend/http/ratelimit"
	integrationtelegram "github.com/MalenkiySolovey/solovey-ui/componentkit/telegram"
)

func TestIsAlreadyRefunded(t *testing.T) {
	if !isAlreadyRefunded(&integrationtelegram.APIError{Code: 400, Description: "Bad Request: CHARGE_ALREADY_REFUNDED"}) {
		t.Error("CHARGE_ALREADY_REFUNDED should be detected as already-refunded")
	}
	if isAlreadyRefunded(&integrationtelegram.APIError{Code: 400, Description: "USER_NOT_FOUND"}) {
		t.Error("unrelated API error must not be treated as already-refunded")
	}
	if isAlreadyRefunded(nil) {
		t.Error("nil error is not already-refunded")
	}
	if isAlreadyRefunded(errors.New("network")) {
		t.Error("non-API error is not already-refunded")
	}
}

func TestRateLimiter(t *testing.T) {
	rl := ratelimit.NewFixedWindow[int64](time.Minute, 3, 8192, 0)
	now := time.Unix(1000, 0)
	for i := 0; i < 3; i++ {
		if !rl.AllowAt(1, now).Allowed {
			t.Fatalf("request %d should be allowed", i)
		}
	}
	if rl.AllowAt(1, now).Allowed {
		t.Fatal("4th request in window should be denied")
	}
	// A different key is independent.
	if !rl.AllowAt(2, now).Allowed {
		t.Fatal("different key should be allowed")
	}
	// New window resets the count.
	if !rl.AllowAt(1, now.Add(time.Minute)).Allowed {
		t.Fatal("request after window should be allowed")
	}
}
