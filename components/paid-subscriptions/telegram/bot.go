//go:build !minimal

package telegram

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	integrationtelegram "github.com/MalenkiySolovey/solovey-ui/componentkit/telegram"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/util/ratelimit"
)

// Bot is the long-poll receiver for the client-facing Telegram bot. One Bot
// instance is the sole getUpdates consumer for its token.
type Bot struct {
	setting      paidSettings
	stats        service.StatsService
	runtime      *service.Runtime
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

func newBotWithRuntime(runtime *service.Runtime) *Bot {
	bot := newBot()
	bot.runtime = runtime
	bot.payments = newPaymentCoordinator(runtime)
	return bot
}

func nowUnix() int64 { return time.Now().Unix() }

// ---- lifecycle (package singleton) ----

var (
	botMu     sync.Mutex
	botCancel context.CancelFunc
	botDone   chan struct{}
)

// StartBot launches the receiver goroutine if not already running. Idempotent.
func StartBot(runtime *service.Runtime) {
	botMu.Lock()
	defer botMu.Unlock()
	if botCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	botCancel = cancel
	botDone = done
	b := newBotWithRuntime(runtime)
	go b.run(ctx, done)
}

// StopBot signals the receiver to stop and waits up to ctx for it to finish.
func StopBot(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	botMu.Lock()
	cancel := botCancel
	done := botDone
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
		botMu.Lock()
		if botDone == done {
			botCancel = nil
			botDone = nil
		}
		botMu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// newSenderBot builds a Bot ready to SEND (not poll) — used by the payment poll
// job to notify users out-of-band. Returns an error if the bot token is unset.
func newSenderBot(runtime ...*service.Runtime) (*Bot, error) {
	var hostRuntime *service.Runtime
	if len(runtime) > 0 {
		hostRuntime = runtime[0]
	}
	b := newBotWithRuntime(hostRuntime)
	token, err := b.setting.GetPaidSubBotToken()
	if err != nil || token == "" {
		return nil, fmt.Errorf("paidsub: bot token not configured")
	}
	poll, _ := b.setting.GetPaidSubBotPollSeconds()
	client, err := newPaidSubHTTPClient(b.runtime, time.Duration(poll+10)*time.Second)
	if err != nil {
		return nil, err
	}
	b.client = client
	b.token = token
	return b, nil
}

func (b *Bot) closeIdleConnections() {
	if b != nil && b.client != nil {
		b.client.CloseIdleConnections()
	}
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
	defer func() {
		if b.client != nil {
			b.client.CloseIdleConnections()
		}
	}()
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		var wait time.Duration
		wait, backoff = b.pollIteration(ctx, backoff)
		if ctx.Err() != nil || (wait > 0 && sleepCtx(ctx, wait)) {
			return
		}
	}
}

// One finite iteration owns token/config/offset reads, provider I/O, handling
// and offset writes on a single DB generation. Backoff owns no DB admission.
func (b *Bot) pollIteration(ctx context.Context, backoff time.Duration) (time.Duration, time.Duration) {
	const maxBackoff = 60 * time.Second
	ctx, release, err := dbsqlite.AcquireOperation(ctx)
	if err != nil {
		return 5 * time.Second, backoff
	}
	defer release()
	enabled, err := b.setting.GetPaidSubEnabled()
	if err != nil || !enabled {
		return 5 * time.Second, backoff
	}
	token, err := b.setting.GetPaidSubBotToken()
	if err != nil || token == "" {
		return 5 * time.Second, backoff
	}
	poll, _ := b.setting.GetPaidSubBotPollSeconds()
	client, err := newPaidSubHTTPClient(b.runtime, time.Duration(poll+10)*time.Second)
	if err != nil {
		logger.Warning("paidsub: build http client: ", err)
		return backoff, nextBackoff(backoff, maxBackoff)
	}
	// Retain the existing transport cleanup when replacing each poll client.
	if b.client != nil && b.client != client {
		b.client.CloseIdleConnections()
	}
	b.client, b.token = client, token
	offset, err := b.setting.GetPaidSubUpdateOffset()
	if err != nil || offset < 0 {
		logger.Warning("paidsub: read update offset: invalid persisted value")
		return backoff, nextBackoff(backoff, maxBackoff)
	}
	updates, err := b.getUpdates(ctx, offset, poll)
	if err != nil {
		if ctx.Err() != nil {
			return 0, backoff
		}
		return b.classifyError(err, backoff), nextBackoff(backoff, maxBackoff)
	}
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
	return 0, time.Second
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
