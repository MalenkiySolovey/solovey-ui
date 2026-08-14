package hostsurface

import (
	"context"
	"encoding/hex"
	"errors"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"
)

type Provider interface {
	SourceID() string
	Observe(context.Context, Limits) (Observation, error)
}

type observationTimeoutProvider interface {
	ObservationTimeout() time.Duration
}

const (
	maxProviderObservationTimeout = 80 * time.Second
	maxProviders                  = 128
)

type Registry struct {
	mu        sync.RWMutex
	next      uint64
	providers map[uint64]providerEntry
	last      Snapshot
	now       func() time.Time
	flightMu  sync.Mutex
	inFlight  chan struct{}
}

type providerEntry struct {
	token    uint64
	sourceID string
	provider Provider
}

func NewRegistry() *Registry {
	return &Registry{providers: make(map[uint64]providerEntry), now: time.Now}
}

func (r *Registry) Register(provider Provider) (func(), error) {
	sourceID, ok := providerSourceID(provider)
	if !ok {
		return nil, errors.New("host surface provider is invalid")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, current := range r.providers {
		if current.sourceID == sourceID {
			return nil, errors.New("host surface provider is already registered: " + sourceID)
		}
	}
	if len(r.providers) >= maxProviders {
		return nil, errors.New("host surface provider registry capacity exceeded")
	}
	id := r.next
	r.next++
	r.providers[id] = providerEntry{token: id, sourceID: sourceID, provider: provider}
	var once sync.Once
	return func() { once.Do(func() { r.mu.Lock(); delete(r.providers, id); r.mu.Unlock() }) }, nil
}

func (r *Registry) Snapshot() Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return cloneSnapshot(r.last)
}

func (r *Registry) Reconcile(ctx context.Context) Snapshot {
	if ctx == nil {
		ctx = context.Background()
	}
	r.flightMu.Lock()
	if r.inFlight != nil {
		done := r.inFlight
		r.flightMu.Unlock()
		select {
		case <-ctx.Done():
			return r.Snapshot()
		case <-done:
			return r.Snapshot()
		}
	}
	done := make(chan struct{})
	r.inFlight = done
	r.flightMu.Unlock()
	defer func() {
		r.flightMu.Lock()
		close(done)
		r.inFlight = nil
		r.flightMu.Unlock()
	}()

	r.mu.RLock()
	providers := make([]providerEntry, 0, len(r.providers))
	for _, provider := range r.providers {
		providers = append(providers, provider)
	}
	r.mu.RUnlock()
	sort.Slice(providers, func(i, j int) bool { return providers[i].sourceID < providers[j].sourceID })

	now := r.now().UTC()
	result := Snapshot{GeneratedAt: now.Unix(), Facts: []HostSurfaceFactV1{}}
	ownerRevisions := make([]string, 0, len(providers))
	if len(providers) == 0 {
		result.Facts = append(result.Facts, unknownFact("hostsurface", "hostsurface_provider_absent", now))
	}
	limits := DefaultLimits()
	for _, entry := range providers {
		provider := entry.provider
		remaining := limits.MaxSockets - len(result.Facts)
		if remaining <= 0 {
			result.Truncated = true
			result.ReasonCodes = append(result.ReasonCodes, "inventory_truncated")
			break
		}
		providerLimits := limits
		providerLimits.MaxSockets = remaining
		providerTimeout := limits.Timeout
		if configured, ok := provider.(observationTimeoutProvider); ok {
			requested := safeObservationTimeout(configured)
			if requested > providerTimeout && requested <= maxProviderObservationTimeout {
				providerTimeout = requested
			}
		}
		providerLimits.Timeout = providerTimeout
		observation, err := observeProvider(ctx, provider, providerLimits, providerTimeout)
		if err != nil {
			result.Facts = append(result.Facts, unknownFact(entry.sourceID, "hostsurface_provider_unavailable", now))
			continue
		}
		if len(observation.Facts) > remaining {
			observation.Facts = observation.Facts[:remaining]
			observation.Truncated = true
			observation.ReasonCodes = append(observation.ReasonCodes, "inventory_truncated")
		}
		for _, fact := range observation.Facts {
			fact = cloneFact(fact)
			fact.Schema = SchemaV1
			fact.Source = strings.TrimSpace(fact.Source)
			if fact.Source == "" {
				fact.Source = entry.sourceID
			}
			if fact.FirstSeen == 0 {
				fact.FirstSeen = now.Unix()
			}
			if fact.LastSeen == 0 {
				fact.LastSeen = now.Unix()
			}
			if fact.ExpiresAt == 0 {
				fact.ExpiresAt = now.Add(90 * time.Second).Unix()
			}
			fact = sanitizeFact(fact, now)
			// IDs are derived from the bounded fact shape. A provider-supplied ID
			// must never carry a local path, secret, or transient free-form text.
			fact.ID = StableID(fact)
			fact.Truncated = fact.Truncated || observation.Truncated
			if fact.Truncated {
				fact.ReasonCodes = append(fact.ReasonCodes, "inventory_truncated")
			}
			fact.ReasonCodes = normalizeReasons(fact.ReasonCodes)
			result.Facts = append(result.Facts, fact)
		}
		result.Truncated = result.Truncated || observation.Truncated
		result.ReasonCodes = append(result.ReasonCodes, observation.ReasonCodes...)
		if observation.OwnerObservationRevision != "" {
			ownerRevisions = append(ownerRevisions, entry.sourceID+":"+observation.OwnerObservationRevision)
		}
	}
	result.ReasonCodes = normalizeReasons(result.ReasonCodes)
	sort.Strings(ownerRevisions)
	if len(ownerRevisions) > 0 {
		result.OwnerObservationRevision = OwnerObservationSetRevision(result.Facts, ownerRevisions)
	}
	sort.Slice(result.Facts, func(i, j int) bool { return result.Facts[i].ID < result.Facts[j].ID })
	r.mu.Lock()
	r.last = cloneSnapshot(result)
	r.mu.Unlock()
	return cloneSnapshot(result)
}

