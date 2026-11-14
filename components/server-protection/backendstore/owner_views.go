package backendstore

import (
	"context"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/durable"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

// These typed owner views remain outside the narrower feature Store interfaces.
func (s *Store) CreateIPAllowlist(ctx context.Context, item *durable.IPAllowlistModel) error {
	return backendError(s.repository.CreateIPAllowlist(ctx, (*repository.IPAllowlistModel)(item)))
}

func (s *Store) FrontingStateV2(ctx context.Context, resourceID string) (durable.FrontingStateV2Model, error) {
	value, err := s.repository.FrontingStateV2(ctx, resourceID)
	return durable.FrontingStateV2Model(value), backendError(err)
}

func (s *Store) LatestNativeFallbackOperations(ctx context.Context, resourceIDs []string) ([]durable.NativeFallbackOperationModel, error) {
	values, err := s.repository.LatestNativeFallbackOperations(ctx, resourceIDs)
	return projectSlice(values, fromNativeFallbackOperationModel), backendError(err)
}
