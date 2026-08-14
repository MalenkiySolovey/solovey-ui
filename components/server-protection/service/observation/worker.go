package observation

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/publicsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/events"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	protectionresources "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/resources"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/scoring"
)

const (
	defaultBatchSize = 100
	maxBatchSize     = 1000
	finalFlushLimit  = 2 * time.Second
)

var ErrWorkerStopping = errors.New("server-protection observation worker is stopping")

type Status struct {
	Running        bool   `json:"running"`
	Received       uint64 `json:"received"`
	Persisted      uint64 `json:"persisted"`
	Batches        uint64 `json:"batches"`
	DroppedBus     uint64 `json:"droppedBus"`
	DroppedBatches uint64 `json:"droppedBatches"`
	Allowlisted    uint64 `json:"allowlisted"`
	QueueDepth     int    `json:"queueDepth"`
	LastError      string `json:"lastError,omitempty"`
	LastDropReason string `json:"lastDropReason,omitempty"`
}

type Worker struct {
	mu          sync.Mutex
	repository  *protectionrepository.Repository
	sub         *publicsurface.Subscription
	unregister  func()
	cancel      context.CancelFunc
	done        chan struct{}
	stopping    bool
	running     atomic.Bool
	received    atomic.Uint64
	persisted   atomic.Uint64
	batches     atomic.Uint64
	dropped     atomic.Uint64
	allowlisted atomic.Uint64
	lastError   atomic.Value // string
	lastDrop    atomic.Value // string
}

func NewWorker() *Worker {
	worker := &Worker{}
	worker.lastError.Store("")
	worker.lastDrop.Store("")
	return worker
}

func (w *Worker) Start(repository *protectionrepository.Repository) error {
	if repository == nil {
		return errors.New("server-protection observation repository is required")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopping || w.done != nil && !w.running.Load() {
		return ErrWorkerStopping
	}
	if w.running.Load() {
		return nil
	}
	settings, _, err := repository.LoadSettings(context.Background())
	if err != nil {
		return err
	}
	if settings.ObservationBufferSize == 0 {
		w.repository = repository
		return nil
	}
	subscription, unregister, err := publicsurface.SubscribeObservations(settings.ObservationBufferSize)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	w.repository = repository
	w.sub = subscription
	w.unregister = unregister
	w.cancel = cancel
	w.done = make(chan struct{})
	w.running.Store(true)
	w.lastError.Store("")
	w.lastDrop.Store("")
	go w.run(ctx, settings)
	return nil
}

func (w *Worker) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	w.mu.Lock()
	if !w.running.Load() {
		w.repository, w.sub, w.unregister, w.cancel, w.done = nil, nil, nil, nil, nil
		w.stopping = false
		w.mu.Unlock()
		return nil
	}
	done := w.done
	var unregister func()
	var cancel context.CancelFunc
	if !w.stopping {
		w.stopping = true
		unregister, cancel = w.unregister, w.cancel
		w.unregister, w.cancel = nil, nil
	}
	w.mu.Unlock()
	if unregister != nil {
		unregister()
	}
	if cancel != nil {
		cancel()
	}
	select {
	case <-done:
		w.mu.Lock()
		if w.done == done {
			w.repository, w.sub, w.done = nil, nil, nil
			w.stopping = false
		}
		w.mu.Unlock()
		return nil
	case <-ctx.Done():
		select {
		case <-done:
			w.mu.Lock()
			if w.done == done {
				w.repository, w.sub, w.done = nil, nil, nil
				w.stopping = false
			}
			w.mu.Unlock()
			return nil
		default:
			return ctx.Err()
		}
	}
}

func (w *Worker) Status() Status {
	w.mu.Lock()
	subscription := w.sub
	w.mu.Unlock()
	status := Status{
		Running: w.running.Load(), Received: w.received.Load(), Persisted: w.persisted.Load(),
		Batches: w.batches.Load(), DroppedBatches: w.dropped.Load(), Allowlisted: w.allowlisted.Load(),
		LastError: w.lastError.Load().(string), LastDropReason: w.lastDrop.Load().(string),
	}
	if subscription != nil {
		status.DroppedBus = subscription.Dropped()
		status.QueueDepth = subscription.Pending()
	}
	return status
}