func providerSourceID(provider Provider) (sourceID string, ok bool) {
	if provider == nil {
		return "", false
	}
	defer func() {
		if recover() != nil {
			sourceID, ok = "", false
		}
	}()
	sourceID = strings.TrimSpace(provider.SourceID())
	return sourceID, safeFactToken(sourceID, 128)
}

func safeObservationTimeout(provider observationTimeoutProvider) (timeout time.Duration) {
	defer func() {
		if recover() != nil {
			timeout = 0
		}
	}()
	return provider.ObservationTimeout()
}

func observeProvider(ctx context.Context, provider Provider, limits Limits, timeout time.Duration) (Observation, error) {
	providerCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	type result struct {
		observation Observation
		err         error
	}
	done := make(chan result, 1)
	go func() {
		value := result{}
		defer func() {
			if recover() != nil {
				value.observation = Observation{}
				value.err = errors.New("host surface provider panicked")
			}
			done <- value
		}()
		value.observation, value.err = provider.Observe(providerCtx, limits)
	}()
	select {
	case value := <-done:
		return value.observation, value.err
	case <-providerCtx.Done():
		return Observation{}, providerCtx.Err()
	}
}

func unknownFact(source, reason string, now time.Time) HostSurfaceFactV1 {
	source = strings.TrimSpace(source)
	if !safeFactToken(source, 128) {
		source = "unknown"
	}
	fact := HostSurfaceFactV1{
		Schema: SchemaV1, Network: NetworkUnknown, Family: FamilyUnknown, Exposure: ExposureUnknown,
		OwnershipMode: OwnershipUnmanaged, FirstSeen: now.Unix(), LastSeen: now.Unix(), ExpiresAt: now.Add(90 * time.Second).Unix(),
		Source: source, Classification: ClassificationUnknownOwner, ReasonCodes: []string{reason},
	}
	fact.ID = StableID(fact) + ":unknown"
	return fact
}

func normalizeReasons(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, min(len(values), 32))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if !safeFactToken(value, 64) {
			value = "hostsurface_reason_invalid"
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
		if len(out) == 32 {
			break
		}
	}
	sort.Strings(out)
	return out
}

