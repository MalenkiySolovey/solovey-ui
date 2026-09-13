package artifacts

import (
	"context"
	"errors"
	"time"

	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

type MetadataStore interface {
	ListArtifacts(context.Context) ([]protectionrepository.ArtifactModel, error)
	ProtectedArtifactOperations(context.Context) (map[string]string, error)
	DeleteArtifact(context.Context, uint) error
	PruneFirewallHistory(context.Context, int, int64, int64) (protectionrepository.FirewallHistoryPruneResult, error)
}

type nativeHistoryStore interface {
	PruneNativeFallbackHistory(context.Context, int, int64, int64) (protectionrepository.NativeFallbackHistoryPruneResult, error)
}

type receiptHistoryStore interface {
	PruneIdempotencyReceipts(context.Context, int, int64, int64) (protectionrepository.IdempotencyReceiptPruneResult, error)
}

type PruneResult struct {
	DeletedFilesets           int   `json:"deletedFilesets"`
	DeletedBytes              int64 `json:"deletedBytes"`
	DeletedOrphans            int   `json:"deletedOrphans"`
	DeletedOperations         int   `json:"deletedOperations"`
	DeletedTransitions        int   `json:"deletedTransitions"`
	DeletedNativeOperations   int   `json:"deletedNativeOperations"`
	DeletedNativeLocks        int   `json:"deletedNativeLocks"`
	DeletedNativeReservations int   `json:"deletedNativeReservations"`
	DeletedFrontingReceipts   int   `json:"deletedFrontingReceipts"`
	DeletedUDPGuardReceipts   int   `json:"deletedUdpGuardReceipts"`
	DeletedLocalProxyReceipts int   `json:"deletedLocalProxyReceipts"`
	Preserved                 int   `json:"preserved"`
}

const (
	maxTerminalArtifactLogicalBytes = int64(8 << 20)
	publicationOrphanGrace          = 5 * time.Minute
)

type Pruner struct {
	storage *Storage
	store   MetadataStore
	now     func() time.Time
}

func NewPruner(storage *Storage, store MetadataStore, now func() time.Time) *Pruner {
	if now == nil {
		now = time.Now
	}
	return &Pruner{storage: storage, store: store, now: now}
}

// Prune removes only terminal artifacts outside the full live-reference
// closure. Safe history is bounded independently by count, age, and logical
// bytes; owned publication debt is enumerated from fixed roots and names.
func (p *Pruner) Prune(ctx context.Context, keepCount, keepDays int) (PruneResult, error) {
	if p == nil || p.storage == nil || p.store == nil {
		return PruneResult{}, errors.New("artifact pruner is not initialized")
	}
	if keepCount < 1 || keepDays < 1 {
		return PruneResult{}, errors.New("artifact retention limits must be positive")
	}
	items, err := p.store.ListArtifacts(ctx)
	if err != nil {
		return PruneResult{}, err
	}
	protected, err := p.store.ProtectedArtifactOperations(ctx)
	if err != nil {
		return PruneResult{}, err
	}
	cutoff := p.now().Add(-time.Duration(keepDays) * 24 * time.Hour).Unix()
	result := PruneResult{}
	keptCount := 0
	var keptBytes int64
	for _, item := range items {
		_, active := protected[item.OperationID]
		fitsBytes := item.Bytes >= 0 && item.Bytes <= maxTerminalArtifactLogicalBytes-keptBytes
		if active || (keptCount < keepCount && item.CreatedAt >= cutoff && fitsBytes) {
			result.Preserved++
			if !active {
				keptCount++
				keptBytes += item.Bytes
			}
			continue
		}
		if err := p.storage.Remove(item.RelativePath); err != nil {
			return result, err
		}
		if err := p.store.DeleteArtifact(ctx, item.ID); err != nil {
			return result, err
		}
		result.DeletedFilesets++
		if item.Bytes > 0 {
			result.DeletedBytes = saturatingAdd(result.DeletedBytes, item.Bytes)
		}
	}
	remaining, err := p.store.ListArtifacts(ctx)
	if err != nil {
		return result, err
	}
	knownPaths := make(map[string]struct{}, len(remaining))
	knownOperations := make(map[string]struct{}, len(remaining))
	for _, item := range remaining {
		knownPaths[item.RelativePath] = struct{}{}
		knownOperations[item.OperationID] = struct{}{}
	}
	orphans, err := p.storage.SweepOwnedOrphans(knownPaths, knownOperations, protected, p.now().Add(-publicationOrphanGrace).Unix())
	if err != nil {
		return result, err
	}
	result.DeletedOrphans = orphans.Deleted
	result.DeletedBytes = saturatingAdd(result.DeletedBytes, orphans.DeletedBytes)
	result.Preserved += orphans.Preserved
	history, err := p.store.PruneFirewallHistory(ctx, keepCount, cutoff, maxTerminalArtifactLogicalBytes)
	if err != nil {
		return result, err
	}
	result.DeletedOperations = history.DeletedOperations
	result.DeletedTransitions = history.DeletedTransitions
	result.DeletedBytes = saturatingAdd(result.DeletedBytes, history.DeletedBytes)
	result.Preserved += history.Preserved
	if nativeStore, ok := p.store.(nativeHistoryStore); ok {
		nativeHistory, err := nativeStore.PruneNativeFallbackHistory(ctx, keepCount, cutoff, maxTerminalArtifactLogicalBytes)
		if err != nil {
			return result, err
		}
		result.DeletedNativeOperations = nativeHistory.DeletedOperations
		result.DeletedNativeLocks = nativeHistory.DeletedLocks
		result.DeletedNativeReservations = nativeHistory.DeletedReservations
		result.DeletedBytes = saturatingAdd(result.DeletedBytes, nativeHistory.DeletedBytes)
		result.Preserved += nativeHistory.Preserved
	}
	if receiptStore, ok := p.store.(receiptHistoryStore); ok {
		receipts, err := receiptStore.PruneIdempotencyReceipts(ctx, keepCount, cutoff, maxTerminalArtifactLogicalBytes)
		if err != nil {
			return result, err
		}
		result.DeletedFrontingReceipts = receipts.DeletedFrontingReceipts
		result.DeletedUDPGuardReceipts = receipts.DeletedUDPGuardReceipts
		result.DeletedLocalProxyReceipts = receipts.DeletedLocalProxyReceipts
		result.DeletedBytes = saturatingAdd(result.DeletedBytes, receipts.DeletedBytes)
		result.Preserved += receipts.Preserved
	}
	return result, nil
}
