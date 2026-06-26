package service

import (
	"context"
	dbhooks "github.com/MalenkiySolovey/solovey-ui/database/hooks"
	"sync"
	"sync/atomic"
	"time"

	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	integrationtelegram "github.com/MalenkiySolovey/solovey-ui/internal/integrations/telegram"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/restart"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
)

const defaultCoreStartCooldown = 15 * time.Second

type CoreProvider interface {
	Core() *coreruntime.Core
}

type CoreProviderFunc func() *coreruntime.Core

func (f CoreProviderFunc) Core() *coreruntime.Core {
	if f == nil {
		return nil
	}
	return f()
}

type LastUpdateStore struct {
	value atomic.Int64
}

func NewLastUpdateStore() *LastUpdateStore {
	return &LastUpdateStore{}
}

func (s *LastUpdateStore) Set(value int64) {
	if s == nil {
		return
	}
	s.value.Store(value)
}

func (s *LastUpdateStore) Get() int64 {
	if s == nil {
		return 0
	}
	return s.value.Load()
}

type Runtime struct {
	mu sync.RWMutex

	coreProvider     CoreProvider
	restartManager   *restart.Manager
	lastUpdate       *LastUpdateStore
	auditWriter      *auditWriter
	telegramNotifier *integrationtelegram.Notifier
	tokenUse         *tokenUseDebouncer

	coreStartCooldown time.Duration
	lastStartFailTime time.Time
}

func NewRuntime(coreInstance *coreruntime.Core) *Runtime {
	return NewRuntimeWithCoreProvider(CoreProviderFunc(func() *coreruntime.Core {
		return coreInstance
	}))
}

func NewRuntimeWithCoreProvider(provider CoreProvider) *Runtime {
	return &Runtime{
		coreProvider:      provider,
		restartManager:    restart.NewManager(restartSignalDelay, signalCurrentProcess),
		lastUpdate:        NewLastUpdateStore(),
		auditWriter:       newAuditWriter(auditQueueCapacity, auditBatchSize, auditFlushInterval, writeAuditEvents),
		tokenUse:          newTokenUseDebouncer(tokenUseFlushInterval, flushTokenUseUpdates),
		coreStartCooldown: defaultCoreStartCooldown,
	}
}

func (r *Runtime) SetCore(coreInstance *coreruntime.Core) {
	if r == nil {
		return
	}
	r.SetCoreProvider(CoreProviderFunc(func() *coreruntime.Core {
		return coreInstance
	}))
}

func (r *Runtime) SetCoreProvider(provider CoreProvider) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.coreProvider = provider
	r.mu.Unlock()
}

func (r *Runtime) Core() *coreruntime.Core {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	provider := r.coreProvider
	r.mu.RUnlock()
	if provider == nil {
		return nil
	}
	return provider.Core()
}

func (r *Runtime) RestartScheduler() RestartScheduler {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	manager := r.restartManager
	r.mu.RUnlock()
	return manager
}

func (r *Runtime) restart() *restart.Manager {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	manager := r.restartManager
	r.mu.RUnlock()
	return manager
}

func (r *Runtime) updates() *LastUpdateStore {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	store := r.lastUpdate
	r.mu.RUnlock()
	return store
}

func (r *Runtime) audit() *auditWriter {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	writer := r.auditWriter
	r.mu.RUnlock()
	return writer
}

func (r *Runtime) replaceAuditWriterIfCurrent(current *auditWriter) {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.auditWriter == current {
		r.auditWriter = newAuditWriter(auditQueueCapacity, auditBatchSize, auditFlushInterval, writeAuditEvents)
	}
	r.mu.Unlock()
}

func (r *Runtime) telegram() *integrationtelegram.Notifier {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.telegramNotifier == nil {
		r.telegramNotifier = newDefaultTelegramNotifier()
	}
	notifier := r.telegramNotifier
	return notifier
}

func (r *Runtime) replaceTelegramNotifierIfCurrent(current *integrationtelegram.Notifier) {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.telegramNotifier == current {
		r.telegramNotifier = newDefaultTelegramNotifier()
	}
	r.mu.Unlock()
}

func (r *Runtime) tokenUseDebouncer() *tokenUseDebouncer {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	debouncer := r.tokenUse
	r.mu.RUnlock()
	return debouncer
}

func (r *Runtime) resetTokenUseDebouncer() {
	if r == nil {
		return
	}
	finishReset := beginTokenUseReset()
	defer finishReset()
	r.mu.Lock()
	current := r.tokenUse
	if current != nil {
		if err := current.flushNow(context.Background(), true); err != nil {
			logger.Warning("token use flush before reset failed:", err)
		}
	}
	r.tokenUse = newTokenUseDebouncer(tokenUseFlushInterval, flushTokenUseUpdates)
	r.mu.Unlock()
}

func (r *Runtime) startCooldownActive() bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	lastStartFailTime := r.lastStartFailTime
	coreStartCooldown := r.coreStartCooldown
	r.mu.RUnlock()
	return time.Since(lastStartFailTime) < coreStartCooldown
}

func (r *Runtime) markCoreStartFailed() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.lastStartFailTime = time.Now()
	r.mu.Unlock()
}

func (r *Runtime) markCoreStartSucceeded() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.lastStartFailTime = time.Time{}
	r.mu.Unlock()
}

func (r *Runtime) coreStartCooldownDuration() time.Duration {
	if r == nil {
		return defaultCoreStartCooldown
	}
	r.mu.RLock()
	coreStartCooldown := r.coreStartCooldown
	r.mu.RUnlock()
	if coreStartCooldown <= 0 {
		return defaultCoreStartCooldown
	}
	return coreStartCooldown
}

var (
	defaultRuntimeMu sync.RWMutex
	defaultRuntime   = NewRuntimeWithCoreProvider(nil)
)

func init() {
	dbhooks.RegisterResetHook("service.token_use_debouncer", func() {
		DefaultRuntime().resetTokenUseDebouncer()
	})
}

func DefaultRuntime() *Runtime {
	defaultRuntimeMu.RLock()
	runtime := defaultRuntime
	defaultRuntimeMu.RUnlock()
	return runtime
}

func SetDefaultRuntime(runtime *Runtime) {
	if runtime == nil {
		runtime = NewRuntimeWithCoreProvider(nil)
	}
	defaultRuntimeMu.Lock()
	defaultRuntime = runtime
	defaultRuntimeMu.Unlock()
}

func runtimeOrDefault(runtime *Runtime) *Runtime {
	if runtime != nil {
		return runtime
	}
	return DefaultRuntime()
}

func writeAuditRuntime(writer *auditWriter, event model.AuditEvent) {
	if writer == nil {
		return
	}
	writer.Enqueue(event)
}