func sanitizeFact(fact HostSurfaceFactV1, now time.Time) HostSurfaceFactV1 {
	invalid := false
	unobserved := fact.Classification == ClassificationUnobserved
	switch fact.Network {
	case NetworkTCP, NetworkUDP, NetworkUnknown:
	default:
		fact.Network = NetworkUnknown
		invalid = true
	}
	switch fact.Family {
	case FamilyIPv4, FamilyIPv6, FamilyUnknown:
	default:
		fact.Family = FamilyUnknown
		invalid = true
	}
	switch fact.Exposure {
	case ExposurePublic, ExposurePrivate, ExposureLocal, ExposureUnknown:
	default:
		fact.Exposure = ExposureUnknown
		invalid = true
	}
	switch fact.OwnershipMode {
	case OwnershipManaged, OwnershipExternalManaged, OwnershipUnmanaged:
	default:
		fact.OwnershipMode = OwnershipUnmanaged
		invalid = true
	}
	switch fact.Classification {
	case ClassificationExpectedManaged, ClassificationExpectedExternal, ClassificationLocalOnly, ClassificationUnexpectedPublic, ClassificationUnknownOwner, ClassificationManagedExact, ClassificationForeign, ClassificationUnobserved, ClassificationStale:
	default:
		fact.Classification = ClassificationUnknownOwner
		invalid = true
	}
	if fact.Network == NetworkUnknown || !unobserved && (fact.Family == FamilyUnknown || fact.Exposure == ExposureUnknown) {
		invalid = true
	}
	if fact.ConfidenceBP < 0 || fact.ConfidenceBP > 10000 {
		fact.ConfidenceBP = 0
		invalid = true
	}
	if fact.Protocol != "" && !safeFactToken(fact.Protocol, 32) {
		fact.Protocol = ""
		invalid = true
	}
	if !safeFactToken(fact.Source, 128) {
		fact.Source = "unknown"
		invalid = true
	}
	address, addressErr := netip.ParseAddr(strings.TrimSpace(fact.Bind))
	if unobserved && fact.Family == FamilyUnknown && (strings.TrimSpace(fact.Bind) == "*" || strings.TrimSpace(fact.Bind) == "0.0.0.0" || strings.TrimSpace(fact.Bind) == "::") {
		fact.Bind = strings.TrimSpace(fact.Bind)
	} else if addressErr != nil || address.Unmap().String() != strings.TrimSpace(fact.Bind) {
		fact.Bind = "*"
		fact.Network = NetworkUnknown
		fact.Family = FamilyUnknown
		fact.Exposure = ExposureUnknown
		invalid = true
	} else {
		fact.Bind = address.Unmap().String()
		wantFamily := FamilyIPv6
		if address.Unmap().Is4() {
			wantFamily = FamilyIPv4
		}
		if fact.Family != wantFamily {
			fact.Family = FamilyUnknown
			invalid = true
		}
	}
	if fact.Port == 0 || fact.FirstSeen <= 0 || fact.LastSeen < fact.FirstSeen || fact.LastSeen > now.Add(5*time.Minute).Unix() || fact.ExpiresAt <= fact.LastSeen || fact.ExpiresAt > fact.LastSeen+300 {
		fact.FirstSeen = now.Unix()
		fact.LastSeen = now.Unix()
		fact.ExpiresAt = now.Unix()
		invalid = true
	}
	if fact.SocketInode != "" && !numericToken(fact.SocketInode) {
		fact.SocketInode = ""
		invalid = true
	}
	if fact.Process.StartTime != "" && !numericToken(fact.Process.StartTime) {
		fact.Process.StartTime = ""
		invalid = true
	}
	if fact.Process.ExeDigest != "" && !sha256Token(fact.Process.ExeDigest) {
		fact.Process.ExeDigest = ""
		invalid = true
	}
	if fact.Process.PID != nil && *fact.Process.PID <= 0 {
		fact.Process.PID = nil
		invalid = true
	}
	if fact.Process.UID != nil && *fact.Process.UID < 0 {
		fact.Process.UID = nil
		invalid = true
	}
	if fact.Process.GID != nil && *fact.Process.GID < 0 {
		fact.Process.GID = nil
		invalid = true
	}
	if fact.Process.ParentPID != nil && *fact.Process.ParentPID < 0 || fact.Process.SessionID != nil && *fact.Process.SessionID < 0 {
		invalid = true
	}
	if fact.Service.SystemdUnit != "" && !safeFactToken(fact.Service.SystemdUnit, 128) {
		fact.Service.SystemdUnit = ""
		invalid = true
	}
	if fact.Service.ContainerCgroup != "" && !safeFactToken(fact.Service.ContainerCgroup, 128) {
		fact.Service.ContainerCgroup = ""
		invalid = true
	}
	if fact.ListenerOwner != nil {
		owner := *fact.ListenerOwner
		owner.Socket.CoverageFamilies = append([]Family(nil), fact.ListenerOwner.Socket.CoverageFamilies...)
		fact.ListenerOwner = &owner
		if !owner.Valid(time.Unix(owner.ObservedAt, 0).UTC()) {
			invalid = true
		}
	}
	if fact.Classification == ClassificationManagedExact && (fact.ListenerOwner == nil || !fact.ListenerOwner.Valid(now) || fact.IsStale(now)) {
		invalid = true
	}
	if fact.RegisteredResourceID != "" && !safeFactToken(fact.RegisteredResourceID, 256) {
		fact.RegisteredResourceID = ""
		invalid = true
	}
	if fact.DesiredOwner != "" && !safeFactToken(fact.DesiredOwner, 128) {
		fact.DesiredOwner = ""
		invalid = true
	}
	if fact.ConfigurationRevision != "" && !safeFactToken(fact.ConfigurationRevision, 128) {
		fact.ConfigurationRevision = ""
		invalid = true
	}
	if invalid {
		fact.ConfidenceBP = 0
		fact.Classification = ClassificationUnknownOwner
		fact.OwnershipMode = OwnershipUnmanaged
		fact.ReasonCodes = append(fact.ReasonCodes, "hostsurface_fact_invalid")
	}
	return fact
}

