package telegram

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	integrationtelegram "github.com/MalenkiySolovey/solovey-ui/internal/integrations/telegram"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/util/ratelimit"
)

// Bot is the long-poll receiver for the client-facing Telegram bot. One Bot
// instance is the sole getUpdates consumer for its token.
type Bot struct {
	setting      service.SettingService
	stats        service.StatsService
	payments     *paymentCoordinator
	client       *http.Client
	token        string
	cmdLimiter   *ratelimit.FixedWindow[int64]
	startLimiter *ratelimit.FixedWindow[int64]
}

func newBot() *Bot {
	return &Bot{
		payments:     newPaymentCoordinator(),
		cmdLimiter:   ratelimit.NewFixedWindow[int64](time.Minute, 20, 8192, 0),
		startLimiter: ratelimit.NewFixedWindow[int64](time.Minute, 0, 8192, 0),
	}
}

func nowUnix() int64 { return time.Now().Unix() }

// ---- lifecycle (package singleton) ----

var (
	botMu     sync.Mutex
	botCancel context.CancelFunc
	botDone   chan struct{}
)

// StartBot launches the receiver goroutine if not already running. Idempotent.
func StartBot() {
	botMu.Lock()
	defer botMu.Unlock()
	if botCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	botCancel = cancel
	botDone = done
	b := newBot()
	go b.run(ctx, done)
}

// StopBot signals the receiver to stop and waits up to ctx for it to finish.
func StopBot(ctx context.Context) error {
	botMu.Lock()
	cancel := botCancel
	done := botDone
	botCancel = nil
	botDone = nil
	botMu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// newSenderBot builds a Bot ready to SEND (not poll) — used by the payment poll
// job to notify users out-of-band. Returns an error if the bot token is unset.
func newSenderBot() (*Bot, error) {
	b := newBot()
	token, err := b.setting.GetPaidSubBotToken()
	if err != nil || token == "" {
		return nil, fmt.Errorf("paidsub: bot token not configured")
	}
	poll, _ := b.setting.GetPaidSubBotPollSeconds()
	client, err := service.NewPaidSubHTTPClient(time.Duration(poll+10) * time.Second)
	if err != nil {
		return nil, err
	}
	b.client = client
	b.token = token
	return b, nil
}

// sleepCtx sleeps for d or until ctx is cancelled. Returns true if cancelled.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return true
	case <-t.C:
		return false
	}
}

func (b *Bot) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	backoff := time.Second
	const maxBackoff = 60 * time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		enabled, err := b.setting.GetPaidSubEnabled()
		if err != nil || !enabled {
			if sleepCtx(ctx, 5*time.Second) {
				return
			}
			continue
		}
		token, err := b.setting.GetPaidSubBotToken()
		if err != nil || token == "" {
			if sleepCtx(ctx, 5*time.Second) {
				return
			}
			continue
		}
		poll, _ := b.setting.GetPaidSubBotPollSeconds()
		client, err := service.NewPaidSubHTTPClient(time.Duration(poll+10) * time.Second)
		if err != nil {
			logger.Warning("paidsub: build http client: ", err)
			if sleepCtx(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff, maxBackoff)
			continue
		}
		// Close the previous client's idle keep-alive connections before
		// replacing it; a discarded *http.Transport (proxy/outbound mode) does
		// not auto-close them, so rebuilding every loop would leak sockets.
		if b.client != nil && b.client != client {
			b.client.CloseIdleConnections()
		}
		b.client = client
		b.token = token

		offset, _ := b.setting.GetPaidSubUpdateOffset()
		updates, err := b.getUpdates(ctx, offset, poll)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			wait := b.classifyError(err, backoff)
			if sleepCtx(ctx, wait) {
				return
			}
			backoff = nextBackoff(backoff, maxBackoff)
			continue
		}
		backoff = time.Second

		maxID := offset
		for i := range updates {
			b.handleUpdate(ctx, &updates[i])
			if updates[i].UpdateID >= maxID {
				maxID = updates[i].UpdateID + 1
			}
		}
		if maxID != offset {
			if err := b.setting.SetPaidSubUpdateOffset(maxID); err != nil {
				logger.Warning("paidsub: persist offset: ", err)
			}
		}
	}
}

func nextBackoff(cur, max time.Duration) time.Duration {
	cur *= 2
	if cur > max {
		return max
	}
	return cur
}

// classifyError returns how long to wait after a getUpdates failure, handling
// 409 (a second consumer / webhook set) and 401 (revoked token) specially. It
// never logs the token (APIError carries only code and description).
func (b *Bot) classifyError(err error, backoff time.Duration) time.Duration {
	var apiErr *integrationtelegram.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.Code {
		case http.StatusConflict: // 409: another getUpdates consumer or webhook
			logger.Warning("paidsub: getUpdates conflict (409); another consumer or webhook is active")
			return 30 * time.Second
		case http.StatusUnauthorized: // 401: token revoked/invalid
			logger.Warning("paidsub: bot token unauthorized (401); pausing until settings change")
			return 60 * time.Second
		case http.StatusTooManyRequests:
			if apiErr.RetryAfter > 0 {
				return time.Duration(apiErr.RetryAfter) * time.Second
			}
		}
		logger.Warning("paidsub: getUpdates error: ", apiErr.Error())
		return backoff
	}
	logger.Warning("paidsub: getUpdates failed")
	return backoff
}

// ---- dispatch ----
