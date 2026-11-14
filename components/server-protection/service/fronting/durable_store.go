package fronting

import (
	"context"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/durable"
)

// RecoveryStore contains only the durable operations consumed by this owner.
type RecoveryStore interface {
	SaveArtifact(context.Context, *protectionrepository.ArtifactModel) error
	ArtifactByOperation(ctx context.Context, operationID string) (protectionrepository.ArtifactModel, error)
}

// SemanticStore contains only the durable operations consumed by this owner.
type SemanticStore interface {
	AmbiguousFrontingReceiptV2(ctx context.Context, action, key, digest string, now int64) error
	ClaimFrontingReceiptV2(ctx context.Context, value protectionrepository.FrontingIdempotencyV2Model) (protectionrepository.FrontingIdempotencyV2Model, bool, error)
	CompleteFrontingReceiptV2(ctx context.Context, action, key, digest string, operationID string, operationRevision int, response []byte, now int64) error
	FrontingReceiptV2(ctx context.Context, action, key string) (protectionrepository.FrontingIdempotencyV2Model, error)
	FrontingStatesV2(ctx context.Context) ([]protectionrepository.FrontingStateV2Model, error)
	OperationByID(ctx context.Context, operationID string) (protectionrepository.OperationLockModel, error)
	ProjectFrontingStateV2(ctx context.Context, value protectionrepository.FrontingStateV2Model) error
}