func safeFactToken(value string, limit int) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > limit || strings.ContainsAny(value, "/\\?#&={}[]<>\"'\r\n\t ") {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._:@+-", r) {
			continue
		}
		return false
	}
	return true
}

func numericToken(value string) bool {
	if value == "" || len(value) > 32 {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func sha256Token(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func cloneSnapshot(value Snapshot) Snapshot {
	result := value
	result.Facts = append([]HostSurfaceFactV1(nil), value.Facts...)
	for index := range result.Facts {
		result.Facts[index] = cloneFact(value.Facts[index])
	}
	result.ReasonCodes = append([]string(nil), value.ReasonCodes...)
	result.OwnerObservationRevision = value.OwnerObservationRevision
	return result
}

func cloneFact(value HostSurfaceFactV1) HostSurfaceFactV1 {
	result := value
	result.ReasonCodes = append([]string(nil), value.ReasonCodes...)
	result.Process = cloneProcessFact(value.Process)
	result.Service = cloneServiceFact(value.Service)
	if value.ListenerOwner != nil {
		owner := *value.ListenerOwner
		owner.Socket.CoverageFamilies = append([]Family(nil), value.ListenerOwner.Socket.CoverageFamilies...)
		owner.Process = cloneProcessFact(value.ListenerOwner.Process)
		owner.Service = cloneServiceFact(value.ListenerOwner.Service)
		result.ListenerOwner = &owner
	}
	return result
}

func cloneProcessFact(value ProcessFact) ProcessFact {
	result := value
	if value.PID != nil {
		copy := *value.PID
		result.PID = &copy
	}
	if value.ParentPID != nil {
		copy := *value.ParentPID
		result.ParentPID = &copy
	}
	if value.SessionID != nil {
		copy := *value.SessionID
		result.SessionID = &copy
	}
	if value.UID != nil {
		copy := *value.UID
		result.UID = &copy
	}
	if value.GID != nil {
		copy := *value.GID
		result.GID = &copy
	}
	return result
}

func cloneServiceFact(value ServiceFact) ServiceFact {
	result := value
	if value.MainPID != nil {
		copy := *value.MainPID
		result.MainPID = &copy
	}
	return result
}

var Default = NewRegistry()

func Register(provider Provider) (func(), error) { return Default.Register(provider) }
func Reconcile(ctx context.Context) Snapshot     { return Default.Reconcile(ctx) }
func CurrentSnapshot() Snapshot                  { return Default.Snapshot() }
