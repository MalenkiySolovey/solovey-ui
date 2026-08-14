package management

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

const (
	maxEvidenceProviders = 16
	evidenceTimeout      = 5 * time.Second
)

type EvidenceProvider interface {
	ProviderID() string
	RecoveryPaths(context.Context, time.Time) ([]hostresources.RecoveryPathV1, error)
}

type evidenceEntry struct {
	token      uint64
	providerID string
	provider   EvidenceProvider
}

type EvidenceSnapshot struct {
	Paths       []hostresources.RecoveryPathV1 `json:"paths"`
	ReasonCodes []string                       `json:"reasonCodes,omitempty"`
	GeneratedAt int64                          `json:"generatedAt"`
}

type EvidenceRegistry struct {
	mu        sync.RWMutex
	next      uint64
	providers map[uint64]evidenceEntry
}

func NewEvidenceRegistry() *EvidenceRegistry {
	return &EvidenceRegistry{providers: make(map[uint64]evidenceEntry)}
}

func (r *EvidenceRegistry) Register(provider EvidenceProvider) (func(), error) {
	providerID, ok := evidenceProviderID(provider)
	if !ok {
		return nil, errors.New("management evidence provider is invalid")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, current := range r.providers {
		if current.providerID == providerID {
			return nil, errors.New("management evidence provider is already registered: " + providerID)
		}
	}
	if len(r.providers) >= maxEvidenceProviders {
		return nil, errors.New("management evidence registry capacity exceeded")
	}
	id := r.next
	r.next++
	r.providers[id] = evidenceEntry{token: id, providerID: providerID, provider: provider}
	var once sync.Once
	return func() { once.Do(func() { r.mu.Lock(); delete(r.providers, id); r.mu.Unlock() }) }, nil
}

func (r *EvidenceRegistry) Snapshot(ctx context.Context, now time.Time) EvidenceSnapshot {
	now = now.UTC()
	r.mu.RLock()
	providers := make([]evidenceEntry, 0, len(r.providers))
	for _, provider := range r.providers {
		providers = append(providers, provider)
	}
	r.mu.RUnlock()
	sort.Slice(providers, func(i, j int) bool {
		left, right := providers[i].providerID, providers[j].providerID
		if left == right {
			return providers[i].token < providers[j].token
		}
		return left < right
	})
	result := EvidenceSnapshot{Paths: []hostresources.RecoveryPathV1{}, GeneratedAt: now.Unix()}
	seenPaths := make(map[string]bool)
	for _, entry := range providers {
		paths, err := recoveryPaths(ctx, entry.provider, now)
		if err != nil {
			result.ReasonCodes = appendReason(result.ReasonCodes, "evidence_provider_unavailable")
			continue
		}
		if len(paths) > 4096 {
			paths = paths[:4096]
			result.ReasonCodes = appendReason(result.ReasonCodes, "evidence_inventory_truncated")
		}
		for _, path := range paths {
			if seenPaths[path.ID] || !hostresources.RecoveryPathValid(path, now) {
				result.ReasonCodes = appendReason(result.ReasonCodes, "recovery_evidence_invalid")
				continue
			}
			seenPaths[path.ID] = true
			result.Paths = append(result.Paths, path)
		}
	}
	sort.Slice(result.Paths, func(i, j int) bool { return result.Paths[i].ID < result.Paths[j].ID })
	sort.Strings(result.ReasonCodes)
	return result
}

func evidenceProviderID(provider EvidenceProvider) (id string, ok bool) {
	if provider == nil {
		return "", false
	}
	defer func() {
		if recover() != nil {
			id, ok = "", false
		}
	}()
	id = strings.TrimSpace(provider.ProviderID())
	return id, safeToken(id)
}

func recoveryPaths(ctx context.Context, provider EvidenceProvider, now time.Time) ([]hostresources.RecoveryPathV1, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	callCtx, cancel := context.WithTimeout(ctx, evidenceTimeout)
	defer cancel()
	type result struct {
		paths []hostresources.RecoveryPathV1
		err   error
	}
	done := make(chan result, 1)
	go func() {
		value := result{}
		defer func() {
			if recover() != nil {
				value.paths = nil
				value.err = errors.New("management evidence provider panicked")
			}
			done <- value
		}()
		value.paths, value.err = provider.RecoveryPaths(callCtx, now)
	}()
	select {
	case value := <-done:
		return value.paths, value.err
	case <-callCtx.Done():
		return nil, callCtx.Err()
	}
}

func safeToken(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 || strings.ContainsAny(value, "/\\?#&={}[]<>\"'\r\n\t ") {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._:@+-", r) {
			continue
		}
		return false
	}
	return true
}

var DefaultEvidence = NewEvidenceRegistry()

func RegisterEvidenceProvider(provider EvidenceProvider) (func(), error) {
	return DefaultEvidence.Register(provider)
}

func RecoveryEvidence(ctx context.Context, now time.Time) EvidenceSnapshot {
	return DefaultEvidence.Snapshot(ctx, now)
}
