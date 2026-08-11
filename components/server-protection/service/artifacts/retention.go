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
}

type PruneResult struct {
	DeletedFilesets int   `json:"deletedFilesets"`
	DeletedBytes    int64 `json:"deletedBytes"`
	Preserved       int   `json:"preserved"`
}

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

// Prune removes only terminal artifacts that are both older than the days
// window and outside the newest count window.
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
	operationArtifacts := make(map[string]int, len(items))
	for _, item := range items {
		operationArtifacts[item.OperationID]++
	}
	for index, item := range items {
		_, active := protected[item.OperationID]
		if active || index < keepCount || item.CreatedAt >= cutoff {
			result.Preserved++
			continue
		}
		if err := p.storage.Remove(item.RelativePath); err != nil {
			return result, err
		}
		if err := p.store.DeleteArtifact(ctx, item.ID); err != nil {
			return result, err
		}
		operationArtifacts[item.OperationID]--
		if operationArtifacts[item.OperationID] == 0 {
			_ = p.storage.Remove(filepathSlash("operations", item.OperationID))
			_ = p.storage.Remove(filepathSlash("recovery", item.OperationID))
		}
		result.DeletedFilesets++
		result.DeletedBytes += item.Bytes
	}
	return result, nil
}
