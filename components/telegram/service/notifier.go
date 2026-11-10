//go:build !minimal

package telegram

import (
	"context"

	canonical "github.com/MalenkiySolovey/solovey-component-telegram/service"
	"github.com/MalenkiySolovey/solovey-ui/logger"
)

const QueueCapacity = canonical.QueueCapacity

type Notification = canonical.Notification
type Notifier = canonical.Notifier

func NewNotifier(capacity int, send func(string) Result, audit func(string, map[string]any)) *Notifier {
	n := canonical.NewNotifier(capacity, send, audit)
	n.Warning = logger.Warning
	return n
}
func NewNotifierContext(capacity int, send func(context.Context, string) Result, audit func(string, map[string]any)) *Notifier {
	return canonical.NewNotifierContext(capacity, send, audit, logger.Warning)
}
