package service

import (
	"context"
	canonical "github.com/MalenkiySolovey/solovey-backend/security/tokenuse"
	"github.com/MalenkiySolovey/solovey-ui/logger"
	"time"
)

const tokenUseFlushInterval = canonical.DefaultInterval

type tokenUseDebouncer = canonical.Debouncer

func newTokenUseDebouncer(interval time.Duration) *tokenUseDebouncer {
	debouncer := canonical.New(interval, canonical.FlushUpdates)
	debouncer.Warning = logger.Warning
	return debouncer
}

func StopTokenUseDebouncer(ctx context.Context) error {
	return canonical.Stop(ctx, DefaultRuntime().tokenUseDebouncer())
}
