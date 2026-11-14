package localproxy

import (
	"context"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/durable"
)

// StateRepository contains only the durable operations consumed by this owner.
type StateRepository interface {
	AdmitMaintenance(context.Context) (context.Context, func(), error)
	AmbiguousLocalProxyReceipt(ctx context.Context, id uint) error
	BeginLocalProxyReceipt(ctx context.Context, action, key, digest string) (protectionrepository.LocalProxyIdempotencyV1Model, bool, error)
	BindLocalProxyReceiptOperation(ctx context.Context, id uint, operationID string, operationRevision int) error
	CompleteLocalProxyReceipt(ctx context.Context, id uint, operationID string, operationRevision int, response any) error
	LocalProxyStateByOperation(ctx context.Context, operationID string) (protectionrepository.LocalProxyStateV1Model, error)
	LocalProxyStates(ctx context.Context) ([]protectionrepository.LocalProxyStateV1Model, error)
	OperationByID(ctx context.Context, operationID string) (protectionrepository.OperationLockModel, error)
	ReplayLocalProxyReceipt(ctx context.Context, action, key, digest string) (protectionrepository.LocalProxyIdempotencyV1Model, bool, error)
	SaveLocalProxyState(ctx context.Context, value protectionrepository.LocalProxyStateV1Model) error
}