func (w *Worker) run(ctx context.Context, settings domain.Settings) {
	done := w.done
	defer func() {
		w.running.Store(false)
		close(done)
	}()
	flushTicker := time.NewTicker(time.Duration(settings.ObservationFlushIntervalMS) * time.Millisecond)
	purgeTicker := time.NewTicker(10 * time.Minute)
	defer flushTicker.Stop()
	defer purgeTicker.Stop()
	batchSize := defaultBatchSize
	if batchSize > maxBatchSize {
		batchSize = maxBatchSize
	}
	batch := make([]publicsurface.Observation, 0, batchSize)
	for {
		select {
		case value := <-w.sub.Observations():
			w.received.Add(1)
			batch = append(batch, value)
			if len(batch) >= batchSize {
				w.flush(ctx, batch)
				batch = batch[:0]
			}
		case <-flushTicker.C:
			if len(batch) > 0 {
				w.flush(ctx, batch)
				batch = batch[:0]
			}
		case <-purgeTicker.C:
			w.purge(ctx)
		case <-ctx.Done():
			for {
				select {
				case value := <-w.sub.Observations():
					w.received.Add(1)
					batch = append(batch, value)
				default:
					flushCtx, cancel := context.WithTimeout(context.Background(), finalFlushLimit)
					if len(batch) > 0 {
						w.flush(flushCtx, batch)
					}
					cancel()
					return
				}
			}
		}
	}
}

func (w *Worker) flush(ctx context.Context, batch []publicsurface.Observation) {
	if len(batch) == 0 {
		return
	}
	persisted, err := w.processBatch(ctx, batch)
	if err != nil {
		w.lastError.Store(err.Error())
		w.lastDrop.Store("persistence_failed")
		w.dropped.Add(uint64(len(batch)))
		return
	}
	w.lastError.Store("")
	w.persisted.Add(uint64(persisted))
	w.batches.Add(1)
}

func (w *Worker) processBatch(ctx context.Context, observations []publicsurface.Observation) (int, error) {
	settings, _, err := w.repository.LoadSettings(ctx)
	if err != nil || !settings.Enabled {
		return 0, err
	}
	profiles, _, err := w.repository.ListProfiles(ctx, protectionrepository.ProfileFilter{PageQuery: protectionrepository.PageQuery{Page: 1, Limit: 1000}, Status: "active"})
	if err != nil {
		return 0, err
	}
	profilesByResource := make(map[string]protectionrepository.ProfileModel, len(profiles))
	for _, profile := range profiles {
		if profile.Enabled && profile.Mode == string(domain.ProfileModeRecordOnly) {
			profilesByResource[profile.ResourceID] = profile
		}
	}
	if len(profilesByResource) == 0 {
		return 0, nil
	}
	allowlist, err := w.repository.ActiveIPAllowlist(ctx, time.Now())
	if err != nil {
		return 0, err
	}
	currentResources := make(map[string]hostresources.ProtectableResource)
	for _, resource := range protectionresources.Snapshot(ctx, false).Resources {
		currentResources[resource.ID] = resource
	}
	states := make(map[string]scoring.ScoreState)
	loaded := make(map[string]bool)
	eventBatch := make([]events.ProbeEvent, 0, len(observations))
	clear := make([]scoring.ScoreKey, 0)
	policyBase := scoring.Policy{
		Threshold: settings.DefaultScoreThreshold, GraylistTTL: time.Duration(settings.DefaultGraylistTTLSeconds) * time.Second,
		MaxScore: settings.MaxScore, IPv6PrefixBits: settings.IPv6GraylistPrefixBits, DecayInterval: 10 * time.Minute,
		DedupeWindow: time.Minute, ClockSkewTolerance: time.Duration(settings.ClockSkewToleranceSeconds) * time.Second,
		SafeMetaMaxBytes: settings.SafeMetaMaxBytes,
	}
	for _, observation := range observations {
		profile, ok := profilesByResource[observation.ResourceID]
		if !ok {
			continue
		}
		address, err := parseSourceIP(observation.SourceIP)
		if err != nil || address.IsLoopback() {
			continue
		}
		prefix, err := scoring.NormalizeSourcePrefix(address, settings.IPv6GraylistPrefixBits)
		if err != nil {
			continue
		}
		key := scoring.ScoreKey{ResourceID: observation.ResourceID, Prefix: prefix}
		if prefixAllowed(prefix, allowlist) {
			clear = append(clear, key)
			w.allowlisted.Add(1)
			continue
		}
		resourceKind := domain.ResourceKind(observation.ResourceKind)
		if resourceKind.Validate() != nil {
			continue
		}
		meta := domain.SafeMeta{
			PathClass: observation.PathClass, UAClass: observation.UserAgentClass, MethodClass: observation.MethodClass,
			StatusClass: observation.StatusClass, BytesClass: observation.BytesClass, DurationClass: observation.DurationClass,
			ClassifierPolicyVersion: domain.ClassifierPolicyVersion,
		}.Bounded(settings.SafeMetaMaxBytes)
		if meta.Validate() != nil {
			continue
		}
		signals := observationSignals(observation)
		resource, exists := currentResources[observation.ResourceID]
		if !exists || resource.Fingerprint != profile.AcceptedFingerprint {
			signals = append(signals, domain.SignalResourceDrift)
		}
		if len(signals) == 0 {
			continue
		}
		stateKey := key.ResourceID + "\x00" + key.Prefix.String()
		state := states[stateKey]
		if !loaded[stateKey] {
			state, err = w.repository.LoadScore(ctx, key)
			if errors.Is(err, protectionrepository.ErrScoreNotFound) {
				state, err = scoring.ScoreState{}, nil
			}
			if err != nil {
				return 0, err
			}
			loaded[stateKey] = true
		}
		policy := policyBase
		policy.Threshold = profile.ScoreThreshold
		policy.GraylistTTL = time.Duration(profile.GraylistTTLSeconds) * time.Second
		for _, signalKind := range signals {
			result, err := scoring.ApplySignal(state, scoring.Signal{
				ResourceID: observation.ResourceID, Source: address, Kind: signalKind,
				ObservedAt: time.Unix(observation.ObservedAt, 0), SafeMeta: meta,
			}, policy, scoring.RealClock{})
			if err != nil {
				return 0, err
			}
			state = result.State
			if result.EventAccepted {
				eventBatch = append(eventBatch, events.ProbeEvent{
					ResourceID: observation.ResourceID, ResourceKind: resourceKind, SourcePrefix: prefix.String(),
					IPFamily: ipFamily(address), SignalKind: signalKind, ScoreDelta: domain.DefaultSignalDelta(signalKind),
					Action: domain.DecisionRecordOnly, SafeMeta: meta, ObservedAt: time.Unix(observation.ObservedAt, 0), DedupeKey: result.DedupeKey,
				})
			}
		}
		states[stateKey] = state
	}
	stateBatch := make([]scoring.ScoreState, 0, len(states))
	for _, state := range states {
		stateBatch = append(stateBatch, state)
	}
	if len(stateBatch) == 0 && len(eventBatch) == 0 && len(clear) == 0 {
		return 0, nil
	}
	if err := persistWithRetry(ctx, func() error {
		return w.repository.PersistObservationBatch(ctx, stateBatch, eventBatch, clear)
	}); err != nil {
		return 0, err
	}
	if w.batches.Load()%10 == 9 {
		w.purge(ctx)
	}
	return len(eventBatch), nil
}

