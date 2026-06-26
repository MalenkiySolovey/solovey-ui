package observability

import (
	dbhooks "github.com/MalenkiySolovey/solovey-ui/database/hooks"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"sync"
	"sync/atomic"
	"time"
)

type ServerMetrics interface {
	GetCpuPercent() float64
	GetMemInfo() map[string]interface{}
	GetNetInfo() map[string]interface{}
	GetSingboxInfo() map[string]interface{}
}

type Settings interface {
	GetObservabilityMemoryCapMB() (int, error)
}

type Service struct {
	Server   ServerMetrics
	Settings Settings
}
type observabilityStore struct {
	mu                   sync.RWMutex
	samples              map[ObservabilityBucket]*ringBuffer[ObservabilitySample]
	core                 map[ObservabilityBucket]*ringBuffer[CoreSample]
	lastMemoryWarnCapMB  int
	lastMemoryWarnUnix   int64
	lastAppliedMemoryCap int
}

var observabilityHistory = newObservabilityStore()
var observabilityMemoryCapCache = newObservabilityMemoryCapCache(time.Now)

func init() {
	dbhooks.RegisterResetHook("service.observability", resetObservabilityCaches)
}

type observabilityMemoryCapCacheState struct {
	capMB             atomic.Int64
	expiresAtUnixNano atomic.Int64
	refreshMu         sync.Mutex
	ttl               time.Duration
	now               func() time.Time
}

