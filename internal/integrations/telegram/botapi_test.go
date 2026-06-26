package telegram

import (
	"net/http"
	"testing"
)

func TestRetryAfterCapsAtMax(t *testing.T) {
	got := RetryAfter(http.StatusTooManyRequests, []byte(`{"ok":false,"error_code":429,"parameters":{"retry_after":999}}`))
	if got != MaxRetryAfter {
		t.Fatalf("retry_after cap=%s, want %s", got, MaxRetryAfter)
	}
}

func TestStatusErrorClassAllowlist(t *testing.T) {
	tests := map[int]string{
		http.StatusUnauthorized:        "unauthorized",
		http.StatusNotFound:            "chat_not_found",
		http.StatusTooManyRequests:     "rate_limited",
		http.StatusInternalServerError: "unknown",
	}
	for status, want := range tests {
		if got := StatusErrorClass(status); got != want {
			t.Fatalf("StatusErrorClass(%d) = %q, want %q", status, got, want)
		}
	}
}