func (w *Worker) purge(ctx context.Context) {
	settings, _, err := w.repository.LoadSettings(ctx)
	if err != nil {
		w.lastError.Store(err.Error())
		return
	}
	_, err = w.repository.Purge(ctx, events.RetentionPolicy{GlobalLimit: settings.RetentionGlobalLimit, PerResourceLimit: settings.RetentionPerResourceLimit})
	if err != nil {
		w.lastError.Store(err.Error())
	}
}

func observationSignals(value publicsurface.Observation) []domain.SignalKind {
	result := make([]domain.SignalKind, 0, 4)
	if value.PathClass == "scanner_path" || value.PathClass == "overlong_uri" || value.PathClass == "invalid_uri" {
		result = append(result, domain.SignalHTTPScannerPath)
	}
	switch value.UserAgentClass {
	case "ua_empty":
		result = append(result, domain.SignalHTTPEmptyUA)
	case "ua_scanner", "ua_overlong", "invalid_utf8":
		result = append(result, domain.SignalHTTPScannerUA)
	}
	if value.MethodClass == "unexpected" {
		result = append(result, domain.SignalHTTPUnexpectedMethod)
	}
	if value.RateLimited {
		result = append(result, domain.SignalRateLimited)
	}
	return result
}

func parseSourceIP(value string) (netip.Addr, error) {
	value = strings.TrimSpace(value)
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	address, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Addr{}, err
	}
	return address.Unmap(), nil
}

func prefixAllowed(source netip.Prefix, allowlist []netip.Prefix) bool {
	address := source.Addr().Unmap()
	for _, allowed := range allowlist {
		if allowed.Contains(address) {
			return true
		}
	}
	return false
}

func ipFamily(address netip.Addr) int {
	if address.Unmap().Is4() {
		return 4
	}
	return 6
}

func persistWithRetry(ctx context.Context, persist func() error) error {
	backoffs := []time.Duration{0, 50 * time.Millisecond, 100 * time.Millisecond, 250 * time.Millisecond}
	var err error
	for _, backoff := range backoffs {
		if backoff > 0 {
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		err = persist()
		if err == nil || !isBusyError(err) {
			return err
		}
	}
	return err
}

func isBusyError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "database is locked") || strings.Contains(message, "database is busy") || strings.Contains(message, "sqlite_busy")
}

var DefaultWorker = NewWorker()
