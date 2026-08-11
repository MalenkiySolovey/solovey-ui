package management

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

const maxEvidenceProviders = 16

type EvidenceProvider interface {
	ProviderID() string
	RecoveryPaths(context.Context, time.Time) ([]hostresources.RecoveryPathV1, error)
}

type evidenceEntry struct {
	id       uint64
	provider EvidenceProvider
}

type EvidenceSnapshot struct {
	Paths       []hostresources.RecoveryPathV1 `json:"paths"`
	ReasonCodes []string                       `json:"reasonCodes,omitempty"`
	GeneratedAt int64                          `json:"generatedAt"`
}

type EvidenceRegistry struct {
	mu        sync.RWMutex
	next      uint64
	providers map[uint64]EvidenceProvider
}

func NewEvidenceRegistry() *EvidenceRegistry {
	return &EvidenceRegistry{providers: make(map[uint64]EvidenceProvider)}
}

func (r *EvidenceRegistry) Register(provider EvidenceProvider) func() {
	if provider == nil || !safeToken(provider.ProviderID()) {
		return func() {}
	}
	r.mu.Lock()
	if len(r.providers) >= maxEvidenceProviders {
		r.mu.Unlock()
		panic("management evidence registry capacity exceeded")
	}
	id := r.next
	r.next++
	r.providers[id] = provider
	r.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { r.mu.Lock(); delete(r.providers, id); r.mu.Unlock() }) }
}

func (r *EvidenceRegistry) Snapshot(ctx context.Context, now time.Time) EvidenceSnapshot {
	now = now.UTC()
	r.mu.RLock()
	providers := make([]evidenceEntry, 0, len(r.providers))
	for id, provider := range r.providers {
		providers = append(providers, evidenceEntry{id: id, provider: provider})
	}
	r.mu.RUnlock()
	sort.Slice(providers, func(i, j int) bool {
		left, right := providers[i].provider.ProviderID(), providers[j].provider.ProviderID()
		if left == right {
			return providers[i].id < providers[j].id
		}
		return left < right
	})
	result := EvidenceSnapshot{Paths: []hostresources.RecoveryPathV1{}, GeneratedAt: now.Unix()}
	seenProviders := make(map[string]bool, len(providers))
	seenPaths := make(map[string]bool)
	for _, entry := range providers {
		providerID := entry.provider.ProviderID()
		if seenProviders[providerID] {
			result.ReasonCodes = appendReason(result.ReasonCodes, "evidence_provider_ambiguous")
			continue
		}
		seenProviders[providerID] = true
		paths, err := entry.provider.RecoveryPaths(ctx, now)
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

func RegisterEvidenceProvider(provider EvidenceProvider) func() {
	return DefaultEvidence.Register(provider)
}

func RecoveryEvidence(ctx context.Context, now time.Time) EvidenceSnapshot {
	return DefaultEvidence.Snapshot(ctx, now)
}