func newObservabilityMemoryCapCache(now func() time.Time) *observabilityMemoryCapCacheState {
	if now == nil {
		now = time.Now
	}
	cache := &observabilityMemoryCapCacheState{
		ttl: observabilityMemoryCapCacheTTL,
		now: now,
	}
	cache.capMB.Store(observabilityDefaultMemoryCapMB)
	return cache
}
func newObservabilityStore() *observabilityStore {
	caps := copyObservabilityCaps(observabilityDefaultBucketCaps)
	return &observabilityStore{
		samples:              newObservabilityRings[ObservabilitySample](caps),
		core:                 newObservabilityRings[CoreSample](caps),
		lastAppliedMemoryCap: observabilityDefaultMemoryCapMB,
	}
}
func resetObservabilityCaches() {
	observabilityMemoryCapCache = newObservabilityMemoryCapCache(time.Now)
	observabilityHistory.mu.Lock()
	observabilityHistory.lastMemoryWarnCapMB = 0
	observabilityHistory.lastMemoryWarnUnix = 0
	observabilityHistory.lastAppliedMemoryCap = observabilityDefaultMemoryCapMB
	observabilityHistory.applyCaps(capsForObservabilityMemory(observabilityDefaultMemoryCapMB), observabilityDefaultMemoryCapMB)
	observabilityHistory.mu.Unlock()
}
func (s *Service) CurrentObservabilitySample() ObservabilitySample {
	return ObservabilitySample{
		DateTime: time.Now().Unix(),
		CPU:      s.Server.GetCpuPercent(),
		Memory:   s.Server.GetMemInfo(),
		Network:  s.Server.GetNetInfo(),
	}
}
func (s *Service) CurrentCoreSample() CoreSample {
	return CoreSample{
		DateTime: time.Now().Unix(),
		Core:     s.Server.GetSingboxInfo(),
	}
}
func (s *Service) History() []ObservabilitySample {
	samples, err := s.HistoryForBucket(ObservabilityBucket2s)
	if err != nil {
		logger.Warning("read observability history failed:", err)
		return nil
	}
	return samples
}
func (s *Service) CoreHistory() []CoreSample {
	samples, err := s.CoreHistoryForBucket(ObservabilityBucket2s)
	if err != nil {
		logger.Warning("read core observability history failed:", err)
		return nil
	}
	return samples
}
func (s *Service) RecordObservabilitySample(bucket ObservabilityBucket, sample ObservabilitySample) error {
	if !IsValidObservabilityBucket(bucket) {
		return common.NewError("invalid observability bucket")
	}
	capMB := s.observabilityMemoryCapMB()
	observabilityHistory.mu.Lock()
	defer observabilityHistory.mu.Unlock()
	observabilityHistory.applyCapsIfNeeded(capMB)
	observabilityHistory.samples[bucket].append(sample)
	return nil
}
func (s *Service) RecordCoreSample(bucket ObservabilityBucket, sample CoreSample) error {
	if !IsValidObservabilityBucket(bucket) {
		return common.NewError("invalid observability bucket")
	}
	capMB := s.observabilityMemoryCapMB()
	observabilityHistory.mu.Lock()
	defer observabilityHistory.mu.Unlock()
	observabilityHistory.applyCapsIfNeeded(capMB)
	observabilityHistory.core[bucket].append(sample)
	return nil
}
func (s *Service) HistoryForBucket(bucket ObservabilityBucket) ([]ObservabilitySample, error) {
	if !IsValidObservabilityBucket(bucket) {
		return nil, common.NewError("invalid observability bucket")
	}
	observabilityHistory.mu.RLock()
	defer observabilityHistory.mu.RUnlock()
	return observabilityHistory.samples[bucket].snapshot(), nil
}
func (s *Service) HistoryForBucketSince(bucket ObservabilityBucket, since int64) ([]ObservabilitySample, error) {
	samples, err := s.HistoryForBucket(bucket)
	if err != nil {
		return nil, err
	}
	return filterObservabilitySamplesSince(samples, since), nil
}
func (s *Service) CoreHistoryForBucket(bucket ObservabilityBucket) ([]CoreSample, error) {
	if !IsValidObservabilityBucket(bucket) {
		return nil, common.NewError("invalid observability bucket")
	}
	observabilityHistory.mu.RLock()
	defer observabilityHistory.mu.RUnlock()
	return observabilityHistory.core[bucket].snapshot(), nil
}
func (s *Service) CoreHistoryForBucketSince(bucket ObservabilityBucket, since int64) ([]CoreSample, error) {
	samples, err := s.CoreHistoryForBucket(bucket)
	if err != nil {
		return nil, err
	}
	return filterCoreSamplesSince(samples, since), nil
}
func (s *Service) MetricHistory(metric ObservabilityMetric, bucket ObservabilityBucket, since int64) ([]ObservabilityMetricSample, error) {
	if !IsValidObservabilityMetric(metric) {
		return nil, common.NewError("invalid observability metric")
	}
	samples, err := s.HistoryForBucketSince(bucket, since)
	if err != nil {
		return nil, err
	}
	result := make([]ObservabilityMetricSample, 0, len(samples))
	for _, sample := range samples {
		value, ok := sample.metricValue(metric)
		if !ok {
			continue
		}
		result = append(result, ObservabilityMetricSample{
			DateTime: sample.DateTime,
			Value:    value,
		})
	}
	return result, nil
}
func (s *Service) observabilityMemoryCapMB() int {
	return observabilityMemoryCapCache.Get(func() int {
		capMB, err := s.loadObservabilityMemoryCapMB()
		if err != nil || capMB <= 0 {
			return observabilityDefaultMemoryCapMB
		}
		return capMB
	})
}
func (s *Service) loadObservabilityMemoryCapMB() (int, error) {
	capMB, err := s.Settings.GetObservabilityMemoryCapMB()
	if err != nil || capMB <= 0 {
		return observabilityDefaultMemoryCapMB, err
	}
	return capMB, nil
}
func (c *observabilityMemoryCapCacheState) Get(load func() int) int {
	now := c.now()
	if capMB, ok := c.cached(now); ok {
		return capMB
	}
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()
	now = c.now()
	if capMB, ok := c.cached(now); ok {
		return capMB
	}
	capMB := load()
	if capMB <= 0 {
		capMB = observabilityDefaultMemoryCapMB
	}
	c.capMB.Store(int64(capMB))
	c.expiresAtUnixNano.Store(now.Add(c.ttl).UnixNano())
	return capMB
}
func (c *observabilityMemoryCapCacheState) cached(now time.Time) (int, bool) {
	capMB := c.capMB.Load()
	if capMB <= 0 {
		return 0, false
	}
	if now.UnixNano() >= c.expiresAtUnixNano.Load() {
		return 0, false
	}
	return int(capMB), true
}
func capsForObservabilityMemory(capMB int) map[ObservabilityBucket]int {
	caps := copyObservabilityCaps(observabilityDefaultBucketCaps)
	capBytes := int64(capMB) * 1024 * 1024
	defaultBytes := estimatedObservabilityBytes(observabilityDefaultBucketCaps)
	if capBytes >= defaultBytes {
		return caps
	}
	if capBytes <= 0 {
		capBytes = 1
	}
	scale := float64(capBytes) / float64(defaultBytes)
	for bucket, defaultCap := range observabilityDefaultBucketCaps {
		capacity := int(float64(defaultCap) * scale)
		if capacity < 1 {
			capacity = 1
		}
		caps[bucket] = capacity
	}
	return caps
}
func estimatedObservabilityBytes(caps map[ObservabilityBucket]int) int64 {
	var total int64
	for _, cap := range caps {
		total += int64(cap) * (observabilitySampleEstimateBytes + observabilityCoreSampleBytes)
	}
	return total
}
func copyObservabilityCaps(src map[ObservabilityBucket]int) map[ObservabilityBucket]int {
	dst := make(map[ObservabilityBucket]int, len(src))
	for bucket, capacity := range src {
		dst[bucket] = capacity
	}
	return dst
}
func filterObservabilitySamplesSince(samples []ObservabilitySample, since int64) []ObservabilitySample {
	if since <= 0 {
		return samples
	}
	filtered := make([]ObservabilitySample, 0, len(samples))
	for _, sample := range samples {
		if sample.DateTime > since {
			filtered = append(filtered, sample)
		}
	}
	return filtered
}
func filterCoreSamplesSince(samples []CoreSample, since int64) []CoreSample {
	if since <= 0 {
		return samples
	}
	filtered := make([]CoreSample, 0, len(samples))
	for _, sample := range samples {
		if sample.DateTime > since {
			filtered = append(filtered, sample)
		}
	}
	return filtered
}
func newObservabilityRings[T any](caps map[ObservabilityBucket]int) map[ObservabilityBucket]*ringBuffer[T] {
	rings := make(map[ObservabilityBucket]*ringBuffer[T], len(observabilityDefaultBucketCaps))
	for bucket := range observabilityDefaultBucketCaps {
		rings[bucket] = newRingBuffer[T](caps[bucket])
	}
	return rings
}
func (h *observabilityStore) applyCaps(caps map[ObservabilityBucket]int, capMB int) {
	for bucket := range observabilityDefaultBucketCaps {
		capacity := caps[bucket]
		if h.samples[bucket] == nil {
			h.samples[bucket] = newRingBuffer[ObservabilitySample](capacity)
		}
		if h.core[bucket] == nil {
			h.core[bucket] = newRingBuffer[CoreSample](capacity)
		}
		h.samples[bucket].setCap(capacity)
		h.core[bucket].setCap(capacity)
	}
	h.warnIfCapped(caps, capMB)
	h.lastAppliedMemoryCap = capMB
}
func (h *observabilityStore) applyCapsIfNeeded(capMB int) {
	if h.lastAppliedMemoryCap == capMB {
		return
	}
	h.applyCaps(capsForObservabilityMemory(capMB), capMB)
}
func (h *observabilityStore) warnIfCapped(caps map[ObservabilityBucket]int, capMB int) {
	if estimatedObservabilityBytes(caps) >= estimatedObservabilityBytes(observabilityDefaultBucketCaps) {
		return
	}
	now := time.Now().Unix()
	if h.lastMemoryWarnCapMB == capMB && now-h.lastMemoryWarnUnix < observabilityWarnMemoryMinSeconds {
		return
	}
	h.lastMemoryWarnCapMB = capMB
	h.lastMemoryWarnUnix = now
	logger.Warningf("observability history capacities reduced by observabilityMemoryCapMB=%d", capMB)
}

