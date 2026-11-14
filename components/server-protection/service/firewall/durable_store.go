package firewall

import (
	"context"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/durable"
)

// BaselineStore contains only the durable operations consumed by this owner.
type BaselineStore interface {
	ListIPAllowlist(ctx context.Context, page protectionrepository.PageQuery) ([]protectionrepository.IPAllowlistModel, int64, error)
}

// RecoveryStore contains only the durable operations consumed by this owner.
type RecoveryStore interface {
	SaveArtifact(context.Context, *protectionrepository.ArtifactModel) error
	ArtifactByOperation(ctx context.Context, operationID string) (protectionrepository.ArtifactModel, error)
	FirewallAuthority(ctx context.Context) (protectionrepository.FirewallAuthoritySnapshot, error)
}
