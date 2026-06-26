package telegram

import (
	"context"
	"strings"
	"time"

	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	paidstore "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/paid/store"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

// broadcastThrottle paces sends to stay well under Telegram's ~30 msg/s limit.
const broadcastThrottle = 60 * time.Millisecond
const broadcastMaxRunes = 4096

// Broadcast sends a custom announcement to every bound Telegram user. It runs
// sequentially with a small throttle. Returns counts of sent/failed messages.
func Broadcast(ctx context.Context, text string) (sent int, failed int, err error) {
	if strings.TrimSpace(text) == "" {
		return 0, 0, common.NewError("message is empty")
	}
	text = truncateRunes(text, broadcastMaxRunes)
	b, err := newSenderBot()
	if err != nil {
		return 0, 0, err
	}
	users, err := paidstore.ListTelegramUserIDs(dbsqlite.DB())
	if err != nil {
		return 0, 0, err
	}
	for _, tgUserID := range users {
		if sendErr := b.sendMessage(ctx, tgUserID, text, nil); sendErr != nil {
			failed++
		} else {
			sent++
		}
		select {
		case <-ctx.Done():
			return sent, failed, ctx.Err()
		case <-time.After(broadcastThrottle):
		}
	}
	return sent, failed, nil
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}