type ringBuffer[T any] struct {
	items []T
	next  int
	full  bool
}

func newRingBuffer[T any](capacity int) *ringBuffer[T] {
	if capacity < 1 {
		capacity = 1
	}
	return &ringBuffer[T]{
		items: make([]T, 0, capacity),
	}
}
func (r *ringBuffer[T]) append(item T) {
	if cap(r.items) == 0 {
		r.items = make([]T, 0, 1)
	}
	if len(r.items) < cap(r.items) {
		r.items = append(r.items, item)
		if len(r.items) == cap(r.items) {
			r.full = true
			r.next = 0
		}
		return
	}
	r.items[r.next] = item
	r.next = (r.next + 1) % len(r.items)
	r.full = true
}
func (r *ringBuffer[T]) setCap(capacity int) {
	if capacity < 1 {
		capacity = 1
	}
	if cap(r.items) == capacity {
		return
	}
	current := r.snapshot()
	if len(current) > capacity {
		current = current[len(current)-capacity:]
	}
	r.items = make([]T, 0, capacity)
	r.next = 0
	r.full = false
	for _, item := range current {
		r.append(item)
	}
}
func (r *ringBuffer[T]) snapshot() []T {
	if len(r.items) == 0 {
		return nil
	}
	out := make([]T, 0, len(r.items))
	if !r.full {
		return append(out, r.items...)
	}
	out = append(out, r.items[r.next:]...)
	out = append(out, r.items[:r.next]...)
	return out
}
