package telegram

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/internal/httpheader"
	"github.com/MalenkiySolovey/solovey-ui/util/redact"
)

const MaxRetryAfter = 300 * time.Second

type Result struct {
	Success    bool          `json:"success"`
	ErrorClass string        `json:"errorClass,omitempty"`
	RetryAfter time.Duration `json:"-"`
}

func RetryAfter(status int, body []byte) time.Duration {
	if status != http.StatusTooManyRequests || len(body) == 0 {
		return 0
	}
	var response struct {
		OK         bool `json:"ok"`
		ErrorCode  int  `json:"error_code"`
		Parameters struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return 0
	}
	if response.ErrorCode != http.StatusTooManyRequests || response.Parameters.RetryAfter <= 0 {
		return 0
	}
	retryAfter := time.Duration(response.Parameters.RetryAfter) * time.Second
	if retryAfter > MaxRetryAfter {
		return MaxRetryAfter
	}
	return retryAfter
}

func StatusErrorClass(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusNotFound:
		return "chat_not_found"
	case http.StatusTooManyRequests:
		return "rate_limited"
	default:
		return "unknown"
	}
}

func Caption(caption string) string {
	return httpheader.Sanitize(redact.String(caption), 1024)
}
