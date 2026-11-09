//go:build !minimal

package telegram

import (
	canonical "github.com/MalenkiySolovey/solovey-host/componentkit/telegram"
	"time"
)

const MaxRetryAfter = canonical.MaxRetryAfter

type Result = canonical.Result

func RetryAfter(status int, body []byte) time.Duration { return canonical.RetryAfter(status, body) }
func StatusErrorClass(status int) string               { return canonical.StatusErrorClass(status) }
func Caption(value string) string                      { return canonical.Caption(value) }
